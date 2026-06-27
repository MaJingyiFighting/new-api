package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const (
	// CodingPlanProbeStatusSupported indicates a provider has a working quota
	// probe endpoint.
	CodingPlanProbeStatusSupported = "supported"

	// CodingPlanProbeStatusUnsupported indicates a provider has no known quota
	// endpoint and always returns a static snapshot.
	CodingPlanProbeStatusUnsupported = "unsupported"
)

// codingPlanProbeTimeout is the default HTTP client timeout for all quota probes.
const codingPlanProbeTimeout = 15 * time.Second

// CodingPlanQuotaSnapshot holds the result of a single quota-probe call.
type CodingPlanQuotaSnapshot struct {
	Provider              string
	FiveHourUsedPercent   *float64
	FiveHourResetAt       *time.Time
	FiveHourResetAfterSec *int64
	WeeklyUsedPercent     *float64
	WeeklyResetAt         *time.Time
	WeeklyResetAfterSec   *int64
	PlanName              *string
	QuotaProbeStatus      string
	Raw                   map[string]any
	UpdatedAt             time.Time
	Source                string
	Success               bool
	ErrorMessage          string
	HTTPStatus            int
	CredentialExpired     bool
}

// CodingPlanQuotaProbe is the interface every quota probe implements.
type CodingPlanQuotaProbe interface {
	Provider() constant.CodingPlanProvider
	Detect(baseURL string) bool
	Probe(ctx context.Context, baseURL, apiKey string) (*CodingPlanQuotaSnapshot, error)
}

// codingPlanHTTPProbe is the common implementation for providers with a working
// HTTP quota endpoint.
type codingPlanHTTPProbe struct {
	provider constant.CodingPlanProvider
	client   *http.Client
}

// ----- Constructors -----

func NewKimiCodingPlanProbe(client *http.Client) CodingPlanQuotaProbe {
	return &codingPlanHTTPProbe{provider: constant.CodingPlanProviderKimi, client: client}
}

func NewZhipuCodingPlanProbe(client *http.Client) CodingPlanQuotaProbe {
	return &codingPlanHTTPProbe{provider: constant.CodingPlanProviderZhipu, client: client}
}

func NewMiniMaxCodingPlanProbe(client *http.Client) CodingPlanQuotaProbe {
	return &codingPlanHTTPProbe{provider: constant.CodingPlanProviderMiniMax, client: client}
}

func NewVolcengineCodingPlanProbe(client *http.Client) CodingPlanQuotaProbe {
	return &codingPlanHTTPProbe{provider: constant.CodingPlanProviderVolcengine, client: client}
}

func NewMiMoCodingPlanProbe(client *http.Client) CodingPlanQuotaProbe {
	return &codingPlanHTTPProbe{provider: constant.CodingPlanProviderMiMo, client: client}
}

// ----- Interface methods -----

func (p *codingPlanHTTPProbe) Provider() constant.CodingPlanProvider {
	if p == nil {
		return ""
	}
	return p.provider
}

func (p *codingPlanHTTPProbe) Detect(baseURL string) bool {
	return DetectCodingPlanProviderFromBaseURL(baseURL) == p.Provider()
}

func (p *codingPlanHTTPProbe) Probe(ctx context.Context, baseURL, apiKey string) (*CodingPlanQuotaSnapshot, error) {
	switch p.Provider() {
	case constant.CodingPlanProviderKimi:
		return p.probeKimi(ctx, baseURL, apiKey)
	case constant.CodingPlanProviderZhipu:
		return p.probeZhipu(ctx, baseURL, apiKey)
	case constant.CodingPlanProviderMiniMax:
		return p.probeMiniMax(ctx, baseURL, apiKey)
	case constant.CodingPlanProviderVolcengine, constant.CodingPlanProviderMiMo:
		snapshot := unsupportedCodingPlanQuotaSnapshot(p.Provider())
		return snapshot, nil
	default:
		return nil, fmt.Errorf("unsupported coding plan provider: %s", p.Provider())
	}
}

