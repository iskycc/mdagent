package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"miaodi-agent/internal/repository"
	"miaodi-agent/internal/timeutil"
	"miaodi-agent/pkg/openai"
)

func commonToolDefinitions() []openai.ToolDefinition {
	return []openai.ToolDefinition{
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "get_current_time",
				Description: "获取当前准确日期、时间、星期和时区。用户询问现在几点、今天日期、星期几、当前时间时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"timezone": map[string]interface{}{
							"type":        "string",
							"description": "可选，IANA 时区名或 UTC 偏移，例如 Asia/Shanghai、UTC、+08:00；默认 Asia/Shanghai",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "calculate",
				Description: "执行基础算术计算，支持 + - * / % 和括号。用户要求算一下、计算表达式时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"expression": map[string]interface{}{
							"type":        "string",
							"description": "算术表达式，例如 (12+8)*3/5",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "date_calculate",
				Description: "做日期推算，返回指定日期前后若干天的日期和星期。用户询问几天后、几天前、明天、后天日期时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"base_date": map[string]interface{}{
							"type":        "string",
							"description": "基准日期，YYYY-MM-DD 或 today/yesterday/tomorrow；默认 today",
						},
						"days_delta": map[string]interface{}{
							"type":        "integer",
							"description": "偏移天数，正数表示之后，负数表示之前",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "random_number",
				Description: "生成随机整数。用户要求随机数、抽一个数字时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"min": map[string]interface{}{
							"type":        "integer",
							"description": "最小值，默认 1",
						},
						"max": map[string]interface{}{
							"type":        "integer",
							"description": "最大值，默认 100",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "choose_option",
				Description: "从用户给出的选项中随机选择一个。用户要求帮我选、二选一、抽一个选项时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"options": map[string]interface{}{
							"type":        "array",
							"description": "候选项列表",
							"items":       map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"options"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "text_stats",
				Description: "统计文本长度、字符数、字节数、行数和粗略词数。用户要求统计字数、文本长度时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{
							"type":        "string",
							"description": "要统计的文本",
						},
					},
					"required": []string{"text"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "count_tokens",
				Description: "计算文本的 token 数。用户询问 token 数、上下文长度、这段话多少 token 时调用。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{
							"type":        "string",
							"description": "要计算 token 的文本",
						},
						"model": map[string]interface{}{
							"type":        "string",
							"description": "可选模型名；未知模型会回退到 cl100k_base",
						},
					},
					"required": []string{"text"},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "get_current_model",
				Description: "查询当前 AI 使用的模型名称。用户问你在用什么模型、当前模型是什么时调用。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: openai.FunctionDef{
				Name:        "get_conversation_start_time",
				Description: "查询当前会话第一条消息的发送时间，也就是会话开始时间。用户问对话什么时候开始、最早的消息是什么时候时调用。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
	}
}

