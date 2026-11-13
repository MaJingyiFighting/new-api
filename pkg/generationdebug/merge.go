package generationdebug

func MergeIntoLogOther(other map[string]any, summary *Summary, raw *RawDebug) {
	if other == nil || summary == nil {
		return
	}
	other["generation_debug"] = summary
	if raw == nil {
		return
	}
	adminInfo, ok := other["admin_info"].(map[string]any)
	if !ok || adminInfo == nil {
		adminInfo = make(map[string]any)
		other["admin_info"] = adminInfo
	}
	adminInfo["generation_debug_raw"] = raw
}