func (p *codingPlanHTTPProbe) httpClient() *http.Client {
	if p != nil && p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: codingPlanProbeTimeout}
}

func (p *codingPlanHTTPProbe) doProbe(req *http.Request) ([]byte, int, error) {
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	return body, resp.StatusCode, nil
}

// ----- Endpoint resolution -----

func kimiCodingPlanEndpoint(baseURL string) string {
	if baseURL != "" && !strings.Contains(baseURL, "api.kimi.com/coding") && !strings.Contains(baseURL, "api.moonshot.cn") {
		return strings.TrimRight(baseURL, "/") + "/v1/usages"
	}
	return KimiCodingPlanQuotaEndpoint
}

func zhipuCodingPlanEndpoint(baseURL string) string {
	if strings.Contains(baseURL, "api.z.ai") {
		return ZhipuZAICodingPlanQuotaEndpoint
	}
	return ZhipuCodingPlanQuotaEndpoint
}

func miniMaxCodingPlanEndpoint(baseURL string) string {
	if strings.Contains(baseURL, "api.minimax.io") {
		return MiniMaxIOCodingPlanQuotaEndpoint
	}
	return MiniMaxCodingPlanQuotaEndpoint
}

// ----- Per-provider probe logic -----

func (p *codingPlanHTTPProbe) probeKimi(ctx context.Context, baseURL, apiKey string) (*CodingPlanQuotaSnapshot, error) {
	endpoint := kimiCodingPlanEndpoint(baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	body, status, err := p.doProbe(req)
	if err != nil {
		return nil, fmt.Errorf("quota probe request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return httpErrorSnapshot(constant.CodingPlanProviderKimi, status), fmt.Errorf("quota probe returned HTTP %d", status)
	}
	return ParseKimiCodingPlanQuota(body, time.Now())
}

func (p *codingPlanHTTPProbe) probeZhipu(ctx context.Context, baseURL, apiKey string) (*CodingPlanQuotaSnapshot, error) {
	endpoint := zhipuCodingPlanEndpoint(baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", apiKey) // Zhipu does NOT use Bearer prefix
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US,en")

	body, status, err := p.doProbe(req)
	if err != nil {
		return nil, fmt.Errorf("quota probe request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return httpErrorSnapshot(constant.CodingPlanProviderZhipu, status), fmt.Errorf("quota probe returned HTTP %d", status)
	}
	return ParseZhipuCodingPlanQuota(body, time.Now())
}

func (p *codingPlanHTTPProbe) probeMiniMax(ctx context.Context, baseURL, apiKey string) (*CodingPlanQuotaSnapshot, error) {
	endpoint := miniMaxCodingPlanEndpoint(baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	body, status, err := p.doProbe(req)
	if err != nil {
		return nil, fmt.Errorf("quota probe request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return httpErrorSnapshot(constant.CodingPlanProviderMiniMax, status), fmt.Errorf("quota probe returned HTTP %d", status)
	}
	return ParseMiniMaxCodingPlanQuota(body, time.Now())
}

// ----- Parse functions -----

// ParseKimiCodingPlanQuota parses a Kimi quota API response.
//
// Expected JSON shape:
//
//	{
//	  "limits": [{"detail": {"limit": <n>, "remaining": <n>, "resetTime": <ts>}}],
//	  "usage":  {"limit": <n>, "remaining": <n>, "resetTime": <ts>}
//	}
func ParseKimiCodingPlanQuota(body []byte, now time.Time) (*CodingPlanQuotaSnapshot, error) {
	var root map[string]any
	if err := common.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to parse kimi quota response: %w", err)
	}
	snapshot := newCodingPlanQuotaSnapshot(constant.CodingPlanProviderKimi, now, "active_probe")
	snapshot.Raw = root

	if limits, ok := root["limits"].([]any); ok {
		for _, item := range limits {
			entry, _ := item.(map[string]any)
			detail, _ := entry["detail"].(map[string]any)
			if detail == nil {
				continue
			}
			limit := parseExtraFloat64(detail["limit"])
			remaining := parseExtraFloat64(detail["remaining"])
			if limit <= 0 {
				continue
			}
			usedPercent := usedPercentFromLimitRemaining(limit, remaining)
			snapshot.FiveHourUsedPercent = &usedPercent
			if resetAt := parseCodingPlanResetTime(detail["resetTime"], now); resetAt != nil {
				snapshot.FiveHourResetAt = resetAt
				seconds := int64(math.Max(0, resetAt.Sub(now).Seconds()))
				snapshot.FiveHourResetAfterSec = &seconds
			}
			break
		}
	}

	if usage, ok := root["usage"].(map[string]any); ok {
		limit := parseExtraFloat64(usage["limit"])
		remaining := parseExtraFloat64(usage["remaining"])
		if limit > 0 {
			usedPercent := usedPercentFromLimitRemaining(limit, remaining)
			snapshot.WeeklyUsedPercent = &usedPercent
			if resetAt := parseCodingPlanResetTime(usage["resetTime"], now); resetAt != nil {
				snapshot.WeeklyResetAt = resetAt
				seconds := int64(math.Max(0, resetAt.Sub(now).Seconds()))
				snapshot.WeeklyResetAfterSec = &seconds
			}
		}
	}

	return snapshot, nil
}

// ParseZhipuCodingPlanQuota parses a Zhipu quota API response.
//
// Expected JSON shape:
//
//	{
//	  "success": true,
//	  "data": {
//	    "level": "pro",
//	    "limits": [
//	      {"type": "TOKENS_LIMIT", "unit": 3, "percentage": <n>, "nextResetTime": <ts>},
//	      {"type": "TOKENS_LIMIT", "unit": 6, "percentage": <n>, "nextResetTime": <ts>}
//	    ]
//	  }
//	}
func ParseZhipuCodingPlanQuota(body []byte, now time.Time) (*CodingPlanQuotaSnapshot, error) {
	var root map[string]any
	if err := common.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to parse zhipu quota response: %w", err)
	}
	snapshot := newCodingPlanQuotaSnapshot(constant.CodingPlanProviderZhipu, now, "active_probe")
	snapshot.Raw = root

	if successRaw, ok := root["success"]; ok {
		if success, ok := successRaw.(bool); ok && !success {
			snapshot.Success = false
			msg := strings.TrimSpace(fmt.Sprint(root["message"]))
			if msg == "" {
				msg = "zhipu quota API returned success=false"
			}
			snapshot.ErrorMessage = msg
			return snapshot, fmt.Errorf("%s", msg)
		}
	}

	data, _ := root["data"].(map[string]any)
	if data == nil {
		return snapshot, nil
	}

	if level, ok := data["level"].(string); ok && strings.TrimSpace(level) != "" {
		plan := strings.TrimSpace(level)
		snapshot.PlanName = &plan
	}

	limits, _ := data["limits"].([]any)
	var tokenLimits []map[string]any
	for _, item := range limits {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		typeVal := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["type"])))
		if typeVal == "tokens_limit" {
			tokenLimits = append(tokenLimits, entry)
		}
	}

	for _, entry := range tokenLimits {
		unit := int(parseExtraFloat64(entry["unit"]))
		usedPercent := parseExtraFloat64(entry["percentage"])
		resetAt := parseCodingPlanResetTime(entry["nextResetTime"], now)
		switch unit {
		case 3:
			snapshot.FiveHourUsedPercent = &usedPercent
			if resetAt != nil {
				snapshot.FiveHourResetAt = resetAt
				seconds := int64(math.Max(0, resetAt.Sub(now).Seconds()))
				snapshot.FiveHourResetAfterSec = &seconds
			}
		case 6:
			snapshot.WeeklyUsedPercent = &usedPercent
			if resetAt != nil {
				snapshot.WeeklyResetAt = resetAt
				seconds := int64(math.Max(0, resetAt.Sub(now).Seconds()))
				snapshot.WeeklyResetAfterSec = &seconds
			}
		}
	}

	// Fallback: if only one TOKENS_LIMIT entry and neither window was matched,
	// treat it as the 5-hour window.
	if len(tokenLimits) == 1 && snapshot.FiveHourUsedPercent == nil && snapshot.WeeklyUsedPercent == nil {
		entry := tokenLimits[0]
		usedPercent := parseExtraFloat64(entry["percentage"])
		snapshot.FiveHourUsedPercent = &usedPercent
		if resetAt := parseCodingPlanResetTime(entry["nextResetTime"], now); resetAt != nil {
			snapshot.FiveHourResetAt = resetAt
			seconds := int64(math.Max(0, resetAt.Sub(now).Seconds()))
			snapshot.FiveHourResetAfterSec = &seconds
		}
	}

	return snapshot, nil
}

// ParseMiniMaxCodingPlanQuota parses a MiniMax quota API response.
//
// Expected JSON shape:
//
//	{
//	  "base_resp": {"status_code": 0, "status_msg": "success"},
//	  "model_remains": [{
//	    "model_name": "general",
//	    "current_interval_remaining_percent": <n>,
//	    "end_time": <ts>,
//	    "current_weekly_status": <0|1>,
//	    "current_weekly_remaining_percent": <n>,
//	    "weekly_end_time": <ts>
//	  }]
//	}
func ParseMiniMaxCodingPlanQuota(body []byte, now time.Time) (*CodingPlanQuotaSnapshot, error) {
	var root map[string]any
	if err := common.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to parse minimax quota response: %w", err)
	}
	snapshot := newCodingPlanQuotaSnapshot(constant.CodingPlanProviderMiniMax, now, "active_probe")
	snapshot.Raw = root

	baseResp, _ := root["base_resp"].(map[string]any)
	if code := parseExtraFloat64(baseResp["status_code"]); code != 0 {
		msg := strings.TrimSpace(fmt.Sprint(baseResp["status_msg"]))
		if msg == "" {
			msg = fmt.Sprintf("minimax business error: %.0f", code)
		}
		snapshot.Success = false
		snapshot.ErrorMessage = msg
		return snapshot, fmt.Errorf("%s", msg)
	}

	modelRemains, _ := root["model_remains"].([]any)
	for _, item := range modelRemains {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(entry["model_name"])), "general") {
			continue
		}
		fiveHourUsed := 100 - parseExtraFloat64(entry["current_interval_remaining_percent"])
		fiveHourUsed = clampPercent(fiveHourUsed)
		snapshot.FiveHourUsedPercent = &fiveHourUsed
		if resetAt := parseCodingPlanResetTime(entry["end_time"], now); resetAt != nil {
			snapshot.FiveHourResetAt = resetAt
			seconds := int64(math.Max(0, resetAt.Sub(now).Seconds()))
			snapshot.FiveHourResetAfterSec = &seconds
		}
		if int(parseExtraFloat64(entry["current_weekly_status"])) == 1 {
			weeklyUsed := 100 - parseExtraFloat64(entry["current_weekly_remaining_percent"])
			weeklyUsed = clampPercent(weeklyUsed)
			snapshot.WeeklyUsedPercent = &weeklyUsed
			if resetAt := parseCodingPlanResetTime(entry["weekly_end_time"], now); resetAt != nil {
				snapshot.WeeklyResetAt = resetAt
				seconds := int64(math.Max(0, resetAt.Sub(now).Seconds()))
				snapshot.WeeklyResetAfterSec = &seconds
			}
		}
		break
	}

	return snapshot, nil
}

