package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

func SupportsDecisionsEndpoint(channelType, relayMode int) bool {
	switch relayMode {
	case relayconstant.RelayModeDecisions:
		return channelType == constant.ChannelTypeOpenAI || channelType == constant.ChannelTypeCustom
	case relayconstant.RelayModeOpenRouterDecisions:
		return channelType == constant.ChannelTypeOpenRouter
	default:
		return false
	}
}

func GetAndValidateDecisionsRequest(c *gin.Context) (*dto.DecisionsRequest, error) {
	request := &dto.DecisionsRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if err := ValidateDecisionsRequest(request, relayconstant.Path2RelayMode(c.Request.URL.Path)); err != nil {
		return nil, err
	}
	return request, nil
}

// ValidateDecisionsRequest is also applied after channel parameter overrides,
// so passthrough and channel tests obey the same request limits.
func ValidateDecisionsRequest(request *dto.DecisionsRequest, relayMode int) error {
	if request == nil || strings.TrimSpace(request.Model) == "" {
		return errors.New("model is required")
	}
	if request.Stream != nil && *request.Stream {
		return errors.New("decisions do not support streaming")
	}
	switch common.GetJsonType(request.State) {
	case "string", "object", "array":
	default:
		return errors.New("state must be a string, object, or array")
	}
	var questions map[string]struct {
		Type         string          `json:"type"`
		Instructions json.RawMessage `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	if err := common.Unmarshal(request.Questions, &questions); err != nil || len(questions) == 0 {
		return errors.New("questions must be a nonempty object")
	}
	for _, question := range questions {
		switch common.GetJsonType(question.Instructions) {
		case "string", "object", "array":
		default:
			return errors.New("question instructions must be a string, object, or array")
		}
		switch question.Type {
		case "noul", "choice":
			if question.Type == "noul" && len(question.Criteria) == 0 {
				continue
			}
			var criteria map[string]json.RawMessage
			if err := common.Unmarshal(question.Criteria, &criteria); err != nil || len(criteria) == 0 {
				return errors.New("noul and choice criteria must be a nonempty object")
			}
			if question.Type == "choice" && relayMode == relayconstant.RelayModeDecisions && len(criteria) > 255 {
				return errors.New("TypeSafe choice questions support at most 255 options")
			}
			if question.Type == "noul" && relayMode == relayconstant.RelayModeOpenRouterDecisions && (criteria["true"] == nil || criteria["false"] == nil) {
				return errors.New("OpenRouter noul criteria require true and false")
			}
			for _, value := range criteria {
				switch common.GetJsonType(value) {
				case "string", "object", "array":
				case "null":
					if question.Type == "choice" {
						continue
					}
					fallthrough
				default:
					return errors.New("invalid question criteria value")
				}
			}
		case "score":
			var criteria []json.RawMessage
			if err := common.Unmarshal(question.Criteria, &criteria); err != nil || len(criteria) == 0 {
				return errors.New("score criteria must be a nonempty array")
			}
			if relayMode == relayconstant.RelayModeDecisions && (len(criteria) < 2 || len(criteria) > 10) {
				return errors.New("TypeSafe score questions require 2 to 10 levels")
			}
			for _, value := range criteria {
				switch common.GetJsonType(value) {
				case "string", "object", "array":
				default:
					return errors.New("score levels must be strings, objects, or arrays")
				}
			}
		default:
			return errors.New("question type must be noul, choice, or score")
		}
	}
	// These fields are not part of either Decisions API today. Bound them if
	// present in a passthrough body rather than letting a future provider turn
	// an unchecked value into a billing multiplier.
	if len(request.RawBody) > 0 {
		var limits struct {
			MaxTokens           *uint `json:"max_tokens"`
			MaxCompletionTokens *uint `json:"max_completion_tokens"`
			MaxOutputTokens     *uint `json:"max_output_tokens"`
		}
		if err := common.Unmarshal(request.RawBody, &limits); err != nil {
			return fmt.Errorf("invalid token limit: %w", err)
		}
		if ExceedsMaxTokensLimit(limits.MaxTokens, limits.MaxCompletionTokens, limits.MaxOutputTokens) {
			return errors.New("max_tokens is invalid")
		}
	}
	return nil
}
