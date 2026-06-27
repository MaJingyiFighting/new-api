package service

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

// CodingPlanProviders returns all known providers in stable order. Used by
// the admin UI to populate provider dropdowns.
func CodingPlanProviders() []constant.CodingPlanProvider {
	return []constant.CodingPlanProvider{
		constant.CodingPlanProviderKimi,
		constant.CodingPlanProviderZhipu,
		constant.CodingPlanProviderMiniMax,
		constant.CodingPlanProviderVolcengine,
		constant.CodingPlanProviderMiMo,
	}
}

// IsCodingPlanChannelType reports whether the given channel type is one of
// the dedicated Coding Plan channel types. The list is defined in the
// constant package so the constant block stays the source of truth.
func IsCodingPlanChannelType(channelType int) bool {
	for _, t := range constant.CodingPlanChannelTypes {
		if t == channelType {
			return true
		}
	}
	return false
}

// IsCodingPlanProvider reports whether the given string matches a known
// provider. Comparison is case-insensitive and tolerates surrounding
// whitespace, so it can be applied directly to JSON-decoded values without
// normalization.
func IsCodingPlanProvider(provider string) bool {
	switch constant.CodingPlanProvider(strings.ToLower(strings.TrimSpace(provider))) {
	case constant.CodingPlanProviderKimi,
		constant.CodingPlanProviderZhipu,
		constant.CodingPlanProviderMiniMax,
		constant.CodingPlanProviderVolcengine,
		constant.CodingPlanProviderMiMo:
		return true
	}
	return false
}

// NormalizeCodingPlanProvider canonicalizes a provider string from user
// input or persisted config. It accepts the canonical names plus a few
// common aliases (moonshot, bigmodel, z.ai, etc.).
func NormalizeCodingPlanProvider(value string) constant.CodingPlanProvider {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(constant.CodingPlanProviderKimi), "moonshot", "kimi_for_coding":
		return constant.CodingPlanProviderKimi
	case string(constant.CodingPlanProviderZhipu), "glm", "bigmodel", "zai", "z.ai":
		return constant.CodingPlanProviderZhipu
	case string(constant.CodingPlanProviderMiniMax), "minimax-coding", "mini-max":
		return constant.CodingPlanProviderMiniMax
	case string(constant.CodingPlanProviderVolcengine), "volcano", "ark", "doubao":
		return constant.CodingPlanProviderVolcengine
	case string(constant.CodingPlanProviderMiMo), "xiaomi":
		return constant.CodingPlanProviderMiMo
	}
	return ""
}

// ResolveCodingPlanProvider inspects a base URL and returns the matching
// provider, or "" if the URL does not belong to a known Coding Plan.
//
// Detection rules mirror sub2api's DetectCodingPlanProviderFromBaseURL:
//
//   - Kimi:       api.kimi.com (path containing "/coding") or api.moonshot.cn
//   - Zhipu:      open.bigmodel.cn, any *.bigmodel.cn, api.z.ai
//   - MiniMax:    api.minimaxi.com, api.minimax.io
//   - Volcengine: *.volces.com, volcengine.com, or host containing "volcengine"
//   - MiMo:       host containing "mimo", or host containing "xiaomi" with
//                 "api" in host or "mimo" in path
//
// The URL is treated case-insensitively. If no scheme is present, https://
// is assumed so url.Parse still succeeds.
func ResolveCodingPlanProvider(rawBaseURL string) constant.CodingPlanProvider {
	normalized := strings.ToLower(strings.TrimSpace(rawBaseURL))
	if normalized == "" {
		return ""
	}

	parseTarget := normalized
	if !strings.Contains(parseTarget, "://") {
		parseTarget = "https://" + parseTarget
	}

	host := ""
	path := ""
	if parsed, err := url.Parse(parseTarget); err == nil && parsed != nil {
		host = strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
		path = strings.ToLower(parsed.EscapedPath())
	}
	if host == "" {
		// Fall back to the raw string when parsing failed; it may still
		// contain a recognizable substring.
		host = normalized
	}

	target := host + path
	switch {
	case (host == "api.kimi.com" && strings.Contains(path, "/coding")) || host == "api.moonshot.cn":
		return constant.CodingPlanProviderKimi
	case host == "open.bigmodel.cn", strings.HasSuffix(host, ".bigmodel.cn"), host == "api.z.ai":
		return constant.CodingPlanProviderZhipu
	case host == "api.minimaxi.com", host == "api.minimax.io":
		return constant.CodingPlanProviderMiniMax
	case strings.HasSuffix(host, ".volces.com"), host == "volces.com", strings.Contains(host, "volcengine"):
		return constant.CodingPlanProviderVolcengine
	case strings.Contains(target, "mimo"),
		strings.Contains(host, "xiaomi") && (strings.Contains(host, "api") || strings.Contains(path, "mimo")):
		return constant.CodingPlanProviderMiMo
	}
	return ""
}

// DetectCodingPlanProviderFromBaseURL is an exported alias for ResolveCodingPlanProvider.
// It is used by the quota probe and scheduling layers.
func DetectCodingPlanProviderFromBaseURL(baseURL string) constant.CodingPlanProvider {
	return ResolveCodingPlanProvider(baseURL)
}

// DomesticSchedulingProviders returns all known Coding Plan providers in their
// canonical order. The slice is guaranteed to be non-nil and non-empty.
func DomesticSchedulingProviders() []constant.CodingPlanProvider {
	return CodingPlanProviders()
}

// CodingPlanBaseURL returns the configured base URL for a channel with
// trailing slashes trimmed. Returns "" when the channel is nil or has no
// base URL configured.
func CodingPlanBaseURL(ch *model.Channel) string {
	if ch == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(ch.GetBaseURL()), "/")
}

// CodingPlanAPIKey returns the first non-empty API key from a channel's key
// pool. Coding Plan accounts are typically single-key, but the function
// tolerates the multi-key layout in case a user pastes a JSON array.
//
// The return value is empty when the channel is nil or has no usable key.
func CodingPlanAPIKey(ch *model.Channel) string {
	if ch == nil {
		return ""
	}
	for _, k := range ch.GetKeys() {
		if trimmed := strings.TrimSpace(k); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
