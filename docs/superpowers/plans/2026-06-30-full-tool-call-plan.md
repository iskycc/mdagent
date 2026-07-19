# 全面 tool-call 化实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除本地命令路由，把所有用户操作（包括帮助、重置、查询历史）都改为 LLM tool-call，并扩展工具集支持自然语言交互。

**Architecture:** 移除 `CommandRouter`，在 `ToolExecutor` 中新增 `reset_conversation`、`show_help`、`list_recent_notes`、`query_notes_by_date` 工具；`Agent.ProcessMessage` 完全依赖 LLM tool-call 决策；`buildSystemPrompt` 更新以反映新的工具集合和自然语言输入要求。

**Tech Stack:** Go 1.26, 标准库, sqlmock, 现有 repository/service 层。

---

## 文件结构

| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/service/command_router.go` | 删除 | 本地命令路由 |
| `internal/service/command_help.go` | 删除 | 帮助文本常量 |
| `internal/service/command_router_test.go` | 删除 | 命令路由测试 |
| `internal/repository/call_log.go` | 修改 | 新增 `RecentByUser` 和 `ByDate` 查询 |
| `internal/repository/call_log_test.go` | 修改 | 为新增查询方法补测试 |
| `internal/service/miaodi_tool.go` | 修改 | 扩展 `ToolDefinitions` 和 `Execute` |
| `internal/service/miaodi_tool_test.go` | 修改 | 新增工具单元测试 |
| `internal/service/agent.go` | 修改 | 删除 `cmdRouter` 和路由逻辑，更新 system prompt |
| `internal/service/agent_test.go` | 修改 | 删除命令路由测试，新增工具集成测试 |
| `README.md` | 修改 | 移除"快捷指令"小节，改为自然语言说明 |

---

## Task 1: 删除本地命令路由文件

**Files:**
- Delete: `internal/service/command_router.go`
- Delete: `internal/service/command_help.go`
- Delete: `internal/service/command_router_test.go`

- [ ] **Step 1: 删除三个文件**

```bash
cd /opt/mdagent/miaodi-agent
rm internal/service/command_router.go internal/service/command_help.go internal/service/command_router_test.go
```

- [ ] **Step 2: 编译检查（预期会失败，因为 agent.go 仍引用 CommandRouter）**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go build ./internal/service/
```

Expected: 编译失败，提示 `CommandRouter` / `cmdRouter` 未定义。这是正常的，Task 3 会修复。不要尝试修复编译错误，直接提交删除操作。

- [ ] **Step 3: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add -A
git commit -m "refactor: remove local CommandRouter"
```

---

## Task 2: 为 api_call_log 增加用户级查询方法

**Files:**
- Modify: `internal/repository/call_log.go`
- Test: `internal/repository/call_log_test.go`

`list_recent_notes` 和 `query_notes_by_date` 需要查询当前用户的调用日志。

- [ ] **Step 1: 在 `internal/repository/call_log.go` 中新增两个方法**

在 `ActionStats` 方法之后追加：

```go
// RecentByUser 查询指定用户最近 N 条调用记录（按时间倒序）
func (r *CallLogRepo) RecentByUser(channelUserID string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	rows, err := r.db.Query(`
		SELECT action, created_at
		FROM api_call_log
		WHERE channel_user_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, channelUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var action string
		var createdAt time.Time
		if err := rows.Scan(&action, &createdAt); err != nil {
			continue
		}
		results = append(results, map[string]interface{}{
			"action":     action,
			"created_at": createdAt.Format("2006-01-02 15:04"),
		})
	}
	return results, nil
}

// ByDate 查询指定用户某一天的调用记录
func (r *CallLogRepo) ByDate(channelUserID, date string) ([]map[string]interface{}, error) {
	rows, err := r.db.Query(`
		SELECT action, created_at
		FROM api_call_log
		WHERE channel_user_id = ? AND DATE(created_at) = ?
		ORDER BY created_at DESC`, channelUserID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var action string
		var createdAt time.Time
		if err := rows.Scan(&action, &createdAt); err != nil {
			continue
		}
		results = append(results, map[string]interface{}{
			"action":     action,
			"created_at": createdAt.Format("2006-01-02 15:04"),
		})
	}
	return results, nil
}
```

- [ ] **Step 2: 在 `internal/repository/call_log_test.go` 中补充测试**

在文件末尾新增：

```go
func TestCallLogRepo_RecentByUser(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", "2026-06-30 10:00:00").
		AddRow("save_image_pending", "2026-06-30 09:00:00")
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).
		WithArgs("u1", 5).WillReturnRows(rows)

	results, err := r.RecentByUser("u1", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 rows, got %d", len(results))
	}
}

func TestCallLogRepo_RecentByUser_OverLimit(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).
		WithArgs("u1", 20).WillReturnRows(sqlmock.NewRows([]string{"action", "created_at"}))

	_, err := r.RecentByUser("u1", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallLogRepo_ByDate(t *testing.T) {
	r, mock := newCallLogRepoMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", "2026-06-30 10:00:00")
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND DATE\(created_at\) = \?`).
		WithArgs("u1", "2026-06-30").WillReturnRows(rows)

	results, err := r.ByDate("u1", "2026-06-30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 row, got %d", len(results))
	}
}
```

- [ ] **Step 3: 运行仓库层测试**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test ./internal/repository/ -v -run TestCallLogRepo
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/repository/call_log.go internal/repository/call_log_test.go
git commit -m "feat: add RecentByUser and ByDate query for call logs"
```

