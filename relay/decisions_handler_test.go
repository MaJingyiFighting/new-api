package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func setupDecisionsBilling(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch, oldLogs := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldBatch, oldLogs
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	return db
}

func resetDecisionsPrices(t *testing.T) {
	t.Helper()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	ratios, prices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`,
		"group_ratio_setting.group_ratio":      `{"default":1}`,
		"quota_setting.pre_consume_multiplier": "1",
	}))
}

func TestDecisionsForwardingAndBilling(t *testing.T) {
	resetDecisionsPrices(t)
	db := setupDecisionsBilling(t)
	service.InitHttpClient()
	const startingQuota = 2_000_000
	const state = `{"ticket":{"id":9007199254740993,"broken":true},"history":["delivered"]}`
	const questions = `{"damaged":{"type":"noul","instructions":{"task":"Is the screen broken?"}},"action":{"type":"choice","instructions":"Choose an action","criteria":{"replace":null,"refund":"Return the payment"}},"severity":{"type":"score","instructions":["Rate damage"],"criteria":["low","high"]}}`
	const answers = `{"damaged":{"type":"noul","noul":0.95},"action":{"type":"choice","choice":"replace","probabilities":{"replace":0.9,"refund":0.1}},"severity":{"type":"score","score":0.8,"probabilities":{"0":0.2,"1":0.8}}}`
	for index, tc := range []struct {
		name, model, path, upstreamPath string
		channelType                     int
		format                          types.RelayFormat
		wantQuota                       int
		override                        map[string]any
		failUpstream, reject, renamed   bool
	}{
		{name: "TypeSafe reserves and settles", model: "jev-latest", path: "/v1/systemone", upstreamPath: "/v1/systemone", channelType: constant.ChannelTypeOpenAI, format: types.RelayFormatTypeSafeDecisions, wantQuota: 21},
		{name: "custom URL retains mapped model", model: "jev-preview", path: "/v1/systemone", upstreamPath: "/native/mapped-model", channelType: constant.ChannelTypeCustom, format: types.RelayFormatTypeSafeDecisions, wantQuota: 21},
		{name: "OpenRouter cache is free", model: "openai/gpt-6-luna-decisions", path: "/v1/alpha/decisions", upstreamPath: "/api/alpha/decisions", channelType: constant.ChannelTypeOpenRouter, format: types.RelayFormatOpenRouterDecisions, wantQuota: 35},
		{name: "upstream failure refunds once", model: "jev-latest", path: "/v1/systemone", upstreamPath: "/v1/systemone", channelType: constant.ChannelTypeOpenAI, format: types.RelayFormatTypeSafeDecisions, failUpstream: true},
		{name: "stream override rejected before submission", model: "jev-latest", path: "/v1/systemone", channelType: constant.ChannelTypeOpenAI, format: types.RelayFormatTypeSafeDecisions, override: map[string]any{"stream": true}, reject: true},
		{name: "large token override rejected before submission", model: "jev-latest", path: "/v1/systemone", channelType: constant.ChannelTypeOpenAI, format: types.RelayFormatTypeSafeDecisions, override: map[string]any{"max_output_tokens": 2147483647}, reject: true},
		{name: "overridden questions match upstream answers", model: "jev-latest", path: "/v1/systemone", upstreamPath: "/v1/systemone", channelType: constant.ChannelTypeOpenAI, format: types.RelayFormatTypeSafeDecisions, wantQuota: 21, renamed: true, override: map[string]any{"questions": map[string]any{"renamed": map[string]any{"type": "noul", "instructions": "Is it broken?"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type receivedRequest struct {
				path, auth, contentType, title string
				body                           []byte
			}
			received := make(chan receivedRequest, 1)
			responseAnswers := answers
			if tc.renamed {
				responseAnswers = `{"renamed":{"type":"noul","noul":0.95}}`
			}
			responseBody := `{"model":"mapped-model","answers":` + responseAnswers + `,"usage":{"input_tokens":1000,"output_tokens":20,"total_tokens":1020,"input_tokens_details":{"cached_tokens":200,"cache_write_tokens":100}}}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				received <- receivedRequest{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Header.Get("X-OpenRouter-Title"), body}
				w.Header().Set("Content-Type", "application/json")
				if tc.failUpstream {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit"}}`)
					return
				}
				_, _ = io.WriteString(w, responseBody)
			}))
			t.Cleanup(upstream.Close)
			user := model.User{Username: fmt.Sprintf("decisions-%d", index), AffCode: fmt.Sprintf("decision%d", index), Quota: startingQuota, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: fmt.Sprintf("decision-fixture-%d", index), RemainQuota: startingQuota, Status: common.TokenStatusEnabled}
			require.NoError(t, db.Create(&token).Error)
			channel := model.Channel{Name: "decisions-fixture", Type: tc.channelType, Key: "fixture-key", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			body := `{"model":"` + tc.model + `","state":` + state + `,"questions":` + questions + `,"stream":false,"max_output_tokens":0,"provider":{"allow_fallbacks":false},"future":{"amount":0}}`
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			baseURL := upstream.URL
			if tc.channelType == constant.ChannelTypeOpenRouter {
				baseURL += "/api"
			}
			if tc.channelType == constant.ChannelTypeCustom {
				baseURL += "/native/{model}"
			}
			common.SetContextKey(ctx, constant.ContextKeyChannelType, tc.channelType)
			common.SetContextKey(ctx, constant.ContextKeyChannelId, channel.Id)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, baseURL)
			common.SetContextKey(ctx, constant.ContextKeyChannelKey, "fixture-key")
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, tc.model)
			if tc.override != nil {
				common.SetContextKey(ctx, constant.ContextKeyChannelParamOverride, tc.override)
			}
			ctx.Set("model_mapping", `{"`+tc.model+`":"intermediate-model","intermediate-model":"mapped-model"}`)
			request, err := helper.GetAndValidateRequest(ctx, tc.format)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(ctx, tc.format, request, nil)
			require.NoError(t, err)
			info.UserId, info.TokenId, info.TokenKey = user.Id, token.Id, token.Key
			info.UserQuota, info.UsingGroup, info.UserGroup = startingQuota, "default", "default"
			info.UserSetting, info.ForcePreConsume = dto.UserSetting{BillingPreference: "wallet_only"}, true
			price, err := helper.ModelPriceHelper(ctx, info, 2000, request.GetTokenCountMeta())
			require.NoError(t, err)
			require.Greater(t, price.QuotaToPreConsume, 0)
			require.Nil(t, service.PreConsumeBilling(ctx, price.QuotaToPreConsume, info))
			var reserved model.User
			require.NoError(t, db.First(&reserved, user.Id).Error)
			assert.Equal(t, startingQuota-price.QuotaToPreConsume, reserved.Quota)
			apiErr := DecisionsHelper(ctx, info)
			if tc.failUpstream || tc.reject {
				require.NotNil(t, apiErr)
				if tc.reject {
					assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
					assert.Empty(t, received)
				} else {
					assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
				}
				refunded := make(chan struct{}, 1)
				callback := fmt.Sprintf("decisions_refund_%d", index)
				require.NoError(t, db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "tokens" && tx.Error == nil {
						select {
						case refunded <- struct{}{}:
						default:
						}
					}
				}))
				t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove(callback)) })
				RefundFailedRequestBilling(ctx, info, apiErr)
				RefundFailedRequestBilling(ctx, info, apiErr)
				select {
				case <-refunded:
				case <-time.After(5 * time.Second):
					t.Fatal("refund did not finish")
				}
			} else {
				require.Nil(t, apiErr)
				assert.Equal(t, responseBody, recorder.Body.String())
				require.NoError(t, info.Billing.Settle(tc.wantQuota), "a repeated settlement must not charge again")
				var log model.Log
				require.NoError(t, db.Where("user_id = ?", user.Id).Take(&log).Error)
				assert.Equal(t, tc.wantQuota, log.Quota)
				assert.Equal(t, 1000, log.PromptTokens)
				assert.Equal(t, 20, log.CompletionTokens)
				assert.Equal(t, int64(200), gjson.Get(log.Other, "cache_tokens").Int())
			}
			var finalUser model.User
			var finalToken model.Token
			require.NoError(t, db.First(&finalUser, user.Id).Error)
			require.NoError(t, db.First(&finalToken, token.Id).Error)
			assert.Equal(t, startingQuota-tc.wantQuota, finalUser.Quota)
			assert.Equal(t, startingQuota-tc.wantQuota, finalToken.RemainQuota)
			if tc.reject {
				return
			}
			require.Len(t, received, 1)
			got := <-received
			assert.Equal(t, tc.upstreamPath, got.path)
			assert.Equal(t, "Bearer fixture-key", got.auth)
			assert.Equal(t, "application/json", got.contentType)
			if tc.channelType == constant.ChannelTypeOpenRouter {
				assert.Equal(t, "New API", got.title)
			}
			assert.Equal(t, "mapped-model", gjson.GetBytes(got.body, "model").String())
			assert.JSONEq(t, state, gjson.GetBytes(got.body, "state").Raw)
			assert.Equal(t, "9007199254740993", gjson.GetBytes(got.body, "state.ticket.id").Raw)
			if !tc.renamed {
				assert.JSONEq(t, questions, gjson.GetBytes(got.body, "questions").Raw)
			}
			assert.Equal(t, "false", gjson.GetBytes(got.body, "stream").Raw)
			assert.Equal(t, "false", gjson.GetBytes(got.body, "provider.allow_fallbacks").Raw)
			assert.Equal(t, "0", gjson.GetBytes(got.body, "max_output_tokens").Raw)
			assert.Equal(t, "0", gjson.GetBytes(got.body, "future.amount").Raw)
		})
	}
}

