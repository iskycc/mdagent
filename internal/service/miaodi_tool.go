package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"miaodi-agent/internal/debuglog"
	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
	"miaodi-agent/internal/timeutil"
	"miaodi-agent/pkg/openai"
)

const (
	userStatusUnbound          = "unbound"
	userStatusWaitingEmailCode = "waiting_email_code"
	userStatusBound            = "bound"
)

var validEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// ToolExecutor 工具执行器
type ToolExecutor struct {
	miaodi      MiaodiClient
	userRepo    *repository.UserRepo
	convRepo    ConversationStore
	pendingRepo *repository.PendingImageRepo
	callLogRepo *repository.CallLogRepo
}

// NewToolExecutor 创建工具执行器
func NewToolExecutor(miaodi MiaodiClient, userRepo *repository.UserRepo, convRepo ConversationStore, pendingRepo *repository.PendingImageRepo, callLogRepo *repository.CallLogRepo) *ToolExecutor {
	return &ToolExecutor{
		miaodi:      miaodi,
		userRepo:    userRepo,
		convRepo:    convRepo,
		pendingRepo: pendingRepo,
		callLogRepo: callLogRepo,
	}
}

// ToolDefinitions 返回模型可见的工具列表
func ToolDefinitions() []openai.ToolDefinition {
	tools := []openai.ToolDefinition{
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "bind_miaodi_key",
				Description: "绑定喵滴 API Key。当用户想绑定喵滴账号或提供了一串类似 key 的字符串时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"key": map[string]interface{}{
							"type":        "string",
							"description": "喵滴 API Key",
						},
					},
					"required": []string{"key"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "send_miaodi_email_code",
				Description: "向用户的喵滴注册邮箱发送验证码。当用户想用邮箱绑定喵滴账号、提供邮箱获取验证码时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"email": map[string]interface{}{
							"type":        "string",
							"description": "喵滴账号邮箱",
						},
					},
					"required": []string{"email"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "bind_miaodi_by_email_code",
				Description: "使用邮箱验证码换取喵滴 API Key 并完成绑定。当用户已收到验证码并提供验证码时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"email": map[string]interface{}{
							"type":        "string",
							"description": "可选，喵滴账号邮箱；为空时使用上次发送验证码的邮箱",
						},
						"code": map[string]interface{}{
							"type":        "string",
							"description": "邮箱验证码",
						},
					},
					"required": []string{"code"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "set_save_path",
				Description: "设置后续保存笔记的位置，包括书本、章节、标题。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"book": map[string]interface{}{
							"type":        "string",
							"description": "书本名称",
						},
						"chapter": map[string]interface{}{
							"type":        "string",
							"description": "章节名称",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "笔记标题，为空时使用当天日期作为标题",
						},
					},
					"required": []string{"book", "chapter"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "get_user_profile",
				Description: "获取当前用户的绑定状态和保存路径。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "get_miaodi_key",
				Description: "查看当前用户已绑定的喵滴 API Key。只有用户明确要求获取当前绑定 key 时调用。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "get_miaodi_annual_report",
				Description: "获取喵滴年度报告链接。当用户询问年度报告、报告地址时调用。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "unbind_miaodi_key",
				Description: "解除当前用户绑定的喵滴 API Key。当用户明确说解除绑定、解绑喵滴账号时调用。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "save_text_note",
				Description: "把文本内容保存到喵滴笔记。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"content": map[string]interface{}{
							"type":        "string",
							"description": "要保存的文本内容",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "可选标题，覆盖当前设置的标题",
						},
					},
					"required": []string{"content"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "save_image_note",
				Description: "把图片链接记录到待上传队列，等待定时任务扫描后上传到喵滴。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"image_url": map[string]interface{}{
							"type":        "string",
							"description": "图片 URL",
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "可选标题",
						},
					},
					"required": []string{"image_url"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "reset_conversation",
				Description: "清空当前会话历史。当用户想重置对话、清空上下文、忘记刚才的对话时调用。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "show_help",
				Description: "返回 Bot 的能力说明。当用户问你能做什么、怎么用、帮助等时调用。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "list_recent_notes",
				Description: "列出用户最近保存的笔记摘要。当用户问最近保存了什么、最近的操作记录等时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "返回条数，默认 5，最大 20",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "query_notes_by_date",
				Description: "按日期查询用户保存的笔记。当用户问某一天的笔记、昨天的记录等时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"date": map[string]interface{}{
							"type":        "string",
							"description": "日期，格式 YYYY-MM-DD",
						},
					},
					"required": []string{"date"},
				},
			},
		},
	}
	return append(tools, commonToolDefinitions()...)
}

