package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"miaodi-agent/internal/model"
)

// UserRepo 用户数据访问层
type UserRepo struct {
	db *sql.DB
}

// NewUserRepo 创建用户仓库
func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

// EnsureTable 确保 agent_users 表存在
func (r *UserRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_users (
			channel_user_id VARCHAR(128) PRIMARY KEY,
			apikey VARCHAR(128) DEFAULT '',
			status VARCHAR(16) DEFAULT 'unbound',
			book VARCHAR(64) DEFAULT '传送鸽',
			chara VARCHAR(64) DEFAULT '喵滴鸽',
			title VARCHAR(128) DEFAULT '',
			email VARCHAR(128) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	if err != nil {
		return err
	}
	if err := r.ensureEmailColumn(); err != nil {
		return err
	}
	return r.migrateLegacyDefaults()
}

// GetOrCreate 根据 channel_user_id 查询用户，不存在则创建（并发安全）
func (r *UserRepo) GetOrCreate(channelUserID string) (*model.User, error) {
	user, err := r.Get(channelUserID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// INSERT IGNORE 可容忍并发同时插入导致的重复主键
	_, err = r.db.Exec(`
		INSERT IGNORE INTO agent_users(channel_user_id, apikey, status, book, chara, title, email)
		VALUES (?, '', 'unbound', ?, ?, '', '')`, channelUserID, DefaultBook, DefaultChara)
	if err != nil {
		return nil, fmt.Errorf("create user failed: %w", err)
	}
	return r.Get(channelUserID)
}

// Get 查询用户
func (r *UserRepo) Get(channelUserID string) (*model.User, error) {
	row := r.db.QueryRow(`
		SELECT channel_user_id, apikey, status, book, chara, title, email
		FROM agent_users WHERE channel_user_id = ?`, channelUserID)
	user := &model.User{}
	err := row.Scan(&user.ChannelUserID, &user.APIKey, &user.Status,
		&user.Book, &user.Chara, &user.Title, &user.Email)
	return user, err
}

// CountTotal 查询总用户数
func (r *UserRepo) CountTotal() (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM agent_users").Scan(&count)
	return count, err
}

// CountByStatus 按状态统计用户数
func (r *UserRepo) CountByStatus(status string) (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM agent_users WHERE status = ?", status).Scan(&count)
	return count, err
}

// UpdateField 更新指定字段
func (r *UserRepo) UpdateField(channelUserID, field, value string) error {
	allowed := map[string]bool{
		"apikey": true,
		"status": true,
		"book":   true,
		"chara":  true,
		"title":  true,
		"email":  true,
	}
	if !allowed[field] {
		return fmt.Errorf("invalid field: %s", field)
	}
	query := fmt.Sprintf("UPDATE agent_users SET %s = ? WHERE channel_user_id = ?", field)
	_, err := r.db.Exec(query, value, channelUserID)
	return err
}

// UpdateAPIKeyAndStatus 同时更新 key 和状态
func (r *UserRepo) UpdateAPIKeyAndStatus(channelUserID, apikey, status string) error {
	_, err := r.db.Exec(`
		UPDATE agent_users SET apikey = ?, status = ? WHERE channel_user_id = ?`,
		apikey, status, channelUserID)
	return err
}

// UpdateEmailAndStatus 更新待验证邮箱和绑定状态。
func (r *UserRepo) UpdateEmailAndStatus(channelUserID, email, status string) error {
	_, err := r.db.Exec(`
		UPDATE agent_users SET email = ?, status = ? WHERE channel_user_id = ?`,
		email, status, channelUserID)
	return err
}

// ClearBinding 清除当前用户绑定信息。
func (r *UserRepo) ClearBinding(channelUserID string) error {
	_, err := r.db.Exec(`
		UPDATE agent_users SET apikey = '', email = '', status = 'unbound' WHERE channel_user_id = ?`,
		channelUserID)
	return err
}

// UpdateSavePath 更新保存路径
func (r *UserRepo) UpdateSavePath(channelUserID, book, chara, title string) error {
	_, err := r.db.Exec(`
		UPDATE agent_users SET book = ?, chara = ?, title = ? WHERE channel_user_id = ?`,
		book, chara, title, channelUserID)
	return err
}

func (r *UserRepo) migrateLegacyDefaults() error {
	_, err := r.db.Exec(`
		UPDATE agent_users
		SET book = ?, chara = ?
		WHERE book = '默认' AND chara = '微信' AND title = ''`,
		DefaultBook, DefaultChara)
	return err
}

func (r *UserRepo) ensureEmailColumn() error {
	_, err := r.db.Exec(`ALTER TABLE agent_users ADD COLUMN email VARCHAR(128) DEFAULT ''`)
	if err == nil {
		return nil
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1060 {
		return nil
	}
	return err
}
