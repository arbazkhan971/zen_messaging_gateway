package webhook

import (
	"zen_messaging_gateway/handlers/webhook"

	"github.com/gin-gonic/gin"
)

// MapWebhookRoutes maps all webhook-related routes
func MapWebhookRoutes(router *gin.Engine) {
	// Initialize webhook handlers
	webhook.InitWebhookHandlers()

	// =========================================================================
	// BSP WEBHOOK ENDPOINTS - Each BSP has its own dedicated endpoint
	// =========================================================================

	// Datagen webhook endpoint
	// Datagen uses Karix underneath, but has its own webhook format
	// Handles delivery events, user messages, and batch webhooks
	router.POST("/webhook/datagen", webhook.DatagenWebhookHandler)

	// Aisensy webhook endpoint (partner account)
	// Handles message status, contact updates, template status from Aisensy
	router.POST("/webhook/aisensy", webhook.AisensyWebhookHandler)

	// Karix direct webhook endpoint
	// For direct Karix API integration (if you use Karix API directly without Datagen)
	// Note: Datagen and Karix are the same BSP, but may have different webhook formats
	router.POST("/webhook/karix", webhook.KarixWebhookHandler)

	// =========================================================================
	// BACKWARD COMPATIBILITY - Keep old endpoints working during migration
	// =========================================================================

	// Legacy Datagen endpoint (redirect to new endpoint)
	router.POST("/datagen-webhook", webhook.DatagenWebhookHandler)

	// Legacy Partner endpoint (redirect to Aisensy endpoint)
	router.POST("/partner-webhook", webhook.AisensyWebhookHandler)

	// =========================================================================
	// WEBHOOK CONFIGURATION & MANAGEMENT ROUTES
	// =========================================================================

	webhookGroup := router.Group("/api/v1/webhooks")
	{
		// Create or update webhook configuration
		// webhookGroup.POST("/configure", controllers.ConfigureWebhook)

		// Get webhook configuration
		// webhookGroup.GET("/config/:project_owner_id", controllers.GetWebhookConfig)

		// Delete webhook configuration
		// webhookGroup.DELETE("/config/:project_owner_id", controllers.DeleteWebhookConfig)

		// Get webhook delivery logs
		// webhookGroup.GET("/logs/:project_owner_id", controllers.GetWebhookLogs)

		// Get webhook delivery stats
		// webhookGroup.GET("/stats/:project_owner_id", controllers.GetWebhookStats)
	}
}
