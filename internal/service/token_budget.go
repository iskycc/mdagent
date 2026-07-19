package service

import (
	"encoding/json"
	"unicode"

	"miaodi-agent/pkg/openai"
)

const tokenSafetyMargin = 128

// FitMessagesForTokenBudget 裁剪历史消息，避免请求超过模型上下文窗口。
func FitMessagesForTokenBudget(messages []openai.ChatMessage, tools []openai.ToolDefinition, modelMaxTokens, maxOutputTokens int) []openai.ChatMessage {
	if len(messages) == 0 {
		return messages
	}
	if modelMaxTokens <= 0 {
		modelMaxTokens = defaultModelMaxTokens
	}
	if maxOutputTokens <= 0 {
		maxOutputTokens = defaultMaxOutputTokens
	}

	inputBudget := modelMaxTokens - maxOutputTokens - estimateToolsTokens(tools) - tokenSafetyMargin
	if inputBudget <= 0 {
		inputBudget = modelMaxTokens / 2
	}
	if inputBudget <= 0 {
		return []openai.ChatMessage{truncateMessage(messages[0], 128)}
	}

	systemMsg := messages[0]
	systemTokens := estimateMessageTokens(systemMsg)
	remaining := inputBudget - systemTokens
	if remaining <= 0 {
		return []openai.ChatMessage{truncateMessage(systemMsg, inputBudget)}
	}

	blocks := buildMessageBlocks(messages[1:])
	kept := make([][]openai.ChatMessage, 0, len(blocks))
	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		blockTokens := estimateBlockTokens(block)
		if blockTokens <= remaining {
			kept = append(kept, block)
			remaining -= blockTokens
			continue
		}
		if len(kept) == 0 {
			kept = append(kept, truncateBlock(block, remaining))
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
	if tokenBudget <= 0 || len(block) == 0 {
		return nil
	}
	if len(block) == 1 {
		return []openai.ChatMessage{truncateMessage(block[0], tokenBudget)}
	}

	result := make([]openai.ChatMessage, 0, len(block))
	for _, msg := range block {
		if msg.Content == "" {
			result = append(result, msg)
			tokenBudget -= estimateMessageTokens(msg)
			continue
		}
		if tokenBudget <= 0 {
			break
		}
		truncated := truncateMessage(msg, tokenBudget)
		result = append(result, truncated)
		tokenBudget -= estimateMessageTokens(truncated)
	}
	return result
}

func truncateMessage(msg openai.ChatMessage, tokenBudget int) openai.ChatMessage {
	if tokenBudget <= 0 || msg.Content == "" || estimateMessageTokens(msg) <= tokenBudget {
		return msg
	}
	overhead := estimateMessageTokens(msg) - estimateTextTokens(msg.Content)
	contentBudget := tokenBudget - overhead
	if contentBudget <= 0 {
		msg.Content = ""
		return msg
	}
	msg.Content = truncateTextToTokens(msg.Content, contentBudget)
	return msg
}

func estimateBlockTokens(block []openai.ChatMessage) int {
	total := 0
	for _, msg := range block {
		total += estimateMessageTokens(msg)
	}
	return total
}

func estimateMessageTokens(msg openai.ChatMessage) int {
	total := 6 + estimateTextTokens(msg.Role) + estimateTextTokens(msg.Content) + estimateTextTokens(msg.ToolCallID) + estimateTextTokens(msg.Name)
	if len(msg.ToolCalls) > 0 {
		if b, err := json.Marshal(msg.ToolCalls); err == nil {
			total += estimateTextTokens(string(b))
		}
	}
	return total
}

func estimateToolsTokens(tools []openai.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	b, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return estimateTextTokens(string(b))
}

func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	ascii := 0
	tokens := 0
	for _, r := range text {
		switch {
		case r <= unicode.MaxASCII:
			if !unicode.IsSpace(r) {
				ascii++
			}
		case unicode.Is(unicode.Han, r):
			tokens++
		default:
			tokens++
		}
	}
	tokens += (ascii + 3) / 4
	if tokens == 0 {
		return 1
	}
	return tokens
}

func truncateTextToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 {
		return ""
	}
	suffix := "\n...（内容过长已截断）"
	suffixTokens := estimateTextTokens(suffix)
	if maxTokens <= suffixTokens {
		return ""
	}
	limit := maxTokens - suffixTokens
	used := 0
	out := make([]rune, 0, len(text))
	for _, r := range text {
		cost := estimateRuneTokens(r)
		if used+cost > limit {
			break
		}
		out = append(out, r)
		used += cost
	}
	if len(out) == 0 {
		return ""
	}
	return string(out) + suffix
}

func estimateRuneTokens(r rune) int {
	if r <= unicode.MaxASCII {
		if unicode.IsSpace(r) {
			return 0
		}
		return 1
	}
	return 1
}
