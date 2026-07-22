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
	ActiveUsers7Days   int                         `json:"active_users_7_days"`
	ActiveUsers30Days  int                         `json:"active_users_30_days"`
	Daily7Days         []repository.DailyCallStat  `json:"daily_7_days"`
	Daily30Days        []repository.DailyCallStat  `json:"daily_30_days"`
	ActionStats        []repository.ActionCallStat `json:"action_stats"`
	Performance        []metrics.MetricSnapshot    `json:"performance"`
	GeneratedAt        string                      `json:"generated_at"`
}

// StatsService 统计服务
type StatsService struct {
	userRepo *repository.UserRepo
	convRepo *repository.ConversationRepo
	logRepo  *repository.CallLogRepo
}

// NewStatsService 创建统计服务
func NewStatsService(userRepo *repository.UserRepo, convRepo *repository.ConversationRepo, logRepo *repository.CallLogRepo) *StatsService {
	return &StatsService{
		userRepo: userRepo,
		convRepo: convRepo,
		logRepo:  logRepo,
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
