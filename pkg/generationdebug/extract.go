package generationdebug

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func BuildCacheStatsFromUsage(usage *dto.Usage) CacheStats {
	if usage == nil {
		return CacheStats{}
	}
	cachedTokens := max(
		usage.PromptTokensDetails.CachedTokens,
		usage.PromptCacheHitTokens,
	)
	if usage.InputTokensDetails != nil {
		cachedTokens = max(cachedTokens, usage.InputTokensDetails.CachedTokens)
	}
	cacheWriteTokens := usage.PromptTokensDetails.CachedCreationTokens
	splitCacheWriteTokens := usage.ClaudeCacheCreation5mTokens + usage.ClaudeCacheCreation1hTokens
	if splitCacheWriteTokens > cacheWriteTokens {
		cacheWriteTokens = splitCacheWriteTokens
	}
	return CacheStats{
		CachedTokens:     cachedTokens,
		CacheWriteTokens: cacheWriteTokens,
		CacheHitRate:     float64(cachedTokens) / float64(max(usage.PromptTokens, 1)),
	}
}

func ExtractPromptFromRequest(data []byte) PromptDebug {
	result := PromptDebug{
		RoleCounts: make(map[string]int),
		Estimated:  true,
	}
	var root map[string]any
	if err := common.Unmarshal(data, &root); err != nil {
		return result
	}
	result.Instructions = root["instructions"]
	if result.Instructions == nil {
		result.Instructions = root["instruction"]
	}
	result.Tools = root["tools"]
	if result.Tools == nil {
		result.Tools = root["functions"]
	}

	if messages, ok := root["messages"].([]any); ok {
		result.Messages = extractMessages(messages)
	} else if inputs, ok := root["input"].([]any); ok {
		result.Messages = extractMessages(inputs)
	} else if input, ok := root["input"].(string); ok {
		result.Messages = []PromptMessage{newPromptMessage("user", input, false, 0)}
	} else if prompt, ok := root["prompt"].(string); ok {
		result.Messages = []PromptMessage{newPromptMessage("user", prompt, false, 0)}
	}
	for _, message := range result.Messages {
		result.RoleCounts[message.Role]++
		result.TotalEstimatedTokens += message.EstimatedTokens
	}
	if result.Instructions != nil {
		result.TotalEstimatedTokens += estimateTokens(contentText(result.Instructions))
	}
	if result.Tools != nil {
		result.TotalEstimatedTokens += estimateTokens(contentText(result.Tools))
	}
	return result
}

func ExtractOutputFromRawResponse(data []byte) ExtractedOutput {
	var root map[string]any
	if err := common.Unmarshal(data, &root); err != nil {
		return ExtractedOutput{}
	}
	result := ExtractedOutput{
		GenerationID: stringValue(root["id"]),
	}
	if choices, ok := root["choices"].([]any); ok {
		for _, choiceValue := range choices {
			choice, ok := choiceValue.(map[string]any)
			if !ok {
				continue
			}
			result.FinishReason = firstNonEmpty(result.FinishReason, stringValue(choice["finish_reason"]))
			if message, ok := choice["message"].(map[string]any); ok {
				result.Output += contentText(message["content"])
				result.Reasoning += firstNonEmpty(
					contentText(message["reasoning_content"]),
					contentText(message["reasoning"]),
				)
			} else {
				result.Output += contentText(choice["text"])
			}
		}
	}
	if outputs, ok := root["output"].([]any); ok {
		result.Output += extractResponsesOutput(outputs)
	}
	if result.FinishReason == "" {
		result.FinishReason = responseFinishReason(root)
	}
	return result
}

func ExtractOutputFromSSE(data []byte) ExtractedOutput {
	var result ExtractedOutput
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), max(len(data), 64<<10))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var root map[string]any
		if err := common.UnmarshalJsonStr(payload, &root); err != nil {
			continue
		}
		result.GenerationID = firstNonEmpty(result.GenerationID, stringValue(root["id"]))
		eventType := stringValue(root["type"])
		switch eventType {
		case "response.output_text.delta":
			result.Output += stringValue(root["delta"])
		case "response.reasoning_summary_text.delta":
			result.Reasoning += stringValue(root["delta"])
		case "response.completed", "response.incomplete":
			if response, ok := root["response"].(map[string]any); ok {
				result.GenerationID = firstNonEmpty(result.GenerationID, stringValue(response["id"]))
				if outputs, ok := response["output"].([]any); ok && result.Output == "" {
					result.Output = extractResponsesOutput(outputs)
				}
				result.FinishReason = firstNonEmpty(result.FinishReason, responseFinishReason(response))
			}
		}
		if choices, ok := root["choices"].([]any); ok {
			for _, choiceValue := range choices {
				choice, ok := choiceValue.(map[string]any)
				if !ok {
					continue
				}
				result.FinishReason = firstNonEmpty(result.FinishReason, stringValue(choice["finish_reason"]))
				if delta, ok := choice["delta"].(map[string]any); ok {
					result.Output += contentText(delta["content"])
					result.Reasoning += firstNonEmpty(
						contentText(delta["reasoning_content"]),
						contentText(delta["reasoning"]),
					)
				}
			}
		}
	}
	return result
}

func extractMessages(values []any) []PromptMessage {
	messages := make([]PromptMessage, 0, len(values))
	for _, value := range values {
		message, ok := value.(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(message["role"])
		if role == "" {
			role = "user"
		}
		content := contentText(message["content"])
		if content == "" {
			content = contentText(message["text"])
		}
		cached := containsCacheMarker(message)
		messages = append(messages, newPromptMessage(role, content, cached, len(messages)))
	}
	return messages
}

func newPromptMessage(role, content string, cached bool, index int) PromptMessage {
	return PromptMessage{
		Role:            role,
		Content:         content,
		EstimatedTokens: estimateTokens(content),
		Cached:          cached,
		Index:           index,
	}
}

func containsCacheMarker(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if lower == "cache_control" || lower == "cached" || lower == "prompt_cache_key" {
				return true
			}
			if containsCacheMarker(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsCacheMarker(child) {
				return true
			}
		}
	}
	return false
}

func contentText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return sanitizeString(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, child := range typed {
			if text := contentText(child); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content", "input_text", "output_text"} {
			if text := contentText(typed[key]); text != "" {
				return text
			}
		}
		if mediaType := stringValue(typed["type"]); mediaType != "" {
			return fmt.Sprintf("[%s omitted]", mediaType)
		}
		data, err := common.Marshal(sanitizeValue(typed, ""))
		if err == nil {
			return string(data)
		}
	default:
		return fmt.Sprint(value)
	}
	return ""
}

func extractResponsesOutput(outputs []any) string {
	var builder strings.Builder
	for _, outputValue := range outputs {
		output, ok := outputValue.(map[string]any)
		if !ok {
			continue
		}
		builder.WriteString(contentText(output["content"]))
	}
	return builder.String()
}

func responseFinishReason(root map[string]any) string {
	if details, ok := root["incomplete_details"].(map[string]any); ok {
		if reason := stringValue(details["reason"]); reason != "" {
			return reason
		}
		if reason := stringValue(details["reasoning"]); reason != "" {
			return reason
		}
	}
	status := stringValue(root["status"])
	if status == "completed" {
		return "stop"
	}
	return status
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return int(math.Ceil(float64(utf8.RuneCountInString(text)) / 4))
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
