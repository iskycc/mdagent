package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/cache"
	"miaodi-agent/internal/config"
	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
	"miaodi-agent/pkg/openai"
)

func TestRun_StartAndShutdown(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users ADD COLUMN email").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users MODIFY COLUMN status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE agent_users").WithArgs(repository.DefaultBook, repository.DefaultChara).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_conversations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS pending_images").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS api_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS llm_call_log").WillReturnResult(sqlmock.NewResult(0, 0))

	cfg := &config.Config{
		Port:          "0",
		CallbackPath:  "/callback",
		OpenAIAPIKey:  "test",
		OpenAIBaseURL: "http://localhost",
		OpenAIModel:   "test-model",
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, db, cfg)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return in time")
	}
}

func TestRun_ReturnsListenError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users ADD COLUMN email").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users MODIFY COLUMN status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE agent_users").WithArgs(repository.DefaultBook, repository.DefaultChara).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_conversations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS pending_images").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS api_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS llm_call_log").WillReturnResult(sqlmock.NewResult(0, 0))

	cfg := &config.Config{
		Port:          "bad port",
		CallbackPath:  "/callback",
		OpenAIAPIKey:  "test",
		OpenAIBaseURL: "http://localhost",
		OpenAIModel:   "test-model",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := Run(ctx, db, cfg); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestInitRepos_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnError(sqlmock.ErrCancelled)

	if err := initRepos(db); err == nil {
		t.Fatal("expected error")
	}
}

// fakeCache is a test double for cache.Cache that records calls and can be configured
// to report availability or return errors.
type fakeCache struct {
	available       bool
	setUserErr      error
	setMessagesErr  error
	users           map[string]*model.User
	messages        map[cacheMessageKey][]repository.StoredChatMessage
	availableCalled int
}

type cacheMessageKey struct {
	channelUserID  string
	conversationID int64
}

func (f *fakeCache) Available(context.Context) bool {
	f.availableCalled++
	return f.available
}

func (f *fakeCache) GetUser(context.Context, string) (*model.User, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCache) SetUser(_ context.Context, user *model.User) error {
	if f.setUserErr != nil {
		return f.setUserErr
	}
	if f.users == nil {
		f.users = make(map[string]*model.User)
	}
	f.users[user.ChannelUserID] = user
	return nil
}

func (f *fakeCache) GetMessages(context.Context, string, int64) ([]repository.StoredChatMessage, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCache) SetMessages(_ context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) error {
	if f.setMessagesErr != nil {
		return f.setMessagesErr
	}
	if f.messages == nil {
		f.messages = make(map[cacheMessageKey][]repository.StoredChatMessage)
	}
	f.messages[cacheMessageKey{channelUserID: channelUserID, conversationID: conversationID}] = msgs
	return nil
}

func (f *fakeCache) AppendMessages(context.Context, string, int64, ...repository.StoredChatMessage) error {
	return errors.New("not implemented")
}

func (f *fakeCache) ClearConversation(context.Context, string, int64) error {
	return errors.New("not implemented")
}

func (f *fakeCache) GetRecentLogs(context.Context, string) ([]repository.UserCallLog, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCache) SetRecentLogs(context.Context, string, []repository.UserCallLog) error {
	return errors.New("not implemented")
}

func (f *fakeCache) AppendLog(context.Context, string, repository.UserCallLog) error {
	return errors.New("not implemented")
}

// Ensure fakeCache implements cache.Cache at compile time.
var _ cache.Cache = (*fakeCache)(nil)

type fakeConversationRepo struct {
	conversations []repository.ConversationWithMessages
	err           error
	called        bool
}

func (f *fakeConversationRepo) ListActiveSince(cutoff time.Time) ([]repository.ConversationWithMessages, error) {
	f.called = true
	return f.conversations, f.err
}

type fakeUserRepo struct {
	users  map[string]*model.User
	errs   map[string]error
	called int
}

func (f *fakeUserRepo) Get(channelUserID string) (*model.User, error) {
	f.called++
	if err, ok := f.errs[channelUserID]; ok {
		return nil, err
	}
	return f.users[channelUserID], nil
}

