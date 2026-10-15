//go:build unit

package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func grokQualityAccount() *service.Account {
	return &service.Account{
		ID:       3,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-test",
			"model_mapping": map[string]any{
				"deep-model": "grok-4.6",
			},
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses),
		},
	}
}

func TestGrokQualityDegradedRetryContinuesPastTheFirstFailure(t *testing.T) {
	account := grokQualityAccount()
	failover := &service.UpstreamFailoverError{
		StatusCode:   http.StatusServiceUnavailable,
		ResponseBody: []byte(`{"error":{"code":"quality_degraded"}}`),
	}
	windows := map[int64]*grokQualityRetryWindow{}
	continued := 0
	for grokQualityDegradedSameAccountRetry(account, "deep-model", "", failover, windows) {
		continued++
	}
	require.Equal(t, grokQualityDegradedMaxAttempts, continued)
	require.False(t, grokQualityDegradedSameAccountRetry(account, "deep-model", "", failover, windows))
}

func TestGrokQualityDegradedRetryStopsWhenTheBudgetIsSpent(t *testing.T) {
	account := grokQualityAccount()
	failover := &service.UpstreamFailoverError{
		StatusCode:   http.StatusServiceUnavailable,
		ResponseBody: []byte(`{"error":{"code":"quality_degraded"}}`),
	}
	windows := map[int64]*grokQualityRetryWindow{
		account.ID: {started: time.Now().Add(-grokQualityDegradedRetryBudget)},
	}
	require.False(t, grokQualityDegradedSameAccountRetry(account, "deep-model", "", failover, windows))
	require.Zero(t, windows[account.ID].attempts)
}

func TestGrokQualityDegradedRetryIgnoresOtherUpstreamFailures(t *testing.T) {
	account := grokQualityAccount()
	failover := &service.UpstreamFailoverError{
		StatusCode:   http.StatusServiceUnavailable,
		ResponseBody: []byte(`{"error":{"code":"server_error"}}`),
	}
	windows := map[int64]*grokQualityRetryWindow{}
	require.False(t, grokQualityDegradedSameAccountRetry(account, "deep-model", "", failover, windows))
	require.NotContains(t, windows, account.ID)
}
