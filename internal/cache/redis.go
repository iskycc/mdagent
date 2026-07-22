package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"miaodi-agent/internal/metrics"
	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
)

const ttl = 24 * time.Hour

// jsonMarshal / jsonUnmarshal 允许测试时替换，以覆盖序列化/反序列化错误分支。
var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal
)

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
	return NewRedisCacheWithOptions(&redis.Options{Addr: addr, Password: password, DB: db})
}

// NewRedisCacheWithOptions 使用自定义 redis.Options 创建缓存，主要用于测试。
func NewRedisCacheWithOptions(opts *redis.Options) *RedisCache {
	return &RedisCache{client: redis.NewClient(opts), enabled: true}
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
	if err := jsonUnmarshal(raw, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *RedisCache) SetUser(ctx context.Context, user *model.User) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	raw, err := jsonMarshal(user)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, userKey(user.ChannelUserID), raw, ttl).Err()
}

func (c *RedisCache) GetMessages(ctx context.Context, channelUserID string, conversationID int64) ([]repository.StoredChatMessage, error) {
	if !c.enabled {
		return nil, fmt.Errorf("redis disabled")
	}
	key := convKey(channelUserID, conversationID)
	exists, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, redis.Nil
	}
	elems, err := c.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	msgs := make([]repository.StoredChatMessage, 0, len(elems))
	for _, elem := range elems {
		var msg repository.StoredChatMessage
		if err := jsonUnmarshal([]byte(elem), &msg); err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

func (c *RedisCache) SetMessages(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	key := convKey(channelUserID, conversationID)
	pipe := c.client.Pipeline()
	pipe.Del(ctx, key)
	if len(msgs) > 0 {
		elems := make([]interface{}, 0, len(msgs))
		for _, msg := range msgs {
			raw, err := jsonMarshal(msg)
			if err != nil {
				return err
			}
			elems = append(elems, raw)
		}
		pipe.RPush(ctx, key, elems...)
		pipe.Expire(ctx, key, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RedisCache) AppendMessages(ctx context.Context, channelUserID string, conversationID int64, msgs ...repository.StoredChatMessage) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	if len(msgs) == 0 {
		return nil
	}
	key := convKey(channelUserID, conversationID)
	elems := make([]interface{}, 0, len(msgs))
	for _, msg := range msgs {
		raw, err := jsonMarshal(msg)
		if err != nil {
			return err
		}
		elems = append(elems, raw)
	}
	pipe := c.client.Pipeline()
	pipe.RPush(ctx, key, elems...)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
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
	key := logsKey(channelUserID)
	exists, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, redis.Nil
	}
	elems, err := c.client.LRange(ctx, key, 0, 19).Result()
	if err != nil {
		return nil, err
	}
	logs := make([]repository.UserCallLog, 0, len(elems))
	for _, elem := range elems {
		var log repository.UserCallLog
		if err := jsonUnmarshal([]byte(elem), &log); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

func (c *RedisCache) SetRecentLogs(ctx context.Context, channelUserID string, logs []repository.UserCallLog) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	key := logsKey(channelUserID)
	pipe := c.client.Pipeline()
	pipe.Del(ctx, key)
	if len(logs) > 0 {
		// logs[0] 是最新的，所以从末尾开始 LPUSH，保证列表顺序与入参一致。
		elems := make([]interface{}, 0, len(logs))
		for i := len(logs) - 1; i >= 0; i-- {
			raw, err := jsonMarshal(logs[i])
			if err != nil {
				return err
			}
			elems = append(elems, raw)
		}
		pipe.LPush(ctx, key, elems...)
		pipe.Expire(ctx, key, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RedisCache) AppendLog(ctx context.Context, channelUserID string, log repository.UserCallLog) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	raw, err := jsonMarshal(log)
	if err != nil {
		return err
	}
	key := logsKey(channelUserID)
	pipe := c.client.Pipeline()
	pipe.LPush(ctx, key, raw)
	pipe.LTrim(ctx, key, 0, 19)
	pipe.Expire(ctx, key, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (c *RedisCache) SetMetricsSnapshot(ctx context.Context, snapshots []metrics.MetricSnapshot) error {
	if !c.enabled {
		return fmt.Errorf("redis disabled")
	}
	raw, err := jsonMarshal(snapshots)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, metricsSnapshotKey(), raw, 5*time.Minute).Err()
}

func (c *RedisCache) GetMetricsSnapshot(ctx context.Context) ([]metrics.MetricSnapshot, error) {
	if !c.enabled {
		return nil, fmt.Errorf("redis disabled")
	}
	raw, err := c.client.Get(ctx, metricsSnapshotKey()).Bytes()
	if err != nil {
		return nil, err
	}
	var snapshots []metrics.MetricSnapshot
	if err := jsonUnmarshal(raw, &snapshots); err != nil {
		return nil, err
	}
	return snapshots, nil
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

func metricsSnapshotKey() string {
	return "md:metrics:snapshot"
}
