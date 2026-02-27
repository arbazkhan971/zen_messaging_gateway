package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WebhookDeliveryLog tracks webhook forwarding attempts and their results
type WebhookDeliveryLog struct {
	ID             primitive.ObjectID     `json:"id" bson:"_id,omitempty"`
	ProjectOwnerID string                 `json:"project_owner_id" bson:"project_owner_id"`
	WebhookID      string                 `json:"webhook_id" bson:"webhook_id"`
	EventID        string                 `json:"event_id" bson:"event_id"`
	EventType      string                 `json:"event_type" bson:"event_type"` // "datagen.delivery", "message.sent", etc.
	WebhookURL     string                 `json:"webhook_url" bson:"webhook_url"`
	Payload        map[string]interface{} `json:"payload" bson:"payload"`

	// Delivery tracking
	AttemptCount       int       `json:"attempt_count" bson:"attempt_count"`
	Status             string    `json:"status" bson:"status"` // pending, success, failed, retrying
	StatusCode         int       `json:"status_code,omitempty" bson:"status_code,omitempty"`
	ResponseBody       string    `json:"response_body,omitempty" bson:"response_body,omitempty"`
	ErrorMessage       string    `json:"error_message,omitempty" bson:"error_message,omitempty"`
	DeliveryDurationMs int64     `json:"delivery_duration_ms,omitempty" bson:"delivery_duration_ms,omitempty"`
	CreatedAt          time.Time `json:"created_at" bson:"created_at"`
	LastAttemptAt      time.Time `json:"last_attempt_at" bson:"last_attempt_at"`
	NextRetryAt        time.Time `json:"next_retry_at,omitempty" bson:"next_retry_at,omitempty"`
}

// DeliveryStatus constants
const (
	DeliveryStatusPending  = "pending"
	DeliveryStatusSuccess  = "success"
	DeliveryStatusFailed   = "failed"
	DeliveryStatusRetrying = "retrying"
)

// EventType constants
const (
	EventTypeDataGenDelivery = "datagen.delivery"
	EventTypeMessageSent     = "message.sent"
	EventTypeMessageDelivered = "message.delivered"
	EventTypeMessageRead     = "message.read"
	EventTypeMessageFailed   = "message.failed"
)

// NewWebhookDeliveryLog creates a new delivery log
func NewWebhookDeliveryLog(projectOwnerID, webhookID, eventID, eventType, webhookURL string, payload map[string]interface{}) *WebhookDeliveryLog {
	now := time.Now()
	return &WebhookDeliveryLog{
		ID:             primitive.NewObjectID(),
		ProjectOwnerID: projectOwnerID,
		WebhookID:      webhookID,
		EventID:        eventID,
		EventType:      eventType,
		WebhookURL:     webhookURL,
		Payload:        payload,
		AttemptCount:   0,
		Status:         DeliveryStatusPending,
		CreatedAt:      now,
		LastAttemptAt:  now,
	}
}

// MarkSuccess marks the delivery as successful
func (w *WebhookDeliveryLog) MarkSuccess(statusCode int, responseBody string) {
	w.Status = DeliveryStatusSuccess
	w.StatusCode = statusCode
	w.ResponseBody = responseBody
	w.LastAttemptAt = time.Now()
	w.AttemptCount++
}

// MarkFailed marks the delivery as failed
func (w *WebhookDeliveryLog) MarkFailed(statusCode int, errorMessage string) {
	w.Status = DeliveryStatusFailed
	w.StatusCode = statusCode
	w.ErrorMessage = errorMessage
	w.LastAttemptAt = time.Now()
	w.AttemptCount++
}

// IncrementAttempt increments the attempt counter
func (w *WebhookDeliveryLog) IncrementAttempt() {
	w.AttemptCount++
	w.LastAttemptAt = time.Now()
}

// ShouldRetry determines if the delivery should be retried
func (w *WebhookDeliveryLog) ShouldRetry(maxAttempts int) bool {
	return w.Status == DeliveryStatusPending && w.AttemptCount < maxAttempts
}
