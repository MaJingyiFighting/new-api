package relay

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func DecisionsHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	if !helper.SupportsDecisionsEndpoint(info.ChannelType, info.RelayMode) {
		return types.NewError(errors.New("channel does not support this decisions endpoint"), types.ErrorCodeInvalidRequest)
	}
	request, ok := info.Request.(*dto.DecisionsRequest)
	if !ok {
		return types.NewError(errors.New("invalid decisions request"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	// Each retry starts from the original request, including the original model.
	upstreamRequest := *request
	if err := helper.ModelMappedHelper(c, info, &upstreamRequest); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	body, err := common.Marshal(upstreamRequest)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		body, err = relaycommon.ApplyParamOverrideWithRelayInfo(body, info)
		if err != nil {
			return newAPIErrorFromParamOverride(err)
		}
	}
	if err := common.Unmarshal(body, &upstreamRequest); err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ValidateDecisionsRequest(&upstreamRequest, info.RelayMode); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	// Match answers against the questions actually sent, while keeping the
	// original request available if this channel fails and another is tried.
	info.Request = &upstreamRequest
	defer func() { info.Request = request }()
	info.UpstreamModelName = upstreamRequest.Model
	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	requestBody, closer, err := relaycommon.NewOutboundJSONBody(body)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()
	response, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	httpResponse, ok := response.(*http.Response)
	if !ok || httpResponse == nil {
		return types.NewOpenAIError(errors.New("invalid decisions response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if httpResponse.StatusCode != http.StatusOK {
		apiErr := service.RelayErrorHandler(c.Request.Context(), httpResponse, false)
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return apiErr
	}
	usage, apiErr := adaptor.DoResponse(c, httpResponse, info)
	if apiErr != nil {
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return apiErr
	}
	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}