func (e *ToolExecutor) getCurrentTime(arguments string) string {
	var args struct {
		Timezone string `json:"timezone"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	loc, locName, err := parseToolLocation(args.Timezone)
	if err != nil {
		return fmt.Sprintf("时区解析失败：%v", err)
	}
	now := time.Now().In(loc)
	return fmt.Sprintf("当前时间：%s\n日期：%s\n星期：%s\n时区：%s",
		now.Format("2006-01-02 15:04:05"), now.Format("2006-01-02"), chineseWeekday(now.Weekday()), locName)
}

func (e *ToolExecutor) calculate(arguments string) string {
	var args struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	expr := strings.TrimSpace(args.Expression)
	if expr == "" {
		return "expression 不能为空"
	}
	value, err := evalExpression(expr)
	if err != nil {
		return fmt.Sprintf("计算失败：%v", err)
	}
	return fmt.Sprintf("%s = %s", expr, formatFloat(value))
}

func (e *ToolExecutor) dateCalculate(arguments string) string {
	var args struct {
		BaseDate  string `json:"base_date"`
		DaysDelta int    `json:"days_delta"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	base, baseText, err := parseBaseDate(args.BaseDate)
	if err != nil {
		return fmt.Sprintf("日期解析失败：%v", err)
	}
	target := base.AddDate(0, 0, args.DaysDelta)
	return fmt.Sprintf("基准日期：%s（%s）\n偏移：%+d 天\n结果：%s（%s）",
		baseText, chineseWeekday(base.Weekday()), args.DaysDelta, target.Format("2006-01-02"), chineseWeekday(target.Weekday()))
}

func (e *ToolExecutor) randomNumber(arguments string) string {
	var args struct {
		Min int64 `json:"min"`
		Max int64 `json:"max"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	if args.Min == 0 && args.Max == 0 {
		args.Min = 1
		args.Max = 100
	}
	if args.Min > args.Max {
		args.Min, args.Max = args.Max, args.Min
	}
	if args.Max-args.Min > math.MaxInt32 {
		return "随机范围过大，请缩小范围"
	}
	n, err := secureRandInt(args.Min, args.Max)
	if err != nil {
		return fmt.Sprintf("随机数生成失败：%v", err)
	}
	return fmt.Sprintf("随机数：%d（范围 %d 到 %d）", n, args.Min, args.Max)
}

func (e *ToolExecutor) chooseOption(arguments string) string {
	var args struct {
		Options []string `json:"options"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	options := make([]string, 0, len(args.Options))
	for _, option := range args.Options {
		option = strings.TrimSpace(option)
		if option != "" {
			options = append(options, option)
		}
	}
	if len(options) == 0 {
		return "options 不能为空"
	}
	idx, err := secureRandInt(0, int64(len(options)-1))
	if err != nil {
		return fmt.Sprintf("随机选择失败：%v", err)
	}
	return fmt.Sprintf("我选：%s", options[idx])
}

func (e *ToolExecutor) textStats(arguments string) string {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	if args.Text == "" {
		return "text 不能为空"
	}
	runes := utf8.RuneCountInString(args.Text)
	lines := 1
	if args.Text != "" {
		lines = strings.Count(args.Text, "\n") + 1
	}
	return fmt.Sprintf("文本统计：字符数 %d，字节数 %d，行数 %d，粗略词数 %d",
		runes, len(args.Text), lines, countWords(args.Text))
}

func (e *ToolExecutor) countTokens(arguments string) string {
	var args struct {
		Text  string `json:"text"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "参数解析失败"
	}
	if args.Text == "" {
		return "text 不能为空"
	}
	model := strings.TrimSpace(args.Model)
	if model == "" {
		model = "cl100k_base"
	}
	tokenizer := newTokenCounter(model)
	tokens := tokenizer.TextTokens(args.Text)
	return fmt.Sprintf("Token 统计：%d tokens（模型/编码：%s）", tokens, tokenizer.EncodingLabel())
}

func (e *ToolExecutor) getCurrentModel(arguments string) string {
	if e.model == "" {
		return "当前模型：未知"
	}
	return fmt.Sprintf("当前模型：%s", e.model)
}

func (e *ToolExecutor) getConversationStartTime(channelUserID string, conversationID int64) string {
	ctx := context.Background()
	var stored []repository.StoredChatMessage
	var err error

	// 优先读缓存，缓存不可用则回源 MySQL。
	stored, err = e.cache.GetMessages(ctx, channelUserID, conversationID)
	if err != nil {
		stored, err = e.convRepo.GetStoredMessages(channelUserID, conversationID)
		if err != nil {
			return fmt.Sprintf("获取会话历史失败：%v", err)
		}
	}

	if len(stored) == 0 {
		return "当前会话还没有消息"
	}

	first := stored[0]
	for _, m := range stored {
		if m.CreatedAt.Before(first.CreatedAt) {
			first = m
		}
	}
	return fmt.Sprintf("当前会话第一条消息时间：%s", first.CreatedAt.In(timeutil.BeijingLocation()).Format("2006-01-02 15:04:05"))
}

func parseToolLocation(name string) (*time.Location, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return timeutil.BeijingLocation(), "Asia/Shanghai", nil
	}
	if strings.EqualFold(name, "beijing") || strings.EqualFold(name, "shanghai") || name == "北京时间" {
		return timeutil.BeijingLocation(), "Asia/Shanghai", nil
	}
	if strings.EqualFold(name, "utc") {
		return time.UTC, "UTC", nil
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc, name, nil
	}
	if loc, ok := parseUTCOffset(name); ok {
		return loc, "UTC" + name, nil
	}
	return nil, "", fmt.Errorf("不支持的时区 %q", name)
}

func parseUTCOffset(offset string) (*time.Location, bool) {
	if len(offset) != 6 || (offset[0] != '+' && offset[0] != '-') || offset[3] != ':' {
		return nil, false
	}
	hour, err1 := strconv.Atoi(offset[1:3])
	minute, err2 := strconv.Atoi(offset[4:6])
	if err1 != nil || err2 != nil || hour > 14 || minute > 59 {
		return nil, false
	}
	seconds := hour*3600 + minute*60
	if offset[0] == '-' {
		seconds = -seconds
	}
	return time.FixedZone("UTC"+offset, seconds), true
}

func parseBaseDate(base string) (time.Time, string, error) {
	base = strings.TrimSpace(strings.ToLower(base))
	now := timeutil.Now()
	switch base {
	case "", "today", "今天":
		d := dateOnly(now)
		return d, d.Format("2006-01-02"), nil
	case "yesterday", "昨天":
		d := dateOnly(now).AddDate(0, 0, -1)
		return d, d.Format("2006-01-02"), nil
	case "tomorrow", "明天":
		d := dateOnly(now).AddDate(0, 0, 1)
		return d, d.Format("2006-01-02"), nil
	default:
		t, err := time.ParseInLocation("2006-01-02", base, timeutil.BeijingLocation())
		if err != nil {
			return time.Time{}, "", fmt.Errorf("请使用 YYYY-MM-DD、today、yesterday 或 tomorrow")
		}
		return t, t.Format("2006-01-02"), nil
	}
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.In(timeutil.BeijingLocation()).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, timeutil.BeijingLocation())
}

func secureRandInt(min, max int64) (int64, error) {
	if min == max {
		return min, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max-min+1))
	if err != nil {
		return 0, err
	}
	return min + n.Int64(), nil
}

func chineseWeekday(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "星期一"
	case time.Tuesday:
		return "星期二"
	case time.Wednesday:
		return "星期三"
	case time.Thursday:
		return "星期四"
	case time.Friday:
		return "星期五"
	case time.Saturday:
		return "星期六"
	default:
		return "星期日"
	}
}

func countWords(text string) int {
	count := 0
	inWord := false
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			count++
			inWord = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !inWord {
				count++
				inWord = true
			}
			continue
		}
		inWord = false
	}
	return count
}

func formatFloat(v float64) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return fmt.Sprintf("%g", v)
	}
	if math.Abs(v-math.Round(v)) < 1e-10 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func evalExpression(expr string) (float64, error) {
	p := expressionParser{input: expr}
	value, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if p.pos != len(p.input) {
		return 0, fmt.Errorf("无法识别 %q", p.input[p.pos:])
	}
	return value, nil
}

type expressionParser struct {
	input string
	pos   int
}

func (p *expressionParser) parseExpression() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.match('+') {
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left += right
			continue
		}
		if p.match('-') {
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left -= right
			continue
		}
		return left, nil
	}
}

func (p *expressionParser) parseTerm() (float64, error) {
	left, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.match('*') {
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			left *= right
			continue
		}
		if p.match('/') {
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if right == 0 {
				return 0, fmt.Errorf("除数不能为 0")
			}
			left /= right
			continue
		}
		if p.match('%') {
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if right == 0 {
				return 0, fmt.Errorf("取模除数不能为 0")
			}
			left = math.Mod(left, right)
			continue
		}
		return left, nil
	}
}

func (p *expressionParser) parseFactor() (float64, error) {
	p.skipSpaces()
	if p.match('+') {
		return p.parseFactor()
	}
	if p.match('-') {
		v, err := p.parseFactor()
		return -v, err
	}
	if p.match('(') {
		v, err := p.parseExpression()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if !p.match(')') {
			return 0, fmt.Errorf("缺少右括号")
		}
		return v, nil
	}
	return p.parseNumber()
}

func (p *expressionParser) parseNumber() (float64, error) {
	p.skipSpaces()
	start := p.pos
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if (c >= '0' && c <= '9') || c == '.' {
			p.pos++
			continue
		}
		break
	}
	if start == p.pos {
		return 0, fmt.Errorf("需要数字")
	}
	v, err := strconv.ParseFloat(p.input[start:p.pos], 64)
	if err != nil {
		return 0, fmt.Errorf("数字格式错误")
	}
	return v, nil
}

func (p *expressionParser) skipSpaces() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *expressionParser) match(c byte) bool {
	if p.pos < len(p.input) && p.input[p.pos] == c {
		p.pos++
		return true
	}
	return false
}
