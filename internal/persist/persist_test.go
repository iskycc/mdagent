package persist

import (
	"context"
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
	q := NewPersistQueue(convRepo, callLogRepo, 10)

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
	q.EnqueueConv(ctx, "u1", 1, []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()},
	})

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
	q := NewPersistQueue(convRepo, callLogRepo, 10)

	mock.ExpectExec("INSERT INTO api_call_log").
		WithArgs("u1", "k1", "miaodi", "put_text", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	q.Run(ctx)
	q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")

	time.Sleep(200 * time.Millisecond)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
