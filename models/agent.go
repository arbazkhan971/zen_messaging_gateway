package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Agent types
const (
	AgentTypeOwner   = "owner"
	AgentTypeManager = "Admin"
	AgentTypeAgent   = "agent"
)

// Currency types
const (
	CurrencyINR = "INR"
	CurrencyUSD = "USD"
)

type Agent struct {
	ID               string `json:"id" bson:"_id"`
	UserName         string `json:"user_name" bson:"user_name"`
	Email            string `json:"email" bson:"email"`
	PhoneNumber      string `json:"phone_number,omitempty" bson:"phone_number,omitempty"` // Optional, used for Karix integration
	Active           bool   `json:"active,omitempty" bson:"active,omitempty"`
	DisplayName      string `json:"display_name,omitempty" bson:"display_name,omitempty"`
	ProjectID        string `json:"project_id" bson:"project_id"`
	ProjectOwnerID   string `json:"project_owner_id,omitempty" bson:"project_owner_id,omitempty"`
	CreatedAt        int64  `json:"createdAt" bson:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt" bson:"updatedAt"`
	Company          string `json:"company,omitempty" bson:"company,omitempty"`
	Contact          string `json:"contact,omitempty" bson:"contact,omitempty"`
	Currency         string `json:"currency,omitempty" bson:"currency,omitempty"`
	Timezone         string `json:"timezone,omitempty" bson:"timezone,omitempty"`
	Type             string `json:"type,omitempty" bson:"type,omitempty"`
	IsInvited        bool   `json:"is_invited" bson:"is_invited"`
	IsInviteSent     bool   `json:"is_invite_sent" bson:"is_invite_sent"`
	IsInviteAccepted bool   `json:"is_invite_accepted" bson:"is_invite_accepted"`
	IsVerified       bool   `json:"is_verified" bson:"is_verified"`
	OrganizationName string `json:"organization_name,omitempty" bson:"organization_name,omitempty"`
	WorkspaceLogo    string `json:"workspace_logo,omitempty" bson:"workspace_logo,omitempty"`
}

// NewAgent creates a new agent with default values
func NewAgent(userName string, email string) *Agent {
	now := time.Now().UnixMilli()
	return &Agent{
		ID:               primitive.NewObjectID().Hex(),
		UserName:         userName,
		Email:            email,
		Active:           true,
		CreatedAt:        now,
		UpdatedAt:        now,
		Type:             AgentTypeAgent,
		IsInvited:        true,
		IsInviteSent:     true,
		IsInviteAccepted: false,
	}
}

// UpdateAgentInfo updates the modifiable agent information
func (a *Agent) UpdateAgentInfo(displayName, email, company, contact string) {
	a.DisplayName = displayName
	a.Email = email
	a.Company = company
	a.Contact = contact
	a.UpdatedAt = time.Now().UnixMilli()
}
