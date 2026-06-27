package service

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

// HandleUpstreamSuccess 处理上游成功响应（清除退避状态 + load_aware 运行时统计）。
// 相当于 sub2api 的 ClearRateLimit + ResetOpenAI403Counter。
// 当 channel 处于 load_aware 模式时,额外更新 MultiKeyLastUsedAt。
func HandleUpstreamSuccess(ch *model.Channel, keyIdx int) {
	info := &ch.ChannelInfo
	clearKeyBackoffState(info, keyIdx)
	clearKeyAuthFailCount(info, keyIdx)
	if info.MultiKeyMode == constant.MultiKeyModeLoadAware {
		if info.MultiKeyLastUsedAt == nil {
			info.MultiKeyLastUsedAt = make(map[int]int64)
		}
		info.MultiKeyLastUsedAt[keyIdx] = time.Now().Unix()
	}
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

func setKeyQuotaReset(info *model.ChannelInfo, keyIdx int, untilUnix int64) {
	if info.MultiKeyQuotaResetAt == nil {
		info.MultiKeyQuotaResetAt = make(map[int]int64)
	}
	info.MultiKeyQuotaResetAt[keyIdx] = untilUnix
}

func setKeyTempReason(info *model.ChannelInfo, keyIdx int, reason string) {
	if info.MultiKeyTempReason == nil {
		info.MultiKeyTempReason = make(map[int]string)
	}
	info.MultiKeyTempReason[keyIdx] = reason
}

func clearKeyBackoffState(info *model.ChannelInfo, keyIdx int) {
	delete(info.MultiKeyRateLimitUntil, keyIdx)
	delete(info.MultiKeyOverloadUntil, keyIdx)
	delete(info.MultiKeyTempUnschedulableUntil, keyIdx)
	delete(info.MultiKeyQuotaResetAt, keyIdx)
	delete(info.MultiKeyTempReason, keyIdx)
}

func clearKeyAuthFailCount(info *model.ChannelInfo, keyIdx int) {
	if info.MultiKeyAuthFailCount != nil {
		delete(info.MultiKeyAuthFailCount, keyIdx)
	}
}

// ParseRetryAfterOrResetAt 解析上游响应中的重试等待时间。
// 解析来源优先级：
//  1. Retry-After header（秒数或 HTTP-date）
//  2. response body JSON 字段：retry_after, reset_after, reset_at, rate_limit_reset, quota_reset_at
//  3. 错误文本中的简单模式：retry after 12s, try again in 60 seconds
//  4. fallback 默认值
func ParseRetryAfterOrResetAt(upstreamRetryAfter string, errBody string, errMsg string, now time.Time, fallback time.Duration) time.Time {
	// 1. Retry-After header
	if upstreamRetryAfter != "" {
		if sec, err := strconv.Atoi(upstreamRetryAfter); err == nil && sec > 0 {
			return now.Add(time.Duration(sec) * time.Second)
		}
		if t, err := time.Parse(http.TimeFormat, upstreamRetryAfter); err == nil {
			return t
		}
	}
	// 2. response body JSON
	if errBody != "" {
		sec := extractResetSeconds(errBody)
		if sec > 0 {
			return now.Add(time.Duration(sec) * time.Second)
		}
		// try additional fields: rate_limit_reset, quota_reset_at
		lower := strings.ToLower(errBody)
		additionalFields := []struct {
			prefix string
			offset int
		}{
			{`"rate_limit_reset":`, 18},
			{`"quota_reset_at":`, 16},
			{`"reset_at":`, 10},
		}
		for _, f := range additionalFields {
			if idx := strings.Index(lower, f.prefix); idx >= 0 {
				end := idx + f.offset
				remain := strings.TrimSpace(lower[end:])
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
						if sec > 3600*24*30 {
							sec = sec / 1000
						}
						return now.Add(time.Duration(sec) * time.Second)
					}
				}
			}
		}
	}
	// 3. error text patterns
	if errMsg != "" {
		lower := strings.ToLower(errMsg)
		if idx := strings.Index(lower, "retry after "); idx >= 0 {
			remain := lower[idx+len("retry after "):]
			var numStr string
			for _, c := range remain {
				if c >= '0' && c <= '9' {
					numStr += string(c)
				} else if numStr != "" {
					break
				}
			}
			if sec, err := strconv.ParseInt(numStr, 10, 64); err == nil && sec > 0 {
				return now.Add(time.Duration(sec) * time.Second)
			}
		}
		if idx := strings.Index(lower, "try again in "); idx >= 0 {
			remain := lower[idx+len("try again in "):]
			var numStr string
			for _, c := range remain {
				if c >= '0' && c <= '9' {
					numStr += string(c)
				} else if numStr != "" {
					break
				}
			}
			if sec, err := strconv.ParseInt(numStr, 10, 64); err == nil && sec > 0 {
				return now.Add(time.Duration(sec) * time.Second)
			}
		}
	}
	// 4. fallback
	return now.Add(fallback)
}

