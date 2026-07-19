package service

import (
	"strings"
	"testing"

	"miaodi-agent/pkg/openai"
)

func TestFitMessagesForTokenBudget_KeepsRecentMessages(t *testing.T) {
	messages := []openai.ChatMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: strings.Repeat("旧", 1000)},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "latest question"},
	}

	got := FitMessagesForTokenBudget(messages, nil, 260, 64)

	if len(got) < 2 {
		t.Fatalf("expected system and latest message, got %+v", got)
	}
	if got[0].Role != "system" {
		t.Fatalf("expected system first, got %s", got[0].Role)
	}
	if got[len(got)-1].Content != "latest question" {
		t.Fatalf("expected latest message to be kept, got %+v", got)
	}
	if estimateBlockTokens(got) >= 260 {
		t.Fatalf("expected trimmed messages below model limit, got %d", estimateBlockTokens(got))
	}
}

func TestFitMessagesForTokenBudget_TruncatesOversizedLatestMessage(t *testing.T) {
	messages := []openai.ChatMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: strings.Repeat("长", 1000)},
	}

	got := FitMessagesForTokenBudget(messages, nil, 260, 64)

	if len(got) != 2 {
		t.Fatalf("expected system and truncated user, got %+v", got)
	}
	if !strings.Contains(got[1].Content, "已截断") {
		t.Fatalf("expected truncation marker, got %q", got[1].Content)
	}
	if estimateBlockTokens(got) >= 260 {
		t.Fatalf("expected trimmed messages below model limit, got %d", estimateBlockTokens(got))
	}
}

func TestFitMessagesForTokenBudget_KeepsToolCallBlockTogether(t *testing.T) {
	messages := []openai.ChatMessage{
		{Role: "system", Content: "system"},
		{Role: "assistant", ToolCalls: []openai.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: openai.ToolCallFunction{
				Name:      "get_user_profile",
				Arguments: "{}",
			},
		}}},
		{Role: "tool", ToolCallID: "call_1", Content: "ok"},
	}

	got := FitMessagesForTokenBudget(messages, nil, 512, 64)

	if len(got) != 3 {
		t.Fatalf("expected complete tool block, got %+v", got)
	}
	if got[1].Role != "assistant" || got[2].Role != "tool" {
		t.Fatalf("tool block was split: %+v", got)
	}
}
