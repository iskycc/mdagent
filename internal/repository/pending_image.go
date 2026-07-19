package repository

import (
	"database/sql"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/timeutil"
)

// PendingImageRepo 待上传图片数据访问层
type PendingImageRepo struct {
	db *sql.DB
}

// NewPendingImageRepo 创建仓库
func NewPendingImageRepo(db *sql.DB) *PendingImageRepo {
	return &PendingImageRepo{db: db}
}

// EnsureTable 确保 pending_images 表存在
func (r *PendingImageRepo) EnsureTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_images (
			id INT AUTO_INCREMENT PRIMARY KEY,
			apikey VARCHAR(128) NOT NULL,
			image_url VARCHAR(512) NOT NULL,
			book VARCHAR(64) DEFAULT '传送鸽',
			chara VARCHAR(64) DEFAULT '喵滴鸽',
			title VARCHAR(128) DEFAULT '',
			status VARCHAR(32) DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_status_created (status, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`)
	return err
}

// Insert 插入一条待上传图片记录
func (r *PendingImageRepo) Insert(apikey, imageURL, book, chara, title string) error {
	_, err := r.db.Exec(`
		INSERT INTO pending_images(apikey, image_url, book, chara, title, status, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?)`,
		apikey, imageURL, book, chara, title, timeutil.Now())
	return err
}

// ListPending 查询待处理图片（外部定时任务调用）
func (r *PendingImageRepo) ListPending(limit int) ([]model.PendingImage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(`
		SELECT id, apikey, image_url, book, chara, title, status, created_at
		FROM pending_images WHERE status = 'pending' ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]model.PendingImage, 0)
	for rows.Next() {
		var img model.PendingImage
		if err := rows.Scan(&img.ID, &img.APIKey, &img.ImageURL, &img.Book, &img.Chara, &img.Title, &img.Status, &img.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, img)
	}
	return list, rows.Err()
}

// UpdateStatus 更新图片处理状态
func (r *PendingImageRepo) UpdateStatus(id int64, status string) error {
	_, err := r.db.Exec(`UPDATE pending_images SET status = ? WHERE id = ?`, status, id)
	return err
}
