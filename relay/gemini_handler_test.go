package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/generationdebug"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiGenerationDebugPrompt(t *testing.T) {
	for _, systemKey := range []string{"systemInstruction", "system_instruction"} {
		t.Run(systemKey, func(t *testing.T) {
			body := `{"` + systemKey + `":{"parts":[{"text":"Be precise."}]},
				"contents":[
					{"parts":[{"text":"First line"},{"text":"Second line"}]},
					{"role":"model","parts":[{"text":"Earlier answer"}]},
					{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"value":42,"api_key":"redaction-fixture"}}}]}
				],
				"tools":[{"functionDeclarations":[{"name":"lookup","description":"Read a value"}]}]}`
			prompt := generationdebug.ExtractPromptFromRequest([]byte(body))

			require.Len(t, prompt.Messages, 3)
			assert.Equal(t, "First line\nSecond line", prompt.Messages[0].Content)
			assert.Equal(t, "user", prompt.Messages[0].Role)
			assert.Equal(t, "Earlier answer", prompt.Messages[1].Content)
			assert.Equal(t, "assistant", prompt.Messages[1].Role)
			assert.Contains(t, prompt.Messages[2].Content, `"value":42`)
			assert.NotContains(t, prompt.Messages[2].Content, "redaction-fixture")
			assert.Equal(t, map[string]int{"user": 2, "assistant": 1}, prompt.RoleCounts)
			require.NotNil(t, prompt.Instructions)
			require.NotNil(t, prompt.Tools)
			require.Len(t, prompt.Units, 6)
			assert.Equal(t, systemKey+".parts[0].text", prompt.Units[0].Path)
			assert.Equal(t, "system", prompt.Units[0].Role)
			assert.Equal(t, "Be precise.", prompt.Units[0].Content)
			assert.Equal(t, "tools", prompt.Units[1].Path)
			assert.Equal(t, "contents[0].parts[0].text", prompt.Units[2].Path)
			assert.Equal(t, "contents[0].parts[1].text", prompt.Units[3].Path)
			assert.Equal(t, "assistant", prompt.Units[4].Role)
			assert.Greater(t, prompt.TotalEstimatedTokens, 0)
		})
	}
}

func TestGeminiGenerationDebugOutput(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		stream     bool
		want       generationdebug.ExtractedOutput
	}{
		{
			name: "JSON separates returned thoughts from the answer",
			body: `{"responseId":"gemini-response","candidates":[{"content":{"parts":[{"thought":true,"text":"Think first."},{"text":"Hello "},{"text":"world"}]},"finishReason":"STOP"}]}`,
			want: generationdebug.ExtractedOutput{Output: "Hello world", Reasoning: "Think first.", FinishReason: "STOP", GenerationID: "gemini-response"},
		},
		{
			name: "SSE joins parts across events",
			body: "data: " + `{"responseId":"gemini-response","candidates":[{"content":{"parts":[{"thought":true,"text":"Think first."},{"text":"Hello "}]}}]}` + "\n\n" +
				"data: " + `{"candidates":[{"content":{"parts":[{"text":"world"}]},"finishReason":"STOP"}]}` + "\n\n",
			stream: true,
			want:   generationdebug.ExtractedOutput{Output: "Hello world", Reasoning: "Think first.", FinishReason: "STOP", GenerationID: "gemini-response"},
		},
		{
			name: "blocked prompt keeps the reason without inventing output",
			body: `{"responseId":"blocked-response","promptFeedback":{"blockReason":"SAFETY"}}`,
			want: generationdebug.ExtractedOutput{FinishReason: "SAFETY", GenerationID: "blocked-response"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output generationdebug.ExtractedOutput
			if tc.stream {
				output = generationdebug.ExtractOutputFromSSE([]byte(tc.body))
			} else {
				output = generationdebug.ExtractOutputFromRawResponse([]byte(tc.body))
			}
			assert.Equal(t, tc.want, output)
		})
	}
}

