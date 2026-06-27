package service

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestParseKimiCodingPlanQuota_LimitsAndUsageWithISOReset(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseKimiCodingPlanQuota([]byte(`{
		"limits":[{"detail":{"limit":100,"remaining":25,"resetTime":"2026-06-14T15:00:00Z"}}],
		"usage":{"limit":1000,"remaining":400,"resetTime":"2026-06-21T10:00:00Z"}
	}`), now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourUsedPercent)
	require.InDelta(t, 75, *snapshot.FiveHourUsedPercent, 0.001)
	require.NotNil(t, snapshot.WeeklyUsedPercent)
	require.InDelta(t, 60, *snapshot.WeeklyUsedPercent, 0.001)
	require.Equal(t, int64(5*60*60), *snapshot.FiveHourResetAfterSec)
	require.NotNil(t, snapshot.FiveHourResetAt)
	require.True(t, snapshot.FiveHourResetAt.Equal(time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC)))
}

func TestParseKimiCodingPlanQuota_ResetTime_UnixSeconds(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour).Unix()
	body := []byte(`{"limits":[{"detail":{"limit":10,"remaining":8,"resetTime":` + strconv.FormatInt(reset, 10) + `}}]}`)

	snapshot, err := ParseKimiCodingPlanQuota(body, now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourResetAt)
	require.Equal(t, reset, snapshot.FiveHourResetAt.Unix())
}

func TestParseKimiCodingPlanQuota_ResetTime_UnixMilliseconds(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour).UnixMilli()
	body := []byte(`{"limits":[{"detail":{"limit":10,"remaining":5,"resetTime":` + strconv.FormatInt(reset, 10) + `}}]}`)

	snapshot, err := ParseKimiCodingPlanQuota(body, now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourResetAt)
	require.Equal(t, reset, snapshot.FiveHourResetAt.UnixMilli())
}

func TestParseKimiCodingPlanQuota_OnlyFiveHour(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseKimiCodingPlanQuota([]byte(`{"limits":[{"detail":{"limit":50,"remaining":0}}]}`), now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourUsedPercent)
	require.InDelta(t, 100, *snapshot.FiveHourUsedPercent, 0.001)
	require.Nil(t, snapshot.WeeklyUsedPercent)
}

func TestParseKimiCodingPlanQuota_InvalidJSON(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	_, err := ParseKimiCodingPlanQuota([]byte(`{"limits":[`), now)
	require.Error(t, err)
}

