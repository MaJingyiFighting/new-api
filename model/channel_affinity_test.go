package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestChannel(keys []string, multiKeyMode constant.MultiKeyMode) *Channel {
	keyStr := ""
	for i, k := range keys {
		if i > 0 {
			keyStr += "\n"
		}
		keyStr += k
	}
	info := ChannelInfo{
		IsMultiKey:   len(keys) > 1,
		MultiKeySize: len(keys),
		MultiKeyMode: multiKeyMode,
		MultiKeyStatusList: func() map[int]int {
			m := make(map[int]int)
			for i := range keys {
				m[i] = common.ChannelStatusEnabled
			}
			return m
		}(),
	}
	return &Channel{
		Id:          1,
		Key:         keyStr,
		ChannelInfo: info,
	}
}

// TestGetNextEnabledKeyWithAffinity_HitPreferred 测试命中 preferred key 时固定 key。
func TestGetNextEnabledKeyWithAffinity_HitPreferred(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModePolling)
	ch.ChannelInfo.MultiKeyPollingIndex = 1 // next would normally be key1

	prefIdx := 0
	key, idx, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.Nil(t, err)
	assert.Equal(t, "key0", key)
	assert.Equal(t, 0, idx)
	// polling index should NOT be advanced when using preferred key
	assert.Equal(t, 1, ch.ChannelInfo.MultiKeyPollingIndex)
}

// TestGetNextEnabledKeyWithAffinity_PreferredDisabled 测试 preferred key 永久禁用时跳过，fallback 到其他 key。
func TestGetNextEnabledKeyWithAffinity_PreferredDisabled(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModeRandom)
	ch.ChannelInfo.MultiKeyStatusList[0] = 3 // auto-disabled

	prefIdx := 0
	key, idx, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.Nil(t, err)
	assert.Equal(t, "key1", key)
	assert.Equal(t, 1, idx)
}

// TestGetNextEnabledKeyWithAffinity_PreferredRateLimited 测试 preferred key 被 MultiKeyRateLimitUntil 冻结时跳过，fallback 到其他 key。
func TestGetNextEnabledKeyWithAffinity_PreferredRateLimited(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModeRandom)
	ch.ChannelInfo.MultiKeyRateLimitUntil = map[int]int64{
		0: time.Now().Add(1 * time.Hour).Unix(),
	}

	prefIdx := 0
	key, idx, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.Nil(t, err)
	assert.Equal(t, "key1", key)
	assert.Equal(t, 1, idx)
}

// TestGetNextEnabledKeyWithAffinity_PreferredFrozen_NoFallback 测试 preferred key 冻结且无其他可用 key。
func TestGetNextEnabledKeyWithAffinity_PreferredFrozen_NoFallback(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModeRandom)
	ch.ChannelInfo.MultiKeyRateLimitUntil = map[int]int64{
		0: time.Now().Add(1 * time.Hour).Unix(),
		1: time.Now().Add(1 * time.Hour).Unix(),
	}

	prefIdx := 0
	_, _, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.NotNil(t, err)
	assert.Equal(t, types.ErrorCodeChannelNoAvailableKey, err.GetErrorCode())
}

// TestGetNextEnabledKeyWithAffinity_ExpiredTempState 测试临时冻结过期后自动恢复（惰性恢复）。
func TestGetNextEnabledKeyWithAffinity_ExpiredTempState(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModePolling)
	// key0 的限流已过期
	ch.ChannelInfo.MultiKeyRateLimitUntil = map[int]int64{
		0: time.Now().Add(-1 * time.Hour).Unix(),
	}

	prefIdx := 0
	key, idx, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.Nil(t, err)
	assert.Equal(t, "key0", key)
	assert.Equal(t, 0, idx)
	// expired state should be cleaned up
	_, stillExists := ch.ChannelInfo.MultiKeyRateLimitUntil[0]
	assert.False(t, stillExists, "expired rate limit should be cleaned up")
}

