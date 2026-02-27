// =====================================================================================
// CAMPAIGN CONSUMER: IN-MEMORY DEDUPLICATION & DELAY HANDLING (NO DB/REDIS)
// =====================================================================================
// - Each consumer keeps an in-memory set of processed message IDs (message-id header).
// - If a duplicate message-id is seen, skip processing and ack immediately.
// - This prevents duplicates per process lifetime (not across restarts).
// - Use manual ack. Use prefetch for throughput.
// - Delay is handled by RabbitMQ x-delayed-message exchange.
// =====================================================================================
// StartCampaignJobConsumer starts a consumer for campaign jobs with in-memory deduplication.
// handler: func(body []byte, msgID string) error (should send the message, return error if failed)
//
// Example handler for campaign job (replace with real logic):
// func campaignJobHandler(body []byte, msgID string) error {
//     // Unmarshal, send message, etc.
//     return nil
// }
//
// Usage:
// err := StartCampaignJobConsumer(campaignJobHandler)
// if err != nil { log.Fatal(err) }
// =====================================================================================
// CAMPAIGN PRODUCER: BATCH ENQUEUE WITH DELAY AND DEDUPLICATION (NO DB/REDIS)
// =====================================================================================
// - Each message must have a unique MessageId (e.g., campaign_id:phone_number) in the header.
// - Use publisher confirms for reliability.
// - Use x-delay header for scheduled delivery (delayed exchange).
// - No DB/Redis: Producer must not enqueue duplicates in the same batch (use a local map).
// - Consumer must use in-memory deduplication (see consumer code for details).
// =====================================================================================

// BatchEnqueueCampaignJobs publishes jobs to RabbitMQ with delay and deduplication.
// jobs: slice of job structs (must be serializable to JSON)
// scheduleTimes: slice of schedule times (epoch ms) for each job (same length as jobs)
// routingKey: usually "campaign.send2"

// =====================================================================================
// RABBITMQ CAMPAIGN SENDING: SCALABILITY PLAN (10 MILLION SCALE)
// =====================================================================================
//
// 1. Producer (Enqueuer) Scalability:
//    - Use batch publishing: Instead of publishing one message at a time, use channel.Publish in batches (e.g., 1000 at a time) to reduce network roundtrips.
//    - Use multiple goroutines for publishing: Parallelize job creation and publishing to RabbitMQ.
//    - Use publisher confirms: Enable confirms to ensure reliability at scale.
//    - Use persistent messages: Set delivery mode to persistent so jobs survive broker restarts.
//    - Avoid large message payloads: Store only essential data in the queue, use S3/DB for large blobs.
//    - Monitor queue length and broker health: Use Prometheus/Grafana or RabbitMQ Management UI.
//
// 2. Consumer Scalability:
//    - Run multiple consumer instances (horizontal scaling): Deploy many pods/VMs/containers, each running the consumer code.
//    - Use prefetch count: Set a reasonable prefetch (e.g., 100-1000) to balance throughput and memory.
//    - Use manual ack: Only ack after successful processing to avoid message loss.
//    - Use idempotent processing: Ensure duplicate deliveries do not cause issues.
//    - Use retry and DLQ: Retry failed jobs, send to DLQ after max attempts.
//    - Use connection pooling: Avoid reconnect storms, reuse connections.
//    - Monitor consumer lag and failures: Alert on high lag or error rates.
//
// 3. Broker/Infrastructure:
//    - Use a RabbitMQ cluster with mirrored queues for HA and throughput.
//    - Use SSD-backed storage for fast disk I/O.
//    - Tune RabbitMQ memory, file descriptors, and network settings for high throughput.
//    - Use lazy queues for very large backlogs (messages stored on disk, not RAM).
//    - Regularly test failover and recovery.
//
// 4. Application Logic:
//    - Producer: After campaign creation, enqueue all jobs (one per recipient) as fast as possible, in parallel.
//    - Consumer: Each consumer only processes and sends one message at a time, but many consumers run in parallel.
//    - Use metrics and logging for observability.
//
// 5. Example: To send 10 million messages in 1 hour, need ~2800 messages/sec throughput. Use 50+ consumer instances, each processing 50-100 messages/sec.
// =====================================================================================
// amqpURL = "amqp://prod-user:YourVeryStrongAndSecretPassword!@34.10.207.221:15672/"

package utils

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	models "zen_messaging_gateway/models"

	"github.com/streadway/amqp"
)

// Global connection and channel variables
var (
	RabbitMQConn    *amqp.Connection
	RabbitMQChannel *amqp.Channel

	// Connection pool management
	connPoolMutex sync.RWMutex
	connPool      *amqp.Connection
	connPoolRefs  int      // Reference count for connection pool
	maxConnRefs   int = 50 // Reduced - use connection pooling instead

	// Topology declaration control
	topologyMutex    sync.Mutex
	topologyDeclared bool // Global flag to prevent duplicate topology declarations

	// Global deduplication map and mutex
	globalDedupMap   = make(map[string]time.Time)
	globalDedupMutex sync.Mutex
)

// StartDedupCleanup starts a goroutine to periodically clean up old message IDs
func StartDedupCleanup() {
	go func() {
		cleanupTicker := time.NewTicker(1 * time.Hour)
		defer cleanupTicker.Stop()

		for range cleanupTicker.C {
			globalDedupMutex.Lock()
			cutoff := time.Now().Add(-24 * time.Hour) // Remove IDs older than 24 hours
			removeCount := 0

			for id, timestamp := range globalDedupMap {
				if timestamp.Before(cutoff) {
					delete(globalDedupMap, id)
					removeCount++
				}
			}
			log.Printf("Cleaned up %d old message IDs, %d remaining in global dedup map", removeCount, len(globalDedupMap))
			globalDedupMutex.Unlock()
		}
	}()
}

// Constants for queue and exchange names
const (
	CampaignQueueName = "campaign_sending_jobs2929"
	CampaignDLQName   = "campaign_sending_dlq2"
	CampaignDLX       = "campaign_sending_dlx2"
	DelayedExchange   = "scheduled_sending_exchange229"

	// Size-tiered delayed exchanges for scaling by batch size
	DelayedExchange1K   = "scheduled_sending_exchange229_1k"
	DelayedExchange10K  = "scheduled_sending_exchange229_10k"
	DelayedExchange50K  = "scheduled_sending_exchange229_50k"
	DelayedExchange100K = "scheduled_sending_exchange229_100k"
	DelayedExchange500K = "scheduled_sending_exchange229_500k"
	DelayedExchange1M   = "scheduled_sending_exchange229_1m"

	// Size-tiered campaign queues to consume from based on volume
	CampaignQueueName1K   = "campaign_sending_jobs2929_1k"
	CampaignQueueName10K  = "campaign_sending_jobs2929_10k"
	CampaignQueueName50K  = "campaign_sending_jobs2929_50k"
	CampaignQueueName100K = "campaign_sending_jobs2929_100k"
	CampaignQueueName500K = "campaign_sending_jobs2929_500k"
	CampaignQueueName1M   = "campaign_sending_jobs2929_1m"

	// Retry queue infrastructure (separate from original campaign queues)
	RetryQueueName       = "campaign_retry_jobs"
	RetryDLQName         = "campaign_retry_dlq"
	RetryDLX             = "campaign_retry_dlx"
	RetryDelayedExchange = "campaign_retry_delayed_exchange"

	// DataGen webhook queue infrastructure
	DataGenWebhookQueueName = "datagen_webhook_processing"
	DataGenWebhookDLQName   = "datagen_webhook_dlq"
	DataGenWebhookDLX       = "datagen_webhook_dlx"
	DataGenWebhookExchange  = "datagen_webhook_exchange"

	// Campaign execution queue (for triggering scheduled campaign processing on producer server)
	CampaignExecutionQueue = "campaign_execution_triggers"

	// Chunk processing queues (NEW - for 20M+ row campaigns)
	ChunkProcessingQueue = "chunk_processing_queue" // Queue for splitting CSV into chunks
	ChunkExecutionQueue  = "chunk_execution_queue"  // Queue for processing individual chunks
	ChunkProcessingDLQ   = "chunk_processing_dlq"   // Dead letter queue for failed chunk splits
	ChunkExecutionDLQ    = "chunk_execution_dlq"    // Dead letter queue for failed chunk executions
	ChunkProcessingDLX   = "chunk_processing_dlx"   // Dead letter exchange
	ChunkExecutionDLX    = "chunk_execution_dlx"    // Dead letter exchange

	// Webhook forwarding queue infrastructure
	WebhookForwardingQueueName        = "webhook_forwarding_queue"
	WebhookForwardingDLQName          = "webhook_forwarding_dlq"
	WebhookForwardingDLX              = "webhook_forwarding_dlx"
	WebhookForwardingExchange         = "webhook_forwarding_exchange"
	WebhookForwardingDelayedExchange  = "webhook_forwarding_delayed_exchange" // x-delayed-message exchange for retries

	// High priority queue for user-initiated messages
	DataGenUserMessageQueueName = "datagen_user_message_priority"
	DataGenUserMessageDLQName   = "datagen_user_message_dlq"
)

func getConnectionFromPool() (*amqp.Connection, int, error) {
	connPoolMutex.Lock()
	defer connPoolMutex.Unlock()

	// Check if existing connection is healthy and under limit
	if connPool != nil && !connPool.IsClosed() && connPoolRefs < maxConnRefs {
		connPoolRefs++
		log.Printf("[ConnectionPool] Reusing existing connection (refs: %d/%d)", connPoolRefs, maxConnRefs)
		return connPool, connPoolRefs, nil
	}

	// If at max capacity, reject new connections
	if connPoolRefs >= maxConnRefs {
		return nil, 0, fmt.Errorf("connection pool at maximum capacity (%d/%d)", connPoolRefs, maxConnRefs)
	}

	// Create new connection
	amqpURL := os.Getenv("RABBITMQ_URL")
	if amqpURL == "" {
		// URL-encode special characters in password (! becomes %21)
		amqpURL = "amqp://admin:StrongPassword123%21@34.66.150.47:5672"
	}

	// Add connection parameters if not already present in the URL
	if !strings.Contains(amqpURL, "heartbeat=") && !strings.Contains(amqpURL, "connection_timeout=") {
		if strings.Contains(amqpURL, "?") {
			amqpURL += "&heartbeat=30&connection_timeout=30"
		} else {
			amqpURL += "?heartbeat=30&connection_timeout=30"
		}
	}

	log.Printf("[ConnectionPool] Creating new RabbitMQ connection...")
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to dial RabbitMQ: %w", err)
	}

	// Set up connection close notification
	closeChan := make(chan *amqp.Error, 1)
	conn.NotifyClose(closeChan)
	go func() {
		err := <-closeChan
		if err != nil {
			log.Printf("[ConnectionPool] Connection closed: %v", err)
			connPoolMutex.Lock()
			if connPool == conn {
				connPool = nil
				connPoolRefs = 0
			}
			connPoolMutex.Unlock()
		}
	}()

	connPool = conn
	connPoolRefs = 1
	log.Printf("[ConnectionPool] New connection established (refs: %d)", connPoolRefs)
	return conn, connPoolRefs, nil
}

