package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

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
	Message CallbackMessage `json:"message"`
}

// Bot 机器人信息
type Bot struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// CallbackMessage 传送鸽消息体。
type CallbackMessage struct {
	ID         int64      `json:"id"`
	Content    string     `json:"content"`
	CreateTime JSONString `json:"createTime"`
}

// JSONString 兼容 JSON 字符串和数字，传送鸽 createTime 真机环境可能是毫秒时间戳。
type JSONString string

// UnmarshalJSON 允许字符串、数字和 null 解码为字符串。
func (s *JSONString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = JSONString(text)
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*s = JSONString(number.String())
		return nil
	}

	return fmt.Errorf("JSONString must be string or number: %s", string(data))
}

// MarshalJSON 按字符串输出，避免响应或测试序列化时改变类型。
func (s JSONString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

func (s JSONString) String() string {
	return string(s)
}

// UnixMilli 尝试把毫秒时间戳格式的字符串转为 time.Time。
func (s JSONString) UnixMilli() (time.Time, bool) {
	n, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(n), true
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

// NewErrorResponse 构造失败响应
func NewErrorResponse(content string) CallbackResponse {
	return CallbackResponse{
		Success: false,
		Reply: struct {
			Content string `json:"content"`
		}{Content: content},
	}
}

// User 用户在 Agent 中的状态
type User struct {
	ChannelUserID string
	APIKey        string
	Status        string // unbound / waiting_email_code / bound
	Book          string
	Chara         string
	Title         string
	Email         string
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
