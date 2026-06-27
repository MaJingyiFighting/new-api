package service

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/tidwall/gjson"
)

// ProviderErrorDecision is the result of classifying a Coding Plan upstream
// provider error. All fields are safe to read without holding a lock; some are
// mutually exclusive by design (e.g. AuthFailed and QuotaExhausted are never
// both true in the same decision).
type ProviderErrorDecision struct {
	Retryable              bool
	ShouldFailover         bool
	QuotaExhausted         bool
	RateLimited            bool
	Overloaded             bool
	AuthFailed             bool
	TempUnschedulableUntil *time.Time
	Reason                 string
	RateLimitStreak        int
}

// maxCooldown caps any Rate-Limited or Overloaded cooldown so a single bogus
// Retry-After cannot strand a provider for hours.
const maxCooldown = 15 * time.Minute

// ClassifyCodingPlanProviderError classifies an upstream provider response into
// a structured decision. This is a pure function: it reads no database, writes
// no state, and depends only on its parameters.
//
// Classification rules (first-match):
//
//   - 401 / 403     → AuthFailed ("credential_expired")
//   - 429           → RateLimited, ShouldFailover. Cooldown via Retry-After /
//                     rate-limit reset headers, falling back to exponential
//                     backoff keyed on currentStreak.
//   - 529           → Overloaded, ShouldFailover, 1m cooldown.
//   - 5xx           → Overloaded, ShouldFailover, 1m cooldown.
//   - body keywords → QuotaExhausted, ShouldFailover. Cooldown determined by
//                     codingPlanCooldownUntil with the matching window.
func ClassifyCodingPlanProviderError(
	provider constant.CodingPlanProvider,
	statusCode int,
	headers http.Header,
	body []byte,
	currentStreak int,
	lastRateLimitAt time.Time,
	fiveHourResetAt, weeklyResetAt *time.Time,
	now time.Time,
) ProviderErrorDecision {
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if message == "" {
		message = strings.ToLower(strings.TrimSpace(string(body)))
	}

	decision := ProviderErrorDecision{}

	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		decision.AuthFailed = true
		decision.Reason = "credential_expired"

	case statusCode == http.StatusTooManyRequests:
		decision.RateLimited = true
		decision.Retryable = true
		decision.ShouldFailover = true
		decision.Reason = "rate_limited"

		streak := nextCodingPlanRateLimitStreak(currentStreak, lastRateLimitAt, now)
		decision.RateLimitStreak = streak

		if delay, ok := codingPlanRetryAfterDelay(headers, now); ok && delay > 0 {
			if delay > maxCooldown {
				delay = maxCooldown
			}
			until := now.Add(delay)
			decision.TempUnschedulableUntil = &until
		} else {
			backoff := codingPlanRateLimitBackoff(streak)
			until := now.Add(backoff)
			decision.TempUnschedulableUntil = &until
		}

	case statusCode == 529:
		decision.Overloaded = true
		decision.Retryable = true
		decision.ShouldFailover = true
		decision.Reason = "overloaded"
		until := now.Add(time.Minute)
		decision.TempUnschedulableUntil = &until

	case statusCode >= 500 && statusCode <= 599:
		decision.Overloaded = true
		decision.Retryable = true
		decision.ShouldFailover = true
		decision.Reason = "server_error"
		until := now.Add(time.Minute)
		decision.TempUnschedulableUntil = &until
	}

	if containsCodingPlanQuotaExhausted(message) {
		decision.QuotaExhausted = true
		decision.Retryable = true
		decision.ShouldFailover = true
		decision.Reason = "quota_exhausted"

		window := "5h"
		if strings.Contains(message, "week") || strings.Contains(message, "weekly") || strings.Contains(message, "7d") {
			window = "weekly"
		}
		until := codingPlanCooldownUntil(window, fiveHourResetAt, weeklyResetAt, now, 30*time.Minute)
		decision.TempUnschedulableUntil = &until
	}

	if decision.Reason == "" {
		decision.Reason = fmt.Sprintf("%s_http_%d", provider, statusCode)
	}

	return decision
}

// codingPlanCooldownUntil returns the unschedulable-until time for a quota
// exhausted decision. When a concrete reset timestamp is available (via the
// fiveHourResetAt or weeklyResetAt parameters) and lies in the future, that
// timestamp is used; otherwise the caller gets now + fallback.
func codingPlanCooldownUntil(
	window string,
	fiveHourResetAt, weeklyResetAt *time.Time,
	now time.Time,
	fallback time.Duration,
) time.Time {
	var resetAt *time.Time
	switch window {
	case "weekly":
		resetAt = weeklyResetAt
	default:
		resetAt = fiveHourResetAt
	}
	if resetAt != nil && resetAt.After(now) {
		return *resetAt
	}
	return now.Add(fallback)
}