// Execute 根据工具名和参数执行，返回给模型看的结果字符串
func (e *ToolExecutor) Execute(user *model.User, channelUserID string, conversationID int64, name string, arguments string) string {
	debuglog.Printf("tool execute start user=%s conversation=%d name=%s arguments=%s", channelUserID, conversationID, name, arguments)
	var result string
	switch name {
	case "bind_miaodi_key":
		result = e.bindMiaodiKey(user, channelUserID, arguments)
	case "send_miaodi_email_code":
		result = e.sendMiaodiEmailCode(user, channelUserID, arguments)
	case "bind_miaodi_by_email_code":
		result = e.bindMiaodiByEmailCode(user, channelUserID, arguments)
	case "set_save_path":
		result = e.setSavePath(user, channelUserID, arguments)
	case "get_user_profile":
		result = e.getUserProfile(user)
	case "get_miaodi_key":
		result = e.getMiaodiKey(user)
	case "get_miaodi_annual_report":
		result = e.getMiaodiAnnualReport(user, channelUserID)
	case "unbind_miaodi_key":
		result = e.unbindMiaodiKey(user, channelUserID)
	case "save_text_note":
		result = e.saveTextNote(user, channelUserID, arguments)
	case "save_image_note":
		result = e.saveImageNote(user, channelUserID, arguments)
	case "reset_conversation":
		result = e.resetConversation(user, channelUserID, conversationID, arguments)
	case "show_help":
		result = e.showHelp()
	case "list_recent_notes":
		result = e.listRecentNotes(channelUserID, arguments)
	case "query_notes_by_date":
		result = e.queryNotesByDate(channelUserID, arguments)
	case "get_current_time":
		result = e.getCurrentTime(arguments)
	case "calculate":
		result = e.calculate(arguments)
	case "date_calculate":
		result = e.dateCalculate(arguments)
	case "random_number":
		result = e.randomNumber(arguments)
	case "choose_option":
		result = e.chooseOption(arguments)
	case "text_stats":
		result = e.textStats(arguments)
	case "count_tokens":
		result = e.countTokens(arguments)
	default:
		result = fmt.Sprintf("未知工具: %s", name)
	}
	debuglog.Printf("tool execute result user=%s conversation=%d name=%s result=%q", channelUserID, conversationID, name, result)
	return result
}

func (e *ToolExecutor) bindMiaodiKey(user *model.User, channelUserID, arguments string) string {
	var args struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	if args.Key == "" {
		return "key 不能为空"
	}
	if !e.miaodi.Check(args.Key) {
		return "Key 校验失败，请检查是否正确"
	}
	if err := e.userRepo.UpdateAPIKeyAndStatus(channelUserID, args.Key, userStatusBound); err != nil {
		return "绑定失败：数据库错误"
	}
	user.APIKey = args.Key
	user.Status = userStatusBound
	e.recordCall(channelUserID, args.Key, "bind_key")
	return "绑定成功，你现在可以保存笔记和图片了"
}

