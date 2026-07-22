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

func TestFitMessagesForTokenBudgetForModel_DeepSeekFallback(t *testing.T) {
	messages := []openai.ChatMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "你好"},
	}

	// deepseek-v4-flash 对 tiktoken 是未知模型，应回退到 cl100k_base 且不触发网络请求。
	got := FitMessagesForTokenBudgetForModel("deepseek-v4-flash", messages, nil, 100000, 64000)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %+v", got)
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

func TestFitMessagesForTokenBudget_TruncatesToolBlock(t *testing.T) {
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
		{Role: "tool", ToolCallID: "call_1", Content: strings.Repeat("x", 1000)},
	}

	got := FitMessagesForTokenBudget(messages, nil, 120, 32)
	if len(got) < 1 {
		t.Fatalf("expected at least system message, got %+v", got)
	}
}

func TestFitMessagesForTokenBudget_Empty(t *testing.T) {
	got := FitMessagesForTokenBudget(nil, nil, 100, 10)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestFitMessagesForTokenBudget_InputBudgetZero(t *testing.T) {
	messages := []openai.ChatMessage{
		{Role: "system", Content: strings.Repeat("x", 100)},
	}
	got := FitMessagesForTokenBudgetForModel("", messages, nil, 10, 20)
	if len(got) != 1 || got[0].Content == messages[0].Content {
		t.Fatalf("expected truncated system message, got %+v", got)
	}
}

func TestTruncateBlock(t *testing.T) {
	if got := truncateBlock(nil, 10); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	block := []openai.ChatMessage{{Role: "user", Content: strings.Repeat("x", 1000)}}
	got := truncateBlock(block, 30)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %+v", got)
	}
	if !strings.Contains(got[0].Content, "已截断") {
		t.Fatalf("expected truncation marker, got %s", got[0].Content)
	}
}

func TestTruncateBlockForModel_ToolBlock(t *testing.T) {
	block := []openai.ChatMessage{
		{Role: "assistant", Content: "", ToolCalls: []openai.ToolCall{{ID: "c1", Type: "function", Function: openai.ToolCallFunction{Name: "f", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 1000)},
	}
	got := truncateBlockForModel("", block, 80)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %+v", got)
	}
}

func TestTruncateMessage(t *testing.T) {
	msg := openai.ChatMessage{Role: "user", Content: strings.Repeat("x", 1000)}
	got := truncateMessage(msg, 80)
	if !strings.Contains(got.Content, "已截断") {
		t.Fatalf("expected truncation marker, got %s", got.Content)
	}
}

func TestEstimateFunctions(t *testing.T) {
	msg := openai.ChatMessage{Role: "user", Content: "hello world"}
	if estimateMessageTokens(msg) == 0 {
		t.Error("expected non-zero message tokens")
	}
	tools := []openai.ToolDefinition{{Type: "function", Function: openai.FunctionDef{Name: "f"}}}
	if estimateToolsTokens(tools) == 0 {
		t.Error("expected non-zero tools tokens")
	}
	if estimateTextTokens("hello world") == 0 {
		t.Error("expected non-zero text tokens")
	}
}

func TestTruncateTextToTokens(t *testing.T) {
	if got := truncateTextToTokens("hello world", 0); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
	long := strings.Repeat("x", 10000)
	got := truncateTextToTokens(long, 80)
	if !strings.Contains(got, "已截断") {
		t.Fatalf("expected truncation marker, got %s", got)
	}
}

func TestTokenCounter_EncodingLabel(t *testing.T) {
	c := tokenCounter{label: ""}
	if got := c.EncodingLabel(); got != "fallback" {
		t.Fatalf("unexpected label: %s", got)
	}
}

func TestTokenCounter_NilEncoding(t *testing.T) {
	c := tokenCounter{encoding: nil}
	if got := c.TextTokens("hello"); got == 0 {
		t.Error("expected fallback tokens")
	}
	if got := c.Encode("hello"); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	if got := c.Decode([]int{1}); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
}

func TestFallbackTextTokens(t *testing.T) {
	if got := fallbackTextTokens(""); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
	if got := fallbackTextTokens("hello world"); got == 0 {
		t.Fatal("expected non-zero")
	}
}
