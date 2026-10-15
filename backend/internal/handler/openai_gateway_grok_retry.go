package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	// grokQualityDegradedMaxAttempts is how many extra upstream calls one client
	// request will spend after grok2api says the response had no reasoning.
	// One extra try is not enough: the replacement account often fails the same way.
	grokQualityDegradedMaxAttempts = 8
	// grokQualityDegradedRetryBudget keeps those extra tries inside the window
	// before a client gives up waiting for the first byte.
	grokQualityDegradedRetryBudget = 90 * time.Second
)

type grokQualityRetryWindow struct {
	attempts int
	started  time.Time
}

// grokQualityDegradedSameAccountRetry keeps the current account and tries the
// upstream again. The account is not added to the failed set, so a lone grok
// upstream is not dropped after the first quality_degraded. Other 5xx do not
// enter this path.
func grokQualityDegradedSameAccountRetry(account *service.Account, requestedModel, dispatchMappedModel string, failoverErr *service.UpstreamFailoverError, windows map[int64]*grokQualityRetryWindow) bool {
	if account == nil || windows == nil || !service.ShouldSameAccountRetryGrokQualityDegraded(account, requestedModel, dispatchMappedModel, failoverErr) {
		return false
	}
	window := windows[account.ID]
	if window == nil {
		window = &grokQualityRetryWindow{started: time.Now()}
		windows[account.ID] = window
	}
	if window.attempts >= grokQualityDegradedMaxAttempts || time.Since(window.started) >= grokQualityDegradedRetryBudget {
		return false
	}
	window.attempts++
	return true
}
