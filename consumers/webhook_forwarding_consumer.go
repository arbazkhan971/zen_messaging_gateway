package consumers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	models "zen_messaging_gateway/models"
	utils "zen_messaging_gateway/utils"

	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	MaxRetryAttempts               = 3
	WebhookTimeout                 = 15 * time.Second
	MaxResponseBodyBytes           = 10 * 1024 // 10KB
	WebhookCircuitBreakerThreshold = 10
	WebhookCircuitBreakerTimeout   = 5 * time.Minute
	ConsumerPrefetchCount          = 1000
	// Number of goroutines concurrently delivering webhooks.
	// Each goroutine holds one in-flight HTTP request, so this is effectively
	// the max concurrent outbound connections.
	WorkerPoolSize = 100
)

// Delayed retry configuration - fixed 3 second delays
var RetryDelays = []time.Duration{
	3 * time.Second,
	3 * time.Second,
	3 * time.Second,
}

// =============================================================================
// Shared HTTP client — created once, reused across all workers.
// A new Transport per request destroys connection pooling.
// =============================================================================
var sharedHTTPClient = &http.Client{
	Timeout: WebhookTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        500,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     90 * time.Second,
	},
}

// =============================================================================
// Circuit breaker — per webhook URL so one bad endpoint doesn't block others.
// =============================================================================
type WebhookCircuitBreaker struct {
	mu           sync.Mutex
	failures     int
	lastFailTime time.Time
	state        int // 0=closed 1=open 2=half-open
}

func (cb *WebhookCircuitBreaker) canCall() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case 0:
		return true
	case 1:
		if time.Since(cb.lastFailTime) > WebhookCircuitBreakerTimeout {
			cb.state = 2
			return true
		}
		return false
	case 2:
		return true
	}
	return false
}

func (cb *WebhookCircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = 0
}

func (cb *WebhookCircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailTime = time.Now()
	if cb.failures >= WebhookCircuitBreakerThreshold {
		cb.state = 1
		log.Printf("[WEBHOOK_FORWARDER] ⚠️ Circuit breaker opened for endpoint after %d failures", cb.failures)
	}
}

// Per-URL circuit breakers so one bad destination doesn't block all others.
var (
	circuitBreakersMu sync.RWMutex
	circuitBreakers   = make(map[string]*WebhookCircuitBreaker)
)

func getCircuitBreaker(url string) *WebhookCircuitBreaker {
	// Fast path — already exists
	circuitBreakersMu.RLock()
	cb, ok := circuitBreakers[url]
	circuitBreakersMu.RUnlock()
	if ok {
		return cb
	}
	// Slow path — create
	circuitBreakersMu.Lock()
	defer circuitBreakersMu.Unlock()
	if cb, ok = circuitBreakers[url]; ok {
		return cb
	}
	cb = &WebhookCircuitBreaker{}
	circuitBreakers[url] = cb
	return cb
}

// =============================================================================
// Async DB write queue — batches MongoDB updates off the hot path.
// Workers push a dbUpdate and immediately ack/nack the AMQP delivery.
// =============================================================================
type dbUpdate struct {
	id     primitive.ObjectID
	update bson.M
}

var dbUpdateCh = make(chan dbUpdate, 10000)

func init() {
	// Start a small pool of DB writers to drain the async queue.
	for i := 0; i < 4; i++ {
		go dbWriteWorker()
	}
}

func dbWriteWorker() {
	collection := utils.GetCollection("webhook_delivery_logs")
	for u := range dbUpdateCh {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := collection.UpdateOne(ctx, bson.M{"_id": u.id}, u.update)
		cancel()
		if err != nil {
			log.Printf("[WEBHOOK_FORWARDER] Async DB write failed: %v", err)
		}
	}
}

