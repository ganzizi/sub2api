//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func grokAPIKeyAccount(mode openai_compat.ResponsesSupportMode, mapping map[string]any) *Account {
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode: string(mode),
	}
	if mapping != nil {
		account.Credentials["model_mapping"] = mapping
	}
	return account
}

func TestForwardResponses_GrokAPIKeyUsesChatEvenWhenForcedResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deep-model","input":"hello","reasoning":{"effort":"minimal"},"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := responsesOKUpstream("grok-4.6")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := grokAPIKeyAccount(openai_compat.ResponsesSupportModeForceResponses, map[string]any{
		"deep-model": "grok-4.6",
	})

	result, err := svc.Forward(WithInboundReasoningDisabled(context.Background()), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "grok-4.6", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "none", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
}

func TestForwardResponses_GrokAPIKeyKeepsMinimalWhenClientDidNotDisableReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deep-model","input":"hello","reasoning":{"effort":"minimal"},"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	upstream := responsesOKUpstream("grok-4.6")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := grokAPIKeyAccount(openai_compat.ResponsesSupportModeForceResponses, map[string]any{
		"deep-model": "grok-4.6",
	})

	_, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, "http://upstream.example/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "xhigh", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
	require.Equal(t, "auto", gjson.GetBytes(upstream.lastBody, "reasoning.summary").String())
}

func TestForwardResponses_NonGrokForceResponsesStaysOnResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_native","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}],"status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.Forward(context.Background(), c, grokAPIKeyAccount(openai_compat.ResponsesSupportModeForceResponses, nil), body)
	require.NoError(t, err)
	require.Equal(t, "http://upstream.example/v1/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
}

func TestForwardAsAnthropic_GrokAPIKeyRestoresNoneAfterGroupMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deep-model","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"},"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}],\"status\":\"completed\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := grokAPIKeyAccount(openai_compat.ResponsesSupportModeForceResponses, map[string]any{
		"deep-model": "grok-4.6",
	})
	ctx := WithInboundReasoningDisabled(WithOpenAIReasoningEffortPolicy(context.Background(), "", []ReasoningEffortMapping{{
		From: "none",
		To:   "minimal",
	}}, ""))

	_, err := svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
	require.NoError(t, err)
	require.Equal(t, "http://upstream.example/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "grok-4.6", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "none", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
}

func TestForwardAsAnthropic_NonGrokStillAppliesNoneToMinimal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"},"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))

	upstream := chatOKUpstream("gpt-5.4")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	ctx := WithInboundReasoningDisabled(WithOpenAIReasoningEffortPolicy(context.Background(), "", []ReasoningEffortMapping{{
		From: "none",
		To:   "minimal",
	}}, ""))

	_, err := svc.ForwardAsAnthropic(ctx, c, forceChatMessagesFallbackAccount(), body, "", "")
	require.NoError(t, err)
	require.Equal(t, "minimal", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
}

func TestShouldSameAccountRetryGrokQualityDegraded(t *testing.T) {
	grok := grokAPIKeyAccount(openai_compat.ResponsesSupportModeForceResponses, map[string]any{
		"deep-model": "grok-4.6",
	})
	other := grokAPIKeyAccount(openai_compat.ResponsesSupportModeForceResponses, nil)
	official := grokAPIKeyAccount(openai_compat.ResponsesSupportModeForceResponses, map[string]any{
		"grok-4.6": "grok-4.6",
	})
	official.Platform = PlatformGrok

	quality := &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, ResponseBody: []byte(`{"error":{"code":"quality_degraded","message":"upstream missing reasoning"}}`)}
	other5xx := &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, ResponseBody: []byte(`{"error":{"code":"server_error"}}`)}
	nested := &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, ResponseBody: []byte(`{"response":{"error":{"code":"quality_degraded"}}}`)}

	require.True(t, ShouldSameAccountRetryGrokQualityDegraded(grok, "deep-model", "", quality))
	require.True(t, ShouldSameAccountRetryGrokQualityDegraded(grok, "deep-model", "", nested))
	require.False(t, ShouldSameAccountRetryGrokQualityDegraded(grok, "deep-model", "", other5xx))
	require.False(t, ShouldSameAccountRetryGrokQualityDegraded(other, "gpt-5.4", "", quality))
	require.False(t, ShouldSameAccountRetryGrokQualityDegraded(official, "grok-4.6", "", quality))
	require.False(t, ShouldSameAccountRetryGrokQualityDegraded(grok, "deep-model", "", nil))
}

func TestGrokQualityDegradedRecoveredAttemptHidesRetryButNotOtherModels(t *testing.T) {
	quality := &OpsUpstreamErrorEvent{Message: "上游响应缺少推理", Detail: `{"error":{"code":"quality_degraded"}}`}
	other := &OpsUpstreamErrorEvent{Message: "upstream overloaded", Detail: `{"error":{"code":"server_error"}}`}
	require.True(t, GrokQualityDegradedRecoveredAttempt("grok-4.6", quality))
	require.True(t, GrokQualityDegradedRecoveredAttempt("xai/grok-4.6", &OpsUpstreamErrorEvent{Message: "上游响应缺少推理"}))
	require.False(t, GrokQualityDegradedRecoveredAttempt("gpt-5.4", quality))
	require.False(t, GrokQualityDegradedRecoveredAttempt("grok-4.6", other))
	require.False(t, GrokQualityDegradedRecoveredAttempt("grok-4.6", nil))
}

func chatOKUpstream(model string) *httpUpstreamRecorder {
	return &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_ok","object":"chat.completion","model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		)),
	}}
}

func responsesOKUpstream(model string) *httpUpstreamRecorder {
	return &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_ok","object":"response","model":"` + model + `","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}],"status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		)),
	}}
}
