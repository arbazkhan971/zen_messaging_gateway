package webhook

import (
	"encoding/json"
	"io"
	"log"

	"zen_messaging_gateway/utils"

	"github.com/gin-gonic/gin"
)

// KarixWebhookPayload represents the Karix direct API webhook structure
type KarixWebhookPayload struct {
	MessageID   string                 `json:"message_id"`
	Status      string                 `json:"status"`
	PhoneNumber string                 `json:"phone_number"`
	Timestamp   string                 `json:"timestamp"`
	EventType   string                 `json:"event_type"`
	Data        map[string]interface{} `json:"data"`
}

// KarixWebhookHandler handles webhooks from Karix direct integration
// URL: POST /webhook/karix
// Note: This is different from Datagen (which also uses Karix underneath)
func KarixWebhookHandler(c *gin.Context) {
	// Read the body content
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("[KARIX_WEBHOOK] Failed to read request body: %v", err)
		c.JSON(400, WebhookResponse{
			Status:  400,
			Message: "Failed to read webhook body",
		})
		return
	}

	// Log reception
	LogWebhookReceived("KARIX", len(bodyBytes))

	// CRITICAL: Return 200 immediately to prevent Karix retries
	RespondImmediately(c, "Karix")

	// Process webhook asynchronously
	ProcessWebhookAsync("KARIX", func() {
		processKarixWebhook(bodyBytes)
	})
}

func processKarixWebhook(bodyBytes []byte) {
	// Parse Karix webhook payload
	var payload KarixWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		log.Printf("[KARIX_WEBHOOK] Failed to parse webhook: %v", err)
		return
	}

	log.Printf("[KARIX_WEBHOOK] Processing webhook - MessageID: %s, Status: %s, EventType: %s",
		payload.MessageID, payload.Status, payload.EventType)

	// Route based on event type
	switch payload.EventType {
	case "message_sent":
		handleKarixMessageSent(payload)
	case "message_delivered":
		handleKarixMessageDelivered(payload)
	case "message_read":
		handleKarixMessageRead(payload)
	case "message_failed":
		handleKarixMessageFailed(payload)
	default:
		log.Printf("[KARIX_WEBHOOK] Unknown event type: %s", payload.EventType)
		storeUnhandledKarixWebhook(payload)
	}

	// Trigger webhook forwarding
	triggerKarixWebhookForwarding(payload)
}

func handleKarixMessageSent(payload KarixWebhookPayload) {
	log.Printf("[KARIX_WEBHOOK] Message sent - ID: %s, Phone: %s",
		payload.MessageID, payload.PhoneNumber)

	// Normalize status
	normalizedPayload := normalizeKarixPayload(payload, "sent")

	// Publish to RabbitMQ
	if err := publishKarixWebhook("message_sent", normalizedPayload); err != nil {
		log.Printf("[KARIX_WEBHOOK] Failed to publish message sent: %v", err)
	}
}

func handleKarixMessageDelivered(payload KarixWebhookPayload) {
	log.Printf("[KARIX_WEBHOOK] Message delivered - ID: %s, Phone: %s",
		payload.MessageID, payload.PhoneNumber)

	// Normalize status
	normalizedPayload := normalizeKarixPayload(payload, "delivered")

	// Publish to RabbitMQ
	if err := publishKarixWebhook("message_delivered", normalizedPayload); err != nil {
		log.Printf("[KARIX_WEBHOOK] Failed to publish message delivered: %v", err)
	}
}

func handleKarixMessageRead(payload KarixWebhookPayload) {
	log.Printf("[KARIX_WEBHOOK] Message read - ID: %s, Phone: %s",
		payload.MessageID, payload.PhoneNumber)

	// Normalize status
	normalizedPayload := normalizeKarixPayload(payload, "read")

	// Publish to RabbitMQ
	if err := publishKarixWebhook("message_read", normalizedPayload); err != nil {
		log.Printf("[KARIX_WEBHOOK] Failed to publish message read: %v", err)
	}
}

func handleKarixMessageFailed(payload KarixWebhookPayload) {
	log.Printf("[KARIX_WEBHOOK] Message failed - ID: %s, Phone: %s",
		payload.MessageID, payload.PhoneNumber)

	// Normalize status
	normalizedPayload := normalizeKarixPayload(payload, "failed")

	// Publish to RabbitMQ
	if err := publishKarixWebhook("message_failed", normalizedPayload); err != nil {
		log.Printf("[KARIX_WEBHOOK] Failed to publish message failed: %v", err)
	}
}

func normalizeKarixPayload(payload KarixWebhookPayload, normalizedStatus string) map[string]interface{} {
	return map[string]interface{}{
		"message_id":   payload.MessageID,
		"status":       normalizedStatus,
		"phone_number": payload.PhoneNumber,
		"timestamp":    payload.Timestamp,
		"event_type":   payload.EventType,
		"bsp":          "karix",
		"raw_data":     payload.Data,
	}
}

func storeUnhandledKarixWebhook(payload KarixWebhookPayload) {
	log.Printf("[KARIX_WEBHOOK] Storing unhandled webhook: %+v", payload)
	// TODO: Implement storage in MongoDB
}

func publishKarixWebhook(eventType string, payload map[string]interface{}) error {
	// All Karix webhooks have normal priority (campaign messages)
	priority := uint8(0)

	// Publish to karix_webhook_queue
	return utils.PublishToQueue(
		"karix_webhook_queue",
		payload,
		priority,
	)
}

func triggerKarixWebhookForwarding(payload KarixWebhookPayload) {
	// Extract project_owner_id from payload
	projectOwnerID := extractProjectOwnerIDFromKarix(payload)
	if projectOwnerID == "" {
		log.Printf("[KARIX_WEBHOOK] No project_owner_id found, skipping forwarding")
		return
	}

	// Determine event type for webhook forwarding
	eventType := "karix." + payload.EventType

	log.Printf("[KARIX_WEBHOOK] Triggering webhook forwarding for project_owner=%s, event=%s",
		projectOwnerID, eventType)

	// TODO: Implement webhook config lookup and forwarding
	// This will be handled by the existing webhook forwarding system
}

func extractProjectOwnerIDFromKarix(payload KarixWebhookPayload) string {
	// Extract business number from payload data
	if businessNumber, ok := payload.Data["business_number"].(string); ok && businessNumber != "" {
		// TODO: Lookup project_owner_id from business number using cache
		return businessNumber
	}

	// Fallback to phone number
	if payload.PhoneNumber != "" {
		return payload.PhoneNumber
	}

	return ""
}
