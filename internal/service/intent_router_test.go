package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"miaodi-agent/internal/model"
	"miaodi-agent/internal/timeutil"
)

func TestIntentRouter_Route(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantTool string
	}{
		{name: "help", text: "你能做什么？", wantTool: "show_help"},
		{name: "reset", text: "清空刚才的对话", wantTool: "reset_conversation"},
		{name: "profile", text: "查看当前保存路径", wantTool: "get_user_profile"},
		{name: "recent", text: "最近保存了什么 3", wantTool: "list_recent_notes"},
		{name: "date", text: "2026-06-30 那天保存了哪些笔记", wantTool: "query_notes_by_date"},
		{name: "bind", text: "绑定我的喵滴 key：abc123456", wantTool: "bind_miaodi_key"},
		{name: "bind help", text: "怎么绑定", wantTool: "show_help"},
		{name: "send email", text: "用邮箱 u@example.com 绑定喵滴", wantTool: "send_miaodi_email_code"},
		{name: "get key", text: "查看key", wantTool: "get_miaodi_key"},
		{name: "annual report", text: "年度报告地址", wantTool: "get_miaodi_annual_report"},
		{name: "unbind", text: "解除绑定", wantTool: "unbind_miaodi_key"},
		{name: "path", text: "把后续内容保存到《日记》第 3 章《今天》", wantTool: "set_save_path"},
		{name: "image", text: "保存图片 https://example.com/a.jpg", wantTool: "save_image_note"},
		{name: "save text", text: "保存：今天读完了第一章", wantTool: "save_text_note"},
		{name: "current time", text: "现在几点？", wantTool: "get_current_time"},
		{name: "calculate", text: "算一下 1+2*3", wantTool: "calculate"},
		{name: "date calculate", text: "7天后是几号", wantTool: "date_calculate"},
		{name: "tomorrow date", text: "明天星期几", wantTool: "date_calculate"},
		{name: "random number", text: "1到10随机数", wantTool: "random_number"},
		{name: "choose option", text: "帮我选 A 还是 B", wantTool: "choose_option"},
		{name: "text stats", text: "统计字数：你好 world", wantTool: "text_stats"},
		{name: "count tokens", text: "计算token：hello world", wantTool: "count_tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeToolRunner{result: "ok"}
			router := NewIntentRouter(runner)
			reply, handled := router.Route(&model.User{}, "u1", 100, tt.text)
			if !handled {
				t.Fatal("expected handled intent")
			}
			if reply != "ok" {
				t.Fatalf("unexpected reply: %s", reply)
			}
			if runner.executedName != tt.wantTool {
				t.Fatalf("expected %s, got %s", tt.wantTool, runner.executedName)
			}
		})
	}
}

func TestIntentRouter_DoesNotRouteUnknownText(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "帮我总结一下这段内容")
	if handled {
		t.Fatal("expected unknown text to go through llm")
	}
}

func TestIntentRouter_BindArgs(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "/绑定 key: sk-test-123")
	if !handled {
		t.Fatal("expected handled intent")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if args["key"] != "sk-test-123" {
		t.Fatalf("unexpected key: %q", args["key"])
	}
}

func TestIntentRouter_SendEmailArgs(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "邮箱绑定 u@example.com")
	if !handled {
		t.Fatal("expected handled intent")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if runner.executedName != "send_miaodi_email_code" || args["email"] != "u@example.com" {
		t.Fatalf("unexpected tool or args: %s %+v", runner.executedName, args)
	}
}

func TestIntentRouter_BareEmailRoutesWhenUnbound(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusUnbound}
	_, handled := router.Route(user, "u1", 100, "u@example.com")
	if !handled {
		t.Fatal("expected handled intent")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if runner.executedName != "send_miaodi_email_code" || args["email"] != "u@example.com" {
		t.Fatalf("unexpected tool or args: %s %+v", runner.executedName, args)
	}
}

func TestIntentRouter_BareEmailDoesNotRouteWhenBound(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusBound}
	_, handled := router.Route(user, "u1", 100, "u@example.com")
	if handled {
		t.Fatal("expected bound bare email to go through llm")
	}
}

func TestIntentRouter_EmailCodeArgs(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusWaitingEmailCode}
	_, handled := router.Route(user, "u1", 100, "123456")
	if !handled {
		t.Fatal("expected handled intent")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if runner.executedName != "bind_miaodi_by_email_code" || args["code"] != "123456" {
		t.Fatalf("unexpected tool or args: %s %+v", runner.executedName, args)
	}
}

