package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
	"miaodi-agent/pkg/openai"
)

// setServerError 让 miniredis 对所有命令立即返回错误，避免网络超时等待。
func setServerError(s *miniredis.Miniredis) {
	s.SetError("broken")
}

func TestNopCache(t *testing.T) {
	ctx := context.Background()
	c := NopCache{}

	if c.Available(ctx) {
		t.Error("expected NopCache not available")
	}

	if _, err := c.GetUser(ctx, "u1"); err == nil {
		t.Error("expected GetUser error")
	}
	if err := c.SetUser(ctx, &model.User{}); err == nil {
		t.Error("expected SetUser error")
	}
	if _, err := c.GetMessages(ctx, "u1", 1); err == nil {
		t.Error("expected GetMessages error")
	}
	if err := c.SetMessages(ctx, "u1", 1, nil); err == nil {
		t.Error("expected SetMessages error")
	}
	if err := c.AppendMessages(ctx, "u1", 1); err == nil {
		t.Error("expected AppendMessages error")
	}
	if err := c.ClearConversation(ctx, "u1", 1); err == nil {
		t.Error("expected ClearConversation error")
	}
	if _, err := c.GetRecentLogs(ctx, "u1"); err == nil {
		t.Error("expected GetRecentLogs error")
	}
	if err := c.SetRecentLogs(ctx, "u1", nil); err == nil {
		t.Error("expected SetRecentLogs error")
	}
	if err := c.AppendLog(ctx, "u1", repository.UserCallLog{}); err == nil {
		t.Error("expected AppendLog error")
	}
}

func TestRedisCache_GetUser_NotFound(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	_, err := c.GetUser(ctx, "u1")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestRedisCache_GetUser_UnmarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	s.Set("md:user:u1", "not-json")

	_, err := c.GetUser(ctx, "u1")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestRedisCache_SetMessages_ClearConversation(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	msgs := []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()},
	}
	if err := c.SetMessages(ctx, "u1", 1, msgs); err != nil {
		t.Fatalf("set messages: %v", err)
	}

	got, err := c.GetMessages(ctx, "u1", 1)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(got) != 1 || got[0].Content != "hi" {
		t.Fatalf("unexpected messages: %+v", got)
	}

	if err := c.ClearConversation(ctx, "u1", 1); err != nil {
		t.Fatalf("clear conversation: %v", err)
	}

	_, err = c.GetMessages(ctx, "u1", 1)
	if err == nil {
		t.Fatal("expected error after clear")
	}
}

func TestRedisCache_GetMessages_KeyNotExists(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	_, err := c.GetMessages(ctx, "u1", 999)
	if err == nil {
		t.Fatal("expected error for missing conversation")
	}
}

func TestRedisCache_GetMessages_UnmarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	s.Lpush("md:conv:u1:1", "not-json")

	_, err := c.GetMessages(ctx, "u1", 1)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestRedisCache_SetRecentLogs(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	logs := []repository.UserCallLog{
		{Action: "put_text", CreatedAt: time.Now()},
		{Action: "save_image_pending", CreatedAt: time.Now().Add(-time.Hour)},
	}
	if err := c.SetRecentLogs(ctx, "u1", logs); err != nil {
		t.Fatalf("set recent logs: %v", err)
	}

	got, err := c.GetRecentLogs(ctx, "u1")
	if err != nil {
		t.Fatalf("get recent logs: %v", err)
	}
	if len(got) != 2 || got[0].Action != "put_text" || got[1].Action != "save_image_pending" {
		t.Fatalf("unexpected logs order: %+v", got)
	}
}

func TestRedisCache_GetRecentLogs_NotFound(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	_, err := c.GetRecentLogs(ctx, "u1")
	if err == nil {
		t.Fatal("expected error for missing logs")
	}
}

func TestRedisCache_GetRecentLogs_UnmarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	s.Lpush("md:logs:u1", "not-json")

	_, err := c.GetRecentLogs(ctx, "u1")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestRedisCache_AppendLog_Trims(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		if err := c.AppendLog(ctx, "u1", repository.UserCallLog{Action: "a", CreatedAt: time.Now()}); err != nil {
			t.Fatalf("append log: %v", err)
		}
	}

	got, err := c.GetRecentLogs(ctx, "u1")
	if err != nil {
		t.Fatalf("get recent logs: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("expected trim to 20, got %d", len(got))
	}
}