// releaseConnectionFromPool releases a reference to the connection pool
func releaseConnectionFromPool() {
	connPoolMutex.Lock()
	defer connPoolMutex.Unlock()

	if connPoolRefs > 0 {
		connPoolRefs--
		log.Printf("[ConnectionPool] Released connection reference (refs: %d)", connPoolRefs)
	}
}

// GetConnectionPoolRefs returns current connection pool reference count (for monitoring)
func GetConnectionPoolRefs() int {
	connPoolMutex.RLock()
	defer connPoolMutex.RUnlock()
	return connPoolRefs
}

// CreateDedicatedConnection creates a new dedicated RabbitMQ connection (not from pool)
// Use this for critical consumers that need isolation from connection pool issues
func CreateDedicatedConnection() (*amqp.Connection, error) {
	amqpURL := os.Getenv("RABBITMQ_URL")
	if amqpURL == "" {
		amqpURL = "amqp://admin:StrongPassword123%21@34.66.150.47:5672"
	}

	// Add connection parameters
	if !strings.Contains(amqpURL, "heartbeat=") && !strings.Contains(amqpURL, "connection_timeout=") {
		if strings.Contains(amqpURL, "?") {
			amqpURL += "&heartbeat=30&connection_timeout=30"
		} else {
			amqpURL += "?heartbeat=30&connection_timeout=30"
		}
	}

	log.Printf("[WEBHOOK_FORWARDER] Creating dedicated RabbitMQ connection...")
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("failed to dial RabbitMQ: %w", err)
	}

	log.Printf("[WEBHOOK_FORWARDER] Dedicated connection established")
	return conn, nil
}

// ManageWebhookForwarderConnection manages a dedicated connection for the webhook forwarder
// This is isolated from the shared connection pool to prevent cascade failures
func ManageWebhookForwarderConnection(startConsumer func(*amqp.Channel) <-chan struct{}) {
	var conn *amqp.Connection
	var ch *amqp.Channel

	for {
		var err error

		// Create dedicated connection if needed
		if conn == nil || conn.IsClosed() {
			if conn != nil {
				conn.Close() // Clean up old connection
			}
			conn, err = CreateDedicatedConnection()
			if err != nil {
				log.Printf("[WEBHOOK_FORWARDER] Failed to create dedicated connection: %v. Retrying in 5s...", err)
				time.Sleep(5 * time.Second)
				continue
			}
		}

		// Create channel
		ch, err = conn.Channel()
		if err != nil {
			log.Printf("[WEBHOOK_FORWARDER] Failed to create channel: %v. Retrying in 5s...", err)
			conn.Close()
			conn = nil
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("[WEBHOOK_FORWARDER] Channel created (channel=%p)", ch)

		// Declare topology
		if err := declareTopology(ch); err != nil {
			log.Printf("[WEBHOOK_FORWARDER] Failed to declare topology: %v. Retrying in 5s...", err)
			ch.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// Set up connection and channel close notifications
		connCloseChan := make(chan *amqp.Error, 1)
		chCloseChan := make(chan *amqp.Error, 1)
		conn.NotifyClose(connCloseChan)
		ch.NotifyClose(chCloseChan)

		// Start consumer and get done channel
		consumerDone := startConsumer(ch)

		// Wait for any failure signal
		select {
		case <-consumerDone:
			log.Printf("[WEBHOOK_FORWARDER] Consumer stopped (channel=%p)", ch)
		case err := <-connCloseChan:
			log.Printf("[WEBHOOK_FORWARDER] Connection closed (reason: %v)", err)
			conn = nil // Force new connection
		case err := <-chCloseChan:
			log.Printf("[WEBHOOK_FORWARDER] Channel closed (reason: %v)", err)
		}

		// Clean up channel
		if ch != nil {
			ch.Close()
		}

		log.Println("[WEBHOOK_FORWARDER] Restarting in 5s...")
		time.Sleep(5 * time.Second)
	}
}

// CloseConnectionPool closes the connection pool gracefully
func CloseConnectionPool() {
	connPoolMutex.Lock()
	defer connPoolMutex.Unlock()

	if connPool != nil {
		log.Printf("[ConnectionPool] Closing connection pool (refs: %d)", connPoolRefs)
		if err := connPool.Close(); err != nil {
			log.Printf("[ConnectionPool] Error closing connection: %v", err)
		}
		connPool = nil
		connPoolRefs = 0
		log.Println("[ConnectionPool] Connection pool closed")
	}

	// Reset topology flag on shutdown (for clean restarts)
	topologyMutex.Lock()
	topologyDeclared = false
	topologyMutex.Unlock()
}

// InitializeHighThroughputPublishing initializes the high-throughput publishing system

// connect establishes a new connection and channel, and declares topology.
// This is kept for backward compatibility but now uses connection pool
func connect() error {
	conn, _, err := getConnectionFromPool()
	if err != nil {
		return err
	}

	// Update global connection (for backward compatibility)
	RabbitMQConn = conn

	// Create a channel for topology declaration
	RabbitMQChannel, err = conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
	}

	// Declare all exchanges, queues, and bindings
	return declareTopology(RabbitMQChannel)
}

// getChannelFromPool gets a new channel from the connection pool
func getChannelFromPool() (*amqp.Channel, error) {
	conn, _, err := getConnectionFromPool()
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		releaseConnectionFromPool()
		return nil, fmt.Errorf("failed to create channel: %w", err)
	}

	return ch, nil
}

