package service

import (
	"context"

	"miaodi-agent/internal/model"
	"miaodi-agent/pkg/openai"
)

// LLMClient 是 Agent 需要的 ChatCompletion 客户端接口
type LLMClient interface {
	CreateChatCompletion(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error)
}

// UserStore 是 Agent 需要的用户状态存储接口。
type UserStore interface {
	GetOrCreate(channelUserID string) (*model.User, error)
}

// ConversationStore 是 Agent 需要的会话历史存储接口。
type ConversationStore interface {
	GetMessages(channelUserID string, conversationID int64) ([]openai.ChatMessage, error)
	AppendMessage(channelUserID string, conversationID int64, msg openai.ChatMessage) error
	AppendMessages(channelUserID string, conversationID int64, msgs ...openai.ChatMessage) error
	Clear(channelUserID string, conversationID int64) error
}

// ToolRunner 是 Agent 调用业务工具的接口。
type ToolRunner interface {
	Execute(user *model.User, channelUserID string, conversationID int64, name, arguments string) string
}

// MiaodiClient 是工具执行器需要的喵滴客户端接口
type MiaodiClient interface {
	Check(key string) bool
	GetInfo(key string) (map[string]interface{}, error)
	PutText(key, book, chapter, title, content string) (map[string]interface{}, error)
}
