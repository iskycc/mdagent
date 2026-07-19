package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"miaodi-agent/internal/model"
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
		{name: "send email", text: "用邮箱 u@example.com 绑定喵滴", wantTool: "send_miaodi_email_code"},
		{name: "get key", text: "查看key", wantTool: "get_miaodi_key"},
		{name: "annual report", text: "年度报告地址", wantTool: "get_miaodi_annual_report"},
		{name: "unbind", text: "解除绑定", wantTool: "unbind_miaodi_key"},
		{name: "path", text: "把后续内容保存到《日记》第 3 章《今天》", wantTool: "set_save_path"},
		{name: "image", text: "保存图片 https://example.com/a.jpg", wantTool: "save_image_note"},
		{name: "save text", text: "保存：今天读完了第一章", wantTool: "save_text_note"},
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
	if !strings.Contains(runner.executedArgs, time.Now().AddDate(0, 0, -1).Format("2006-01-02")) {
		t.Fatalf("unexpected yesterday args: %s", runner.executedArgs)
	}
}
