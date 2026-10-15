package service

import (
	"errors"
	"testing"
)

func TestShouldRecordOpenAIUsage(t *testing.T) {
	result := &OpenAIForwardResult{}
	upstreamErr := errors.New("stream usage incomplete")

	if ShouldRecordOpenAIUsage(nil, nil) {
		t.Fatal("nil result must not be billed")
	}
	if ShouldRecordOpenAIUsage(nil, upstreamErr) {
		t.Fatal("upstream error without a result must not be billed")
	}
	if !ShouldRecordOpenAIUsage(result, nil) {
		t.Fatal("successful forward must be billed")
	}
	if ShouldRecordOpenAIUsage(result, upstreamErr) {
		t.Fatal("upstream error must not be billed")
	}
	result.ClientDisconnect = true
	if !ShouldRecordOpenAIUsage(result, upstreamErr) {
		t.Fatal("client disconnect must still be billed")
	}
}
