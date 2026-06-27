package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCodingPlanProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"kimi canonical", "kimi", true},
		{"zhipu canonical", "zhipu", true},
		{"minimax canonical", "minimax", true},
		{"volcengine canonical", "volcengine", true},
		{"mimo canonical", "mimo", true},
		{"uppercase canonical", "KIMI", true},
		{"trimmed canonical", "  zhipu  ", true},
		{"mixed case alias mo", "Moonshot", false},
		{"empty", "", false},
		{"whitespace", "   ", false},
		{"openai", "openai", false},
		{"anthropic", "anthropic", false},
		{"deepseek", "deepseek", false},
		{"unknown random", "foo-bar", false},
		{"partial match", "kimi-coding", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, IsCodingPlanProvider(tc.input))
		})
	}
}

func TestNormalizeCodingPlanProvider_Canonical(t *testing.T) {
	t.Parallel()

	for _, provider := range []constant.CodingPlanProvider{
		constant.CodingPlanProviderKimi,
		constant.CodingPlanProviderZhipu,
		constant.CodingPlanProviderMiniMax,
		constant.CodingPlanProviderVolcengine,
		constant.CodingPlanProviderMiMo,
	} {
		provider := provider
		t.Run("canonical_"+string(provider), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, provider, NormalizeCodingPlanProvider(string(provider)))
		})
	}
}

func TestNormalizeCodingPlanProvider_Aliases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		expected constant.CodingPlanProvider
	}{
		{"kimi moonshot", "moonshot", constant.CodingPlanProviderKimi},
		{"kimi uppercase moonshot", "MOONSHOT", constant.CodingPlanProviderKimi},
		{"kimi kimi_for_coding", "kimi_for_coding", constant.CodingPlanProviderKimi},
		{"zhipu glm", "glm", constant.CodingPlanProviderZhipu},
		{"zhipu bigmodel", "bigmodel", constant.CodingPlanProviderZhipu},
		{"zhipu zai", "zai", constant.CodingPlanProviderZhipu},
		{"zhipu z.ai", "z.ai", constant.CodingPlanProviderZhipu},
		{"zhipu trimmed z.ai", "  Z.AI  ", constant.CodingPlanProviderZhipu},
		{"minimax mini-max", "mini-max", constant.CodingPlanProviderMiniMax},
		{"minimax minimax-coding", "minimax-coding", constant.CodingPlanProviderMiniMax},
		{"volcengine volcano", "volcano", constant.CodingPlanProviderVolcengine},
		{"volcengine ark", "ark", constant.CodingPlanProviderVolcengine},
		{"volcengine doubao", "doubao", constant.CodingPlanProviderVolcengine},
		{"mimo xiaomi", "xiaomi", constant.CodingPlanProviderMiMo},
		{"mimo XIAOMI", "XIAOMI", constant.CodingPlanProviderMiMo},
		{"unknown empty", "", ""},
		{"unknown whitespace", "   ", ""},
		{"unknown openai", "openai", ""},
		{"unknown foo", "foo-bar", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, NormalizeCodingPlanProvider(tc.input))
		})
	}
}

