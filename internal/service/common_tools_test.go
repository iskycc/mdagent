package service

import (
	"strings"
	"testing"

	"miaodi-agent/internal/model"
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

func TestCommonToolDefinitions(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range ToolDefinitions() {
		names[tool.Function.Name] = true
	}
	for _, name := range []string{"get_current_time", "calculate", "date_calculate", "random_number", "choose_option", "text_stats"} {
		if !names[name] {
			t.Fatalf("missing tool definition: %s", name)
		}
	}
}