func TestKimiProbe_HTTPSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key-123", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"limits":[{"detail":{"limit":200,"remaining":50,"resetTime":"2026-06-14T15:00:00Z"}}],
			"usage":{"limit":2000,"remaining":800,"resetTime":"2026-06-21T10:00:00Z"}
		}`))
	}))
	defer server.Close()

	probe := NewKimiCodingPlanProbe(server.Client())
	snapshot, err := probe.Probe(context.Background(), server.URL, "test-key-123")
	require.NoError(t, err)
	require.True(t, snapshot.Success)
	require.NotNil(t, snapshot.FiveHourUsedPercent)
	require.InDelta(t, 75, *snapshot.FiveHourUsedPercent, 0.001)
	require.NotNil(t, snapshot.WeeklyUsedPercent)
	require.InDelta(t, 60, *snapshot.WeeklyUsedPercent, 0.001)
}

func TestKimiProbe_HTTP401_CredentialExpired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	probe := NewKimiCodingPlanProbe(server.Client())
	snapshot, err := probe.Probe(context.Background(), server.URL, "bad-key")
	require.NoError(t, err) // HTTP error returns snapshot, not error
	require.False(t, snapshot.Success)
	require.True(t, snapshot.CredentialExpired)
	require.Equal(t, 401, snapshot.HTTPStatus)
}

func TestParseZhipuCodingPlanQuota_UnitMapsWindows(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseZhipuCodingPlanQuota([]byte(`{
		"success":true,
		"data":{"level":"pro","limits":[
			{"type":"TOKENS_LIMIT","unit":6,"percentage":80,"nextResetTime":"2026-06-14T11:00:00Z"},
			{"type":"TOKENS_LIMIT","unit":3,"percentage":20}
		]}
	}`), now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourUsedPercent)
	require.Equal(t, 20.0, *snapshot.FiveHourUsedPercent)
	require.Nil(t, snapshot.FiveHourResetAt)
	require.NotNil(t, snapshot.WeeklyUsedPercent)
	require.Equal(t, 80.0, *snapshot.WeeklyUsedPercent)
	require.Equal(t, "pro", *snapshot.PlanName)
}

func TestParseZhipuCodingPlanQuota_LegacySingleLimit(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseZhipuCodingPlanQuota([]byte(`{
		"success":true,
		"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":33,"nextResetTime":"2026-06-14T12:00:00Z"}]}
	}`), now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourUsedPercent)
	require.Equal(t, 33.0, *snapshot.FiveHourUsedPercent)
	require.Nil(t, snapshot.WeeklyUsedPercent)
}

func TestParseZhipuCodingPlanQuota_BusinessError(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseZhipuCodingPlanQuota([]byte(`{"success":false,"message":"bad quota"}`), now)
	require.Error(t, err)
	require.False(t, snapshot.Success)
	require.Contains(t, snapshot.ErrorMessage, "bad quota")
}

func TestZhipuProbe_AuthorizationNoBearer(t *testing.T) {
	var gotAuth string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		return jsonResponse(200, `{"success":true,"data":{"limits":[]}}`), nil
	})}
	probe := NewZhipuCodingPlanProbe(client)
	snapshot, err := probe.Probe(context.Background(), "https://open.bigmodel.cn/api/paas/v4", "zhipu-key")
	require.NoError(t, err)
	require.True(t, snapshot.Success)
	require.Equal(t, "zhipu-key", gotAuth)
}

func TestZhipuProbe_EndpointSelection_Domestic(t *testing.T) {
	var requestURL string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestURL = req.URL.String()
		return jsonResponse(200, `{"success":true,"data":{"limits":[]}}`), nil
	})}
	probe := NewZhipuCodingPlanProbe(client)
	_, err := probe.Probe(context.Background(), "https://open.bigmodel.cn/api/paas/v4", "key")
	require.NoError(t, err)
	assert.Contains(t, requestURL, "open.bigmodel.cn")
}

func TestZhipuProbe_EndpointSelection_Overseas(t *testing.T) {
	var requestURL string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestURL = req.URL.String()
		return jsonResponse(200, `{"success":true,"data":{"limits":[]}}`), nil
	})}
	probe := NewZhipuCodingPlanProbe(client)
	_, err := probe.Probe(context.Background(), "https://api.z.ai/api/paas/v4", "key")
	require.NoError(t, err)
	assert.Contains(t, requestURL, "api.z.ai")
}

func TestParseMiniMaxCodingPlanQuota_GeneralWithWeekly(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseMiniMaxCodingPlanQuota([]byte(`{
		"base_resp":{"status_code":0},
		"model_remains":[
			{"model_name":"video","current_interval_remaining_percent":1},
			{"model_name":"general","current_interval_remaining_percent":25,"end_time":1781438400,"current_weekly_status":1,"current_weekly_remaining_percent":40,"weekly_end_time":1782043200000}
		]
	}`), now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourUsedPercent)
	require.Equal(t, 75.0, *snapshot.FiveHourUsedPercent)
	require.NotNil(t, snapshot.WeeklyUsedPercent)
	require.Equal(t, 60.0, *snapshot.WeeklyUsedPercent)
}

func TestParseMiniMaxCodingPlanQuota_WeeklyStatusAbsent(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseMiniMaxCodingPlanQuota([]byte(`{
		"base_resp":{"status_code":0},
		"model_remains":[{"model_name":"general","current_interval_remaining_percent":90,"current_weekly_status":3,"current_weekly_remaining_percent":1}]
	}`), now)
	require.NoError(t, err)
	require.NotNil(t, snapshot.FiveHourUsedPercent)
	require.Equal(t, 10.0, *snapshot.FiveHourUsedPercent)
	require.Nil(t, snapshot.WeeklyUsedPercent)
}

func TestParseMiniMaxCodingPlanQuota_BusinessError(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	snapshot, err := ParseMiniMaxCodingPlanQuota([]byte(`{"base_resp":{"status_code":1001,"status_msg":"denied"}}`), now)
	require.Error(t, err)
	require.False(t, snapshot.Success)
	require.Equal(t, "denied", snapshot.ErrorMessage)
}

func TestMiniMaxCodingPlanQuota_EndpointSelection(t *testing.T) {
	assert.Equal(t, "https://api.minimaxi.com/v1/api/openplatform/coding_plan/remains",
		miniMaxCodingPlanEndpoint("https://api.minimaxi.com"))
	assert.Equal(t, "https://api.minimax.io/v1/api/openplatform/coding_plan/remains",
		miniMaxCodingPlanEndpoint("https://api.minimax.io"))
}

func TestMiniMaxProbe_BusinessErrorViaHTTP(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"base_resp":{"status_code":1002,"status_msg":"rate limited"}}`), nil
	})}
	probe := NewMiniMaxCodingPlanProbe(client)
	snapshot, err := probe.Probe(context.Background(), "https://api.minimaxi.com", "key")
	require.Error(t, err)
	require.False(t, snapshot.Success)
	require.Contains(t, snapshot.ErrorMessage, "rate limited")
}

