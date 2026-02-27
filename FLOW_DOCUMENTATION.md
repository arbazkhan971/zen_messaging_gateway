# Complete Request Flow Documentation

## Table of Contents
1. [Infrastructure Overview](#infrastructure-overview)
2. [Message Sending Flow](#message-sending-flow)
3. [Webhook Processing Flow](#webhook-processing-flow)
4. [Webhook Forwarding Flow](#webhook-forwarding-flow)
5. [Data Flow Diagrams](#data-flow-diagrams)

---

## Infrastructure Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Internet / Client                           │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Kubernetes Ingress (NGINX)                      │
│                          serri.co.in                                 │
│                                                                       │
│  Routes:                                                             │
│  - /datagen-webhook        → Webhook Consumer Pod                   │
│  - /webhook/config/*       → API Pod                                │
│  - /project/campaign/*     → API Pod                                │
│  - All other routes        → API Pod                                │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
┌──────────────────────────────┐   ┌──────────────────────────────┐
│   Webhook Consumer Pods      │   │        API Pods              │
│   (webhook-consumer branch)  │   │    (main application)        │
│                              │   │                              │
│  - Receives webhooks         │   │  - Handles API requests      │
│  - Publishes to RabbitMQ     │   │  - Business logic            │
│  - Webhook forwarding        │   │  - Campaign management       │
└──────────────────────────────┘   └──────────────────────────────┘
                    │                               │
                    └───────────────┬───────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                          Infrastructure                              │
│                                                                       │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐            │
│  │  RabbitMQ   │    │   MongoDB   │    │   Redis     │            │
│  │  (Queues)   │    │  (Storage)  │    │  (Cache)    │            │
│  └─────────────┘    └─────────────┘    └─────────────┘            │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Message Sending Flow

### Scenario 1: User Sends a Campaign Message

```
┌──────────────┐
│  Frontend    │  User creates campaign, uploads CSV
│  Dashboard   │
└──────┬───────┘
       │ POST /project/campaign/event/send
       │ {
       │   "campaign_id": "...",
       │   "csv_url": "...",
       │   "template_id": "..."
       │ }
       ▼
┌──────────────────────────────────────────────────────────────┐
│  API Server (main.go → Routers → CampaignController)        │
│  File: Controllers/Campaign/CampaignController.go            │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ 1. Validate campaign request
       │ 2. Download CSV from S3
       │ 3. Parse recipients
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  Campaign Processing                                          │
│                                                               │
│  For each recipient:                                         │
│  - Extract phone number, variables                           │
│  - Create campaign job                                       │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ Batch publish to RabbitMQ
       ▼
┌──────────────────────────────────────────────────────────────┐
│  RabbitMQ Queue: campaign_sending_jobs2929                   │
│                                                               │
│  Message Structure:                                          │
│  {                                                            │
│    "campaign_id": "...",                                     │
│    "phone_number": "+91...",                                 │
│    "template_id": "...",                                     │
│    "variables": {...},                                       │
│    "schedule_time": 0  // or timestamp for scheduled        │
│  }                                                            │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ Consumed by campaign consumer
       ▼
┌──────────────────────────────────────────────────────────────┐
│  Campaign Consumer (main.go - commented out)                 │
│  File: Consumers/campaign_sender.go                          │
│                                                               │
│  For each message:                                           │
│  1. Check deduplication (in-memory + Redis)                  │
│  2. Determine BSP (Aisensy, Datagen, Direct)                │
│  3. Build message payload                                    │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ Routes to appropriate BSP
       ▼
┌───────────────┬───────────────┬────────────────┐
│   Aisensy     │   Datagen     │  Aisensy       │
│   (Default)   │   (Karix)     │  Direct        │
└───────┬───────┴───────┬───────┴───────┬────────┘
        │               │               │
        │ HTTP POST     │ HTTP POST     │ HTTP POST
        ▼               ▼               ▼
┌──────────────────────────────────────────────────┐
│         WhatsApp Business APIs                    │
│         (Meta / BSP APIs)                        │
└──────┬───────────────────────────────────────────┘
       │
       │ Returns wamid (WhatsApp Message ID)
       │ wamid.HBgLMTY0NjcwNDM1OTUVAgARGBI1RjQyNUE3...
       ▼
┌──────────────────────────────────────────────────┐
│  Store in MongoDB:                                │
│  - datagen_audience (if Datagen)                 │
│  - campaign_messages (if Aisensy)                │
│                                                   │
│  Track:                                          │
│  - wamid (for status tracking)                   │
│  - campaign_id                                   │
│  - phone_number                                  │
│  - initial_status: "sent"                        │
└──────────────────────────────────────────────────┘
       │
       │ BSP sends webhooks for status updates
       ▼
    (See Webhook Processing Flow)
```

---

## Webhook Processing Flow

### Scenario 2: BSP Sends Status Update Webhook

```
┌──────────────────────────────────────────────────┐
│  External BSP (Datagen/Karix/Aisensy)           │
│  Sends webhook for message status update        │
└──────┬───────────────────────────────────────────┘
       │
       │ HTTP POST https://serri.co.in/datagen-webhook
       │ {
       │   "events": {
       │     "mid": "wamid.HBgL...",  ← WhatsApp Message ID
       │     "eventType": "DELIVERY EVENTS"
       │   },
       │   "notificationAttributes": {
       │     "status": "delivered"
       │   },
       │   "recipient": {
       │     "to": "+91...",
       │     "reference": {
       │       "parent_campaign_id": "..."
       │     }
       │   }
       │ }
       ▼
┌──────────────────────────────────────────────────────────────┐
│  Kubernetes Ingress (serri.co.in)                            │
│  Route: /datagen-webhook → Webhook Consumer Pod             │
└──────┬───────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  DatagenWebhookHandler                                        │
│  File: Controllers/Webhook/WebhookHandler.go:222             │
│                                                               │
│  Step 1: Read request body                                   │
│  Step 2: Return 200 OK IMMEDIATELY (prevent retries)        │
│  Step 3: Determine webhook type:                            │
│          - batch (array)                                     │
│          - delivery (contains "DELIVERY EVENTS")             │
│          - user_message (contains "User initiated")          │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ Spawns goroutine (with semaphore limit: 1000)
       ▼
┌──────────────────────────────────────────────────────────────┐
│  Background Processing Goroutine                              │
│                                                               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ Step 1: Publish to RabbitMQ                         │    │
│  │         utils.PublishDataGenWebhookHighThroughput() │    │
│  │                                                      │    │
│  │         Queue: datagen_webhook_processing           │    │
│  │         Priority: 5 (user_message) or 0 (campaign)  │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ Step 2: Trigger Webhook Forwarding                  │    │
│  │         triggerWebhookForwarding()                  │    │
│  │                                                      │    │
│  │         - Extract project_owner_id from payload     │    │
│  │         - Check if webhook config exists            │    │
│  │         - Enqueue to webhook forwarding queue       │    │
│  └─────────────────────────────────────────────────────┘    │
└──────┬───────────────────────────────────────────────────────┘
       │
       ├──────────────────┬──────────────────┐
       ▼                  ▼                  ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  RabbitMQ    │  │  RabbitMQ    │  │  Fallback    │
│  datagen_    │  │  webhook_    │  │  Queue       │
│  webhook_    │  │  forwarding_ │  │  (on error)  │
│  processing  │  │  queue       │  │              │
└──────┬───────┘  └──────┬───────┘  └──────────────┘
       │                  │
       │                  │ (See Webhook Forwarding Flow)
       ▼                  ▼
┌──────────────────────────────────────────────────────────────┐
│  Webhook Processor Consumer                                   │
│  File: Utils/rabbitmq_worker_pool.go                         │
│                                                               │
│  Configuration:                                              │
│  - 75 workers (worker pool)                                  │
│  - 15 messages per batch                                     │
│  - 2 second timeout                                          │
│                                                               │
│  For each batch:                                             │
│  1. Parse webhook payload                                    │
│  2. Extract wamid, status, phone number                      │
│  3. Update datagen_audience collection                       │
└──────┬───────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  MongoDB Update: datagen_audience                             │
│                                                               │
│  db.datagen_audience.updateOne(                              │
│    { "wamid": "wamid.HBgL..." },                             │
│    {                                                          │
│      $set: {                                                 │
│        "current_status": "delivered",                        │
│        "last_updated": new Date()                            │
│      },                                                       │
│      $push: {                                                │
│        "status_events": {                                    │
│          "status": "delivered",                              │
│          "timestamp": new Date(),                            │
│          "event_id": "..."                                   │
│        }                                                      │
│      }                                                        │
│    }                                                          │
│  )                                                            │
│                                                               │
│  Status Progression:                                         │
│  sent → delivered → read                                     │
│  sent → failed                                               │
└──────────────────────────────────────────────────────────────┘
```

---

## Webhook Forwarding Flow

### Scenario 3: Forward Webhook to Client's Endpoint

```
┌──────────────────────────────────────────────────────────────┐
│  triggerWebhookForwarding()                                   │
│  File: Controllers/Webhook/WebhookHandler.go:317             │
│                                                               │
│  Step 1: Extract project_owner_id from payload              │
│         - For delivery: sender.from (business number)        │
│         - For user_message: recipient.to (business number)   │
│         - Uses ProjectLookupCache (memory + Redis)          │
└──────┬───────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  GetWebhookByProjectOwnerID()                                │
│  File: Controllers/Webhook/WebhookConfigController.go        │
│                                                               │
│  Multi-layer cache lookup:                                   │
│  1. Memory cache (sync.Map) - 1ms latency                   │
│  2. Redis cache - 5ms latency                                │
│  3. MongoDB - 50-100ms latency (fallback)                    │
│                                                               │
│  Returns:                                                     │
│  {                                                            │
│    "project_owner_id": "user123",                            │
│    "webhook_url": "https://client.com/webhook",              │
│    "shared_secret": "secret_for_hmac",                       │
│    "topics": ["datagen.delivery", "datagen.user_message"]   │
│  }                                                            │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ Check if subscribed to event type
       ▼
┌──────────────────────────────────────────────────────────────┐
│  EnqueueWebhookDelivery()                                     │
│  File: Controllers/Webhook/WebhookDeliveryBatcher.go         │
│                                                               │
│  Batched delivery log writer:                                │
│  - Batches DB inserts (100 per batch)                        │
│  - Reduces DB load                                           │
│  - Publishes to RabbitMQ in parallel                         │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ Insert into MongoDB & Publish to RabbitMQ
       ▼
┌──────────────────────────────────────────────────────────────┐
│  MongoDB: webhook_delivery_logs                               │
│                                                               │
│  {                                                            │
│    "_id": ObjectId("..."),                                   │
│    "project_owner_id": "user123",                            │
│    "webhook_id": "webhook_abc",                              │
│    "event_type": "datagen.delivery",                         │
│    "event_id": "unique_event_id",                            │
│    "webhook_url": "https://client.com/webhook",              │
│    "payload": { ...original_webhook_data... },               │
│    "attempt_count": 0,                                       │
│    "status": "pending",                                      │
│    "created_at": ISODate("...")                              │
│  }                                                            │
└──────────────────────────────────────────────────────────────┘
       │
       │ Publishes to RabbitMQ
       ▼
┌──────────────────────────────────────────────────────────────┐
│  RabbitMQ: webhook_forwarding_queue                          │
│                                                               │
│  Message Structure:                                          │
│  {                                                            │
│    "delivery_log_id": "ObjectId(...)",                       │
│    "project_owner_id": "user123",                            │
│    "webhook_id": "webhook_abc",                              │
│    "webhook_url": "https://client.com/webhook",              │
│    "shared_secret": "secret_for_hmac",                       │
│    "event_type": "datagen.delivery",                         │
│    "event_id": "unique_event_id",                            │
│    "payload": { ...webhook_data... },                        │
│    "attempt_count": 0                                        │
│  }                                                            │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ Consumed by dedicated webhook forwarder
       ▼
┌──────────────────────────────────────────────────────────────┐
│  StartWebhookForwardingConsumer()                            │
│  File: Consumers/webhook_forwarding_consumer.go              │
│                                                               │
│  Configuration:                                              │
│  - 100 concurrent workers (goroutine pool)                   │
│  - 1000 message prefetch                                     │
│  - Circuit breaker per webhook URL                           │
│  - Dedicated RabbitMQ connection (isolated)                  │
│                                                               │
│  For each webhook:                                           │
│  1. Check circuit breaker for URL                            │
│  2. Spawn goroutine from pool                                │
│  3. Process delivery                                         │
└──────┬───────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  processWebhookForwarding()                                   │
│  File: Consumers/webhook_forwarding_consumer.go:275          │
│                                                               │
│  Step 1: Build HTTP request                                  │
│         Body: {                                              │
│           "event_type": "datagen.delivery",                  │
│           "event_id": "...",                                 │
│           "timestamp": "...",                                │
│           "data": { ...original_payload... }                 │
│         }                                                     │
│                                                               │
│  Step 2: Generate HMAC signature                             │
│         signature = HMAC-SHA256(body, shared_secret)         │
│                                                               │
│  Step 3: Set headers                                         │
│         X-Event-Type: datagen.delivery                       │
│         X-Event-ID: unique_event_id                          │
│         X-Webhook-Signature: <hmac_signature>                │
│         X-Attempt-Count: 1                                   │
└──────┬───────────────────────────────────────────────────────┘
       │
       │ HTTP POST with shared HTTP client (connection pooling)
       ▼
┌──────────────────────────────────────────────────────────────┐
│  deliverWebhook()                                             │
│  File: Consumers/webhook_forwarding_consumer.go:391          │
│                                                               │
│  - Timeout: 15 seconds                                       │
│  - Max response body: 10KB                                   │
│  - Shared HTTP client (connection reuse)                     │
└──────┬───────────────────────────────────────────────────────┘
       │
       ▼
    ┌──────────────────────────────────┐
    │   Client Webhook URL             │
    │   https://client.com/webhook     │
    └──────┬───────────────────────────┘
           │
           │ Response Status Code
           ▼
    ┌──────────────────────────────────────────┐
    │  Handle Response                          │
    │                                           │
    │  Success (2xx):                          │
    │  - Record success in circuit breaker     │
    │  - Update DB: status = "success"         │
    │  - ACK RabbitMQ message                  │
    │                                           │
    │  Retryable (408, 429, 5xx):             │
    │  - Increment attempt_count               │
    │  - Publish to delayed exchange           │
    │  - Retry after 3 seconds                 │
    │  - Max 3 attempts                        │
    │                                           │
    │  Permanent Failure (4xx except 429):    │
    │  - Update DB: status = "failed"          │
    │  - Send to DLQ (Dead Letter Queue)       │
    │  - Record failure in circuit breaker     │
    │                                           │
    │  Circuit Breaker:                        │
    │  - Opens after 10 failures               │
    │  - Half-open after 5 minutes             │
    │  - Prevents cascade failures             │
    └──────────────────────────────────────────┘
```

---

## Data Flow Diagrams

### Complete End-to-End Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    1. Message Sending Request                        │
└─────────────────────────────────────────────────────────────────────┘
                                    │
    User API Request (Campaign)     │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Frontend → API Server → RabbitMQ → Campaign Consumer → BSP         │
│                                                              │         │
│                                                              ▼         │
│                                                        Meta/WhatsApp  │
│                                                              │         │
│                                                      Returns wamid    │
│                                                              │         │
│                                                              ▼         │
│                                                     Store in MongoDB  │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ BSP sends webhook
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    2. Webhook Processing                             │
└─────────────────────────────────────────────────────────────────────┘
                                    │
    BSP Webhook (Status Update)     │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│  BSP → serri.co.in/datagen-webhook → WebhookHandler                 │
│                         │                                             │
│                         ├─→ Publish to datagen_webhook_processing   │
│                         │   (Update status in MongoDB)               │
│                         │                                             │
│                         └─→ Trigger webhook forwarding               │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    3. Webhook Forwarding                             │
└─────────────────────────────────────────────────────────────────────┘
                                    │
    Forward to Client Endpoint      │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Extract project_owner_id → Lookup webhook config (cache)           │
│         │                                                             │
│         ├─→ Create delivery log in MongoDB                          │
│         │                                                             │
│         └─→ Publish to webhook_forwarding_queue                     │
│                         │                                             │
│                         ▼                                             │
│            Webhook Forwarder Consumer                                │
│            (100 workers, circuit breaker)                            │
│                         │                                             │
│                         ▼                                             │
│            HTTP POST → Client Webhook URL                            │
│            (with HMAC signature)                                     │
│                         │                                             │
│                         ▼                                             │
│            ┌──────────────────────────────┐                          │
│            │  Success: Update status      │                          │
│            │  Failure: Retry (3 attempts) │                          │
│            │  Permanent: Send to DLQ      │                          │
│            └──────────────────────────────┘                          │
└─────────────────────────────────────────────────────────────────────┘
```

### Queue Processing Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                         RabbitMQ Queues                            │
├───────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. campaign_sending_jobs2929                                     │
│     - Message sending jobs                                        │
│     - Consumed by: CampaignSendConsumer                          │
│     - Prefetch: 100-1000                                         │
│                                                                     │
│  2. datagen_webhook_processing                                    │
│     - Webhook processing jobs                                     │
│     - Consumed by: DataGenWebhookConsumer                        │
│     - Worker Pool: 75 workers                                    │
│     - Batch Size: 15 messages                                    │
│     - Priority: user_message (5) > campaign (0)                  │
│                                                                     │
│  3. webhook_forwarding_queue                                      │
│     - Webhook delivery jobs                                       │
│     - Consumed by: WebhookForwardingConsumer                     │
│     - Worker Pool: 100 workers                                   │
│     - Prefetch: 1000                                             │
│     - Dedicated connection (isolated)                            │
│                                                                     │
│  4. webhook_forwarding_delayed_exchange                           │
│     - Retry queue with x-delayed-message plugin                  │
│     - Delay: 3 seconds per retry                                 │
│     - Max retries: 3                                             │
│                                                                     │
│  5. webhook_forwarding_dlq                                        │
│     - Dead Letter Queue for permanent failures                   │
│     - Manual intervention required                               │
└───────────────────────────────────────────────────────────────────┘
```

### Caching Strategy

```
┌───────────────────────────────────────────────────────────────────┐
│                    Multi-Layer Cache                               │
├───────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Request: Get webhook config for project_owner_id                 │
│                                                                     │
│  Layer 1: In-Memory Cache (sync.Map)                              │
│  ├─ Hit Rate: 90%+                                                │
│  ├─ Latency: <1ms                                                 │
│  └─ TTL: 5 minutes                                                │
│           │                                                         │
│           │ (cache miss)                                           │
│           ▼                                                         │
│  Layer 2: Redis Cache                                             │
│  ├─ Hit Rate: 8-9%                                                │
│  ├─ Latency: ~5ms                                                 │
│  └─ TTL: 1 hour                                                   │
│           │                                                         │
│           │ (cache miss)                                           │
│           ▼                                                         │
│  Layer 3: MongoDB (Fallback)                                      │
│  ├─ Hit Rate: 1-2%                                                │
│  ├─ Latency: 50-100ms                                             │
│  └─ Permanent storage                                             │
│           │                                                         │
│           │ (result)                                               │
│           ▼                                                         │
│  Populate all cache layers (write-through)                        │
│                                                                     │
│  Circuit Breaker:                                                  │
│  - Protects against Redis failures                                │
│  - Falls back to MongoDB directly                                 │
│  - Opens after 5 consecutive failures                             │
└───────────────────────────────────────────────────────────────────┘
```

---

## Key Concepts

### 1. **wamid (WhatsApp Message ID)**
- Unique identifier for each WhatsApp message
- Format: `wamid.HBgLMTY0NjcwNDM1OTUVAgARGBI1RjQyNUE3...`
- Used to track message status across the lifecycle
- Returned by BSP after successful message submission
- Stored in MongoDB for status tracking

### 2. **Status Progression**
```
sent → delivered → read       (successful flow)
sent → failed                 (failure flow)
```

### 3. **Priority Queues**
- User-initiated messages: Priority 5 (high)
- Campaign messages: Priority 0 (normal)
- Ensures user messages are processed first

### 4. **Circuit Breaker Pattern**
- Per-URL circuit breakers in webhook forwarding
- Opens after 10 consecutive failures
- Half-open state after 5 minutes
- Prevents cascade failures to client endpoints

### 5. **Deduplication**
- In-memory deduplication (per process)
- Redis-based deduplication (distributed)
- Prevents duplicate message processing
- TTL: 24 hours

### 6. **Batching**
- Campaign messages: Batch published to RabbitMQ
- Webhook processing: Batch consumed (15 per batch)
- Delivery logs: Batch inserted to MongoDB (100 per batch)
- Reduces DB load and improves throughput

---

## Performance Characteristics

| Component | Throughput | Latency | Workers |
|-----------|------------|---------|---------|
| Webhook Ingress | ~3000/sec | <50ms | N/A |
| Webhook Processing | ~1500/sec | ~100ms | 75 |
| Webhook Forwarding | ~100 concurrent | <15s | 100 |
| Campaign Sending | ~1000/sec | ~200ms | Variable |

---

## Error Handling

### Retry Strategy
1. **Transient Errors** (408, 429, 5xx):
   - Retry 3 times with 3-second delay
   - Exponential backoff (optional)
   - Circuit breaker protection

2. **Permanent Errors** (4xx except 429):
   - No retry
   - Send to Dead Letter Queue
   - Alert/manual intervention

3. **Network Errors**:
   - Timeout after 15 seconds
   - Retry with exponential backoff
   - Max 3 attempts

### Monitoring Points
- Queue depth (RabbitMQ)
- Consumer lag
- Circuit breaker status
- Delivery success rate
- Database connection pool
- Cache hit rates

---

## Security

### HMAC Signature Verification
```javascript
// Generate signature (server-side)
signature = HMAC-SHA256(request_body, shared_secret)

// Verify signature (client-side)
expected_signature = HMAC-SHA256(request_body, shared_secret)
is_valid = (received_signature === expected_signature)
```

### Headers Sent to Client
```
X-Event-Type: datagen.delivery
X-Event-ID: unique_event_id
X-Webhook-Signature: <hmac_sha256_signature>
X-Attempt-Count: 1
User-Agent: Serri-Webhook-Forwarder/1.0
Content-Type: application/json
```

---

This documentation provides a complete overview of how requests flow through your system. For specific implementation details, refer to the source files mentioned in each section.
