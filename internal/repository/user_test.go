package repository

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newUserRepoMock(t *testing.T) (*UserRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewUserRepo(db), mock
}

func TestUserRepo_EnsureTable(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE agent_users").WithArgs(DefaultBook, DefaultChara).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.EnsureTable(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUserRepo_EnsureTable_MigrateError(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE agent_users").WithArgs(DefaultBook, DefaultChara).WillReturnError(sqlmock.ErrCancelled)
	if err := r.EnsureTable(); err == nil {
		t.Fatal("expected error")
	}
}

func TestUserRepo_Get(t *testing.T) {
	r, mock := newUserRepoMock(t)
	rows := sqlmock.NewRows([]string{"channel_user_id", "apikey", "status", "book", "chara", "title"}).
		AddRow("u1", "key1", "bound", "book1", "chara1", "title1")
	mock.ExpectQuery("SELECT channel_user_id").WithArgs("u1").WillReturnRows(rows)

	user, err := r.Get("u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ChannelUserID != "u1" || user.Status != "bound" {
		t.Errorf("unexpected user: %+v", user)
	}
}

func TestUserRepo_GetOrCreate_Existing(t *testing.T) {
	r, mock := newUserRepoMock(t)
	rows := sqlmock.NewRows([]string{"channel_user_id", "apikey", "status", "book", "chara", "title"}).
		AddRow("u1", "key1", "bound", "book1", "chara1", "title1")
	mock.ExpectQuery("SELECT channel_user_id").WithArgs("u1").WillReturnRows(rows)

	user, err := r.GetOrCreate("u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ChannelUserID != "u1" {
		t.Errorf("unexpected user: %+v", user)
	}
}

func TestUserRepo_GetOrCreate_New(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectQuery("SELECT channel_user_id").WithArgs("u1").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT IGNORE INTO agent_users").WithArgs("u1", DefaultBook, DefaultChara).WillReturnResult(sqlmock.NewResult(1, 1))
	rows := sqlmock.NewRows([]string{"channel_user_id", "apikey", "status", "book", "chara", "title"}).
		AddRow("u1", "", "unbound", DefaultBook, DefaultChara, "")
	mock.ExpectQuery("SELECT channel_user_id").WithArgs("u1").WillReturnRows(rows)

	user, err := r.GetOrCreate("u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ChannelUserID != "u1" {
		t.Errorf("unexpected user: %+v", user)
	}
}

func TestUserRepo_CountTotal(t *testing.T) {
	r, mock := newUserRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(10)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users`).WillReturnRows(rows)

	c, err := r.CountTotal()
	if err != nil || c != 10 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestUserRepo_CountByStatus(t *testing.T) {
	r, mock := newUserRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(3)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(rows)

	c, err := r.CountByStatus("bound")
	if err != nil || c != 3 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestUserRepo_UpdateField(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectExec("UPDATE agent_users SET book").WithArgs("newbook", "u1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.UpdateField("u1", "book", "newbook"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUserRepo_UpdateField_Invalid(t *testing.T) {
	r, _ := newUserRepoMock(t)
	if err := r.UpdateField("u1", "invalid", "x"); err == nil {
		t.Error("expected error for invalid field")
	}
}

func TestUserRepo_UpdateAPIKeyAndStatus(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectExec("UPDATE agent_users SET apikey").WithArgs("key", "bound", "u1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.UpdateAPIKeyAndStatus("u1", "key", "bound"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUserRepo_UpdateSavePath(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectExec("UPDATE agent_users SET book").WithArgs("b", "c", "t", "u1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.UpdateSavePath("u1", "b", "c", "t"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUserRepo_GetOrCreate_DBError(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectQuery("SELECT channel_user_id").WithArgs("u1").WillReturnError(sqlmock.ErrCancelled)
	_, err := r.GetOrCreate("u1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUserRepo_GetOrCreate_InsertError(t *testing.T) {
	r, mock := newUserRepoMock(t)
	mock.ExpectQuery("SELECT channel_user_id").WithArgs("u1").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT IGNORE INTO agent_users").WithArgs("u1", DefaultBook, DefaultChara).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.GetOrCreate("u1")
	if err == nil {
		t.Fatal("expected error")
	}
}
