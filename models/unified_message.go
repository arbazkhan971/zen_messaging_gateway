package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UnifiedMessage represents a normalized message across all BSPs
// This provides a single source of truth for message tracking
type UnifiedMessage struct {
	ID primitive.ObjectID `json:"id" bson:"_id,omitempty"`

	// Internal tracking
	MessageID string `json:"message_id" bson:"message_id"` // Our internal unique ID

	// WhatsApp Message ID (wamid)
	Wamid string `json:"wamid" bson:"wamid"` // Format: wamid.HBgL...

	// BSP Information
	BSP          string `json:"bsp" bson:"bsp"`                     // datagen, aisensy, karix
	BSPMessageID string `json:"bsp_message_id" bson:"bsp_message_id"` // BSP's message ID

	// Contact Information
	PhoneNumber    string `json:"phone_number" bson:"phone_number"`       // Recipient phone
	BusinessNumber string `json:"business_number" bson:"business_number"` // Sender business number

	// Status Tracking
	CurrentStatus string          `json:"current_status" bson:"current_status"` // sent, delivered, read, failed
	StatusHistory []MessageStatus `json:"status_history" bson:"status_history"`

	// Campaign/Project Context
	CampaignID     string `json:"campaign_id,omitempty" bson:"campaign_id,omitempty"`
	ProjectID      string `json:"project_id,omitempty" bson:"project_id,omitempty"`
	ProjectOwnerID string `json:"project_owner_id" bson:"project_owner_id"`

	// BSP-Specific Data (stored as flexible map)
	BSPData map[string]interface{} `json:"bsp_data,omitempty" bson:"bsp_data,omitempty"`

	// Timestamps
	SentAt      *time.Time `json:"sent_at,omitempty" bson:"sent_at,omitempty"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty" bson:"delivered_at,omitempty"`
	ReadAt      *time.Time `json:"read_at,omitempty" bson:"read_at,omitempty"`
	FailedAt    *time.Time `json:"failed_at,omitempty" bson:"failed_at,omitempty"`

	CreatedAt time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt time.Time `json:"updated_at" bson:"updated_at"`
}

// MessageStatus represents a single status event in the message lifecycle
type MessageStatus struct {
	Status    string    `json:"status" bson:"status"`         // sent, delivered, read, failed
	Source    string    `json:"source" bson:"source"`         // BSP name
	Timestamp time.Time `json:"timestamp" bson:"timestamp"`
	Reason    string    `json:"reason,omitempty" bson:"reason,omitempty"`       // For failures
	ErrorCode string    `json:"error_code,omitempty" bson:"error_code,omitempty"` // BSP error code
}

// NewUnifiedMessage creates a new unified message
func NewUnifiedMessage(bsp, phoneNumber, businessNumber, projectOwnerID string) *UnifiedMessage {
	now := time.Now()
	return &UnifiedMessage{
		ID:             primitive.NewObjectID(),
		MessageID:      primitive.NewObjectID().Hex(), // Generate unique ID
		BSP:            bsp,
		PhoneNumber:    phoneNumber,
		BusinessNumber: businessNumber,
		ProjectOwnerID: projectOwnerID,
		CurrentStatus:  "pending",
		StatusHistory:  []MessageStatus{},
		BSPData:        make(map[string]interface{}),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// AddStatus adds a new status event to the message
func (m *UnifiedMessage) AddStatus(status, source, reason, errorCode string) {
	now := time.Now()

	// Add to history
	m.StatusHistory = append(m.StatusHistory, MessageStatus{
		Status:    status,
		Source:    source,
		Timestamp: now,
		Reason:    reason,
		ErrorCode: errorCode,
	})

	// Update current status
	m.CurrentStatus = status
	m.UpdatedAt = now

	// Update specific timestamp fields
	switch status {
	case "sent":
		m.SentAt = &now
	case "delivered":
		m.DeliveredAt = &now
	case "read":
		m.ReadAt = &now
	case "failed":
		m.FailedAt = &now
	}
}

// SetWamid sets the WhatsApp Message ID
func (m *UnifiedMessage) SetWamid(wamid string) {
	m.Wamid = wamid
	m.UpdatedAt = time.Now()
}

// SetBSPMessageID sets the BSP's message ID
func (m *UnifiedMessage) SetBSPMessageID(bspMessageID string) {
	m.BSPMessageID = bspMessageID
	m.UpdatedAt = time.Now()
}

// SetCampaignContext sets campaign-related information
func (m *UnifiedMessage) SetCampaignContext(campaignID, projectID string) {
	m.CampaignID = campaignID
	m.ProjectID = projectID
	m.UpdatedAt = time.Now()
}

// AddBSPData adds BSP-specific data
func (m *UnifiedMessage) AddBSPData(key string, value interface{}) {
	if m.BSPData == nil {
		m.BSPData = make(map[string]interface{})
	}
	m.BSPData[key] = value
	m.UpdatedAt = time.Now()
}

// GetLatestStatus returns the most recent status
func (m *UnifiedMessage) GetLatestStatus() MessageStatus {
	if len(m.StatusHistory) == 0 {
		return MessageStatus{
			Status:    m.CurrentStatus,
			Timestamp: m.CreatedAt,
		}
	}
	return m.StatusHistory[len(m.StatusHistory)-1]
}

// IsDelivered checks if the message has been delivered
func (m *UnifiedMessage) IsDelivered() bool {
	return m.CurrentStatus == "delivered" || m.CurrentStatus == "read"
}

// IsFailed checks if the message has failed
func (m *UnifiedMessage) IsFailed() bool {
	return m.CurrentStatus == "failed"
}

// GetDeliveryDuration returns the time taken for delivery
func (m *UnifiedMessage) GetDeliveryDuration() *time.Duration {
	if m.SentAt != nil && m.DeliveredAt != nil {
		duration := m.DeliveredAt.Sub(*m.SentAt)
		return &duration
	}
	return nil
}
