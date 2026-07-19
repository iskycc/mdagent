package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newPendingImageRepoMock(t *testing.T) (*PendingImageRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewPendingImageRepo(db), mock
}

func TestPendingImageRepo_EnsureTable(t *testing.T) {
	r, mock := newPendingImageRepoMock(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS pending_images").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.EnsureTable(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPendingImageRepo_Insert(t *testing.T) {
	r, mock := newPendingImageRepoMock(t)
	mock.ExpectExec("INSERT INTO pending_images").WithArgs("key1", "http://img", "book", "chara", "title", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.Insert("key1", "http://img", "book", "chara", "title"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPendingImageRepo_ListPending(t *testing.T) {
	r, mock := newPendingImageRepoMock(t)
	createdAt := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"id", "apikey", "image_url", "book", "chara", "title", "status", "created_at"}).
		AddRow(1, "key1", "http://img", "b", "c", "t", "pending", createdAt)
	mock.ExpectQuery("SELECT id, apikey").WithArgs(10).WillReturnRows(rows)

	list, err := r.ListPending(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 item, got %d", len(list))
	}
	if !list[0].CreatedAt.Equal(createdAt) {
		t.Errorf("unexpected created_at: %v", list[0].CreatedAt)
	}
}

func TestPendingImageRepo_ListPending_Empty(t *testing.T) {
	r, mock := newPendingImageRepoMock(t)
	rows := sqlmock.NewRows([]string{"id", "apikey", "image_url", "book", "chara", "title", "status", "created_at"})
	mock.ExpectQuery("SELECT id, apikey").WithArgs(10).WillReturnRows(rows)

	list, err := r.ListPending(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestPendingImageRepo_UpdateStatus(t *testing.T) {
	r, mock := newPendingImageRepoMock(t)
	mock.ExpectExec("UPDATE pending_images SET status").WithArgs("done", int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.UpdateStatus(1, "done"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPendingImageRepo_ListPending_QueryError(t *testing.T) {
	r, mock := newPendingImageRepoMock(t)
	mock.ExpectQuery("SELECT id, apikey").WithArgs(10).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.ListPending(10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPendingImageRepo_ListPending_ScanError(t *testing.T) {
	r, mock := newPendingImageRepoMock(t)
	rows := sqlmock.NewRows([]string{"id", "apikey", "image_url", "book", "chara", "title", "status", "created_at"}).
		AddRow("bad-id", "key1", "http://img", "b", "c", "t", "pending", time.Now())
	mock.ExpectQuery("SELECT id, apikey").WithArgs(10).WillReturnRows(rows)

	_, err := r.ListPending(10)
	if err == nil {
		t.Fatal("expected error")
	}
}
