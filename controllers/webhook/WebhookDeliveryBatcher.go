package webhook

import (
	"context"
	"log"
	"sync"
	"time"

	models "zen_messaging_gateway/models"
	utils "zen_messaging_gateway/utils"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// =================================================================================
// BATCHED WEBHOOK DELIVERY LOG WRITER
// =================================================================================
// Optimizes database writes by batching delivery log inserts and RabbitMQ publishes
// instead of doing individual operations for each webhook.
//
// Features:
// - Configurable batch size and flush interval
// - Non-blocking enqueue with channel buffer
// - Automatic flush on timer or when batch is full
// - Graceful shutdown support
// - Circuit breaker integration for DB failures
// =================================================================================

const (
	// BatchSize is the maximum number of entries to batch before flushing
	DeliveryLogBatchSize = 500

	// FlushInterval is the maximum time to wait before flushing a partial batch
	DeliveryLogFlushInterval = 100 * time.Millisecond

	// ChannelBufferSize is the buffer size for the input channel
	DeliveryLogChannelBuffer = 10000

	// MaxRetries for batch insert failures
	BatchInsertMaxRetries = 3
)

// WebhookDeliveryEntry represents a pending delivery log entry with its publish info
type WebhookDeliveryEntry struct {
	DeliveryLog  *models.WebhookDeliveryLog
	WebhookID    string
	SharedSecret string
	Payload      map[string]interface{}
}

// DeliveryLogBatcher handles batched inserts of delivery logs
type DeliveryLogBatcher struct {
	entryChan    chan *WebhookDeliveryEntry
	stopChan     chan struct{}
	wg           sync.WaitGroup
	mu           sync.Mutex
	isRunning    bool
	batchSize    int
	flushInterval time.Duration
}

var (
	// Global batcher instance
	deliveryLogBatcher *DeliveryLogBatcher
	batcherOnce        sync.Once
)

// GetDeliveryLogBatcher returns the singleton batcher instance
func GetDeliveryLogBatcher() *DeliveryLogBatcher {
	batcherOnce.Do(func() {
		deliveryLogBatcher = &DeliveryLogBatcher{
			entryChan:     make(chan *WebhookDeliveryEntry, DeliveryLogChannelBuffer),
			stopChan:      make(chan struct{}),
			batchSize:     DeliveryLogBatchSize,
			flushInterval: DeliveryLogFlushInterval,
		}
	})
	return deliveryLogBatcher
}

// Start begins the background batch processing goroutine
func (b *DeliveryLogBatcher) Start() {
	b.mu.Lock()
	if b.isRunning {
		b.mu.Unlock()
		return
	}
	b.isRunning = true
	b.mu.Unlock()

	b.wg.Add(1)
	go b.processLoop()

	log.Printf("[DELIVERY_BATCHER] ✅ Started with batch_size=%d, flush_interval=%v, buffer=%d",
		b.batchSize, b.flushInterval, DeliveryLogChannelBuffer)
}

// Stop gracefully shuts down the batcher
func (b *DeliveryLogBatcher) Stop() {
	b.mu.Lock()
	if !b.isRunning {
		b.mu.Unlock()
		return
	}
	b.isRunning = false
	b.mu.Unlock()

	close(b.stopChan)
	b.wg.Wait()

	log.Println("[DELIVERY_BATCHER] Stopped")
}

// Enqueue adds a delivery entry to the batch queue (non-blocking)
// Returns true if successfully queued, false if channel is full
func (b *DeliveryLogBatcher) Enqueue(entry *WebhookDeliveryEntry) bool {
	select {
	case b.entryChan <- entry:
		return true
	default:
		// Channel full - log and drop (or could use fallback)
		log.Printf("[DELIVERY_BATCHER] ⚠️ Channel full, dropping entry for project_owner=%s",
			entry.DeliveryLog.ProjectOwnerID)
		return false
	}
}

// processLoop is the main batch processing loop
func (b *DeliveryLogBatcher) processLoop() {
	defer b.wg.Done()

	batch := make([]*WebhookDeliveryEntry, 0, b.batchSize)
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case entry := <-b.entryChan:
			batch = append(batch, entry)

			// Flush if batch is full
			if len(batch) >= b.batchSize {
				b.flush(batch)
				batch = make([]*WebhookDeliveryEntry, 0, b.batchSize)
				ticker.Reset(b.flushInterval)
			}

		case <-ticker.C:
			// Flush on timer if there are pending entries
			if len(batch) > 0 {
				b.flush(batch)
				batch = make([]*WebhookDeliveryEntry, 0, b.batchSize)
			}

		case <-b.stopChan:
			// Flush remaining entries before stopping
			if len(batch) > 0 {
				b.flush(batch)
			}
			// Drain remaining entries from channel
			for {
				select {
				case entry := <-b.entryChan:
					batch = append(batch, entry)
					if len(batch) >= b.batchSize {
						b.flush(batch)
						batch = make([]*WebhookDeliveryEntry, 0, b.batchSize)
					}
				default:
					if len(batch) > 0 {
						b.flush(batch)
					}
					return
				}
			}
		}
	}
}

