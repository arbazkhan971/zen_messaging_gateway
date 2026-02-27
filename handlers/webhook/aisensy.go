package webhook

import (
	"encoding/json"
	"io"
	"log"

	"zen_messaging_gateway/utils"

	"github.com/gin-gonic/gin"
)

// AisensyWebhookPayload represents the Aisensy webhook structure
type AisensyWebhookPayload struct {
	Topic   string                 `json:"topic"`
	Data    map[string]interface{} `json:"data"`
	EventID string                 `json:"id"`
}

// AisensyWebhookHandler handles webhooks from Aisensy BSP (partner account)
// URL: POST /webhook/aisensy
func AisensyWebhookHandler(c *gin.Context) {
	// Read the body content
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("[AISENSY_WEBHOOK] Failed to read request body: %v", err)
		c.JSON(400, WebhookResponse{
			Status:  400,
			Message: "Failed to read webhook body",
		})
		return
	}

	// Log reception
	LogWebhookReceived("AISENSY", len(bodyBytes))

	// CRITICAL: Return 200 immediately to prevent Aisensy retries
	RespondImmediately(c, "Aisensy")

	// Process webhook asynchronously
	ProcessWebhookAsync("AISENSY", func() {
		processAisensyWebhook(bodyBytes)
	})
}

func processAisensyWebhook(bodyBytes []byte) {
	// Parse Aisensy webhook payload
	var payload AisensyWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		log.Printf("[AISENSY_WEBHOOK] Failed to parse webhook: %v", err)
		return
	}

	log.Printf("[AISENSY_WEBHOOK] Processing webhook - Topic: %s, EventID: %s",
		payload.Topic, payload.EventID)

	// Route based on topic
	switch payload.Topic {
	case "message.status.updated":
		handleAisensyMessageStatus(payload)
	case "message.created":
		handleAisensyMessageCreated(payload)
	case "contact.created":
		handleAisensyContactCreated(payload)
	case "contact.attribute.updated":
		handleAisensyContactAttributeUpdate(payload)
	case "wa_template.status.updated":
		handleAisensyTemplateStatusUpdate(payload)
	default:
		log.Printf("[AISENSY_WEBHOOK] Unhandled topic: %s", payload.Topic)
		// Store for future processing
		storeUnhandledAisensyWebhook(payload)
	}

	// Trigger webhook forwarding
	triggerAisensyWebhookForwarding(payload)
}

func handleAisensyMessageStatus(payload AisensyWebhookPayload) {
	// Extract message details
	messageID, _ := payload.Data["message_id"].(string)
	status, _ := payload.Data["status"].(string)
	phoneNumber, _ := payload.Data["phone_number"].(string)

	log.Printf("[AISENSY_WEBHOOK] Message status update - ID: %s, Status: %s, Phone: %s",
		messageID, status, phoneNumber)

	// Publish to RabbitMQ for processing
	if err := publishAisensyWebhook("message_status", payload); err != nil {
		log.Printf("[AISENSY_WEBHOOK] Failed to publish message status: %v", err)
	}
}

func handleAisensyMessageCreated(payload AisensyWebhookPayload) {
	messageID, _ := payload.Data["message_id"].(string)
	log.Printf("[AISENSY_WEBHOOK] Message created - ID: %s", messageID)

	// Publish to RabbitMQ
	if err := publishAisensyWebhook("message_created", payload); err != nil {
		log.Printf("[AISENSY_WEBHOOK] Failed to publish message created: %v", err)
	}
}

func handleAisensyContactCreated(payload AisensyWebhookPayload) {
	phoneNumber, _ := payload.Data["phone_number"].(string)
	log.Printf("[AISENSY_WEBHOOK] Contact created - Phone: %s", phoneNumber)

	// Publish to RabbitMQ
	if err := publishAisensyWebhook("contact_created", payload); err != nil {
		log.Printf("[AISENSY_WEBHOOK] Failed to publish contact created: %v", err)
	}
}

func handleAisensyContactAttributeUpdate(payload AisensyWebhookPayload) {
	phoneNumber, _ := payload.Data["phone_number"].(string)
	log.Printf("[AISENSY_WEBHOOK] Contact attribute updated - Phone: %s", phoneNumber)

	// Publish to RabbitMQ
	if err := publishAisensyWebhook("contact_attribute", payload); err != nil {
		log.Printf("[AISENSY_WEBHOOK] Failed to publish contact attribute: %v", err)
	}
}

func handleAisensyTemplateStatusUpdate(payload AisensyWebhookPayload) {
	templateID, _ := payload.Data["template_id"].(string)
	status, _ := payload.Data["status"].(string)
	log.Printf("[AISENSY_WEBHOOK] Template status update - ID: %s, Status: %s",
		templateID, status)

	// Publish to RabbitMQ
	if err := publishAisensyWebhook("template_status", payload); err != nil {
		log.Printf("[AISENSY_WEBHOOK] Failed to publish template status: %v", err)
	}
}

func storeUnhandledAisensyWebhook(payload AisensyWebhookPayload) {
	// Store in a separate collection for analysis
	log.Printf("[AISENSY_WEBHOOK] Storing unhandled webhook: %+v", payload)
	// TODO: Implement storage in MongoDB
}

func publishAisensyWebhook(eventType string, payload AisensyWebhookPayload) error {
	// All Aisensy webhooks have normal priority (campaign messages)
	priority := uint8(0)

	// Publish to aisensy_webhook_queue
	return utils.PublishToQueue(
		"aisensy_webhook_queue",
		payload,
		priority,
	)
}

func triggerAisensyWebhookForwarding(payload AisensyWebhookPayload) {
	// Extract project_owner_id from payload
	projectOwnerID := extractProjectOwnerIDFromAisensy(payload)
	if projectOwnerID == "" {
		log.Printf("[AISENSY_WEBHOOK] No project_owner_id found, skipping forwarding")
		return
	}

	// Determine event type for webhook forwarding
	eventType := "aisensy." + payload.Topic

	log.Printf("[AISENSY_WEBHOOK] Triggering webhook forwarding for project_owner=%s, event=%s",
		projectOwnerID, eventType)

	// TODO: Implement webhook config lookup and forwarding
	// This will be handled by the existing webhook forwarding system
}

func extractProjectOwnerIDFromAisensy(payload AisensyWebhookPayload) string {
	// Try to extract phone number or business identifier
	phoneNumber, _ := payload.Data["phone_number"].(string)
	if phoneNumber == "" {
		phoneNumber, _ = payload.Data["business_phone"].(string)
	}

	if phoneNumber == "" {
		return ""
	}

	// TODO: Lookup project_owner_id from phone number using cache
	// For now, return the phone number (will be implemented with cache)
	return phoneNumber
}
