package webhook

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	models "zen_messaging_gateway/models"
	utils "zen_messaging_gateway/utils"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// CreateWebhookConfigRequest represents the request body for creating/updating a webhook
type CreateWebhookConfigRequest struct {
	ProjectOwnerID   string   `json:"project_owner_id" binding:"required"`
	WebhookURL       string   `json:"webhook_url" binding:"required,url"`
	Topics           []string `json:"topics"`            // Optional, defaults to ["datagen.delivery"]
	EnableAuth       bool     `json:"enable_auth"`       // Optional, defaults to true (with HMAC signature)
	RegenerateSecret bool     `json:"regenerate_secret"` // Optional, regenerate secret on update
}

// WebhookConfigResponse represents the response for webhook configuration
type WebhookConfigResponse struct {
	ID             string   `json:"id"`
	ProjectOwnerID string   `json:"project_owner_id"`
	WebhookURL     string   `json:"webhook_url"`
	Topics         []string `json:"topics"`
	SharedSecret   string   `json:"shared_secret,omitempty"` // Only returned on creation/regeneration
	AuthEnabled    bool     `json:"auth_enabled"`            // Whether authentication is enabled
	AuthStatus     string   `json:"auth_status"`             // "enabled" or "disabled"
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
}

// CreateOrUpdateWebhookConfig creates or updates a webhook configuration for a project owner
// POST /webhook/config
func CreateOrUpdateWebhookConfig(c *gin.Context) {
	var req CreateWebhookConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Default topics to datagen.delivery if not provided
	if len(req.Topics) == 0 {
		req.Topics = []string{models.EventTypeDataGenDelivery}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := utils.GetCollection("webhooks")

	// Check if webhook already exists for this project owner
	var existingWebhook models.Webhook
	err := collection.FindOne(ctx, bson.M{"project_owner_id": req.ProjectOwnerID}).Decode(&existingWebhook)

	if err == nil {
		// Webhook exists - update it
		existingWebhook.WebhookURL = req.WebhookURL
		existingWebhook.Topics = req.Topics
		existingWebhook.UpdatedAt = time.Now().UnixMilli()

		// Handle authentication settings
		updateFields := bson.M{
			"webhook_url": existingWebhook.WebhookURL,
			"topics":      existingWebhook.Topics,
			"updated_at":  existingWebhook.UpdatedAt,
		}

		// If user wants to regenerate secret or enable auth
		var newSecret string
		if req.RegenerateSecret || (req.EnableAuth && existingWebhook.SharedSecret == "") {
			if err := existingWebhook.GenerateSharedSecret(); err != nil {
				log.Printf("[WEBHOOK_CONFIG] Failed to generate secret: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate secret"})
				return
			}
			updateFields["shared_secret"] = existingWebhook.SharedSecret
			newSecret = existingWebhook.SharedSecret
		} else if !req.EnableAuth {
			// User wants to disable authentication
			existingWebhook.SharedSecret = ""
			updateFields["shared_secret"] = ""
		}

		// Validate the webhook
		if err := existingWebhook.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("validation failed: %v", err)})
			return
		}

		// Update in database
		_, err := collection.UpdateOne(
			ctx,
			bson.M{"project_owner_id": req.ProjectOwnerID},
			bson.M{"$set": updateFields},
		)
		if err != nil {
			log.Printf("[WEBHOOK_CONFIG] Failed to update webhook: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update webhook"})
			return
		}

		// Invalidate webhook cache after successful update
		invalidateWebhookCache(req.ProjectOwnerID)

		log.Printf("[WEBHOOK_CONFIG] Updated webhook for project_owner=%s", req.ProjectOwnerID)

		response := WebhookConfigResponse{
			ID:             existingWebhook.ID,
			ProjectOwnerID: existingWebhook.ProjectOwnerID,
			WebhookURL:     existingWebhook.WebhookURL,
			Topics:         existingWebhook.Topics,
			CreatedAt:      existingWebhook.CreatedAt,
			UpdatedAt:      existingWebhook.UpdatedAt,
		}

		// Return new secret if it was regenerated
		if newSecret != "" {
			response.SharedSecret = newSecret
		}

		c.JSON(http.StatusOK, response)
		return
	}

	if err != mongo.ErrNoDocuments {
		log.Printf("[WEBHOOK_CONFIG] Database error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// Webhook doesn't exist - create new one
	newWebhook := &models.Webhook{
		ID:             primitive.NewObjectID().Hex(),
		ProjectOwnerID: req.ProjectOwnerID,
		WebhookURL:     req.WebhookURL,
		Topics:         req.Topics,
		CreatedAt:      time.Now().UnixMilli(),
		UpdatedAt:      time.Now().UnixMilli(),
	}

	// Generate shared secret only if EnableAuth is true (default behavior)
	// If EnableAuth is not explicitly set to false, generate secret for security
	shouldEnableAuth := !c.Request.URL.Query().Has("enable_auth") || req.EnableAuth

	if shouldEnableAuth {
		if err := newWebhook.GenerateSharedSecret(); err != nil {
			log.Printf("[WEBHOOK_CONFIG] Failed to generate secret: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate secret"})
			return
		}
	}

	// Validate the webhook
	if err := newWebhook.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("validation failed: %v", err)})
		return
	}

	// Insert into database
	_, err = collection.InsertOne(ctx, newWebhook)
	if err != nil {
		log.Printf("[WEBHOOK_CONFIG] Failed to create webhook: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create webhook"})
		return
	}

	// Cache the new webhook configuration
	updateWebhookMemoryCache(req.ProjectOwnerID, newWebhook)
	updateWebhookRedisCache(req.ProjectOwnerID, newWebhook)

	authStatus := "disabled"
	if newWebhook.SharedSecret != "" {
		authStatus = "enabled"
	}
	log.Printf("[WEBHOOK_CONFIG] Created webhook for project_owner=%s (auth: %s)", req.ProjectOwnerID, authStatus)

	response := WebhookConfigResponse{
		ID:             newWebhook.ID,
		ProjectOwnerID: newWebhook.ProjectOwnerID,
		WebhookURL:     newWebhook.WebhookURL,
		Topics:         newWebhook.Topics,
		CreatedAt:      newWebhook.CreatedAt,
		UpdatedAt:      newWebhook.UpdatedAt,
	}

	// Return secret only on creation if it was generated
	if newWebhook.SharedSecret != "" {
		response.SharedSecret = newWebhook.SharedSecret
	}

	c.JSON(http.StatusCreated, response)
}

