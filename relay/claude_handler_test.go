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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeGenerationDebugRelay(t *testing.T) {
	t.Setenv("GENERATION_DEBUG_ENABLED", "true")
	t.Setenv("GENERATION_DEBUG_CAPTURE_RAW", "true")
	t.Setenv("GENERATION_DEBUG_CAPTURE_OUTPUT", "true")
	t.Setenv("GENERATION_DEBUG_USER_VISIBLE", "true")
	t.Setenv("GENERATION_DEBUG_SAMPLE_RATE", "1")
	t.Setenv("GENERATION_DEBUG_MAX_BYTES", "4096")
	gin.SetMode(gin.TestMode)
	db := setupDecisionsBilling(t)
	service.InitHttpClient()
	oldTimeout := constant.StreamingTimeout
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	constant.StreamingTimeout = 5
	user := model.User{Username: "claude-debug", AffCode: "claude-debug", Status: common.UserStatusEnabled}
	channel := model.Channel{Name: "claude-debug", Type: constant.ChannelTypeAnthropic, Key: "fixture-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&channel).Error)

	claudeResponse := `{"id":"msg-debug","type":"message","role":"assistant","model":"claude-mapped",` +
		`"content":[{"type":"thinking","thinking":"Think first."},{"type":"text","text":"Hello world"}],` +
		`"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":4}}`
	claudeStream := "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"msg-debug","type":"message","role":"assistant","model":"claude-mapped","content":[],"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":4}}}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Think first."}}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello "}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"world"}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}` + "\n\n" +
		"event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	for index, tc := range []struct {
		name                     string
		stream, passthrough, off bool
	}{
		{name: "native JSON"},
		{name: "native SSE", stream: true},
		{name: "native passthrough", passthrough: true},
		{name: "capture disabled", off: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GENERATION_DEBUG_ENABLED", strconv.FormatBool(!tc.off))
			response := claudeResponse
			if tc.stream {
				response = claudeStream
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
			body := `{"model":"client-model","max_tokens":64,"system":"Client system",` +
				`"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],` +
				`"stream":` + strconv.FormatBool(tc.stream) + `,"extra":{"api_key":"redaction-fixture"}}`
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
			requestID := fmt.Sprintf("claude-debug-%d", index)
			ctx.Set(common.RequestIdKey, requestID)
			common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
			common.SetContextKey(ctx, constant.ContextKeyChannelId, channel.Id)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(ctx, constant.ContextKeyChannelKey, "fixture-key")
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "client-model")
			common.SetContextKey(ctx, constant.ContextKeyUserId, user.Id)
			common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{
				PassThroughBodyEnabled: tc.passthrough,
			})
			ctx.Set("model_mapping", `{"client-model":"claude-mapped"}`)
			request, err := helper.GetAndValidateRequest(ctx, types.RelayFormatClaude)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(ctx, types.RelayFormatClaude, request, nil)
			require.NoError(t, err)
			require.Nil(t, ClaudeHelper(ctx, info))
			require.Len(t, received, 1)
			sent := <-received
			assert.Equal(t, "/v1/messages", sent.path)
			if tc.passthrough {
				assert.JSONEq(t, body, string(sent.body))
			} else {
				assert.Equal(t, "claude-mapped", gjson.GetBytes(sent.body, "model").String())
			}
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "Hello")

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
			assert.Equal(t, "msg-debug", details.Summary.GenerationID)
			assert.Equal(t, "Hello world", details.Summary.Completion.NormalizedOutput)
			assert.Equal(t, "Think first.", details.Summary.Completion.ReasoningOutput)
			assert.Equal(t, "end_turn", details.Summary.Completion.FinishReason)
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
				assert.Contains(t, raw.RawStream.Value, "msg-debug")
				assert.Equal(t, len(claudeStream), raw.RawStream.CapturedBytes)
			} else {
				require.NotNil(t, raw.RawResponse)
				assert.Nil(t, raw.RawStream)
				capturedResponse, err := common.Marshal(raw.RawResponse.Value)
				require.NoError(t, err)
				assert.JSONEq(t, claudeResponse, string(capturedResponse))
			}
		})
	}
}