---

## Task 3: 修改 Agent，删除命令路由

**Files:**
- Modify: `internal/service/agent.go`

- [ ] **Step 1: 删除 `cmdRouter` 字段和初始化**

修改 `Agent` 结构体为：

```go
type Agent struct {
	llm      LLMClient
	model    string
	userRepo UserStore
	convRepo ConversationStore
	toolExec ToolRunner
}
```

修改 `NewAgent` 为：

```go
func NewAgent(llm LLMClient, modelName string, userRepo UserStore, convRepo ConversationStore, toolExec ToolRunner) *Agent {
	return &Agent{
		llm:      llm,
		model:    modelName,
		userRepo: userRepo,
		convRepo: convRepo,
		toolExec: toolExec,
	}
}
```

- [ ] **Step 2: 删除 ProcessMessage 中的命令路由逻辑**

删除这一段：

```go
	// 先尝试本地快捷指令
	if reply, handled := a.cmdRouter.Route(user, channelUserID, conversationID, payload.Message.Content); handled {
		return reply
	}
```

`ProcessMessage` 获取 user 后直接追加用户消息到历史。

- [ ] **Step 3: 更新 buildSystemPrompt**

将 `buildSystemPrompt` 替换为：

```go
func buildSystemPrompt(user *model.User) string {
	status := "未绑定"
	if user.Status == "bound" {
		status = "已绑定"
	}
	title := user.Title
	if title == "" || title == "null" {
		title = time.Now().Format("2006-01-02")
	}

	return fmt.Sprintf(`你是“喵滴 AI 助手”，通过传送鸽为用户服务。用户可以用自然语言与你交流，不需要固定格式。你拥有以下工具：

1. bind_miaodi_key(key): 绑定喵滴 API Key。
2. set_save_path(book, chapter, title): 设置后续保存笔记的路径。
3. get_user_profile(): 查看当前绑定状态和保存路径。
4. save_text_note(content, title?): 把文本保存到喵滴笔记。
5. save_image_note(image_url, title?): 把图片链接写入待上传队列（由后台定时任务扫描后上传到喵滴），不要直接上传。
6. reset_conversation(): 清空当前会话历史。
7. show_help(): 返回你能提供的能力说明。
8. list_recent_notes(limit?): 列出最近保存的笔记摘要，limit 默认 5，最大 20。
9. query_notes_by_date(date): 按日期查询已保存笔记，date 格式为 YYYY-MM-DD。

当前用户状态：
- 绑定状态：%s
- 默认保存路径：书本《%s》/ 章节《%s》/ 标题《%s》

