package repository

import (
	"database/sql"
	"fmt"
	"time"

	"miaodi-agent/internal/timeutil"
)

// DailyCallStat 是按日期聚合的调用次数。
type DailyCallStat struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// ActionCallStat 是按操作类型聚合的调用次数。
type ActionCallStat struct {
	Action string `json:"action"`
	Count  int    `json:"count"`
}

// UserCallLog 是单个用户的一条调用记录。
type UserCallLog struct {
	Action    string
	CreatedAt time.Time
}

// CallLogRepo 调用日志数据访问层
type CallLogRepo struct {
	db *sql.DB
}

// NewCallLogRepo 创建调用日志仓库
func NewCallLogRepo(db *sql.DB) *CallLogRepo {
	return &CallLogRepo{db: db}
}

// EnsureTable 确保 api_call_log 表存在
func (r *CallLogRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS api_call_log (
			id INT AUTO_INCREMENT PRIMARY KEY,
			channel_user_id VARCHAR(128) NOT NULL COMMENT '传送鸽用户ID',
			apikey VARCHAR(64) DEFAULT '' COMMENT '喵滴API Key',
			channel VARCHAR(32) DEFAULT 'miaodi' COMMENT '渠道',
			action VARCHAR(64) DEFAULT '' COMMENT '操作类型',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_channel_user (channel_user_id),
			INDEX idx_channel (channel),
			INDEX idx_created_at (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	return err
}

// Record 记录一次 API 调用
func (r *CallLogRepo) Record(channelUserID, apikey, channel, action string) error {
	_, err := r.db.Exec(`
		INSERT INTO api_call_log(channel_user_id, apikey, channel, action, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		channelUserID, apikey, channel, action, timeutil.Now())
	return err
}

// TotalCalls 查询近 N 天总调用次数（N<=0 表示全部）
func (r *CallLogRepo) TotalCalls(days int) (int, error) {
	var query string
	var args []interface{}
	if days > 0 {
		query = "SELECT COUNT(*) FROM api_call_log WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)"
		args = append(args, days)
	} else {
		query = "SELECT COUNT(*) FROM api_call_log"
	}
	var count int
	err := r.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// DailyStats 按天统计近 N 天调用次数。
func (r *CallLogRepo) DailyStats(days int) ([]DailyCallStat, error) {
	if days <= 0 {
		days = 30
	}
	query := `
		SELECT DATE(created_at) as date, COUNT(*) as count
		FROM api_call_log
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`
	rows, err := r.db.Query(query, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]DailyCallStat, 0)
	for rows.Next() {
		var raw interface{}
		var count int
		if err := rows.Scan(&raw, &count); err != nil {
			return nil, err
		}
		date, err := parseDateValue(raw)
		if err != nil {
			return nil, err
		}
		stats = append(stats, DailyCallStat{Date: date, Count: count})
	}
	return stats, rows.Err()
}

// ActiveUsers 查询近 N 天活跃用户数（distinct channel_user_id）
func (r *CallLogRepo) ActiveUsers(days int) (int, error) {
	if days <= 0 {
		days = 30
	}
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT channel_user_id)
		FROM api_call_log
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)`,
		days).Scan(&count)
	return count, err
}

// ActionStats 按 action 统计近 N 天调用次数。
func (r *CallLogRepo) ActionStats(days int) ([]ActionCallStat, error) {
	if days <= 0 {
		days = 30
	}
	query := `
		SELECT action, COUNT(*) as count
		FROM api_call_log
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		GROUP BY action
		ORDER BY count DESC
	`
	rows, err := r.db.Query(query, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]ActionCallStat, 0)
	for rows.Next() {
		var action string
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			return nil, err
		}
		stats = append(stats, ActionCallStat{Action: action, Count: count})
	}
	return stats, rows.Err()
}

// parseDateValue 把 DATE 列的扫描结果统一转为 YYYY-MM-DD 字符串。
// MySQL 驱动在 parseTime=true 时返回 time.Time，sqlmock 等场景可能返回 string/[]byte。
func parseDateValue(v interface{}) (string, error) {
	switch d := v.(type) {
	case time.Time:
		return d.In(timeutil.BeijingLocation()).Format("2006-01-02"), nil
	case string:
		return d, nil
	case []byte:
		return string(d), nil
	default:
		return "", fmt.Errorf("unsupported date type %T", v)
	}
}

// CleanupOlderThan 删除指定天数之前的调用日志。
func (r *CallLogRepo) CleanupOlderThan(days int) (int64, error) {
	if days <= 0 {
		days = 30
	}
	cutoff := timeutil.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	res, err := r.db.Exec("DELETE FROM api_call_log WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RecentByUser 查询指定用户最近 N 条调用记录（按时间倒序）
func (r *CallLogRepo) RecentByUser(channelUserID string, limit int) ([]UserCallLog, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	rows, err := r.db.Query(`
		SELECT action, created_at
		FROM api_call_log
		WHERE channel_user_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, channelUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]UserCallLog, 0)
	for rows.Next() {
		var action string
		var createdAt time.Time
		if err := rows.Scan(&action, &createdAt); err != nil {
			return nil, err
		}
		results = append(results, UserCallLog{Action: action, CreatedAt: createdAt})
	}
	return results, rows.Err()
}

// ByDate 查询指定用户某一天的调用记录。
// 使用 created_at 的范围查询，避免对 DATE(created_at) 使用函数导致索引失效。
func (r *CallLogRepo) ByDate(channelUserID, date string) ([]UserCallLog, error) {
	start, err := time.ParseInLocation("2006-01-02", date, timeutil.BeijingLocation())
	if err != nil {
		return nil, fmt.Errorf("invalid date %q: %w", date, err)
	}
	end := start.AddDate(0, 0, 1)
	rows, err := r.db.Query(`
		SELECT action, created_at
		FROM api_call_log
		WHERE channel_user_id = ? AND created_at >= ? AND created_at < ?
		ORDER BY created_at DESC`, channelUserID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]UserCallLog, 0)
	for rows.Next() {
		var action string
		var createdAt time.Time
		if err := rows.Scan(&action, &createdAt); err != nil {
			return nil, err
		}
		results = append(results, UserCallLog{Action: action, CreatedAt: createdAt})
	}
	return results, rows.Err()
}