// TestGetNextEnabledKeyWithAffinity_PollingNormal 测试 polling 模式下普通请求仍按可用 key 轮询。
// Note: uses MultiKeyModeRandom since polling mode requires DB access via CacheGetChannelInfo.
func TestGetNextEnabledKeyWithAffinity_PollingNormal(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1", "key2"}, constant.MultiKeyModeRandom)
	ch.ChannelInfo.MultiKeyPollingIndex = 1

	key, idx, err := ch.GetNextEnabledKeyWithAffinity(nil)
	require.Nil(t, err)
	assert.Contains(t, []string{"key0", "key1", "key2"}, key)
	assert.True(t, idx >= 0 && idx <= 2)
}

// TestGetNextEnabledKeyWithAffinity_StickyDoesNotAdvancePolling 测试 sticky 请求不推进 MultiKeyPollingIndex。
func TestGetNextEnabledKeyWithAffinity_StickyDoesNotAdvancePolling(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModePolling)
	ch.ChannelInfo.MultiKeyPollingIndex = 0

	prefIdx := 0
	_, idx, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.Nil(t, err)
	assert.Equal(t, 0, idx)
	assert.Equal(t, 0, ch.ChannelInfo.MultiKeyPollingIndex, "sticky request should not advance polling index")
}

// TestGetNextEnabledKeyWithAffinity_SingleKey 测试单 Key channel 正常返回原 key。
func TestGetNextEnabledKeyWithAffinity_SingleKey(t *testing.T) {
	ch := newTestChannel([]string{"sk-mykey"}, constant.MultiKeyModePolling)
	ch.ChannelInfo.IsMultiKey = false
	ch.ChannelInfo.MultiKeySize = 1

	key, idx, err := ch.GetNextEnabledKeyWithAffinity(nil)
	require.Nil(t, err)
	assert.Equal(t, "sk-mykey", key)
	assert.Equal(t, 0, idx)
}

// TestGetNextEnabledKeyWithAffinity_AllTempFrozen 测试所有 key 都临时冻结时返回 no available key。
// Note: uses MultiKeyModeRandom since polling mode requires DB access via CacheGetChannelInfo.
func TestGetNextEnabledKeyWithAffinity_AllTempFrozen(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModeRandom)
	ch.ChannelInfo.MultiKeyRateLimitUntil = map[int]int64{
		0: time.Now().Add(1 * time.Hour).Unix(),
	}
	ch.ChannelInfo.MultiKeyOverloadUntil = map[int]int64{
		1: time.Now().Add(1 * time.Hour).Unix(),
	}

	_, _, err := ch.GetNextEnabledKeyWithAffinity(nil)
	require.NotNil(t, err)
	assert.Equal(t, types.ErrorCodeChannelNoAvailableKey, err.GetErrorCode())
}

// TestGetNextEnabledKeyWithAffinity_QuotaReset 测试 quota_reset 冻结时跳过 preferred key，fallback 到其他 key。
func TestGetNextEnabledKeyWithAffinity_QuotaReset(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModeRandom)
	ch.ChannelInfo.MultiKeyQuotaResetAt = map[int]int64{
		0: time.Now().Add(1 * time.Hour).Unix(),
	}

	prefIdx := 0
	key, idx, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.Nil(t, err)
	assert.Equal(t, "key1", key)
	assert.Equal(t, 1, idx)
}

// TestGetNextEnabledKeyWithAffinity_PreferredKey_NotAdvancePollingIndexInRandomMode
// random 模式下 preferred key 也不应该推进 polling index。
func TestGetNextEnabledKeyWithAffinity_PreferredKeyInRandomMode(t *testing.T) {
	ch := newTestChannel([]string{"key0", "key1"}, constant.MultiKeyModeRandom)

	prefIdx := 1
	key, idx, err := ch.GetNextEnabledKeyWithAffinity(&prefIdx)
	require.Nil(t, err)
	assert.Equal(t, "key1", key)
	assert.Equal(t, 1, idx)
}


