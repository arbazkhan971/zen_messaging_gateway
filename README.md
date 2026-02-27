# ZEN Messaging Gateway

A high-performance webhook forwarding and message tracking system for the ZEN platform.

## Overview

The ZEN Messaging Gateway provides:

- **Unified API Gateway**: Single entry point (`api.zen.serri.in`) for all messaging operations across multiple BSPs (Business Service Providers)
- **Webhook Processing**: High-throughput webhook consumer that captures, standardizes, and forwards webhooks
- **Message Tracking**: Tracks WhatsApp message IDs (wamid) and status updates (sent, delivered, read, failed)
- **Fan-out Support**: Forward webhooks to multiple destinations (1:N mapping)
- **Reliability**: Circuit breakers, retries, DLQ (Dead Letter Queue) support
- **Scalability**: Handles 3x ingress on webhook processing compared to API calls

## Architecture

```
Client/User
    |
    | 1. Send Message
    v
ZEN API Gateway
    |
    | 2. Route to BSP (Aisensy, Datagen, etc.)
    v
BSP Router Layer
    |
    | 3. Deliver to WhatsApp/Meta
    v
WhatsApp/Meta API
    |
    | 4. Return wamid
    v
ZEN API Gateway
    |
    | 5. Track wamid
    | 6. Log usage
    v
Tracking Database (Redis/MongoDB)
    |
    | 7. Webhook callbacks (status updates)
    v
Webhook Processor/Capturer
    |
    | 8. Standardize & Store
    | 9. Fan-out (1:N)
    v
[Client Webhook URL | Analytics | Historical Logs]
```

## Features

### Webhook Forwarding Consumer
- **Worker Pool**: 100 concurrent workers for HTTP delivery
- **Circuit Breaker**: Per-URL circuit breakers to prevent cascade failures
- **Retry Logic**: 3 attempts with configurable delays (default: 3s each)
- **Async DB Writes**: Non-blocking database updates for high throughput
- **Prefetch**: 1000 messages for optimal RabbitMQ performance

### Message Tracking
- Tracks wamid (WhatsApp Message ID) for each sent message
- Records status transitions: sent → delivered → read / failed
- Provides historical conversation data
- Usage analytics per user/project

### Scalability
- RabbitMQ-based queue system
- Connection pooling for MongoDB and HTTP clients
- Dedicated RabbitMQ connection for webhook forwarder (isolated from other consumers)
- Lazy queues for handling large backlogs
- Priority queue support

## Installation

### Prerequisites

- Go 1.21 or higher
- MongoDB
- Redis
- RabbitMQ with `rabbitmq_delayed_message_exchange` plugin

### Local Development

1. Clone the repository:
```bash
git clone https://github.com/your-org/zen_messaging_gateway.git
cd zen_messaging_gateway
```

2. Install dependencies:
```bash
go mod download
```

3. Set environment variables:
```bash
export MONGO_URI="mongodb://localhost:27017"
export DATABASE_NAME="zen_messaging"
export REDIS_URL="redis://localhost:6379/0"
export RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
export SERVER_PORT="8080"
```

4. Run the application:
```bash
go run main.go
```

### Docker

Build and run with Docker:

```bash
docker build -t zen-messaging-gateway .
docker run -p 8080:8080 \
  -e MONGO_URI="mongodb://..." \
  -e REDIS_URL="redis://..." \
  -e RABBITMQ_URL="amqp://..." \
  zen-messaging-gateway
```

### Kubernetes

Deploy to Kubernetes:

```bash
# Create secrets
kubectl apply -f k8s/secrets.yaml

# Deploy application
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MONGO_URI` | MongoDB connection string | Required |
| `DATABASE_NAME` | MongoDB database name | `zen_messaging` |
| `REDIS_URL` | Redis connection string | Required |
| `RABBITMQ_URL` | RabbitMQ connection string | Required |
| `SERVER_PORT` | HTTP server port | `8080` |

### RabbitMQ Queues

- **webhook_forwarding_queue**: Main queue for webhook delivery
- **webhook_forwarding_dlq**: Dead letter queue for failed webhooks
- **webhook_forwarding_delayed_exchange**: Delayed exchange for retries

## API Endpoints

### Health Check
```
GET /health
GET /
```

Returns `200 OK` with `{"status": "healthy"}`

### BSP Webhook Endpoints

Each BSP has a dedicated webhook endpoint for isolation and custom processing:

#### Datagen Webhook
```
POST /webhook/datagen
```
Handles webhooks from Datagen (uses Karix underneath). Supports:
- Delivery events (`DELIVERY EVENTS`)
- User-initiated messages
- Batch webhooks (array of events)

**Legacy endpoint**: `POST /datagen-webhook` (redirects to new endpoint)

#### Aisensy Webhook
```
POST /webhook/aisensy
```
Handles webhooks from Aisensy partner account. Topics:
- `message.status.updated`
- `message.created`
- `contact.created`
- `contact.attribute.updated`
- `wa_template.status.updated`

**Legacy endpoint**: `POST /partner-webhook` (redirects to new endpoint)

#### Karix Direct Webhook
```
POST /webhook/karix
```
Handles webhooks from direct Karix API integration (if not using Datagen).

### Webhook Configuration Management
```
POST /api/v1/webhooks/configure
GET /api/v1/webhooks/config/:project_owner_id
DELETE /api/v1/webhooks/config/:project_owner_id
GET /api/v1/webhooks/logs/:project_owner_id
GET /api/v1/webhooks/stats/:project_owner_id
```

## Performance

### Throughput
- **API Ingress**: ~1000 messages/sec
- **Webhook Ingress**: ~3000 webhooks/sec (3x API ingress)
- **Webhook Delivery**: 100 concurrent deliveries with circuit breakers

### Latency
- **Webhook Delivery**: <15s (with 3 retries)
- **Database Writes**: Async, non-blocking

## Monitoring

### Metrics
- Webhook delivery success rate
- Circuit breaker status per URL
- Queue depth (RabbitMQ)
- Consumer lag
- Database connection pool status

### Logging
- Structured logging with timestamps
- Per-webhook delivery tracking
- Error tracking with context

## Development

### Project Structure

```
zen_messaging_gateway/
├── config/             # Configuration management
├── consumers/          # RabbitMQ consumers
├── controllers/        # HTTP controllers
│   └── webhook/       # Webhook-related controllers
├── models/            # Data models
├── routers/           # HTTP route definitions
│   └── webhook/       # Webhook routes
├── utils/             # Utility functions
│   ├── mongo.go       # MongoDB client
│   ├── redis.go       # Redis client
│   ├── rabbitmq.go    # RabbitMQ management
│   └── webhook_signature.go  # HMAC signature generation
├── k8s/               # Kubernetes manifests
├── main.go            # Application entry point
├── Dockerfile         # Docker build configuration
└── README.md          # This file
```

### Code Style

- Follow Go standard conventions
- Use snake_case for package names
- Use camelCase for Go identifiers
- Add comments for exported functions

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

[Add your license here]

## Support

For issues and questions, please create an issue in the GitHub repository.
