# Webhook Routing Design - Multiple Endpoints for Different Outcomes

## Overview

Instead of a single webhook endpoint that handles all BSPs, we create dedicated endpoints for each BSP/use-case, allowing different processing logic, queues, and outcomes.

## Current Architecture (serri.co.in)

```
External Sources → Single Endpoint → Universal Handler → Different Queues
                   /datagen-webhook
                   /partner-webhook
```

## Proposed ZEN Architecture (api.zen.serri.in)

```
┌─────────────────────────────────────────────────────────────────────┐
│                    api.zen.serri.in (Ingress)                        │
└─────────────────────────────────────────────────────────────────────┘
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        │                           │                           │
        ▼                           ▼                           ▼
┌──────────────┐          ┌──────────────┐          ┌──────────────┐
│  /aisensy    │          │  /datagen    │          │  /karix      │
│  webhook     │          │  webhook     │          │  webhook     │
└──────┬───────┘          └──────┬───────┘          └──────┬───────┘
       │                          │                          │
       ▼                          ▼                          ▼
┌──────────────┐          ┌──────────────┐          ┌──────────────┐
│ AisensyWebh- │          │ DatagenWebh- │          │ KarixWebhook │
│ookHandler    │          │ ookHandler   │          │ Handler      │
└──────┬───────┘          └──────┬───────┘          └──────┬───────┘
       │                          │                          │
       │                          │                          │
       ▼                          ▼                          ▼
┌──────────────────────────────────────────────────────────────────┐
│              RabbitMQ - Different Queues                          │
├──────────────────────────────────────────────────────────────────┤
│                                                                    │
│  aisensy_webhook_queue      (Aisensy-specific processing)        │
│  datagen_webhook_queue      (Datagen-specific processing)        │
│  karix_webhook_queue        (Karix-specific processing)          │
│                                                                    │
└──────────────────────────────────────────────────────────────────┘
       │                          │                          │
       ▼                          ▼                          ▼
┌──────────────┐          ┌──────────────┐          ┌──────────────┐
│  Consumer    │          │  Consumer    │          │  Consumer    │
│  Aisensy     │          │  Datagen     │          │  Karix       │
└──────┬───────┘          └──────┬───────┘          └──────┬───────┘
       │                          │                          │
       │                          │                          │
       ▼                          ▼                          ▼
┌──────────────────────────────────────────────────────────────────┐
│              MongoDB - Different Collections                      │
├──────────────────────────────────────────────────────────────────┤
│                                                                    │
│  aisensy_messages           (Aisensy message tracking)           │
│  datagen_audience           (Datagen delivery tracking)          │
│  karix_messages             (Karix message tracking)             │
│                                                                    │
│  unified_message_tracking   (Normalized view across all BSPs)    │
│                                                                    │
└──────────────────────────────────────────────────────────────────┘
```

## Endpoint Design

### 1. Aisensy Webhook Endpoint

**URL**: `POST /webhook/aisensy`

**Purpose**: 
- Handle Aisensy-specific webhook format
- Process message status updates from Aisensy
- Track Aisensy-specific events

**Payload Example**:
```json
{
  "topic": "message.status.updated",
  "data": {
    "message_id": "msg_123",
    "phone_number": "+91...",
    "status": "delivered",
    "timestamp": "..."
  }
}
```

**Processing Flow**:
```
AisensyWebhookHandler
    ↓
Validate Aisensy format
    ↓
Publish to: aisensy_webhook_queue
    ↓
AisensyWebhookConsumer
    ↓
Update: aisensy_messages collection
    ↓
Trigger webhook forwarding
```

### 2. Datagen Webhook Endpoint

**URL**: `POST /webhook/datagen`

**Purpose**:
- Handle Datagen/Karix webhook format
- Process delivery events with wamid
- Track conversation details and billing

**Payload Example**:
```json
{
  "channel": "whatsapp",
  "events": {
    "eventType": "DELIVERY EVENTS",
    "mid": "wamid.HBgL..."
  },
  "notificationAttributes": {
    "status": "delivered"
  },
  "recipient": {
    "to": "+91...",
    "reference": {
      "parent_campaign_id": "..."
    }
  }
}
```

**Processing Flow**:
```
DatagenWebhookHandler
    ↓
Validate Datagen format
    ↓
Publish to: datagen_webhook_queue
    ↓
DatagenWebhookConsumer
    ↓
Update: datagen_audience collection
    ↓
Trigger webhook forwarding
```

