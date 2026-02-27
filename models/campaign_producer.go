package models

import "time"

// =================================================================================
// CAMPAIGN PRODUCER API MODELS
// =================================================================================
// These models are used for communication between the main server and the
// dedicated campaign producer server for job queuing.

// QueueJobsRequest represents the request payload for queuing campaign jobs
// from the main server to the producer server.
type QueueJobsRequest struct {
	CampaignID         string                  `json:"campaign_id" binding:"required"`
	CSVGCSURL          string                  `json:"csv_gcs_url" binding:"required"`
	CSVFileName        string                  `json:"csv_file_name" binding:"required"`
	ProjectConfigs     []ProjectCampaignConfig `json:"project_configs" binding:"required"`
	VariableMappings   []VariableMapping       `json:"variable_mappings"`
	MediaURL           string                  `json:"media_url,omitempty"`
	MediaFilename      string                  `json:"media_filename,omitempty"`
	TemplateDocumentID string                  `json:"template_document_id,omitempty"` // MongoDB ObjectID as string
	BaseButtons        []Button                `json:"base_buttons,omitempty"`
	ProjectIDs         []string                `json:"project_ids" binding:"required"`
	ExcludeOptOuts     bool                    `json:"exclude_opt_outs"`
	HasDatagenProject  bool                    `json:"has_datagen_project"`
	ScheduledAt        time.Time               `json:"scheduled_at"` // For delayed execution
	ProjectOwnerID     string                  `json:"project_owner_id" binding:"required"`
}

// ProjectCampaignConfig holds the details of a successfully created AiSensy campaign.
type ProjectCampaignConfig struct {
	ProjectID         string `json:"project_id" binding:"required"`
	TemplateName      string `json:"template_name" binding:"required"`
	CampaignName      string `json:"campaign_name" binding:"required"`
	CampaignID        string `json:"id"`                  // Parent campaign ID
	AiSensyCampaignID string `json:"aisensy_campaign_id"` // Real AiSensy campaign ID from API
}

// VariableMapping defines how CSV columns map to template parameters.
type VariableMapping struct {
	ParamIndex    string `json:"param_index" bson:"param_index"`
	AttributeKey  string `json:"attribute_key" bson:"attribute_key"`
	FallbackValue string `json:"fallback_value" bson:"fallback_value"`
}

// ButtonMapping defines how CSV columns map to button parameters.
type ButtonMapping struct {
	ButtonIndex   string `json:"button_index" bson:"button_index"`
	AttributeKey  string `json:"attribute_key" bson:"attribute_key"`
	FallbackValue string `json:"fallback_value" bson:"fallback_value"`
}

// QueueJobsResponse represents the response from the producer server
// after queuing campaign jobs.
type QueueJobsResponse struct {
	Success       bool   `json:"success"`
	JobsEnqueued  int64  `json:"jobs_enqueued"`
	RowsProcessed int64  `json:"rows_processed"`
	RowsSkipped   int64  `json:"rows_skipped"`
	Error         string `json:"error,omitempty"`
	CampaignID    string `json:"campaign_id"`
}

// CampaignExecutionMessage represents a message sent to RabbitMQ
// to trigger scheduled campaign execution.
type CampaignExecutionMessage struct {
	CampaignID    string    `json:"campaign_id"`
	ScheduledAt   time.Time `json:"scheduled_at"`
	ExecutionType string    `json:"execution_type"` // "SCHEDULED" or "IMMEDIATE"
}