// codingPlanRateLimitBackoff maps a consecutive-429 streak to a cooldown:
//
//	1 → 60s, 2 → 2m, 3 → 5m, 4 → 10m, 5+ → 15m.
func codingPlanRateLimitBackoff(streak int) time.Duration {
	switch {
	case streak <= 1:
		return 60 * time.Second
	case streak == 2:
		return 2 * time.Minute
	case streak == 3:
		return 5 * time.Minute
	case streak == 4:
		return 10 * time.Minute
	default:
		return maxCooldown
	}
}

// nextCodingPlanRateLimitStreak returns the next consecutive-429 streak value.
// The streak resets to 1 when the previous 429 is older than maxCooldown,
// indicating the provider has had enough time to recover.
func nextCodingPlanRateLimitStreak(currentStreak int, lastRateLimitAt, now time.Time) int {
	if currentStreak <= 0 {
		return 1
	}
	if !lastRateLimitAt.IsZero() && now.Sub(lastRateLimitAt) > maxCooldown {
		return 1
	}
	return currentStreak + 1
}

// codingPlanRetryAfterDelay extracts a positive cooldown from the upstream
// response headers. It tries the standard Retry-After header first (both
// delta-seconds and HTTP-date formats), then falls back to common
// provider-specific rate-limit reset headers.
func codingPlanRetryAfterDelay(headers http.Header, now time.Time) (time.Duration, bool) {
	if headers == nil {
		return 0, false
	}

	// 1. Standard Retry-After header.
	if raw := strings.TrimSpace(headers.Get("Retry-After")); raw != "" {
		if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
			if d := time.Duration(seconds * float64(time.Second)); d > 0 {
				return d, true
			}
			return time.Second, true
		}
		if parsed, err := http.ParseTime(raw); err == nil {
			if d := parsed.Sub(now); d > 0 {
				return d, true
			}
			return time.Second, true
		}
	}

	// 2. Provider-specific rate-limit reset headers.
	for _, name := range []string{
		"x-ratelimit-reset",
		"ratelimit-reset",
		"x-ratelimit-reset-requests",
		"x-ratelimit-reset-tokens",
	} {
		raw := strings.TrimSpace(headers.Get(name))
		if raw == "" {
			continue
		}
		if d, ok := parseCodingPlanResetHeaderValue(raw, now); ok {
			return d, true
		}
	}

	return 0, false
}

// parseCodingPlanResetHeaderValue interprets a rate-limit reset header value.
// Providers express this either as a Go-style duration ("12s", "1m"), as a
// delta in seconds (a small number like "120"), or as an absolute Unix
// timestamp (e.g. "1700000000").
func parseCodingPlanResetHeaderValue(raw string, now time.Time) (time.Duration, bool) {
	// Try as a Go duration ("12s", "1m", "2h").
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d, true
	}

	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		return 0, false
	}

	// A value large enough to be a Unix timestamp is treated as an absolute
	// reset time; otherwise it is a delta in seconds.
	if seconds > 1_000_000_000 {
		resetAt := unixFlexibleTime(int64(seconds))
		if resetAt == nil {
			return 0, false
		}
		if d := resetAt.Sub(now); d > 0 {
			return d, true
		}
		return time.Second, true
	}

	return time.Duration(seconds * float64(time.Second)), true
}

// unixFlexibleTime converts a Unix timestamp (seconds or milliseconds) to a
// time.Time. Values above 1e12 are treated as milliseconds; lower values as
// seconds. Returns nil for non-positive input.
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

// containsCodingPlanQuotaExhausted checks whether a lowercased upstream error
// message contains any of the known quota-exhausted keywords.
func containsCodingPlanQuotaExhausted(message string) bool {
	if message == "" {
		return false
	}
	keywords := []string{
		"quota exhausted",
		"insufficient quota",
		"limit exceeded",
		"usage limit",
		"rate limit exceeded",
		"afp exhausted",
		"余额不足",
		"配额不足",
		"额度不足",
		"配额已用尽",
		"额度已用尽",
		"quota reached",
		"daily limit",
		"monthly limit",
		"out of quota",
		"quota exceed",
		"流量耗尽",
	}
	for _, keyword := range keywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}
	return false
}

// extractUpstreamErrorMessage extracts a human-readable error message from an
// upstream provider response body. It probes JSON structures in order:
//
//  1. error.message (OpenAI / Claude style)
//  2. detail          (ChatGPT internal API style)
//  3. message         (generic fallback)
//
// Returns "" when no message can be extracted.
func extractUpstreamErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	// Claude / OpenAI style: {"error":{"message":"..."}}
	if m := gjson.GetBytes(body, "error.message").String(); strings.TrimSpace(m) != "" {
		inner := strings.TrimSpace(m)
		if strings.HasPrefix(inner, "{") {
			if innerMsg := gjson.Get(inner, "error.message").String(); strings.TrimSpace(innerMsg) != "" {
				return innerMsg
			}
		}
		return m
	}

	// ChatGPT internal API style: {"detail":"..."}
	if d := gjson.GetBytes(body, "detail").String(); strings.TrimSpace(d) != "" {
		return d
	}

	// Fallback: top-level message field.
	return gjson.GetBytes(body, "message").String()
}