func TestGeminiGenerationDebugRelay(t *testing.T) {
	t.Setenv("GENERATION_DEBUG_ENABLED", "true")
	t.Setenv("GENERATION_DEBUG_CAPTURE_RAW", "true")
	t.Setenv("GENERATION_DEBUG_CAPTURE_OUTPUT", "true")
	t.Setenv("GENERATION_DEBUG_USER_VISIBLE", "true")
	t.Setenv("GENERATION_DEBUG_SAMPLE_RATE", "1")
	t.Setenv("GENERATION_DEBUG_MAX_BYTES", "4096")
	gin.SetMode(gin.TestMode)
	db := setupDecisionsBilling(t)
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	oldPassThrough, oldTimeout := settings.PassThroughRequestEnabled, constant.StreamingTimeout
	t.Cleanup(func() {
		settings.PassThroughRequestEnabled = oldPassThrough
		constant.StreamingTimeout = oldTimeout
	})
	constant.StreamingTimeout = 5
	user := model.User{Username: "gemini-debug", AffCode: "gemini-debug", Status: common.UserStatusEnabled}
	channel := model.Channel{Name: "gemini-debug", Type: constant.ChannelTypeGemini, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&channel).Error)

	for index, tc := range []struct {
		name                                                    string
		stream, passthrough, globalPassthrough, compatible, off bool
	}{
		{name: "native JSON"},
		{name: "native SSE", stream: true},
		{name: "native passthrough", passthrough: true},
		{name: "native global passthrough SSE", stream: true, globalPassthrough: true},
		{name: "OpenAI request with Gemini upstream", compatible: true},
		{name: "OpenAI request with Gemini upstream SSE", compatible: true, stream: true},
		{name: "capture disabled", off: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GENERATION_DEBUG_ENABLED", strconv.FormatBool(!tc.off))
			settings.PassThroughRequestEnabled = tc.globalPassthrough
			usage := `"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":1,"totalTokenCount":13,"cachedContentTokenCount":4}`
			response := `{"responseId":"gemini-response","candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"Think first."},{"text":"Hello world"}]},"finishReason":"STOP"}],` + usage + `}`
			if tc.stream {
				response = "data: " + `{"responseId":"gemini-response","candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"Think first."},{"text":"Hello "}]}}]}` + "\n\n" +
					"data: " + `{"candidates":[{"content":{"role":"model","parts":[{"text":"world"}]},"finishReason":"STOP"}],` + usage + `}` + "\n\n"
			}
			type receivedRequest struct {
				path string
				body []byte
			}
			received := make(chan receivedRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				received <- receivedRequest{path: r.URL.Path, body: body}
				w.Header().Set("Content-Type", "application/json")
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				_, _ = io.WriteString(w, response)
			}))
			t.Cleanup(upstream.Close)
			action := "generateContent"
			if tc.stream {
				action = "streamGenerateContent"
			}
			path := "/v1beta/models/client-model:" + action
			body := `{"systemInstruction":{"parts":[{"text":"Client system"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}],"extra":{"api_key":"redaction-fixture"}}`
			var format types.RelayFormat = types.RelayFormatGemini
			if tc.compatible {
				path = "/v1/chat/completions"
				body = `{"model":"client-model","messages":[{"role":"system","content":"Client system"},{"role":"user","content":"hello"}],"stream":` + strconv.FormatBool(tc.stream) + `,"extra":{"api_key":"redaction-fixture"}}`
				format = types.RelayFormatOpenAI
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
			requestID := fmt.Sprintf("gemini-debug-%d", index)
			ctx.Set(common.RequestIdKey, requestID)
			common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
			common.SetContextKey(ctx, constant.ContextKeyChannelId, channel.Id)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "client-model")
			common.SetContextKey(ctx, constant.ContextKeyUserId, user.Id)
			common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{
				PassThroughBodyEnabled: tc.passthrough,
				SystemPrompt:           "Channel system",
				SystemPromptOverride:   true,
			})
			ctx.Set("model_mapping", `{"client-model":"gemini-2.5-flash"}`)
			request, err := helper.GetAndValidateRequest(ctx, format)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(ctx, format, request, nil)
			require.NoError(t, err)
			if tc.compatible {
				require.Nil(t, TextHelper(ctx, info))
			} else {
				require.Nil(t, GeminiHelper(ctx, info))
			}
			require.Len(t, received, 1)
			sent := <-received
			assert.Equal(t, "/v1beta/models/gemini-2.5-flash:"+action, sent.path)
			if tc.passthrough || tc.globalPassthrough {
				assert.JSONEq(t, body, string(sent.body))
			} else if !tc.compatible {
				assert.Equal(t, "Channel system\nClient system", gjson.GetBytes(sent.body, "systemInstruction.parts.0.text").String())
			}
			assert.Equal(t, http.StatusOK, recorder.Code)
			if !tc.compatible && !tc.stream {
				assert.JSONEq(t, response, recorder.Body.String())
			} else {
				assert.Contains(t, recorder.Body.String(), "Hello ")
				assert.Contains(t, recorder.Body.String(), "world")
			}

			var log model.Log
			require.NoError(t, db.Where("request_id = ?", requestID).First(&log).Error)
			assert.Equal(t, 10, log.PromptTokens)
			assert.Equal(t, 3, log.CompletionTokens)
			var details struct {
				Summary *generationdebug.Summary `json:"generation_debug"`
				Admin   struct {
					Raw *generationdebug.RawDebug `json:"generation_debug_raw"`
				} `json:"admin_info"`
			}
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &details))
			if tc.off {
				assert.Nil(t, details.Summary)
				assert.Nil(t, details.Admin.Raw)
				return
			}
			require.NotNil(t, details.Summary)
			require.NotNil(t, details.Summary.Prompt)
			require.NotNil(t, details.Summary.Completion)
			assert.Equal(t, requestID, details.Summary.RequestID)
			assert.Equal(t, tc.stream, details.Summary.Streaming)
			assert.Equal(t, "gemini-response", details.Summary.GenerationID)
			assert.Equal(t, "Hello world", details.Summary.Completion.NormalizedOutput)
			assert.Equal(t, "Think first.", details.Summary.Completion.ReasoningOutput)
			assert.Equal(t, "STOP", details.Summary.Completion.FinishReason)
			assert.Equal(t, 4, details.Summary.Cache.CachedTokens)
			require.Len(t, details.Summary.Prompt.UpstreamMessages, 1)
			assert.Equal(t, "hello", details.Summary.Prompt.UpstreamMessages[0].Content)
			raw := details.Admin.Raw
			require.NotNil(t, raw)
			require.NotNil(t, raw.InboundRequest)
			require.NotNil(t, raw.UpstreamRequest)
			assert.NotContains(t, log.Other, "redaction-fixture")
			capturedRequest, err := common.Marshal(raw.UpstreamRequest.Value)
			require.NoError(t, err)
			safeRequest, err := generationdebug.SanitizeJSON(sent.body)
			require.NoError(t, err)
			assert.JSONEq(t, string(safeRequest), string(capturedRequest))
			if tc.stream {
				require.NotNil(t, raw.RawStream)
				assert.Nil(t, raw.RawResponse)
				assert.Contains(t, raw.RawStream.Value, "gemini-response")
				assert.Equal(t, len(response), raw.RawStream.CapturedBytes)
			} else {
				require.NotNil(t, raw.RawResponse)
				assert.Nil(t, raw.RawStream)
				capturedResponse, err := common.Marshal(raw.RawResponse.Value)
				require.NoError(t, err)
				assert.JSONEq(t, response, string(capturedResponse))
			}
		})
	}
}