func (e *ToolExecutor) sendMiaodiEmailCode(user *model.User, channelUserID, arguments string) string {
	var args struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	args.Email = strings.TrimSpace(args.Email)
	if !isValidEmail(args.Email) {
		return "邮箱格式不正确，请检查后重试"
	}
	res, err := e.miaodi.SendEmail(args.Email)
	if err != nil {
		e.recordCall(channelUserID, "", "send_email_failed")
		return fmt.Sprintf("邮件发送失败：%v", err)
	}
	if !isMiaodiSuccess(res) {
		e.recordCall(channelUserID, "", "send_email_failed")
		return fmt.Sprintf("邮件发送失败：%s", miaodiMessage(res))
	}
	if err := e.userRepo.UpdateEmailAndStatus(channelUserID, args.Email, userStatusWaitingEmailCode); err != nil {
		debuglog.Printf("send email state update failed user=%s email=%s status=%s error=%v", channelUserID, args.Email, userStatusWaitingEmailCode, err)
		return "邮件已发送，但保存绑定状态失败，请稍后重试"
	}
	user.Email = args.Email
	user.Status = userStatusWaitingEmailCode
	e.recordCall(channelUserID, "", "send_email")
	return "邮件已发送，请检查收件箱并回复收到的验证码"
}

func (e *ToolExecutor) bindMiaodiByEmailCode(user *model.User, channelUserID, arguments string) string {
	var args struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	email := strings.TrimSpace(args.Email)
	if email == "" {
		email = strings.TrimSpace(user.Email)
	}
	if email == "" {
		return "请先提供邮箱获取验证码"
	}
	code := strings.TrimSpace(args.Code)
	if code == "" {
		return "验证码不能为空"
	}

	res, err := e.miaodi.GetKey(email, code)
	if err != nil {
		e.recordCall(channelUserID, "", "get_key_failed")
		return fmt.Sprintf("绑定失败：%v", err)
	}
	if !isMiaodiSuccess(res) {
		debuglog.Printf("get key failed user=%s email=%s code=%s response=%v", channelUserID, email, code, res)
		e.recordCall(channelUserID, "", "get_key_failed")
		return fmt.Sprintf("邮箱或验证码不正确：%s", miaodiMessage(res))
	}
	key, ok := res["key"].(string)
	if !ok || key == "" {
		e.recordCall(channelUserID, "", "get_key_failed")
		return "绑定失败：喵滴没有返回 API Key"
	}
	if err := e.userRepo.UpdateAPIKeyAndStatus(channelUserID, key, userStatusBound); err != nil {
		return "绑定失败：数据库错误"
	}
	user.APIKey = key
	user.Email = email
	user.Status = userStatusBound
	e.recordCall(channelUserID, key, "get_key")
	return "绑定成功，你现在可以保存笔记和图片了"
}

