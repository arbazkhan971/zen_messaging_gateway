#!/bin/bash

echo "🔍 Verifying RabbitMQ Queues..."
echo ""

RABBITMQ_HOST="34.66.150.47"
RABBITMQ_USER="admin"
RABBITMQ_PASS="StrongPassword123!"

# Expected queues
EXPECTED_QUEUES=(
  "datagen_webhook_queue"
  "aisensy_webhook_queue"
  "karix_webhook_queue"
  "webhook_forwarding_queue"
  "webhook_forwarding_dlq"
)

echo "Expected BSP Queues:"
for queue in "${EXPECTED_QUEUES[@]}"; do
  echo "  - $queue"
done
echo ""

# Check each queue
echo "Checking RabbitMQ..."
for queue in "${EXPECTED_QUEUES[@]}"; do
  result=$(curl -s -u $RABBITMQ_USER:$RABBITMQ_PASS \
    "http://$RABBITMQ_HOST:15672/api/queues/%2F/$queue" 2>&1)
  
  if echo "$result" | grep -q '"name"'; then
    messages=$(echo "$result" | grep -o '"messages":[0-9]*' | cut -d':' -f2)
    echo "✅ $queue (messages: $messages)"
  else
    echo "❌ $queue - NOT FOUND"
  fi
done

echo ""
echo "✅ Verification complete!"