// pushDBUpdate enqueues a non-blocking DB update. Drops if the queue is full
// to avoid blocking the delivery goroutine.
func pushDBUpdate(id primitive.ObjectID, update bson.M) {
	select {
	case dbUpdateCh <- dbUpdate{id: id, update: update}:
	default:
		log.Printf("[WEBHOOK_FORWARDER] DB update queue full, dropping update for %s", id.Hex())
	}
}

// =============================================================================
// Consumer entry point
// =============================================================================
func StartWebhookForwardingConsumer(ch *amqp.Channel) <-chan struct{} {
	done := make(chan struct{})
	startTime := time.Now()

	if ch == nil {
		log.Println("[WEBHOOK_FORWARDER] Cannot start consumer: channel is nil")
		close(done)
		return done
	}

	log.Printf("[WEBHOOK_FORWARDER] DEBUG: Setting QoS (prefetch=%d) on channel %p", ConsumerPrefetchCount, ch)

	if err := ch.Qos(ConsumerPrefetchCount, 0, false); err != nil {
		log.Printf("[WEBHOOK_FORWARDER] Failed to set QoS: %v (channel=%p)", err, ch)
		close(done)
		return done
	}

	log.Printf("[WEBHOOK_FORWARDER] DEBUG: Registering consumer on queue %s (channel=%p)", utils.WebhookForwardingQueueName, ch)

	msgs, err := ch.Consume(
		utils.WebhookForwardingQueueName,
		"webhook-forwarder",
		false, // manual ack
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Printf("[WEBHOOK_FORWARDER] Failed to start consumer: %v (channel=%p)", err, ch)
		close(done)
		return done
	}

	log.Printf("[WEBHOOK_FORWARDER] ✅ Started webhook forwarding consumer (workers=%d, QoS=%d, channel=%p)",
		WorkerPoolSize, ConsumerPrefetchCount, ch)

	go func() {
		messagesProcessed := 0
		defer func() {
			uptime := time.Since(startTime)
			log.Printf("[WEBHOOK_FORWARDER] Consumer stopped (channel=%p, uptime=%v, messages=%d)",
				ch, uptime, messagesProcessed)
			close(done)
		}()

		log.Printf("[WEBHOOK_FORWARDER] DEBUG: Entering message processing loop (channel=%p)", ch)

		// Semaphore-based worker pool: limits concurrent in-flight deliveries.
		sem := make(chan struct{}, WorkerPoolSize)

		for msg := range msgs {
			messagesProcessed++

			// Check circuit breaker for this specific URL before dispatching.
			// We need to parse the URL from the body first — do a quick peek.
			webhookURL := peekWebhookURL(msg.Body)
			cb := getCircuitBreaker(webhookURL)
			if !cb.canCall() {
				log.Printf("[WEBHOOK_FORWARDER] ⚠️ Circuit breaker open for %s, requeueing", webhookURL)
				msg.Nack(false, true)
				time.Sleep(1 * time.Second)
				continue
			}

			// Acquire a slot from the pool (blocks if all workers are busy).
			sem <- struct{}{}

			// Copy the delivery for the goroutine (msg is reused by the range loop).
			delivery := msg
			go func() {
				defer func() { <-sem }()
				processWebhookForwarding(delivery, ch)
			}()
		}

		// Drain the semaphore — wait for all in-flight workers to finish.
		for i := 0; i < WorkerPoolSize; i++ {
			sem <- struct{}{}
		}

		uptime := time.Since(startTime)
		log.Printf("[WEBHOOK_FORWARDER] Consumer channel closed (channel=%p, uptime=%v, messages=%d)",
			ch, uptime, messagesProcessed)
	}()

	return done
}

// peekWebhookURL does a minimal JSON parse to extract only the webhook_url field,
// avoiding a full Unmarshal just for the circuit breaker check.
func peekWebhookURL(body []byte) string {
	var peek struct {
		WebhookURL string `json:"webhook_url"`
	}
	if err := json.Unmarshal(body, &peek); err == nil {
		return peek.WebhookURL
	}
	return ""
}

