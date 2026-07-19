package model

import "time"

// CallbackPayload 传送鸽回调请求体
type CallbackPayload struct {
	EventType    string `json:"eventType"`
	Bot          Bot    `json:"bot"`
	Conversation struct {
		ID int64 `json:"id"`
	} `json:"conversation"`
	User struct {
		UserID   string `json:"userId"`
		Username string `json:"username"`
	} `json:"user"`
	Message struct {
		ID         int64  `json:"id"`
		Content    string `json:"content"`
		CreateTime string `json:"createTime"`
	} `json:"message"`
}

// Bot 机器人信息
type Bot struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// CallbackResponse 传送鸽回调响应体
type CallbackResponse struct {
	Success bool `json:"success"`
	Reply   struct {
		Content string `json:"content"`
	} `json:"reply"`
}

// NewSuccessResponse 构造成功响应
func NewSuccessResponse(content string) CallbackResponse {
	return CallbackResponse{
		Success: true,
		Reply: struct {
			Content string `json:"content"`
		}{Content: content},
	}
}

// User 用户在 Agent 中的状态
type User struct {
	ChannelUserID string
	APIKey        string
	Status        string // unbound / bound
	Book          string
	Chara         string
	Title         string
}

// Conversation 会话记录
type Conversation struct {
	ChannelUserID  string
	ConversationID int64
	Messages       []byte // JSON 数组
	UpdatedAt      time.Time
}

// PendingImage 待上传图片记录
type PendingImage struct {
	ID        int64
	APIKey    string
	ImageURL  string
	Book      string
	Chara     string
	Title     string
	Status    string
	CreatedAt time.Time
}
