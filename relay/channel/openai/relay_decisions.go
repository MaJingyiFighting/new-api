package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// OaiDecisionsHandler preserves native answers, including refusals and future
// answer types, while accepting only public, validated token counts for billing.
func OaiDecisionsHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(errors.New("empty decisions response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	var response struct {
		Model   string          `json:"model"`
		Answers json.RawMessage `json:"answers"`
		Error   json.RawMessage `json:"error"`
		Usage   *struct {
			InputTokens         *int                    `json:"input_tokens"`
			OutputTokens        *int                    `json:"output_tokens"`
			TotalTokens         *int                    `json:"total_tokens"`
			InputTokensDetails  *dto.InputTokenDetails  `json:"input_tokens_details"`
			OutputTokensDetails *dto.OutputTokenDetails `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if errorBody := bytes.TrimSpace(response.Error); len(errorBody) > 0 && !bytes.Equal(errorBody, []byte("null")) {
		var upstreamError types.OpenAIError
		if err := common.Unmarshal(errorBody, &upstreamError); err != nil || upstreamError.Message == "" {
			return nil, types.NewOpenAIError(errors.New("upstream returned a decisions error"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		return nil, types.WithOpenAIError(upstreamError, http.StatusBadGateway)
	}
	var answers map[string]json.RawMessage
	if err := common.Unmarshal(response.Answers, &answers); err != nil || len(answers) == 0 {
		return nil, types.NewOpenAIError(errors.New("decisions answers must be a non-empty object"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	request, _ := info.Request.(*dto.DecisionsRequest)
	if request != nil {
		var questions map[string]json.RawMessage
		if err := common.Unmarshal(request.Questions, &questions); err != nil || len(questions) != len(answers) {
			return nil, types.NewOpenAIError(errors.New("decisions answers do not match the questions"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		for name := range questions {
			if _, ok := answers[name]; !ok {
				return nil, types.NewOpenAIError(errors.New("decisions answer is missing for a question"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
	}
	for _, answer := range answers {
		if common.GetJsonType(answer) != "object" {
			return nil, types.NewOpenAIError(errors.New("each decisions answer must be an object"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
	}
	if response.Usage == nil || response.Usage.InputTokens == nil || response.Usage.OutputTokens == nil {
		return nil, types.NewOpenAIError(errors.New("decisions response is missing token usage"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	inputTokens, outputTokens := *response.Usage.InputTokens, *response.Usage.OutputTokens
	if inputTokens < 0 || inputTokens > common.MaxQuota || outputTokens < 0 || outputTokens > common.MaxQuota-inputTokens {
		return nil, types.NewOpenAIError(errors.New("decisions token usage is out of range"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	totalTokens := inputTokens + outputTokens
	if response.Usage.TotalTokens != nil && *response.Usage.TotalTokens != totalTokens {
		return nil, types.NewOpenAIError(errors.New("decisions total token usage is inconsistent"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if details := response.Usage.InputTokensDetails; details != nil {
		for _, count := range []int{details.CachedTokens, details.CacheWriteTokens, details.CachedCreationTokens, details.TextTokens, details.ImageTokens, details.AudioTokens} {
			if count < 0 || count > inputTokens {
				return nil, types.NewOpenAIError(errors.New("decisions input token details are out of range"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		if cached := details.CachedTokensDetails; cached != nil {
			for _, count := range []*int{cached.TextTokens, cached.ImageTokens, cached.AudioTokens} {
				if count != nil && (*count < 0 || *count > details.CachedTokens) {
					return nil, types.NewOpenAIError(errors.New("decisions cached token details are out of range"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
			}
		}
	}
	if details := response.Usage.OutputTokensDetails; details != nil {
		for _, count := range []int{details.TextTokens, details.ImageTokens, details.AudioTokens, details.ReasoningTokens} {
			if count < 0 || count > outputTokens {
				return nil, types.NewOpenAIError(errors.New("decisions output token details are out of range"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
	}
	usage := &dto.Usage{}
	service.ApplyResponsesUsage(usage, &dto.Usage{
		InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: totalTokens,
		InputTokensDetails: response.Usage.InputTokensDetails, OutputTokensDetails: response.Usage.OutputTokensDetails,
	})
	info.ObserveResponseModel(response.Model)
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}
