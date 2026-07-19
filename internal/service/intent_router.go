package service

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/timeutil"
)

var (
	datePattern       = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	urlPattern        = regexp.MustCompile(`https?://[^\s，,。；;]+`)
	emailPattern      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	keyAfterPattern   = regexp.MustCompile(`(?i)(?:api\s*key|apikey|key|密钥)[：:\s]*([^\s，,。；;]+)`)
	numberCodePattern = regexp.MustCompile(`[0-9]{4,8}`)
	alphaCodePattern  = regexp.MustCompile(`^[A-Za-z0-9]+$`)
)

// IntentRouter 为小模型兜底处理高置信度意图。
type IntentRouter struct {
	toolExec ToolRunner
}

func NewIntentRouter(toolExec ToolRunner) *IntentRouter {
	return &IntentRouter{toolExec: toolExec}
}

// Route 尝试在本地处理明确意图，返回 handled=false 时继续走 LLM。
func (r *IntentRouter) Route(user *model.User, channelUserID string, conversationID int64, text string) (string, bool) {
	if r == nil || r.toolExec == nil {
		return "", false
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	normalized := normalizeIntentText(text)

	if isResetIntent(normalized) {
		return r.toolExec.Execute(user, channelUserID, conversationID, "reset_conversation", "{}"), true
	}
	if isUnbindIntent(normalized) {
		return r.toolExec.Execute(user, channelUserID, conversationID, "unbind_miaodi_key", "{}"), true
	}
	if isHelpIntent(normalized) {
		return r.toolExec.Execute(user, channelUserID, conversationID, "show_help", "{}"), true
	}
	if isAnnualReportIntent(normalized) {
		return r.toolExec.Execute(user, channelUserID, conversationID, "get_miaodi_annual_report", "{}"), true
	}
	if isGetKeyIntent(normalized) {
		return r.toolExec.Execute(user, channelUserID, conversationID, "get_miaodi_key", "{}"), true
	}
	if isProfileIntent(normalized) {
		return r.toolExec.Execute(user, channelUserID, conversationID, "get_user_profile", "{}"), true
	}
	if args, ok := parseRecentNotesIntent(normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "list_recent_notes", toJSONString(args)), true
	}
	if args, ok := parseBindIntent(text, normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "bind_miaodi_key", toJSONString(args)), true
	}
	if args, ok := parseEmailCodeIntent(user, text, normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "bind_miaodi_by_email_code", toJSONString(args)), true
	}
	if args, ok := parseSendEmailIntent(user, text, normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "send_miaodi_email_code", toJSONString(args)), true
	}
	if args, ok := parsePathIntent(text, normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "set_save_path", toJSONString(args)), true
	}
	if args, ok := parseImageIntent(text, normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "save_image_note", toJSONString(args)), true
	}
	if args, ok := parseSaveTextIntent(text, normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "save_text_note", toJSONString(args)), true
	}
	if args, ok := parseDateQueryIntent(normalized); ok {
		return r.toolExec.Execute(user, channelUserID, conversationID, "query_notes_by_date", toJSONString(args)), true
	}

	return "", false
}

func normalizeIntentText(text string) string {
	text = strings.TrimSpace(text)
	replacer := strings.NewReplacer("：", ":", "，", ",", "。", ".", "\n", " ", "\t", " ")
	text = replacer.Replace(text)
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}
	text = strings.TrimPrefix(text, "/")
	return strings.ToLower(strings.TrimSpace(text))
}

func isHelpIntent(text string) bool {
	switch text {
	case "帮助", "help", "?", "？", "菜单", "使用说明", "怎么用", "怎么绑定", "如何绑定", "绑定":
		return true
	}
	return len([]rune(text)) <= 30 && (strings.Contains(text, "你能做什么") ||
		strings.Contains(text, "有什么功能") ||
		strings.Contains(text, "如何使用"))
}

