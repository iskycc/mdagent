package repository

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"

	"miaodi-agent/pkg/openai"
)

type stringArg struct{}

func (stringArg) Match(v driver.Value) bool {
	_, ok := v.(string)
	return ok
}

func newConversationRepoMock(t *testing.T) (*ConversationRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewConversationRepo(db), mock
}

func TestConversationRepo_EnsureTable(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_conversations").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.EnsureTable(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConversationRepo_CountTotal(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(5)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(rows)
	c, err := r.CountTotal()
	if err != nil || c != 5 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestConversationRepo_GetMessages_Empty(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnError(sql.ErrNoRows)
	msgs, err := r.GetMessages("u1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty messages, got %d", len(msgs))
	}
}

func TestConversationRepo_GetMessages_Existing(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	raw := `[{"role":"user","content":"hi"}]`
	rows := sqlmock.NewRows([]string{"messages"}).AddRow(raw)
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnRows(rows)

	msgs, err := r.GetMessages("u1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hi" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestConversationRepo_GetMessages_InvalidJSON(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	rows := sqlmock.NewRows([]string{"messages"}).AddRow("not json")
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnRows(rows)

	_, err := r.GetMessages("u1", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestConversationRepo_AppendMessage(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO agent_conversations").
		WithArgs("u1", int64(1), stringArg{}, beijingTimeArg{}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := r.AppendMessage("u1", 1, openai.ChatMessage{Role: "user", Content: "hi"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConversationRepo_AppendMessages(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO agent_conversations").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := r.AppendMessages("u1", 1,
		openai.ChatMessage{Role: "user", Content: "a"},
		openai.ChatMessage{Role: "assistant", Content: "b"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConversationRepo_AppendMessages_RetryOnDuplicateKey(t *testing.T) {
	r, mock := newConversationRepoMock(t)

	// first attempt insert conflicts
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO agent_conversations").WillReturnError(&mysql.MySQLError{Number: 1062, Message: "duplicate"})
	mock.ExpectRollback()

	// second attempt finds existing row and updates
	mock.ExpectBegin()
	raw := `[{"role":"user","content":"old"}]`
	rows := sqlmock.NewRows([]string{"messages"}).AddRow(raw)
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnRows(rows)
	mock.ExpectExec("INSERT INTO agent_conversations").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	if err := r.AppendMessages("u1", 1, openai.ChatMessage{Role: "user", Content: "new"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConversationRepo_AppendMessages_NonDuplicateError(t *testing.T) {
	r, mock := newConversationRepoMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT messages FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO agent_conversations").WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	if err := r.AppendMessages("u1", 1, openai.ChatMessage{Role: "user", Content: "x"}); err == nil {
		t.Error("expected error")
	}
}

func TestConversationRepo_Clear(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	repo := NewConversationRepo(db)
	mock.ExpectExec("DELETE FROM agent_conversations").
		WithArgs("u1", int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Clear("u1", 100); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
