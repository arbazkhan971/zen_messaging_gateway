#!/bin/bash

# Source directory
SRC="/Users/arbaz/Lineupx/SerriBackendGolang"

# Copy webhook consumer
cp "$SRC/Consumers/webhook_forwarding_consumer.go" ./consumers/

# Copy webhook controllers
mkdir -p controllers/webhook
cp "$SRC/Controllers/Webhook/WebhookForwardingTrigger.go" ./controllers/webhook/
cp "$SRC/Controllers/Webhook/WebhookDeliveryBatcher.go" ./controllers/webhook/
cp "$SRC/Controllers/Webhook/ProjectLookupCache.go" ./controllers/webhook/
cp "$SRC/Controllers/Webhook/WebhookConfigController.go" ./controllers/webhook/

# Copy models
cp "$SRC/Models/Webhook.go" ./models/
cp "$SRC/Models/WebhookDeliveryLog.go" ./models/

# Copy utilities
cp "$SRC/Utils/webhook_signature.go" ./utils/
cp "$SRC/Utils/rabbitmq.go" ./utils/
cp "$SRC/Utils/Mongo.go" ./utils/mongo.go
cp "$SRC/Utils/Redis.go" ./utils/redis.go

echo "Files copied successfully"