func TestRedisCache_SetMessages_Empty(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	if err := c.SetMessages(ctx, "u1", 1, nil); err != nil {
		t.Fatalf("set empty messages: %v", err)
	}

	_, err := c.GetMessages(ctx, "u1", 1)
	if err == nil {
		t.Fatal("expected error for empty conversation")
	}
}

func TestRedisCache_AppendMessages_Empty(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	if err := c.AppendMessages(ctx, "u1", 1); err != nil {
		t.Fatalf("append empty messages: %v", err)
	}
}

func TestRedisCache_TTL(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	user := &model.User{ChannelUserID: "u1", APIKey: "k1"}
	if err := c.SetUser(ctx, user); err != nil {
		t.Fatalf("set user: %v", err)
	}

	ttl := s.TTL("md:user:u1")
	if ttl < 23*time.Hour || ttl > 25*time.Hour {
		t.Fatalf("expected TTL around 24h, got %v", ttl)
	}
}

func TestRedisCache_SetMessages_TTL(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	msgs := []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()},
	}
	if err := c.SetMessages(ctx, "u1", 1, msgs); err != nil {
		t.Fatalf("set messages: %v", err)
	}

	ttl := s.TTL("md:conv:u1:1")
	if ttl < 23*time.Hour || ttl > 25*time.Hour {
		t.Fatalf("expected TTL around 24h, got %v", ttl)
	}
}

func TestRedisCache_SetUser_Disabled(t *testing.T) {
	c := &RedisCache{enabled: false}
	if err := c.SetUser(context.Background(), &model.User{}); err == nil {
		t.Error("expected error when disabled")
	}
}

func TestRedisCache_ClearConversation_Disabled(t *testing.T) {
	c := &RedisCache{enabled: false}
	if err := c.ClearConversation(context.Background(), "u1", 1); err == nil {
		t.Error("expected error when disabled")
	}
}

func TestRedisCache_SetMessages_Disabled(t *testing.T) {
	c := &RedisCache{enabled: false}
	if err := c.SetMessages(context.Background(), "u1", 1, []repository.StoredChatMessage{}); err == nil {
		t.Error("expected error when disabled")
	}
}

func TestRedisCache_AppendMessages_Disabled(t *testing.T) {
	c := &RedisCache{enabled: false}
	if err := c.AppendMessages(context.Background(), "u1", 1, repository.StoredChatMessage{}); err == nil {
		t.Error("expected error when disabled")
	}
}

func TestRedisCache_SetRecentLogs_Disabled(t *testing.T) {
	c := &RedisCache{enabled: false}
	if err := c.SetRecentLogs(context.Background(), "u1", []repository.UserCallLog{}); err == nil {
		t.Error("expected error when disabled")
	}
}

func TestRedisCache_AppendLog_Disabled(t *testing.T) {
	c := &RedisCache{enabled: false}
	if err := c.AppendLog(context.Background(), "u1", repository.UserCallLog{}); err == nil {
		t.Error("expected error when disabled")
	}
}

func TestRedisCache_Available_NilClient(t *testing.T) {
	c := &RedisCache{enabled: true, client: nil}
	if c.Available(context.Background()) {
		t.Error("expected not available with nil client")
	}
}

func TestRedisCache_GetMessages_ExistsError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	// 把 key 设成非 list 类型，让 LRange 报错
	s.Set("md:conv:u1:1", "string-value")

	_, err := c.GetMessages(ctx, "u1", 1)
	if err == nil {
		t.Fatal("expected LRange error")
	}
}

func TestRedisCache_GetRecentLogs_ExistsError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	s.Set("md:logs:u1", "string-value")

	_, err := c.GetRecentLogs(ctx, "u1")
	if err == nil {
		t.Fatal("expected LRange error")
	}
}

func TestRedisCache_SetMessages_ClientError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	ctx := context.Background()
	msgs := []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}},
	}
	if err := c.SetMessages(ctx, "u1", 1, msgs); err == nil {
		t.Fatal("expected client error")
	}
}

func TestRedisCache_AppendMessages_ClientError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	ctx := context.Background()
	if err := c.AppendMessages(ctx, "u1", 1, repository.StoredChatMessage{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected client error")
	}
}

func TestRedisCache_SetRecentLogs_ClientError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	ctx := context.Background()
	logs := []repository.UserCallLog{
		{Action: "put_text", CreatedAt: time.Now()},
	}
	if err := c.SetRecentLogs(ctx, "u1", logs); err == nil {
		t.Fatal("expected client error")
	}
}

