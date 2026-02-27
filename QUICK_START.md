# Quick Start - 3 Steps to Run

## 🚀 Fastest Way to Get Started

### Prerequisites
- Go 1.21+ installed
- MongoDB, Redis, RabbitMQ accessible (already configured in Keys.json)

### Step 1: Clone
```bash
git clone https://github.com/arbazkhan971/zen_messaging_gateway.git
cd zen_messaging_gateway
```

### Step 2: Build
```bash
go build -o zen_messaging_gateway .
```

### Step 3: Run
```bash
./zen_messaging_gateway
```

**Done!** Server runs on http://localhost:8080

---

## 🧪 Test It Works

```bash
# Health check
curl http://localhost:8080/health

# Test webhook
curl -X POST http://localhost:8080/webhook/datagen \
  -H "Content-Type: application/json" \
  -d '{"events":{"mid":"wamid.test"},"notificationAttributes":{"status":"sent"}}'
```

---

## 📍 Webhook Endpoints

| Endpoint | BSP | Purpose |
|----------|-----|---------|
| `POST /webhook/datagen` | Datagen | Main webhook endpoint |
| `POST /webhook/aisensy` | Aisensy | Partner webhooks |
| `POST /webhook/karix` | Karix | Direct API webhooks |

---

## 📦 What Happens

```
Webhook received → Return 200 OK → Background processing:
                                   ├─ Store in MongoDB
                                   ├─ Publish to RabbitMQ
                                   └─ Forward to client
```

---

## 🔧 Already Configured

✅ MongoDB: `mongodb://34.131.69.61:27017`  
✅ Redis: `redis://34.100.179.109:6379`  
✅ RabbitMQ: `amqp://34.66.150.47:5672`  
✅ Database: `testing`  
✅ Port: `8080`  

**All configured in Keys.json - no changes needed!**

---

## 📚 Full Documentation

- [SETUP_GUIDE.md](SETUP_GUIDE.md) - Complete setup instructions
- [FLOW_DOCUMENTATION.md](FLOW_DOCUMENTATION.md) - How requests flow
- [README.md](README.md) - Full project documentation

---

**That's it! 3 commands and you're running.** 🎉
