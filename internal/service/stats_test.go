package service

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/repository"
)

func newStatsDeps(t *testing.T) (*StatsService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	userRepo := repository.NewUserRepo(db)
	convRepo := repository.NewConversationRepo(db)
	logRepo := repository.NewCallLogRepo(db)
	return NewStatsService(userRepo, convRepo, logRepo), mock
}

func TestStatsService_GetStats(t *testing.T) {
	svc, mock := newStatsDeps(t)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(60))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 2))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 2))
	mock.ExpectQuery(`SELECT action, COUNT\(\*\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"action", "count"}).AddRow("put_text", 10))

	data, err := svc.GetStats()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.TotalUsers != 100 || data.BoundUsers != 70 || data.UnboundUsers != 30 {
		t.Errorf("unexpected user stats: %+v", data)
	}
	if data.TotalConversations != 50 {
		t.Errorf("unexpected conversations: %d", data.TotalConversations)
	}
	if data.Calls7Days != 14 || data.Calls30Days != 60 {
		t.Errorf("unexpected calls: %+v", data)
	}
}

func TestStatsService_ToJSON(t *testing.T) {
	svc := NewStatsService(nil, nil, nil)
	jsonStr, err := svc.ToJSON(&StatsData{TotalUsers: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jsonStr == "" {
		t.Error("empty json")
	}
}

func TestFillMissingDates(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	input := []repository.DailyCallStat{
		{Date: yesterday, Count: 5},
		{Date: today, Count: 3},
	}

	result := fillMissingDates(input, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 days, got %d", len(result))
	}
	if result[1].Date != yesterday || result[1].Count != 5 {
		t.Fatalf("unexpected yesterday: %+v", result[1])
	}
	if result[2].Date != today || result[2].Count != 3 {
		t.Fatalf("unexpected today: %+v", result[2])
	}
	if result[0].Count != 0 {
		t.Fatalf("expected 0 for day before yesterday, got %v", result[0].Count)
	}
}

func TestStatsService_GetStats_Error(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFillMissingDates_ZeroDays(t *testing.T) {
	result := fillMissingDates([]repository.DailyCallStat{}, 0)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestStatsService_GetStats_CallsError(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_DailyStatsError(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(60))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}