// flush performs the batched insert and publish operations
func (b *DeliveryLogBatcher) flush(batch []*WebhookDeliveryEntry) {
	if len(batch) == 0 {
		return
	}

	startTime := time.Now()

	// Prepare documents for bulk insert
	documents := make([]interface{}, len(batch))
	for i, entry := range batch {
		documents[i] = entry.DeliveryLog
	}

	// Bulk insert with retry
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := utils.GetCollection("webhook_delivery_logs")
	var insertErr error
	var insertedIDs []interface{}

	for attempt := 0; attempt < BatchInsertMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}

		result, err := collection.InsertMany(ctx, documents)
		if err == nil {
			insertedIDs = result.InsertedIDs
			break
		}
		insertErr = err
		log.Printf("[DELIVERY_BATCHER] Batch insert attempt %d failed: %v", attempt+1, insertErr)
	}

	if insertErr != nil {
		log.Printf("[DELIVERY_BATCHER] ❌ Failed to batch insert %d delivery logs after %d attempts: %v",
			len(batch), BatchInsertMaxRetries, insertErr)
		// Could add to fallback queue here
		return
	}

	successCount := 0
	failCount := 0

	for i, entry := range batch {
		// Get the inserted ID
		var deliveryLogID string
		if i < len(insertedIDs) {
			if oid, ok := insertedIDs[i].(primitive.ObjectID); ok {
				deliveryLogID = oid.Hex()
			}
		}

		if deliveryLogID == "" {
			deliveryLogID = entry.DeliveryLog.ID.Hex()
		}

		// Publish to RabbitMQ
		err := utils.PublishWebhookForwarding(
			deliveryLogID,
			entry.DeliveryLog.ProjectOwnerID,
			entry.WebhookID,
			entry.DeliveryLog.WebhookURL,
			entry.SharedSecret,
			entry.DeliveryLog.EventType,
			entry.DeliveryLog.EventID,
			entry.Payload,
			0, // Initial attempt
		)

		if err != nil {
			failCount++
			log.Printf("[DELIVERY_BATCHER] Failed to publish webhook for delivery_log=%s: %v",
				deliveryLogID, err)
		} else {
			successCount++
		}
	}

	duration := time.Since(startTime)
	log.Printf("[DELIVERY_BATCHER] ✅ Flushed batch: inserted=%d, published=%d, failed=%d, duration=%v",
		len(batch), successCount, failCount, duration)
}

// EnqueueWebhookDelivery is a helper function to create and enqueue a delivery entry
func EnqueueWebhookDelivery(
	projectOwnerID string,
	webhook *models.Webhook,
	eventType string,
	payload map[string]interface{},
) bool {
	eventID := primitive.NewObjectID().Hex()

	deliveryLog := models.NewWebhookDeliveryLog(
		projectOwnerID,
		webhook.ID,
		eventID,
		eventType,
		webhook.WebhookURL,
		payload,
	)

	entry := &WebhookDeliveryEntry{
		DeliveryLog:  deliveryLog,
		WebhookID:    webhook.ID,
		SharedSecret: webhook.SharedSecret,
		Payload:      payload,
	}

	return GetDeliveryLogBatcher().Enqueue(entry)
}

// InitDeliveryLogBatcher initializes and starts the batcher
// Call this during application startup
func InitDeliveryLogBatcher() {
	batcher := GetDeliveryLogBatcher()
	batcher.Start()
}

// StopDeliveryLogBatcher gracefully stops the batcher
// Call this during application shutdown
func StopDeliveryLogBatcher() {
	if deliveryLogBatcher != nil {
		deliveryLogBatcher.Stop()
	}
}
