package service

import (
	"encoding/json"

	"miaodi-agent/internal/metrics"
	"miaodi-agent/internal/repository"
	"miaodi-agent/internal/timeutil"
)

// StatsData 统计页面数据模型
type StatsData struct {
	TotalUsers         int                         `json:"total_users"`
	BoundUsers         int                         `json:"bound_users"`
	UnboundUsers       int                         `json:"unbound_users"`
	TotalConversations int                         `json:"total_conversations"`
	Calls7Days         int                         `json:"calls_7_days"`
	Calls30Days        int                         `json:"calls_30_days"`
	LLMCalls7Days      int                         `json:"llm_calls_7_days"`
	LLMCalls30Days     int                         `json:"llm_calls_30_days"`
	ActiveUsers7Days   int                         `json:"active_users_7_days"`
	ActiveUsers30Days  int                         `json:"active_users_30_days"`
	Daily7Days         []repository.DailyCallStat  `json:"daily_7_days"`
	Daily30Days        []repository.DailyCallStat  `json:"daily_30_days"`
	LLMDaily7Days      []repository.LLMDailyStat   `json:"llm_daily_7_days"`
	LLMDaily30Days     []repository.LLMDailyStat   `json:"llm_daily_30_days"`
	ActionStats        []repository.ActionCallStat `json:"action_stats"`
	Performance        []metrics.MetricSnapshot    `json:"performance"`
	GeneratedAt        string                      `json:"generated_at"`
}

// StatsService 统计服务
type StatsService struct {
	userRepo     *repository.UserRepo
	convRepo     *repository.ConversationRepo
	logRepo      *repository.CallLogRepo
	llmCallLogRepo *repository.LLMCallLogRepo
}

// NewStatsService 创建统计服务
func NewStatsService(userRepo *repository.UserRepo, convRepo *repository.ConversationRepo, logRepo *repository.CallLogRepo, llmCallLogRepo *repository.LLMCallLogRepo) *StatsService {
	return &StatsService{
		userRepo:       userRepo,
		convRepo:       convRepo,
		logRepo:        logRepo,
		llmCallLogRepo: llmCallLogRepo,
	}
}

// GetStats 聚合所有统计数据
func (s *StatsService) GetStats() (*StatsData, error) {
	data := &StatsData{GeneratedAt: timeutil.DateTime()}

	totalUsers, err := s.userRepo.CountTotal()
	if err != nil {
		return nil, err
	}
	data.TotalUsers = totalUsers

	boundUsers, err := s.userRepo.CountByStatus("bound")
	if err != nil {
		return nil, err
	}
	data.BoundUsers = boundUsers
	data.UnboundUsers = totalUsers - boundUsers

	convCount, err := s.convRepo.CountTotal()
	if err != nil {
		return nil, err
	}
	data.TotalConversations = convCount

	calls7, err := s.logRepo.TotalCalls(7)
	if err != nil {
		return nil, err
	}
	data.Calls7Days = calls7

	calls30, err := s.logRepo.TotalCalls(30)
	if err != nil {
		return nil, err
	}
	data.Calls30Days = calls30

	llmCalls7, err := s.llmCallLogRepo.TotalCalls(7)
	if err != nil {
		return nil, err
	}
	data.LLMCalls7Days = llmCalls7

	llmCalls30, err := s.llmCallLogRepo.TotalCalls(30)
	if err != nil {
		return nil, err
	}
	data.LLMCalls30Days = llmCalls30

	active7, err := s.logRepo.ActiveUsers(7)
	if err != nil {
		return nil, err
	}
	data.ActiveUsers7Days = active7

	active30, err := s.logRepo.ActiveUsers(30)
	if err != nil {
		return nil, err
	}
	data.ActiveUsers30Days = active30

	daily7, err := s.logRepo.DailyStats(7)
	if err != nil {
		return nil, err
	}
	data.Daily7Days = fillMissingDates(daily7, 7)

	daily30, err := s.logRepo.DailyStats(30)
	if err != nil {
		return nil, err
	}
	data.Daily30Days = fillMissingDates(daily30, 30)

	llmDaily7, err := s.llmCallLogRepo.DailyStats(7)
	if err != nil {
		return nil, err
	}
	data.LLMDaily7Days = fillMissingLLMDates(llmDaily7, 7)

	llmDaily30, err := s.llmCallLogRepo.DailyStats(30)
	if err != nil {
		return nil, err
	}
	data.LLMDaily30Days = fillMissingLLMDates(llmDaily30, 30)

	actionStats, err := s.logRepo.ActionStats(30)
	if err != nil {
		return nil, err
	}
	data.ActionStats = actionStats

	data.Performance = metrics.Snapshot()

	return data, nil
}

// ToJSON 将统计数据序列化为 JSON 字符串（用于模板注入）
func (s *StatsService) ToJSON(data *StatsData) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fillMissingDates 把缺失的日期补 0，保证折线图连续
func fillMissingDates(stats []repository.DailyCallStat, days int) []repository.DailyCallStat {
	if days <= 0 {
		return stats
	}
	m := make(map[string]int)
	for _, stat := range stats {
		m[stat.Date] = stat.Count
	}

	var result []repository.DailyCallStat
	now := timeutil.Now()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		count := 0
		if c, ok := m[d]; ok {
			count = c
		}
		result = append(result, repository.DailyCallStat{Date: d, Count: count})
	}
	return result
}

// fillMissingLLMDates 把缺失的日期补 0，保证 LLM 折线图连续。
func fillMissingLLMDates(stats []repository.LLMDailyStat, days int) []repository.LLMDailyStat {
	if days <= 0 {
		return stats
	}
	m := make(map[string]repository.LLMDailyStat)
	for _, stat := range stats {
		m[stat.Date] = stat
	}

	var result []repository.LLMDailyStat
	now := timeutil.Now()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		if s, ok := m[d]; ok {
			result = append(result, s)
		} else {
			result = append(result, repository.LLMDailyStat{Date: d})
		}
	}
	return result
}
