package repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDeadLetterRepo_EnsureTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewDeadLetterRepo(db)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS persist_dead_letters").WillReturnResult(sqlmock.NewResult(0, 0))

	if err := repo.EnsureTable(); err != nil {
		t.Fatalf("ensure table failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestDeadLetterRepo_RecordConv(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewDeadLetterRepo(db)
	mock.ExpectExec("INSERT INTO persist_dead_letters").
		WithArgs("conv", sqlmock.AnyArg(), "db down", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.RecordConv("u1", 1, []StoredChatMessage{}, errors.New("db down")); err != nil {
		t.Fatalf("record conv failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestDeadLetterRepo_RecordLog(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewDeadLetterRepo(db)
	mock.ExpectExec("INSERT INTO persist_dead_letters").
		WithArgs("log", sqlmock.AnyArg(), "db down", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.RecordLog("u1", "k1", "miaodi", "put_text", errors.New("db down")); err != nil {
		t.Fatalf("record log failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
