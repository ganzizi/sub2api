package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// grokQualityDegradedSameAccountRetry allows one more attempt on the same
// account when grok2api rejected a grok-* turn for missing reasoning. The
// account is not added to the failed set, so the scheduler does not drop it.
func grokQualityDegradedSameAccountRetry(account *service.Account, requestedModel, dispatchMappedModel string, failoverErr *service.UpstreamFailoverError, retryCount map[int64]int) bool {
	if account == nil || retryCount == nil || retryCount[account.ID] >= 1 {
		return false
	}
	if !service.ShouldSameAccountRetryGrokQualityDegraded(account, requestedModel, dispatchMappedModel, failoverErr) {
		return false
	}
	retryCount[account.ID]++
	return true
}