func isResetIntent(text string) bool {
	if len([]rune(text)) > 30 {
		return false
	}
	return strings.Contains(text, "重置") ||
		strings.Contains(text, "清空") ||
		strings.Contains(text, "重新开始") ||
		strings.Contains(text, "忘记刚才")
}

func isUnbindIntent(text string) bool {
	if len([]rune(text)) > 30 {
		return false
	}
	return strings.Contains(text, "解绑") || strings.Contains(text, "解除绑定")
}

func isAnnualReportIntent(text string) bool {
	if len([]rune(text)) > 40 {
		return false
	}
	return strings.Contains(text, "年度报告") || strings.Contains(text, "报告地址")
}

func isGetKeyIntent(text string) bool {
	if len([]rune(text)) > 40 {
		return false
	}
	return strings.Contains(text, "获取当前绑定key") ||
		strings.Contains(text, "当前绑定key") ||
		strings.Contains(text, "我的key") ||
		strings.Contains(text, "查看key")
}

func isProfileIntent(text string) bool {
	if len([]rune(text)) > 40 {
		return false
	}
	if strings.Contains(text, "设置") || strings.Contains(text, "修改") || strings.Contains(text, "保存到") {
		return false
	}
	return strings.Contains(text, "绑定状态") ||
		strings.Contains(text, "当前配置") ||
		strings.Contains(text, "我的配置") ||
		strings.Contains(text, "保存路径")
}

func parseRecentNotesIntent(text string) (map[string]int, bool) {
	if !(strings.Contains(text, "最近") && (strings.Contains(text, "保存") || strings.Contains(text, "记录") || strings.Contains(text, "笔记"))) {
		return nil, false
	}
	limit := 5
	for _, field := range strings.Fields(text) {
		if n, err := strconv.Atoi(field); err == nil && n > 0 {
			limit = n
			break
		}
	}
	return map[string]int{"limit": limit}, true
}

func parseDateQueryIntent(text string) (map[string]string, bool) {
	if !(strings.Contains(text, "保存") || strings.Contains(text, "记录") || strings.Contains(text, "笔记")) {
		return nil, false
	}
	if !(strings.Contains(text, "什么") || strings.Contains(text, "哪些") || strings.Contains(text, "查询") || strings.Contains(text, "查看")) {
		return nil, false
	}
	if strings.Contains(text, "昨天") {
		return map[string]string{"date": timeutil.Now().AddDate(0, 0, -1).Format("2006-01-02")}, true
	}
	if strings.Contains(text, "今天") {
		return map[string]string{"date": timeutil.Date()}, true
	}
	date := datePattern.FindString(text)
	if date == "" {
		return nil, false
	}
	return map[string]string{"date": date}, true
}

func parseBindIntent(original, normalized string) (map[string]string, bool) {
	if !(strings.Contains(normalized, "绑定") || strings.Contains(normalized, "bind")) {
		return nil, false
	}
	if emailPattern.MatchString(original) || strings.Contains(normalized, "邮箱") {
		return nil, false
	}
	original = strings.TrimPrefix(strings.TrimSpace(original), "/")
	if matches := keyAfterPattern.FindStringSubmatch(original); len(matches) == 2 {
		return map[string]string{"key": strings.TrimSpace(matches[1])}, true
	}
	for _, prefix := range []string{"绑定", "bind"} {
		if strings.HasPrefix(normalized, prefix) {
			key := strings.TrimSpace(original[len(prefix):])
			key = strings.TrimLeft(key, " :：")
			if key != "" {
				fields := strings.Fields(key)
				return map[string]string{"key": fields[len(fields)-1]}, true
			}
		}
	}
	return nil, false
}

func parseSendEmailIntent(user *model.User, original, normalized string) (map[string]string, bool) {
	email := emailPattern.FindString(original)
	if email == "" {
		return nil, false
	}
	hasBindingHint := strings.Contains(normalized, "邮箱") ||
		strings.Contains(normalized, "验证码") ||
		strings.Contains(normalized, "绑定")
	if !hasBindingHint && user != nil && user.Status == userStatusBound {
		return nil, false
	}
	return map[string]string{"email": email}, true
}