// =============================================================================
// Per-message processing (runs in a goroutine from the pool)
// =============================================================================
func processWebhookForwarding(delivery amqp.Delivery, _ *amqp.Channel) {
	var payload utils.WebhookForwardingPayload
	if err := json.Unmarshal(delivery.Body, &payload); err != nil {
		log.Printf("[WEBHOOK_FORWARDER] Failed to unmarshal payload: %v", err)
		delivery.Nack(false, false)
		return
	}

	log.Printf("[WEBHOOK_FORWARDER] Processing webhook: project_owner=%s, event_type=%s, attempt=%d",
		payload.ProjectOwnerID, payload.EventType, payload.AttemptCount)

	startTime := time.Now()
	success, statusCode, responseBody, errorMsg := deliverWebhook(payload)
	duration := time.Since(startTime)

	deliveryLogObjectID, err := primitive.ObjectIDFromHex(payload.DeliveryLogID)
	if err != nil {
		log.Printf("[WEBHOOK_FORWARDER] Invalid delivery log ID: %v", err)
		delivery.Nack(false, false)
		return
	}

	cb := getCircuitBreaker(payload.WebhookURL)

	if success {
		cb.recordSuccess()

		// Ack immediately, write to DB asynchronously.
		delivery.Ack(false)

		pushDBUpdate(deliveryLogObjectID, bson.M{
			"$set": bson.M{
				"status":               models.DeliveryStatusSuccess,
				"status_code":          statusCode,
				"response_body":        truncateString(responseBody, 1000),
				"last_attempt_at":      time.Now(),
				"delivery_duration_ms": duration.Milliseconds(),
			},
			"$inc": bson.M{"attempt_count": 1},
		})

		log.Printf("[WEBHOOK_FORWARDER] ✅ Webhook delivered: project_owner=%s, status=%d, duration=%v",
			payload.ProjectOwnerID, statusCode, duration)
		return
	}

	// Permanent failures — malformed request, 4xx (excluding 429)
	isClientError := strings.Contains(errorMsg, "client error HTTP")
	isPermanentFailure := strings.Contains(errorMsg, "failed to marshal payload") ||
		strings.Contains(errorMsg, "failed to create request")

	if isClientError || isPermanentFailure {
		log.Printf("[WEBHOOK_FORWARDER] ❌ Permanent failure - sending to DLQ: project_owner=%s, status=%d",
			payload.ProjectOwnerID, statusCode)
		delivery.Nack(false, false)
		return
	}

	// 429 is a downstream rate limit — retry but don't trip the circuit breaker.
	isRateLimit := strings.Contains(errorMsg, "retryable HTTP 429")
	if !isRateLimit {
		cb.recordFailure()
	}

	payload.AttemptCount++

	if payload.AttemptCount >= MaxRetryAttempts {
		// Ack + async DB write, then let it go to DLQ via Nack below.
		pushDBUpdate(deliveryLogObjectID, bson.M{
			"$set": bson.M{
				"status":               models.DeliveryStatusFailed,
				"status_code":          statusCode,
				"error_message":        errorMsg,
				"last_attempt_at":      time.Now(),
				"delivery_duration_ms": duration.Milliseconds(),
			},
			"$inc": bson.M{"attempt_count": 1},
		})

		log.Printf("[WEBHOOK_FORWARDER] ❌ Webhook failed permanently after %d attempts: project_owner=%s, status=%d, error=%s",
			MaxRetryAttempts, payload.ProjectOwnerID, statusCode, errorMsg)
		delivery.Nack(false, false)
		return
	}

	// Schedule delayed retry via RabbitMQ delayed exchange.
	retryDelay := getRetryDelay(payload.AttemptCount - 1)

	pushDBUpdate(deliveryLogObjectID, bson.M{
		"$set": bson.M{
			"status":               models.DeliveryStatusRetrying,
			"status_code":          statusCode,
			"error_message":        errorMsg,
			"last_attempt_at":      time.Now(),
			"next_retry_at":        time.Now().Add(retryDelay),
			"delivery_duration_ms": duration.Milliseconds(),
		},
		"$inc": bson.M{"attempt_count": 1},
	})

	log.Printf("[WEBHOOK_FORWARDER] ⏳ Webhook delivery failed (attempt %d/%d), scheduling retry in %s: project_owner=%s, status=%d",
		payload.AttemptCount, MaxRetryAttempts, retryDelay, payload.ProjectOwnerID, statusCode)

	if err := publishDelayedRetry(payload, retryDelay); err != nil {
		log.Printf("[WEBHOOK_FORWARDER] Failed to schedule delayed retry: %v", err)
		delivery.Nack(false, true)
		return
	}

	delivery.Ack(false)
}

