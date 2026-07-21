package cache

import (
	"context"
	"fmt"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
)

// Cache 定义 Redis 缓存操作，所有方法遇到 Redis 不可用返回 error，由业务层降级。
type Cache interface {
	Available(ctx context.Context) bool

	GetUser(ctx context.Context, channelUserID string) (*model.User, error)
	SetUser(ctx context.Context, user *model.User) error

	GetMessages(ctx context.Context, channelUserID string, conversationID int64) ([]repository.StoredChatMessage, error)
	SetMessages(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) error
	AppendMessages(ctx context.Context, channelUserID string, conversationID int64, msgs ...repository.StoredChatMessage) error
	ClearConversation(ctx context.Context, channelUserID string, conversationID int64) error

	GetRecentLogs(ctx context.Context, channelUserID string) ([]repository.UserCallLog, error)
	SetRecentLogs(ctx context.Context, channelUserID string, logs []repository.UserCallLog) error
	AppendLog(ctx context.Context, channelUserID string, log repository.UserCallLog) error
}

// NopCache 是一个始终返回 error 的 Cache，用于测试。
// 它确保业务层在测试环境下总是回源 MySQL，保持现有测试行为不变。
type NopCache struct{}

func (NopCache) Available(context.Context) bool { return false }
func (NopCache) GetUser(context.Context, string) (*model.User, error) {
	return nil, fmt.Errorf("nop")
}
func (NopCache) SetUser(context.Context, *model.User) error { return fmt.Errorf("nop") }
func (NopCache) GetMessages(context.Context, string, int64) ([]repository.StoredChatMessage, error) {
	return nil, fmt.Errorf("nop")
}
func (NopCache) SetMessages(context.Context, string, int64, []repository.StoredChatMessage) error {
	return fmt.Errorf("nop")
}
func (NopCache) AppendMessages(context.Context, string, int64, ...repository.StoredChatMessage) error {
	return fmt.Errorf("nop")
}
func (NopCache) ClearConversation(context.Context, string, int64) error { return fmt.Errorf("nop") }
func (NopCache) GetRecentLogs(context.Context, string) ([]repository.UserCallLog, error) {
	return nil, fmt.Errorf("nop")
}
func (NopCache) SetRecentLogs(context.Context, string, []repository.UserCallLog) error {
	return fmt.Errorf("nop")
}
func (NopCache) AppendLog(context.Context, string, repository.UserCallLog) error {
	return fmt.Errorf("nop")
}
