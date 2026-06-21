package service

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// HandleUpstream429 处理上游 429 响应。
// 移植自 sub2api ratelimit_service.handle429()：
//   - 优先解析 Retry-After 响应头
//   - 尝试从响应体 JSON 中提取 reset_at / retry_after 字段
//   - 兜底默认冷却 5s
// 设置 key 的 RateLimitUntil 为计算出的恢复时间。
func HandleUpstream429(ch *model.Channel, keyIdx int, respHeaders http.Header, responseBody []byte) {
	info := &ch.ChannelInfo
	now := time.Now()

	// 1. 尝试解析 Retry-After 响应头（HTTP 标准）
	if ra := respHeaders.Get("Retry-After"); ra != "" {
		if sec, err := strconv.Atoi(ra); err == nil && sec > 0 {
			setKeyRateLimit(info, keyIdx, now.Add(time.Duration(sec)*time.Second).Unix())
			return
		}
		// 也可能是 HTTP-date 格式
		if t, err := time.Parse(http.TimeFormat, ra); err == nil {
			setKeyRateLimit(info, keyIdx, t.Unix())
			return
		}
	}

	// 2. 尝试从响应体 JSON 中取 reset_after / reset_at / retry_after
	bodyStr := string(responseBody)
	resetSeconds := extractResetSeconds(bodyStr)
	if resetSeconds > 0 {
		setKeyRateLimit(info, keyIdx, now.Add(time.Duration(resetSeconds)*time.Second).Unix())
		return
	}

	// 3. 兜底：默认 5 秒冷却
	setKeyRateLimit(info, keyIdx, now.Add(5*time.Second).Unix())
}

// HandleUpstreamOverload 处理上游 529/503 过载响应。
// 默认冷却 10 分钟（同 sub2api 的 overload_cooldown_minutes=10）。
func HandleUpstreamOverload(ch *model.Channel, keyIdx int, respHeaders http.Header) {
	info := &ch.ChannelInfo
	now := time.Now()

	// 尝试解析 Retry-After
	if ra := respHeaders.Get("Retry-After"); ra != "" {
		if sec, err := strconv.Atoi(ra); err == nil && sec > 0 {
			setKeyOverload(info, keyIdx, now.Add(time.Duration(sec)*time.Second).Unix())
			return
		}
	}
	// 默认 10 分钟
	setKeyOverload(info, keyIdx, now.Add(10*time.Minute).Unix())
}

// HandleUpstreamAuthError 处理上游 401 认证错误。
// 非 OAuth key：直接禁用（设置 status = auto-disabled）。
// OAuth key（带 refresh_token 特征）：设置 TempUnschedulable（10 分钟冷却，
// 给刷新服务窗口），保持 status=enabled 以便刷新服务拾取。
// 移植自 sub2api ratelimit_service 的 401 处理逻辑。
func HandleUpstreamAuthError(ch *model.Channel, keyIdx int, isOAuth bool) {
	info := &ch.ChannelInfo
	now := time.Now()

	if isOAuth {
		// OAuth：临时不可调度 10 分钟，等待 token 刷新
		setKeyTempUnschedulable(info, keyIdx, now.Add(10*time.Minute).Unix())
	} else {
		// API key：直接永久禁用
		if info.MultiKeyStatusList == nil {
			info.MultiKeyStatusList = make(map[int]int)
		}
		info.MultiKeyStatusList[keyIdx] = 3 // auto-disabled
	}
	_ = ch.SaveChannelInfo()
}