func parseEmailCodeIntent(user *model.User, original, normalized string) (map[string]string, bool) {
	if strings.Contains(normalized, "发送") || strings.Contains(normalized, "邮箱") && emailPattern.MatchString(original) {
		return nil, false
	}
	if user.Status != userStatusWaitingEmailCode && !strings.Contains(normalized, "验证码") {
		return nil, false
	}
	code := extractVerificationCode(original)
	if code == "" {
		return nil, false
	}
	args := map[string]string{"code": code}
	if email := emailPattern.FindString(original); email != "" {
		args["email"] = email
	}
	return args, true
}

func extractVerificationCode(text string) string {
	if code := numberCodePattern.FindString(text); code != "" {
		return code
	}
	text = strings.TrimSpace(text)
	replacer := strings.NewReplacer("：", ":", "，", ",", "。", ".", "\n", " ", "\t", " ")
	text = replacer.Replace(text)
	for _, field := range strings.Fields(strings.TrimPrefix(text, "/")) {
		field = strings.Trim(field, " :.,;，。；")
		lowerField := strings.ToLower(field)
		if lowerField == "code" || lowerField == "验证码" || lowerField == "verify" {
			continue
		}
		if len(field) >= 4 && len(field) <= 12 && alphaCodePattern.MatchString(field) {
			return field
		}
	}
	return ""
}

func parsePathIntent(original, normalized string) (map[string]string, bool) {
	if !(strings.Contains(normalized, "路径") || strings.Contains(normalized, "保存到")) {
		return nil, false
	}

	book, chapter, title := parseChinesePath(original)
	if book != "" && chapter != "" {
		return map[string]string{"book": book, "chapter": chapter, "title": title}, true
	}

	fields := strings.Fields(strings.TrimPrefix(strings.TrimPrefix(normalized, "/"), "路径"))
	if len(fields) >= 2 && (strings.HasPrefix(normalized, "路径") || strings.HasPrefix(normalized, "set path")) {
		title = ""
		if len(fields) > 2 {
			title = strings.Join(fields[2:], " ")
		}
		return map[string]string{"book": fields[0], "chapter": fields[1], "title": title}, true
	}
	return nil, false
}

func parseChinesePath(text string) (string, string, string) {
	if matches := regexp.MustCompile(`《([^》]+)》\s*第?\s*([^章《》]+)\s*章(?:节)?\s*《([^》]+)》`).FindStringSubmatch(text); len(matches) == 4 {
		return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]), strings.TrimSpace(matches[3])
	}
	if matches := regexp.MustCompile(`书本?[:：]\s*([^,，\s]+).*章节?[:：]\s*([^,，\s]+)(?:.*标题[:：]\s*([^,，\s]+))?`).FindStringSubmatch(text); len(matches) >= 3 {
		title := ""
		if len(matches) == 4 {
			title = strings.TrimSpace(matches[3])
		}
		return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]), title
	}
	return "", "", ""
}

func parseImageIntent(original, normalized string) (map[string]string, bool) {
	if !(strings.Contains(normalized, "图片") || strings.Contains(normalized, "照片") || strings.Contains(normalized, "image")) {
		return nil, false
	}
	url := urlPattern.FindString(original)
	if url == "" {
		return nil, false
	}
	return map[string]string{"image_url": url}, true
}

func parseSaveTextIntent(original, normalized string) (map[string]string, bool) {
	if strings.Contains(normalized, "怎么保存") || strings.Contains(normalized, "如何保存") {
		return nil, false
	}
	original = strings.TrimPrefix(strings.TrimSpace(original), "/")
	for _, keyword := range []string{"帮我保存", "保存", "记一下"} {
		if !strings.HasPrefix(normalized, strings.ToLower(keyword)) {
			continue
		}
		content := strings.TrimSpace(strings.TrimPrefix(original, keyword))
		content = strings.TrimLeft(content, " :：")
		if content != "" {
			return map[string]string{"content": content}, true
		}
	}
	return nil, false
}

func toJSONString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
