package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"miaodi-agent/pkg/openai"
)

// ConversationRepo 会话数据访问层
type ConversationRepo struct {
	db     *sql.DB
	maxLen int
}

// NewConversationRepo 创建会话仓库
func NewConversationRepo(db *sql.DB) *ConversationRepo {
	return &ConversationRepo{db: db, maxLen: 20}
}

// EnsureTable 确保 agent_conversations 表存在
func (r *ConversationRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_conversations (
			channel_user_id VARCHAR(128) NOT NULL,
			conversation_id BIGINT NOT NULL,
			messages JSON NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (channel_user_id, conversation_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	return err
}

// CountTotal 查询总会话数
func (r *ConversationRepo) CountTotal() (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM agent_conversations").Scan(&count)
	return count, err
}

// Clear 删除指定会话记录
func (r *ConversationRepo) Clear(channelUserID string, conversationID int64) error {
	_, err := r.db.Exec(`
		DELETE FROM agent_conversations
		WHERE channel_user_id = ? AND conversation_id = ?`,
		channelUserID, conversationID)
	return err
}

// GetMessages 获取会话历史（不加锁，用于读取）
func (r *ConversationRepo) GetMessages(channelUserID string, conversationID int64) ([]openai.ChatMessage, error) {
	row := r.db.QueryRow(`
		SELECT messages FROM agent_conversations
		WHERE channel_user_id = ? AND conversation_id = ?`, channelUserID, conversationID)
	var raw []byte
	err := row.Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []openai.ChatMessage{}, nil
		}
		return nil, err
	}
	var messages []openai.ChatMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, fmt.Errorf("unmarshal messages failed: %w", err)
	}
	return messages, nil
}

// AppendMessage 追加一条消息（原子、并发安全）
func (r *ConversationRepo) AppendMessage(channelUserID string, conversationID int64, msg openai.ChatMessage) error {
	return r.AppendMessages(channelUserID, conversationID, msg)
}

// AppendMessages 追加多条消息（原子、并发安全）
// 使用事务 + SELECT FOR UPDATE 对同一会话串行化，避免并发写丢失。
func (r *ConversationRepo) AppendMessages(channelUserID string, conversationID int64, msgs ...openai.ChatMessage) error {
	const maxRetries = 3
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		err := r.appendMessagesTx(channelUserID, conversationID, msgs)
		if err == nil {
			return nil
		}
		lastErr = err
		// 如果是主键冲突（两个并发同时 insert 同一会话），重试一次即可读到已提交记录。
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			continue
		}
		return err
	}
	return fmt.Errorf("append messages failed after %d retries: %w", maxRetries, lastErr)
}

func (r *ConversationRepo) appendMessagesTx(channelUserID string, conversationID int64, msgs []openai.ChatMessage) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var raw []byte
	err = tx.QueryRow(`
		SELECT messages FROM agent_conversations
		WHERE channel_user_id = ? AND conversation_id = ? FOR UPDATE`,
		channelUserID, conversationID).Scan(&raw)

	var messages []openai.ChatMessage
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// 记录不存在，从头开始
		messages = []openai.ChatMessage{}
	} else {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return fmt.Errorf("unmarshal messages failed: %w", err)
		}
	}

	messages = append(messages, msgs...)
	if len(messages) > r.maxLen {
		messages = messages[len(messages)-r.maxLen:]
	}
	newRaw, err := json.Marshal(messages)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO agent_conversations(channel_user_id, conversation_id, messages, updated_at)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE messages = VALUES(messages), updated_at = VALUES(updated_at)`,
		channelUserID, conversationID, newRaw, time.Now())
	if err != nil {
		return err
	}
	return tx.Commit()
}
