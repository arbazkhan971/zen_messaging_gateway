package models

import "time"

// =====================================================================================
// RETRY CONFIGURATION
// =====================================================================================

// RetryConfig defines a single retry attempt configuration
type RetryConfig struct {
	RetryNumber  int `json:"retry_number" bson:"retry_number"`   // 1, 2, or 3
	DelayHours   int `json:"delay_hours" bson:"delay_hours"`     // Hours to wait before this retry
	DelayMinutes int `json:"delay_minutes" bson:"delay_minutes"` // Additional minutes to wait
}

// =====================================================================================
// CampaignSendJob: SHARED STRUCT FOR RABBITMQ PRODUCER/CONSUMER CONTRACT
// =====================================================================================
// This struct is used for both producing and consuming campaign jobs via RabbitMQ.
// Do not change field names/types without updating both producer and consumer.
// Add these new structs before CampaignSendJob
type ButtonParameter struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Button struct {
	Type       string            `json:"type"`
	SubType    string            `json:"sub_type"`
	Index      int               `json:"index"`
	Parameters []ButtonParameter `json:"parameters"`
}

// CampaignSendJob struct serves as the common interface for all message providers
// This struct is used to standardize message sending across different providers:
// - Meta: Uses Graph API with specific message structure
// - Datagen: Uses RCM API with similar structure to Meta
// - AiSensy: Uses AiSensy API with a different structure
//
// Fields:
// - ParentCampaignID: ID of the parent campaign
// - TargetProjectID: ID of the project to send the message from
// - AiSensyCampaignName: Campaign name for AiSensy provider (can also be template name for Meta/Datagen)
// - RecipientName: Name of the recipient for personalization
// - PhoneNumber: Recipient's phone number
// - TemplateParams: Array of template parameters for dynamic content
// - Buttons: Interactive buttons for the message
// - GlobalMediaInfo: Media attachment information (image, video, document)
// - SourceFileName: Original CSV file name for tracking
// - CSVRowNumber: Row number in the CSV for tracking
// - ProviderType: Provider type (META, DATAGEN, AISENSY)
// - TemplateID: Template ID/name for Meta/Datagen provider
type CampaignSendJob struct {
	ParentCampaignID    string     `json:"ParentCampaignID"`
	TargetProjectID     string     `json:"TargetProjectID"`
	AiSensyCampaignName string     `json:"AiSensyCampaignName"`
	RecipientName       string     `json:"RecipientName"`
	PhoneNumber         string     `json:"PhoneNumber"`
	TemplateParams      []string   `json:"TemplateParams"`
	Buttons             []Button   `json:"buttons,omitempty"`
	GlobalMediaInfo     *MediaInfo `json:"GlobalMediaInfo"`
	SourceFileName      string     `json:"SourceFileName,omitempty"`
	CSVRowNumber        int        `json:"CSVRowNumber,omitempty"`
	ProviderType        string     `json:"ProviderType,omitempty"` // META, DATAGEN, or AISENSY
	TemplateID          string     `json:"TemplateID,omitempty"`   // Template ID/name for Meta/Datagen provider
	MessageID           string     `json:"MessageID,omitempty"`    // Unique message ID for deduplication (optional)

	// --- RETRY TRACKING FIELDS ---
	RetryAttempt         int    `json:"RetryAttempt,omitempty"`         // 0 for original, 1-3 for individual message retries (retry_consumer)
	OriginalMessageID    string `json:"OriginalMessageID,omitempty"`    // Links retry messages to the original message
	RetryCampaignID      string `json:"RetryCampaignID,omitempty"`      // The sub-campaign ID for this retry attempt
	CampaignRetryAttempt int    `json:"CampaignRetryAttempt,omitempty"` // Campaign-level retry attempt (0=original, 1-3=campaign retries)
}

// Campaign types
const (
	CampaignTypeBroadcast = "BROADCAST"
	CampaignTypeAPI       = "API"
)

// Provider types for campaign sending
const (
	ProviderTypeAISensy = "AISENSY"
	ProviderTypeMeta    = "META"
	ProviderTypeDatagen = "DATAGEN"
)

