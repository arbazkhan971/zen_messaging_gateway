# ZEN Messaging Gateway - Complete Setup Guide

## 📋 Table of Contents
1. [Prerequisites](#prerequisites)
2. [Local Development Setup](#local-development-setup)
3. [Docker Setup](#docker-setup)
4. [Kubernetes Deployment](#kubernetes-deployment)
5. [Configuration](#configuration)
6. [Testing](#testing)
7. [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Required Infrastructure

| Service | Version | Purpose |
|---------|---------|---------|
| **Go** | 1.21+ | Application runtime |
| **MongoDB** | 4.4+ | Message and webhook storage |
| **Redis** | 6.0+ | Caching and deduplication |
| **RabbitMQ** | 3.9+ | Message queuing |

### RabbitMQ Plugin Required

```bash
# Enable delayed message exchange plugin (REQUIRED for retries)
rabbitmq-plugins enable rabbitmq_delayed_message_exchange
```

---

## Local Development Setup

### Step 1: Clone the Repository

```bash
git clone https://github.com/arbazkhan971/zen_messaging_gateway.git
cd zen_messaging_gateway
```

### Step 2: Configure Keys.json

The `Keys.json` file is already present in the repository (copied from your current setup).

**Verify configuration:**
```bash
cat Keys.json
```

**Expected structure:**
```json
{
  "database_name": "testing",
  "mongo_uri": "mongodb://admin:password@host:27017/?authSource=admin",
  "port": 8080
}
```

**If needed, update:**
```bash
# Edit Keys.json with your credentials
nano Keys.json
```

### Step 3: Install Dependencies

```bash
go mod download
```

### Step 4: Verify Infrastructure is Running

#### Check MongoDB
```bash
# Test MongoDB connection
mongosh "mongodb://admin:password@34.131.69.61:27017/?authSource=admin"
```

#### Check Redis
```bash
# Test Redis connection
redis-cli -h 34.100.179.109 -p 6379 -a YOUR_STRONG_PASSWORD ping
# Expected: PONG
```

#### Check RabbitMQ
```bash
# Test RabbitMQ connection
curl -u admin:StrongPassword123! http://34.66.150.47:15672/api/overview
# Should return RabbitMQ status JSON
```

### Step 5: Run the Application

```bash
# Run directly
go run main.go
```

**Expected output:**
```
🚀 Starting ZEN Messaging Gateway...
Configuration loaded - Database: testing, Port: 8080
Connecting to MongoDB...
Successfully connected to MongoDB database: testing
Initializing message tracker...
[MESSAGE_TRACKER] Initialized unified message tracker
Connecting to Redis...
✅ Successfully connected to Redis.
Initializing RabbitMQ...
[RABBITMQ] ✅ Successfully initialized RabbitMQ
[TOPOLOGY] ✅ All queues and exchanges declared successfully
Starting webhook forwarding consumer...
[WEBHOOK_FORWARDER] ✅ Starting consumer (channel=0x...)
🌐 HTTP server listening on :8080
```

### Step 6: Test the Endpoints

```bash
# Test health endpoint
curl http://localhost:8080/health
# Expected: {"status":"healthy"}

# Test root endpoint
curl http://localhost:8080/
# Expected: {"message":"ZEN Messaging Gateway is running"}
```

### Step 7: Test Webhook Reception

```bash
# Test Datagen webhook
curl -X POST http://localhost:8080/webhook/datagen \
  -H "Content-Type: application/json" \
  -d '{
    "events": {
      "mid": "wamid.HBgLtest123",
      "eventType": "DELIVERY EVENTS"
    },
    "notificationAttributes": {
      "status": "delivered"
    },
    "recipient": {
      "to": "+919876543210",
      "reference": {
        "parent_campaign_id": "test_campaign"
      }
    },
    "sender": {
      "from": "918888888888"
    }
  }'

# Expected: {"status":200,"message":"Datagen webhook received successfully"}
```

### Step 8: Verify Data in MongoDB

```bash
# Connect to MongoDB
mongosh "mongodb://admin:password@34.131.69.61:27017/?authSource=admin"

# Switch to database
use testing

# Check unified message tracking
db.unified_message_tracking.find().pretty()

# Check webhook delivery logs
db.webhook_delivery_logs.find().pretty()

# Check webhook configurations
db.webhooks.find().pretty()
```

---

## Docker Setup

### Step 1: Build Docker Image

```bash
cd zen_messaging_gateway

# Build the image
docker build -t zen-messaging-gateway:latest .
```

### Step 2: Create .env File

```bash
cat > .env << 'EOF'
MONGO_URI=mongodb://admin:password@34.131.69.61:27017/?authSource=admin
DATABASE_NAME=testing
REDIS_URL=redis://:YOUR_STRONG_PASSWORD@34.100.179.109:6379/0
RABBITMQ_URL=amqp://admin:StrongPassword123!@34.66.150.47:5672
SERVER_PORT=8080
EOF
```

### Step 3: Run Container

```bash
# Run with .env file
docker run -d \
  --name zen-messaging-gateway \
  -p 8080:8080 \
  --env-file .env \
  zen-messaging-gateway:latest

# Check logs
docker logs -f zen-messaging-gateway

# Stop container
docker stop zen-messaging-gateway
docker rm zen-messaging-gateway
```

### Step 4: Docker Compose (Optional)

Create `docker-compose.yml`:
```yaml
version: '3.8'

services:
  zen-messaging-gateway:
    build: .
    ports:
      - "8080:8080"
    environment:
      - MONGO_URI=mongodb://admin:password@34.131.69.61:27017/?authSource=admin
      - DATABASE_NAME=testing
      - REDIS_URL=redis://:password@34.100.179.109:6379/0
      - RABBITMQ_URL=amqp://admin:password@34.66.150.47:5672
      - SERVER_PORT=8080
    restart: unless-stopped
```

Run with Docker Compose:
```bash
docker-compose up -d
docker-compose logs -f
```

---

## Kubernetes Deployment

### Step 1: Create Namespace

```bash
kubectl create namespace zen-messaging
```

### Step 2: Create Secrets

```bash
# Create secrets file from example
cp k8s/secrets.yaml.example k8s/secrets.yaml

# Edit with your actual credentials
nano k8s/secrets.yaml
```

**Update secrets.yaml:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: zen-messaging-secrets
  namespace: zen-messaging
type: Opaque
stringData:
  mongo-uri: "mongodb://admin:NewSecurePassword123%21@34.131.69.61:27017/?authSource=admin"
  redis-url: "redis://:YOUR_STRONG_PASSWORD@34.100.179.109:6379/0"
  rabbitmq-url: "amqp://admin:StrongPassword123%21@34.66.150.47:5672"
```

**Apply secrets:**
```bash
kubectl apply -f k8s/secrets.yaml
```

### Step 3: Build and Push Docker Image

```bash
# Tag your image (replace with your registry)
docker build -t gcr.io/YOUR_PROJECT/zen-messaging-gateway:latest .

# Push to registry
docker push gcr.io/YOUR_PROJECT/zen-messaging-gateway:latest
```

### Step 4: Update Deployment Image

Edit `k8s/deployment.yaml`:
```yaml
spec:
  template:
    spec:
      containers:
      - name: zen-messaging-gateway
        image: gcr.io/YOUR_PROJECT/zen-messaging-gateway:latest  # Update this
```

### Step 5: Deploy to Kubernetes

```bash
# Apply all manifests
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml

# Check deployment status
kubectl get pods -n zen-messaging
kubectl get svc -n zen-messaging
kubectl get ingress -n zen-messaging

# View logs
kubectl logs -f deployment/zen-messaging-gateway -n zen-messaging
```

### Step 6: Configure DNS

Point your domain to the ingress IP:

```bash
# Get ingress IP
kubectl get ingress zen-messaging-gateway -n zen-messaging

# Add DNS A record:
# api.zen.serri.in → <INGRESS_IP>
```

### Step 7: Verify SSL Certificate

```bash
# Wait for cert-manager to issue certificate (2-5 minutes)
kubectl get certificate -n zen-messaging

# Test HTTPS
curl https://api.zen.serri.in/health
```

---

## Configuration

### Option 1: Keys.json (Local Development)

The repository already has `Keys.json` configured. Just verify:

```bash
cat Keys.json
```

### Option 2: Environment Variables (Production)

```bash
export MONGO_URI="mongodb://admin:password@host:27017/?authSource=admin"
export DATABASE_NAME="zen_messaging"
export REDIS_URL="redis://:password@host:6379/0"
export RABBITMQ_URL="amqp://user:password@host:5672/"
export SERVER_PORT="8080"
```

### Configuration Priority

```
1. Environment Variables (highest priority)
   ↓ (if not set)
2. Keys.json file
   ↓ (if not found)
3. Hardcoded defaults (fallback)
```

---

## Testing

### 1. Health Check

```bash
curl http://localhost:8080/health
# Expected: {"status":"healthy"}
```

### 2. Test Datagen Webhook

```bash
curl -X POST http://localhost:8080/webhook/datagen \
  -H "Content-Type: application/json" \
  -d '{
    "events": {
      "mid": "wamid.test123",
      "eventType": "DELIVERY EVENTS",
      "timestamp": "2024-01-01T00:00:00Z"
    },
    "notificationAttributes": {
      "status": "delivered"
    },
    "recipient": {
      "to": "+919876543210",
      "reference": {
        "parent_campaign_id": "test_campaign_123"
      }
    },
    "sender": {
      "from": "918888888888"
    }
  }'

# Expected: {"status":200,"message":"Datagen webhook received successfully"}
```

### 3. Test Aisensy Webhook

```bash
curl -X POST http://localhost:8080/webhook/aisensy \
  -H "Content-Type: application/json" \
  -d '{
    "topic": "message.status.updated",
    "id": "event_123",
    "data": {
      "message_id": "msg_456",
      "phone_number": "+919876543210",
      "status": "delivered"
    }
  }'

# Expected: {"status":200,"message":"Aisensy webhook received successfully"}
```

### 4. Verify in MongoDB

```bash
mongosh "mongodb://admin:password@34.131.69.61:27017/?authSource=admin"

use testing

# Check if messages are being stored
db.unified_message_tracking.find().limit(5).pretty()

# Check webhook delivery logs
db.webhook_delivery_logs.find().limit(5).pretty()
```

### 5. Check RabbitMQ Queues

```bash
# Via RabbitMQ Management UI
open http://34.66.150.47:15672
# Login: admin / StrongPassword123!

# Or via CLI
rabbitmqadmin -H 34.66.150.47 -u admin -p StrongPassword123! list queues

# Expected queues:
# - datagen_webhook_queue
# - aisensy_webhook_queue
# - karix_webhook_queue
# - webhook_forwarding_queue
```

---

## Troubleshooting

### Issue 1: "Failed to connect to MongoDB"

**Solution:**
```bash
# Verify MongoDB is accessible
mongosh "mongodb://admin:password@34.131.69.61:27017/?authSource=admin"

# Check if password has special characters (encode them)
# ! becomes %21
# @ becomes %40
# Example: "Pass!word@123" → "Pass%21word%40123"
```

### Issue 2: "Failed to connect to RabbitMQ"

**Solution:**
```bash
# Check RabbitMQ is running
curl -u admin:StrongPassword123! http://34.66.150.47:15672/api/overview

# Verify plugin is enabled
rabbitmq-plugins list | grep delayed

# If not enabled:
rabbitmq-plugins enable rabbitmq_delayed_message_exchange
```

### Issue 3: "Redis connection lost"

**Solution:**
```bash
# Test Redis
redis-cli -h 34.100.179.109 -p 6379 -a YOUR_PASSWORD ping

# The app will continue with degraded mode (no caching)
# Redis failures are non-fatal
```

### Issue 4: Port already in use

**Solution:**
```bash
# Change port in Keys.json
{
  "port": 8081  # Use different port
}

# Or set environment variable
export SERVER_PORT=8081
```

### Issue 5: Webhooks not being stored

**Check logs:**
```bash
# Application logs will show:
# [DATAGEN_WEBHOOK] Processing webhook...
# [MESSAGE_TRACKER] Created message: id=..., bsp=datagen

# If you don't see these, check:
# 1. Is webhook payload valid JSON?
# 2. Is MongoDB connection working?
# 3. Are there any panic/error logs?
```

---

## Quick Start Commands

### Absolute Quickest Setup (Using existing Keys.json)

```bash
# 1. Clone
git clone https://github.com/arbazkhan971/zen_messaging_gateway.git
cd zen_messaging_gateway

# 2. Build
go build -o zen_messaging_gateway .

# 3. Run (Keys.json already configured)
./zen_messaging_gateway
```

That's it! The server will start on port 8080 using the existing Keys.json configuration.

### Alternative: Using Docker

```bash
# 1. Clone
git clone https://github.com/arbazkhan971/zen_messaging_gateway.git
cd zen_messaging_gateway

# 2. Build Docker image
docker build -t zen-messaging-gateway .

# 3. Run (using Keys.json inside container)
docker run -d -p 8080:8080 zen-messaging-gateway
```

### Production: Kubernetes

```bash
# 1. Update secrets
nano k8s/secrets.yaml

# 2. Update deployment image
nano k8s/deployment.yaml  # Change image to your registry

# 3. Build and push
docker build -t gcr.io/YOUR_PROJECT/zen-messaging-gateway:latest .
docker push gcr.io/YOUR_PROJECT/zen-messaging-gateway:latest

# 4. Deploy
kubectl apply -f k8s/secrets.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml

# 5. Check status
kubectl get pods -n zen-messaging
kubectl logs -f deployment/zen-messaging-gateway -n zen-messaging
```

---

## Configuration Details

### Current Infrastructure (from Keys.json)

```
MongoDB:   34.131.69.61:27017
Redis:     34.100.179.109:6379
RabbitMQ:  34.66.150.47:5672
Database:  testing
```

### Webhook Endpoints Available

Once deployed to `api.zen.serri.in`:

```
POST https://api.zen.serri.in/webhook/datagen
POST https://api.zen.serri.in/webhook/aisensy
POST https://api.zen.serri.in/webhook/karix

# Backward compatibility
POST https://api.zen.serri.in/datagen-webhook
POST https://api.zen.serri.in/partner-webhook
```

### Configure BSP to Send Webhooks

**For Datagen:**
```
Webhook URL: https://api.zen.serri.in/webhook/datagen
```

**For Aisensy:**
```
Webhook URL: https://api.zen.serri.in/webhook/aisensy
```

**For Karix:**
```
Webhook URL: https://api.zen.serri.in/webhook/karix
```

---

## Monitoring

### View Application Logs

**Local:**
```bash
# Logs are output to stdout
./zen_messaging_gateway
```

**Docker:**
```bash
docker logs -f zen-messaging-gateway
```

**Kubernetes:**
```bash
kubectl logs -f deployment/zen-messaging-gateway -n zen-messaging
```

### Monitor RabbitMQ

```bash
# Access RabbitMQ Management UI
open http://34.66.150.47:15672
# Login: admin / StrongPassword123!

# Check queue depths
# - datagen_webhook_queue should be processed quickly
# - webhook_forwarding_queue should have active consumers
```

### Monitor MongoDB

```bash
mongosh "mongodb://admin:password@34.131.69.61:27017/?authSource=admin"

use testing

# Count messages
db.unified_message_tracking.countDocuments()

# Recent messages
db.unified_message_tracking.find().sort({created_at: -1}).limit(10)

# Stats by status
db.unified_message_tracking.aggregate([
  {$group: {_id: "$current_status", count: {$sum: 1}}}
])
```

### Monitor Redis

```bash
redis-cli -h 34.100.179.109 -a YOUR_PASSWORD

# Check memory usage
INFO memory

# Check keys
DBSIZE

# Monitor real-time
MONITOR
```

---

## Development Workflow

### Making Changes

```bash
# 1. Make code changes
nano handlers/webhook/datagen.go

# 2. Build
go build -o zen_messaging_gateway .

# 3. Test locally
./zen_messaging_gateway

# 4. Commit and push
git add .
git commit -m "Your changes"
git push origin main

# 5. Deploy to Kubernetes
docker build -t gcr.io/YOUR_PROJECT/zen-messaging-gateway:v1.1 .
docker push gcr.io/YOUR_PROJECT/zen-messaging-gateway:v1.1
kubectl set image deployment/zen-messaging-gateway \
  zen-messaging-gateway=gcr.io/YOUR_PROJECT/zen-messaging-gateway:v1.1 \
  -n zen-messaging
```

---

## Performance Tuning

### Increase Worker Pool

Edit `consumers/webhook_forwarding_consumer.go`:
```go
WorkerPoolSize = 200  // Increase from 100
```

### Increase Goroutine Semaphore

Edit `handlers/webhook/base.go`:
```go
maxConcurrentGoroutines = numCPU * 200  // Increase from 100
```

### Increase Kubernetes Resources

Edit `k8s/deployment.yaml`:
```yaml
resources:
  requests:
    memory: "1Gi"    # Increase from 512Mi
    cpu: "1000m"     # Increase from 500m
  limits:
    memory: "4Gi"    # Increase from 2Gi
    cpu: "4000m"     # Increase from 2000m
```

---

## Security Checklist

✅ **Keys.json is in .gitignore** (secrets not committed)
✅ **HMAC signatures** on forwarded webhooks
✅ **HTTPS** enforced via ingress
✅ **MongoDB authentication** enabled
✅ **Redis password** protected
✅ **RabbitMQ authentication** enabled

### Rotate Secrets

```bash
# Generate new MongoDB password
# Generate new Redis password
# Generate new RabbitMQ password

# Update in Kubernetes secrets
kubectl edit secret zen-messaging-secrets -n zen-messaging

# Restart pods to pick up new secrets
kubectl rollout restart deployment/zen-messaging-gateway -n zen-messaging
```

---

## Next Steps

1. ✅ **Setup complete** - Application is running
2. 🔄 **Configure BSPs** - Point Datagen/Aisensy webhooks to your endpoints
3. 📊 **Monitor metrics** - Watch RabbitMQ queues and MongoDB
4. 🚀 **Scale up** - Increase replicas as needed
5. 📈 **Add analytics** - Build dashboards on top of unified_message_tracking

---

## Support

**Repository**: https://github.com/arbazkhan971/zen_messaging_gateway

**Documentation:**
- [README.md](README.md) - Overview and features
- [FLOW_DOCUMENTATION.md](FLOW_DOCUMENTATION.md) - Complete request flow
- [WEBHOOK_ROUTING_DESIGN.md](WEBHOOK_ROUTING_DESIGN.md) - Architecture design
- [DEPLOYMENT_STATUS.md](DEPLOYMENT_STATUS.md) - Build verification

**For issues:** Create an issue on GitHub
EOF
cat SETUP_GUIDE.md