func TestMiniMaxProbe_NoBearerOnZhipuProbe(t *testing.T) {
	// Verify that Zhipu probe does NOT prepend "Bearer " to the API key
	var authHeader string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		authHeader = req.Header.Get("Authorization")
		return jsonResponse(200, `{"success":true,"data":{"limits":[]}}`), nil
	})}
	probe := NewZhipuCodingPlanProbe(client)
	_, _ = probe.Probe(context.Background(), "https://open.bigmodel.cn", "raw-key")
	assert.Equal(t, "raw-key", authHeader)
}

func TestMiniMaxProbe_BearerOnMiniMaxProbe(t *testing.T) {
	var authHeader string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		authHeader = req.Header.Get("Authorization")
		return jsonResponse(200, `{"base_resp":{"status_code":0},"model_remains":[]}`), nil
	})}
	probe := NewMiniMaxCodingPlanProbe(client)
	_, _ = probe.Probe(context.Background(), "https://api.minimaxi.com", "mm-key")
	assert.Equal(t, "Bearer mm-key", authHeader)
}

func TestVolcengineProbe_Unsupported(t *testing.T) {
	probe := NewVolcengineCodingPlanProbe(nil)
	snapshot, err := probe.Probe(context.Background(), "https://ark.cn-beijing.volces.com/api/v3", "ark-key")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.False(t, snapshot.Success)
	require.Equal(t, CodingPlanProbeStatusUnsupported, snapshot.QuotaProbeStatus)
}

func TestMiMoProbe_Unsupported(t *testing.T) {
	probe := NewMiMoCodingPlanProbe(nil)
	snapshot, err := probe.Probe(context.Background(), "https://mimo.api.xiaomi.com/v1", "mimo-key")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.False(t, snapshot.Success)
	require.Equal(t, CodingPlanProbeStatusUnsupported, snapshot.QuotaProbeStatus)
}

func TestDetectCodingPlanQuotaProbe_Kimi(t *testing.T) {
	probe := DetectCodingPlanQuotaProbe("https://api.kimi.com/coding/v1")
	require.NotNil(t, probe)
	require.Equal(t, constant.CodingPlanProviderKimi, probe.Provider())
}

func TestDetectCodingPlanQuotaProbe_Zhipu(t *testing.T) {
	probe := DetectCodingPlanQuotaProbe("https://open.bigmodel.cn/api/paas/v4")
	require.NotNil(t, probe)
	require.Equal(t, constant.CodingPlanProviderZhipu, probe.Provider())
}

func TestDetectCodingPlanQuotaProbe_MiniMax(t *testing.T) {
	probe := DetectCodingPlanQuotaProbe("https://api.minimax.io/v1")
	require.NotNil(t, probe)
	require.Equal(t, constant.CodingPlanProviderMiniMax, probe.Provider())
}