// GetWebhookConfig retrieves the webhook configuration for a project owner
// GET /webhook/config/:project_owner_id
func GetWebhookConfig(c *gin.Context) {
	projectOwnerID := c.Param("project_owner_id")
	if projectOwnerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_owner_id is required"})
		return
	}

	webhook, err := GetWebhookByProjectOwnerID(projectOwnerID)
	if err != nil {
		log.Printf("[WEBHOOK_CONFIG] Failed to get webhook: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve webhook"})
		return
	}

	if webhook == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	authEnabled := webhook.SharedSecret != ""
	authStatus := "disabled"
	if authEnabled {
		authStatus = "enabled"
	}

	c.JSON(http.StatusOK, WebhookConfigResponse{
		ID:             webhook.ID,
		ProjectOwnerID: webhook.ProjectOwnerID,
		WebhookURL:     webhook.WebhookURL,
		Topics:         webhook.Topics,
		AuthEnabled:    authEnabled,
		AuthStatus:     authStatus,
		CreatedAt:      webhook.CreatedAt,
		UpdatedAt:      webhook.UpdatedAt,
	})
}

// DeleteWebhookConfig deletes a webhook configuration
// DELETE /webhook/config/:project_owner_id
func DeleteWebhookConfig(c *gin.Context) {
	projectOwnerID := c.Param("project_owner_id")
	if projectOwnerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_owner_id is required"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := utils.GetCollection("webhooks")

	result, err := collection.DeleteOne(ctx, bson.M{"project_owner_id": projectOwnerID})
	if err != nil {
		log.Printf("[WEBHOOK_CONFIG] Failed to delete webhook: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete webhook"})
		return
	}

	if result.DeletedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	// Invalidate webhook cache after successful deletion
	invalidateWebhookCache(projectOwnerID)

	log.Printf("[WEBHOOK_CONFIG] Deleted webhook for project_owner=%s", projectOwnerID)
	c.JSON(http.StatusOK, gin.H{"message": "webhook deleted successfully"})
}

