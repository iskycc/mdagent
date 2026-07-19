package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"time"
)

// MiaodiClient 喵滴 API 客户端
type MiaodiClient struct {
	client *http.Client
}

// NewMiaodiClient 创建喵滴客户端
func NewMiaodiClient() *MiaodiClient {
	return &MiaodiClient{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewMiaodiClientWithHTTP 使用自定义 http.Client 创建喵滴客户端（主要用于测试）
func NewMiaodiClientWithHTTP(httpClient *http.Client) *MiaodiClient {
	return &MiaodiClient{client: httpClient}
}

// Check 校验 API Key 是否有效
func (m *MiaodiClient) Check(key string) bool {
	info, err := m.GetInfo(key)
	if err != nil {
		return false
	}
	if code, ok := info["code"].(float64); ok && code == 20000 {
		return true
	}
	return false
}

// SendEmail 发送邮箱验证码
func (m *MiaodiClient) SendEmail(email string) (map[string]interface{}, error) {
	form := url.Values{}
	form.Add("email", email)
	return m.postForm("https://api.libv.cc/miaodi/user/open/emailVerify", form)
}

// GetKey 通过邮箱和验证码获取 API Key
func (m *MiaodiClient) GetKey(email, code string) (map[string]interface{}, error) {
	form := url.Values{}
	form.Add("email", email)
	form.Add("code", code)
	return m.postForm("https://api.miaodiapp.com/api/newmail.php", form)
}

// GetInfo 获取用户信息
func (m *MiaodiClient) GetInfo(key string) (map[string]interface{}, error) {
	form := url.Values{}
	form.Add("key", key)
	return m.postForm("https://api.libv.cc/miaodi/user/open/info", form)
}

// PutText 保存文本笔记
func (m *MiaodiClient) PutText(key, book, chapter, title, content string) (map[string]interface{}, error) {
	form := url.Values{}
	form.Add("key", key)
	form.Add("book", book)
	form.Add("chapter", chapter)
	form.Add("title", title)
	form.Add("content", content)
	form.Add("mode", "add")
	return m.postForm("https://api.libv.cc/miaodi/page/open/add", form)
}

// UpImage 上传图片
func (m *MiaodiClient) UpImage(token, filePath string) (map[string]interface{}, error) {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("token", token)

	part, err := writer.CreateFormFile("image", fmt.Sprintf("%d.jpg", time.Now().Unix()))
	if err != nil {
		return nil, fmt.Errorf("创建文件字段失败: %w", err)
	}
	if _, err = part.Write(fileContent); err != nil {
		return nil, fmt.Errorf("写入文件内容失败: %w", err)
	}
	if err = writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭写入器失败: %w", err)
	}

	req, err := http.NewRequest("POST", "https://picture.miaodiapp.com/api/upload", body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	return decodeResponse(resp)
}

func (m *MiaodiClient) postForm(url string, form url.Values) (map[string]interface{}, error) {
	resp, err := m.client.PostForm(url, form)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	return decodeResponse(resp)
}

func decodeResponse(resp *http.Response) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}
	return result, nil
}