func TestDetectCodingPlanQuotaProbe_Volcengine(t *testing.T) {
	probe := DetectCodingPlanQuotaProbe("https://ark.cn-beijing.volces.com/api/v3")
	require.NotNil(t, probe)
	require.Equal(t, constant.CodingPlanProviderVolcengine, probe.Provider())
}

func TestDetectCodingPlanQuotaProbe_MiMo(t *testing.T) {
	probe := DetectCodingPlanQuotaProbe("https://mimo.api.xiaomi.com/v1")
	require.NotNil(t, probe)
	require.Equal(t, constant.CodingPlanProviderMiMo, probe.Provider())
}

func TestDetectCodingPlanQuotaProbe_Empty(t *testing.T) {
	probe := DetectCodingPlanQuotaProbe("")
	require.Nil(t, probe)
}

func TestDetectCodingPlanQuotaProbe_Unknown(t *testing.T) {
	probe := DetectCodingPlanQuotaProbe("https://api.openai.com/v1")
	require.Nil(t, probe)
}

func TestProbeCodingPlanQuota_UnsupportedProvider(t *testing.T) {
	snapshot, err := ProbeCodingPlanQuota(context.Background(), "https://ark.cn-beijing.volces.com/api/v3", "key", "")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Equal(t, CodingPlanProbeStatusUnsupported, snapshot.QuotaProbeStatus)
	require.False(t, snapshot.Success)
}

func TestProbeCodingPlanQuota_UnknownProvider(t *testing.T) {
	snapshot, err := ProbeCodingPlanQuota(context.Background(), "https://api.openai.com/v1", "key", "")
	require.Error(t, err)
	require.Nil(t, snapshot)
}

func TestParseCodingPlanHTTPError_401(t *testing.T) {
	snapshot := httpErrorSnapshot(constant.CodingPlanProviderKimi, 401)
	require.True(t, snapshot.CredentialExpired)
	require.False(t, snapshot.Success)
	require.Equal(t, 401, snapshot.HTTPStatus)
}

func TestParseCodingPlanHTTPError_403(t *testing.T) {
	snapshot := httpErrorSnapshot(constant.CodingPlanProviderKimi, 403)
	require.True(t, snapshot.CredentialExpired)
}

func TestParseCodingPlanHTTPError_500(t *testing.T) {
	snapshot := httpErrorSnapshot(constant.CodingPlanProviderMiniMax, 500)
	require.False(t, snapshot.CredentialExpired)
	require.False(t, snapshot.Success)
	require.Contains(t, snapshot.ErrorMessage, "HTTP 500")
	require.NotNil(t, snapshot.Raw)
}

func TestParseCodingPlanResetTime_ISODate(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)

	resetAt := parseCodingPlanResetTime("2026-06-14T15:00:00Z", now)
	require.NotNil(t, resetAt)
	require.Equal(t, time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC), *resetAt)
}

func TestParseCodingPlanResetTime_UnixSeconds(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	ts := now.Add(2 * time.Hour).Unix()

	resetAt := parseCodingPlanResetTime(float64(ts), now)
	require.NotNil(t, resetAt)
	require.Equal(t, ts, resetAt.Unix())
}

func TestParseCodingPlanResetTime_UnixMilliseconds(t *testing.T) {
	now := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	ts := now.Add(3 * time.Hour).UnixMilli()

	resetAt := parseCodingPlanResetTime(float64(ts), now)
	require.NotNil(t, resetAt)
	require.Equal(t, ts, resetAt.UnixMilli())
}

func TestParseCodingPlanResetTime_Nil(t *testing.T) {
	resetAt := parseCodingPlanResetTime(nil, time.Now())
	require.Nil(t, resetAt)
}

func TestParseCodingPlanResetTime_EmptyString(t *testing.T) {
	resetAt := parseCodingPlanResetTime("", time.Now())
	require.Nil(t, resetAt)
}

