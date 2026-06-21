package service

import (
	"context"
	"time"
)

// QuotaFetcher 额度获取接口，各平台实现此接口。
// 直接移植自 sub2api quota_fetcher.go。
type QuotaFetcher interface {
	// CanFetch 检查是否可获取此渠道的额度
	CanFetch(channelType int, channelSetting map[string]interface{}) bool
	// FetchQuota 获取渠道额度信息
	FetchQuota(ctx context.Context, channelType int, apiKey string, baseURL string) (*QuotaResult, error)
}

// QuotaResult 额度获取结果
// 移植自 sub2api QuotaResult。
type QuotaResult struct {
	UsageInfo *UsageInfo     // 转换后的使用信息
	Raw       map[string]any // 原始响应
}

// UsageInfo 使用信息
// 移植自 sub2api UsageInfo 的核心字段。
type UsageInfo struct {
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	QuotaUsed     float64    `json:"quota_used,omitempty"`    // 已使用额度
	QuotaTotal    float64    `json:"quota_total,omitempty"`   // 总额度
	Remaining     float64    `json:"remaining,omitempty"`     // 剩余额度
	IsForbidden   bool       `json:"is_forbidden,omitempty"`  // 是否被禁止访问
	ForbiddenType string     `json:"forbidden_type,omitempty"` // 禁止类型
	ResetUnix     int64      `json:"reset_unix,omitempty"`    // 重置时间戳
}
