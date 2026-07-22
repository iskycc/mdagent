package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"miaodi-agent/internal/timeutil"
)

// DeadLetterRepo 持久化队列最终失败任务的死信存储。
type DeadLetterRepo struct {
	db *sql.DB
}

// NewDeadLetterRepo 创建死信仓库。
func NewDeadLetterRepo(db *sql.DB) *DeadLetterRepo {
	return &DeadLetterRepo{db: db}
}

// EnsureTable 确保 persist_dead_letters 表存在。
func (r *DeadLetterRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS persist_dead_letters (
			id INT AUTO_INCREMENT PRIMARY KEY,
			kind VARCHAR(32) NOT NULL COMMENT '任务类型：conv/log',
			payload JSON NOT NULL COMMENT '任务原始 payload',
			last_error TEXT COMMENT '最终失败错误信息',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_kind_created (kind, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	return err
}

// RecordConv 记录一条会话持久化失败的死信。
func (r *DeadLetterRepo) RecordConv(channelUserID string, conversationID int64, msgs []StoredChatMessage, lastErr error) error {
	payload := map[string]interface{}{
		"channel_user_id": channelUserID,
		"conversation_id": conversationID,
		"messages":        msgs,
	}
	return r.insert("conv", payload, lastErr)
}

// RecordLog 记录一条调用日志持久化失败的死信。
func (r *DeadLetterRepo) RecordLog(channelUserID, apikey, channel, action string, lastErr error) error {
	payload := map[string]interface{}{
		"channel_user_id": channelUserID,
		"apikey":          apikey,
		"channel":         channel,
		"action":          action,
	}
	return r.insert("log", payload, lastErr)
}

func (r *DeadLetterRepo) insert(kind string, payload map[string]interface{}, lastErr error) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal dead letter payload failed: %w", err)
	}
	errMsg := ""
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	_, err = r.db.Exec(`
		INSERT INTO persist_dead_letters(kind, payload, last_error, created_at)
		VALUES (?, ?, ?, ?)`,
		kind, string(data), errMsg, timeutil.Now())
	return err
}
