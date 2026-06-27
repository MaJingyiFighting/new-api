package constant

type MultiKeyMode string

const (
	MultiKeyModeRandom    MultiKeyMode = "random"     // 随机
	MultiKeyModePolling   MultiKeyMode = "polling"    // 轮询
	MultiKeyModeLoadAware MultiKeyMode = "load_aware" // 负载感知:基于配额使用率、最近使用时间、429 风暴检测选择最合适的 key
)