// CampaignRequest defines the structure for each item in the bulk request body.
type CampaignRequest struct {
	ProjectID    string `json:"project_id"`
	CampaignName string `json:"campaign_name"`
}

// Campaign status types
const (
	CampaignStatusSent    = "SENT"
	CampaignStatusLive    = "LIVE"
	CampaignStatusSending = "SENDING"
	CampaignStatusStopped = "STOPPED"
	CampaignStatusPaused  = "PAUSED"
	CampaignStatusFailed  = "FAILED"
)

// Message types
const (
	MessageTypeTemplate = "TEMPLATE"
	MessageTypeRegular  = "REGULAR"
)

type WhatsAppTemplate1 struct {
	ID                string      `json:"id" bson:"id"`
	Name              string      `json:"name" bson:"name"`
	Label             string      `json:"label" bson:"label"`
	Status            string      `json:"status" bson:"status"`
	CallToAction      []CTAButton `json:"call_to_action" bson:"call_to_action"`
	QuickReplies      []string    `json:"quick_replies" bson:"quick_replies"`
	Type              string      `json:"type" bson:"type"`
	Language          string      `json:"language" bson:"language"`
	Text              string      `json:"text" bson:"text"`
	SampleText        string      `json:"sample_text" bson:"sample_text"`
	MessageActionType string      `json:"message_action_type" bson:"message_action_type"`
	TotalParameters   int         `json:"total_parameters" bson:"total_parameters"`
	ProjectID         string      `json:"project_id" bson:"project_id"`
	ProjectOwnerID    string      `json:"project_owner_id" bson:"project_owner_id"`
	CreatedAt         int64       `json:"created_at" bson:"created_at"`
	UpdatedAt         int64       `json:"updated_at" bson:"updated_at"`
	Category          string      `json:"category,omitempty" bson:"category,omitempty"`
	RejectedReason    string      `json:"rejected_reason,omitempty" bson:"rejected_reason,omitempty"`
}

type CTAButton struct {
	Type        string `json:"type" bson:"type"`
	ButtonValue string `json:"button_value" bson:"button_value"`
	ButtonTitle string `json:"button_title" bson:"button_title"`
}

type MediaInfo struct {
	Filename string `json:"filename" bson:"filename"`
	URL      string `json:"url" bson:"url"`
}

type MessagePayload struct {
	Template   WhatsAppTemplate1 `json:"template" bson:"template"`
	Parameters []string          `json:"parameters,omitempty" bson:"parameters,omitempty"`
	Media      *MediaInfo        `json:"media,omitempty" bson:"media,omitempty"`
}

