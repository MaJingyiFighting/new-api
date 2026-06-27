package model

import "time"

// CodingPlanQuotaState holds the result of a Coding Plan quota probe for a single
// channel. It is persisted inside ChannelInfo.CodingPlanQuota so that the admin
// dashboard can display the latest known usage without re-probing on every page
// load.
type CodingPlanQuotaState struct {
	Provider               string     `json:"provider,omitempty"`
	PlanName               string     `json:"plan_name,omitempty"`
	FiveHourUsedPercent    *float64   `json:"five_hour_used_percent,omitempty"`
	FiveHourResetAt        *time.Time `json:"five_hour_reset_at,omitempty"`
	WeeklyUsedPercent      *float64   `json:"weekly_used_percent,omitempty"`
	WeeklyResetAt          *time.Time `json:"weekly_reset_at,omitempty"`
	Success                bool       `json:"success"`
	ErrorMessage           string     `json:"error_message,omitempty"`
	HTTPStatus             int        `json:"http_status,omitempty"`
	CredentialExpired      bool       `json:"credential_expired,omitempty"`
	QuotaProbeStatus       string     `json:"quota_probe_status,omitempty"`
	UpdatedAt              time.Time  `json:"updated_at"`
}