func TestDecisionsResponsePreservesAnswersAndRejectsInvalidUsage(t *testing.T) {
	const allAnswers = `{"boolean":{"type":"noul","noul":0},"selection":{"type":"choice","choice":"false"},"rating":{"type":"score","score":0},"refused":{"type":"refusal","reason":"policy"},"future":{"type":"new-kind","payload":{"flag":false}}}`
	for _, tc := range []struct {
		name, answers, usage           string
		wantError                      bool
		input, output, cached, written int
	}{
		{name: "all answer shapes and injected private billing ignored", answers: allAnswers, usage: `{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":10},"output_tokens_details":{"reasoning_tokens":5},"prompt_tokens":999999,"billing_usage":{"prompt_tokens":999999,"completion_tokens":999999}}`, input: 100, output: 20, cached: 30, written: 10},
		{name: "explicit zero", answers: `{"q":{"type":"noul","noul":0}}`, usage: `{"input_tokens":0,"output_tokens":0,"total_tokens":0}`},
		{name: "missing usage", answers: `{"q":{}}`, usage: `null`, wantError: true},
		{name: "negative input", answers: `{"q":{}}`, usage: `{"input_tokens":-1,"output_tokens":0}`, wantError: true},
		{name: "overflowing total", answers: `{"q":{}}`, usage: `{"input_tokens":2147483647,"output_tokens":1}`, wantError: true},
		{name: "negative cache", answers: `{"q":{}}`, usage: `{"input_tokens":10,"output_tokens":0,"input_tokens_details":{"cached_tokens":-1}}`, wantError: true},
		{name: "cache exceeds input", answers: `{"q":{}}`, usage: `{"input_tokens":10,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":11}}`, wantError: true},
		{name: "inconsistent total", answers: `{"q":{}}`, usage: `{"input_tokens":10,"output_tokens":2,"total_tokens":1}`, wantError: true},
		{name: "fractional token", answers: `{"q":{}}`, usage: `{"input_tokens":1.5,"output_tokens":0}`, wantError: true},
		{name: "wrong answer shape", answers: `[]`, usage: `{"input_tokens":10,"output_tokens":0}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"answers":` + tc.answers + `,"usage":` + tc.usage + `,"future_response":true}`
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", nil)
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeDecisions, RelayFormat: types.RelayFormatTypeSafeDecisions}
			usage, apiErr := openaichannel.OaiDecisionsHandler(ctx, info, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))})
			if tc.wantError {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				assert.Empty(t, recorder.Body.String())
				return
			}
			require.Nil(t, apiErr)
			assert.Equal(t, body, recorder.Body.String())
			assert.Equal(t, tc.input, usage.PromptTokens)
			assert.Equal(t, tc.output, usage.CompletionTokens)
			assert.Equal(t, tc.cached, usage.PromptTokensDetails.CachedTokens)
			assert.Equal(t, tc.written, usage.PromptTokensDetails.CacheCreationTokensTotal())
			params := service.BuildTieredTokenParams(usage, false, map[string]bool{"cr": true, "cc": true})
			assert.Equal(t, float64(tc.input-tc.cached-tc.written), params.P)
			assert.Equal(t, float64(tc.output), params.C)
		})
	}
}

