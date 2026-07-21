package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
	"miaodi-agent/pkg/openai"
)

func TestRedisCache_UserRoundTrip(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()
	user := &model.User{ChannelUserID: "u1", APIKey: "k1", Status: "bound"}

	if err := c.SetUser(ctx, user); err != nil {
		t.Fatalf("set user: %v", err)
	}
	got, err := c.GetUser(ctx, "u1")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.APIKey != "k1" {
		t.Fatalf("unexpected api key: %s", got.APIKey)
	}
}

func TestRedisCache_MessagesAppend(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	msg := repository.StoredChatMessage{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()}
	if err := c.AppendMessages(ctx, "u1", 1, msg); err != nil {
		t.Fatalf("append: %v", err)
	}
	msgs, err := c.GetMessages(ctx, "u1", 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
}

func TestRedisCache_LogsAppend(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	c := NewRedisCache(s.Addr(), "", 0, true)
	ctx := context.Background()

	log := repository.UserCallLog{Action: "put_text", CreatedAt: time.Now()}
	if err := c.AppendLog(ctx, "u1", log); err != nil {
		t.Fatalf("append log: %v", err)
	}
	logs, err := c.GetRecentLogs(ctx, "u1")
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	if len(logs) != 1 || logs[0].Action != "put_text" {
		t.Fatalf("unexpected logs: %+v", logs)
	}
}

func TestRedisCache_Disabled(t *testing.T) {
	c := NewRedisCache("", "", 0, false)
	ctx := context.Background()
	if c.Available(ctx) {
		t.Fatal("expected not available")
	}
	if _, err := c.GetUser(ctx, "u1"); err == nil {
		t.Fatal("expected error")
	}
}