func TestSeedCache_Success(t *testing.T) {
	ctx := context.Background()
	c := &fakeCache{available: true}
	convRepo := &fakeConversationRepo{
		conversations: []repository.ConversationWithMessages{
			{
				ChannelUserID:  "u1",
				ConversationID: 1,
				Messages: []repository.StoredChatMessage{
					{ChatMessage: openaiMsg("user", "hello")},
				},
			},
			{
				ChannelUserID:  "u2",
				ConversationID: 2,
				Messages: []repository.StoredChatMessage{
					{ChatMessage: openaiMsg("assistant", "hi")},
				},
			},
		},
	}
	userRepo := &fakeUserRepo{
		users: map[string]*model.User{
			"u1": {ChannelUserID: "u1", Status: "bound"},
			"u2": {ChannelUserID: "u2", Status: "unbound"},
		},
	}

	if err := seedCache(ctx, c, convRepo, userRepo); err != nil {
		t.Fatalf("seedCache failed: %v", err)
	}

	if c.availableCalled != 1 {
		t.Fatalf("expected Available called once, got %d", c.availableCalled)
	}
	if !convRepo.called {
		t.Fatal("expected ListActiveSince called")
	}
	if userRepo.called != 2 {
		t.Fatalf("expected Get called twice, got %d", userRepo.called)
	}
	if len(c.users) != 2 {
		t.Fatalf("expected 2 users cached, got %d", len(c.users))
	}
	if len(c.messages) != 2 {
		t.Fatalf("expected 2 conversations cached, got %d", len(c.messages))
	}
	if c.users["u1"].Status != "bound" {
		t.Fatalf("unexpected user u1: %+v", c.users["u1"])
	}
	if len(c.messages[cacheMessageKey{"u1", 1}]) != 1 {
		t.Fatalf("unexpected messages for u1/1: %+v", c.messages[cacheMessageKey{"u1", 1}])
	}
}

func TestSeedCache_RedisUnavailable(t *testing.T) {
	ctx := context.Background()
	c := &fakeCache{available: false}
	convRepo := &fakeConversationRepo{}
	userRepo := &fakeUserRepo{}

	err := seedCache(ctx, c, convRepo, userRepo)
	if err == nil {
		t.Fatal("expected error when redis unavailable")
	}
	if convRepo.called {
		t.Fatal("expected ListActiveSince not called when redis unavailable")
	}
}

func TestSeedCache_ListActiveSinceError(t *testing.T) {
	ctx := context.Background()
	c := &fakeCache{available: true}
	convRepo := &fakeConversationRepo{err: errors.New("db down")}
	userRepo := &fakeUserRepo{}

	err := seedCache(ctx, c, convRepo, userRepo)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSeedCache_GetUserError(t *testing.T) {
	ctx := context.Background()
	c := &fakeCache{available: true}
	convRepo := &fakeConversationRepo{
		conversations: []repository.ConversationWithMessages{
			{ChannelUserID: "u1", ConversationID: 1, Messages: []repository.StoredChatMessage{{ChatMessage: openaiMsg("user", "hi")}}},
		},
	}
	userRepo := &fakeUserRepo{
		users: map[string]*model.User{},
		errs:  map[string]error{"u1": errors.New("not found")},
	}

	if err := seedCache(ctx, c, convRepo, userRepo); err != nil {
		t.Fatalf("seedCache should continue on user get error: %v", err)
	}
	if userRepo.called != 1 {
		t.Fatalf("expected Get called once, got %d", userRepo.called)
	}
	// Messages should still be cached even if user lookup failed.
	if len(c.messages) != 1 {
		t.Fatalf("expected messages cached despite user error, got %d", len(c.messages))
	}
}

func TestSeedCache_SetUserError(t *testing.T) {
	ctx := context.Background()
	c := &fakeCache{available: true, setUserErr: errors.New("redis busy")}
	convRepo := &fakeConversationRepo{
		conversations: []repository.ConversationWithMessages{
			{ChannelUserID: "u1", ConversationID: 1, Messages: []repository.StoredChatMessage{{ChatMessage: openaiMsg("user", "hi")}}},
		},
	}
	userRepo := &fakeUserRepo{users: map[string]*model.User{"u1": {ChannelUserID: "u1"}}}

	if err := seedCache(ctx, c, convRepo, userRepo); err != nil {
		t.Fatalf("seedCache should continue on set user error: %v", err)
	}
	if len(c.messages) != 1 {
		t.Fatalf("expected messages cached despite set user error, got %d", len(c.messages))
	}
}

func TestSeedCache_SetMessagesError(t *testing.T) {
	ctx := context.Background()
	c := &fakeCache{available: true, setMessagesErr: errors.New("redis busy")}
	convRepo := &fakeConversationRepo{
		conversations: []repository.ConversationWithMessages{
			{ChannelUserID: "u1", ConversationID: 1, Messages: []repository.StoredChatMessage{{ChatMessage: openaiMsg("user", "hi")}}},
		},
	}
	userRepo := &fakeUserRepo{users: map[string]*model.User{"u1": {ChannelUserID: "u1"}}}

	if err := seedCache(ctx, c, convRepo, userRepo); err != nil {
		t.Fatalf("seedCache should continue on set messages error: %v", err)
	}
	if len(c.users) != 1 {
		t.Fatalf("expected users cached despite set messages error, got %d", len(c.users))
	}
}

type fakeConversationCleaner struct {
	mu      sync.Mutex
	removed int
	err     error
	called  int
}

func (f *fakeConversationCleaner) CleanupExpiredMessages(cutoff time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called++
	return f.removed, f.err
}

func (f *fakeConversationCleaner) setResult(removed int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = removed
	f.err = err
}

func (f *fakeConversationCleaner) calledCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

func TestStartConversationCleanup_CtxDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cleaner := &fakeConversationCleaner{}
	startConversationCleanup(ctx, cleaner, time.Hour)

	cancel()
	time.Sleep(50 * time.Millisecond)

	if cleaner.called != 0 {
		t.Fatalf("expected cleanup not called, got %d", cleaner.called)
	}
}

