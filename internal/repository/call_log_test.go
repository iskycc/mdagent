package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"miaodi-agent/internal/timeutil"
)

func newCallLogRepoMock(t *testing.T) (*CallLogRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewCallLogRepo(db), mock
}

func TestCallLogRepo_EnsureTable(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS api_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.EnsureTable(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallLogRepo_Record(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", hashAPIKey("key"), "miaodi", "put_text", beijingTimeArg{}).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.Record("u1", "key", "miaodi", "put_text"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallLogRepo_TotalCalls_WithDays(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(7)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(rows)
	c, err := r.TotalCalls(7)
	if err != nil || c != 7 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestCallLogRepo_TotalCalls_All(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(100)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log$`).WillReturnRows(rows)
	c, err := r.TotalCalls(0)
	if err != nil || c != 100 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestCallLogRepo_DailyStats(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count"}).
		AddRow("2026-06-29", 3).
		AddRow("2026-06-30", 4)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)
	stats, err := r.DailyStats(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("expected 2 rows, got %d", len(stats))
	}
}

// TestCallLogRepo_DailyStats_DateFormatting 验证 DATE 类型在 parseTime=true 时被驱动
// 解析为 time.Time，最终应格式化为 YYYY-MM-DD 字符串，而不是 time.Time 的默认格式。
func TestCallLogRepo_DailyStats_DateFormatting(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count"}).
		AddRow(time.Date(2026, 7, 19, 0, 0, 0, 0, timeutil.BeijingLocation()), 6)
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
	if stats[0].Count != 6 {
		t.Errorf("expected count 6, got %d", stats[0].Count)
	}
}

func TestCallLogRepo_ActiveUsers(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(5)
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(rows)
	c, err := r.ActiveUsers(30)
	if err != nil || c != 5 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestCallLogRepo_ActionStats(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "count"}).
		AddRow("put_text", 10).
		AddRow("bind_key", 2)
	mock.ExpectQuery(`SELECT action, COUNT\(\*\)`).WithArgs(30).WillReturnRows(rows)
	stats, err := r.ActionStats(30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 2 {
		t.Errorf("expected 2 rows, got %d", len(stats))
	}
}

func TestCallLogRepo_ActionStats_Empty(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT action, COUNT\(\*\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"action", "count"}))
	stats, err := r.ActionStats(30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(stats) != 0 {
		t.Fatalf("expected empty stats, got %d", len(stats))
	}
}

func TestCallLogRepo_DailyStats_QueryError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.DailyStats(7)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_DailyStats_ScanError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", "not-int")
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)
	_, err := r.DailyStats(7)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_ActiveUsers_QueryError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.ActiveUsers(7)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_ActionStats_QueryError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT action, COUNT\(\*\)`).WithArgs(30).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.ActionStats(30)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_ActionStats_ScanError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "count"}).AddRow("put_text", "not-int")
	mock.ExpectQuery(`SELECT action, COUNT\(\*\)`).WithArgs(30).WillReturnRows(rows)
	_, err := r.ActionStats(30)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_RecentByUser(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)).
		AddRow("save_image_pending", time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC))
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).
		WithArgs("u1", 5).WillReturnRows(rows)

	results, err := r.RecentByUser("u1", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 rows, got %d", len(results))
	}
}

func TestCallLogRepo_RecentByUser_OverLimit(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).
		WithArgs("u1", 20).WillReturnRows(sqlmock.NewRows([]string{"action", "created_at"}))

	_, err := r.RecentByUser("u1", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallLogRepo_CleanupOlderThan(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectExec("DELETE FROM api_call_log WHERE created_at < ?").WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 5))

	deleted, err := r.CleanupOlderThan(30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 5 {
		t.Fatalf("expected 5 deleted, got %d", deleted)
	}
}

