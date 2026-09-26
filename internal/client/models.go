package client

import "encoding/json"

// The structs below mirror the engine's wire types (orch8-api / orch8-types).
// Only fields the provider manages or exposes are modelled.

// Trigger mirrors orch8_types::trigger::TriggerDef.
type Trigger struct {
	Slug         string          `json:"slug"`
	SequenceName string          `json:"sequence_name"`
	Version      *int64          `json:"version,omitempty"`
	TenantID     string          `json:"tenant_id"`
	Namespace    string          `json:"namespace"`
	Enabled      bool            `json:"enabled"`
	Secret       *string         `json:"secret,omitempty"`
	TriggerType  string          `json:"trigger_type"`
	Config       json.RawMessage `json:"config,omitempty"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// CreateTriggerRequest mirrors orch8_api::triggers::CreateTriggerRequest.
type CreateTriggerRequest struct {
	Slug         string          `json:"slug"`
	SequenceName string          `json:"sequence_name"`
	Version      *int64          `json:"version,omitempty"`
	TenantID     string          `json:"tenant_id"`
	Namespace    string          `json:"namespace"`
	Secret       *string         `json:"secret,omitempty"`
	TriggerType  string          `json:"trigger_type,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
}

// RetargetTriggerRequest mirrors PATCH /triggers/{slug}/target.
type RetargetTriggerRequest struct {
	SequenceID        string `json:"sequence_id"`
	ExpectedUpdatedAt string `json:"expected_updated_at"`
}

// CronSchedule mirrors orch8_types::cron::CronSchedule.
type CronSchedule struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	Namespace       string          `json:"namespace"`
	SequenceID      string          `json:"sequence_id"`
	CronExpr        string          `json:"cron_expr"`
	Timezone        string          `json:"timezone"`
	Enabled         bool            `json:"enabled"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	OverlapPolicy   string          `json:"overlap_policy"`
	LastTriggeredAt *string         `json:"last_triggered_at"`
	NextFireAt      *string         `json:"next_fire_at"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

// CreateCronRequest mirrors orch8_api::cron::CreateCronRequest.
type CreateCronRequest struct {
	TenantID      string          `json:"tenant_id"`
	Namespace     string          `json:"namespace"`
	SequenceID    string          `json:"sequence_id"`
	CronExpr      string          `json:"cron_expr"`
	Timezone      string          `json:"timezone"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	Enabled       bool            `json:"enabled"`
	OverlapPolicy string          `json:"overlap_policy"`
}

// UpdateCronRequest mirrors orch8_api::cron::UpdateCronRequest (PUT).
type UpdateCronRequest struct {
	CronExpr      *string         `json:"cron_expr,omitempty"`
	Timezone      *string         `json:"timezone,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	OverlapPolicy *string         `json:"overlap_policy,omitempty"`
}

// CreateAPIKeyRequest mirrors orch8_api::api_keys::CreateApiKeyRequest.
type CreateAPIKeyRequest struct {
	TenantID     string   `json:"tenant_id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities,omitempty"`
	ExpiresAt    *string  `json:"expires_at,omitempty"`
}

// APIKey mirrors CreatedApiKey / ApiKeyInfo.
type APIKey struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Secret       string   `json:"secret,omitempty"`
	CreatedAt    string   `json:"created_at"`
	ExpiresAt    *string  `json:"expires_at"`
	Revoked      bool     `json:"revoked"`
}

// RoutingRule mirrors orch8_types::queue_routing::QueueRoutingRule.
type RoutingRule struct {
	ID            string  `json:"id,omitempty"`
	TenantID      string  `json:"tenant_id"`
	HandlerName   string  `json:"handler_name"`
	MatchQueue    *string `json:"match_queue"`
	QueueOverride string  `json:"queue_override"`
	Priority      int64   `json:"priority"`
	Enabled       bool    `json:"enabled"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
}

// QueueDispatch mirrors orch8_types::queue_dispatch::QueueDispatchConfig.
type QueueDispatch struct {
	TenantID  string  `json:"tenant_id"`
	QueueName string  `json:"queue_name"`
	Mode      string  `json:"mode"`
	PushURL   *string `json:"push_url,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
}

// RollbackPolicy mirrors orch8_api::rollback::PolicyResponse.
type RollbackPolicy struct {
	ID                     int64   `json:"id"`
	TenantID               string  `json:"tenant_id"`
	SequenceName           string  `json:"sequence_name"`
	ErrorRateThreshold     float64 `json:"error_rate_threshold"`
	TimeWindowSecs         int64   `json:"time_window_secs"`
	Enabled                bool    `json:"enabled"`
	CooldownSecs           int64   `json:"cooldown_secs"`
	ConfirmationWindowSecs int64   `json:"confirmation_window_secs"`
	WebhookURL             *string `json:"webhook_url"`
	CreatedAt              string  `json:"created_at"`
	UpdatedAt              string  `json:"updated_at"`
}

// CreateRollbackPolicyRequest mirrors orch8_api::rollback::CreatePolicyRequest.
// The engine upserts on (tenant_id, sequence_name).
type CreateRollbackPolicyRequest struct {
	TenantID               string  `json:"tenant_id"`
	SequenceName           string  `json:"sequence_name"`
	ErrorRateThreshold     float64 `json:"error_rate_threshold"`
	TimeWindowSecs         int64   `json:"time_window_secs"`
	CooldownSecs           *int64  `json:"cooldown_secs,omitempty"`
	ConfirmationWindowSecs *int64  `json:"confirmation_window_secs,omitempty"`
	WebhookURL             *string `json:"webhook_url,omitempty"`
}

// Credential mirrors orch8_api::credentials::CredentialResponse (no secret).
type Credential struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	Enabled         bool    `json:"enabled"`
	ExpiresAt       *string `json:"expires_at"`
	RefreshURL      *string `json:"refresh_url"`
	HasRefreshToken bool    `json:"has_refresh_token"`
	Description     *string `json:"description"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// CreateCredentialRequest mirrors orch8_api::credentials::CreateCredentialRequest.
type CreateCredentialRequest struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Kind         string  `json:"kind,omitempty"`
	Value        string  `json:"value"`
	TenantID     string  `json:"tenant_id"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
	RefreshURL   *string `json:"refresh_url,omitempty"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	Description  *string `json:"description,omitempty"`
}

// UpdateCredentialRequest mirrors orch8_api::credentials::UpdateCredentialRequest.
type UpdateCredentialRequest struct {
	Name         *string `json:"name,omitempty"`
	Kind         *string `json:"kind,omitempty"`
	Value        *string `json:"value,omitempty"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
	RefreshURL   *string `json:"refresh_url,omitempty"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
	Description  *string `json:"description,omitempty"`
}