type Campaign struct {
	ID              string           `json:"id" bson:"_id"`
	Name            string           `json:"name" bson:"name"`
	Type            string           `json:"type" bson:"type"`
	AudienceSize    *int             `json:"audience_size,omitempty" bson:"audience_size,omitempty"`
	Submitted       *int             `json:"submitted,omitempty" bson:"submitted,omitempty"`
	ProjectIDs      []string         `json:"project_ids" bson:"project_ids"` // CHANGED: array of object ids
	ProjectOwnerID  string           `json:"project_owner_id" bson:"project_owner_id"`
	Status          string           `json:"status" bson:"status"`
	MessageType     string           `json:"message_type" bson:"message_type"`
	MessagePayloads []MessagePayload `json:"message_payloads" bson:"message_payloads"`

	// --- CAMPAIGN BUDGET FIELDS ---
	BudgetEnabled bool    `json:"budget_enabled" bson:"budget_enabled"`
	BudgetLimit   float64 `json:"budget_limit,omitempty" bson:"budget_limit,omitempty"`

	// --- FLATTENED MESSAGE PAYLOAD FIELDS ---
	TemplateID                string      `json:"template_id" bson:"template_id"`
	TemplateName              string      `json:"template_name" bson:"template_name"`
	TemplateLabel             string      `json:"template_label" bson:"template_label"`
	TemplateStatus            string      `json:"template_status" bson:"template_status"`
	TemplateType              string      `json:"template_type" bson:"template_type"`
	TemplateLanguage          string      `json:"template_language" bson:"template_language"`
	TemplateText              string      `json:"template_text" bson:"template_text"`
	TemplateSampleText        string      `json:"template_sample_text" bson:"template_sample_text"`
	TemplateCategory          string      `json:"template_category,omitempty" bson:"template_category,omitempty"`
	TemplateRejectedReason    string      `json:"template_rejected_reason,omitempty" bson:"template_rejected_reason,omitempty"`
	TemplateCallToAction      []CTAButton `json:"template_call_to_action" bson:"template_call_to_action"`
	TemplateQuickReplies      []string    `json:"template_quick_replies" bson:"template_quick_replies"`
	TemplateTotalParameters   int         `json:"template_total_parameters" bson:"template_total_parameters"`
	TemplateMessageActionType string      `json:"template_message_action_type" bson:"template_message_action_type"`
	TemplateGroupID           string      `json:"template_group_id,omitempty" bson:"template_group_id,omitempty"`
	Parameters                []string    `json:"parameters,omitempty" bson:"parameters,omitempty"`
	MediaFilename             string      `json:"media_filename,omitempty" bson:"media_filename,omitempty"`
	MediaURL                  string      `json:"media_url,omitempty" bson:"media_url,omitempty"`
	CreatedAt                 int64       `json:"created_at" bson:"created_at"`
	UpdatedAt                 int64       `json:"updated_at" bson:"updated_at"`
	// --- DYNAMODB BULK REQUEST FIELDS (added for parity with BulkCampaignRequestRecord) ---
	BatchRequestID           string   `json:"batch_request_id,omitempty" bson:"batch_request_id,omitempty"`
	RequestTimestamp         string   `json:"request_timestamp,omitempty" bson:"request_timestamp,omitempty"`
	ScheduleTime             string   `json:"schedule_time,omitempty" bson:"schedule_time,omitempty"`
	ArchivedFileS3URLs       []string `json:"archived_file_s3_urls,omitempty" bson:"archived_file_s3_urls,omitempty"`
	CampaignConfigsJSON      string   `json:"campaign_configs_json,omitempty" bson:"campaign_configs_json,omitempty"`
	GlobalMediaURL           string   `json:"global_media_url,omitempty" bson:"global_media_url,omitempty"`
	GlobalMediaFilename      string   `json:"global_media_filename,omitempty" bson:"global_media_filename,omitempty"`
	GlobalSource             string   `json:"global_source,omitempty" bson:"global_source,omitempty"`
	GlobalDefaultCountryCode string   `json:"global_default_country_code,omitempty" bson:"global_default_country_code,omitempty"`
	GlobalTags               string   `json:"global_tags,omitempty" bson:"global_tags,omitempty"`
	GlobalAttributesJSON     string   `json:"global_attributes_json,omitempty" bson:"global_attributes_json,omitempty"`
	ReceivedFileNames        []string `json:"received_file_names,omitempty" bson:"received_file_names,omitempty"`
	FileCount                int      `json:"file_count,omitempty" bson:"file_count,omitempty"`

	// --- RETRY CONFIGURATION FIELDS ---
	EnableRetry        bool          `json:"enable_retry" bson:"enable_retry"`
	RetryConfigs       []RetryConfig `json:"retry_configs,omitempty" bson:"retry_configs,omitempty"`
	IsRetryCampaign    bool          `json:"is_retry_campaign" bson:"is_retry_campaign"`
	ParentCampaignID   string        `json:"parent_campaign_id,omitempty" bson:"parent_campaign_id,omitempty"`
	CurrentRetryNumber int           `json:"current_retry_number,omitempty" bson:"current_retry_number,omitempty"` // 0 for original, 1-3 for retries
}

