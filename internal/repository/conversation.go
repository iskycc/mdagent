package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"miaodi-agent/internal/timeutil"
	"miaodi-agent/pkg/openai"
)

// ConversationRepo 会话数据访问层
type ConversationRepo struct {
	db *sql.DB
}

// NewConversationRepo 创建会话仓库
func NewConversationRepo(db *sql.DB) *ConversationRepo {
	return &ConversationRepo{db: db}
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

// ConversationWithMessages 是会话 ID 与消息的聚合，用于缓存预热。
type ConversationWithMessages struct {
	ChannelUserID  string
	ConversationID int64
	Messages       []StoredChatMessage
}

// ListActiveSince 返回 updated_at 晚于 cutoff 的所有会话及消息。
func (r *ConversationRepo) ListActiveSince(cutoff time.Time) ([]ConversationWithMessages, error) {
	rows, err := r.db.Query(`
		SELECT channel_user_id, conversation_id, messages, updated_at
		FROM agent_conversations
		WHERE updated_at >= ?`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ConversationWithMessages
	for rows.Next() {
		var channelUserID string
		var conversationID int64
		var raw []byte
		var updatedAt time.Time
		if err := rows.Scan(&channelUserID, &conversationID, &raw, &updatedAt); err != nil {
			return nil, err
		}
		stored, err := decodeStoredMessages(raw, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("decode conversation %s/%d failed: %w", channelUserID, conversationID, err)
		}
		result = append(result, ConversationWithMessages{
			ChannelUserID:  channelUserID,
			ConversationID: conversationID,
			Messages:       stored,
		})
	}
	return result, rows.Err()
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
		SELECT messages, updated_at FROM agent_conversations
		WHERE channel_user_id = ? AND conversation_id = ?`, channelUserID, conversationID)
	var raw []byte
	var updatedAt time.Time
	err := row.Scan(&raw, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []openai.ChatMessage{}, nil
		}
		return nil, err
	}
	stored, err := decodeStoredMessages(raw, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("unmarshal messages failed: %w", err)
	}
	return storedToChatMessages(pruneStoredMessages(stored, historyCutoff())), nil
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
	var updatedAt time.Time
	err = tx.QueryRow(`
		SELECT messages, updated_at FROM agent_conversations
		WHERE channel_user_id = ? AND conversation_id = ? FOR UPDATE`,
		channelUserID, conversationID).Scan(&raw, &updatedAt)

	var messages []StoredChatMessage
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// 记录不存在，从头开始
		messages = []StoredChatMessage{}
	} else {
		decoded, err := decodeStoredMessages(raw, updatedAt)
		if err != nil {
			return fmt.Errorf("unmarshal messages failed: %w", err)
		}
		messages = decoded
	}

	messages = append(messages, chatMessagesToStored(msgs, timeutil.Now())...)
	messages = pruneStoredMessages(messages, historyCutoff())
	newRaw, err := json.Marshal(messages)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO agent_conversations(channel_user_id, conversation_id, messages, updated_at)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE messages = VALUES(messages), updated_at = VALUES(updated_at)`,
		channelUserID, conversationID, string(newRaw), timeutil.Now())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// CleanupExpiredMessages 删除 24 小时窗口外的历史消息。
func (r *ConversationRepo) CleanupExpiredMessages(cutoff time.Time) (int, error) {
	rows, err := r.db.Query(`
		SELECT channel_user_id, conversation_id, messages, updated_at
		FROM agent_conversations`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type conversationRow struct {
		channelUserID  string
		conversationID int64
		raw            []byte
		updatedAt      time.Time
	}
	conversations := make([]conversationRow, 0)
	for rows.Next() {
		var row conversationRow
		if err := rows.Scan(&row.channelUserID, &row.conversationID, &row.raw, &row.updatedAt); err != nil {
			return 0, err
		}
		conversations = append(conversations, row)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	removed := 0
	for _, row := range conversations {
		stored, err := decodeStoredMessages(row.raw, row.updatedAt)
		if err != nil {
			return removed, fmt.Errorf("decode conversation %s/%d failed: %w", row.channelUserID, row.conversationID, err)
		}
		pruned := pruneStoredMessages(stored, cutoff)
		if len(pruned) == len(stored) {
			continue
		}
		removed += len(stored) - len(pruned)
		if len(pruned) == 0 {
			if _, err := r.db.Exec(`
				DELETE FROM agent_conversations
				WHERE channel_user_id = ? AND conversation_id = ?`,
				row.channelUserID, row.conversationID); err != nil {
				return removed, err
			}
			continue
		}
		newRaw, err := json.Marshal(pruned)
		if err != nil {
			return removed, err
		}
		if _, err := r.db.Exec(`
			UPDATE agent_conversations
			SET messages = ?, updated_at = ?
			WHERE channel_user_id = ? AND conversation_id = ?`,
			string(newRaw), timeutil.Now(), row.channelUserID, row.conversationID); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// StoredChatMessage 是会话消息的持久化/缓存格式，带创建时间戳。
type StoredChatMessage struct {
	openai.ChatMessage
	CreatedAt time.Time `json:"created_at"`
}

func decodeStoredMessages(raw []byte, fallbackTime time.Time) ([]StoredChatMessage, error) {
	var stored []StoredChatMessage
	if err := json.Unmarshal(raw, &stored); err == nil && storedMessagesHavePayload(stored) {
		for i := range stored {
			if stored[i].CreatedAt.IsZero() {
				stored[i].CreatedAt = fallbackTime
			}
		}
		return stored, nil
	}

	var legacy []openai.ChatMessage
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, err
	}
	return chatMessagesToStored(legacy, fallbackTime), nil
}

func storedMessagesHavePayload(messages []StoredChatMessage) bool {
	if len(messages) == 0 {
		return true
	}
	for _, msg := range messages {
		if msg.Role != "" || msg.Content != "" || len(msg.ToolCalls) > 0 || msg.ToolCallID != "" || msg.Name != "" {
			return true
		}
	}
	return false
}

// ChatMessageToStored 把单条 ChatMessage 转为 StoredChatMessage。
func ChatMessageToStored(msg openai.ChatMessage, createdAt time.Time) StoredChatMessage {
	return StoredChatMessage{
		ChatMessage: msg,
		CreatedAt:   createdAt.In(timeutil.BeijingLocation()),
	}
}

// ChatMessagesToStored 把 ChatMessage 切片转为 StoredChatMessage 切片。
func ChatMessagesToStored(messages []openai.ChatMessage, createdAt time.Time) []StoredChatMessage {
	stored := make([]StoredChatMessage, 0, len(messages))
	for _, msg := range messages {
		stored = append(stored, ChatMessageToStored(msg, createdAt))
	}
	return stored
}

// StoredToChatMessages 把 StoredChatMessage 切片转回 ChatMessage 切片。
func StoredToChatMessages(stored []StoredChatMessage) []openai.ChatMessage {
	messages := make([]openai.ChatMessage, 0, len(stored))
	for _, msg := range stored {
		messages = append(messages, msg.ChatMessage)
	}
	return messages
}

func chatMessagesToStored(messages []openai.ChatMessage, createdAt time.Time) []StoredChatMessage {
	return ChatMessagesToStored(messages, createdAt)
}

func storedToChatMessages(stored []StoredChatMessage) []openai.ChatMessage {
	return StoredToChatMessages(stored)
}

func pruneStoredMessages(messages []StoredChatMessage, cutoff time.Time) []StoredChatMessage {
	cutoff = cutoff.In(timeutil.BeijingLocation())
	pruned := make([]StoredChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.CreatedAt.IsZero() || !msg.CreatedAt.Before(cutoff) {
			pruned = append(pruned, msg)
		}
	}
	return pruned
}

func historyCutoff() time.Time {
	return timeutil.Now().Add(-24 * time.Hour)
}