func TestIntentRouter_EmailAlphaCodeArgs(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusWaitingEmailCode}
	_, handled := router.Route(user, "u1", 100, "code abc123")
	if !handled {
		t.Fatal("expected handled intent")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if args["code"] != "abc123" {
		t.Fatalf("unexpected code: %+v", args)
	}
}

func TestIntentRouter_EmailUpperAlphaCodePreservesCase(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusWaitingEmailCode}
	_, handled := router.Route(user, "u1", 100, "TLRF")
	if !handled {
		t.Fatal("expected handled intent")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if args["code"] != "TLRF" {
		t.Fatalf("unexpected code: %+v", args)
	}
}

func TestIntentRouter_PathArgs(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "路径 日记 第三章 周末")
	if !handled {
		t.Fatal("expected handled intent")
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if args["book"] != "日记" || args["chapter"] != "第三章" || args["title"] != "周末" {
		t.Fatalf("unexpected args: %+v", args)
	}
}

func TestIntentRouter_TodayAndYesterdayDateArgs(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "昨天保存了哪些笔记")
	if !handled {
		t.Fatal("expected handled intent")
	}
	if !strings.Contains(runner.executedArgs, timeutil.Now().AddDate(0, 0, -1).Format("2006-01-02")) {
		t.Fatalf("unexpected yesterday args: %s", runner.executedArgs)
	}
}

