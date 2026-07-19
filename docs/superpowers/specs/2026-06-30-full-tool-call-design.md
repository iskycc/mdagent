# 全面 tool-call 化设计文档

## 背景

当前 `miaodi-agent` 已支持通过 LLM tool-call 完成绑定喵滴 Key、设置保存路径、保存文本/图片等操作。但上一版实现引入了本地 `CommandRouter`，要求用户按固定格式输入命令（如 `绑定 key123`、`保存 内容`）。这与"AI Agent 应理解自然语言"的目标相违背，也限制了可支持的操作范围。

## 目标

1. 删除本地命令路由，所有用户操作都通过 LLM tool-call 完成。
2. 扩展模型可见工具集，支持更丰富的对话式操作。
3. 用户无需记忆固定格式，完全用自然语言表达意图。
4. 保持测试覆盖率 ≥ 90%。

## 设计

### 删除的组件

- `internal/service/command_router.go`
- `internal/service/command_help.go`
- `internal/service/command_router_test.go`
- `Agent` 中的 `cmdRouter` 字段及路由调用
- README 中的"快捷指令"小节

### 保留并扩展的工具集

`ToolDefinitions()` 返回以下工具：

| 工具名 | 说明 |
|---|---|
| `bind_miaodi_key(key)` | 绑定喵滴 API Key |
| `set_save_path(book, chapter, title)` | 设置保存路径 |
| `get_user_profile()` | 查看当前绑定状态和保存路径 |
| `save_text_note(content, title?)` | 保存文本笔记 |
| `save_image_note(image_url, title?)` | 保存图片到待上传队列 |
| `reset_conversation()` | 清空当前会话历史 |
| `show_help()` | 返回可用能力说明 |
| `list_recent_notes(limit?)` | 列出最近保存的笔记摘要 |
| `query_notes_by_date(date)` | 按日期查询已保存笔记 |

### 新增工具详情

#### `reset_conversation()`

调用 `ConversationStore.Clear(channelUserID, conversationID)`，清空当前会话历史。
返回："已清空当前会话，我们可以重新开始。"

#### `show_help()`

返回帮助文本，列出 Bot 能做什么：

```
我是喵滴 AI 助手，可以帮你：
- 绑定喵滴 API Key
- 设置保存路径（书/章/标题）
- 保存文本笔记
- 保存图片到待上传队列
- 查看当前绑定状态和路径
- 查询最近保存的笔记
- 按日期查询笔记
- 清空当前会话

你可以直接用自然语言告诉我你想做什么。
```

#### `list_recent_notes(limit?)`

查询 `api_call_log` 中当前用户的最近 `limit` 条成功操作（默认 5，最大 20）。
返回格式化的摘要列表，包括时间、动作、标题/内容摘要。

#### `query_notes_by_date(date)`

查询 `api_call_log` 中当前用户在指定日期（格式 `YYYY-MM-DD`）的操作记录。
返回格式化列表。

### 数据层说明

当前 `api_call_log` 表结构已记录：
- `channel_user_id`
- `api_key`
- `service`（如 "miaodi"）
- `action`（如 "put_text", "save_image_pending"）
- `created_at`

`list_recent_notes` / `query_notes_by_date` 直接基于该表查询。由于该表目前没有 `title`/`content_snippet` 字段，返回结果仅包含时间、动作类型等元信息。本次设计保持最小改动，不扩展表结构。

### Agent 集成变更

1. `internal/service/agent.go`：
   - 删除 `cmdRouter` 字段。
   - 删除 `ProcessMessage` 开头的本地命令路由逻辑。
   - 更新 `buildSystemPrompt`，让模型了解所有工具，并强调自然语言输入。
   - `ConversationStore.Clear` 保留，由 `reset_conversation` tool 调用。

2. `internal/service/miaodi_tool.go`：
   - `ToolDefinitions()` 返回扩展后的工具列表。
   - `ToolExecutor.Execute` 增加 `reset_conversation`、`show_help`、`list_recent_notes`、`query_notes_by_date` 分支。

### System Prompt 更新

```
你是“喵滴 AI 助手”，通过传送鸽为用户服务。用户可以用自然语言与你交流，不需要固定格式。你拥有以下工具：

1. bind_miaodi_key(key): 绑定喵滴 API Key。
2. set_save_path(book, chapter, title): 设置后续保存笔记的路径。
3. get_user_profile(): 查看当前绑定状态和保存路径。
4. save_text_note(content, title?): 把文本保存到喵滴笔记。
5. save_image_note(image_url, title?): 把图片链接写入待上传队列。
6. reset_conversation(): 清空当前会话历史。
7. show_help(): 返回你能提供的能力说明。
8. list_recent_notes(limit?): 列出最近保存的笔记摘要。
9. query_notes_by_date(date): 按日期查询已保存笔记。

当前用户状态：...

注意事项：
- 如果用户没有绑定喵滴 Key，请先引导绑定。
- 如果用户想保存图片，请调用 save_image_note。
- 如果用户说"清空"、"重置"、"忘记刚才的对话"等，调用 reset_conversation。
- 如果用户问"你能做什么"、"怎么用"等，调用 show_help。
- 回复简洁自然，控制在 200 字以内。
```

### 测试策略

1. 删除 `command_router_test.go`。
2. 更新 `agent_test.go`：移除命令路由相关测试；新增 reset/help/list/query 工具的集成测试。
3. 更新 `miaodi_tool_test.go`：新增 reset/help/list/query 工具的单元测试（使用 sqlmock 模拟 `api_call_log` 查询）。
4. 更新 `app_test.go`（如有必要，确保接口兼容性）。
5. 保持覆盖率 ≥ 90%。

## 验收标准

- [ ] `command_router.go`、`command_help.go`、`command_router_test.go` 已删除。
- [ ] `Agent` 不再包含 `cmdRouter`。
- [ ] `ToolDefinitions()` 包含 9 个工具定义。
- [ ] `reset_conversation`、`show_help`、`list_recent_notes`、`query_notes_by_date` 均可在测试中正确执行。
- [ ] 用户发送自然语言（如"帮我清空对话"、"最近保存了什么"）可由模型调用对应工具。
- [ ] README 移除"快捷指令"小节，改为说明自然语言交互。
- [ ] `go test ./...` 通过，覆盖率 ≥ 90%。
