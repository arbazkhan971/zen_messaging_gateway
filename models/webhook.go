package models

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"time"
)

type Webhook struct {
	ID             string   `json:"id" bson:"_id"`
	AppID          string   `json:"app_id" bson:"app_id"`
	ProjectID      string   `json:"project_id" bson:"project_id"`
	ProjectOwnerID string   `json:"project_owner_id" bson:"project_owner_id"`
	Topics         []string `json:"topics" bson:"topics"`
	WebhookURL     string   `json:"webhook_url" bson:"webhook_url"`
	PartnerID      string   `json:"partner_id" bson:"partner_id"`
	SharedSecret   string   `json:"shared_secret" bson:"shared_secret"`
	CreatedAt      int64    `json:"created_at" bson:"created_at"`
	UpdatedAt      int64    `json:"updated_at" bson:"updated_at"`
}

// NewWebhook creates a new webhook with default values
func NewWebhook(appID, projectID, projectOwnerID, partnerID, webhookURL string) *Webhook {
	now := time.Now().UnixMilli()
	return &Webhook{
		AppID:          appID,
		ProjectID:      projectID,
		ProjectOwnerID: projectOwnerID,
		Topics:         make([]string, 0),
		WebhookURL:     webhookURL,
		PartnerID:      partnerID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// AddTopic adds a new topic to the webhook subscription
func (w *Webhook) AddTopic(topic string) {
	w.Topics = append(w.Topics, topic)
	w.UpdatedAt = time.Now().UnixMilli()
}

// RemoveTopic removes a topic from the webhook subscription
func (w *Webhook) RemoveTopic(topic string) {
	for i, t := range w.Topics {
		if t == topic {
			w.Topics = append(w.Topics[:i], w.Topics[i+1:]...)
			break
		}
	}
	w.UpdatedAt = time.Now().UnixMilli()
}

// UpdateWebhookURL updates the webhook URL
func (w *Webhook) UpdateWebhookURL(url string) {
	w.WebhookURL = url
	w.UpdatedAt = time.Now().UnixMilli()
}

// HasTopic checks if the webhook is subscribed to a specific topic
func (w *Webhook) HasTopic(topic string) bool {
	for _, t := range w.Topics {
		if t == topic {
			return true
		}
	}
	return false
}

// SetSharedSecret sets the shared secret for the webhook
func (w *Webhook) SetSharedSecret(secret string) {
	w.SharedSecret = secret
	w.UpdatedAt = time.Now().UnixMilli()
}

// GenerateSharedSecret generates a cryptographically secure random secret
func (w *Webhook) GenerateSharedSecret() error {
	secret, err := GenerateSecureSecret(32)
	if err != nil {
		return err
	}
	w.SharedSecret = secret
	w.UpdatedAt = time.Now().UnixMilli()
	return nil
}

// Validate checks if the webhook configuration is valid
func (w *Webhook) Validate() error {
	if w.ProjectOwnerID == "" {
		return errors.New("project_owner_id is required")
	}
	if w.WebhookURL == "" {
		return errors.New("webhook_url is required")
	}

	// Validate URL format
	parsedURL, err := url.Parse(w.WebhookURL)
	if err != nil {
		return errors.New("invalid webhook_url format")
	}

	// Ensure HTTPS for production
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return errors.New("webhook_url must use http or https scheme")
	}

	return nil
}

// GenerateSecureSecret generates a cryptographically secure random string
func GenerateSecureSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