func TestStartConversationCleanup_ErrorAndRemoved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleaner := &fakeConversationCleaner{err: errors.New("cleanup failed")}
	startConversationCleanup(ctx, cleaner, 10*time.Millisecond)

	time.Sleep(40 * time.Millisecond)
	if cleaner.calledCount() == 0 {
		t.Fatal("expected cleanup called")
	}

	// Switch to success with removed count to cover removed>0 path.
	cleaner.setResult(3, nil)
	time.Sleep(40 * time.Millisecond)
	if cleaner.calledCount() < 2 {
		t.Fatalf("expected cleanup called multiple times, got %d", cleaner.calledCount())
	}
}

type fakeCallLogCleaner struct {
	mu      sync.Mutex
	deleted int64
	err     error
	called  int
}

func (f *fakeCallLogCleaner) CleanupOlderThan(days int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called++
	return f.deleted, f.err
}

func (f *fakeCallLogCleaner) setResult(deleted int64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = deleted
	f.err = err
}

func (f *fakeCallLogCleaner) calledCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

func TestStartCallLogCleanup_CtxDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cleaner := &fakeCallLogCleaner{}
	startCallLogCleanup(ctx, cleaner, time.Hour)

	cancel()
	time.Sleep(50 * time.Millisecond)

	if cleaner.calledCount() != 0 {
		t.Fatalf("expected cleanup not called, got %d", cleaner.calledCount())
	}
}

func TestStartCallLogCleanup_ErrorAndDeleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleaner := &fakeCallLogCleaner{err: errors.New("cleanup failed")}
	startCallLogCleanup(ctx, cleaner, 10*time.Millisecond)

	time.Sleep(40 * time.Millisecond)
	if cleaner.calledCount() == 0 {
		t.Fatal("expected cleanup called")
	}

	cleaner.setResult(5, nil)
	time.Sleep(40 * time.Millisecond)
	if cleaner.calledCount() < 2 {
		t.Fatalf("expected cleanup called multiple times, got %d", cleaner.calledCount())
	}
}

func TestStartLLMCallLogCleanup_CtxDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cleaner := &fakeCallLogCleaner{}
	startLLMCallLogCleanup(ctx, cleaner, time.Hour)

	cancel()
	time.Sleep(50 * time.Millisecond)

	if cleaner.calledCount() != 0 {
		t.Fatalf("expected cleanup not called, got %d", cleaner.calledCount())
	}
}

func TestStartLLMCallLogCleanup_ErrorAndDeleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cleaner := &fakeCallLogCleaner{err: errors.New("cleanup failed")}
	startLLMCallLogCleanup(ctx, cleaner, 10*time.Millisecond)

	time.Sleep(40 * time.Millisecond)
	if cleaner.calledCount() == 0 {
		t.Fatal("expected cleanup called")
	}

	cleaner.setResult(5, nil)
	time.Sleep(40 * time.Millisecond)
	if cleaner.calledCount() < 2 {
		t.Fatalf("expected cleanup called multiple times, got %d", cleaner.calledCount())
	}
}

func openaiMsg(role, content string) openai.ChatMessage {
	return openai.ChatMessage{Role: role, Content: content}
}
