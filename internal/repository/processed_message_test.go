package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func TestProcessedMessageRepo_EnsureTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS processed_messages").WillReturnResult(sqlmock.NewResult(0, 0))

	if err := repo.EnsureTable(); err != nil {
		t.Fatalf("ensure table failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_StartProcessing_New(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("INSERT INTO processed_messages").
		WithArgs("u1", int64(1), int64(100), ProcessedMessageProcessing, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	shouldProcess, reply, err := repo.StartProcessing("u1", 1, 100, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldProcess {
		t.Fatal("expected shouldProcess=true for new message")
	}
	if reply != "" {
		t.Fatalf("expected empty reply, got %q", reply)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_StartProcessing_Done(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("INSERT INTO processed_messages").
		WithArgs("u1", int64(1), int64(100), ProcessedMessageProcessing, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectQuery("SELECT status, reply, updated_at FROM processed_messages").
		WithArgs("u1", int64(1), int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "reply", "updated_at"}).
			AddRow(string(ProcessedMessageDone), "saved", time.Now()))

	shouldProcess, reply, err := repo.StartProcessing("u1", 1, 100, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldProcess {
		t.Fatal("expected shouldProcess=false for done message")
	}
	if reply != "saved" {
		t.Fatalf("expected saved reply, got %q", reply)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_StartProcessing_ProcessingNotTimeout(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("INSERT INTO processed_messages").
		WithArgs("u1", int64(1), int64(100), ProcessedMessageProcessing, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectQuery("SELECT status, reply, updated_at FROM processed_messages").
		WithArgs("u1", int64(1), int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "reply", "updated_at"}).
			AddRow(string(ProcessedMessageProcessing), nil, time.Now()))

	shouldProcess, reply, err := repo.StartProcessing("u1", 1, 100, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldProcess {
		t.Fatal("expected shouldProcess=false for processing message")
	}
	if reply == "" {
		t.Fatal("expected wait reply")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_StartProcessing_ProcessingTimeoutAllowsRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("INSERT INTO processed_messages").
		WithArgs("u1", int64(1), int64(100), ProcessedMessageProcessing, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectQuery("SELECT status, reply, updated_at FROM processed_messages").
		WithArgs("u1", int64(1), int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "reply", "updated_at"}).
			AddRow(string(ProcessedMessageProcessing), nil, time.Now().Add(-time.Hour)))
	mock.ExpectExec("UPDATE processed_messages SET status").
		WithArgs(ProcessedMessageProcessing, sqlmock.AnyArg(), "u1", int64(1), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	shouldProcess, reply, err := repo.StartProcessing("u1", 1, 100, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldProcess {
		t.Fatal("expected shouldProcess=true for timed-out processing message")
	}
	if reply != "" {
		t.Fatalf("expected empty reply, got %q", reply)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_StartProcessing_FailedAllowsRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("INSERT INTO processed_messages").
		WithArgs("u1", int64(1), int64(100), ProcessedMessageProcessing, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectQuery("SELECT status, reply, updated_at FROM processed_messages").
		WithArgs("u1", int64(1), int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "reply", "updated_at"}).
			AddRow(string(ProcessedMessageFailed), nil, time.Now()))
	mock.ExpectExec("UPDATE processed_messages SET status").
		WithArgs(ProcessedMessageProcessing, sqlmock.AnyArg(), "u1", int64(1), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	shouldProcess, reply, err := repo.StartProcessing("u1", 1, 100, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldProcess {
		t.Fatal("expected shouldProcess=true for failed message")
	}
	if reply != "" {
		t.Fatalf("expected empty reply, got %q", reply)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_MarkDone(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("UPDATE processed_messages SET status").
		WithArgs(ProcessedMessageDone, "hello", sqlmock.AnyArg(), "u1", int64(1), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkDone("u1", 1, 100, "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_MarkFailed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectExec("UPDATE processed_messages SET status").
		WithArgs(ProcessedMessageFailed, sqlmock.AnyArg(), "u1", int64(1), int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkFailed("u1", 1, 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_TotalMessages(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM processed_messages").
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	count, err := repo.TotalMessages(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 42 {
		t.Errorf("expected 42, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestProcessedMessageRepo_DailyMessageStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewProcessedMessageRepo(db)
	mock.ExpectQuery("SELECT DATE\\(created_at\\)").
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-07-22", 5))

	stats, err := repo.DailyMessageStats(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 1 || stats[0].Date != "2026-07-22" || stats[0].Count != 5 {
		t.Errorf("unexpected stats: %+v", stats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