func TestDecisionsRequestValidationAndEndpointDiscovery(t *testing.T) {
	for _, tc := range []struct {
		model, path string
		channel     int
		endpoint    constant.EndpointType
		format      types.RelayFormat
		mode        int
	}{
		{"jev-latest", "/v1/systemone", constant.ChannelTypeOpenAI, constant.EndpointTypeTypeSafeDecisions, types.RelayFormatTypeSafeDecisions, relayconstant.RelayModeDecisions},
		{"jev-preview", "/v1/systemone", constant.ChannelTypeCustom, constant.EndpointTypeTypeSafeDecisions, types.RelayFormatTypeSafeDecisions, relayconstant.RelayModeDecisions},
		{"jev-1.13.0", "/v1/systemone", constant.ChannelTypeOpenAI, constant.EndpointTypeTypeSafeDecisions, types.RelayFormatTypeSafeDecisions, relayconstant.RelayModeDecisions},
		{"openai/gpt-6-luna-decisions", "/v1/alpha/decisions", constant.ChannelTypeOpenRouter, constant.EndpointTypeOpenRouterDecisions, types.RelayFormatOpenRouterDecisions, relayconstant.RelayModeOpenRouterDecisions},
	} {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, []constant.EndpointType{tc.endpoint}, common.GetEndpointTypesByChannelType(tc.channel, tc.model))
			endpoint, ok := common.GetDefaultEndpointInfo(tc.endpoint)
			require.True(t, ok)
			assert.Equal(t, tc.path, endpoint.Path)
			assert.Equal(t, tc.mode, relayconstant.Path2RelayMode(tc.path))
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"model":"`+tc.model+`","state":"broken","questions":{"q":{"type":"noul","instructions":"Is it broken?"}}}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			request, err := helper.GetAndValidateRequest(ctx, tc.format)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(ctx, tc.format, request, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.mode, info.RelayMode)
			assert.False(t, info.IsStream)
		})
	}
	for _, tc := range []struct{ name, body string }{
		{"missing model", `{"state":"x","questions":{"q":{"type":"noul","instructions":"x"}}}`},
		{"streaming", `{"model":"jev-latest","state":"x","stream":true,"questions":{"q":{"type":"noul","instructions":"x"}}}`},
		{"missing state", `{"model":"jev-latest","questions":{"q":{"type":"noul","instructions":"x"}}}`},
		{"empty questions", `{"model":"jev-latest","state":"x","questions":{}}`},
		{"OpenAI array is a different protocol", `{"model":"jev-latest","state":"x","questions":[{"type":"predicate","instructions":"x"}]}`},
		{"missing choice options", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"choice","instructions":"x"}}}`},
		{"too few score levels", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"score","instructions":"x","criteria":["only"]}}}`},
		{"wrapped token value", `{"model":"jev-latest","state":"x","questions":{"q":{"type":"noul","instructions":"x"}},"max_tokens":18446744073709551615}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			_, err := helper.GetAndValidateDecisionsRequest(ctx)
			require.Error(t, err)
		})
	}
	for _, size := range []int{255, 256} {
		criteria := make(map[string]any, size)
		for i := range size {
			criteria[fmt.Sprint(i)] = nil
		}
		questions, err := common.Marshal(map[string]any{"q": map[string]any{"type": "choice", "instructions": "Choose", "criteria": criteria}})
		require.NoError(t, err)
		err = helper.ValidateDecisionsRequest(&dto.DecisionsRequest{Model: "jev-latest", State: json.RawMessage(`"x"`), Questions: questions}, relayconstant.RelayModeDecisions)
		if size == 255 {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestDecisionsPricingPreservesAdministratorOverrides(t *testing.T) {
	resetDecisionsPrices(t)
	for _, tc := range []struct {
		name, model, mode, expression, ratio, price string
		want                                        int
	}{
		{name: "TypeSafe default", model: "jev-latest", want: 21},
		{name: "OpenRouter default excludes caches", model: "openai/gpt-6-luna-decisions", want: 35},
		{name: "explicit expression wins", model: "jev-latest", mode: "tiered_expr", expression: `tier("custom", p * 2 + c * 3)`, want: 1015},
		{name: "explicit free price stays free", model: "jev-latest", mode: "tiered_expr", expression: `tier("free", p * 0)`, want: 0},
		{name: "legacy input ratio wins", model: "jev-latest", ratio: `{"jev-latest":2}`, want: 2000},
		{name: "legacy fixed price wins", model: "jev-latest", price: `{"jev-latest":0.01}`, want: 5000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
			modes, expressions := map[string]string{}, map[string]string{}
			if tc.mode != "" {
				modes[tc.model] = tc.mode
				expressions[tc.model] = tc.expression
			}
			modeJSON, err := common.Marshal(modes)
			require.NoError(t, err)
			exprJSON, err := common.Marshal(expressions)
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modeJSON), "billing_setting.billing_expr": string(exprJSON)}))
			if tc.ratio != "" {
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(tc.ratio))
			}
			if tc.price != "" {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(tc.price))
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", nil)
			info := &relaycommon.RelayInfo{OriginModelName: tc.model, UsingGroup: "default", UserGroup: "default", BillingRequestInput: &billingexpr.RequestInput{}}
			price, err := helper.ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
			require.NoError(t, err)
			if tc.ratio != "" || tc.price != "" {
				assert.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode(tc.model))
				assert.Equal(t, tc.want, price.QuotaToPreConsume)
				return
			}
			require.NotNil(t, info.TieredBillingSnapshot)
			usage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 10, TotalTokens: 1010, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CacheWriteTokens: 100}}
			params := service.BuildTieredTokenParams(usage, false, billingexpr.UsedVars(info.TieredBillingSnapshot.ExprString))
			ok, quota, _ := service.TryTieredSettle(info, params)
			require.True(t, ok)
			assert.Equal(t, tc.want, quota)
		})
	}
}
