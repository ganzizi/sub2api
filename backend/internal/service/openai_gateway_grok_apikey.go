package service

import (
	"context"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const grokQualityDegradedCode = "quality_degraded"

type inboundReasoningDisabledContextKey struct{}

// isGrokFamilyModel reports a mapped upstream model in the grok-* family.
// The gate is the model name, never an account id, name, or base URL.
func isGrokFamilyModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return strings.HasPrefix(model, "grok-")
}

func grokAPIKeyUpstreamModel(account *Account, requestedModel, messagesDispatchMappedModel string) string {
	if account == nil {
		return ""
	}
	mapped := resolveOpenAIForwardModel(account, requestedModel, messagesDispatchMappedModel)
	return normalizeOpenAIModelForUpstream(account, mapped)
}

// shouldForceGrokAPIKeyChat keeps OpenAI API-key accounts that map onto grok-*
// on Chat Completions. grok2api's Responses scanner does not see reasoning_content.
// Official PlatformGrok stays on its own forwarder.
func shouldForceGrokAPIKeyChat(account *Account, requestedModel, messagesDispatchMappedModel string) bool {
	if account == nil || account.Platform == PlatformGrok || !account.IsOpenAIApiKey() {
		return false
	}
	return isGrokFamilyModel(grokAPIKeyUpstreamModel(account, requestedModel, messagesDispatchMappedModel))
}

// InboundReasoningDisabled reports an explicit client request for no reasoning.
// Canonical effort capture drops "none"; grok2api only skips its quality hold
// when the outbound effort is the explicit value.
func InboundReasoningDisabled(body []byte) bool {
	if strings.EqualFold(strings.TrimSpace(explicitRequestedReasoningEffortFromBody(body)), "none") {
		return true
	}
	thinking := gjson.GetBytes(body, "thinking.type")
	return thinking.Exists() && strings.EqualFold(strings.TrimSpace(thinking.String()), "disabled")
}

// WithInboundReasoningDisabled marks a request whose client asked for no reasoning
// before group policy rewrote the body.
func WithInboundReasoningDisabled(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, inboundReasoningDisabledContextKey{}, true)
}

func inboundReasoningDisabledFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	disabled, _ := ctx.Value(inboundReasoningDisabledContextKey{}).(bool)
	return disabled
}

// preserveGrokExplicitNoneReasoningEffort puts effort "none" back on a grok-*
// Chat Completions body. Group policy runs while the body model is still the
// client alias, so a none → minimal rule matches before the upstream name is
// known. Omitting the field does not disable grok2api's hold.
func preserveGrokExplicitNoneReasoningEffort(ctx context.Context, upstreamModel string, body []byte) []byte {
	if len(body) == 0 || !isGrokFamilyModel(upstreamModel) || !inboundReasoningDisabledFromContext(ctx) {
		return body
	}
	updated, err := sjson.SetBytes(body, "reasoning_effort", "none")
	if err != nil {
		return body
	}
	return updated
}

// ShouldSameAccountRetryGrokQualityDegraded is the one extra try grok2api needs
// to rotate its own account after a quality hold. Other 5xx stay on the normal
// switch-account path. Pool mode is not consulted: a zero pool retry limit must
// not swallow this single retry.
func ShouldSameAccountRetryGrokQualityDegraded(account *Account, requestedModel, messagesDispatchMappedModel string, failoverErr *UpstreamFailoverError) bool {
	if account == nil || account.Platform == PlatformGrok || !account.IsOpenAIApiKey() || failoverErr == nil {
		return false
	}
	if !isGrokFamilyModel(grokAPIKeyUpstreamModel(account, requestedModel, messagesDispatchMappedModel)) {
		return false
	}
	return upstreamErrorCodeIs(failoverErr.ResponseBody, grokQualityDegradedCode)
}

func upstreamErrorCodeIs(body []byte, want string) bool {
	if len(body) == 0 || strings.TrimSpace(want) == "" {
		return false
	}
	for _, path := range []string{"error.code", "response.error.code", "detail.code", "code"} {
		if strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, path).String()), want) {
			return true
		}
	}
	return false
}
