package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
)

const ttl = 24 * time.Hour

// RedisCache 是基于 go-redis 的 Cache 实现。
type RedisCache struct {
	client  *redis.Client
	enabled bool
}

// NewRedisCache 创建缓存；enabled=false 时所有操作返回 error。
func NewRedisCache(addr, password string, db int, enabled bool) *RedisCache {
	if !enabled {
		return &RedisCache{enabled: false}
	}
	return &RedisCache{
		client:  redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db}),
		enabled: true,
	}
}

func (c *RedisCache) Available(ctx context.Context) bool {
	if !c.enabled || c.client == nil {
		return false
	}
	return c.client.Ping(ctx).Err() == nil
}

func (c *RedisCache) GetUser(ctx context.Context, channelUserID string) (*model.User, error) {
	if !c.enabled {
		return nil, fmt.Errorf("redis disabled")
	}
	raw, err := c.client.Get(ctx, userKey(channelUserID)).Bytes()
	if err != nil {
		return nil, err
	}
	var user model.User
	if err := json.Unmarshal(raw, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *RedisCache) SetUser(ctx context.Context, user *model.User) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	raw, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, userKey(user.ChannelUserID), raw, ttl).Err()
}

func (c *RedisCache) GetMessages(ctx context.Context, channelUserID string, conversationID int64) ([]repository.StoredChatMessage, error) {
	if !c.enabled {
		return nil, fmt.Errorf("redis disabled")
	}
	raw, err := c.client.Get(ctx, convKey(channelUserID, conversationID)).Bytes()
	if err != nil {
		return nil, err
	}
	var msgs []repository.StoredChatMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (c *RedisCache) SetMessages(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	raw, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, convKey(channelUserID, conversationID), raw, ttl).Err()
}

func (c *RedisCache) AppendMessages(ctx context.Context, channelUserID string, conversationID int64, msgs ...repository.StoredChatMessage) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	existing, err := c.GetMessages(ctx, channelUserID, conversationID)
	if err != nil && err != redis.Nil {
		return err
	}
	combined := append(existing, msgs...)
	return c.SetMessages(ctx, channelUserID, conversationID, combined)
}

func (c *RedisCache) ClearConversation(ctx context.Context, channelUserID string, conversationID int64) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	return c.client.Del(ctx, convKey(channelUserID, conversationID)).Err()
}

func (c *RedisCache) GetRecentLogs(ctx context.Context, channelUserID string) ([]repository.UserCallLog, error) {
	if !c.enabled {
		return nil, fmt.Errorf("redis disabled")
	}
	raw, err := c.client.Get(ctx, logsKey(channelUserID)).Bytes()
	if err != nil {
		return nil, err
	}
	var logs []repository.UserCallLog
	if err := json.Unmarshal(raw, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (c *RedisCache) SetRecentLogs(ctx context.Context, channelUserID string, logs []repository.UserCallLog) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	raw, err := json.Marshal(logs)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, logsKey(channelUserID), raw, ttl).Err()
}

func (c *RedisCache) AppendLog(ctx context.Context, channelUserID string, log repository.UserCallLog) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	logs, err := c.GetRecentLogs(ctx, channelUserID)
	if err != nil && err != redis.Nil {
		return err
	}
	logs = append([]repository.UserCallLog{log}, logs...)
	if len(logs) > 20 {
		logs = logs[:20]
	}
	return c.SetRecentLogs(ctx, channelUserID, logs)
}

func userKey(channelUserID string) string {
	return fmt.Sprintf("md:user:%s", channelUserID)
}

func convKey(channelUserID string, conversationID int64) string {
	return fmt.Sprintf("md:conv:%s:%d", channelUserID, conversationID)
}

func logsKey(channelUserID string) string {
	return fmt.Sprintf("md:logs:%s", channelUserID)
}
