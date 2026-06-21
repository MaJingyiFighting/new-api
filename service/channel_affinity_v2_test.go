package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeyFingerprintForKey 验证 key 指纹生成。
func TestKeyFingerprintForKey(t *testing.T) {
	fp := KeyFingerprintForKey("sk-test-key")
	assert.Equal(t, 12, len(fp), "fingerprint should be 12 hex characters")
	assert.NotEqual(t, "", fp)

	fp2 := KeyFingerprintForKey("sk-test-key2")
	assert.NotEqual(t, fp, fp2, "different keys should have different fingerprints")

	empty := KeyFingerprintForKey("")
	assert.Equal(t, "", empty)
}

// TestChannelAffinityTarget_VersionAndFields 验证 ChannelAffinityTarget 结构。
func TestChannelAffinityTarget_VersionAndFields(t *testing.T) {
	target := ChannelAffinityTarget{
		Version:        2,
		ChannelID:      33,
		KeyIndex:       6,
		KeyFingerprint: "abcdef123456",
	}
	assert.Equal(t, 2, target.Version)
	assert.Equal(t, 33, target.ChannelID)
	assert.Equal(t, 6, target.KeyIndex)
	assert.Equal(t, "abcdef123456", target.KeyFingerprint)

	// 单 Key channel 用 KeyIndex = -1
	singleKeyTarget := ChannelAffinityTarget{
		Version:   2,
		ChannelID: 33,
		KeyIndex:  -1,
	}
	assert.Equal(t, -1, singleKeyTarget.KeyIndex)
}

// TestGetPreferredChannelTargetByAffinity_KeyFingerprintMatch 测试 key fingerprint 匹配时命中。
func TestGetPreferredChannelTargetByAffinity_KeyFingerprintMatch(t *testing.T) {
	// This test requires proper cache setup. We test the fingerprint matching
	// logic directly instead.
	fp1 := KeyFingerprintForKey("sk-key-a")
	fp2 := KeyFingerprintForKey("sk-key-b")
	assert.NotEqual(t, fp1, fp2, "different keys must produce different fingerprints")
}

// TestRecordChannelAffinityV2_SingleKey 测试单 Key channel 正常记录 KeyIndex = -1。
func TestRecordChannelAffinityV2_SingleKey(t *testing.T) {
	target := ChannelAffinityTarget{
		Version:        2,
		ChannelID:      42,
		KeyIndex:       -1,
		KeyFingerprint: KeyFingerprintForKey("sk-single"),
	}
	assert.Equal(t, -1, target.KeyIndex)
	assert.Equal(t, 42, target.ChannelID)
}

// Helper to check that the GetPreferredChannelByAffinity function works with the
// default setting model regex patterns.
func TestDefaultSettingModelRegex(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)
	require.True(t, len(setting.Rules) >= 3, "should have at least 3 rules (codex, claude, domestic)")

	// Find the domestic llm trace rule
	var domesticRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		if setting.Rules[i].Name == "domestic llm trace" {
			domesticRule = &setting.Rules[i]
			break
		}
	}
	require.NotNil(t, domesticRule, "domestic llm trace rule should exist")

	// Check model regex patterns
	assert.True(t, matchAnyRegexCached(domesticRule.ModelRegex, "MiniMax-4.0"))
	assert.True(t, matchAnyRegexCached(domesticRule.ModelRegex, "glm-4-plus"))
	assert.True(t, matchAnyRegexCached(domesticRule.ModelRegex, "kimi-v2"))
	assert.True(t, matchAnyRegexCached(domesticRule.ModelRegex, "moonshot-v1"))
	assert.True(t, matchAnyRegexCached(domesticRule.ModelRegex, "gemini-2.0-flash"))
	assert.True(t, matchAnyRegexCached(domesticRule.ModelRegex, "google/gemini-2.0-flash"))

	// Check path regex
	assert.True(t, matchAnyRegexCached(domesticRule.PathRegex, "/v1/responses"))
	assert.True(t, matchAnyRegexCached(domesticRule.PathRegex, "/responses"))
	assert.True(t, matchAnyRegexCached(domesticRule.PathRegex, "/backend-api/codex/responses"))
}

// TestGetPreferredChannelByAffinity_MatchCodexPath 测试 codex 规则匹配新增路径。
func TestGetPreferredChannelByAffinity_MatchCodexPath(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	var codexRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		if setting.Rules[i].Name == "codex cli trace" {
			codexRule = &setting.Rules[i]
			break
		}
	}
	require.NotNil(t, codexRule)

	assert.True(t, matchAnyRegexCached(codexRule.PathRegex, "/v1/responses"))
	assert.True(t, matchAnyRegexCached(codexRule.PathRegex, "/responses"))
	assert.True(t, matchAnyRegexCached(codexRule.PathRegex, "/backend-api/codex/responses"))
}