### 3. Karix Direct Webhook Endpoint

**URL**: `POST /webhook/karix`

**Purpose**:
- Handle Karix direct integration
- Different from Datagen (if using Karix directly)
- Custom processing logic

**Processing Flow**:
```
KarixWebhookHandler
    ↓
Validate Karix format
    ↓
Publish to: karix_webhook_queue
    ↓
KarixWebhookConsumer
    ↓
Update: karix_messages collection
    ↓
Trigger webhook forwarding
```

### 4. Generic Partner Webhook Endpoint

**URL**: `POST /webhook/partner/:partner_id`

**Purpose**:
- Handle any future BSP integration
- Generic webhook processor
- Dynamic routing based on partner_id

**Processing Flow**:
```
PartnerWebhookHandler
    ↓
Identify partner from URL parameter
    ↓
Route to partner-specific processor
    ↓
Publish to: partner_webhook_queue_{partner_id}
    ↓
PartnerWebhookConsumer
    ↓
Update: partner_messages collection
```

## Unified Message Tracking

### Concept: Single Source of Truth

Regardless of which endpoint receives the webhook, all messages are normalized and stored in a unified format for tracking.

```javascript
// Unified Message Tracking Document
{
  "_id": ObjectId("..."),
  "message_id": "unique_id",           // Our internal ID
  "wamid": "wamid.HBgL...",           // WhatsApp Message ID
  "bsp": "datagen",                    // Which BSP sent this
  "bsp_message_id": "msg_123",        // BSP's message ID
  
  // Contact info
  "phone_number": "+91...",
  "business_number": "91...",
  
  // Status tracking
  "current_status": "delivered",
  "status_history": [
    {"status": "sent", "timestamp": "...", "source": "datagen"},
    {"status": "delivered", "timestamp": "...", "source": "datagen"}
  ],
  
  // Campaign/context
  "campaign_id": "...",
  "project_id": "...",
  "project_owner_id": "user123",
  
  // BSP-specific data
  "bsp_data": {
    "datagen": {
      "conv_details": {...},
      "is_billable": true
    }
  },
  
  // Timestamps
  "sent_at": ISODate("..."),
  "delivered_at": ISODate("..."),
  "read_at": null,
  "failed_at": null,
  
  "created_at": ISODate("..."),
  "updated_at": ISODate("...")
}
```

## Routing Configuration

### Dynamic BSP Selection

When sending a message, the system decides which BSP to use:

```javascript
// Example: Campaign creation
POST /api/v1/campaign/create
{
  "campaign_name": "Summer Sale",
  "template_id": "...",
  "recipients": [...],
  "bsp": "datagen",           // User selects BSP
  "webhook_callback": "auto"   // Auto-configure webhook URL
}

// System automatically sets webhook URL based on BSP:
// BSP: datagen  → https://api.zen.serri.in/webhook/datagen
// BSP: aisensy  → https://api.zen.serri.in/webhook/aisensy
// BSP: karix    → https://api.zen.serri.in/webhook/karix
```

### BSP Configuration in Database

```javascript
// BSP Configuration Collection
{
  "bsp_name": "datagen",
  "bsp_id": "datagen_prod",
  "webhook_endpoint": "/webhook/datagen",
  "webhook_url": "https://api.zen.serri.in/webhook/datagen",
  "queue_name": "datagen_webhook_queue",
  "consumer_config": {
    "worker_pool_size": 75,
    "batch_size": 15,
    "timeout": "2s"
  },
  "api_config": {
    "base_url": "https://api.karix.com",
    "auth_type": "bearer",
    "rate_limit": 100
  },
  "status_mapping": {
    "sent": "sent",
    "delivered": "delivered",
    "Delivered": "delivered",  // Normalize capitalization
    "read": "read",
    "Read": "read",
    "failed": "failed",
    "Not Sent": "failed"
  }
}
```

## Implementation Plan

### Phase 1: Core Webhook Routing

1. **Create webhook handlers for each BSP**
   - `handlers/webhook_aisensy.go`
   - `handlers/webhook_datagen.go`
   - `handlers/webhook_karix.go`

2. **Set up dedicated RabbitMQ queues**
   - `aisensy_webhook_queue`
   - `datagen_webhook_queue`
   - `karix_webhook_queue`