func (e *ToolExecutor) setSavePath(user *model.User, channelUserID, arguments string) string {
	var args struct {
		Book    string `json:"book"`
		Chapter string `json:"chapter"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	if args.Book == "" || args.Chapter == "" {
		return "book 和 chapter 不能为空"
	}
	if err := e.userRepo.UpdateSavePath(channelUserID, args.Book, args.Chapter, args.Title); err != nil {
		return "设置失败：数据库错误"
	}
	user.Book = args.Book
	user.Chara = args.Chapter
	user.Title = args.Title
	return fmt.Sprintf("保存路径已设置：书本《%s》/ 章节《%s》/ 标题《%s》", args.Book, args.Chapter, displayTitle(args.Title))
}

func (e *ToolExecutor) getUserProfile(user *model.User) string {
	masked := "未绑定"
	if user.Status == userStatusBound && user.APIKey != "" {
		masked = maskKey(user.APIKey)
	}
	email := user.Email
	if email == "" {
		email = "未记录"
	}
	return fmt.Sprintf("绑定状态：%s\n喵滴 Key：%s\n绑定邮箱：%s\n保存路径：书本《%s》/ 章节《%s》/ 标题《%s》",
		user.Status, masked, email, user.Book, user.Chara, displayTitle(user.Title))
}

func (e *ToolExecutor) getMiaodiKey(user *model.User) string {
	if user.Status != userStatusBound || user.APIKey == "" {
		return "尚未绑定喵滴 Key"
	}
	return user.APIKey
}

func (e *ToolExecutor) getMiaodiAnnualReport(user *model.User, channelUserID string) string {
	if user.Status != userStatusBound || user.APIKey == "" {
		return "尚未绑定喵滴 Key，请先绑定"
	}
	if !e.miaodi.Check(user.APIKey) {
		return "你的喵滴 API Key 已失效，请重新绑定"
	}
	e.recordCall(channelUserID, user.APIKey, "annual_report")
	return fmt.Sprintf("你的喵滴年度报告地址：https://api.libv.cc/miaodi/report/page?key=%s", user.APIKey)
}

func (e *ToolExecutor) unbindMiaodiKey(user *model.User, channelUserID string) string {
	if user.Status != userStatusBound && user.APIKey == "" {
		return "你当前还没有绑定喵滴 Key"
	}
	if err := e.userRepo.ClearBinding(channelUserID); err != nil {
		return "解绑失败：数据库错误"
	}
	user.APIKey = ""
	user.Email = ""
	user.Status = userStatusUnbound
	e.recordCall(channelUserID, "", "unbind_key")
	return "解绑成功，欢迎再次使用"
}

func (e *ToolExecutor) saveTextNote(user *model.User, channelUserID, arguments string) string {
	if user.Status != userStatusBound || user.APIKey == "" {
		return "尚未绑定喵滴 Key，请先绑定"
	}
	var args struct {
		Content string `json:"content"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	if args.Content == "" {
		return "content 不能为空"
	}
	title := args.Title
	if title == "" {
		title = getNowTitle(user.Title)
	}
	res, err := e.miaodi.PutText(user.APIKey, user.Book, user.Chara, title, args.Content)
	if err != nil {
		e.recordCall(channelUserID, user.APIKey, "put_text_failed")
		return fmt.Sprintf("保存失败：%v", err)
	}
	if isMiaodiSuccess(res) {
		e.recordCall(channelUserID, user.APIKey, "put_text")
		return fmt.Sprintf("已保存到：书本《%s》/ 章节《%s》/ 标题《%s》", user.Book, user.Chara, title)
	}
	msg := ""
	if m, ok := res["message"].(string); ok {
		msg = m
	}
	e.recordCall(channelUserID, user.APIKey, "put_text_failed")
	return fmt.Sprintf("保存失败：%s", msg)
}

func (e *ToolExecutor) saveImageNote(user *model.User, channelUserID, arguments string) string {
	if user.Status != userStatusBound || user.APIKey == "" {
		return "尚未绑定喵滴 Key，请先绑定"
	}
	var args struct {
		ImageURL string `json:"image_url"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	if args.ImageURL == "" {
		return "image_url 不能为空"
	}
	title := args.Title
	if title == "" {
		title = getNowTitle(user.Title)
	}
	if err := e.pendingRepo.Insert(user.APIKey, args.ImageURL, user.Book, user.Chara, title); err != nil {
		return fmt.Sprintf("图片落库失败：%v", err)
	}
	e.recordCall(channelUserID, user.APIKey, "save_image_pending")
	return fmt.Sprintf("图片已加入待上传队列：%s，等待后台扫描上传到喵滴", args.ImageURL)
}

func (e *ToolExecutor) resetConversation(user *model.User, channelUserID string, conversationID int64, arguments string) string {
	if e.convRepo == nil {
		return "重置失败：会话仓库未初始化"
	}
	if err := e.convRepo.Clear(channelUserID, conversationID); err != nil {
		return fmt.Sprintf("重置失败：%v", err)
	}
	return "已清空当前会话，我们可以重新开始。"
}

func (e *ToolExecutor) showHelp() string {
	return `我是喵滴 AI 助手，可以帮你：
- 绑定喵滴 API Key
- 通过邮箱验证码绑定喵滴账号
- 查看当前绑定 Key、解除绑定
- 获取喵滴年度报告链接
- 设置保存路径（书/章/标题）
- 保存文本笔记
- 保存图片到待上传队列
- 查看当前绑定状态和路径
- 查询最近保存的笔记
- 按日期查询笔记
- 查询当前准确时间、日期和星期
- 做基础计算、日期推算、随机数、随机选择和文本统计
- 计算文本 token 数
- 清空当前会话

绑定方式：
- 直接发送：绑定我的喵滴 key：xxxxx
- 或发送喵滴注册邮箱，例如：user@example.com，然后再回复收到的验证码

你可以直接用自然语言告诉我你想做什么。`
}

func (e *ToolExecutor) listRecentNotes(channelUserID, arguments string) string {
	if e.callLogRepo == nil {
		return "查询失败：日志仓库未初始化"
	}
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	logs, err := e.callLogRepo.RecentByUser(channelUserID, args.Limit)
	if err != nil {
		return fmt.Sprintf("查询失败：%v", err)
	}
	var sb strings.Builder
	count := 0
	for _, log := range logs {
		if !isNoteAction(log.Action) {
			continue
		}
		if count == 0 {
			sb.WriteString("最近保存记录：\n")
		}
		sb.WriteString(fmt.Sprintf("- %s %s\n", formatLogTime(log.CreatedAt), formatAction(log.Action)))
		count++
	}
	if count == 0 {
		return "最近没有保存记录。"
	}
	return sb.String()
}

func (e *ToolExecutor) queryNotesByDate(channelUserID, arguments string) string {
	if e.callLogRepo == nil {
		return "查询失败：日志仓库未初始化"
	}
	var args struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	if args.Date == "" {
		return "date 不能为空"
	}
	logs, err := e.callLogRepo.ByDate(channelUserID, args.Date)
	if err != nil {
		return fmt.Sprintf("查询失败：%v", err)
	}
	var sb strings.Builder
	count := 0
	for _, log := range logs {
		if !isNoteAction(log.Action) {
			continue
		}
		if count == 0 {
			sb.WriteString(fmt.Sprintf("%s 的保存记录：\n", args.Date))
		}
		sb.WriteString(fmt.Sprintf("- %s %s\n", formatLogTime(log.CreatedAt), formatAction(log.Action)))
		count++
	}
	if count == 0 {
		return fmt.Sprintf("%s 没有保存记录。", args.Date)
	}
	return sb.String()
}

// formatAction 把 api_call_log 中的 action 翻译为面向用户的中文标签
func formatAction(action string) string {
	switch action {
	case "put_text":
		return "文本笔记"
	case "save_image_pending":
		return "图片笔记"
	case "put_text_failed":
		return "文本保存失败"
	case "save_image_failed":
		return "图片保存失败"
	case "bind_key":
		return "绑定 Key"
	case "send_email":
		return "发送邮箱验证码"
	case "get_key":
		return "邮箱验证码绑定"
	case "annual_report":
		return "年度报告"
	case "unbind_key":
		return "解除绑定"
	default:
		return action
	}
}

// isNoteAction 判断 action 是否属于笔记相关操作
func isNoteAction(action string) bool {
	switch action {
	case "put_text", "save_image_pending", "put_text_failed", "save_image_failed":
		return true
	default:
		return false
	}
}

func isMiaodiSuccess(res map[string]interface{}) bool {
	switch code := res["code"].(type) {
	case float64:
		return code == 20000
	case int:
		return code == 20000
	case int64:
		return code == 20000
	default:
		return false
	}
}

func miaodiMessage(res map[string]interface{}) string {
	if msg, ok := res["message"].(string); ok && msg != "" {
		return msg
	}
	if msg, ok := res["msg"].(string); ok && msg != "" {
		return msg
	}
	return "未知错误"
}

func isValidEmail(email string) bool {
	return validEmailPattern.MatchString(email)
}

func (e *ToolExecutor) recordCall(channelUserID, apikey, action string) {
	if e.callLogRepo != nil {
		_ = e.callLogRepo.Record(channelUserID, apikey, "miaodi", action)
	}
}

func getNowTitle(title string) string {
	if title == "" || title == "null" {
		return timeutil.Date()
	}
	return title
}

func displayTitle(title string) string {
	if title == "" {
		return timeutil.Date() + "（默认）"
	}
	return title
}

func formatLogTime(t time.Time) string {
	return timeutil.FormatMinute(t)
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}
