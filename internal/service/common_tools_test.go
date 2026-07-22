package service

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
	"miaodi-agent/pkg/openai"
)

func TestToolExecutor_getCurrentTime(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "get_current_time", `{}`)
	if !strings.Contains(res, "当前时间：") || !strings.Contains(res, "Asia/Shanghai") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_calculate(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "calculate", `{"expression":"(12+8)*3/5"}`)
	if res != "(12+8)*3/5 = 12" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_calculate_DivideByZero(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "calculate", `{"expression":"1/0"}`)
	if !strings.Contains(res, "除数不能为 0") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_dateCalculate(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "date_calculate", `{"base_date":"2026-07-20","days_delta":7}`)
	if !strings.Contains(res, "2026-07-27") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_randomNumber(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "random_number", `{"min":1,"max":1}`)
	if res != "随机数：1（范围 1 到 1）" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_chooseOption(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "choose_option", `{"options":["A"]}`)
	if res != "我选：A" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_textStats(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "text_stats", `{"text":"你好\nworld"}`)
	if !strings.Contains(res, "字符数 8") || !strings.Contains(res, "行数 2") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_countTokens(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "count_tokens", `{"text":"hello world"}`)
	if !strings.Contains(res, "Token 统计：") || !strings.Contains(res, "cl100k_base") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getCurrentModel(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	exec.SetModel("deepseek-v4-flash")
	res := exec.Execute(&model.User{}, "u1", 1, "get_current_model", `{}`)
	if res != "当前模型：deepseek-v4-flash" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getConversationStartTime(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	messages := []repository.StoredChatMessage{
		{ChatMessage: openai.ChatMessage{Role: "user", Content: "你好"}, CreatedAt: time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)},
		{ChatMessage: openai.ChatMessage{Role: "assistant", Content: "你好呀"}, CreatedAt: time.Date(2026, 7, 21, 10, 1, 0, 0, time.UTC)},
	}
	raw, _ := json.Marshal(messages)
	rows := sqlmock.NewRows([]string{"messages", "updated_at"}).
		AddRow(string(raw), time.Date(2026, 7, 21, 10, 1, 0, 0, time.UTC))
	mock.ExpectQuery("SELECT messages, updated_at FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnRows(rows)

	res := exec.Execute(&model.User{}, "u1", 1, "get_conversation_start_time", `{}`)
	if !strings.Contains(res, "2026-07-21 18:00:00") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getConversationStartTime_Empty(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	rows := sqlmock.NewRows([]string{"messages", "updated_at"})
	mock.ExpectQuery("SELECT messages, updated_at FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnRows(rows)

	res := exec.Execute(&model.User{}, "u1", 1, "get_conversation_start_time", `{}`)
	if res != "当前会话还没有消息" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestCommonToolDefinitions(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range ToolDefinitions() {
		names[tool.Function.Name] = true
	}
	for _, name := range []string{"get_current_time", "calculate", "date_calculate", "random_number", "choose_option", "text_stats", "count_tokens", "get_current_model", "get_conversation_start_time"} {
		if !names[name] {
			t.Fatalf("missing tool definition: %s", name)
		}
	}
}

func TestToolExecutor_getCurrentTime_UTC(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "get_current_time", `{"timezone":"UTC"}`)
	if !strings.Contains(res, "UTC") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getCurrentTime_InvalidTimezone(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "get_current_time", `{"timezone":"Mars/Time"}`)
	if !strings.Contains(res, "时区解析失败") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getCurrentTime_UTCOffset(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "get_current_time", `{"timezone":"+08:00"}`)
	if !strings.Contains(res, "UTC+08:00") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_calculate_Errors(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	tests := []struct {
		name, args, want string
	}{
		{"invalid json", `bad`, "参数解析失败"},
		{"empty expression", `{"expression":" "}`, "expression 不能为空"},
		{"bad chars", `{"expression":"1+"}`, "计算失败"},
		{"mod by zero", `{"expression":"5%0"}`, "取模除数不能为 0"},
		{"nan", `{"expression":"0/0"}`, "计算失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := exec.Execute(&model.User{}, "u1", 1, "calculate", tt.args)
			if !strings.Contains(res, tt.want) {
				t.Fatalf("expected %q in result, got: %s", tt.want, res)
			}
		})
	}
}

func TestToolExecutor_dateCalculate_Defaults(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "date_calculate", `{}`)
	if !strings.Contains(res, "基准日期") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_dateCalculate_InvalidBase(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "date_calculate", `{"base_date":"bad"}`)
	if !strings.Contains(res, "日期解析失败") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_randomNumber_Defaults(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "random_number", `{}`)
	if !strings.Contains(res, "随机数：") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_randomNumber_SwapAndRange(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "random_number", `{"min":10,"max":5}`)
	if !strings.Contains(res, "随机数：") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_randomNumber_TooLarge(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "random_number", `{"min":0,"max":99999999999}`)
	if !strings.Contains(res, "随机范围过大") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_chooseOption_EmptyAndInvalid(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	if res := exec.Execute(&model.User{}, "u1", 1, "choose_option", `{"options":[]}`); !strings.Contains(res, "options 不能为空") {
		t.Fatalf("unexpected result: %s", res)
	}
	if res := exec.Execute(&model.User{}, "u1", 1, "choose_option", `bad`); !strings.Contains(res, "参数解析失败") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_textStats_EmptyAndInvalid(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	if res := exec.Execute(&model.User{}, "u1", 1, "text_stats", `{"text":""}`); !strings.Contains(res, "text 不能为空") {
		t.Fatalf("unexpected result: %s", res)
	}
	if res := exec.Execute(&model.User{}, "u1", 1, "text_stats", `bad`); !strings.Contains(res, "参数解析失败") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_countTokens_EmptyAndInvalid(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	if res := exec.Execute(&model.User{}, "u1", 1, "count_tokens", `{"text":""}`); !strings.Contains(res, "text 不能为空") {
		t.Fatalf("unexpected result: %s", res)
	}
	if res := exec.Execute(&model.User{}, "u1", 1, "count_tokens", `bad`); !strings.Contains(res, "参数解析失败") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getCurrentModel_Unknown(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "get_current_model", `{}`)
	if res != "当前模型：未知" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getConversationStartTime_CacheError(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectQuery("SELECT messages, updated_at FROM agent_conversations").WithArgs("u1", int64(1)).WillReturnError(sqlmock.ErrCancelled)
	res := exec.Execute(&model.User{}, "u1", 1, "get_conversation_start_time", `{}`)
	if !strings.Contains(res, "获取会话历史失败") {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestParseToolLocation(t *testing.T) {
	loc, name, err := parseToolLocation("Asia/Shanghai")
	if err != nil || loc == nil || name != "Asia/Shanghai" {
		t.Fatalf("unexpected result: %v %v %v", loc, name, err)
	}
	_, name, err = parseToolLocation("beijing")
	if err != nil || name != "Asia/Shanghai" {
		t.Fatalf("unexpected result: %v %v", name, err)
	}
	_, name, err = parseToolLocation("北京时间")
	if err != nil || name != "Asia/Shanghai" {
		t.Fatalf("unexpected result: %v %v", name, err)
	}
	_, name, err = parseToolLocation("utc")
	if err != nil || name != "UTC" {
		t.Fatalf("unexpected result: %v %v", name, err)
	}
	_, name, err = parseToolLocation("-05:30")
	if err != nil || name != "UTC-05:30" {
		t.Fatalf("unexpected result: %v %v", name, err)
	}
	if _, _, err := parseToolLocation("bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseUTCOffset(t *testing.T) {
	bad := []string{"", "08:00", "+8:00", "+08-00", "+08:60", "+15:00"}
	for _, v := range bad {
		if _, ok := parseUTCOffset(v); ok {
			t.Fatalf("expected %q to fail", v)
		}
	}
}

func TestParseBaseDate(t *testing.T) {
	if _, text, err := parseBaseDate("today"); err != nil || text == "" {
		t.Fatalf("unexpected result: %v %v", text, err)
	}
	if _, text, err := parseBaseDate("昨天"); err != nil || text == "" {
		t.Fatalf("unexpected result: %v %v", text, err)
	}
	if _, text, err := parseBaseDate("明天"); err != nil || text == "" {
		t.Fatalf("unexpected result: %v %v", text, err)
	}
	if _, _, err := parseBaseDate("bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDateOnly(t *testing.T) {
	if dateOnly(time.Now()).IsZero() {
		t.Fatal("expected non-zero date")
	}
}

func TestSecureRandInt(t *testing.T) {
	if n, err := secureRandInt(5, 5); err != nil || n != 5 {
		t.Fatalf("unexpected result: %v %v", n, err)
	}
	if _, err := secureRandInt(1, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChineseWeekday(t *testing.T) {
	want := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	for i, d := range []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday} {
		if got := chineseWeekday(d); got != want[i] {
			t.Fatalf("weekday %v: got %s, want %s", d, got, want[i])
		}
	}
}

func TestFormatFloat(t *testing.T) {
	if got := formatFloat(3.0); got != "3" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := formatFloat(3.14); got != "3.14" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := formatFloat(math.Inf(1)); got != "+Inf" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := formatFloat(math.NaN()); got != "NaN" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestEvalExpression(t *testing.T) {
	if _, err := evalExpression("1+"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := evalExpression("1 2"); err == nil {
		t.Fatal("expected error")
	}
	if v, err := evalExpression(" - (3 + 2) * 4 "); err != nil || v != -20 {
		t.Fatalf("unexpected result: %v %v", v, err)
	}
	if v, err := evalExpression(" 2 ^ 3 "); err == nil {
		t.Fatalf("expected error for unsupported operator, got %v", v)
	}
}

func TestExpressionParser_Edges(t *testing.T) {
	tests := []struct {
		expr string
		err  bool
	}{
		{"3 + 4 * 2 / (1 - 5) % 3", false},
		{"(1+2", true},
		{"", true},
		{"abc", true},
		{"1 / 0", true},
		{"1 % 0", true},
	}
	for _, tt := range tests {
		_, err := evalExpression(tt.expr)
		if tt.err && err == nil {
			t.Fatalf("expected error for %q", tt.expr)
		}
		if !tt.err && err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.expr, err)
		}
	}
}