func TestDetectCodingPlanProviderFromBaseURL_Positive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		baseURL  string
		expected constant.CodingPlanProvider
	}{
		// Kimi
		{"kimi with coding path", "https://api.kimi.com/coding/v1", constant.CodingPlanProviderKimi},
		{"kimi trailing slash", "https://api.kimi.com/coding/v1/", constant.CodingPlanProviderKimi},
		{"kimi http scheme", "http://api.kimi.com/coding/v1", constant.CodingPlanProviderKimi},
		{"kimi no scheme", "api.kimi.com/coding/v1", constant.CodingPlanProviderKimi},
		{"moonshot host", "https://api.moonshot.cn/v1", constant.CodingPlanProviderKimi},
		{"moonshot host v3", "https://api.moonshot.cn/v3/chat/completions", constant.CodingPlanProviderKimi},

		// Zhipu
		{"zhipu open.bigmodel.cn", "https://open.bigmodel.cn/api/paas/v4", constant.CodingPlanProviderZhipu},
		{"zhipu api.z.ai", "https://api.z.ai/api/paas/v4", constant.CodingPlanProviderZhipu},
		{"zhipu subdomain bigmodel", "https://foo.bigmodel.cn/api/paas/v4", constant.CodingPlanProviderZhipu},
		{"zhipu uppercase host", "https://API.Z.AI/api/paas/v4", constant.CodingPlanProviderZhipu},

		// MiniMax
		{"minimax minimaxi", "https://api.minimaxi.com/v1", constant.CodingPlanProviderMiniMax},
		{"minimax io", "https://api.minimax.io/v1", constant.CodingPlanProviderMiniMax},
		{"minimax io with path", "https://api.minimax.io/v1/text/chatcompletion_v2", constant.CodingPlanProviderMiniMax},

		// Volcengine
		{"volcengine ark region", "https://ark.cn-beijing.volces.com/api/v3", constant.CodingPlanProviderVolcengine},
		{"volcengine bare volces.com", "https://volces.com/api/v3", constant.CodingPlanProviderVolcengine},
		{"volcengine host contains volcengine", "https://console.volcengine.com/blank", constant.CodingPlanProviderVolcengine},

		// MiMo
		{"mimo xiaomi api", "https://mimo.api.xiaomi.com/v1", constant.CodingPlanProviderMiMo},
		{"mimo path contains mimo", "https://api.xiaomi.com/mimo/v1", constant.CodingPlanProviderMiMo},
		{"mimo target substring", "https://mimo.example.com/v1", constant.CodingPlanProviderMiMo},
		{"mimo xiaomi host with api", "https://api.xiaomi.com/v1", constant.CodingPlanProviderMiMo},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, DetectCodingPlanProviderFromBaseURL(tc.baseURL))
		})
	}
}

func TestDetectCodingPlanProviderFromBaseURL_Negative(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		baseURL string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"openai", "https://api.openai.com/v1"},
		{"anthropic", "https://api.anthropic.com/v1"},
		{"generic openai compatible", "https://openai-compatible.example.com/v1"},
		{"mi.com shop", "https://www.mi.com/shop"},
		{"kimi without coding path", "https://api.kimi.com/v1"},
		{"zhipu not bigmodel", "https://example.cn/api"},
		{"minimaxi typo", "https://api.minnimaxi.com/v1"},
		{"mimo no xiaomi no mimo", "https://api.openai.com/v1"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, constant.CodingPlanProvider(""), DetectCodingPlanProviderFromBaseURL(tc.baseURL))
		})
	}
}

func TestDetectCodingPlanProviderFromBaseURL_MalformedURL(t *testing.T) {
	t.Parallel()

	// Malformed URLs must not panic and must return a deterministic empty
	// result; the function should fall back gracefully on the raw lowercase
	// input.
	cases := []string{
		"::::",
		"http://[::1",
		"//no-scheme-no-host",
		"http://%41:8080",
		"\x00invalid",
	}

	for _, baseURL := range cases {
		baseURL := baseURL
		t.Run(baseURL, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, constant.CodingPlanProvider(""), DetectCodingPlanProviderFromBaseURL(baseURL))
		})
	}
}

func TestDomesticSchedulingProviders(t *testing.T) {
	t.Parallel()

	providers := DomesticSchedulingProviders()

	require.Len(t, providers, 5)
	assert.Equal(t, constant.CodingPlanProviderKimi, providers[0])
	assert.Equal(t, constant.CodingPlanProviderZhipu, providers[1])
	assert.Equal(t, constant.CodingPlanProviderMiniMax, providers[2])
	assert.Equal(t, constant.CodingPlanProviderVolcengine, providers[3])
	assert.Equal(t, constant.CodingPlanProviderMiMo, providers[4])

	for _, p := range providers {
		assert.True(t, IsCodingPlanProvider(string(p)), "every listed provider must satisfy IsCodingPlanProvider: %s", p)
	}
}
