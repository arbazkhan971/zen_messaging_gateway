package webhook

import (
	"context"
	"log"
	"time"

	utils "zen_messaging_gateway/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TriggerWebhookForwarding checks if a project owner has webhook configured and triggers forwarding
// This should be called after processing DataGen delivery events or any other events (contact updates, etc.)
// Uses batched delivery log writer to optimize database writes under high load
func TriggerWebhookForwarding(projectOwnerID string, eventType string, payload map[string]interface{}) {
	if projectOwnerID == "" {
		log.Printf("[WEBHOOK_TRIGGER] Skipping webhook forwarding: empty project_owner_id")
		return
	}

	// Get webhook configuration using multi-layer cache (memory -> Redis -> MongoDB)
	webhook, err := GetWebhookByProjectOwnerID(projectOwnerID)
	if err != nil {
		log.Printf("[WEBHOOK_TRIGGER] Failed to get webhook config for project_owner=%s: %v", projectOwnerID, err)
		return
	}

	if webhook == nil {
		// No webhook configured for this project owner - silently skip
		return
	}

	// Check if webhook is subscribed to this specific event type
	if !webhook.HasTopic(eventType) {
		log.Printf("[WEBHOOK_TRIGGER] Webhook for project_owner=%s not subscribed to %s", projectOwnerID, eventType)
		return
	}

	// Enqueue to batched delivery log writer (handles DB insert + RabbitMQ publish in batches)
	if !EnqueueWebhookDelivery(projectOwnerID, webhook, eventType, payload) {
		log.Printf("[WEBHOOK_TRIGGER] ⚠️ Failed to enqueue webhook delivery for project_owner=%s (buffer full)", projectOwnerID)
	}
}

// GetProjectOwnerIDFromCampaignReference extracts project_owner_id from campaign reference
// This should be called when processing DataGen webhooks that contain campaign references
func GetProjectOwnerIDFromCampaignReference(reference map[string]interface{}) string {
	if reference == nil {
		return ""
	}

	// Check if reference contains parent_campaign_id
	parentCampaignID, ok := reference["parent_campaign_id"].(string)
	if !ok || parentCampaignID == "" {
		// Try campaign_id as fallback
		campaignID, ok := reference["campaign_id"].(string)
		if !ok || campaignID == "" {
			return ""
		}
		parentCampaignID = campaignID
	}

	// Look up campaign in database to get project_owner_id
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	campaignCollection := utils.GetCollection("campaigns")

	// Try to parse as ObjectID first
	campaignObjectID, err := primitive.ObjectIDFromHex(parentCampaignID)
	if err != nil {
		// If not ObjectID, search by string campaign_id
		var campaign struct {
			ProjectOwnerID string `bson:"project_owner_id"`
		}
		err = campaignCollection.FindOne(ctx, bson.M{"campaign_id": parentCampaignID}).Decode(&campaign)
		if err != nil {
			log.Printf("[WEBHOOK_TRIGGER] Failed to find campaign by campaign_id: %v", err)
			return ""
		}
		return campaign.ProjectOwnerID
	}

	// Search by ObjectID _id
	var campaign struct {
		ProjectOwnerID string `bson:"project_owner_id"`
	}
	err = campaignCollection.FindOne(ctx, bson.M{"_id": campaignObjectID}).Decode(&campaign)
	if err != nil {
		log.Printf("[WEBHOOK_TRIGGER] Failed to find campaign by _id: %v", err)
		return ""
	}

	return campaign.ProjectOwnerID
}

// GetProjectOwnerIDFromProjectID extracts project_owner_id from project_id
// This can be used when DataGen webhook reference contains project information
func GetProjectOwnerIDFromProjectID(projectID string) string {
	if projectID == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	projectCollection := utils.GetCollection("projects")

	// Parse projectID as ObjectID
	projectObjectID, err := primitive.ObjectIDFromHex(projectID)
	if err != nil {
		log.Printf("[WEBHOOK_TRIGGER] Invalid project_id format: %v", err)
		return ""
	}

	var project struct {
		ProjectOwnerID string `bson:"project_owner_id"`
	}
	err = projectCollection.FindOne(ctx, bson.M{"_id": projectObjectID}).Decode(&project)
	if err != nil {
		log.Printf("[WEBHOOK_TRIGGER] Failed to find project: %v", err)
		return ""
	}

	return project.ProjectOwnerID
}