// ----- Shared utility functions -----

func parseExtraFloat64(value any) float64 {
	if value == nil {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f
		}
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	case bool:
		if v {
			return 1
		}
		return 0
	}
	return 0
}

func parseCodingPlanResetTime(value any, now time.Time) *time.Time {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		if t, err := parseTime(s); err == nil {
			return &t
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return unixFlexibleTime(n)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return unixFlexibleTime(n)
		}
	case float64:
		return unixFlexibleTime(int64(v))
	case int64:
		return unixFlexibleTime(v)
	case int:
		return unixFlexibleTime(int64(v))
	}
	return nil
}

func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

func unixFlexibleTime(n int64) *time.Time {
	if n <= 0 {
		return nil
	}
	if n > 1_000_000_000_000 {
		t := time.UnixMilli(n).UTC()
		return &t
	}
	t := time.Unix(n, 0).UTC()
	return &t
}

func usedPercentFromLimitRemaining(limit, remaining float64) float64 {
	if limit <= 0 {
		return 0
	}
	used := limit - remaining
	if used < 0 {
		used = 0
	}
	return clampPercent(used / limit * 100)
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// ----- Snapshot helpers -----

func newCodingPlanQuotaSnapshot(provider constant.CodingPlanProvider, now time.Time, source string) *CodingPlanQuotaSnapshot {
	if now.IsZero() {
		now = time.Now()
	}
	return &CodingPlanQuotaSnapshot{
		Provider:         string(provider),
		QuotaProbeStatus: CodingPlanProbeStatusSupported,
		UpdatedAt:        now.UTC(),
		Source:           source,
		Success:          true,
	}
}

func unsupportedCodingPlanQuotaSnapshot(provider constant.CodingPlanProvider) *CodingPlanQuotaSnapshot {
	now := time.Now().UTC()
	return &CodingPlanQuotaSnapshot{
		Provider:         string(provider),
		QuotaProbeStatus: CodingPlanProbeStatusUnsupported,
		UpdatedAt:        now,
		Source:           "active_probe",
		Success:          false,
	}
}

func httpErrorSnapshot(provider constant.CodingPlanProvider, status int) *CodingPlanQuotaSnapshot {
	snapshot := newCodingPlanQuotaSnapshot(provider, time.Now(), "active_probe")
	snapshot.Success = false
	snapshot.HTTPStatus = status
	snapshot.ErrorMessage = fmt.Sprintf("quota probe returned HTTP %d", status)
	if status == http.StatusUnauthorized {
		snapshot.CredentialExpired = true
	}
	return snapshot
}

// ----- Public API -----

// DetectCodingPlanQuotaProbe finds a probe whose Detect method matches the
// given base URL. Returns nil when no provider matches.
func DetectCodingPlanQuotaProbe(baseURL string) CodingPlanQuotaProbe {
	probes := []CodingPlanQuotaProbe{
		NewKimiCodingPlanProbe(nil),
		NewZhipuCodingPlanProbe(nil),
		NewMiniMaxCodingPlanProbe(nil),
		NewVolcengineCodingPlanProbe(nil),
		NewMiMoCodingPlanProbe(nil),
	}
	for _, probe := range probes {
		if probe.Detect(baseURL) {
			return probe
		}
	}
	return nil
}

// ProbeCodingPlanQuota dispatches a quota probe by provider.
// Volcengine and MiMo return an unsupported snapshot without making an HTTP
// request. Kimi, Zhipu, and MiniMax perform an HTTP probe against the
// appropriate endpoint derived from baseURL.
func ProbeCodingPlanQuota(ctx context.Context, baseURL, apiKey string, provider constant.CodingPlanProvider) (*CodingPlanQuotaSnapshot, error) {
	var probe CodingPlanQuotaProbe
	switch provider {
	case constant.CodingPlanProviderKimi:
		probe = NewKimiCodingPlanProbe(nil)
	case constant.CodingPlanProviderZhipu:
		probe = NewZhipuCodingPlanProbe(nil)
	case constant.CodingPlanProviderMiniMax:
		probe = NewMiniMaxCodingPlanProbe(nil)
	case constant.CodingPlanProviderVolcengine, constant.CodingPlanProviderMiMo:
		snapshot := unsupportedCodingPlanQuotaSnapshot(provider)
		return snapshot, nil
	default:
		return nil, fmt.Errorf("unsupported coding plan provider: %s", provider)
	}
	return probe.Probe(ctx, baseURL, apiKey)
}