注意事项：
- 如果用户没有绑定喵滴 Key，请先引导绑定。
- 如果用户想保存图片或发给你图片链接，请调用 save_image_note，不要直接调用喵滴上传接口。
- 如果用户说“清空”、“重置”、“忘记刚才的对话”等，调用 reset_conversation。
- 如果用户问“你能做什么”、“怎么用”等，调用 show_help。
- 如果用户问“最近保存了什么”、“我昨天保存了什么”等，调用 list_recent_notes 或 query_notes_by_date。
- 回复要简洁自然，控制在 200 字以内。`, status, user.Book, user.Chara, title)
}
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
git commit -m "refactor: remove cmdRouter from Agent and update system prompt"
```

---

## Task 4: 扩展 ToolExecutor 工具集

**Files:**
- Modify: `internal/service/miaodi_tool.go`

- [ ] **Step 1: 修改 ToolExecutor 结构体，增加 callLogRepo 作为历史查询依赖**

当前 `ToolExecutor` 已经有 `callLogRepo` 字段，无需新增。但需要让 `callLogRepo` 支持查询接口。

由于 `callLogRepo` 类型是 `*repository.CallLogRepo`，新增方法 `RecentByUser` 和 `ByDate` 已在 Task 2 完成，这里直接使用。

- [ ] **Step 2: 在 ToolDefinitions() 中新增工具定义**

在 `save_image_note` 定义之后追加：

```go
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
```

- [ ] **Step 3: 修改 ToolRunner 接口以传递 conversationID**

由于 reset 工具需要 `conversationID`，必须修改接口。

修改 `internal/service/agent.go` 中的 `ToolRunner` 接口：

```go
type ToolRunner interface {
	Execute(user *model.User, channelUserID string, conversationID int64, name, arguments string) string
}
```

修改 `internal/service/miaodi_tool.go` 中的 `Execute` 方法签名：

```go
func (e *ToolExecutor) Execute(user *model.User, channelUserID string, conversationID int64, name string, arguments string) string
```

修改 `internal/service/agent.go` 中调用 `toolExec.Execute` 的地方：

```go
result := a.toolExec.Execute(user, channelUserID, conversationID, tc.Function.Name, tc.Function.Arguments)
```

- [ ] **Step 4: 在 Execute 中增加新分支**

在 switch 中 `default` 之前添加：

```go
	case "reset_conversation":
		return e.resetConversation(user, channelUserID, conversationID, arguments)
	case "show_help":
		return e.showHelp()
	case "list_recent_notes":
		return e.listRecentNotes(channelUserID, arguments)
	case "query_notes_by_date":
		return e.queryNotesByDate(channelUserID, arguments)
```

- [ ] **Step 5: 修改 ToolExecutor 结构体，增加 convRepo 字段**

```go
type ToolExecutor struct {
	miaodi      MiaodiClient
	userRepo    *repository.UserRepo
	convRepo    ConversationStore
	pendingRepo *repository.PendingImageRepo
	callLogRepo *repository.CallLogRepo
}
```

修改 `NewToolExecutor` 签名以接收 `convRepo`：

```go
func NewToolExecutor(miaodi MiaodiClient, userRepo *repository.UserRepo, convRepo ConversationStore, pendingRepo *repository.PendingImageRepo, callLogRepo *repository.CallLogRepo) *ToolExecutor {
	return &ToolExecutor{
		miaodi:      miaodi,
		userRepo:    userRepo,
		convRepo:    convRepo,
		pendingRepo: pendingRepo,
		callLogRepo: callLogRepo,
	}
}
```

这里 `ConversationStore` 接口已在 `agent.go` 中定义，由于 `miaodi_tool.go` 和 `agent.go` 同包，可以直接使用。

- [ ] **Step 6: 实现 reset_conversation 和其他新工具**

在 `miaodi_tool.go` 中添加：

```go
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
- 设置保存路径（书/章/标题）
- 保存文本笔记
- 保存图片到待上传队列
- 查看当前绑定状态和路径
- 查询最近保存的笔记
- 按日期查询笔记
- 清空当前会话

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
	if len(logs) == 0 {
		return "最近没有保存记录。"
	}
	var sb strings.Builder
	sb.WriteString("最近保存记录：\n")
	for _, log := range logs {
		action, _ := log["action"].(string)
		createdAt, _ := log["created_at"].(string)
		sb.WriteString(fmt.Sprintf("- %s %s\n", createdAt, action))
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
	if len(logs) == 0 {
		return fmt.Sprintf("%s 没有保存记录。", args.Date)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s 的保存记录：\n", args.Date))
	for _, log := range logs {
		action, _ := log["action"].(string)
		createdAt, _ := log["created_at"].(string)
		sb.WriteString(fmt.Sprintf("- %s %s\n", createdAt, action))
	}
	return sb.String()
}
```

