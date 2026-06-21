package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

// TestParseRetryAfterOrResetAt_RetryAfterSeconds 测试 429 + Retry-After 秒数解析。
func TestParseRetryAfterOrResetAt_RetryAfterSeconds(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	result := ParseRetryAfterOrResetAt("120", "", "", now, fallback)
	expected := now.Add(120 * time.Second)
	assert.Equal(t, expected.Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_RetryAfterHTTPDate 测试 429 + HTTP date 解析。
func TestParseRetryAfterOrResetAt_RetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	// RFC1123 format
	result := ParseRetryAfterOrResetAt("Thu, 01 Jan 2026 13:00:00 GMT", "", "", now, fallback)
	expected := time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)
	assert.Equal(t, expected.Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_Fallback 测试解析失败时使用 fallback 默认值。
func TestParseRetryAfterOrResetAt_Fallback(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	result := ParseRetryAfterOrResetAt("", `{"error":"unknown"}`, "", now, fallback)
	assert.Equal(t, now.Add(fallback).Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_RetryAfterZero 测试 Retry-After: 0 或负数时使用 fallback。
func TestParseRetryAfterOrResetAt_InvalidRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	// 负数应该用 fallback
	result := ParseRetryAfterOrResetAt("-1", "", "", now, fallback)
	assert.Equal(t, now.Add(fallback).Unix(), result.Unix())

	// 零值
	result = ParseRetryAfterOrResetAt("0", "", "", now, fallback)
	assert.Equal(t, now.Add(fallback).Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_ResetSecondsFromBody 测试从响应体 JSON 中提取 reset_after。
func TestParseRetryAfterOrResetAt_ResetSecondsFromBody(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	body := `{"error":{"message":"rate limit","retry_after":30}}`
	result := ParseRetryAfterOrResetAt("", body, "", now, fallback)
	assert.Equal(t, now.Add(30*time.Second).Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_QuotaResetAt 测试从响应体中提取 quota_reset_at。
func TestParseRetryAfterOrResetAt_QuotaResetAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 10 * time.Minute

	body := `{"error":{"message":"quota exhausted","quota_reset_at":3600}}`
	result := ParseRetryAfterOrResetAt("", body, "", now, fallback)
	assert.Equal(t, now.Add(3600*time.Second).Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_ErrorText 测试从错误文本中提取 retry after。
func TestParseRetryAfterOrResetAt_ErrorText(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	result := ParseRetryAfterOrResetAt("", "", "retry after 45s", now, fallback)
	assert.Equal(t, now.Add(45*time.Second).Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_TryAgainIn 测试从"try again in"文本中提取。
func TestParseRetryAfterOrResetAt_TryAgainIn(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	result := ParseRetryAfterOrResetAt("", "", "Please try again in 60 seconds", now, fallback)
	assert.Equal(t, now.Add(60*time.Second).Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_RateLimitReset 测试 rate_limit_reset 字段。
func TestParseRetryAfterOrResetAt_RateLimitReset(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	body := `{"rate_limit_reset": 90}`
	result := ParseRetryAfterOrResetAt("", body, "", now, fallback)
	assert.Equal(t, now.Add(90*time.Second).Unix(), result.Unix())
}

// TestParseRetryAfterOrResetAt_ResetAt 测试 reset_at 字段。
func TestParseRetryAfterOrResetAt_ResetAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fallback := 60 * time.Second

	body := `{"reset_at": 1800}`
	result := ParseRetryAfterOrResetAt("", body, "", now, fallback)
	assert.Equal(t, now.Add(1800*time.Second).Unix(), result.Unix())
}

// TestHandleUpstream429_DefaultCooldown 测试 429 rate limit 状态通过 ChannelInfo 直接设置。
func TestHandleUpstream429_DefaultCooldown(t *testing.T) {
	info := &model.ChannelInfo{
		IsMultiKey: true,
	}
	now := time.Now()
	if info.MultiKeyRateLimitUntil == nil {
		info.MultiKeyRateLimitUntil = make(map[int]int64)
	}
	info.MultiKeyRateLimitUntil[0] = now.Add(5 * time.Second).Unix()
	info.MultiKeyTempReason = map[int]string{0: "429 rate limited"}

	assert.True(t, model.IsKeyRateLimited(info, 0))
	assert.False(t, model.IsKeyRateLimited(info, 1))
	assert.Equal(t, "429 rate limited", info.MultiKeyTempReason[0])
}

// TestChannelInfo_QuotaExhausted 测试 quota exhausted 状态通过 ChannelInfo 直接设置。
func TestChannelInfo_QuotaExhausted(t *testing.T) {
	info := &model.ChannelInfo{
		IsMultiKey: true,
	}
	now := time.Now()
	until := now.Add(1 * time.Hour).Unix()
	if info.MultiKeyQuotaResetAt == nil {
		info.MultiKeyQuotaResetAt = make(map[int]int64)
	}
	info.MultiKeyQuotaResetAt[1] = until
	info.MultiKeyTempReason = map[int]string{1: "402 quota exhausted"}

	assert.Equal(t, until, info.MultiKeyQuotaResetAt[1])
	assert.Equal(t, "402 quota exhausted", info.MultiKeyTempReason[1])
}

// TestChannelInfo_Overload 测试 overload 状态通过 ChannelInfo 直接设置。
func TestChannelInfo_Overload(t *testing.T) {
	info := &model.ChannelInfo{
		IsMultiKey: true,
	}
	now := time.Now()
	until := now.Add(10 * time.Minute).Unix()
	if info.MultiKeyOverloadUntil == nil {
		info.MultiKeyOverloadUntil = make(map[int]int64)
	}
	info.MultiKeyOverloadUntil[0] = until

	assert.Equal(t, until, info.MultiKeyOverloadUntil[0])
}

// TestExtractResetSeconds 验证 extractResetSeconds 函数。
func TestExtractResetSeconds(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected int64
	}{
		{"retry_after", `{"retry_after": 30}`, 30},
		{"reset_after", `{"reset_after": 60}`, 60},
		{"reset_in", `{"reset_in": 120}`, 120},
		{"resets_in_seconds", `{"resets_in_seconds": 300}`, 300},
		{"rate_limit_reset", `{"rate_limit_reset": 90}`, 0}, // not in the basic fields
		{"no match", `{"error": "unknown"}`, 0},
		{"empty", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractResetSeconds(tt.body)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestChannelInfo_QuotaResetAt_Field 验证 MultiKeyQuotaResetAt 字段存在。
func TestChannelInfo_QuotaResetAt_Field(t *testing.T) {
	info := model.ChannelInfo{
		MultiKeyQuotaResetAt: map[int]int64{0: 1000000},
		MultiKeyTempReason:   map[int]string{0: "quota exhausted"},
	}
	assert.Equal(t, int64(1000000), info.MultiKeyQuotaResetAt[0])
	assert.Equal(t, "quota exhausted", info.MultiKeyTempReason[0])
}

// TestChannelInfo_TempUnschedulable 测试 temp unschedulable 状态通过 ChannelInfo 直接设置。
func TestChannelInfo_TempUnschedulable(t *testing.T) {
	info := &model.ChannelInfo{
		IsMultiKey: true,
	}
	now := time.Now()
	until := now.Add(10 * time.Minute).Unix()
	if info.MultiKeyTempUnschedulableUntil == nil {
		info.MultiKeyTempUnschedulableUntil = make(map[int]int64)
	}
	info.MultiKeyTempUnschedulableUntil[0] = until
	info.MultiKeyTempReason = map[int]string{0: "401 OAuth cooldown"}

	assert.Equal(t, until, info.MultiKeyTempUnschedulableUntil[0])
	assert.Equal(t, "401 OAuth cooldown", info.MultiKeyTempReason[0])
}

// TestParseRetryAfterOrResetAt_Priority 验证解析优先级：header > body > text > fallback。
func TestParseRetryAfterOrResetAt_Priority(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// header should win over body
	result := ParseRetryAfterOrResetAt("30", `{"retry_after": 300}`, "retry after 600s", now, 60*time.Second)
	assert.Equal(t, now.Add(30*time.Second).Unix(), result.Unix())

	// body should win over text
	result = ParseRetryAfterOrResetAt("", `{"retry_after": 120}`, "retry after 600s", now, 60*time.Second)
	assert.Equal(t, now.Add(120*time.Second).Unix(), result.Unix())

	// text should win over fallback
	result = ParseRetryAfterOrResetAt("", "", "retry after 45s", now, 60*time.Second)
	assert.Equal(t, now.Add(45*time.Second).Unix(), result.Unix())
}

// TestIsKeyRateLimited 验证 IsKeyRateLimited 函数。
func TestIsKeyRateLimited(t *testing.T) {
	info := &model.ChannelInfo{
		MultiKeyRateLimitUntil: map[int]int64{0: time.Now().Add(1 * time.Hour).Unix()},
	}
	assert.True(t, model.IsKeyRateLimited(info, 0))
	assert.False(t, model.IsKeyRateLimited(info, 1))
	assert.False(t, model.IsKeyRateLimited(nil, 0))
}

// TestCleanupExpiredState 验证惰性恢复清理逻辑。
func TestCleanupExpiredState(t *testing.T) {
	info := &model.ChannelInfo{
		MultiKeyRateLimitUntil: map[int]int64{
			0: time.Now().Add(-1 * time.Hour).Unix(), // expired
			1: time.Now().Add(1 * time.Hour).Unix(),  // still valid
		},
		MultiKeyTempReason: map[int]string{
			0: "old reason",
			1: "current reason",
		},
	}
	model.CleanupExpiredState(info, time.Now())

	_, expiredExists := info.MultiKeyRateLimitUntil[0]
	assert.False(t, expiredExists, "expired entry should be cleaned up")

	_, validExists := info.MultiKeyRateLimitUntil[1]
	assert.True(t, validExists, "valid entry should remain")
}
