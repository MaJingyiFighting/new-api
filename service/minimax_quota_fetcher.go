package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// minimaxBalanceResponse MiniMax 余额查询响应
type minimaxBalanceResponse struct {
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	Data struct {
		TotalBalance    float64 `json:"total_balance"`     // 总额度（元）
		RemainingCredit float64 `json:"remaining_credit"`  // 剩余额度
		UsedCredit      float64 `json:"used_credit"`       // 已用额度
		IsExpired       bool    `json:"is_expired"`
		ExpiredAt       string  `json:"expired_at"`
	} `json:"data"`
}

// minimaxQuotaFetcher MiniMax 平台额度查询适配器
type minimaxQuotaFetcher struct{}

func NewMinimaxQuotaFetcher() QuotaFetcher {
	return &minimaxQuotaFetcher{}
}

func (f *minimaxQuotaFetcher) CanFetch(channelType int, channelSetting map[string]interface{}) bool {
	// constant.ChannelTypeMiniMax 应为 MiniMax 对应的渠道类型值
	// 通过 setting 中的 type 判断
	return channelType == 32 || channelType == 33 // MiniMax 渠道类型
}

func (f *minimaxQuotaFetcher) FetchQuota(ctx context.Context, channelType int, apiKey string, baseURL string) (*QuotaResult, error) {
	if baseURL == "" {
		baseURL = "https://api.minimax.chat"
	}
	url := fmt.Sprintf("%s/v1/users/me/balance", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create minimax balance request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query minimax balance failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read minimax balance response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// 401 → 账号已失效
		return &QuotaResult{
			UsageInfo: &UsageInfo{
				IsForbidden: resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
				ResetUnix:   0,
			},
			Raw: map[string]any{"status_code": resp.StatusCode, "body": string(body)},
		}, nil
	}

	var balanceResp minimaxBalanceResponse
	if err := json.Unmarshal(body, &balanceResp); err != nil {
		return nil, fmt.Errorf("parse minimax balance response failed: %w", err)
	}

	if balanceResp.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax balance api error: %s", balanceResp.BaseResp.StatusMsg)
	}

	now := time.Now()
	usageInfo := &UsageInfo{
		UpdatedAt:  &now,
		QuotaTotal: balanceResp.Data.TotalBalance,
		Remaining:  balanceResp.Data.RemainingCredit,
		QuotaUsed:  balanceResp.Data.UsedCredit,
	}

	return &QuotaResult{
		UsageInfo: usageInfo,
		Raw: map[string]any{
			"total_balance":    balanceResp.Data.TotalBalance,
			"remaining_credit": balanceResp.Data.RemainingCredit,
			"is_expired":       balanceResp.Data.IsExpired,
		},
	}, nil
}

// GetQuotaFetcher 根据渠道类型返回对应的额度查询适配器。
func GetQuotaFetcher(channelType int) QuotaFetcher {
	// TODO: 按 channelType 分发到不同平台适配器
	switch channelType {
	case 32, 33: // MiniMax
		return NewMinimaxQuotaFetcher()
	default:
		return nil
	}
}

// CheckChannelQuotaAndBackoff 检查渠道额度，若余量不足则对对应 key 设置退避。
// 适配器模式同 sub2api 的 QuotaFetcher 调度。
func CheckChannelQuotaAndBackoff(ch *model.Channel, keyIdx int) {
	fetcher := GetQuotaFetcher(ch.Type)
	if fetcher == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	apiKeys := ch.GetKeys()
	if keyIdx < 0 || keyIdx >= len(apiKeys) {
		return
	}

	baseURL := ""
	if ch.BaseURL != nil {
		baseURL = *ch.BaseURL
	}

	result, err := fetcher.FetchQuota(ctx, ch.Type, apiKeys[keyIdx], baseURL)
	if err != nil {
		common.SysLog(fmt.Sprintf("query channel #%d key #%d quota failed: %v", ch.Id, keyIdx, err))
		return
	}

	if result == nil || result.UsageInfo == nil {
		return
	}

	info := &ch.ChannelInfo

	// 401/403 → 禁用 key
	if result.UsageInfo.IsForbidden {
		if info.MultiKeyStatusList == nil {
			info.MultiKeyStatusList = make(map[int]int)
		}
		info.MultiKeyStatusList[keyIdx] = 3
		_ = ch.SaveChannelInfo()
		return
	}

	// 余量 ≤ 0 → 设置 RateLimitUntil 到 resetUnix（如果不知道重置时间则默认 1 小时）
	if result.UsageInfo.Remaining <= 0 {
		untilUnix := result.UsageInfo.ResetUnix
		if untilUnix <= 0 {
			untilUnix = time.Now().Add(1 * time.Hour).Unix()
		}
		if info.MultiKeyRateLimitUntil == nil {
			info.MultiKeyRateLimitUntil = make(map[int]int64)
		}
		info.MultiKeyRateLimitUntil[keyIdx] = untilUnix
		common.SysLog(fmt.Sprintf("channel #%d key #%d quota exhausted, backoff until %d", ch.Id, keyIdx, untilUnix))
		_ = ch.SaveChannelInfo()
	}
}