// declareTopology sets up all the necessary RabbitMQ infrastructure.
func declareTopology(ch *amqp.Channel) error {
	// ===================================================================
	// CAMPAIGN-RELATED TOPOLOGY - COMMENTED OUT FOR WEBHOOK BRANCH
	// ===================================================================
	// // Delayed Message Exchange
	// args := amqp.Table{"x-delayed-type": "direct"}
	// if err := ch.ExchangeDeclare(DelayedExchange, "x-delayed-message", true, false, false, false, args); err != nil {
	// 	return err
	// }
	// // Size-tiered delayed exchanges
	// if err := ch.ExchangeDeclare(DelayedExchange1K, "x-delayed-message", true, false, false, false, args); err != nil {
	// 	return err
	// }
	// if err := ch.ExchangeDeclare(DelayedExchange10K, "x-delayed-message", true, false, false, false, args); err != nil {
	// 	return err
	// }
	// if err := ch.ExchangeDeclare(DelayedExchange50K, "x-delayed-message", true, false, false, false, args); err != nil {
	// 	return err
	// }
	// if err := ch.ExchangeDeclare(DelayedExchange100K, "x-delayed-message", true, false, false, false, args); err != nil {
	// 	return err
	// }
	// if err := ch.ExchangeDeclare(DelayedExchange500K, "x-delayed-message", true, false, false, false, args); err != nil {
	// 	return err
	// }
	// if err := ch.ExchangeDeclare(DelayedExchange1M, "x-delayed-message", true, false, false, false, args); err != nil {
	// 	return err
	// }

	// // Dead Letter Exchange
	// if err := ch.ExchangeDeclare(CampaignDLX, "fanout", true, false, false, false, nil); err != nil {
	// 	return err
	// }

	// // Dead Letter Queue (Lazy)
	// dlqArgs := amqp.Table{"x-queue-mode": "lazy"}
	// if _, err := ch.QueueDeclare(CampaignDLQName, true, false, false, false, dlqArgs); err != nil {
	// 	return err
	// }
	// if err := ch.QueueBind(CampaignDLQName, "", CampaignDLX, false, nil); err != nil {
	// 	return err
	// }

	// Size-tiered Queue Args (Lazy + DLX + Priority)
	queueArgs := amqp.Table{
		"x-dead-letter-exchange": CampaignDLX,
		"x-queue-mode":           "lazy",
		"x-max-priority":         10, // Enable priority support (0-10)
	}

	// Declare and bind size-tiered queues (no legacy queue)
	if _, err := ch.QueueDeclare(CampaignQueueName1K, true, false, false, false, queueArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(CampaignQueueName1K, "campaign.send2", DelayedExchange1K, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(CampaignQueueName10K, true, false, false, false, queueArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(CampaignQueueName10K, "campaign.send2", DelayedExchange10K, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(CampaignQueueName50K, true, false, false, false, queueArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(CampaignQueueName50K, "campaign.send2", DelayedExchange50K, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(CampaignQueueName100K, true, false, false, false, queueArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(CampaignQueueName100K, "campaign.send2", DelayedExchange100K, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(CampaignQueueName500K, true, false, false, false, queueArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(CampaignQueueName500K, "campaign.send2", DelayedExchange500K, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(CampaignQueueName1M, true, false, false, false, queueArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(CampaignQueueName1M, "campaign.send2", DelayedExchange1M, false, nil); err != nil {
		return err
	}

	// =====================================================================================
	// RETRY QUEUE INFRASTRUCTURE (Separate from Original Campaign Queues)
	// =====================================================================================

	// // Retry Delayed Exchange (x-delayed-message plugin)
	// retryExchangeArgs := amqp.Table{"x-delayed-type": "direct"}
	// if err := ch.ExchangeDeclare(RetryDelayedExchange, "x-delayed-message", true, false, false, false, retryExchangeArgs); err != nil {
	// 	return err
	// }

	// // Retry Dead Letter Exchange
	// if err := ch.ExchangeDeclare(RetryDLX, "fanout", true, false, false, false, nil); err != nil {
	// 	return err
	// }

	// Retry Dead Letter Queue (Lazy)
	// retryDLQArgs := amqp.Table{"x-queue-mode": "lazy"}
	// if _, err := ch.QueueDeclare(RetryDLQName, true, false, false, false, retryDLQArgs); err != nil {
	// 	return err
	// }
	// if err := ch.QueueBind(RetryDLQName, "", RetryDLX, false, nil); err != nil {
	// 	return err
	// }

	// Retry Main Queue (Lazy + DLX + Priority)
	// retryQueueArgs := amqp.Table{
	// 	"x-dead-letter-exchange": RetryDLX,
	// 	"x-queue-mode":           "lazy",
	// 	"x-max-priority":         10,
	// }
	// if _, err := ch.QueueDeclare(RetryQueueName, true, false, false, false, retryQueueArgs); err != nil {
	// 	return err
	// }
	// if err := ch.QueueBind(RetryQueueName, "campaign.retry", RetryDelayedExchange, false, nil); err != nil {
	// 	return err
	// }

	// log.Println("[RABBITMQ] Retry queue infrastructure declared successfully")

	// DataGen Webhook Infrastructure
	// DataGen Webhook Exchange
	if err := ch.ExchangeDeclare(DataGenWebhookExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}

	// DataGen Webhook Dead Letter Exchange
	if err := ch.ExchangeDeclare(DataGenWebhookDLX, "fanout", true, false, false, false, nil); err != nil {
		return err
	}

	// DataGen Webhook Dead Letter Queue (Lazy)
	datagenDLQArgs := amqp.Table{"x-queue-mode": "lazy"}
	if _, err := ch.QueueDeclare(DataGenWebhookDLQName, true, false, false, false, datagenDLQArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(DataGenWebhookDLQName, "", DataGenWebhookDLX, false, nil); err != nil {
		return err
	}

	// DataGen Webhook Main Queue (Lazy + DLX + Priority Support)
	// Note: RabbitMQ doesn't natively support x-message-deduplication
	// Deduplication is handled at the application level (Redis + Consumer-side)
	// Priority support enabled for chat_send (priority 5) vs campaign_send (priority 0)
	datagenQueueArgs := amqp.Table{
		"x-dead-letter-exchange": DataGenWebhookDLX,
		"x-queue-mode":           "lazy",
		"x-max-priority":         10, // Enable priority support (0-10, 10 is highest)
	}
	if _, err := ch.QueueDeclare(DataGenWebhookQueueName, true, false, false, false, datagenQueueArgs); err != nil {
		return err
	}

	// Bind DataGen webhook queue to the exchange
	if err := ch.QueueBind(DataGenWebhookQueueName, "datagen.webhook", DataGenWebhookExchange, false, nil); err != nil {
		return err
	}

	// High Priority Queue for User-Initiated Messages
	// Priority queue with higher priority (x-max-priority) and faster processing
	userMessageQueueArgs := amqp.Table{
		"x-dead-letter-exchange": DataGenWebhookDLX,
		"x-queue-mode":           "lazy",
		"x-max-priority":         10, // Enable priority support (0-10, 10 is highest)
	}
	if _, err := ch.QueueDeclare(DataGenUserMessageQueueName, true, false, false, false, userMessageQueueArgs); err != nil {
		return err
	}
	// Bind to delayed exchange for scheduled execution
	if err := ch.QueueBind(CampaignExecutionQueue, "campaign.execute", DelayedExchange, false, nil); err != nil {
		return err
	}

	// ===========================================================================
	// CHUNK PROCESSING INFRASTRUCTURE (NEW - for 20M+ row campaigns)
	// ===========================================================================

	// Chunk Processing Dead Letter Exchange
	if err := ch.ExchangeDeclare(ChunkProcessingDLX, "fanout", true, false, false, false, nil); err != nil {
		return err
	}

	// Chunk Execution Dead Letter Exchange
	if err := ch.ExchangeDeclare(ChunkExecutionDLX, "fanout", true, false, false, false, nil); err != nil {
		return err
	}

	// Chunk Processing Dead Letter Queue (for failed CSV splits)
	chunkProcDLQArgs := amqp.Table{"x-queue-mode": "lazy"}
	if _, err := ch.QueueDeclare(ChunkProcessingDLQ, true, false, false, false, chunkProcDLQArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(ChunkProcessingDLQ, "", ChunkProcessingDLX, false, nil); err != nil {
		return err
	}

	// Chunk Execution Dead Letter Queue (for failed chunk processing)
	chunkExecDLQArgs := amqp.Table{"x-queue-mode": "lazy"}
	if _, err := ch.QueueDeclare(ChunkExecutionDLQ, true, false, false, false, chunkExecDLQArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(ChunkExecutionDLQ, "", ChunkExecutionDLX, false, nil); err != nil {
		return err
	}

	// Chunk Processing Queue (for splitting CSVs into chunks)
	// Low prefetch, single consumer recommended
	chunkProcArgs := amqp.Table{
		"x-dead-letter-exchange": ChunkProcessingDLX,
		"x-queue-mode":           "lazy",
		"x-max-priority":         10,
	}
	if _, err := ch.QueueDeclare(ChunkProcessingQueue, true, false, false, false, chunkProcArgs); err != nil {
		return err
	}

	// Chunk Execution Queue (for processing individual chunks)
	// High throughput, multiple consumers can process in parallel
	chunkExecArgs := amqp.Table{
		"x-dead-letter-exchange": ChunkExecutionDLX,
		"x-queue-mode":           "lazy",
		"x-max-priority":         10,
	}
	if _, err := ch.QueueDeclare(ChunkExecutionQueue, true, false, false, false, chunkExecArgs); err != nil {
		return err
	}
	// Bind chunk execution queue to delayed exchange to support x-delay retries
	if err := ch.QueueBind(ChunkExecutionQueue, "chunk.execute", DelayedExchange, false, nil); err != nil {
		return err
	}

	// ===========================================================================
	// WEBHOOK FORWARDING INFRASTRUCTURE
	// ===========================================================================

	// Webhook Forwarding Exchange
	if err := ch.ExchangeDeclare(WebhookForwardingExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}

	// Webhook Forwarding Delayed Exchange (x-delayed-message plugin for reliable retries)
	delayedExchangeArgs := amqp.Table{"x-delayed-type": "direct"}
	if err := ch.ExchangeDeclare(WebhookForwardingDelayedExchange, "x-delayed-message", true, false, false, false, delayedExchangeArgs); err != nil {
		log.Printf("[TOPOLOGY] Warning: Failed to declare delayed exchange (plugin may not be installed): %v", err)
		// Don't return error - fall back to in-memory delay if plugin not available
	}

	// Webhook Forwarding Dead Letter Exchange
	if err := ch.ExchangeDeclare(WebhookForwardingDLX, "fanout", true, false, false, false, nil); err != nil {
		return err
	}

	// Webhook Forwarding Dead Letter Queue (Lazy)
	webhookDLQArgs := amqp.Table{"x-queue-mode": "lazy"}
	if _, err := ch.QueueDeclare(WebhookForwardingDLQName, true, false, false, false, webhookDLQArgs); err != nil {
		return err
	}
	if err := ch.QueueBind(WebhookForwardingDLQName, "", WebhookForwardingDLX, false, nil); err != nil {
		return err
	}

	// Webhook Forwarding Main Queue (Lazy + DLX + Priority Support)
	webhookQueueArgs := amqp.Table{
		"x-dead-letter-exchange": WebhookForwardingDLX,
		"x-queue-mode":           "lazy",
		"x-max-priority":         10,
	}
	if _, err := ch.QueueDeclare(WebhookForwardingQueueName, true, false, false, false, webhookQueueArgs); err != nil {
		return err
	}

	// Bind webhook forwarding queue to the exchange
	if err := ch.QueueBind(WebhookForwardingQueueName, "webhook.forward", WebhookForwardingExchange, false, nil); err != nil {
		return err
	}

	// Bind webhook forwarding queue to delayed exchange for retry support
	if err := ch.QueueBind(WebhookForwardingQueueName, "webhook.forward", WebhookForwardingDelayedExchange, false, nil); err != nil {
		log.Printf("[TOPOLOGY] Warning: Failed to bind queue to delayed exchange: %v", err)
		// Don't return error - delayed exchange binding is optional
	}

	// Note: Success logging moved to ManageConnection to prevent duplicates
	return nil
}

// ManageConnection is a long-running function that handles the connection lifecycle.
// It takes a function `startConsumers` which it will call upon a successful connection.
// Uses connection pool to share connections across consumers.
func ManageConnection(startConsumers func(*amqp.Channel)) {
	for {
		// Get connection from pool (returns refs at time of getting connection)
		conn, refs, err := getConnectionFromPool()
		if err != nil {
			log.Printf("[ConnectionPool] Failed to get connection. Retrying in 5 seconds... Error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Create a dedicated channel for this consumer
		ch, err := conn.Channel()
		if err != nil {
			log.Printf("[ConnectionPool] Failed to create channel. Retrying in 5 seconds... Error: %v", err)
			releaseConnectionFromPool()
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("[ConnectionPool] ✅ Connection established (pool refs: %d)", refs)

		// Declare topology (queues, exchanges, bindings) before starting consumers
		// This ensures all infrastructure exists before consumers try to use it
		// Only declare once globally to prevent duplicates
		topologyMutex.Lock()
		if !topologyDeclared {
			if err := declareTopology(ch); err != nil {
				topologyMutex.Unlock()
				log.Printf("[ConnectionPool] Failed to declare topology: %v. Retrying in 5 seconds...", err)
				ch.Close()
				releaseConnectionFromPool()
				time.Sleep(5 * time.Second)
				continue
			}
			topologyDeclared = true
			log.Println("✅ RabbitMQ topology declared successfully (first declaration)")
		} else {
			log.Println("ℹ️  RabbitMQ topology already declared, skipping duplicate declaration")
		}
		topologyMutex.Unlock()

		// --- Start consumers on the new, healthy channel ---
		startConsumers(ch)

		// --- Listen for connection or channel closure ---
		connCloseChan := make(chan *amqp.Error, 1)
		chCloseChan := make(chan *amqp.Error, 1)
		conn.NotifyClose(connCloseChan)
		ch.NotifyClose(chCloseChan)

		// Start a health check ticker
		healthTicker := time.NewTicker(30 * time.Second)

		// Block until a disconnection is detected or health check fails
		select {
		case err := <-connCloseChan:
			healthTicker.Stop()
			ch.Close()
			// Connection closed - release reference and invalidate pool
			connPoolMutex.Lock()
			if connPool == conn {
				connPool = nil
				connPoolRefs = 0
			}
			connPoolMutex.Unlock()
			log.Printf("[ConnectionPool] 🔴 Connection closed. Reason: %v. Starting reconnection loop.", err)
		case err := <-chCloseChan:
			healthTicker.Stop()
			ch.Close()
			// Channel closed but connection may still be valid - just release our reference
			releaseConnectionFromPool()
			log.Printf("[ConnectionPool] 🔴 Channel closed. Reason: %v. Starting reconnection loop.", err)
		case <-healthTicker.C:
			// Check if connection is still healthy
			connPoolMutex.RLock()
			isHealthy := connPool != nil && !connPool.IsClosed() && conn == connPool
			connPoolMutex.RUnlock()

			if !isHealthy {
				healthTicker.Stop()
				ch.Close()
				// Connection unhealthy - release reference
				releaseConnectionFromPool()
				log.Println("[ConnectionPool] 🔴 Health check detected unhealthy connection. Starting reconnection loop.")
				break
			}
			// Reset ticker and continue monitoring
			healthTicker.Reset(30 * time.Second)
		}
		// The loop will now restart, attempting to reconnect.
	}
}

func StartCampaignJobConsumer(handler func([]byte, string) error) error {
	if RabbitMQChannel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}
	// Set prefetch for throughput (tune as needed)
	if err := RabbitMQChannel.Qos(500, 0, false); err != nil {
		return fmt.Errorf("failed to set prefetch: %w", err)
	}
	// Consume from legacy and all size-tiered queues
	consumeQueues := []string{
		CampaignQueueName,
		CampaignQueueName1K,
		CampaignQueueName10K,
		CampaignQueueName50K,
		CampaignQueueName100K,
		CampaignQueueName500K,
		CampaignQueueName1M,
	}

	dedupSet := make(map[string]time.Time)
	var mu sync.Mutex // Mutex to protect the deduplication map

	// Start a goroutine to clean up old message IDs
	go func() {
		cleanupTicker := time.NewTicker(1 * time.Hour)
		defer cleanupTicker.Stop()

		for range cleanupTicker.C {
			mu.Lock()
			cutoff := time.Now().Add(-24 * time.Hour) // Remove IDs older than 24 hours
			removeCount := 0

			for id, timestamp := range dedupSet {
				if timestamp.Before(cutoff) {
					delete(dedupSet, id)
					removeCount++
				}
			}
			log.Printf("Cleaned up %d old message IDs, %d remaining", removeCount, len(dedupSet))
			mu.Unlock()
		}
	}()

	for _, q := range consumeQueues {
		msgs, err := RabbitMQChannel.Consume(
			q,
			"",    // consumer tag
			false, // auto-ack
			false, // exclusive
			false, // no-local
			false, // no-wait
			nil,   // args
		)
		if err != nil {
			return fmt.Errorf("failed to start consumer for queue %s: %w", q, err)
		}

		go func(msgs <-chan amqp.Delivery, queueName string) {
			for d := range msgs {
				// Get msgID from MessageId property first
				msgID := d.MessageId
				if msgID == "" {
					// Fallback to header "message-id"
					if v, ok := d.Headers["message-id"]; ok {
						switch t := v.(type) {
						case string:
							msgID = t
						case []byte:
							msgID = string(t)
						default:
							msgID = fmt.Sprintf("%v", v)
						}
					}
				}

				if msgID == "" {
					// fallback: use delivery tag
					msgID = fmt.Sprintf("delivery-%d", d.DeliveryTag)
				}
				mu.Lock()
				if _, exists := dedupSet[msgID]; exists {
					mu.Unlock()
					d.Ack(false)
					continue // skip duplicate
				}
				dedupSet[msgID] = time.Now() // Store timestamp for cleanup
				mu.Unlock()

				// Call handler
				err := handler(d.Body, msgID)
				if err != nil {
					d.Nack(false, false) // send to DLQ
					continue
				}
				d.Ack(false)
			}
		}(msgs, q)
	}
	return nil
}

// IsConnectionHealthy checks if the RabbitMQ connection pool is healthy
func IsConnectionHealthy() bool {
	connPoolMutex.RLock()
	defer connPoolMutex.RUnlock()

	if connPool == nil {
		return false
	}

	// Check if connection is closed
	if connPool.IsClosed() {
		return false
	}

	// Try a lightweight operation to verify connection is working
	ch, err := connPool.Channel()
	if err != nil {
		return false
	}
	defer ch.Close()

	_, err = ch.QueueInspect(CampaignQueueName)
	return err == nil
}

// EnsureConnection attempts to get a healthy connection from the pool
func EnsureConnection() error {
	connPoolMutex.RLock()
	if connPool != nil && !connPool.IsClosed() {
		connPoolMutex.RUnlock()
		// Update global connection for backward compatibility
		RabbitMQConn = connPool

		// Also ensure channel is available
		if RabbitMQChannel == nil {
			var err error
			RabbitMQChannel, err = connPool.Channel()
			if err != nil {
				connPoolMutex.RUnlock()
				return fmt.Errorf("failed to create channel: %w", err)
			}
		}
		return nil
	}
	connPoolMutex.RUnlock()

	log.Println("[ConnectionPool] Connection unhealthy, getting new connection from pool...")
	conn, _, err := getConnectionFromPool()
	if err != nil {
		return err
	}

	// Update global connection for backward compatibility
	RabbitMQConn = conn

	// Create channel if it doesn't exist
	if RabbitMQChannel == nil {
		RabbitMQChannel, err = conn.Channel()
		if err != nil {
			return fmt.Errorf("failed to create channel: %w", err)
		}
	}

	return nil
}

func BatchEnqueueCampaignJobs(jobs []interface{}, scheduleTimes []int64, routingKey string) error {
	// First ensure we have a healthy connection
	if err := EnsureConnection(); err != nil {
		return fmt.Errorf("failed to ensure RabbitMQ connection: %w", err)
	}

	if RabbitMQChannel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}
	if len(jobs) != len(scheduleTimes) {
		return fmt.Errorf("jobs and scheduleTimes length mismatch")
	}

	// Create a new channel for this batch operation to avoid shared channel issues
	// This helps isolate publisher confirms and prevents issues with concurrent operations
	batchChannel, err := RabbitMQConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create dedicated channel for batch operation: %w", err)
	}
	defer batchChannel.Close()

	// Enable publisher confirms on the dedicated channel
	if err := batchChannel.Confirm(false); err != nil {
		return fmt.Errorf("failed to enable publisher confirms: %w", err)
	}
	confirmCh := batchChannel.NotifyPublish(make(chan amqp.Confirmation, len(jobs)))

	dedup := make(map[string]struct{}, len(jobs))
	var mu sync.Mutex
	publishedCount := 0

	// Select delayed exchange based on total jobs
	exchange := GetDelayedExchangeForJobs(len(jobs))
	// Calculate priority once per batch (smaller batches = higher priority)
	priority := GetPriorityForBatchSize(len(jobs))

	for i, job := range jobs {
		// Marshal job to JSON
		body, err := json.Marshal(job)
		if err != nil {
			log.Printf("[PRODUCER_ERROR] Failed to marshal job %d: %v", i, err)
			return fmt.Errorf("failed to marshal job: %w", err)
		}
		// Extract unique message ID (must be present in job struct as ParentCampaignID and PhoneNumber)
		var msgID string
		switch v := job.(type) {
		case map[string]interface{}:
			msgID = fmt.Sprintf("%v:%v", v["ParentCampaignID"], v["PhoneNumber"])
		case models.CampaignSendJob:
			msgID = fmt.Sprintf("%s:%s", v.ParentCampaignID, v.PhoneNumber)
		case *models.CampaignSendJob:
			msgID = fmt.Sprintf("%s:%s", v.ParentCampaignID, v.PhoneNumber)
		default:
			msgID = fmt.Sprintf("job-%d", i)
		}

		// Check for duplicates in both local batch and global deduplication map
		mu.Lock() // Local mutex for this batch's dedup map
		if _, exists := dedup[msgID]; exists {
			mu.Unlock()
			log.Printf("[PRODUCER_DEBUG] Skipping duplicate job in current batch: %s", msgID)
			continue // skip duplicate in this batch
		}
		dedup[msgID] = struct{}{}
		mu.Unlock()

		// Check global deduplication map
		globalDedupMutex.Lock()
		if timestamp, exists := globalDedupMap[msgID]; exists {
			// Only consider it a duplicate if it was processed recently (within last hour)
			if time.Since(timestamp) < time.Hour {
				globalDedupMutex.Unlock()
				log.Printf("[PRODUCER_DEBUG] Skipping recently processed job: %s (processed %v ago)",
					msgID, time.Since(timestamp))
				continue
			}
			// Otherwise, update the timestamp and proceed
		}
		// Add or update in global map
		globalDedupMap[msgID] = time.Now()
		globalDedupMutex.Unlock()

		// Calculate delay (in ms) from now to scheduleTime
		delayMs := scheduleTimes[i] - time.Now().UnixMilli()
		if delayMs < 0 {
			delayMs = 0
		}

		log.Printf("[PRODUCER_DEBUG] Preparing to publish job %d: msgID=%s delayMs=%d", i, msgID, delayMs)
		log.Printf("[PRODUCER_DEBUG] Marshaled job JSON: %s", string(body))

		// Publish with x-delay header, MessageId property, and priority
		err = batchChannel.Publish(
			exchange,
			routingKey,
			false,
			false,
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				ContentType:  "application/json",
				Body:         body,
				MessageId:    msgID,    // Set MessageId property
				Priority:     priority, // Set priority (higher = processed first)
				Headers: amqp.Table{
					"x-delay": delayMs,
				},
			},
		)
		if err != nil {
			log.Printf("[PRODUCER_ERROR] Failed to publish job %d (msgID=%s): %v", i, msgID, err)
			return fmt.Errorf("failed to publish job: %w", err)
		}
		publishedCount++
		log.Printf("[PRODUCER_DEBUG] Published job %d (msgID=%s) to exchange '%s' with routing key '%s', delay %dms, priority %d", i, msgID, exchange, routingKey, delayMs, priority)
	}

	// Wait for all confirms with timeout
	timeoutDuration := 10 * time.Second
	timeoutTimer := time.NewTimer(timeoutDuration)
	defer timeoutTimer.Stop()

	confirmationCount := 0
	expectedConfirmations := publishedCount
	publishedMessages := make(map[uint64]bool)

	// Wait for all confirmations or timeout
	for confirmationCount < expectedConfirmations {
		select {
		case confirm := <-confirmCh:
			publishedMessages[confirm.DeliveryTag] = confirm.Ack
			confirmationCount++
			log.Printf("[PRODUCER_DEBUG] Publish confirmed for message with tag %d: ack=%v", confirm.DeliveryTag, confirm.Ack)

		case <-timeoutTimer.C:
			// Timeout occurred before receiving all confirmations
			return fmt.Errorf("timeout waiting for publisher confirmations after %v", timeoutDuration)
		}
	}

	// Check for unconfirmed messages
	unconfirmedCount := 0
	for tag, confirmed := range publishedMessages {
		if !confirmed {
			log.Printf("[PRODUCER_ERROR] Message with delivery tag %d was not confirmed", tag)
			unconfirmedCount++
		}
	}

	if unconfirmedCount > 0 {
		return fmt.Errorf("%d messages were not confirmed by RabbitMQ", unconfirmedCount)
	}

	log.Printf("[PRODUCER_DEBUG] Successfully published %d jobs.", publishedCount)
	return nil
}

// GetDelayedExchangeForJobs maps total job count to the appropriate delayed exchange.
func GetDelayedExchangeForJobs(total int) string {
	if total <= 1000 {
		return DelayedExchange1K
	}
	if total <= 10000 {
		return DelayedExchange10K
	}
	if total <= 50000 {
		return DelayedExchange50K
	}
	if total <= 100000 {
		return DelayedExchange100K
	}
	if total <= 500000 {
		return DelayedExchange500K
	}
	return DelayedExchange1M
}

// GetPriorityForBatchSize returns message priority based on batch size.
// Smaller batches get higher priority (10 = highest, 0 = lowest) to prevent them from getting stuck.
func GetPriorityForBatchSize(batchSize int) uint8 {
	if batchSize <= 1000 {
		return 6 // Highest priority for smallest batches
	}
	if batchSize <= 10000 {
		return 5
	}
	if batchSize <= 50000 {
		return 4
	}
	if batchSize <= 100000 {
		return 3
	}
	if batchSize <= 500000 {
		return 2
	}
	return 1 // Lowest priority for largest batches
}

// DataGenWebhookPayload represents the structure for DataGen webhook messages
type DataGenWebhookPayload struct {
	WebhookType string                 `json:"webhook_type"` // "delivery", "user_message", "batch"
	Payload     map[string]interface{} `json:"payload"`
	Timestamp   int64                  `json:"timestamp"`
	MessageID   string                 `json:"message_id"`
}

// generateWebhookDeduplicationID creates a unique identifier for webhook deduplication
// This ID is based on webhook content, not timestamp, to prevent duplicate processing
func generateWebhookDeduplicationID(webhookType string, payload map[string]interface{}) string {
	// Create a deterministic hash based on webhook content
	// This ensures the same webhook content gets the same ID regardless of when it's received

	var contentHash string

	switch webhookType {
	case "delivery":
		// For delivery webhooks, use key fields that identify the specific delivery event
		// CRITICAL: Include events.mid + status to ensure each status update is unique
		// This prevents deduplication from blocking legitimate status updates (sent -> delivered -> read)

		status := getStringValue(payload, "notificationAttributes", "status")
		to := getStringValue(payload, "recipient", "to")
		messageID := getStringValue(payload, "events", "mid") // DataGen message ID
		campaignID := getStringValue(payload, "recipient", "reference", "messageTag1")

		// Use messageID + status + timestamp for unique identification
		// This ensures each status update (sent, delivered, read) gets a unique dedup ID
		if status != "" && to != "" && messageID != "" {
			contentHash = fmt.Sprintf("delivery_%s_%s_%s", status, to, messageID)
		} else if status != "" && to != "" && campaignID != "" {
			contentHash = fmt.Sprintf("delivery_%s_%s_%s", status, to, campaignID)
		}
	case "user_message":
		// For user message webhooks, use the unique message ID from DataGen
		// This is the most reliable identifier (wamid.*) from eventContent.message.id
		messageID := getStringValue(payload, "eventContent", "message", "id")
		from := getStringValue(payload, "eventContent", "message", "from")
		to := getStringValue(payload, "eventContent", "message", "to")
		timestamp := getStringValue(payload, "events", "timestamp")

		// Primary: Use message ID if available (most reliable - unique per message from DataGen)
		if messageID != "" {
			contentHash = fmt.Sprintf("user_msg_%s", messageID)
		} else if from != "" && to != "" && timestamp != "" {
			// Fallback: Use from + to + timestamp if message ID is missing
			contentHash = fmt.Sprintf("user_msg_%s_%s_%s", from, to, timestamp)
		}
	case "batch":
		// For batch webhooks, hash the entire batch content
		if batchData, ok := payload["batch_data"].([]interface{}); ok {
			// Create hash from batch content
			batchContent := fmt.Sprintf("%v", batchData)
			hash := sha256.Sum256([]byte(batchContent))
			contentHash = fmt.Sprintf("batch_%x", hash[:8])
		}
	default:
		// For unknown types, hash the entire payload
		payloadBytes, _ := json.Marshal(payload)
		hash := sha256.Sum256(payloadBytes)
		contentHash = fmt.Sprintf("unknown_%x", hash[:8])
	}

	// If we couldn't create a content-based hash, fall back to timestamp-based
	if contentHash == "" {
		contentHash = fmt.Sprintf("fallback_%s_%d", webhookType, time.Now().UnixNano())
	}

	return contentHash
}

// PublishDataGenWebhook publishes a DataGen webhook to the processing queue with deduplication and publisher confirms
func PublishDataGenWebhook(webhookType string, payload map[string]interface{}) error {
	return PublishDataGenWebhookWithRetry(webhookType, payload, 3) // 3 retries
}

// PublishDataGenWebhookHighThroughput publishes directly to RabbitMQ (simple approach)
func PublishDataGenWebhookHighThroughput(webhookType string, payload map[string]interface{}) error {
	// Generate deduplication ID based on webhook content
	deduplicationID := generateWebhookDeduplicationID(webhookType, payload)

	// Create webhook message (original format)
	webhookMsg := DataGenWebhookPayload{
		WebhookType: webhookType,
		Payload:     payload,
		Timestamp:   time.Now().UnixMilli(),
		MessageID:   deduplicationID, // Use deduplication ID as message ID
	}

	// Marshal to JSON
	body, err := json.Marshal(webhookMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Determine routing key and priority based on webhook type
	routingKey := "datagen.webhook"
	priority := uint8(0) // Default priority

	if webhookType == "user_message" {
		routingKey = "datagen.user.message"
		priority = 8 // Highest priority for user messages
	} else {
		// Check messageTag3 to determine priority for delivery webhooks
		messageTag3 := getStringValue(payload, "recipient", "reference", "messageTag3")
		if messageTag3 != "" {
			// Parse messageTag3 JSON to extract type
			if strings.Contains(messageTag3, "chat_send") {
				priority = 9 // Higher priority for chat messages
			} else {
				priority = 10 // Lower priority for campaign messages
			}
		}
	}

	// Retry logic for connection issues
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Ensure we have a healthy connection
		if err := EnsureConnection(); err != nil {
			if attempt == maxRetries-1 {
				return fmt.Errorf("failed to ensure RabbitMQ connection after %d attempts: %w", maxRetries, err)
			}
			log.Printf("[CONNECTION_RETRY] Failed to ensure connection (attempt %d/%d): %v", attempt+1, maxRetries, err)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond) // Exponential backoff
			continue
		}

		if RabbitMQChannel == nil {
			if attempt == maxRetries-1 {
				return fmt.Errorf("RabbitMQ channel is not initialized after %d attempts", maxRetries)
			}
			log.Printf("[CONNECTION_RETRY] Channel not initialized (attempt %d/%d)", attempt+1, maxRetries)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}

		// Publish directly to RabbitMQ
		err = RabbitMQChannel.Publish(
			DataGenWebhookExchange,
			routingKey,
			false, // mandatory
			false, // immediate
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				ContentType:  "application/json",
				Body:         body,
				Priority:     priority,
				MessageId:    deduplicationID,
				Timestamp:    time.Now(),
			},
		)

		if err != nil {
			// Check if this is a connection/channel error that should be retried
			errStr := err.Error()
			isConnectionError := strings.Contains(errStr, "channel/connection is not open") ||
				strings.Contains(errStr, "connection refused") ||
				strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "broken pipe") ||
				strings.Contains(errStr, "connection closed") ||
				strings.Contains(errStr, "Exception (504)") // RabbitMQ connection errors

			if isConnectionError && attempt < maxRetries-1 {
				log.Printf("[CONNECTION_RETRY] Connection error (attempt %d/%d): %v, forcing reconnection...", attempt+1, maxRetries, err)
				// Force reconnection by clearing the global channel
				RabbitMQChannel = nil
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			} else {
				log.Printf("[PUBLISH_ERROR] Failed to publish %s webhook: %v", webhookType, err)
				return fmt.Errorf("failed to publish webhook: %w", err)
			}
		}

		// Success!
		log.Printf("[WEBHOOK_PUBLISHED] ✅ Webhook %s published successfully: %s", webhookType, deduplicationID)
		return nil
	}

	return fmt.Errorf("failed to publish webhook after %d connection retry attempts", maxRetries)
}

// PublishDataGenWebhookWithRetry publishes with retry mechanism and publisher confirms
func PublishDataGenWebhookWithRetry(webhookType string, payload map[string]interface{}, maxRetries int) error {
	// Generate deduplication ID based on webhook content
	deduplicationID := generateWebhookDeduplicationID(webhookType, payload)

	// Debug logging for delivery webhooks
	if webhookType == "delivery" {
		status := getStringValue(payload, "notificationAttributes", "status")
		phone := getStringValue(payload, "recipient", "to")
		msgID := getStringValue(payload, "events", "mid")
		log.Printf("[StatusDebug] Publishing delivery webhook: phone=%s, status=%s, msgID=%s, dedupID=%s", phone, status, msgID, deduplicationID)
	}

	// Create webhook message
	webhookMsg := DataGenWebhookPayload{
		WebhookType: webhookType,
		Payload:     payload,
		Timestamp:   time.Now().UnixMilli(),
		MessageID:   deduplicationID, // Use deduplication ID as message ID
	}

	// Marshal to JSON
	body, err := json.Marshal(webhookMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Determine routing key and priority based on webhook type
	routingKey := "datagen.webhook"
	priority := uint8(0) // Default priority

	if webhookType == "user_message" {
		routingKey = "datagen.user.message"
		priority = 8 // Highest priority for user messages
	} else {
		// Check messageTag3 to determine priority for delivery webhooks
		messageTag3 := getStringValue(payload, "recipient", "reference", "messageTag3")
		if messageTag3 != "" {
			// Parse messageTag3 JSON to extract type
			if strings.Contains(messageTag3, "chat_send") {
				priority = 9 // Higher priority for chat messages
			} else {
				priority = 10 // Lower priority for campaign messages
			}
		}
	}

	// Retry loop
	var lastErr error
	var conn *amqp.Connection
	var publishChannel *amqp.Channel
	var confirmCh chan amqp.Confirmation

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Get connection from pool and create channel for each attempt
		if conn == nil || conn.IsClosed() {
			// Release old connection if exists
			if conn != nil {
				releaseConnectionFromPool()
			}

			conn, _, err = getConnectionFromPool()
			if err != nil {
				lastErr = fmt.Errorf("failed to get connection from pool: %w", err)
				if attempt < maxRetries {
					backoff := time.Duration(100*(1<<uint(attempt))) * time.Millisecond
					log.Printf("[PublishRetry] Retrying connection (attempt %d/%d) after %v: %v", attempt+1, maxRetries, backoff, lastErr)
					time.Sleep(backoff)
				}
				continue
			}
		}

		// Create or recreate channel
		if publishChannel != nil {
			publishChannel.Close()
		}

		publishChannel, err = conn.Channel()
		if err != nil {
			lastErr = fmt.Errorf("failed to create publish channel: %w", err)
			if attempt < maxRetries {
				backoff := time.Duration(100*(1<<uint(attempt))) * time.Millisecond
				log.Printf("[PublishRetry] Retrying channel creation (attempt %d/%d) after %v: %v", attempt+1, maxRetries, backoff, lastErr)
				time.Sleep(backoff)
			}
			continue
		}

		// Enable publisher confirms
		if err := publishChannel.Confirm(false); err != nil {
			publishChannel.Close()
			lastErr = fmt.Errorf("failed to enable publisher confirms: %w", err)
			if attempt < maxRetries {
				backoff := time.Duration(100*(1<<uint(attempt))) * time.Millisecond
				log.Printf("[PublishRetry] Retrying confirm setup (attempt %d/%d) after %v: %v", attempt+1, maxRetries, backoff, lastErr)
				time.Sleep(backoff)
			}
			continue
		}

		confirmCh = publishChannel.NotifyPublish(make(chan amqp.Confirmation, 1))

		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms
			backoff := time.Duration(100*(1<<uint(attempt-1))) * time.Millisecond
			log.Printf("[PublishRetry] Retrying publish (attempt %d/%d) after %v: %v", attempt, maxRetries, backoff, lastErr)
			time.Sleep(backoff)
		}

		// Publish to queue with priority
		err = publishChannel.Publish(
			DataGenWebhookExchange,
			routingKey,
			false, // not mandatory
			false, // not immediate
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				ContentType:  "application/json",
				Body:         body,
				MessageId:    deduplicationID,
				Priority:     priority, // Set priority for user messages
				Headers: amqp.Table{
					"message-id":    deduplicationID,
					"webhook-type":  webhookType,
					"deduplication": "true",
					"content-hash":  deduplicationID,
				},
			},
		)
		if err != nil {
			lastErr = fmt.Errorf("failed to publish: %w", err)
			continue
		}

		// Wait for confirmation with timeout
		select {
		case confirm := <-confirmCh:
			if confirm.Ack {
				if publishChannel != nil {
					publishChannel.Close()
				}
				releaseConnectionFromPool()
				log.Printf("Published DataGen webhook: type=%s, dedupID=%s, priority=%d", webhookType, deduplicationID, priority)
				return nil
			}
			lastErr = fmt.Errorf("publish not acknowledged by broker")
		case <-time.After(5 * time.Second):
			lastErr = fmt.Errorf("timeout waiting for publish confirmation")
		}
	}

	// Cleanup on failure
	if publishChannel != nil {
		publishChannel.Close()
	}
	if conn != nil {
		releaseConnectionFromPool()
	}

	// All retries exhausted - message will go to DLQ via RabbitMQ's retry mechanism
	return fmt.Errorf("failed to publish after %d attempts: %w", maxRetries+1, lastErr)
}

// WebhookBatchItem represents a webhook message with its delivery information
type WebhookBatchItem struct {
	Webhook   DataGenWebhookPayload
	Delivery  amqp.Delivery
	MessageID string // Store message ID for batch deduplication
}

// performBatchDeduplication performs efficient batch deduplication using Redis pipeline
// Returns unique items and duplicate items separately
func performBatchDeduplication(items []WebhookBatchItem) ([]WebhookBatchItem, []WebhookBatchItem) {
	if len(items) == 0 {
		return items, []WebhookBatchItem{}
	}

	// Extract message IDs for batch Redis check
	messageIDs := make([]string, 0, len(items))
	messageIDToItems := make(map[string][]WebhookBatchItem)

	for _, item := range items {
		if item.MessageID != "" {
			messageIDs = append(messageIDs, item.MessageID)
			messageIDToItems[item.MessageID] = append(messageIDToItems[item.MessageID], item)
		}
	}

	// If no message IDs, return all items as unique
	if len(messageIDs) == 0 {
		return items, []WebhookBatchItem{}
	}

	// OPTIMIZATION: Single Redis pipeline call for all message IDs
	redisResults, err := BatchTryDedupeMessages(context.Background(), messageIDs, 24*time.Hour)
	if err != nil {
		log.Printf("[BatchDedup] Redis batch deduplication failed: %v. Treating all as unique.", err)
		return items, []WebhookBatchItem{}
	}

	// Separate unique and duplicate items
	var uniqueItems []WebhookBatchItem
	var duplicateItems []WebhookBatchItem

	for messageID, isUnique := range redisResults {
		if items, exists := messageIDToItems[messageID]; exists {
			if isUnique {
				// First occurrence - add all items with this message ID as unique
				uniqueItems = append(uniqueItems, items...)
			} else {
				// Duplicate - add all items with this message ID as duplicates
				duplicateItems = append(duplicateItems, items...)
			}
		}
	}

	// Handle items without message IDs (treat as unique)
	for _, item := range items {
		if item.MessageID == "" {
			uniqueItems = append(uniqueItems, item)
		}
	}

	return uniqueItems, duplicateItems
}

// StartDataGenWebhookConsumer starts a consumer for DataGen webhook processing with adaptive batch processing
// ch: The RabbitMQ channel to use (passed from ManageConnection)
func StartDataGenWebhookConsumer(ch *amqp.Channel, handler func([]DataGenWebhookPayload) error) error {
	if ch == nil {
		return fmt.Errorf("RabbitMQ channel is nil")
	}

	// OPTIMIZATION: Set prefetch for optimal throughput (reduced from 1000 to 100 for better responsiveness)
	if err := ch.Qos(1000, 0, false); err != nil {
		return fmt.Errorf("failed to set prefetch: %w", err)
	}

	msgs, err := ch.Consume(
		DataGenWebhookQueueName,
		"",    // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("failed to start DataGen webhook consumer: %w", err)
	}

	// Redis-based deduplication (primary) + Consumer-side deduplication (backup)
	consumerDedupMap := make(map[string]time.Time)
	var consumerDedupMutex sync.RWMutex

	// Start consumer-side deduplication cleanup
	go func() {
		cleanupTicker := time.NewTicker(1 * time.Hour)
		defer cleanupTicker.Stop()

		for range cleanupTicker.C {
			consumerDedupMutex.Lock()
			cutoff := time.Now().Add(-24 * time.Hour) // Remove IDs older than 24 hours
			removeCount := 0

			for id, timestamp := range consumerDedupMap {
				if timestamp.Before(cutoff) {
					delete(consumerDedupMap, id)
					removeCount++
				}
			}
			log.Printf("[ConsumerDedup] Cleaned up %d old message IDs, %d remaining in consumer dedup map", removeCount, len(consumerDedupMap))
			consumerDedupMutex.Unlock()
		}
	}()

	// OPTIMIZATION: Adaptive batch processing variables with mutex for thread safety
	var batchSize int = 100                          // Process 100 webhooks per batch (minimum batch size)
	var batchTimeout time.Duration = 2 * time.Second // 2-second timeout for faster processing
	var batchSizeMutex sync.RWMutex
	batch := make([]WebhookBatchItem, 0, 1000) // Pre-allocate with max capacity for larger batches
	var batchMutex sync.Mutex
	timeoutTimer := time.NewTimer(batchTimeout)
	timeoutTimer.Stop()

	// OPTIMIZATION: Dynamic batch sizing based on queue inspection
	go func() {
		queueInspectTicker := time.NewTicker(5 * time.Second)
		defer queueInspectTicker.Stop()

		for range queueInspectTicker.C {
			if qi, err := ch.QueueInspect(DataGenWebhookQueueName); err == nil {
				queueLength := qi.Messages

				batchSizeMutex.Lock()
				// Adjust batch size based on queue length
				if queueLength > 10000 {
					batchSize = 1000 // Very Large batches for high volume
					batchTimeout = 20 * time.Second
				} else if queueLength > 5000 {
					batchSize = 500 // Large batches for high volume
					batchTimeout = 15 * time.Second
				} else if queueLength > 1000 {
					batchSize = 200 // Medium-large batches
					batchTimeout = 15 * time.Second
				} else {
					batchSize = 100                 // Minimum batch size for low volume
					batchTimeout = 10 * time.Second // Aggressive timeout
				}
				batchSizeMutex.Unlock()
			}
		}
	}()

	// Process messages in high-throughput batches with OPTIMIZED batch deduplication
	go func() {
		for d := range msgs {
			// Extract message ID for deduplication
			messageID := d.MessageId
			if messageID == "" {
				// Extract message ID from headers if not in MessageId field
				if msgID, ok := d.Headers["message-id"].(string); ok {
					messageID = msgID
				}
			}

			// Parse webhook message first
			var webhookMsg DataGenWebhookPayload
			if err := json.Unmarshal(d.Body, &webhookMsg); err != nil {
				log.Printf("Failed to unmarshal DataGen webhook: %v", err)
				d.Nack(false, false) // Send to DLQ
				continue
			}

			// Add to batch immediately (deduplication happens at batch processing time)
			batchMutex.Lock()
			batch = append(batch, WebhookBatchItem{
				Webhook:   webhookMsg,
				Delivery:  d,
				MessageID: messageID, // Store message ID for batch deduplication
			})

			// Get current batch size and timeout (thread-safe)
			batchSizeMutex.RLock()
			currentBatchSize := batchSize
			currentBatchTimeout := batchTimeout
			batchSizeMutex.RUnlock()

			shouldProcess := len(batch) >= currentBatchSize

			// OPTIMIZATION: For smaller queues, process immediately when we have enough messages
			// This prevents waiting for timeout when queue is small
			if currentBatchSize <= 200 && len(batch) >= 50 {
				// For smaller batches (≤200), process when we have 50+ messages
				shouldProcess = true
			} else if currentBatchSize <= 500 && len(batch) >= 100 {
				// For medium batches (≤500), process when we have 100+ messages
				shouldProcess = true
			}

			if len(batch) == 1 {
				// Start timeout timer for first message in batch
				timeoutTimer.Reset(currentBatchTimeout)
			}

			if shouldProcess {
				// Process current batch
				processBatch := make([]WebhookBatchItem, len(batch))
				copy(processBatch, batch)

				// Reset batch
				batch = batch[:0]
				timeoutTimer.Stop()
				batchMutex.Unlock()

				// Process batch synchronously - ack only after successful processing
				startTime := time.Now()

				// OPTIMIZATION: Batch deduplication - single Redis pipeline call instead of N individual calls
				uniqueItems, _ := performBatchDeduplication(processBatch)

				// Extract webhooks for processing (only unique messages)
				webhooks := make([]DataGenWebhookPayload, len(uniqueItems))
				for i, item := range uniqueItems {
					webhooks[i] = item.Webhook
				}

				if err := handler(webhooks); err != nil {
					log.Printf("Failed to process DataGen webhook batch: %v", err)
					// Nack all messages in the batch (both unique and duplicates)
					for _, item := range processBatch {
						item.Delivery.Nack(false, false)
					}
				} else {
					// Ack all messages in the batch (both unique and duplicates) only after successful processing
					for _, item := range processBatch {
						item.Delivery.Ack(false)
					}
					processingTime := time.Since(startTime)
					throughput := float64(len(webhooks)) / processingTime.Seconds()
					log.Printf("Successfully processed DataGen webhook batch: %d unique messages in %v (%.2f msg/sec)",
						len(webhooks), processingTime, throughput)
				}
			} else {
				batchMutex.Unlock()
				// Don't ack yet, wait for batch completion
			}
		}
	}()

	// Handle timeout for batch processing
	go func() {
		for range timeoutTimer.C {
			batchMutex.Lock()
			if len(batch) > 0 {
				// Process timeout batch
				processBatch := make([]WebhookBatchItem, len(batch))
				copy(processBatch, batch)

				// Reset batch
				batch = batch[:0]
				batchMutex.Unlock()

				// Process timeout batch synchronously - ack only after successful processing
				startTime := time.Now()

				// OPTIMIZATION: Batch deduplication - single Redis pipeline call instead of N individual calls
				uniqueItems, duplicateItems := performBatchDeduplication(processBatch)

				// Debug logging for deduplication
				if len(duplicateItems) > 0 {
					log.Printf("[StatusDebug] Timeout batch deduplication: %d unique, %d duplicates filtered", len(uniqueItems), len(duplicateItems))
				}

				// Extract webhooks for processing (only unique messages)
				webhooks := make([]DataGenWebhookPayload, len(uniqueItems))
				for i, item := range uniqueItems {
					webhooks[i] = item.Webhook
				}

				if err := handler(webhooks); err != nil {
					log.Printf("Failed to process DataGen webhook timeout batch: %v", err)
					// Nack all messages in the batch (both unique and duplicates)
					for _, item := range processBatch {
						item.Delivery.Nack(false, false)
					}
				} else {
					// Ack all messages in the batch (both unique and duplicates) only after successful processing
					for _, item := range processBatch {
						item.Delivery.Ack(false)
					}
					processingTime := time.Since(startTime)
					throughput := float64(len(webhooks)) / processingTime.Seconds()
					log.Printf("Successfully processed DataGen webhook timeout batch: %d unique messages in %v (%.2f msg/sec)",
						len(webhooks), processingTime, throughput)
				}
			} else {
				batchMutex.Unlock()
			}

			// CRITICAL FIX: Restart timer after each timeout to handle subsequent messages
			// This ensures the timer keeps working for new messages that arrive
			batchMutex.Lock()
			if len(batch) > 0 {
				// Get current timeout for restarting timer (thread-safe read)
				batchSizeMutex.RLock()
				currentBatchTimeout := batchTimeout
				batchSizeMutex.RUnlock()
				timeoutTimer.Reset(currentBatchTimeout)
			}
			batchMutex.Unlock()
		}
	}()

	return nil
}

// StartDataGenWebhookDLQConsumer starts a consumer for DataGen webhook DLQ processing with adaptive batch processing
// ch: The RabbitMQ channel to use (passed from ManageConnection)
// This consumer processes messages from the DLQ and always acks them to prevent loops
func StartDataGenWebhookDLQConsumer(ch *amqp.Channel, handler func([]DataGenWebhookPayload) error) error {
	if ch == nil {
		return fmt.Errorf("RabbitMQ channel is nil")
	}

	// OPTIMIZATION: Set prefetch for optimal throughput
	if err := ch.Qos(1000, 0, false); err != nil {
		return fmt.Errorf("failed to set prefetch: %w", err)
	}

	msgs, err := ch.Consume(
		DataGenWebhookDLQName,
		"",    // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("failed to start DataGen webhook DLQ consumer: %w", err)
	}

	// Redis-based deduplication (primary) + Consumer-side deduplication (backup)
	consumerDedupMap := make(map[string]time.Time)
	var consumerDedupMutex sync.RWMutex

	// Start consumer-side deduplication cleanup
	go func() {
		cleanupTicker := time.NewTicker(1 * time.Hour)
		defer cleanupTicker.Stop()

		for range cleanupTicker.C {
			consumerDedupMutex.Lock()
			cutoff := time.Now().Add(-24 * time.Hour) // Remove IDs older than 24 hours
			removeCount := 0

			for id, timestamp := range consumerDedupMap {
				if timestamp.Before(cutoff) {
					delete(consumerDedupMap, id)
					removeCount++
				}
			}
			log.Printf("[DLQ-ConsumerDedup] Cleaned up %d old message IDs, %d remaining in consumer dedup map", removeCount, len(consumerDedupMap))
			consumerDedupMutex.Unlock()
		}
	}()

	// OPTIMIZATION: Adaptive batch processing variables with mutex for thread safety
	var batchSize int = 100                          // Process 100 webhooks per batch (minimum batch size)
	var batchTimeout time.Duration = 2 * time.Second // 2-second timeout for faster processing
	var batchSizeMutex sync.RWMutex
	batch := make([]WebhookBatchItem, 0, 1000) // Pre-allocate with max capacity for larger batches
	var batchMutex sync.Mutex
	timeoutTimer := time.NewTimer(batchTimeout)
	timeoutTimer.Stop()

	// OPTIMIZATION: Dynamic batch sizing based on queue inspection
	go func() {
		queueInspectTicker := time.NewTicker(5 * time.Second)
		defer queueInspectTicker.Stop()

		for range queueInspectTicker.C {
			if qi, err := ch.QueueInspect(DataGenWebhookDLQName); err == nil {
				queueLength := qi.Messages

				batchSizeMutex.Lock()
				// Adjust batch size based on queue length
				if queueLength > 10000 {
					batchSize = 1000 // Very Large batches for high volume
					batchTimeout = 20 * time.Second
				} else if queueLength > 5000 {
					batchSize = 500 // Large batches for high volume
					batchTimeout = 15 * time.Second
				} else if queueLength > 1000 {
					batchSize = 200 // Medium-large batches
					batchTimeout = 10 * time.Second
				} else {
					batchSize = 100                // Minimum batch size for low volume
					batchTimeout = 5 * time.Second // Aggressive timeout
				}
				batchSizeMutex.Unlock()
			}
		}
	}()

	// Process messages in high-throughput batches with OPTIMIZED batch deduplication
	go func() {
		for d := range msgs {
			// Extract message ID for deduplication
			messageID := d.MessageId
			if messageID == "" {
				// Extract message ID from headers if not in MessageId field
				if msgID, ok := d.Headers["message-id"].(string); ok {
					messageID = msgID
				}
			}

			// Parse webhook message first
			var webhookMsg DataGenWebhookPayload
			if err := json.Unmarshal(d.Body, &webhookMsg); err != nil {
				log.Printf("[DLQ] Failed to unmarshal DataGen webhook: %v", err)
				// Always ack DLQ messages to prevent loops, even on parse errors
				d.Ack(false)
				continue
			}

			// Add to batch immediately (deduplication happens at batch processing time)
			batchMutex.Lock()
			batch = append(batch, WebhookBatchItem{
				Webhook:   webhookMsg,
				Delivery:  d,
				MessageID: messageID, // Store message ID for batch deduplication
			})

			// Get current batch size and timeout (thread-safe)
			batchSizeMutex.RLock()
			currentBatchSize := batchSize
			currentBatchTimeout := batchTimeout
			batchSizeMutex.RUnlock()

			shouldProcess := len(batch) >= currentBatchSize

			// OPTIMIZATION: For smaller queues, process immediately when we have enough messages
			// This prevents waiting for timeout when queue is small
			if currentBatchSize <= 200 && len(batch) >= 50 {
				// For smaller batches (≤200), process when we have 50+ messages
				shouldProcess = true
			} else if currentBatchSize <= 500 && len(batch) >= 100 {
				// For medium batches (≤500), process when we have 100+ messages
				shouldProcess = true
			}

			if len(batch) == 1 {
				// Start timeout timer for first message in batch
				timeoutTimer.Reset(currentBatchTimeout)
			}

			if shouldProcess {
				// Process current batch
				processBatch := make([]WebhookBatchItem, len(batch))
				copy(processBatch, batch)

				// Reset batch
				batch = batch[:0]
				timeoutTimer.Stop()
				batchMutex.Unlock()

				// Process batch synchronously - ack only after processing completes
				startTime := time.Now()

				// OPTIMIZATION: Batch deduplication - single Redis pipeline call instead of N individual calls
				uniqueItems, _ := performBatchDeduplication(processBatch)

				// Extract webhooks for processing (only unique messages)
				webhooks := make([]DataGenWebhookPayload, len(uniqueItems))
				for i, item := range uniqueItems {
					webhooks[i] = item.Webhook
				}

				if err := handler(webhooks); err != nil {
					log.Printf("[DLQ] Failed to process DataGen webhook batch: %v", err)
					// CRITICAL: Always ack DLQ messages even on failure to prevent infinite loops
					for _, item := range processBatch {
						item.Delivery.Ack(false)
					}
				} else {
					// Ack all messages in the batch (both unique and duplicates) only after successful processing
					for _, item := range processBatch {
						item.Delivery.Ack(false)
					}
					processingTime := time.Since(startTime)
					throughput := float64(len(webhooks)) / processingTime.Seconds()
					log.Printf("[DLQ] Successfully processed DataGen webhook batch: %d unique messages in %v (%.2f msg/sec)",
						len(webhooks), processingTime, throughput)
				}
			} else {
				batchMutex.Unlock()
				// Don't ack yet, wait for batch completion
			}
		}
	}()

	// Handle timeout for batch processing
	go func() {
		for range timeoutTimer.C {
			batchMutex.Lock()
			if len(batch) > 0 {
				// Process timeout batch
				processBatch := make([]WebhookBatchItem, len(batch))
				copy(processBatch, batch)

				// Reset batch
				batch = batch[:0]
				batchMutex.Unlock()

				// Process timeout batch synchronously - ack only after processing completes
				startTime := time.Now()

				// OPTIMIZATION: Batch deduplication - single Redis pipeline call instead of N individual calls
				uniqueItems, _ := performBatchDeduplication(processBatch)

				// Extract webhooks for processing (only unique messages)
				webhooks := make([]DataGenWebhookPayload, len(uniqueItems))
				for i, item := range uniqueItems {
					webhooks[i] = item.Webhook
				}

				if err := handler(webhooks); err != nil {
					log.Printf("[DLQ] Failed to process DataGen webhook timeout batch: %v", err)
					// CRITICAL: Always ack DLQ messages even on failure to prevent infinite loops
					for _, item := range processBatch {
						item.Delivery.Ack(false)
					}
				} else {
					// Ack all messages in the batch (both unique and duplicates) only after successful processing
					for _, item := range processBatch {
						item.Delivery.Ack(false)
					}
					processingTime := time.Since(startTime)
					throughput := float64(len(webhooks)) / processingTime.Seconds()
					log.Printf("[DLQ] Successfully processed DataGen webhook timeout batch: %d unique messages in %v (%.2f msg/sec)",
						len(webhooks), processingTime, throughput)
				}
			} else {
				batchMutex.Unlock()
			}

			// CRITICAL FIX: Restart timer after each timeout to handle subsequent messages
			// This ensures the timer keeps working for new messages that arrive
			batchMutex.Lock()
			if len(batch) > 0 {
				// Get current timeout for restarting timer (thread-safe read)
				batchSizeMutex.RLock()
				currentBatchTimeout := batchTimeout
				batchSizeMutex.RUnlock()
				timeoutTimer.Reset(currentBatchTimeout)
			}
			batchMutex.Unlock()
		}
	}()

	return nil
}

// =====================================================================================
// WEBHOOK FORWARDING: OUTBOUND WEBHOOK DELIVERY TO PROJECT OWNERS
// =====================================================================================

// WebhookForwardingPayload represents a webhook to be forwarded to an external endpoint
type WebhookForwardingPayload struct {
	DeliveryLogID  string                 `json:"delivery_log_id"`  // Reference to WebhookDeliveryLog
	ProjectOwnerID string                 `json:"project_owner_id"` // Project owner identifier
	WebhookID      string                 `json:"webhook_id"`       // Webhook configuration ID
	WebhookURL     string                 `json:"webhook_url"`      // Destination URL
	SharedSecret   string                 `json:"shared_secret"`    // Secret for HMAC signature
	EventType      string                 `json:"event_type"`       // e.g., "datagen.delivery"
	EventID        string                 `json:"event_id"`         // Unique event identifier
	Payload        map[string]interface{} `json:"payload"`          // The actual webhook payload
	Timestamp      int64                  `json:"timestamp"`        // Unix timestamp in milliseconds
	AttemptCount   int                    `json:"attempt_count"`    // Number of delivery attempts
}

// PublishWebhookForwarding publishes a webhook to the forwarding queue for delivery
func PublishWebhookForwarding(deliveryLogID, projectOwnerID, webhookID, webhookURL, sharedSecret, eventType, eventID string, payload map[string]interface{}, attemptCount int) error {
	// Ensure we have a healthy connection
	if err := EnsureConnection(); err != nil {
		return fmt.Errorf("failed to ensure RabbitMQ connection: %w", err)
	}

	if RabbitMQChannel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}

	// Create forwarding message
	forwardingMsg := WebhookForwardingPayload{
		DeliveryLogID:  deliveryLogID,
		ProjectOwnerID: projectOwnerID,
		WebhookID:      webhookID,
		WebhookURL:     webhookURL,
		SharedSecret:   sharedSecret,
		EventType:      eventType,
		EventID:        eventID,
		Payload:        payload,
		Timestamp:      time.Now().UnixMilli(),
		AttemptCount:   attemptCount,
	}

	// Marshal to JSON
	body, err := json.Marshal(forwardingMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook forwarding payload: %w", err)
	}

	// Calculate priority based on attempt count (higher attempts = lower priority)
	priority := uint8(10 - attemptCount)
	if priority < 1 {
		priority = 1
	}

	// Publish to webhook forwarding queue
	err = RabbitMQChannel.Publish(
		WebhookForwardingExchange,
		"webhook.forward",
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
			Priority:     priority,
			Headers: amqp.Table{
				"project-owner-id": projectOwnerID,
				"webhook-id":       webhookID,
				"event-type":       eventType,
				"event-id":         eventID,
				"attempt-count":    attemptCount,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish webhook forwarding: %w", err)
	}

	log.Printf("[WEBHOOK_FORWARD] Published forwarding job: project_owner=%s, event_type=%s, attempt=%d",
		projectOwnerID, eventType, attemptCount)
	return nil
}

// PublishWebhookForwardingWithDelay publishes a webhook retry to the delayed exchange
// The message will be held by RabbitMQ for the specified delay before being delivered to the main queue
func PublishWebhookForwardingWithDelay(deliveryLogID, projectOwnerID, webhookID, webhookURL, sharedSecret, eventType, eventID string, payload map[string]interface{}, attemptCount int, delay time.Duration) error {
	// Ensure we have a healthy connection
	if err := EnsureConnection(); err != nil {
		return fmt.Errorf("failed to ensure RabbitMQ connection: %w", err)
	}

	if RabbitMQChannel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}

	// Create forwarding message
	forwardingMsg := WebhookForwardingPayload{
		DeliveryLogID:  deliveryLogID,
		ProjectOwnerID: projectOwnerID,
		WebhookID:      webhookID,
		WebhookURL:     webhookURL,
		SharedSecret:   sharedSecret,
		EventType:      eventType,
		EventID:        eventID,
		Payload:        payload,
		Timestamp:      time.Now().UnixMilli(),
		AttemptCount:   attemptCount,
	}

	// Marshal to JSON
	body, err := json.Marshal(forwardingMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook forwarding payload: %w", err)
	}

	// Calculate priority based on attempt count (higher attempts = lower priority)
	priority := uint8(10 - attemptCount)
	if priority < 1 {
		priority = 1
	}

	// Convert delay to milliseconds for x-delay header
	delayMs := delay.Milliseconds()

	// Publish to webhook forwarding delayed exchange
	err = RabbitMQChannel.Publish(
		WebhookForwardingDelayedExchange, // Use delayed exchange
		"webhook.forward",                // Same routing key
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
			Priority:     priority,
			Headers: amqp.Table{
				"x-delay":          delayMs, // Delay in milliseconds
				"project-owner-id": projectOwnerID,
				"webhook-id":       webhookID,
				"event-type":       eventType,
				"event-id":         eventID,
				"attempt-count":    attemptCount,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish delayed webhook forwarding: %w", err)
	}

	log.Printf("[WEBHOOK_FORWARD] Published delayed retry: project_owner=%s, event_type=%s, attempt=%d, delay=%v",
		projectOwnerID, eventType, attemptCount, delay)
	return nil
}

func PublishCampaignExecutionMessage(message models.CampaignExecutionMessage) error {
	// Ensure connection is healthy
	if err := EnsureConnection(); err != nil {
		return fmt.Errorf("failed to ensure RabbitMQ connection: %w", err)
	}

	// Marshal message to JSON
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal campaign execution message: %w", err)
	}

	// Calculate delay in milliseconds
	delayMs := int64(0)
	if message.ScheduledAt.After(time.Now()) {
		delayMs = message.ScheduledAt.UnixMilli() - time.Now().UnixMilli()
	}

	// Create a dedicated channel for this operation
	ch, err := RabbitMQConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create dedicated channel: %w", err)
	}
	defer ch.Close()

	// Enable publisher confirms
	if err := ch.Confirm(false); err != nil {
		return fmt.Errorf("failed to enable publisher confirms: %w", err)
	}
	confirmCh := ch.NotifyPublish(make(chan amqp.Confirmation, 1))

	// Publish message with delay to existing delayed exchange infrastructure
	// Using routing key "campaign.execute" which is bound to CampaignExecutionQueue
	err = ch.Publish(
		DelayedExchange,    // Use existing delayed exchange
		"campaign.execute", // Special routing key for campaign execution triggers
		true,               // mandatory
		false,              // immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
			Priority:     8, // High priority (scheduled campaigns are important)
			Headers: amqp.Table{
				"x-delay":        delayMs,
				"campaign-id":    message.CampaignID,
				"message-type":   "campaign-execution",
				"execution-type": message.ExecutionType,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish campaign execution message: %w", err)
	}

	// Wait for confirmation
	select {
	case confirm := <-confirmCh:
		if !confirm.Ack {
			return fmt.Errorf("campaign execution message was not confirmed by RabbitMQ")
		}
		log.Printf("[CAMPAIGN_EXEC] Published campaign execution message for campaign %s with delay %dms",
			message.CampaignID, delayMs)
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for campaign execution message confirmation")
	}
}

func GetQueueNameForJobs(total int) string {
	if total <= 1000 {
		return CampaignQueueName1K
	}
	if total <= 10000 {
		return CampaignQueueName10K
	}
	if total <= 50000 {
		return CampaignQueueName50K
	}
	if total <= 100000 {
		return CampaignQueueName100K
	}
	if total <= 500000 {
		return CampaignQueueName500K
	}
	return CampaignQueueName1M
}