// HandleUpstream403 处理上游 403 错误。
// 移植自 sub2api 的三击机制：连续 3 次 403（3 小时窗口）→ 永久禁用；
// 不足 3 次 → 10 分钟 TempUnschedulable。
func HandleUpstream403(ch *model.Channel, keyIdx int) {
	info := &ch.ChannelInfo
	now := time.Now()

	if info.MultiKeyAuthFailCount == nil {
		info.MultiKeyAuthFailCount = make(map[int]int)
	}
	info.MultiKeyAuthFailCount[keyIdx]++

	const threshold = 3
	count := info.MultiKeyAuthFailCount[keyIdx]

	if count >= threshold {
		// 三击 → 永久禁用 key
		if info.MultiKeyStatusList == nil {
			info.MultiKeyStatusList = make(map[int]int)
		}
		info.MultiKeyStatusList[keyIdx] = 3 // auto-disabled
		clearKeyAuthFailCount(info, keyIdx)
	} else {
		// 不足三次 → 10 分钟 TempUnschedulable
		setKeyTempUnschedulable(info, keyIdx, now.Add(10*time.Minute).Unix())
	}
	_ = ch.SaveChannelInfo()
}

// HandleUpstreamSuccess 处理上游成功响应（清除退避状态）。
// 相当于 sub2api 的 ClearRateLimit + ResetOpenAI403Counter。
func HandleUpstreamSuccess(ch *model.Channel, keyIdx int) {
	info := &ch.ChannelInfo
	clearKeyBackoffState(info, keyIdx)
	clearKeyAuthFailCount(info, keyIdx)
	_ = ch.SaveChannelInfo()
}

// ── 内部私有辅助 ──

func setKeyRateLimit(info *model.ChannelInfo, keyIdx int, untilUnix int64) {
	if info.MultiKeyRateLimitUntil == nil {
		info.MultiKeyRateLimitUntil = make(map[int]int64)
	}
	info.MultiKeyRateLimitUntil[keyIdx] = untilUnix
}

func setKeyOverload(info *model.ChannelInfo, keyIdx int, untilUnix int64) {
	if info.MultiKeyOverloadUntil == nil {
		info.MultiKeyOverloadUntil = make(map[int]int64)
	}
	info.MultiKeyOverloadUntil[keyIdx] = untilUnix
}

func setKeyTempUnschedulable(info *model.ChannelInfo, keyIdx int, untilUnix int64) {
	if info.MultiKeyTempUnschedulableUntil == nil {
		info.MultiKeyTempUnschedulableUntil = make(map[int]int64)
	}
	info.MultiKeyTempUnschedulableUntil[keyIdx] = untilUnix
}

func clearKeyBackoffState(info *model.ChannelInfo, keyIdx int) {
	delete(info.MultiKeyRateLimitUntil, keyIdx)
	delete(info.MultiKeyOverloadUntil, keyIdx)
	delete(info.MultiKeyTempUnschedulableUntil, keyIdx)
}

func clearKeyAuthFailCount(info *model.ChannelInfo, keyIdx int) {
	if info.MultiKeyAuthFailCount != nil {
		delete(info.MultiKeyAuthFailCount, keyIdx)
	}
}

// extractResetSeconds 从响应体 JSON 中提取 reset 时间（秒）。
// 支持字段名：retry_after, reset_after, reset_in, resets_in_seconds, rate_limit.reset_seconds
func extractResetSeconds(body string) int64 {
	lower := strings.ToLower(body)

	// 简单字符串搜索常见字段（避免引入完整 JSON 解析依赖）
	fields := []struct {
		prefix string
		offset int
	}{
		{`"retry_after":`, 14},
		{`"reset_after":`, 13},
		{`"reset_in":`, 10},
		{`"resets_in_seconds":`, 19},
	}
	for _, f := range fields {
		if idx := strings.Index(lower, f.prefix); idx >= 0 {
			end := idx + f.offset
			// 跳过空白和冒号，取数字
			remain := lower[end:]
			remain = strings.TrimSpace(remain)
			// 取第一个数字序列
			var numStr string
			for _, c := range remain {
				if c >= '0' && c <= '9' {
					numStr += string(c)
				} else if numStr != "" {
					break
				}
			}
			if numStr != "" {
				if sec, err := strconv.ParseInt(numStr, 10, 64); err == nil && sec > 0 {
					// Gemini 等平台返回毫秒级值，自动转换
					if sec > 3600*24*30 {
						sec = sec / 1000
					}
					return sec
				}
			}
		}
	}
	return 0
}