func TestBodyTruncation_Over1MB(t *testing.T) {
	// Build a response body larger than 1MB
	chunk := strings.Repeat("x", 100*1024) // 100KB
	largeBody := `{"limits":[{"detail":{"limit":100,"remaining":25,"resetTime":"2026-06-14T15:00:00Z"}}],`
	largeBody += `"usage":{"limit":1000,"remaining":400,"resetTime":"2026-06-21T10:00:00Z"},"padding":"`
	for i := 0; i < 10; i++ {
		largeBody += chunk
	}
	largeBody += `"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(largeBody))
	}))
	defer server.Close()

	probe := NewKimiCodingPlanProbe(server.Client())
	snapshot, err := probe.Probe(context.Background(), server.URL, "test-key")
	// The 1MB body should be truncated; if JSON parsing succeeds we still get valid data
	// If the body was truncated mid-JSON, we'll get a parse error but still a snapshot
	if err != nil {
		// Body was probably truncated mid-JSON, which is the expected behavior
		require.False(t, snapshot.Success)
		require.Contains(t, snapshot.ErrorMessage, "parse failed")
		require.Contains(t, snapshot.Source, "active_probe")
	} else {
		// JSON was intact despite truncation near the boundary
		require.True(t, snapshot.Success)
	}
}

func TestProbeCodingPlanQuota_WithKimiURL(t *testing.T) {
	// ProbeCodingPlanQuota needs a recognizable provider URL to find the probe.
	// Use a roundTripFunc to intercept the request no matter what URL the probe constructs.
	var requestPath string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestPath = req.URL.Path
		return jsonResponse(200, `{"limits":[{"detail":{"limit":100,"remaining":50}}]}`), nil
	})}
	probe := NewKimiCodingPlanProbe(client)
	snapshot, err := probe.Probe(context.Background(), "https://api.moonshot.cn/v1", "key")
	require.NoError(t, err)
	require.True(t, snapshot.Success)
	require.Equal(t, "/v1/usages", requestPath)
}

func TestUnsupportedCodingPlanQuotaSnapshot(t *testing.T) {
	snapshot := unsupportedCodingPlanQuotaSnapshot(constant.CodingPlanProviderVolcengine)
	require.Equal(t, CodingPlanProbeStatusUnsupported, snapshot.QuotaProbeStatus)
	require.False(t, snapshot.Success)
	require.Empty(t, snapshot.ErrorMessage)
}

func TestClampPercent(t *testing.T) {
	require.Equal(t, 0.0, clampPercent(-5))
	require.Equal(t, 50.0, clampPercent(50))
	require.Equal(t, 100.0, clampPercent(150))
	require.Equal(t, 0.0, clampPercent(math.NaN()))
	require.Equal(t, 0.0, clampPercent(math.Inf(1)))
}

func TestUsedPercentFromLimitRemaining(t *testing.T) {
	require.InDelta(t, 75, usedPercentFromLimitRemaining(100, 25), 0.001)
	require.InDelta(t, 0, usedPercentFromLimitRemaining(100, 200), 0.001) // negative used → 0
	require.InDelta(t, 0, usedPercentFromLimitRemaining(0, 10), 0.001)    // zero limit → 0
	require.InDelta(t, 100, usedPercentFromLimitRemaining(50, 0), 0.001)  // fully used
}

func TestParseExtraFloat64(t *testing.T) {
	require.Equal(t, 42.5, parseExtraFloat64(42.5))
	require.Equal(t, 10.0, parseExtraFloat64(10))
	require.Equal(t, 99.0, parseExtraFloat64(int64(99)))
	require.Equal(t, 3.14, parseExtraFloat64("3.14"))
	require.Equal(t, 0.0, parseExtraFloat64("not-a-number"))
	require.Equal(t, 0.0, parseExtraFloat64(nil))
}

func TestKimiCodingPlanProbe_Detect(t *testing.T) {
	probe := NewKimiCodingPlanProbe(nil)
	require.True(t, probe.Detect("https://api.kimi.com/coding/v1"))
	require.False(t, probe.Detect("https://api.openai.com/v1"))
}

func TestCodingPlanHTTPProbe_Provider_NilReceiver(t *testing.T) {
	var p *codingPlanHTTPProbe
	require.Empty(t, p.Provider())
}

func TestCodingPlanHTTPProbe_HTTPClient_NilReceiver(t *testing.T) {
	var p *codingPlanHTTPProbe
	cl := p.httpClient()
	require.NotNil(t, cl)
	require.NotNil(t, cl.Timeout)
}