// RegenerateWebhookSecret regenerates the shared secret for a webhook
// POST /webhook/config/:project_owner_id/regenerate-secret
func RegenerateWebhookSecret(c *gin.Context) {
	projectOwnerID := c.Param("project_owner_id")
	if projectOwnerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_owner_id is required"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := utils.GetCollection("webhooks")

	// Get existing webhook
	var webhook models.Webhook
	err := collection.FindOne(ctx, bson.M{"project_owner_id": projectOwnerID}).Decode(&webhook)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
			return
		}
		log.Printf("[WEBHOOK_CONFIG] Failed to get webhook: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve webhook"})
		return
	}

	// Generate new secret
	if err := webhook.GenerateSharedSecret(); err != nil {
		log.Printf("[WEBHOOK_CONFIG] Failed to generate secret: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate secret"})
		return
	}

	// Update in database
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"project_owner_id": projectOwnerID},
		bson.M{"$set": bson.M{
			"shared_secret": webhook.SharedSecret,
			"updated_at":    webhook.UpdatedAt,
		}},
	)
	if err != nil {
		log.Printf("[WEBHOOK_CONFIG] Failed to update secret: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update secret"})
		return
	}

	// Invalidate webhook cache after secret update
	invalidateWebhookCache(projectOwnerID)

	log.Printf("[WEBHOOK_CONFIG] Regenerated secret for project_owner=%s", projectOwnerID)
	c.JSON(http.StatusOK, gin.H{
		"message":       "secret regenerated successfully",
		"shared_secret": webhook.SharedSecret,
	})
}

// ResetWebhookSecret clears the shared secret (disables authentication)
// POST /webhook/config/:project_owner_id/reset-secret
func ResetWebhookSecret(c *gin.Context) {
	projectOwnerID := c.Param("project_owner_id")
	if projectOwnerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_owner_id is required"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := utils.GetCollection("webhooks")

	// Get existing webhook
	var webhook models.Webhook
	err := collection.FindOne(ctx, bson.M{"project_owner_id": projectOwnerID}).Decode(&webhook)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
			return
		}
		log.Printf("[WEBHOOK_CONFIG] Failed to get webhook: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve webhook"})
		return
	}

	// Clear the secret
	webhook.SharedSecret = ""
	webhook.UpdatedAt = time.Now().UnixMilli()

	// Update in database
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"project_owner_id": projectOwnerID},
		bson.M{"$set": bson.M{
			"shared_secret": "",
			"updated_at":    webhook.UpdatedAt,
		}},
	)
	if err != nil {
		log.Printf("[WEBHOOK_CONFIG] Failed to reset secret: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset secret"})
		return
	}

	// Invalidate webhook cache after secret reset
	invalidateWebhookCache(projectOwnerID)

	log.Printf("[WEBHOOK_CONFIG] Reset secret (disabled auth) for project_owner=%s", projectOwnerID)
	c.JSON(http.StatusOK, gin.H{
		"message": "authentication disabled successfully",
		"status":  "no_authentication_required",
	})
}

// TestWebhookConfig sends a test webhook to the configured endpoint
// POST /webhook/config/:project_owner_id/test
func TestWebhookConfig(c *gin.Context) {
	projectOwnerID := c.Param("project_owner_id")
	if projectOwnerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_owner_id is required"})
		return
	}

	// Get webhook configuration
	webhook, err := GetWebhookByProjectOwnerID(projectOwnerID)
	if err != nil {
		log.Printf("[WEBHOOK_CONFIG] Failed to get webhook: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve webhook"})
		return
	}

	if webhook == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	// Create test payload
	testPayload := map[string]interface{}{
		"test":      true,
		"message":   "This is a test webhook from Serri",
		"timestamp": time.Now().Unix(),
	}

	// Create test forwarding payload
	forwardingPayload := utils.WebhookForwardingPayload{
		DeliveryLogID:  "test",
		ProjectOwnerID: projectOwnerID,
		WebhookID:      webhook.ID,
		WebhookURL:     webhook.WebhookURL,
		SharedSecret:   webhook.SharedSecret,
		EventType:      "test.webhook",
		EventID:        fmt.Sprintf("test_%d", time.Now().UnixMilli()),
		Payload:        testPayload,
		Timestamp:      time.Now().UnixMilli(),
		AttemptCount:   0,
	}

	// Try to deliver the test webhook
	success, statusCode, responseBody, errorMsg := DeliverWebhook(forwardingPayload)

	if success {
		log.Printf("[WEBHOOK_CONFIG] Test webhook sent successfully: project_owner=%s, status=%d", projectOwnerID, statusCode)
		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"message":       "test webhook delivered successfully",
			"status_code":   statusCode,
			"response_body": responseBody,
		})
	} else {
		log.Printf("[WEBHOOK_CONFIG] Test webhook failed: project_owner=%s, error=%s", projectOwnerID, errorMsg)
		c.JSON(http.StatusOK, gin.H{
			"success":       false,
			"message":       "test webhook delivery failed",
			"status_code":   statusCode,
			"response_body": responseBody,
			"error":         errorMsg,
		})
	}
}