3. **Create BSP-specific consumers**
   - `consumers/aisensy_webhook_consumer.go`
   - `consumers/datagen_webhook_consumer.go`
   - `consumers/karix_webhook_consumer.go`

4. **Implement unified message tracker**
   - `services/message_tracker.go`
   - Normalize all BSP formats into unified format
   - Store in `unified_message_tracking` collection

### Phase 2: Advanced Features

5. **BSP health monitoring**
   - Track webhook delivery rate per BSP
   - Detect BSP outages
   - Auto-failover to backup BSP

6. **Smart routing**
   - Route based on message type (marketing vs transactional)
   - Cost optimization (cheapest BSP first)
   - Load balancing across BSPs

7. **Analytics dashboard**
   - Per-BSP delivery rates
   - Cost per message by BSP
   - BSP reliability metrics

## Benefits of Multiple Endpoints

### 1. **Isolation**
- BSP failures don't affect others
- Independent scaling per BSP
- Separate rate limiting

### 2. **Flexibility**
- Different processing logic per BSP
- Custom retry strategies
- BSP-specific optimizations

### 3. **Monitoring**
- Clear metrics per BSP
- Easy to identify BSP issues
- Better debugging

### 4. **Security**
- Different authentication per BSP
- Separate webhook secrets
- IP whitelisting per BSP

### 5. **Migration**
- Easy to add/remove BSPs
- Gradual migration between BSPs
- A/B testing with different BSPs

## Code Structure

```
zen_messaging_gateway/
├── handlers/
│   ├── webhook/
│   │   ├── aisensy.go          # Aisensy webhook handler
│   │   ├── datagen.go          # Datagen webhook handler
│   │   ├── karix.go            # Karix webhook handler
│   │   ├── partner.go          # Generic partner handler
│   │   └── base.go             # Shared webhook utilities
│   │
├── consumers/
│   ├── aisensy_webhook_consumer.go
│   ├── datagen_webhook_consumer.go
│   ├── karix_webhook_consumer.go
│   └── webhook_forwarder.go    # Existing forwarder
│
├── services/
│   ├── message_tracker.go      # Unified tracking
│   ├── bsp_router.go           # BSP selection logic
│   └── normalizer.go           # Format normalization
│
├── models/
│   ├── aisensy_webhook.go
│   ├── datagen_webhook.go
│   ├── karix_webhook.go
│   └── unified_message.go      # Unified format
│
└── config/
    └── bsp_config.go           # BSP configuration
```

## Example: Adding a New BSP

```go
// 1. Create webhook handler
// handlers/webhook/new_bsp.go
func NewBSPWebhookHandler(c *gin.Context) {
    // Parse webhook
    var payload NewBSPWebhookPayload
    if err := c.ShouldBindJSON(&payload); err != nil {
        c.JSON(400, gin.H{"error": "Invalid payload"})
        return
    }
    
    // Return 200 immediately
    c.JSON(200, gin.H{"status": "received"})
    
    // Process in background
    go func() {
        // Publish to new_bsp_webhook_queue
        utils.PublishToQueue("new_bsp_webhook_queue", payload)
        
        // Trigger webhook forwarding
        triggerWebhookForwarding("new_bsp.delivery", payload)
    }()
}

// 2. Register route
router.POST("/webhook/new-bsp", NewBSPWebhookHandler)

// 3. Create consumer
// consumers/new_bsp_webhook_consumer.go
func StartNewBSPWebhookConsumer(ch *amqp.Channel) {
    // Consumer logic
}

// 4. Update BSP config
bspConfig := BSPConfig{
    Name: "new_bsp",
    WebhookEndpoint: "/webhook/new-bsp",
    QueueName: "new_bsp_webhook_queue",
}
```

## Migration Strategy

### From Single Endpoint to Multiple Endpoints

**Step 1**: Keep existing `/datagen-webhook` working
**Step 2**: Create new BSP-specific endpoints
**Step 3**: Configure BSPs to use new endpoints
**Step 4**: Monitor both endpoints during transition
**Step 5**: Deprecate old endpoint after migration complete

```
Week 1-2: Set up new endpoints (parallel with old)
Week 3-4: Configure Datagen to use /webhook/datagen
Week 5-6: Configure Aisensy to use /webhook/aisensy
Week 7-8: Monitor and verify all webhooks working
Week 9+:  Deprecate /datagen-webhook endpoint
```

---

This design provides maximum flexibility for handling different BSP webhooks with dedicated processing logic and outcomes for each source.
