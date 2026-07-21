package service

import (
	"encoding/json"
	"strings"
	"sync"
	"unicode/utf8"

	tiktoken "github.com/pkoukk/tiktoken-go"

	_ "miaodi-agent/internal/service/tiktokenbpe"
	"miaodi-agent/pkg/openai"
)

const tokenSafetyMargin = 128

var (
	tokenEncoderMu sync.Mutex
	tokenEncoders  = map[string]tokenEncoding{}
)

// FitMessagesForTokenBudget 裁剪历史消息，避免请求超过模型上下文窗口。
// 不限制历史消息条数；如果 token 没超过预算，会完整保留 24 小时内历史。
func FitMessagesForTokenBudget(messages []openai.ChatMessage, tools []openai.ToolDefinition, modelMaxTokens, maxOutputTokens int) []openai.ChatMessage {
	return FitMessagesForTokenBudgetForModel("", messages, tools, modelMaxTokens, maxOutputTokens)
}

// FitMessagesForTokenBudgetForModel 使用指定模型对应的 tokenizer 计算并裁剪消息。
func FitMessagesForTokenBudgetForModel(model string, messages []openai.ChatMessage, tools []openai.ToolDefinition, modelMaxTokens, maxOutputTokens int) []openai.ChatMessage {
	if len(messages) == 0 {
		return messages
	}
	if modelMaxTokens <= 0 {
		modelMaxTokens = defaultModelMaxTokens
	}
	if maxOutputTokens <= 0 {
		maxOutputTokens = defaultMaxOutputTokens
	}

	tokenizer := newTokenCounter(model)
	inputBudget := modelMaxTokens - maxOutputTokens - tokenizer.ToolsTokens(tools) - tokenSafetyMargin
	if inputBudget <= 0 {
		inputBudget = modelMaxTokens / 2
	}
	if inputBudget <= 0 {
		return []openai.ChatMessage{truncateMessage(messages[0], 128)}
	}

	if tokenizer.MessagesTokens(messages) <= inputBudget {
		return messages
	}

	systemMsg := messages[0]
	systemTokens := tokenizer.MessageTokens(systemMsg)
	remaining := inputBudget - systemTokens
	if remaining <= 0 {
		return []openai.ChatMessage{truncateMessage(systemMsg, inputBudget)}
	}

	blocks := buildMessageBlocks(messages[1:])
	kept := make([][]openai.ChatMessage, 0, len(blocks))
	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		blockTokens := tokenizer.MessagesTokens(block)
		if blockTokens <= remaining {
			kept = append(kept, block)
			remaining -= blockTokens
			continue
		}
		if len(kept) == 0 {
			kept = append(kept, truncateBlockForModel(model, block, remaining))
		}
		break
	}

	result := []openai.ChatMessage{systemMsg}
	for i := len(kept) - 1; i >= 0; i-- {
		result = append(result, kept[i]...)
	}
	return result
}

func buildMessageBlocks(messages []openai.ChatMessage) [][]openai.ChatMessage {
	blocks := make([][]openai.ChatMessage, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			block := []openai.ChatMessage{msg}
			for i+1 < len(messages) && messages[i+1].Role == "tool" {
				i++
				block = append(block, messages[i])
			}
			blocks = append(blocks, block)
			continue
		}
		blocks = append(blocks, []openai.ChatMessage{msg})
	}
	return blocks
}

func truncateBlock(block []openai.ChatMessage, tokenBudget int) []openai.ChatMessage {
	return truncateBlockForModel("", block, tokenBudget)
}

func truncateBlockForModel(model string, block []openai.ChatMessage, tokenBudget int) []openai.ChatMessage {
	if tokenBudget <= 0 || len(block) == 0 {
		return nil
	}
	if len(block) == 1 {
		return []openai.ChatMessage{truncateMessageForModel(model, block[0], tokenBudget)}
	}

	tokenizer := newTokenCounter(model)
	result := make([]openai.ChatMessage, 0, len(block))
	for _, msg := range block {
		if msg.Content == "" {
			result = append(result, msg)
			tokenBudget -= tokenizer.MessageTokens(msg)
			continue
		}
		if tokenBudget <= 0 {
			break
		}
		truncated := truncateMessageForModel(model, msg, tokenBudget)
		result = append(result, truncated)
		tokenBudget -= tokenizer.MessageTokens(truncated)
	}
	return result
}

func truncateMessage(msg openai.ChatMessage, tokenBudget int) openai.ChatMessage {
	return truncateMessageForModel("", msg, tokenBudget)
}