func TestIntentRouter_CommonToolArgs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "calculate", text: "算一下 1+2*3", want: `"expression":"1+2*3"`},
		{name: "random", text: "10到20随机数", want: `"max":20`},
		{name: "choose", text: "帮我选 A 还是 B", want: `"options":["A","B"]`},
		{name: "text stats", text: "统计字数：你好 world", want: `"text":"你好 world"`},
		{name: "tokens", text: "计算token：hello world", want: `"text":"hello world"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeToolRunner{result: "ok"}
			router := NewIntentRouter(runner)
			_, handled := router.Route(&model.User{}, "u1", 100, tt.text)
			if !handled {
				t.Fatal("expected handled intent")
			}
			if !strings.Contains(runner.executedArgs, tt.want) {
				t.Fatalf("expected args to contain %s, got %s", tt.want, runner.executedArgs)
			}
		})
	}
}

func TestIntentRouter_LengthLimits(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	long := strings.Repeat("a", 50)
	tools := []string{"reset", "解绑", "年度报告地址", "获取当前绑定key", "查看key", "现在几点"}
	for _, tool := range tools {
		_, handled := router.Route(&model.User{}, "u1", 100, long+tool)
		if handled {
			t.Fatalf("expected %s with long prefix to be ignored", tool)
		}
	}
}

func TestIntentRouter_ResetBranches(t *testing.T) {
	tests := map[string]string{
		"reset":          "清空对话",
		"restart":        "重新开始",
		"forget":         "忘记刚才",
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &fakeToolRunner{result: "ok"}
			router := NewIntentRouter(runner)
			_, handled := router.Route(&model.User{}, "u1", 100, text)
			if !handled || runner.executedName != "reset_conversation" {
				t.Fatalf("expected reset, got %s handled=%v", runner.executedName, handled)
			}
		})
	}
}

func TestIntentRouter_DateQuery_Today(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "今天保存了哪些笔记")
	if !handled || runner.executedName != "query_notes_by_date" {
		t.Fatalf("unexpected: %s handled=%v", runner.executedName, handled)
	}
	if !strings.Contains(runner.executedArgs, timeutil.Date()) {
		t.Fatalf("expected today date, got %s", runner.executedArgs)
	}
}

func TestIntentRouter_DateQuery_NoDate(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "查询笔记")
	if handled {
		t.Fatal("expected no date query intent")
	}
}

func TestIntentRouter_DateCalculateBranches(t *testing.T) {
	tests := []struct {
		text  string
		delta int
	}{
		{"大后天是几号", 3},
		{"后天是几号", 2},
		{"前天是几号", -2},
		{"5天前是几号", -5},
		{"3天后是几号", 3},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			runner := &fakeToolRunner{result: "ok"}
			router := NewIntentRouter(runner)
			_, handled := router.Route(&model.User{}, "u1", 100, tt.text)
			if !handled || runner.executedName != "date_calculate" {
				t.Fatalf("unexpected: %s handled=%v", runner.executedName, handled)
			}
			want := fmt.Sprintf(`"days_delta":%d`, tt.delta)
			if !strings.Contains(runner.executedArgs, want) {
				t.Fatalf("expected %s, got %s", want, runner.executedArgs)
			}
		})
	}
}

func TestIntentRouter_BindBranches(t *testing.T) {
	tests := []struct {
		text     string
		wantKey  string
	}{
		{"绑定 mykey", "mykey"},
		{"/bind key: sk-test", "sk-test"},
		{"绑定我的喵滴 key：abc123", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			runner := &fakeToolRunner{result: "ok"}
			router := NewIntentRouter(runner)
			_, handled := router.Route(&model.User{}, "u1", 100, tt.text)
			if !handled || runner.executedName != "bind_miaodi_key" {
				t.Fatalf("unexpected: %s handled=%v", runner.executedName, handled)
			}
			var args map[string]string
			if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
				t.Fatalf("bad args: %v", err)
			}
			if args["key"] != tt.wantKey {
				t.Fatalf("expected key %q, got %q", tt.wantKey, args["key"])
			}
		})
	}
}

func TestIntentRouter_BindWithEmailRoutesToSendCode(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{Status: userStatusUnbound}, "u1", 100, "绑定邮箱 u@example.com")
	if !handled || runner.executedName != "send_miaodi_email_code" {
		t.Fatalf("unexpected: %s handled=%v", runner.executedName, handled)
	}
}

func TestIntentRouter_ParseChinesePathAlt(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "保存到书本：日记 章节：第三章 标题：周末")
	if !handled || runner.executedName != "set_save_path" {
		t.Fatalf("unexpected: %s handled=%v", runner.executedName, handled)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(runner.executedArgs), &args); err != nil {
		t.Fatalf("bad args: %v", err)
	}
	if args["book"] != "日记" || args["chapter"] != "第三章" {
		t.Fatalf("unexpected args: %+v", args)
	}
}

func TestIntentRouter_ImageNoURL(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "发一张图片")
	if handled {
		t.Fatal("expected image intent without url to be skipped")
	}
}

func TestIntentRouter_SaveTextNoContent(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "保存")
	if handled {
		t.Fatal("expected save text with no content to be skipped")
	}
	_, handled = router.Route(&model.User{}, "u1", 100, "今天天气真好")
	if handled {
		t.Fatal("expected non-save text to be skipped")
	}
}

func TestIntentRouter_SendEmail_NoBindingHintWhenBound(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusBound}
	_, handled := router.Route(user, "u1", 100, "联系我 u@example.com")
	if handled {
		t.Fatal("expected bound user with no binding hint to skip")
	}
}

func TestIntentRouter_EmailCode_NotWaitingNoCodeWord(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusUnbound}
	_, handled := router.Route(user, "u1", 100, "abc123")
	if handled {
		t.Fatal("expected alpha code without code word to skip when not waiting")
	}
}

func TestIntentRouter_NilRouter(t *testing.T) {
	var router *IntentRouter
	reply, handled := router.Route(&model.User{}, "u1", 100, "hello")
	if handled || reply != "" {
		t.Fatalf("expected no handling, got %q %v", reply, handled)
	}
}

func TestIntentRouter_EmptyText(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "  ")
	if handled {
		t.Fatal("expected empty text to skip")
	}
}

func TestIntentRouter_CalculateNoOperator(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "计算今天")
	if handled {
		t.Fatal("expected calculate without operator to skip")
	}
}

func TestIntentRouter_ChooseTooFewOptions(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "帮我选 A")
	if handled {
		t.Fatal("expected choose with one option to skip")
	}
}

func TestIntentRouter_TextStatsNoSeparator(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "统计字数你好")
	if handled {
		t.Fatal("expected text stats without separator to skip")
	}
}

func TestIntentRouter_TokenCountNoSeparator(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	_, handled := router.Route(&model.User{}, "u1", 100, "计算token")
	if handled {
		t.Fatal("expected token count without separator to skip")
	}
}

func TestIntentRouter_EmailCode_ExtractAlphaFromFields(t *testing.T) {
	runner := &fakeToolRunner{result: "ok"}
	router := NewIntentRouter(runner)
	user := &model.User{Status: userStatusWaitingEmailCode}
	_, handled := router.Route(user, "u1", 100, " /验证码 abcDEF123")
	if !handled || runner.executedName != "bind_miaodi_by_email_code" {
		t.Fatalf("unexpected: %s handled=%v", runner.executedName, handled)
	}
}

func TestIntentRouter_ToJSONString_Error(t *testing.T) {
	if got := toJSONString(make(chan int)); got != "{}" {
		t.Fatalf("unexpected: %s", got)
	}
}
