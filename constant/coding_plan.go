package constant

// CodingPlanProvider identifies a domestic AI platform that exposes a
// subscription-style "Coding Plan" with shared 5-hour / weekly token quotas
// rather than per-request pay-as-you-go billing.
type CodingPlanProvider string

const (
	CodingPlanProviderKimi       CodingPlanProvider = "kimi"
	CodingPlanProviderZhipu      CodingPlanProvider = "zhipu"
	CodingPlanProviderMiniMax    CodingPlanProvider = "minimax"
	CodingPlanProviderVolcengine CodingPlanProvider = "volcengine"
	CodingPlanProviderMiMo       CodingPlanProvider = "mimo"
)