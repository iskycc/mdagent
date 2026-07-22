package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newLLMCallLogRepoMock(t *testing.T) (*LLMCallLogRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewLLMCallLogRepo(db), mock
}

func TestLLMCallLogRepo_EnsureTable(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS llm_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.EnsureTable(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLLMCallLogRepo_Record(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	mock.ExpectExec("INSERT INTO llm_call_log").
		WithArgs("u1", "deepseek-v4", 100, 50, 150, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := r.Record("u1", "deepseek-v4", 100, 50, 150); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLLMCallLogRepo_TotalCalls_WithDays(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(7)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(7).WillReturnRows(rows)
	c, err := r.TotalCalls(7)
	if err != nil || c != 7 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestLLMCallLogRepo_TotalCalls_All(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(100)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log$`).WillReturnRows(rows)
	c, err := r.TotalCalls(0)
	if err != nil || c != 100 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestLLMCallLogRepo_DailyStats(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count", "prompt_tokens", "completion_tokens", "total_tokens"}).
		AddRow("2026-06-30", 3, 100, 50, 150).
		AddRow("2026-07-01", 4, 200, 80, 280)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)

	stats, err := r.DailyStats(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(stats))
	}
	if stats[0].TotalTokens != 150 || stats[1].TotalTokens != 280 {
		t.Errorf("unexpected total tokens: %+v", stats)
	}
}

func TestLLMCallLogRepo_DailyStats_QueryError(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.DailyStats(30)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLLMCallLogRepo_DailyStats_ScanError(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count", "prompt_tokens", "completion_tokens", "total_tokens"}).
		AddRow("2026-06-30", "not-int", 100, 50, 150)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)
	_, err := r.DailyStats(7)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLLMCallLogRepo_CleanupOlderThan(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	mock.ExpectExec("DELETE FROM llm_call_log WHERE created_at < ?").WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 5))

	deleted, err := r.CleanupOlderThan(30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 5 {
		t.Fatalf("expected 5 deleted, got %d", deleted)
	}
}

func TestLLMCallLogRepo_DailyStats_TimeDate(t *testing.T) {
	r, mock := newLLMCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count", "prompt_tokens", "completion_tokens", "total_tokens"}).
		AddRow(time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC), 6, 100, 50, 150)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)

	stats, err := r.DailyStats(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 row, got %d", len(stats))
	}
	if stats[0].Date != "2026-07-19" {
		t.Errorf("expected date 2026-07-19, got %s", stats[0].Date)
	}
}
