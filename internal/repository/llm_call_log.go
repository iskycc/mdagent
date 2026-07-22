package repository

import (
	"database/sql"
	"fmt"

	"miaodi-agent/internal/timeutil"
)

// LLMDailyStat 是按日期聚合的 LLM 调用次数与 Token 消耗。
type LLMDailyStat struct {
	Date             string `json:"date"`
	Count            int    `json:"count"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

// LLMCallLogRepo LLM 调用日志数据访问层
type LLMCallLogRepo struct {
	db *sql.DB
}

// NewLLMCallLogRepo 创建 LLM 调用日志仓库
func NewLLMCallLogRepo(db *sql.DB) *LLMCallLogRepo {
	return &LLMCallLogRepo{db: db}
}

// EnsureTable 确保 llm_call_log 表存在。新增表不影响已有的 api_call_log 数据。
func (r *LLMCallLogRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS llm_call_log (
			id INT AUTO_INCREMENT PRIMARY KEY,
			channel_user_id VARCHAR(128) NOT NULL COMMENT '传送鸽用户ID',
			model VARCHAR(64) DEFAULT '' COMMENT '模型名',
			prompt_tokens INT DEFAULT 0 COMMENT '提示 token 数',
			completion_tokens INT DEFAULT 0 COMMENT '生成 token 数',
			total_tokens INT DEFAULT 0 COMMENT '总 token 数',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_channel_user (channel_user_id),
			INDEX idx_created_at (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	return err
}

// Record 记录一次 LLM 调用，包含 token 消耗。失败调用也可以记录，token 数为 0。
func (r *LLMCallLogRepo) Record(channelUserID, model string, promptTokens, completionTokens, totalTokens int) error {
	_, err := r.db.Exec(`
		INSERT INTO llm_call_log(channel_user_id, model, prompt_tokens, completion_tokens, total_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		channelUserID, model, promptTokens, completionTokens, totalTokens, timeutil.Now())
	return err
}

// TotalCalls 查询近 N 天 LLM 总调用次数（N<=0 表示全部）
func (r *LLMCallLogRepo) TotalCalls(days int) (int, error) {
	var query string
	var args []interface{}
	if days > 0 {
		query = "SELECT COUNT(*) FROM llm_call_log WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)"
		args = append(args, days)
	} else {
		query = "SELECT COUNT(*) FROM llm_call_log"
	}
	var count int
	err := r.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// DailyStats 按天统计近 N 天 LLM 调用次数与 token 消耗。
func (r *LLMCallLogRepo) DailyStats(days int) ([]LLMDailyStat, error) {
	if days <= 0 {
		days = 30
	}
	query := `
		SELECT DATE(created_at) as date,
		       COUNT(*) as count,
		       COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
		       COALESCE(SUM(completion_tokens), 0) as completion_tokens,
		       COALESCE(SUM(total_tokens), 0) as total_tokens
		FROM llm_call_log
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`
	rows, err := r.db.Query(query, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]LLMDailyStat, 0)
	for rows.Next() {
		var raw interface{}
		var s LLMDailyStat
		if err := rows.Scan(&raw, &s.Count, &s.PromptTokens, &s.CompletionTokens, &s.TotalTokens); err != nil {
			return nil, err
		}
		date, err := parseDateValue(raw)
		if err != nil {
			return nil, fmt.Errorf("parse llm daily date: %w", err)
		}
		s.Date = date
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// CleanupOlderThan 删除指定天数之前的 LLM 调用日志。
func (r *LLMCallLogRepo) CleanupOlderThan(days int) (int64, error) {
	if days <= 0 {
		days = 30
	}
	cutoff := timeutil.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	res, err := r.db.Exec("DELETE FROM llm_call_log WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