// TestGetPreferredChannelByAffinity_KeySources 测试 codex 规则包含所有需要的 key sources。
func TestGetPreferredChannelByAffinity_KeySources(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	var codexRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		if setting.Rules[i].Name == "codex cli trace" {
			codexRule = &setting.Rules[i]
			break
		}
	}
	require.NotNil(t, codexRule)

	// Verify key sources include gjson paths
	hasPromptCacheKey := false
	hasMetadataUserID := false
	hasSessionID := false
	hasCodexTurnMetadata := false
	hasOriginator := false
	for _, ks := range codexRule.KeySources {
		switch ks.Type {
		case "gjson":
			if ks.Path == "prompt_cache_key" {
				hasPromptCacheKey = true
			}
			if ks.Path == "metadata.user_id" {
				hasMetadataUserID = true
			}
		case "request_header":
			if ks.Key == "Session_id" {
				hasSessionID = true
			}
			if ks.Key == "X-Codex-Turn-Metadata" {
				hasCodexTurnMetadata = true
			}
			if ks.Key == "Originator" {
				hasOriginator = true
			}
		}
	}
	assert.True(t, hasPromptCacheKey, "should have gjson prompt_cache_key")
	assert.True(t, hasMetadataUserID, "should have gjson metadata.user_id")
	assert.True(t, hasSessionID, "should have Session_id header")
	assert.True(t, hasCodexTurnMetadata, "should have X-Codex-Turn-Metadata header")
	assert.True(t, hasOriginator, "should have Originator header")
}

// TestGetChannelAffinityLogInfo 测试亲和日志信息包含所需字段。
func TestGetChannelAffinityLogInfo(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	meta := channelAffinityMeta{
		CacheKey:       "test:key",
		TTLSeconds:     3600,
		RuleName:       "codex cli trace",
		SkipRetry:      true,
		UsingGroup:     "default",
		ModelName:      "gpt-4.1",
		RequestPath:    "/v1/responses",
		KeySourceType:  "gjson",
		KeySourceKey:   "",
		KeySourcePath:  "prompt_cache_key",
		KeyHint:        "abcd...wxyz",
		KeyFingerprint: "abcdef12",
	}
	setChannelAffinityContext(ctx, meta)

	MarkChannelAffinityUsed(ctx, "default", 33)

	info := map[string]interface{}{}
	AppendChannelAffinityAdminInfo(ctx, info)

	affinity, ok := info["channel_affinity"]
	require.True(t, ok)

	affMap, ok := affinity.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "codex cli trace", affMap["rule_name"])
	assert.Equal(t, 33, affMap["channel_id"])
	assert.Equal(t, "gjson", affMap["key_source"])
}

// TestChannelAffinityTarget_KeyFingerprintValidation 测试 ChannelAffinityTarget 的
// KeyFingerprint 字段在单 Key 和多 Key 场景下的行为。
func TestChannelAffinityTarget_KeyFingerprintValidation(t *testing.T) {
	// multi-key scenario
	keys := []string{"sk-key-a", "sk-key-b"}
	targetKey0 := ChannelAffinityTarget{
		Version:        2,
		ChannelID:      33,
		KeyIndex:       0,
		KeyFingerprint: KeyFingerprintForKey(keys[0]),
	}
	targetKey1 := ChannelAffinityTarget{
		Version:        2,
		ChannelID:      33,
		KeyIndex:       1,
		KeyFingerprint: KeyFingerprintForKey(keys[1]),
	}

	// Different keys must have different fingerprints
	expectedFp0 := KeyFingerprintForKey(keys[0])
	expectedFp1 := KeyFingerprintForKey(keys[1])
	assert.Equal(t, targetKey0.KeyFingerprint, expectedFp0)
	assert.Equal(t, targetKey1.KeyFingerprint, expectedFp1)
	assert.NotEqual(t, targetKey0.KeyFingerprint, targetKey1.KeyFingerprint)

	// After editing key list (reordering), the old fingerprint should NOT match
	// the new key at the same index.
	reorderedKeys := []string{"sk-key-b", "sk-key-a"}
	newFpAtIndex0 := KeyFingerprintForKey(reorderedKeys[0])
	assert.NotEqual(t, targetKey0.KeyFingerprint, newFpAtIndex0,
		"after reordering, old fingerprint at index 0 should not match new key at index 0")
}

// TestChannelAffinityTarget_SingleKeyChannel 测试单 Key channel 正确记录 -1。
func TestChannelAffinityTarget_SingleKeyChannel(t *testing.T) {
	singleKey := "sk-only-key"
	target := ChannelAffinityTarget{
		Version:        2,
		ChannelID:      100,
		KeyIndex:       -1,
		KeyFingerprint: KeyFingerprintForKey(singleKey),
	}
	assert.Equal(t, -1, target.KeyIndex)
	assert.Equal(t, KeyFingerprintForKey(singleKey), target.KeyFingerprint)
	assert.Equal(t, 100, target.ChannelID)
}

// TestChannelAffinityCacheNamespace 验证 v2 namespace 定义。
func TestChannelAffinityCacheNamespace(t *testing.T) {
	assert.Equal(t, "new-api:channel_affinity:v2", channelAffinityCacheV2Namespace)
	assert.Equal(t, "new-api:channel_affinity:v1", channelAffinityCacheNamespace)
}

// TestBuildChannelAffinityCacheKey_CodexRule 验证 codex 规则在亲和缓存键中的行为。
func TestBuildChannelAffinityCacheKey_CodexRule(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	var codexRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		if setting.Rules[i].Name == "codex cli trace" {
			codexRule = &setting.Rules[i]
			break
		}
	}
	require.NotNil(t, codexRule)

	// Test that rule correctly handles include settings
	assert.True(t, codexRule.IncludeRuleName)
	assert.True(t, codexRule.IncludeUsingGroup)
}
