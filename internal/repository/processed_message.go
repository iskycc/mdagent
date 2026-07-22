package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"miaodi-agent/internal/timeutil"
)

// ProcessedMessageStatus 表示消息处理状态。
type ProcessedMessageStatus string

const (
	ProcessedMessageProcessing ProcessedMessageStatus = "processing"
	ProcessedMessageDone       ProcessedMessageStatus = "done"
	ProcessedMessageFailed     ProcessedMessageStatus = "failed"
)

// ProcessedMessageRepo 用于消息幂等去重。
type ProcessedMessageRepo struct {
	db *sql.DB
}

// NewProcessedMessageRepo 创建幂等仓库。
func NewProcessedMessageRepo(db *sql.DB) *ProcessedMessageRepo {
	return &ProcessedMessageRepo{db: db}
}

// EnsureTable 确保 processed_messages 表存在。
func (r *ProcessedMessageRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS processed_messages (
			channel_user_id VARCHAR(128) NOT NULL,
			conversation_id BIGINT NOT NULL,
			message_id BIGINT NOT NULL,
			reply TEXT,
			status VARCHAR(32) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (channel_user_id, conversation_id, message_id),
			INDEX idx_updated_at (updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	return err
}

// StartProcessing 尝试将消息标记为 processing。
// 返回 (shouldProcess, previousReply, error)：
//   - 如果是新消息或之前失败/超时，返回 shouldProcess=true。
//   - 如果已经处理完成，返回 shouldProcess=false 和已保存的 reply。
//   - 如果正在处理且未超时，返回 shouldProcess=false 和等待提示。
func (r *ProcessedMessageRepo) StartProcessing(channelUserID string, conversationID, messageID int64, processingTimeout time.Duration) (bool, string, error) {
	now := timeutil.Now()
	_, err := r.db.Exec(`
		INSERT INTO processed_messages(channel_user_id, conversation_id, message_id, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		channelUserID, conversationID, messageID, ProcessedMessageProcessing, now, now)
	if err == nil {
		return true, "", nil
	}

	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		// 非 MySQL 驱动错误（如 sqlmock）直接返回
		return false, "", fmt.Errorf("insert processed message failed: %w", err)
	}

	// 主键冲突：查询已有记录
	row := r.db.QueryRow(`
		SELECT status, reply, updated_at FROM processed_messages
		WHERE channel_user_id = ? AND conversation_id = ? AND message_id = ?`,
		channelUserID, conversationID, messageID)
	var status string
	var reply sql.NullString
	var updatedAt time.Time
	if err := row.Scan(&status, &reply, &updatedAt); err != nil {
		return false, "", fmt.Errorf("scan processed message failed: %w", err)
	}

	switch status {
	case string(ProcessedMessageDone):
		if reply.Valid {
			return false, reply.String, nil
		}
		return false, "", nil
	case string(ProcessedMessageProcessing):
		if now.Sub(updatedAt) <= processingTimeout {
			return false, "消息正在处理中，请稍后再试", nil
		}
		// 处理超时，允许重试，更新为新的 processing 时间
		_, err := r.db.Exec(`
			UPDATE processed_messages SET status = ?, updated_at = ?
			WHERE channel_user_id = ? AND conversation_id = ? AND message_id = ?`,
			ProcessedMessageProcessing, now, channelUserID, conversationID, messageID)
		if err != nil {
			return false, "", fmt.Errorf("update processing message failed: %w", err)
		}
		return true, "", nil
	case string(ProcessedMessageFailed):
		// 失败后允许重试
		_, err := r.db.Exec(`
			UPDATE processed_messages SET status = ?, updated_at = ?
			WHERE channel_user_id = ? AND conversation_id = ? AND message_id = ?`,
			ProcessedMessageProcessing, now, channelUserID, conversationID, messageID)
		if err != nil {
			return false, "", fmt.Errorf("update failed message failed: %w", err)
		}
		return true, "", nil
	default:
		return false, "", fmt.Errorf("unknown processed message status: %s", status)
	}
}

// MarkDone 将消息标记为处理完成并保存回复。
func (r *ProcessedMessageRepo) MarkDone(channelUserID string, conversationID, messageID int64, reply string) error {
	_, err := r.db.Exec(`
		UPDATE processed_messages SET status = ?, reply = ?, updated_at = ?
		WHERE channel_user_id = ? AND conversation_id = ? AND message_id = ?`,
		ProcessedMessageDone, reply, timeutil.Now(), channelUserID, conversationID, messageID)
	return err
}

// MarkFailed 将消息标记为处理失败。
func (r *ProcessedMessageRepo) MarkFailed(channelUserID string, conversationID, messageID int64) error {
	_, err := r.db.Exec(`
		UPDATE processed_messages SET status = ?, updated_at = ?
		WHERE channel_user_id = ? AND conversation_id = ? AND message_id = ?`,
		ProcessedMessageFailed, timeutil.Now(), channelUserID, conversationID, messageID)
	return err
}

// DailyMessageStat 是按日期聚合的处理消息数。
type DailyMessageStat struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// TotalMessages 查询近 N 天处理消息总数。
func (r *ProcessedMessageRepo) TotalMessages(days int) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM processed_messages
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)`,
		days).Scan(&count)
	return count, err
}

// DailyMessageStats 按天统计近 N 天处理消息数。
func (r *ProcessedMessageRepo) DailyMessageStats(days int) ([]DailyMessageStat, error) {
	rows, err := r.db.Query(`
		SELECT DATE(created_at) as date, COUNT(*) as count
		FROM processed_messages
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
		GROUP BY DATE(created_at)
		ORDER BY date ASC`,
		days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]DailyMessageStat, 0)
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
		stats = append(stats, DailyMessageStat{Date: date, Count: count})
	}
	return stats, rows.Err()
}
