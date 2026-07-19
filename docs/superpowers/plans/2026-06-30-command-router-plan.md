# 智能帮助与快捷指令系统实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `miaodi-agent` 增加本地帮助与快捷指令系统，使用户输入 `帮助`、`绑定`、`路径`、`保存`、`重置` 等命令时无需经过 LLM 即可快速响应。

**Architecture:** 在 `Agent.ProcessMessage` 开头引入 `CommandRouter`；`CommandRouter` 按顺序匹配本地命令并直接调用现有 `ToolRunner` 或返回帮助文本；未命中命令时回退到原有 LLM tool-call 链路。新增 `ConversationRepo.Clear` 以支持重置会话。

**Tech Stack:** Go 1.26, 标准库, 现有 repository/service 层。

---

## 文件结构

| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/repository/conversation.go` | 修改 | 新增 `Clear(channelUserID, conversationID)` 方法，删除指定会话记录 |
| `internal/repository/conversation_test.go` | 修改 | 为 `Clear` 方法补充单元测试 |
| `internal/service/command_help.go` | 创建 | 集中维护 `HelpMessage` 常量 |
| `internal/service/command_router.go` | 创建 | 实现 `CommandRouter` 及 `Route` 方法 |
| `internal/service/command_router_test.go` | 创建 | `CommandRouter` 单元测试 |
| `internal/service/agent.go` | 修改 | `Agent` 结构体新增 `cmdRouter` 字段；`ProcessMessage` 先走命令路由 |
| `internal/service/agent_test.go` | 修改 | 补充命中命令时不调用 LLM、未命中时调用 LLM 的测试 |
| `internal/app/app.go` | 修改 | 创建 `CommandRouter` 并注入 `Agent` |

---

## Task 1: 为 ConversationRepo 增加 Clear 方法

**Files:**
- Modify: `internal/repository/conversation.go`
- Test: `internal/repository/conversation_test.go`

`ConversationStore` 接口目前缺少清空会话的能力。`重置` 命令需要它。

- [ ] **Step 1: 在 `internal/repository/conversation.go` 中新增 Clear 方法**

在 `CountTotal` 方法之后、`GetMessages` 之前插入：

```go
// Clear 删除指定会话记录
func (r *ConversationRepo) Clear(channelUserID string, conversationID int64) error {
	_, err := r.db.Exec(`
		DELETE FROM agent_conversations
		WHERE channel_user_id = ? AND conversation_id = ?`,
		channelUserID, conversationID)
	return err
}
```

- [ ] **Step 2: 在 `internal/repository/conversation_test.go` 中补充测试**

在文件末尾新增：

```go
func TestConversationRepo_Clear(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	repo := NewConversationRepo(db)
	mock.ExpectExec("DELETE FROM agent_conversations").
		WithArgs("u1", int64(100)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Clear("u1", 100); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
```

- [ ] **Step 3: 运行仓库层测试**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test ./internal/repository/ -v -run TestConversationRepo_Clear
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/repository/conversation.go internal/repository/conversation_test.go
git commit -m "feat: add ConversationRepo.Clear for reset command"
```

---

## Task 2: 创建帮助文本常量文件

**Files:**
- Create: `internal/service/command_help.go`
- Test: `internal/service/command_router_test.go`（后续任务引用）

- [ ] **Step 1: 创建 `internal/service/command_help.go`**

```go
package service

// HelpMessage 是用户请求帮助时返回的指令说明。
const HelpMessage = `🐱 喵滴助手可用指令：

绑定 <喵滴Key>         - 绑定你的喵滴 API Key
路径 <书> <章> <标题>   - 设置默认保存路径
保存 <内容>             - 保存文本笔记
重置                    - 清空当前会话
帮助                    - 显示本帮助
`
```

- [ ] **Step 2: 编译检查**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go build ./internal/service/
```

Expected: 成功，无输出

- [ ] **Step 3: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/service/command_help.go
git commit -m "feat: add HelpMessage constant"
```

---

## Task 3: 实现 CommandRouter

**Files:**
- Create: `internal/service/command_router.go`
- Test: `internal/service/command_router_test.go`

- [ ] **Step 1: 创建 `internal/service/command_router.go`**

```go
package service

import (
	"encoding/json"
	"strings"

	"miaodi-agent/internal/model"
)

// CommandRouter 负责识别并执行本地快捷指令，绕过 LLM。
type CommandRouter struct {
	userRepo UserStore
	convRepo ConversationStore
	toolExec ToolRunner
}

// NewCommandRouter 创建命令路由器。
func NewCommandRouter(userRepo UserStore, convRepo ConversationStore, toolExec ToolRunner) *CommandRouter {
	return &CommandRouter{
		userRepo: userRepo,
		convRepo: convRepo,
		toolExec: toolExec,
	}
}

// Route 尝试把用户输入识别为本地命令。
// 返回 (reply, handled)。handled=true 表示已处理，调用方直接返回 reply。
func (r *CommandRouter) Route(user *model.User, channelUserID string, conversationID int64, text string) (reply string, handled bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}

	lower := strings.ToLower(text)
	// 帮助命令：帮助、?、菜单、help
	if lower == "帮助" || lower == "?" || lower == "菜单" || lower == "help" {
		return HelpMessage, true
	}

	// 重置命令
	if lower == "重置" || lower == "/重置" || lower == "清空" || lower == "/清空" {
		if err := r.convRepo.Clear(channelUserID, conversationID); err != nil {
			return "❌ 清空会话失败，请稍后再试", true
		}
		return "✅ 已清空当前会话", true
	}

	// 绑定命令
	if strings.HasPrefix(lower, "绑定") || strings.HasPrefix(lower, "/绑定") {
		key := extractLastToken(text)
		if key == "" {
			return "❌ 请提供喵滴 Key，例如：绑定 xxx", true
		}
		args, _ := json.Marshal(map[string]string{"key": key})
		res := r.toolExec.Execute(user, channelUserID, "bind_miaodi_key", string(args))
		return formatReceipt(res), true
	}

	// 路径命令
	if strings.HasPrefix(lower, "路径") || strings.HasPrefix(lower, "/路径") {
		parts := splitCommandArgs(text)
		if len(parts) < 4 {
			return "❌ 用法：路径 <书> <章> <标题>", true
		}
		args, _ := json.Marshal(map[string]string{
			"book":    parts[1],
			"chapter": parts[2],
			"title":   parts[3],
		})
		res := r.toolExec.Execute(user, channelUserID, "set_save_path", string(args))
		return formatReceipt(res), true
	}

	// 保存命令
	if strings.HasPrefix(lower, "保存") || strings.HasPrefix(lower, "/保存") {
		content := extractAfterCommand(text)
		if content == "" {
			return "❌ 请提供要保存的内容，例如：保存 今天的心情", true
		}
		args, _ := json.Marshal(map[string]string{"content": content})
		res := r.toolExec.Execute(user, channelUserID, "save_text_note", string(args))
		return formatReceipt(res), true
	}

	return "", false
}

// extractLastToken 提取字符串中最后一个非空 token，用于绑定 key。
func extractLastToken(s string) string {
	fields := strings.Fields(s)
	for i := len(fields) - 1; i >= 0; i-- {
		t := strings.Trim(fields[i], "：:")
		if t != "" {
			return t
		}
	}
	return ""
}

// splitCommandArgs 按空白拆分命令参数，保留原大小写。
func splitCommandArgs(s string) []string {
	return strings.Fields(s)
}

// extractAfterCommand 取命令关键字后的所有内容作为参数。
func extractAfterCommand(s string) string {
	s = strings.TrimSpace(s)
	idx := strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t'
	})
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(s[idx+1:])
}

// formatReceipt 把工具返回的文本包装成更友好的回执。
func formatReceipt(result string) string {
	if result == "" {
		return "❌ 操作失败，无返回结果"
	}
	// 已有 emoji 前缀的保留原样
	if strings.HasPrefix(result, "✅") || strings.HasPrefix(result, "❌") {
		return result
	}
	return "✅ " + result
}
```

注意：当前 `ConversationStore` 接口没有 `Clear` 方法，需要在 Task 1 完成后，同步修改 `internal/service/agent.go` 中的 `ConversationStore` 接口定义，增加：

```go
Clear(channelUserID string, conversationID int64) error
```

- [ ] **Step 2: 编译检查**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go build ./internal/service/
```

Expected: 成功，无输出

- [ ] **Step 3: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/service/command_router.go
git commit -m "feat: implement CommandRouter for local shortcuts"
```

---

## Task 4: 为 CommandRouter 编写单元测试

**Files:**
- Create: `internal/service/command_router_test.go`

- [ ] **Step 1: 创建 `internal/service/command_router_test.go`**

```go
package service

import (
	"testing"

	"miaodi-agent/internal/model"
	"miaodi-agent/pkg/openai"
)

type fakeToolRunnerForRouter struct {
	executedName string
	executedArgs string
	result       string
}

func (f *fakeToolRunnerForRouter) Execute(user *model.User, channelUserID, name, arguments string) string {
	f.executedName = name
	f.executedArgs = arguments
	return f.result
}

type fakeConversationStoreForRouter struct {
	cleared bool
	clearID int64
}

func (f *fakeConversationStoreForRouter) GetMessages(channelUserID string, conversationID int64) ([]openai.ChatMessage, error) {
	return nil, nil
}

func (f *fakeConversationStoreForRouter) AppendMessage(channelUserID string, conversationID int64, msg openai.ChatMessage) error {
	return nil
}

func (f *fakeConversationStoreForRouter) AppendMessages(channelUserID string, conversationID int64, msgs ...openai.ChatMessage) error {
	return nil
}

func (f *fakeConversationStoreForRouter) Clear(channelUserID string, conversationID int64) error {
	f.cleared = true
	f.clearID = conversationID
	return nil
}

func newTestUser() *model.User {
	return &model.User{
		ChannelUserID: "u1",
		Status:        "bound",
		APIKey:        "key1",
		Book:          "b",
		Chara:         "c",
		Title:         "t",
	}
}

func TestCommandRouter_Help(t *testing.T) {
	router := NewCommandRouter(nil, nil, nil)
	for _, input := range []string{"帮助", "?", "菜单", "help", "Help"} {
		reply, handled := router.Route(nil, "u1", 100, input)
		if !handled {
			t.Errorf("%s should be handled", input)
		}
		if reply != HelpMessage {
			t.Errorf("%s: unexpected reply: %s", input, reply)
		}
	}
}

func TestCommandRouter_Reset(t *testing.T) {
	store := &fakeConversationStoreForRouter{}
	router := NewCommandRouter(nil, store, nil)
	reply, handled := router.Route(newTestUser(), "u1", 100, "重置")
	if !handled {
		t.Error("reset should be handled")
	}
	if reply != "✅ 已清空当前会话" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if !store.cleared || store.clearID != 100 {
		t.Errorf("clear not called correctly: cleared=%v id=%d", store.cleared, store.clearID)
	}
}

func TestCommandRouter_Bind(t *testing.T) {
	runner := &fakeToolRunnerForRouter{result: "绑定成功"}
	router := NewCommandRouter(nil, nil, runner)
	for _, input := range []string{"绑定 key123", "/绑定 key123", "绑定我的喵滴 key：key123"} {
		reply, handled := router.Route(newTestUser(), "u1", 100, input)
		if !handled {
			t.Errorf("%s should be handled", input)
		}
		if runner.executedName != "bind_miaodi_key" {
			t.Errorf("%s: expected bind_miaodi_key, got %s", input, runner.executedName)
		}
		if reply != "✅ 绑定成功" {
			t.Errorf("%s: unexpected reply: %s", input, reply)
		}
	}
}

func TestCommandRouter_Bind_MissingKey(t *testing.T) {
	router := NewCommandRouter(nil, nil, nil)
	reply, handled := router.Route(newTestUser(), "u1", 100, "绑定")
	if !handled {
		t.Error("bind missing key should be handled")
	}
	if reply != "❌ 请提供喵滴 Key，例如：绑定 xxx" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestCommandRouter_SetPath(t *testing.T) {
	runner := &fakeToolRunnerForRouter{result: "保存路径已设置"}
	router := NewCommandRouter(nil, nil, runner)
	reply, handled := router.Route(newTestUser(), "u1", 100, "路径 我的书 第一章 开场")
	if !handled {
		t.Error("set path should be handled")
	}
	if runner.executedName != "set_save_path" {
		t.Errorf("expected set_save_path, got %s", runner.executedName)
	}
	if reply != "✅ 保存路径已设置" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestCommandRouter_SetPath_TooFewArgs(t *testing.T) {
	router := NewCommandRouter(nil, nil, nil)
	reply, handled := router.Route(newTestUser(), "u1", 100, "路径 我的书")
	if !handled {
		t.Error("set path with few args should be handled")
	}
	if reply != "❌ 用法：路径 <书> <章> <标题>" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestCommandRouter_SaveText(t *testing.T) {
	runner := &fakeToolRunnerForRouter{result: "已保存"}
	router := NewCommandRouter(nil, nil, runner)
	reply, handled := router.Route(newTestUser(), "u1", 100, "保存 今天天气不错")
	if !handled {
		t.Error("save text should be handled")
	}
	if runner.executedName != "save_text_note" {
		t.Errorf("expected save_text_note, got %s", runner.executedName)
	}
	if reply != "✅ 已保存" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestCommandRouter_SaveText_MissingContent(t *testing.T) {
	router := NewCommandRouter(nil, nil, nil)
	reply, handled := router.Route(newTestUser(), "u1", 100, "保存")
	if !handled {
		t.Error("save text missing content should be handled")
	}
	if reply != "❌ 请提供要保存的内容，例如：保存 今天的心情" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestCommandRouter_Unhandled(t *testing.T) {
	router := NewCommandRouter(nil, nil, nil)
	reply, handled := router.Route(newTestUser(), "u1", 100, "随便聊聊")
	if handled {
		t.Error("plain text should not be handled")
	}
	if reply != "" {
		t.Errorf("expected empty reply, got %s", reply)
	}
}

func TestExtractLastToken(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"绑定 key123", "key123"},
		{"绑定我的喵滴 key：abc", "abc"},
		{"绑定", ""},
	}
	for _, c := range cases {
		got := extractLastToken(c.input)
		if got != c.want {
			t.Errorf("extractLastToken(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestExtractAfterCommand(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"保存 今天天气不错", "今天天气不错"},
		{"保存   多个空格", "多个空格"},
		{"保存", ""},
	}
	for _, c := range cases {
		got := extractAfterCommand(c.input)
		if got != c.want {
			t.Errorf("extractAfterCommand(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestFormatReceipt(t *testing.T) {
	if formatReceipt("ok") != "✅ ok" {
		t.Errorf("unexpected format: %s", formatReceipt("ok"))
	}
	if formatReceipt("✅ done") != "✅ done" {
		t.Errorf("unexpected format: %s", formatReceipt("✅ done"))
	}
	if formatReceipt("") != "❌ 操作失败，无返回结果" {
		t.Errorf("unexpected format: %s", formatReceipt(""))
	}
}
```

- [ ] **Step 2: 运行 CommandRouter 测试**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test ./internal/service/ -v -run TestCommandRouter
```

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/service/command_router_test.go
git commit -m "test: add CommandRouter unit tests"
```

---

## Task 5: 修改 Agent 接入 CommandRouter

**Files:**
- Modify: `internal/service/agent.go`

- [ ] **Step 1: 更新 `ConversationStore` 接口，增加 Clear 方法**

在 `internal/service/agent.go` 中找到：

```go
type ConversationStore interface {
	GetMessages(channelUserID string, conversationID int64) ([]openai.ChatMessage, error)
	AppendMessage(channelUserID string, conversationID int64, msg openai.ChatMessage) error
	AppendMessages(channelUserID string, conversationID int64, msgs ...openai.ChatMessage) error
}
```

替换为：

```go
type ConversationStore interface {
	GetMessages(channelUserID string, conversationID int64) ([]openai.ChatMessage, error)
	AppendMessage(channelUserID string, conversationID int64, msg openai.ChatMessage) error
	AppendMessages(channelUserID string, conversationID int64, msgs ...openai.ChatMessage) error
	Clear(channelUserID string, conversationID int64) error
}
```

- [ ] **Step 2: Agent 结构体新增 cmdRouter 字段，修改构造函数**

修改 `Agent` 结构体：

```go
type Agent struct {
	llm       LLMClient
	model     string
	userRepo  UserStore
	convRepo  ConversationStore
	toolExec  ToolRunner
	cmdRouter *CommandRouter
}
```

修改 `NewAgent` 以初始化 `cmdRouter`：

```go
func NewAgent(llm LLMClient, modelName string, userRepo UserStore, convRepo ConversationStore, toolExec ToolRunner) *Agent {
	return &Agent{
		llm:       llm,
		model:     modelName,
		userRepo:  userRepo,
		convRepo:  convRepo,
		toolExec:  toolExec,
		cmdRouter: NewCommandRouter(userRepo, convRepo, toolExec),
	}
}
```

- [ ] **Step 3: 在 ProcessMessage 开头加入命令路由**

在 `ProcessMessage` 中，获取 `user` 之后、追加用户消息到历史之前，插入：

```go
	// 先尝试本地快捷指令
	if reply, handled := a.cmdRouter.Route(user, channelUserID, conversationID, payload.Message.Content); handled {
		return reply
	}
```

最终 `ProcessMessage` 开头大致如下：

```go
func (a *Agent) ProcessMessage(ctx context.Context, payload *model.CallbackPayload) string {
	channelUserID := payload.User.UserID
	conversationID := payload.Conversation.ID

	user, err := a.userRepo.GetOrCreate(channelUserID)
	if err != nil {
		log.Printf("get or create user failed: %v", err)
		return "系统内部错误，请稍后再试"
	}

	// 先尝试本地快捷指令
	if reply, handled := a.cmdRouter.Route(user, channelUserID, conversationID, payload.Message.Content); handled {
		return reply
	}

	// 追加用户消息到历史
	userMsg := openai.ChatMessage{Role: "user", Content: payload.Message.Content}
	...
```

- [ ] **Step 4: 编译检查**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go build ./internal/service/
```

Expected: 成功，无输出

- [ ] **Step 5: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/service/agent.go
git commit -m "feat: wire CommandRouter into Agent"
```

---

## Task 6: 更新 Agent 单元测试

**Files:**
- Modify: `internal/service/agent_test.go`

- [ ] **Step 1: 让 fakeConversationStore 实现 Clear 方法**

在 `internal/service/agent_test.go` 的 `fakeConversationStore` 定义后新增方法：

```go
func (f *fakeConversationStore) Clear(channelUserID string, conversationID int64) error {
	f.messages = nil
	return nil
}
```

- [ ] **Step 2: 新增命中命令时不调用 LLM 的测试**

在文件末尾新增：

```go
func TestAgent_ProcessMessage_CommandRouter_Help(t *testing.T) {
	llm := &fakeLLM{}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), &model.CallbackPayload{
		Conversation: struct{ ID int64 }{ID: 100},
		User:         struct{ UserID, Username string }{UserID: "u1", Username: "*"},
		Message:      struct{ ID int64; Content, CreateTime string }{Content: "帮助"},
	})
	if reply != HelpMessage {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 0 {
		t.Errorf("expected no llm call, got %d", llm.callCount)
	}
}

func TestAgent_ProcessMessage_CommandRouter_Bind(t *testing.T) {
	llm := &fakeLLM{}
	runner := &fakeToolRunner{result: "绑定成功"}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)
	reply := agent.ProcessMessage(context.Background(), &model.CallbackPayload{
		Conversation: struct{ ID int64 }{ID: 100},
		User:         struct{ UserID, Username string }{UserID: "u1", Username: "*"},
		Message:      struct{ ID int64; Content, CreateTime string }{Content: "绑定 key123"},
	})
	if reply != "✅ 绑定成功" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 0 {
		t.Errorf("expected no llm call, got %d", llm.callCount)
	}
}

func TestAgent_ProcessMessage_CommandRouter_FallbackToLLM(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("hello")}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), &model.CallbackPayload{
		Conversation: struct{ ID int64 }{ID: 100},
		User:         struct{ UserID, Username string }{UserID: "u1", Username: "*"},
		Message:      struct{ ID int64; Content, CreateTime string }{Content: "随便说点什么"},
	})
	if reply != "hello" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 1 {
		t.Errorf("expected 1 llm call, got %d", llm.callCount)
	}
}
```

注意：`model.CallbackPayload` 是值类型，构造测试数据时需要补全必要字段，可仿照 `newTestPayload()`。

- [ ] **Step 3: 运行 Agent 测试**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test ./internal/service/ -v -run TestAgent_ProcessMessage
```

Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/service/agent_test.go
git commit -m "test: add Agent command routing tests"
```

---

## Task 7: 在 app.go 中无需改动（已自动注入）

`Agent` 构造函数内部已经创建 `CommandRouter`，`app.go` 不需要额外改动。只需确认 `app.go` 中的仓库实现了 `ConversationStore` 接口即可。

- [ ] **Step 1: 编译整个应用**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go build ./...
```

Expected: 成功，无输出

- [ ] **Step 2: 提交（如需要）**

如果无改动，无需提交。

---

## Task 8: 全量测试与覆盖率检查

**Files:**
- All

- [ ] **Step 1: 运行全部测试**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test ./...
```

Expected: 全部 PASS

- [ ] **Step 2: 检查覆盖率**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -n 1
```

Expected: total ≥ 90.0%

- [ ] **Step 3: 清理覆盖率文件**

Run:
```bash
cd /opt/mdagent/miaodi-agent && rm -f coverage.out
```

- [ ] **Step 4: 运行 go vet**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go vet ./...
```

Expected: 无输出（成功）

- [ ] **Step 5: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add -A
git commit -m "test: verify full suite passes with command router"
```

---

## Task 9: 更新 README 说明

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 在 README 的"支持的模型能力（tools）"之后新增"快捷指令"小节**

```markdown
## 快捷指令

除了自然语言对话，你也可以直接发送以下指令，Bot 会立即响应：

| 指令 | 示例 | 说明 |
|---|---|---|
| `帮助` / `?` / `菜单` | `帮助` | 显示可用指令 |
| `绑定 <key>` | `绑定 abc123` | 绑定喵滴 API Key |
| `路径 <书> <章> <标题>` | `路径 日记 6月 今天` | 设置保存路径 |
| `保存 <内容>` | `保存 今天天气不错` | 保存文本笔记 |
| `重置` | `重置` | 清空当前会话历史 |

指令不区分大小写，也支持 `/` 前缀，例如 `/绑定 abc123`。
```

- [ ] **Step 2: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add README.md
git commit -m "docs: document command shortcuts in README"
```

---

## Self-Review Checklist

1. **Spec coverage**
   - [x] `帮助` / `?` / `菜单` / `help` 返回帮助文本 → Task 3 + Task 4
   - [x] `绑定 <key>` / `/绑定 <key>` / 自然语言前缀 → Task 3 + Task 4
   - [x] `路径 <book> <chapter> <title>` / `/路径 ...` → Task 3 + Task 4
   - [x] `保存 <内容>` / `/保存 <内容>` → Task 3 + Task 4
   - [x] `重置` / `/重置` / `清空` → Task 1 + Task 3 + Task 4
   - [x] 未命中命令走 LLM → Task 5 + Task 6
   - [x] 覆盖率 ≥ 90% → Task 8

2. **Placeholder scan**
   - [x] 无 TBD/TODO/"implement later"
   - [x] 每个测试步骤都包含实际代码
   - [x] 所有命令和期望输出明确

3. **Type consistency**
   - [x] `ConversationStore` 接口在 Task 5 中新增 `Clear` 方法，与 Task 1 的 `ConversationRepo.Clear` 签名一致
   - [x] `CommandRouter.Route` 签名带 `conversationID`，调用处（Agent.ProcessMessage、测试）一致
   - [x] `CommandRouter` 构造函数签名在 Task 3 和 Task 5 中一致
   - [x] `fakeConversationStore` 在 Task 6 中实现 `Clear` 以匹配接口
   - [x] `command_router_test.go` 已导入 `miaodi-agent/pkg/openai` 以使用 `openai.ChatMessage`
