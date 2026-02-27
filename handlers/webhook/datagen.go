package webhook

import (
	"encoding/json"
	"io"
	"log"

	"zen_messaging_gateway/utils"

	"github.com/gin-gonic/gin"
)

// DatagenWebhookHandler handles webhooks from Datagen/Karix BSP
// URL: POST /webhook/datagen
func DatagenWebhookHandler(c *gin.Context) {
	// Read the body content to determine format and event type
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("[DATAGEN_WEBHOOK] Failed to read request body: %v", err)
		c.JSON(400, WebhookResponse{
			Status:  400,
			Message: "Failed to read webhook body",
		})
		return
	}

	// Log reception
	LogWebhookReceived("DATAGEN", len(bodyBytes))

	// CRITICAL: Return 200 immediately to prevent Datagen retries
	RespondImmediately(c, "Datagen")

	// Process webhook asynchronously
	ProcessWebhookAsync("DATAGEN", func() {
		processDatagenWebhook(bodyBytes)
	})
}

func processDatagenWebhook(bodyBytes []byte) {
	// Determine webhook type
	var webhookType string
	var payload map[string]interface{}

	if len(bodyBytes) > 0 && bodyBytes[0] == '[' {
		// Batch webhook (array of webhooks)
		webhookType = "batch"
		var batchPayload []map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &batchPayload); err != nil {
			log.Printf("[DATAGEN_WEBHOOK] Failed to parse batch webhook: %v", err)
			return
		}
		payload = map[string]interface{}{
			"batch_data": batchPayload,
			"batch_size": len(batchPayload),
		}
		log.Printf("[DATAGEN_WEBHOOK] Processing batch webhook with %d items", len(batchPayload))
	} else {
		// Single webhook - determine subtype
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			log.Printf("[DATAGEN_WEBHOOK] Failed to parse webhook: %v", err)
			return
		}

		// Check for delivery event
		if events, ok := payload["events"].(map[string]interface{}); ok {
			if eventType, ok := events["eventType"].(string); ok && eventType == "DELIVERY EVENTS" {
				webhookType = "delivery"
			}
		}

		// Check for user message
		if eventContent, ok := payload["eventContent"].(map[string]interface{}); ok {
			if message, ok := eventContent["message"].(map[string]interface{}); ok {
				if messageType, ok := message["type"].(string); ok && messageType == "text" {
					webhookType = "user_message"
				}
			}
		}

		// Default to unknown if not identified
		if webhookType == "" {
			webhookType = "unknown"
			log.Printf("[DATAGEN_WEBHOOK] Unknown webhook type, payload: %s", string(bodyBytes))
		}
	}

	// Step 1: Publish to RabbitMQ for processing
	if err := publishDatagenWebhook(webhookType, payload); err != nil {
		log.Printf("[DATAGEN_WEBHOOK] Failed to publish webhook: %v", err)
		// TODO: Add to fallback queue
		return
	}

	// Step 2: Trigger webhook forwarding (if client has webhook configured)
	triggerDatagenWebhookForwarding(webhookType, payload)
}

func publishDatagenWebhook(webhookType string, payload map[string]interface{}) error {
	// Determine priority based on webhook type
	priority := uint8(0) // Default: campaign messages
	if webhookType == "user_message" {
		priority = 5 // High priority for user-initiated messages
	}

	// Publish to datagen_webhook_queue
	return utils.PublishToQueue(
		"datagen_webhook_queue",
		payload,
		priority,
	)
}

func triggerDatagenWebhookForwarding(webhookType string, payload map[string]interface{}) {
	// Extract project_owner_id from payload
	projectOwnerID := extractProjectOwnerIDFromDatagen(payload)
	if projectOwnerID == "" {
		log.Printf("[DATAGEN_WEBHOOK] No project_owner_id found, skipping forwarding")
		return
	}

	// Determine event type for webhook forwarding
	eventType := "datagen." + webhookType

	// Check if webhook configuration exists (this will use cache)
	// Then enqueue to webhook_forwarding_queue
	log.Printf("[DATAGEN_WEBHOOK] Triggering webhook forwarding for project_owner=%s, event=%s",
		projectOwnerID, eventType)

	// TODO: Implement webhook config lookup and forwarding
	// This will be handled by the existing webhook forwarding system
}

func extractProjectOwnerIDFromDatagen(payload map[string]interface{}) string {
	var businessNumber string

	// For user messages: extract from eventContent.message.to (business number)
	if eventContent, ok := payload["eventContent"].(map[string]interface{}); ok {
		if message, ok := eventContent["message"].(map[string]interface{}); ok {
			if to, ok := message["to"].(string); ok && to != "" {
				businessNumber = to
			}
		}
	}

	// For delivery events: extract from sender.from (business number)
	if businessNumber == "" {
		if sender, ok := payload["sender"].(map[string]interface{}); ok {
			if from, ok := sender["from"].(string); ok && from != "" {
				businessNumber = from
			}
		}
	}

	if businessNumber == "" {
		return ""
	}

	// TODO: Lookup project_owner_id from business number using cache
	// For now, return the business number (will be implemented with cache)
	return businessNumber
}