// =============================================================================
// HTTP delivery — uses shared client, no internal retry loop.
// RabbitMQ-level retries via delayed exchange handle transient failures cleanly.
// =============================================================================
func deliverWebhook(payload utils.WebhookForwardingPayload) (bool, int, string, string) {
	webhookData := map[string]any{
		"event_type": payload.EventType,
		"event_id":   payload.EventID,
		"timestamp":  payload.Timestamp,
		"data":       payload.Payload,
	}

	jsonData, err := json.Marshal(webhookData)
	if err != nil {
		return false, 0, "", fmt.Sprintf("failed to marshal payload: %v", err)
	}

	signature := utils.GenerateWebhookSignature(jsonData, payload.SharedSecret)

	req, err := http.NewRequest("POST", payload.WebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return false, 0, "", fmt.Sprintf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Type", payload.EventType)
	req.Header.Set("X-Event-ID", payload.EventID)
	req.Header.Set("User-Agent", "Serri-Webhook-Forwarder/1.0")
	req.Header.Set("X-Attempt-Count", fmt.Sprintf("%d", payload.AttemptCount))
	if payload.SharedSecret != "" {
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// Use the single shared client — connection reuse is free this way.
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return false, 0, "", fmt.Sprintf("request failed: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBodyBytes))
	if err != nil {
		log.Printf("[WEBHOOK_FORWARDER] Failed to read response body: %v", err)
		responseBody = []byte{}
	}

	statusCode := resp.StatusCode
	switch {
	case statusCode >= 200 && statusCode < 300:
		return true, statusCode, string(responseBody), ""
	case statusCode == 408 || statusCode == 429 || statusCode >= 500:
		return false, statusCode, string(responseBody), fmt.Sprintf("retryable HTTP %d: %s", statusCode, string(responseBody))
	case statusCode >= 400 && statusCode < 500:
		return false, statusCode, string(responseBody), fmt.Sprintf("client error HTTP %d: %s", statusCode, string(responseBody))
	default:
		return false, statusCode, string(responseBody), fmt.Sprintf("unknown HTTP %d: %s", statusCode, string(responseBody))
	}
}

func getRetryDelay(attemptNumber int) time.Duration {
	if attemptNumber < 0 {
		attemptNumber = 0
	}
	if attemptNumber >= len(RetryDelays) {
		return RetryDelays[len(RetryDelays)-1]
	}
	return RetryDelays[attemptNumber]
}

func publishDelayedRetry(payload utils.WebhookForwardingPayload, delay time.Duration) error {
	err := utils.PublishWebhookForwardingWithDelay(
		payload.DeliveryLogID,
		payload.ProjectOwnerID,
		payload.WebhookID,
		payload.WebhookURL,
		payload.SharedSecret,
		payload.EventType,
		payload.EventID,
		payload.Payload,
		payload.AttemptCount,
		delay,
	)
	if err != nil {
		log.Printf("[WEBHOOK_FORWARDER] ❌ Failed to publish delayed retry: %v", err)
		return err
	}
	log.Printf("[WEBHOOK_FORWARD] Published delayed retry: project_owner=%s, event_type=%s, attempt=%d, delay=%s",
		payload.ProjectOwnerID, payload.EventType, payload.AttemptCount, delay)
	return nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