func truncateMessageForModel(model string, msg openai.ChatMessage, tokenBudget int) openai.ChatMessage {
	tokenizer := newTokenCounter(model)
	if tokenBudget <= 0 || msg.Content == "" || tokenizer.MessageTokens(msg) <= tokenBudget {
		return msg
	}
	overhead := tokenizer.MessageTokens(msg) - tokenizer.TextTokens(msg.Content)
	contentBudget := tokenBudget - overhead
	if contentBudget <= 0 {
		msg.Content = ""
		return msg
	}
	msg.Content = truncateTextToTokensForModel(model, msg.Content, contentBudget)
	return msg
}

func estimateBlockTokens(block []openai.ChatMessage) int {
	return newTokenCounter("").MessagesTokens(block)
}

func estimateMessageTokens(msg openai.ChatMessage) int {
	return newTokenCounter("").MessageTokens(msg)
}

func estimateToolsTokens(tools []openai.ToolDefinition) int {
	return newTokenCounter("").ToolsTokens(tools)
}

func estimateTextTokens(text string) int {
	return newTokenCounter("").TextTokens(text)
}

func truncateTextToTokens(text string, maxTokens int) string {
	return truncateTextToTokensForModel("", text, maxTokens)
}

func truncateTextToTokensForModel(model, text string, maxTokens int) string {
	if maxTokens <= 0 {
		return ""
	}
	suffix := "\n...（内容过长已截断）"
	tokenizer := newTokenCounter(model)
	suffixTokens := tokenizer.TextTokens(suffix)
	if maxTokens <= suffixTokens {
		return ""
	}
	limit := maxTokens - suffixTokens
	used := 0
	out := strings.Builder{}
	for _, r := range text {
		cost := tokenizer.TextTokens(string(r))
		if used+cost > limit {
			break
		}
		out.WriteRune(r)
		used += cost
	}
	if out.Len() == 0 {
		return ""
	}
	return out.String() + suffix
}

type tokenCounter struct {
	encoding *tiktoken.Tiktoken
	label    string
}

type tokenEncoding struct {
	encoding *tiktoken.Tiktoken
	label    string
}

func newTokenCounter(model string) tokenCounter {
	enc := getTokenEncoding(model)
	return tokenCounter{encoding: enc.encoding, label: enc.label}
}

func (c tokenCounter) EncodingLabel() string {
	if c.label == "" {
		return "fallback"
	}
	return c.label
}

func (c tokenCounter) MessagesTokens(messages []openai.ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += c.MessageTokens(msg)
	}
	return total
}

func (c tokenCounter) MessageTokens(msg openai.ChatMessage) int {
	total := 6 + c.TextTokens(msg.Role) + c.TextTokens(msg.Content) + c.TextTokens(msg.ToolCallID) + c.TextTokens(msg.Name)
	if len(msg.ToolCalls) > 0 {
		if b, err := json.Marshal(msg.ToolCalls); err == nil {
			total += c.TextTokens(string(b))
		}
	}
	return total
}

func (c tokenCounter) ToolsTokens(tools []openai.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	b, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return c.TextTokens(string(b))
}

func (c tokenCounter) TextTokens(text string) int {
	if text == "" {
		return 0
	}
	if c.encoding != nil {
		return len(c.encoding.Encode(text, nil, nil))
	}
	return fallbackTextTokens(text)
}

func getTokenEncoding(model string) tokenEncoding {
	requested := strings.TrimSpace(model)
	model = requested
	if model == "" {
		model = "cl100k_base"
	}

	tokenEncoderMu.Lock()
	defer tokenEncoderMu.Unlock()
	if enc, ok := tokenEncoders[model]; ok {
		return enc
	}

	var (
		enc   *tiktoken.Tiktoken
		err   error
		label string
	)
	if strings.Contains(model, "_base") {
		enc, err = tiktoken.GetEncoding(model)
		label = model
	} else {
		enc, err = tiktoken.EncodingForModel(model)
		label = model
	}
	if err != nil {
		enc, err = tiktoken.GetEncoding("cl100k_base")
		if requested == "" || requested == "cl100k_base" {
			label = "cl100k_base"
		} else {
			label = requested + " -> cl100k_base"
		}
	}
	if err != nil {
		return tokenEncoding{label: "fallback"}
	}
	resolved := tokenEncoding{encoding: enc, label: label}
	tokenEncoders[model] = resolved
	return resolved
}

func fallbackTextTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return (runes + 2) / 3
}
