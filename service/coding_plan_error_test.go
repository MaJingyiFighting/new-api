package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timePtr is a small helper that returns a pointer to a copy of t.
func timePtr(t time.Time) *time.Time { return &t }

func TestClassifyCodingPlanProviderError_401_AuthFailed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderKimi, http.StatusUnauthorized,
		nil, nil, 0, time.Time{}, nil, nil, now,
	)
	assert.True(t, decision.AuthFailed, "401 should set AuthFailed")
	assert.False(t, decision.Retryable)
	assert.False(t, decision.ShouldFailover)
	assert.False(t, decision.RateLimited)
	assert.False(t, decision.Overloaded)
	assert.False(t, decision.QuotaExhausted)
	assert.Equal(t, "credential_expired", decision.Reason)
	assert.Nil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, 0, decision.RateLimitStreak)
}

func TestClassifyCodingPlanProviderError_403_AuthFailed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderZhipu, http.StatusForbidden,
		nil, nil, 0, time.Time{}, nil, nil, now,
	)
	assert.True(t, decision.AuthFailed, "403 should set AuthFailed")
	assert.Equal(t, "credential_expired", decision.Reason)
	assert.Nil(t, decision.TempUnschedulableUntil)
}

func TestClassifyCodingPlanProviderError_429_Streak1_Backoff60s(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// currentStreak=0, lastRateLimitAt=zero → resets to streak 1 → 60s backoff.
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderMiniMax, http.StatusTooManyRequests,
		nil, nil, 0, time.Time{}, nil, nil, now,
	)
	assert.True(t, decision.RateLimited)
	assert.True(t, decision.Retryable)
	assert.True(t, decision.ShouldFailover)
	assert.Equal(t, 1, decision.RateLimitStreak)
	require.NotNil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, now.Add(60*time.Second).Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_429_Streak4_Backoff10m(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// currentStreak=3, lastRateLimitAt=5m ago (<15m) → streak becomes 4 → 10m.
	lastRateLimit := now.Add(-5 * time.Minute)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderVolcengine, http.StatusTooManyRequests,
		nil, nil, 3, lastRateLimit, nil, nil, now,
	)
	assert.True(t, decision.RateLimited)
	assert.Equal(t, 4, decision.RateLimitStreak)
	require.NotNil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, now.Add(10*time.Minute).Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_429_RetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	headers := http.Header{"Retry-After": {"120"}}
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderKimi, http.StatusTooManyRequests,
		headers, nil, 0, time.Time{}, nil, nil, now,
	)
	require.NotNil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, now.Add(120*time.Second).Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_429_RetryAfterCapped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// 1 hour → capped to 15 minutes.
	headers := http.Header{"Retry-After": {"3600"}}
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderZhipu, http.StatusTooManyRequests,
		headers, nil, 0, time.Time{}, nil, nil, now,
	)
	require.NotNil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, now.Add(maxCooldown).Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_429_RateLimitResetHeader(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// Unix timestamp ~ 2 minutes from now as a float in the header.
	resetUnix := now.Add(2 * time.Minute).Unix()
	headers := http.Header{"X-Ratelimit-Reset": {formatInt(int(resetUnix))}}
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderKimi, http.StatusTooManyRequests,
		headers, nil, 0, time.Time{}, nil, nil, now,
	)
	require.NotNil(t, decision.TempUnschedulableUntil)
	// Allow a 1-second tolerance for rounding.
	assert.InDelta(t, now.Add(2*time.Minute).Unix(), decision.TempUnschedulableUntil.Unix(), 1)
}

func TestClassifyCodingPlanProviderError_529_Overloaded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderKimi, 529,
		nil, nil, 0, time.Time{}, nil, nil, now,
	)
	assert.True(t, decision.Overloaded)
	assert.True(t, decision.Retryable)
	assert.True(t, decision.ShouldFailover)
	assert.Equal(t, "overloaded", decision.Reason)
	require.NotNil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, now.Add(time.Minute).Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_5xx_Overloaded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	for _, code := range []int{500, 502, 503, 504} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()
			decision := ClassifyCodingPlanProviderError(
				constant.CodingPlanProviderMiniMax, code,
				nil, nil, 0, time.Time{}, nil, nil, now,
			)
			assert.True(t, decision.Overloaded, "5xx should set Overloaded")
			assert.True(t, decision.Retryable)
			assert.True(t, decision.ShouldFailover)
			assert.Equal(t, "server_error", decision.Reason)
			require.NotNil(t, decision.TempUnschedulableUntil)
			assert.Equal(t, now.Add(time.Minute).Unix(), decision.TempUnschedulableUntil.Unix())
		})
	}
}