func TestRedisCache_AppendLog_ClientError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	ctx := context.Background()
	if err := c.AppendLog(ctx, "u1", repository.UserCallLog{Action: "put_text", CreatedAt: time.Now()}); err == nil {
		t.Fatal("expected client error")
	}
}

func TestRedisCache_ClearConversation_ClientError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	ctx := context.Background()
	if err := c.ClearConversation(ctx, "u1", 1); err == nil {
		t.Fatal("expected client error")
	}
}

func TestRedisCache_SetUser_ClientError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	ctx := context.Background()
	if err := c.SetUser(ctx, &model.User{ChannelUserID: "u1"}); err == nil {
		t.Fatal("expected client error")
	}
}

func TestRedisCache_Available_PingError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	if c.Available(context.Background()) {
		t.Error("expected not available when ping fails")
	}
}

func TestRedisCache_GetMessages_ClosedServer(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	_, err := c.GetMessages(context.Background(), "u1", 1)
	if err == nil {
		t.Fatal("expected error from closed server")
	}
}

func TestRedisCache_GetRecentLogs_ClosedServer(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	_, err := c.GetRecentLogs(context.Background(), "u1")
	if err == nil {
		t.Fatal("expected error from closed server")
	}
}

func TestRedisCache_GetUser_ClosedServer(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	c := NewRedisCache(s.Addr(), "", 0, true)
	setServerError(s)

	_, err := c.GetUser(context.Background(), "u1")
	if err == nil {
		t.Fatal("expected error from closed server")
	}
}

func TestRedisCache_SetUser_MarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	old := jsonMarshal
	jsonMarshal = func(v interface{}) ([]byte, error) { return nil, fmt.Errorf("marshal error") }
	defer func() { jsonMarshal = old }()

	if err := c.SetUser(ctx, &model.User{ChannelUserID: "u1"}); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestRedisCache_SetMessages_MarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	old := jsonMarshal
	jsonMarshal = func(v interface{}) ([]byte, error) { return nil, fmt.Errorf("marshal error") }
	defer func() { jsonMarshal = old }()

	msgs := []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}},
	}
	if err := c.SetMessages(ctx, "u1", 1, msgs); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestRedisCache_AppendMessages_MarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	old := jsonMarshal
	jsonMarshal = func(v interface{}) ([]byte, error) { return nil, fmt.Errorf("marshal error") }
	defer func() { jsonMarshal = old }()

	if err := c.AppendMessages(ctx, "u1", 1, repository.StoredChatMessage{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestRedisCache_SetRecentLogs_MarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	old := jsonMarshal
	jsonMarshal = func(v interface{}) ([]byte, error) { return nil, fmt.Errorf("marshal error") }
	defer func() { jsonMarshal = old }()

	logs := []repository.UserCallLog{
		{Action: "put_text", CreatedAt: time.Now()},
	}
	if err := c.SetRecentLogs(ctx, "u1", logs); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestRedisCache_AppendLog_MarshalError(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	old := jsonMarshal
	jsonMarshal = func(v interface{}) ([]byte, error) { return nil, fmt.Errorf("marshal error") }
	defer func() { jsonMarshal = old }()

	if err := c.AppendLog(ctx, "u1", repository.UserCallLog{Action: "put_text", CreatedAt: time.Now()}); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestRedisCache_GetUser_UnmarshalErrorViaStub(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	s.Set("md:user:u1", "{}")

	old := jsonUnmarshal
	jsonUnmarshal = func(data []byte, v interface{}) error { return fmt.Errorf("unmarshal error") }
	defer func() { jsonUnmarshal = old }()

	_, err := c.GetUser(ctx, "u1")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestRedisCache_GetMessages_UnmarshalErrorViaStub(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	s.Lpush("md:conv:u1:1", "{}")

	old := jsonUnmarshal
	jsonUnmarshal = func(data []byte, v interface{}) error { return fmt.Errorf("unmarshal error") }
	defer func() { jsonUnmarshal = old }()

	_, err := c.GetMessages(ctx, "u1", 1)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestRedisCache_GetRecentLogs_UnmarshalErrorViaStub(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	s.Lpush("md:logs:u1", "{}")

	old := jsonUnmarshal
	jsonUnmarshal = func(data []byte, v interface{}) error { return fmt.Errorf("unmarshal error") }
	defer func() { jsonUnmarshal = old }()

	_, err := c.GetRecentLogs(ctx, "u1")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}
