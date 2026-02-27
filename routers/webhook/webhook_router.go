package webhook

import (
	"github.com/gin-gonic/gin"
)

// MapWebhookRoutes maps all webhook-related routes
func MapWebhookRoutes(router *gin.Engine) {
	// Webhook configuration routes
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

	// Webhook receiver endpoint (for receiving webhooks from external systems)
	// router.POST("/webhooks/receive/:project_owner_id", controllers.ReceiveWebhook)
}