注意：需要在 `miaodi_tool.go` 顶部 import `strings`。

- [ ] **Step 7: 更新 app.go 中 NewToolExecutor 调用**

修改 `internal/app/app.go`：

```go
toolExec := service.NewToolExecutor(miaodi, userRepo, convRepo, pendingRepo, callLogRepo)
```

（原代码为 `service.NewToolExecutor(miaodi, userRepo, pendingRepo, callLogRepo)`，需要插入 `convRepo`。）

- [ ] **Step 8: 编译检查**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go build ./...
```

Expected: 成功，无输出

- [ ] **Step 9: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add -A
git commit -m "feat: extend ToolExecutor with reset, help, list/query notes tools"
```

---

## Task 5: 更新 Agent 测试

**Files:**
- Modify: `internal/service/agent_test.go`

- [ ] **Step 1: 更新 fakeToolRunner 签名**

将：

```go
type fakeToolRunner struct {
	result string
}

func (f *fakeToolRunner) Execute(user *model.User, channelUserID, name, arguments string) string {
	return f.result
}
```

改为：

```go
type fakeToolRunner struct {
	result string
}

func (f *fakeToolRunner) Execute(user *model.User, channelUserID string, conversationID int64, name, arguments string) string {
	return f.result
}
```

- [ ] **Step 2: 删除命令路由相关测试**

删除以下三个测试：
- `TestAgent_ProcessMessage_CommandRouter_Help`
- `TestAgent_ProcessMessage_CommandRouter_Bind`
- `TestAgent_ProcessMessage_CommandRouter_FallbackToLLM`

- [ ] **Step 3: 新增 reset/help/list/query 工具的 Agent 集成测试**

在文件末尾新增：

```go
func TestAgent_ProcessMessage_ResetTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("reset_conversation", "{}"),
		makeTextResponse("已经清空"),
	}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "已清空"})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "已经清空" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 2 {
		t.Errorf("expected 2 llm calls, got %d", llm.callCount)
	}
}

func TestAgent_ProcessMessage_HelpTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("show_help", "{}"),
		makeTextResponse("我可以帮你..."),
	}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "帮助内容"})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "我可以帮你..." {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_ListRecentNotesTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("list_recent_notes", `{"limit":5}`),
		makeTextResponse("最近你保存了..."),
	}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "最近记录"})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "最近你保存了..." {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_QueryNotesByDateTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("query_notes_by_date", `{"date":"2026-06-30"}`),
		makeTextResponse("那天的记录是..."),
	}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "日期记录"})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "那天的记录是..." {
		t.Errorf("unexpected reply: %s", reply)
	}
}
```

- [ ] **Step 4: 运行 Agent 测试**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test ./internal/service/ -v -run TestAgent_ProcessMessage
```

Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/service/agent_test.go
git commit -m "test: update Agent tests for full tool-call flow"
```

---

## Task 6: 更新 ToolExecutor 单元测试

**Files:**
- Modify: `internal/service/miaodi_tool_test.go`

- [ ] **Step 1: 更新 fakeMiaodi 和 newToolExecutorMock**

当前 `newToolExecutorMock` 需要增加 `convRepo` 参数。修改如下：

```go
func newToolExecutorMock(t *testing.T) (*ToolExecutor, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	userRepo := repository.NewUserRepo(db)
	convRepo := repository.NewConversationRepo(db)
	pendingRepo := repository.NewPendingImageRepo(db)
	logRepo := repository.NewCallLogRepo(db)
	miaodi := &fakeMiaodi{checkResult: true, putResult: map[string]interface{}{"code": 20000}}
	return NewToolExecutor(miaodi, userRepo, convRepo, pendingRepo, logRepo), mock
}
```