// TemporarilyDisableChannelKey 对 multi-key channel 的单个 key 执行临时冻结。
// kind: "rate_limit" | "overload" | "quota_exhausted" | "temp_unschedulable"
// 不会将 key 写入 MultiKeyStatusList（永久禁用），也不会禁用整个 channel。
func TemporarilyDisableChannelKey(channelID int, keyIdx int, kind string, untilUnix int64, reason string) {
	ch, cacheErr := model.CacheGetChannel(channelID)
	if cacheErr != nil || ch == nil || !ch.ChannelInfo.IsMultiKey {
		return
	}
	if keyIdx < 0 || keyIdx >= len(ch.GetKeys()) {
		return
	}
	info := &ch.ChannelInfo

	lock := model.GetChannelPollingLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	setKeyTempReason(info, keyIdx, reason)
	switch kind {
	case "rate_limit":
		setKeyRateLimit(info, keyIdx, untilUnix)
	case "overload":
		setKeyOverload(info, keyIdx, untilUnix)
	case "quota_exhausted":
		setKeyQuotaReset(info, keyIdx, untilUnix)
	case "temp_unschedulable":
		setKeyTempUnschedulable(info, keyIdx, untilUnix)
	}
	_ = ch.SaveChannelInfo()
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

// ============================================================
// Agent B additions: load_aware runtime stats + 429 storm detection
// ============================================================

// RecordKeyUsage 记录 key 的最后使用时间,用于 load_aware 模式 LRU 决策.
// 该函数在每次成功或失败的 key 调度之后被调用,以保持 LastUsedAt 最新.
// 仅当 channel 处于 load_aware 模式时,才会更新 MultiKeyLastUsedAt 并持久化.
func RecordKeyUsage(channelId int, keyIdx int) {
	if channelId <= 0 || keyIdx < 0 {
		return
	}
	channel, err := model.CacheGetChannel(channelId)
	if err != nil || channel == nil {
		return
	}
	if !channel.ChannelInfo.IsMultiKey {
		return
	}
	if channel.ChannelInfo.MultiKeyMode != constant.MultiKeyModeLoadAware {
		return
	}
	if channel.ChannelInfo.MultiKeyLastUsedAt == nil {
		channel.ChannelInfo.MultiKeyLastUsedAt = make(map[int]int64)
	}
	channel.ChannelInfo.MultiKeyLastUsedAt[keyIdx] = time.Now().Unix()
	if !common.MemoryCacheEnabled {
		_ = channel.SaveChannelInfo()
	}
}

// HandleUpstream429Storm 记录一次上游 429 事件并判断是否进入 429 风暴.
// 该函数包装 model 包实现,在此处提供 service 层的稳定 API.
func HandleUpstream429Storm(channelId int, channelType int) bool {
	return model.HandleUpstream429Storm(channelId, channelType)
}

// IsKeyStormPaused 判断指定 key 是否处于 429 风暴暂停窗口内.
func IsKeyStormPaused(channelId int, keyIdx int, nowSec int64) bool {
	return model.IsKeyStormPaused(channelId, keyIdx, nowSec)
}

// GetKey429StormCount 获取指定 channel 当前的 429 风暴计数.
func GetKey429StormCount(channelId int) int {
	return model.GetKey429StormCount(channelId)
}

// IsChannelIn429Storm 判断指定 channel 当前是否处于 429 风暴模式.
func IsChannelIn429Storm(channelId int) bool {
	return model.IsChannelIn429Storm(channelId)
}

// ResetChannel429Storm 重置指定 channel 的 429 风暴状态.
func ResetChannel429Storm(channelId int) {
	model.ResetChannel429Storm(channelId)
}

// (HandleUpstreamSuccess 已存在于 line 117 的 HEAD 版本中,此处删除 Agent B 的重复定义)
