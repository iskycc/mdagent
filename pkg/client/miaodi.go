package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"time"

	"miaodi-agent/internal/metrics"
)

// multipartWriter abstracts *multipart.Writer so tests can inject failures.
type multipartWriter interface {
	WriteField(fieldname, value string) error
	CreateFormFile(fieldname, filename string) (io.Writer, error)
	Close() error
	FormDataContentType() string
}

// newMultipartWriter is swappable in tests to exercise UpImage error paths.
var newMultipartWriter = func(w io.Writer) multipartWriter { return multipart.NewWriter(w) }

const (
	defaultAPIBaseURL = "https://api.libv.cc/miaodi"
	defaultMailURL    = "https://api.miaodiapp.com/api/newmail.php"
	defaultPictureURL = "https://picture.miaodiapp.com/api/upload"
)

// MiaodiClient 喵滴 API 客户端
type MiaodiClient struct {
	client     *http.Client
	apiBaseURL string
	mailURL    string
	pictureURL string
}

// NewMiaodiClient 创建喵滴客户端，使用默认 endpoint。
func NewMiaodiClient() *MiaodiClient {
	return NewMiaodiClientWithEndpoints(defaultAPIBaseURL, defaultMailURL, defaultPictureURL)
}

// NewMiaodiClientWithEndpoints 使用自定义 endpoint 创建喵滴客户端。
func NewMiaodiClientWithEndpoints(apiBaseURL, mailURL, pictureURL string) *MiaodiClient {
	return &MiaodiClient{
		client:     &http.Client{Timeout: 10 * time.Second},
		apiBaseURL: apiBaseURL,
		mailURL:    mailURL,
		pictureURL: pictureURL,
	}
}

// NewMiaodiClientWithHTTP 使用自定义 http.Client 创建喵滴客户端（主要用于测试）
func NewMiaodiClientWithHTTP(httpClient *http.Client) *MiaodiClient {
	return NewMiaodiClientWithHTTPAndEndpoints(httpClient, defaultAPIBaseURL, defaultMailURL, defaultPictureURL)
}

// NewMiaodiClientWithHTTPAndEndpoints 使用自定义 http.Client 和自定义 endpoint 创建喵滴客户端。
func NewMiaodiClientWithHTTPAndEndpoints(httpClient *http.Client, apiBaseURL, mailURL, pictureURL string) *MiaodiClient {
	return &MiaodiClient{
		client:     httpClient,
		apiBaseURL: apiBaseURL,
		mailURL:    mailURL,
		pictureURL: pictureURL,
	}
}

// Check 校验 API Key 是否有效
func (m *MiaodiClient) Check(key string) bool {
	span := metrics.Start("miaodi_check")
	info, err := m.GetInfo(key)
	if err != nil {
		span.Finish(false)
		return false
	}
	if code, ok := info["code"].(float64); ok && code == 20000 {
		span.Finish(true)
		return true
	}
	span.Finish(false)
	return false
}

// SendEmail 发送邮箱验证码
func (m *MiaodiClient) SendEmail(email string) (map[string]interface{}, error) {
	span := metrics.Start("miaodi_send_email")
	form := url.Values{}
	form.Add("email", email)
	res, err := m.postForm(m.apiBaseURL+"/user/open/emailVerify", form)
	span.Finish(err == nil)
	return res, err
}

// GetKey 通过邮箱和验证码获取 API Key
func (m *MiaodiClient) GetKey(email, code string) (map[string]interface{}, error) {
	span := metrics.Start("miaodi_get_key")
	form := url.Values{}
	form.Add("email", email)
	form.Add("code", code)
	res, err := m.postForm(m.mailURL, form)
	span.Finish(err == nil)
	return res, err
}

// GetInfo 获取用户信息
func (m *MiaodiClient) GetInfo(key string) (map[string]interface{}, error) {
	span := metrics.Start("miaodi_get_info")
	form := url.Values{}
	form.Add("key", key)
	res, err := m.postForm(m.apiBaseURL+"/user/open/info", form)
	span.Finish(err == nil)
	return res, err
}

// PutText 保存文本笔记
func (m *MiaodiClient) PutText(key, book, chapter, title, content string) (map[string]interface{}, error) {
	span := metrics.Start("miaodi_put_text")
	form := url.Values{}
	form.Add("key", key)
	form.Add("book", book)
	form.Add("chapter", chapter)
	form.Add("title", title)
	form.Add("content", content)
	form.Add("mode", "add")
	res, err := m.postForm(m.apiBaseURL+"/page/open/add", form)
	span.Finish(err == nil)
	return res, err
}

// UpImage 上传图片
func (m *MiaodiClient) UpImage(token, filePath string) (map[string]interface{}, error) {
	span := metrics.Start("miaodi_up_image")
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		span.Finish(false)
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	body := &bytes.Buffer{}
	writer := newMultipartWriter(body)
	_ = writer.WriteField("token", token)

	part, err := writer.CreateFormFile("image", fmt.Sprintf("%d.jpg", time.Now().Unix()))
	if err != nil {
		span.Finish(false)
		return nil, fmt.Errorf("创建文件字段失败: %w", err)
	}
	if _, err = part.Write(fileContent); err != nil {
		span.Finish(false)
		return nil, fmt.Errorf("写入文件内容失败: %w", err)
	}
	if err = writer.Close(); err != nil {
		span.Finish(false)
		return nil, fmt.Errorf("关闭写入器失败: %w", err)
	}

	req, err := http.NewRequest("POST", m.pictureURL, body)
	if err != nil {
		span.Finish(false)
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := m.client.Do(req)
	if err != nil {
		span.Finish(false)
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	res, err := decodeResponse(resp)
	span.Finish(err == nil)
	return res, err
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
