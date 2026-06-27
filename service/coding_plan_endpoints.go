package service

// Default quota-probe endpoints for each Coding Plan provider.
//
// These URLs are the canonical public endpoints used to query the 5-hour /
// weekly subscription usage of a Coding Plan. Probes (PR-2) will resolve the
// base URL through DetectCodingPlanProviderFromBaseURL first and then pick the
// matching endpoint; having the constants centralised here keeps both PR-2
// (probe) and PR-3 (error classification) referring to the same source of
// truth.
const (
	KimiCodingPlanQuotaEndpoint       = "https://api.kimi.com/coding/v1/usages"
	ZhipuCodingPlanQuotaEndpoint      = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"
	ZhipuZAICodingPlanQuotaEndpoint   = "https://api.z.ai/api/monitor/usage/quota/limit"
	MiniMaxCodingPlanQuotaEndpoint    = "https://api.minimaxi.com/v1/api/openplatform/coding_plan/remains"
	MiniMaxIOCodingPlanQuotaEndpoint  = "https://api.minimax.io/v1/api/openplatform/coding_plan/remains"
)