func TestCallLogRepo_ByDate(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC))
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND created_at >= \? AND created_at < \?`).
		WithArgs("u1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(rows)

	results, err := r.ByDate("u1", "2026-06-30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 row, got %d", len(results))
	}
}

func TestCallLogRepo_DailyStats_Empty(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}))
	stats, err := r.DailyStats(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats == nil || len(stats) != 0 {
		t.Fatalf("expected empty stats, got %+v", stats)
	}
}

func TestCallLogRepo_DailyStats_RowsErr(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 1).CloseError(sqlmock.ErrCancelled)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)
	_, err := r.DailyStats(7)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_DailyStats_ParseDateValue_Unsupported(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count"}).AddRow(123, 1)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)
	_, err := r.DailyStats(7)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_DailyStats_ParseDateValue_Bytes(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"date", "count"}).AddRow([]byte("2026-07-20"), 1)
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(rows)
	stats, err := r.DailyStats(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 1 || stats[0].Date != "2026-07-20" {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCallLogRepo_ActiveUsers_DefaultDays(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"count"}).AddRow(4)
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(rows)
	c, err := r.ActiveUsers(0)
	if err != nil || c != 4 {
		t.Fatalf("unexpected result: %d %v", c, err)
	}
}

func TestCallLogRepo_CleanupOlderThan_DefaultDays(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectExec("DELETE FROM api_call_log WHERE created_at < ?").WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 3))
	deleted, err := r.CleanupOlderThan(0)
	if err != nil || deleted != 3 {
		t.Fatalf("unexpected result: %d %v", deleted, err)
	}
}

func TestCallLogRepo_CleanupOlderThan_RowsAffectedError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectExec("DELETE FROM api_call_log WHERE created_at < ?").WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewErrorResult(sqlmock.ErrCancelled))
	_, err := r.CleanupOlderThan(30)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_RecentByUser_DefaultLimit(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnRows(sqlmock.NewRows([]string{"action", "created_at"}))
	_, err := r.RecentByUser("u1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallLogRepo_RecentByUser_UnderLimit(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnRows(sqlmock.NewRows([]string{"action", "created_at"}))
	_, err := r.RecentByUser("u1", -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallLogRepo_RecentByUser_RowsErr(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).AddRow("put_text", time.Now()).CloseError(sqlmock.ErrCancelled)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnRows(rows)
	_, err := r.RecentByUser("u1", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_RecentByUser_ScanError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).AddRow("put_text", "not-time")
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnRows(rows)
	_, err := r.RecentByUser("u1", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_ByDate_InvalidDate(t *testing.T) {
	r, _ := newCallLogRepoMock(t)
	_, err := r.ByDate("u1", "not-a-date")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_ByDate_QueryError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND created_at >= \? AND created_at < \?`).
		WithArgs("u1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.ByDate("u1", "2026-06-30")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_ByDate_ScanError(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).AddRow("put_text", "not-time")
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND created_at >= \? AND created_at < \?`).
		WithArgs("u1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(rows)
	_, err := r.ByDate("u1", "2026-06-30")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallLogRepo_ByDate_RowsErr(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).AddRow("put_text", time.Now()).CloseError(sqlmock.ErrCancelled)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND created_at >= \? AND created_at < \?`).
		WithArgs("u1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(rows)
	_, err := r.ByDate("u1", "2026-06-30")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseDateValue_Time(t *testing.T) {
	date, err := parseDateValue(time.Date(2026, 7, 20, 0, 0, 0, 0, timeutil.BeijingLocation()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if date != "2026-07-20" {
		t.Fatalf("unexpected date: %s", date)
	}
}

func TestParseDateValue_String(t *testing.T) {
	date, err := parseDateValue("2026-07-20")
	if err != nil || date != "2026-07-20" {
		t.Fatalf("unexpected result: %s %v", date, err)
	}
}

func TestParseDateValue_Unsupported(t *testing.T) {
	_, err := parseDateValue(123)
	if err == nil {
		t.Fatal("expected error")
	}
}
