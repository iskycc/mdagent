package service

import (
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/metrics"
	"miaodi-agent/internal/repository"
	"miaodi-agent/internal/timeutil"
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
	llmCallLogRepo := repository.NewLLMCallLogRepo(db)
	processedMsgRepo := repository.NewProcessedMessageRepo(db)
	return NewStatsService(userRepo, convRepo, logRepo, llmCallLogRepo, processedMsgRepo), mock
}

func TestStatsService_GetStats(t *testing.T) {
	svc, mock := newStatsDeps(t)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(60))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(28))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(120))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(200))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 2))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 2))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count", "prompt_tokens", "completion_tokens", "total_tokens"}).AddRow("2026-06-30", 2, 100, 50, 150))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count", "prompt_tokens", "completion_tokens", "total_tokens"}).AddRow("2026-06-30", 2, 100, 50, 150))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 5))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 5))
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
	if data.LLMCalls7Days != 28 || data.LLMCalls30Days != 120 {
		t.Errorf("unexpected llm calls: %+v", data)
	}
	if data.Messages7Days != 50 || data.Messages30Days != 200 {
		t.Errorf("unexpected message stats: %+v", data)
	}
}

func TestStatsService_ToJSON(t *testing.T) {
	svc := NewStatsService(nil, nil, nil, nil, nil)
	jsonStr, err := svc.ToJSON(&StatsData{TotalUsers: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jsonStr == "" {
		t.Error("empty json")
	}
}

func TestFillMissingDates(t *testing.T) {
	today := timeutil.Date()
	yesterday := timeutil.Now().AddDate(0, 0, -1).Format("2006-01-02")

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
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(28))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(120))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_BoundError(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_ConversationError(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_Calls30Error(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_ActiveUsers7Error(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(60))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(28))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(120))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(200))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_ActiveUsers30Error(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(60))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(28))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(120))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(200))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_Daily30Error(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(60))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(28))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(120))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(200))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 2))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_GetStats_ActionStatsError(t *testing.T) {
	svc, mock := newStatsDeps(t)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users$`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_users WHERE status`).WithArgs("bound").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(70))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM agent_conversations`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(14))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(60))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(28))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM llm_call_log WHERE created_at`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(120))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM processed_messages`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(200))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT channel_user_id\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 2))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 2))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count", "prompt_tokens", "completion_tokens", "total_tokens"}).AddRow("2026-06-30", 2, 100, 50, 150))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count", "prompt_tokens", "completion_tokens", "total_tokens"}).AddRow("2026-06-30", 2, 100, 50, 150))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 5))
	mock.ExpectQuery(`SELECT DATE\(created_at\)`).WithArgs(30).WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-30", 5))
	mock.ExpectQuery(`SELECT action, COUNT\(\*\)`).WithArgs(30).WillReturnError(sqlmock.ErrCancelled)
	_, err := svc.GetStats()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStatsService_ToJSON_Error(t *testing.T) {
	svc := NewStatsService(nil, nil, nil, nil, nil)
	_, err := svc.ToJSON(&StatsData{Performance: []metrics.MetricSnapshot{{SuccessRate: math.Inf(1)}}})
	if err == nil {
		t.Fatal("expected error")
	}
}