- [ ] **Step 2: 新增 reset/show_help/list/query 工具测试**

在文件末尾新增：

```go
func TestToolExecutor_resetConversation(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("DELETE FROM agent_conversations").WithArgs("u1", int64(100)).WillReturnResult(sqlmock.NewResult(0, 1))

	user := &model.User{ChannelUserID: "u1"}
	res := exec.Execute(user, "u1", 100, "reset_conversation", "{}")
	if res != "已清空当前会话，我们可以重新开始。" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_showHelp(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 100, "show_help", "{}")
	if res == "" {
		t.Error("expected help content")
	}
}

func TestToolExecutor_listRecentNotes(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", "2026-06-30 10:00:00")
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnRows(rows)

	res := exec.Execute(&model.User{}, "u1", 100, "list_recent_notes", "{}")
	if res == "" || res == "查询失败" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_queryNotesByDate(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", "2026-06-30 10:00:00")
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND DATE\(created_at\) = \?`).
		WithArgs("u1", "2026-06-30").WillReturnRows(rows)

	res := exec.Execute(&model.User{}, "u1", 100, "query_notes_by_date", `{"date":"2026-06-30"}`)
	if res == "" || res == "查询失败" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_queryNotesByDate_MissingDate(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 100, "query_notes_by_date", "{}")
	if res != "date 不能为空" {
		t.Errorf("unexpected result: %s", res)
	}
}
```

- [ ] **Step 3: 运行 ToolExecutor 测试**

Run:
```bash
cd /opt/mdagent/miaodi-agent && go test ./internal/service/ -v -run TestToolExecutor
```

Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add internal/service/miaodi_tool_test.go
git commit -m "test: add tests for new tool-call tools"
```

---

## Task 7: 更新 README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 删除"快捷指令"小节**

找到并删除整个 `## 快捷指令` 小节。

- [ ] **Step 2: 在"支持的模型能力（tools）"后增加自然语言说明**

在 `## 支持的模型能力（tools）` 小节末尾追加：

```markdown
你可以直接用自然语言与 Bot 交流，例如：

- "绑定我的喵滴 key：xxxxx"
- "把后续内容保存到《日记》第 3 章《今天》"
- "帮我清空刚才的对话"
- "最近我保存了什么？"
- "2026-06-30 那天我保存了哪些笔记？"

Bot 会通过 tool-call 自动调用合适的工具完成操作。
```

- [ ] **Step 3: 提交**

```bash
cd /opt/mdagent/miaodi-agent
git add README.md
git commit -m "docs: replace command shortcuts with natural-language guidance"
```

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
git commit -m "test: verify full suite after full tool-call migration"
```

---

## Self-Review Checklist

1. **Spec coverage**
   - [x] 删除 `command_router.go`、`command_help.go`、`command_router_test.go` → Task 1
   - [x] 删除 `Agent.cmdRouter` 和路由逻辑 → Task 3
   - [x] 新增 `reset_conversation` tool → Task 4
   - [x] 新增 `show_help` tool → Task 4
   - [x] 新增 `list_recent_notes` tool → Task 4 + Task 2
   - [x] 新增 `query_notes_by_date` tool → Task 4 + Task 2
   - [x] 更新 system prompt → Task 3
   - [x] 更新 Agent 测试 → Task 5
   - [x] 更新 ToolExecutor 测试 → Task 6
   - [x] 更新 README → Task 7
   - [x] 覆盖率 ≥ 90% → Task 8

2. **Placeholder scan**
   - [x] 无 TBD/TODO/"implement later"
   - [x] 每个测试步骤都包含实际代码
   - [x] 所有命令和期望输出明确

3. **Type consistency**
   - [x] `ToolRunner.Execute` 签名在 `agent.go` 和 `miaodi_tool.go` 中一致
   - [x] `NewToolExecutor` 新增 `convRepo` 参数，调用处（`app.go`）同步更新
   - [x] `fakeToolRunner` 在 `agent_test.go` 中签名同步更新
   - [x] `ToolExecutor` 中 `convRepo` 类型为 `ConversationStore`（与 `agent.go` 接口一致）