func TestClassifyCodingPlanProviderError_QuotaExhausted_BodyKeywords(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"error":{"message":"余额不足"}}`)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderKimi, 400,
		nil, body, 0, time.Time{}, nil, nil, now,
	)
	assert.True(t, decision.QuotaExhausted)
	assert.True(t, decision.Retryable)
	assert.True(t, decision.ShouldFailover)
	assert.Equal(t, "quota_exhausted", decision.Reason)
	require.NotNil(t, decision.TempUnschedulableUntil)
	// No fiveHourResetAt set → falls back to now + 30m.
	assert.Equal(t, now.Add(30*time.Minute).Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_QuotaExhausted_WeeklyWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	weeklyReset := now.Add(2 * time.Hour)
	body := []byte(`{"error":{"message":"weekly quota exhausted"}}`)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderKimi, 400,
		nil, body, 0, time.Time{}, nil, timePtr(weeklyReset), now,
	)
	assert.True(t, decision.QuotaExhausted)
	require.NotNil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, weeklyReset.Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_QuotaExhausted_FiveHourWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	fiveHourReset := now.Add(1 * time.Hour)
	body := []byte(`{"error":{"message":"quota exhausted"}}`)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderKimi, 400,
		nil, body, 0, time.Time{}, timePtr(fiveHourReset), nil, now,
	)
	assert.True(t, decision.QuotaExhausted)
	require.NotNil(t, decision.TempUnschedulableUntil)
	assert.Equal(t, fiveHourReset.Unix(), decision.TempUnschedulableUntil.Unix())
}

func TestClassifyCodingPlanProviderError_UnhandledStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderMiMo, http.StatusBadRequest,
		nil, nil, 0, time.Time{}, nil, nil, now,
	)
	assert.False(t, decision.AuthFailed)
	assert.False(t, decision.RateLimited)
	assert.False(t, decision.Overloaded)
	assert.False(t, decision.QuotaExhausted)
	assert.Equal(t, "mimo_http_400", decision.Reason)
}

// --- Pure function tests (no DB, no Account) --------------------------------

func TestNextCodingPlanRateLimitStreak_ResetsWhenStale(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// lastRateLimitAt is 20 minutes ago, > maxCooldown.
	lastAt := now.Add(-20 * time.Minute)
	assert.Equal(t, 1, nextCodingPlanRateLimitStreak(5, lastAt, now))
}

func TestNextCodingPlanRateLimitStreak_IncrementsWhenRecent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// lastRateLimitAt is 5 minutes ago, < maxCooldown.
	lastAt := now.Add(-5 * time.Minute)
	assert.Equal(t, 3, nextCodingPlanRateLimitStreak(2, lastAt, now))
}

func TestNextCodingPlanRateLimitStreak_ZeroOrNegativeResets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	lastAt := now.Add(-1 * time.Minute)
	assert.Equal(t, 1, nextCodingPlanRateLimitStreak(0, lastAt, now))
	assert.Equal(t, 1, nextCodingPlanRateLimitStreak(-3, lastAt, now))
}

func TestNextCodingPlanRateLimitStreak_ZeroTimeIncrements(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// Zero time means "no prior rate limit recorded" → can't verify staleness
	// → just increment the streak.
	assert.Equal(t, 4, nextCodingPlanRateLimitStreak(3, time.Time{}, now))
}

func TestCodingPlanRateLimitBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		streak   int
		expected time.Duration
	}{
		{0, 60 * time.Second},  // <=1 → 60s
		{1, 60 * time.Second},  // 1 → 60s
		{2, 2 * time.Minute},   // 2 → 2m
		{3, 5 * time.Minute},   // 3 → 5m
		{4, 10 * time.Minute},  // 4 → 10m
		{5, maxCooldown},       // 5+ → 15m
		{10, maxCooldown},      // 5+ → 15m
	}
	for _, tc := range tests {
		tc := tc
		t.Run(formatInt(tc.streak), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, codingPlanRateLimitBackoff(tc.streak))
		})
	}
}

func TestCodingPlanCooldownUntil_FiveHour(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(1 * time.Hour)
	result := codingPlanCooldownUntil("5h", &resetAt, nil, now, 30*time.Minute)
	assert.Equal(t, resetAt.Unix(), result.Unix())
}

func TestCodingPlanCooldownUntil_Weekly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	result := codingPlanCooldownUntil("weekly", nil, &resetAt, now, 30*time.Minute)
	assert.Equal(t, resetAt.Unix(), result.Unix())
}

func TestCodingPlanCooldownUntil_ExpiredResetFallsBack(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-1 * time.Hour)
	result := codingPlanCooldownUntil("5h", &expired, nil, now, 30*time.Minute)
	assert.Equal(t, now.Add(30*time.Minute).Unix(), result.Unix())
}

func TestCodingPlanCooldownUntil_NilResetFallsBack(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	result := codingPlanCooldownUntil("5h", nil, nil, now, 30*time.Minute)
	assert.Equal(t, now.Add(30*time.Minute).Unix(), result.Unix())
}

func TestCodingPlanRetryAfterDelay_RetryAfterSeconds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	headers := http.Header{"Retry-After": {"90"}}
	d, ok := codingPlanRetryAfterDelay(headers, now)
	assert.True(t, ok)
	assert.Equal(t, 90*time.Second, d)
}

func TestCodingPlanRetryAfterDelay_RetryAfterHTTPDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	future := now.Add(2 * time.Minute)
	headers := http.Header{"Retry-After": {future.Format(http.TimeFormat)}}
	d, ok := codingPlanRetryAfterDelay(headers, now)
	assert.True(t, ok)
	assert.Equal(t, 2*time.Minute, d)
}

func TestCodingPlanRetryAfterDelay_RetryAfterZero(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	// Retry-After: 0 → signaled as rate limited → return 1s.
	headers := http.Header{"Retry-After": {"0"}}
	d, ok := codingPlanRetryAfterDelay(headers, now)
	assert.True(t, ok)
	assert.Equal(t, time.Second, d)
}

func TestCodingPlanRetryAfterDelay_RateLimitResetHeader(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	headers := http.Header{"X-Ratelimit-Reset": {"120"}}
	d, ok := codingPlanRetryAfterDelay(headers, now)
	assert.True(t, ok)
	assert.Equal(t, 120*time.Second, d)
}

func TestCodingPlanRetryAfterDelay_NilHeaders(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	d, ok := codingPlanRetryAfterDelay(nil, now)
	assert.False(t, ok)
	assert.Equal(t, time.Duration(0), d)
}

func TestParseCodingPlanResetHeaderValue_Duration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	d, ok := parseCodingPlanResetHeaderValue("30s", now)
	assert.True(t, ok)
	assert.Equal(t, 30*time.Second, d)

	d, ok = parseCodingPlanResetHeaderValue("5m", now)
	assert.True(t, ok)
	assert.Equal(t, 5*time.Minute, d)
}

func TestParseCodingPlanResetHeaderValue_UnixTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	futureUnix := now.Add(3 * time.Minute).Unix()
	d, ok := parseCodingPlanResetHeaderValue(formatInt(int(futureUnix)), now)
	assert.True(t, ok)
	assert.InDelta(t, 3*time.Minute, d, float64(time.Second))
}

func TestParseCodingPlanResetHeaderValue_Invalid(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	d, ok := parseCodingPlanResetHeaderValue("", now)
	assert.False(t, ok)
	assert.Equal(t, time.Duration(0), d)

	d, ok = parseCodingPlanResetHeaderValue("not-a-number", now)
	assert.False(t, ok)
	assert.Equal(t, time.Duration(0), d)

	d, ok = parseCodingPlanResetHeaderValue("-10", now)
	assert.False(t, ok)
	assert.Equal(t, time.Duration(0), d)
}

func TestContainsCodingPlanQuotaExhausted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		message  string
		expected bool
	}{
		// English keywords
		{"quota exhausted exact", "quota exhausted", true},
		{"insufficient quota", "insufficient quota for this request", true},
		{"limit exceeded", "rate limit exceeded", true},
		{"usage limit", "usage limit reached", true},
		{"rate limit exceeded", "your request was rate limit exceeded", true},
		{"afp exhausted", "afp exhausted for today", true},
		{"quota reached", "your quota reached", true},
		{"daily limit", "daily limit exceeded", true},
		{"monthly limit", "monthly limit reached", true},
		{"out of quota", "you are out of quota", true},
		{"quota exceed", "your quota exceed the limit", true},

		// Chinese keywords
		{"余额不足", "您的账户余额不足", true},
		{"配额不足", "当前配额不足", true},
		{"额度不足", "您的额度不足", true},
		{"配额已用尽", "您的配额已用尽", true},
		{"额度已用尽", "您的额度已用尽", true},
		{"流量耗尽", "本月流量耗尽", true},

		// False positives (should not match)
		{"empty", "", false},
		{"no match", "everything is fine", false},
		{"close but not exact", "exhausted", false},
		{"partial quota", "quota check is ok", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, containsCodingPlanQuotaExhausted(tc.message))
		})
	}
}

func TestExtractUpstreamErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     []byte
		expected string
	}{
		{"nil body", nil, ""},
		{"empty body", []byte{}, ""},
		{"OpenAI style", []byte(`{"error":{"message":"Rate limit exceeded"}}`), "Rate limit exceeded"},
		{"nested inner error", []byte(`{"error":{"message":"{\"error\":{\"message\":\"inner error\"}}"}}`), "inner error"},
		{"Claude style detail", []byte(`{"detail":"not found"}`), "not found"},
		{"generic message", []byte(`{"message":"server error"}`), "server error"},
		{"no message", []byte(`{"code":500}`), ""},
		{"no JSON match", []byte(`plain text`), ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, extractUpstreamErrorMessage(tc.body))
		})
	}
}

func TestUnhandledStatusWithBodyKeyword(t *testing.T) {
	t.Parallel()

	// QuotaExhausted should fire regardless of status code when the body
	// contains a matching keyword.
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"error":{"message":"quota exhausted"}}`)
	decision := ClassifyCodingPlanProviderError(
		constant.CodingPlanProviderZhipu, http.StatusOK,
		nil, body, 0, time.Time{}, nil, nil, now,
	)
	assert.True(t, decision.QuotaExhausted)
	assert.True(t, decision.Retryable)
	assert.True(t, decision.ShouldFailover)
	assert.Equal(t, "quota_exhausted", decision.Reason)
}

// formatInt is a test helper that converts an int to a string without
// importing strconv separately in every test.
func formatInt(v int) string {
	if v < 10 {
		return string(rune('0' + v))
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
