package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestResolveChannelSelectionError(t *testing.T) {
	channelErr := types.NewError(
		errors.New("no available channel"),
		types.ErrorCodeGetChannelFailed,
		types.ErrOptionWithSkipRetry(),
	)

	tests := []struct {
		name           string
		lastRelayError *types.NewAPIError
		want           *types.NewAPIError
	}{
		{
			name: "preserves upstream 429 after channels are exhausted",
			lastRelayError: types.NewOpenAIError(
				errors.New("resource exhausted"),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusTooManyRequests,
			),
		},
		{
			name: "preserves upstream 503 after channels are exhausted",
			lastRelayError: types.NewOpenAIError(
				errors.New("service unavailable"),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusServiceUnavailable,
			),
		},
		{
			name: "keeps channel selection error when no upstream was called",
			want: channelErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := test.want
			if want == nil {
				want = test.lastRelayError
			}

			got := resolveChannelSelectionError(channelErr, test.lastRelayError)

			require.Same(t, want, got)
		})
	}
}
