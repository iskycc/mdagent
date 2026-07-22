package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MetricSample 是一条指标采样记录。
type MetricSample struct {
	Name       string
	DurationMs float64
	Success    bool
	CreatedAt  time.Time
}

// MetricSampleRepo 指标采样数据访问层。
type MetricSampleRepo struct {
	db *sql.DB
}

// NewMetricSampleRepo 创建指标采样仓库。
func NewMetricSampleRepo(db *sql.DB) *MetricSampleRepo {
	return &MetricSampleRepo{db: db}
}

// EnsureTable 确保 metric_samples 表存在。
func (r *MetricSampleRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS metric_samples (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			duration_ms DOUBLE NOT NULL,
			success TINYINT(1) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_name_created (name, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	return err
}

// Save 批量保存指标采样记录。空切片直接返回 nil。
func (r *MetricSampleRepo) Save(samples []MetricSample) error {
	if len(samples) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(samples))
	args := make([]interface{}, 0, len(samples)*4)
	for _, s := range samples {
		placeholders = append(placeholders, "(?, ?, ?, ?)")
		args = append(args, s.Name, s.DurationMs, boolToInt(s.Success), s.CreatedAt)
	}

	query := fmt.Sprintf(
		"INSERT INTO metric_samples(name, duration_ms, success, created_at) VALUES %s",
		strings.Join(placeholders, ", "),
	)
	_, err := r.db.Exec(query, args...)
	return err
}

// LoadRecent 查询最近的 limit 条采样记录，按时间升序返回。
func (r *MetricSampleRepo) LoadRecent(limit int) ([]MetricSample, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(`
		SELECT name, duration_ms, success, created_at
		FROM metric_samples
		ORDER BY created_at ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]MetricSample, 0)
	for rows.Next() {
		var s MetricSample
		var successInt int
		if err := rows.Scan(&s.Name, &s.DurationMs, &successInt, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.Success = successInt != 0
		results = append(results, s)
	}
	return results, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
