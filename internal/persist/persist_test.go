package persist

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/repository"
	"miaodi-agent/pkg/openai"
)

func TestPersistQueue_ConvTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	convRepo := repository.NewConversationRepo(db)
	callLogRepo := repository.NewCallLogRepo(db)
	q := NewPersistQueue(convRepo, callLogRepo, nil, 10)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT messages, updated_at FROM agent_conversations").
		WithArgs("u1", int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"messages", "updated_at"}).AddRow("[]", time.Now()))
	mock.ExpectExec("INSERT INTO agent_conversations").
		WithArgs("u1", int64(1), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ctx := context.Background()
	q.Run(ctx)
	if !q.EnqueueConv(ctx, "u1", 1, []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()},
	}) {
		t.Fatal("expected enqueue success")
	}

	time.Sleep(200 * time.Millisecond)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPersistQueue_LogTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	convRepo := repository.NewConversationRepo(db)
	callLogRepo := repository.NewCallLogRepo(db)
	q := NewPersistQueue(convRepo, callLogRepo, nil, 10)

	mock.ExpectExec("INSERT INTO api_call_log").
		WithArgs("u1", sqlmock.AnyArg(), "miaodi", "put_text", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	q.Run(ctx)
	if !q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text") {
		t.Fatal("expected enqueue success")
	}

	time.Sleep(200 * time.Millisecond)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestNewPersistQueue_DefaultBuffer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	convRepo := repository.NewConversationRepo(db)
	callLogRepo := repository.NewCallLogRepo(db)
	// bufferSize <= 0 should default to 1024.
	q := NewPersistQueue(convRepo, callLogRepo, nil, 0)

	mock.ExpectExec("INSERT INTO api_call_log").
		WithArgs("u1", sqlmock.AnyArg(), "miaodi", "put_text", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPersistQueue_Enqueue_ReturnsFalseWhenFull(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 1)
	ctx := context.Background()
	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	if q.EnqueueLog(ctx, "u2", "k2", "miaodi", "put_text") {
		t.Fatal("expected enqueue to fail when buffer full")
	}
}

func TestPersistQueue_Enqueue_ContextCancelled(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 1)
	q.EnqueueLog(context.Background(), "u1", "k1", "miaodi", "put_text")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		q.EnqueueConv(ctx, "u1", 1, nil)
		q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue did not return on cancelled context")
	}

	if len(q.tasks) != 1 {
		t.Fatalf("expected 1 task in buffer, got %d", len(q.tasks))
	}
}

func TestPersistQueue_Flush_Empty(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 10)
	if err := q.Flush(context.Background()); err != nil {
		t.Fatalf("flush empty queue failed: %v", err)
	}
}

func TestPersistQueue_Flush_LogTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 10)

	mock.ExpectExec("INSERT INTO api_call_log").
		WithArgs("u1", sqlmock.AnyArg(), "miaodi", "put_text", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPersistQueue_Flush_ConvTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 10)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT messages, updated_at FROM agent_conversations").
		WithArgs("u1", int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"messages", "updated_at"}).AddRow("[]", time.Now()))
	mock.ExpectExec("INSERT INTO agent_conversations").
		WithArgs("u1", int64(1), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ctx := context.Background()
	q.EnqueueConv(ctx, "u1", 1, []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()},
	})
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPersistQueue_Flush_ContextCancelled(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 10)

	ctx, cancel := context.WithCancel(context.Background())
	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	cancel()

	err = q.Flush(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestPersistQueue_LogTask_RetryExhausted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 10)

	// process retries maxRetries (3) times before giving up.
	for i := 0; i < 3; i++ {
		mock.ExpectExec("INSERT INTO api_call_log").
			WithArgs("u1", sqlmock.AnyArg(), "miaodi", "put_text", sqlmock.AnyArg()).
			WillReturnError(errors.New("db down"))
	}

	ctx := context.Background()
	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPersistQueue_ConvTask_RetryExhausted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 10)

	// Make Begin fail with a non-retryable (non-MySQL 1062) error. AppendMessages
	// returns immediately, and process retries it maxRetries (3) times.
	for i := 0; i < 3; i++ {
		mock.ExpectBegin().WillReturnError(errors.New("tx failed"))
	}

	ctx := context.Background()
	q.EnqueueConv(ctx, "u1", 1, []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()},
	})
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPersistQueue_LogTask_DeadLetter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	callLogRepo := repository.NewCallLogRepo(db)
	deadLetterRepo := repository.NewDeadLetterRepo(db)
	q := NewPersistQueue(repository.NewConversationRepo(db), callLogRepo, deadLetterRepo, 10)

	for i := 0; i < 3; i++ {
		mock.ExpectExec("INSERT INTO api_call_log").
			WithArgs("u1", sqlmock.AnyArg(), "miaodi", "put_text", sqlmock.AnyArg()).
			WillReturnError(errors.New("db down"))
	}
	mock.ExpectExec("INSERT INTO persist_dead_letters").
		WithArgs("log", sqlmock.AnyArg(), "db down", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPersistQueue_Flush_WaitsForInFlight(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	q := NewPersistQueue(repository.NewConversationRepo(db), repository.NewCallLogRepo(db), nil, 10)
	ctx := context.Background()
	q.Run(ctx)

	mock.ExpectExec("INSERT INTO api_call_log").
		WithArgs("u1", sqlmock.AnyArg(), "miaodi", "put_text", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestStoredToChatMessages(t *testing.T) {
	stored := []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hello"}},
		{ChatMessage: openai.ChatMessage{Role: "assistant", Content: "world"}},
	}
	msgs := storedToChatMessages(stored)
	if len(msgs) != len(stored) {
		t.Fatalf("expected %d messages, got %d", len(stored), len(msgs))
	}
	for i, m := range msgs {
		if m.Role != stored[i].Role || m.Content != stored[i].Content {
			t.Fatalf("message mismatch at %d: %+v vs %+v", i, m, stored[i].ChatMessage)
		}
	}
}