// NewCampaign creates a new campaign with default values
func NewCampaign(name, projectID, projectOwnerID string, campaignType string) *Campaign {
	now := time.Now().UnixMilli()
	return &Campaign{
		Name:           name,
		Type:           campaignType,
		ProjectIDs:     []string{projectID},
		ProjectOwnerID: projectOwnerID,
		Status:         CampaignStatusPaused,
		MessageType:    MessageTypeTemplate,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// UpdateStatus updates the campaign status
func (c *Campaign) UpdateStatus(status string) {
	c.Status = status
	c.UpdatedAt = time.Now().UnixMilli()
}

// UpdateMessagePayload updates the campaign message payload
func (c *Campaign) UpdateMessagePayload(payload MessagePayload) {
	// Flatten fields from MessagePayload and WhatsAppTemplate1
	t := payload.Template
	c.TemplateID = t.ID
	c.TemplateName = t.Name
	c.TemplateLabel = t.Label
	c.TemplateStatus = t.Status
	c.TemplateType = t.Type
	c.TemplateLanguage = t.Language
	c.TemplateText = t.Text
	c.TemplateSampleText = t.SampleText
	c.TemplateCategory = t.Category
	c.TemplateRejectedReason = t.RejectedReason
	c.TemplateCallToAction = t.CallToAction
	c.TemplateQuickReplies = t.QuickReplies
	c.TemplateTotalParameters = t.TotalParameters
	c.TemplateMessageActionType = t.MessageActionType
	c.Parameters = payload.Parameters
	if payload.Media != nil {
		c.MediaFilename = payload.Media.Filename
		c.MediaURL = payload.Media.URL
	} else {
		c.MediaFilename = ""
		c.MediaURL = ""
	}
	c.UpdatedAt = time.Now().UnixMilli()
}

// IncrementSubmitted increments the submitted count
func (c *Campaign) IncrementSubmitted() {
	if c.Submitted == nil {
		count := 1
		c.Submitted = &count
	} else {
		*c.Submitted++
	}
	c.UpdatedAt = time.Now().UnixMilli()
}

// FailedAudience represents a single failed message record from the AiSensy API.
type FailedAudience struct {
	ID             string         `json:"_id"`
	AssistantID    string         `json:"assistantId"`
	CampaignID     string         `json:"campaignId"`
	ChatID         string         `json:"chatId"`
	UserNumber     string         `json:"userNumber"`
	ConversationID string         `json:"conversationId"`
	FailedAt       time.Time      `json:"failedAt"` // Use time.Time for easy parsing
	FailurePayload FailurePayload `json:"failurePayload"`
	ISOCode        string         `json:"isoCode"`
	SentAt         time.Time      `json:"sentAt"`
	UserName       string         `json:"userName"`
}

// FailurePayload contains details about why the message failed.
type FailurePayload struct {
	Code      string          `json:"code"`
	Reason    string          `json:"reason"`
	Message   string          `json:"message"`
	ErrorData ErrorDataDetail `json:"error_data"`
}

// ErrorDataDetail has the specific error details.
type ErrorDataDetail struct {
	Details string `json:"details"`
}

// AiSensyAudienceResponse is the top-level structure for the audience API response.
type AiSensyAudienceResponse struct {
	Total  int              `json:"total"`
	Data   []FailedAudience `json:"data"`
	Paging PagingInfo       `json:"paging"`
}

// PagingInfo contains the cursors for navigating through results.
type PagingInfo struct {
	Cursors CursorInfo `json:"cursors"`
}

// CursorInfo holds the 'after' and 'before' tokens for pagination.
type CursorInfo struct {
	After  string `json:"after"`
	Before string `json:"before"`
}

// MetaData represents metadata for the message
type MetaData struct {
	Version string `json:"version"`
}

// =====================================================================================
// META/DATAGEN MESSAGE STRUCTURES
// =====================================================================================
//
// The following structures define the payload format for sending messages via Meta/Datagen API.
// These structures are used by both the MetaMessageSender and the ScalableCampaignConsumer.
//
// For consistency across providers (Meta, Datagen, AiSensy), the following fields are handled:
// 1. Template Parameters:
//    - Meta/Datagen: Converted to map with numeric keys (parameterValues/bodyParameterValues)
//    - AiSensy: Passed directly as template_params array
//
// 2. Media Attachments:
//    - Meta/Datagen: Structured with specific fields for type and URL
//    - AiSensy: Passed directly as media object
//
// 3. Interactive Buttons:
//    - Meta/Datagen: Structured as MetaButtons with QuickReplies
//    - AiSensy: Passed directly as buttons array
//
// The CampaignSendJob struct is the common interface used by all providers.

// MetaMessage represents the main message structure for Meta/Datagen API
type MetaMessage struct {
	Message  MetaMessageContent `json:"message"`
	MetaData MetaData           `json:"metaData"`
}

// MetaMessageContent represents the message content for Meta/Datagen
type MetaMessageContent struct {
	Type        string          `json:"type,omitempty"`
	Channel     string          `json:"channel"`
	Content     MetaContent     `json:"content"`
	Recipient   MetaRecipient   `json:"recipient"`
	Sender      MetaSender      `json:"sender"`
	Preferences MetaPreferences `json:"preferences"`
}

// MetaContent represents the content of the message
type MetaContent struct {
	PreviewURL    bool               `json:"preview_url"`
	ShortenURL    bool               `json:"shorten_url,omitempty"` // For location request messages
	Type          string             `json:"type"`
	Text          string             `json:"text,omitempty"`
	Template      *MetaTemplate      `json:"template,omitempty"`
	MediaTemplate *MetaMediaTemplate `json:"mediaTemplate,omitempty"`
	Attachment    *MetaAttachment    `json:"attachment,omitempty"`
	Interactive   interface{}        `json:"interactive,omitempty"` // For INTERACTIVE type messages (list, buttons, location request)
}

// MetaAttachment represents a media attachment
type MetaAttachment struct {
	Type     string `json:"type"`
	URL      string `json:"url"`
	MimeType string `json:"mimeType,omitempty"`
	Caption  string `json:"caption,omitempty"`
	Filename string `json:"fileName,omitempty"`
}

// MetaTemplate represents a simple template message
type MetaTemplate struct {
	TemplateID      string            `json:"templateId"`
	ParameterValues map[string]string `json:"parameterValues,omitempty"`
	Buttons         *MetaButtons      `json:"buttons,omitempty"`
}

// MetaMediaTemplate represents a media template message
type MetaMediaTemplate struct {
	TemplateID          string            `json:"templateId,omitempty"`
	AutoTemplate        string            `json:"autoTemplate,omitempty"`
	Media               *MetaMedia        `json:"media,omitempty"`
	BodyParameterValues map[string]string `json:"bodyParameterValues,omitempty"`
	Buttons             *MetaButtons      `json:"buttons,omitempty"`
}

// MetaMedia represents media content
type MetaMedia struct {
	Caption  string `json:"caption,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Filename string `json:"fileName,omitempty"`
	Type     string `json:"type,omitempty"`
	URL      string `json:"url,omitempty"`
}

// MetaButtons represents button options
type MetaButtons struct {
	Actions      []MetaAction     `json:"actions,omitempty"`
	QuickReplies []MetaQuickReply `json:"quickReplies,omitempty"`
}

// MetaAction represents a URL action button
type MetaAction struct {
	Type    string `json:"type"`
	Index   string `json:"index"`
	Payload string `json:"payload"`
}

// MetaQuickReply represents a quick reply button
type MetaQuickReply struct {
	Index   string `json:"index"`
	Payload string `json:"payload"`
}

// MetaRecipient represents the message recipient
type MetaRecipient struct {
	To            string                 `json:"to"`
	RecipientType string                 `json:"recipient_type"`
	Reference     MetaRecipientReference `json:"reference"`
}

type MetaRecipientReference struct {
	CustRef        string `json:"cust_ref,omitempty"`
	MessageTag1    string `json:"messageTag1,omitempty"`
	MessageTag2    string `json:"messageTag2,omitempty"`
	MessageTag3    string `json:"messageTag3,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
}

// MetaSender represents the message sender
type MetaSender struct {
	From string `json:"from"`
}

// MetaPreferences represents message preferences
type MetaPreferences struct {
	WebHookDNId string `json:"webHookDNId"`
}
