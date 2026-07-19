# 智能帮助与快捷指令系统设计文档

## 背景

`miaodi-agent` 当前已具备完整的回调处理链路：接收 传送鸽 消息 → 调用 DeepSeek 等 OpenAI 兼容模型进行 tool-call 决策 → 执行喵滴工具。但新用户首次使用时，不了解 Bot 能做什么；同时高频操作（绑定 Key、设置路径、保存文本）每次都经过 LLM，增加了响应时间和失败概率。

## 目标

1. 提供即时帮助响应：用户输入 `帮助`、`?`、`菜单` 等关键词时，立即返回帮助文本。
2. 支持高频操作的本地快捷指令，绕过 LLM，降低响应时间和成本。
3. 未命中指令时，保持原有 LLM tool-call 链路不变，避免影响现有功能。

## 设计

### 新增组件

#### `internal/service/command_router.go`

```go
type CommandRouter struct {
    userRepo  UserStore
    convRepo  ConversationStore
    toolExec  ToolRunner
}

func NewCommandRouter(userRepo UserStore, convRepo ConversationStore, toolExec ToolRunner) *CommandRouter

// Route 尝试把用户输入识别为本地命令。
// 返回 (reply, handled)。handled=true 表示已处理，调用方直接返回 reply。
func (r *CommandRouter) Route(user *model.User, channelUserID, text string) (reply string, handled bool)
```

`Route` 内部按顺序匹配：

| 用户输入 | 处理 |
|---|---|
| `帮助` / `?` / `菜单` / `help` | 返回帮助文本 |
| `绑定 <key>` / `/绑定 <key>` / `绑定我的喵滴 key：xxx` | 调用 `bind_miaodi_key` |
| `路径 <book> <chapter> <title>` / `/路径 ...` | 调用 `set_save_path` |
| `保存 <内容>` / `/保存 <内容>` | 调用 `save_text_note` |
| `重置` / `/重置` / `清空` | 清空当前用户会话历史 |
| 其他 | `handled=false` |

#### `internal/service/command_help.go`

集中维护帮助文本常量，便于统一修改：

```go
const HelpMessage = `🐱 喵滴助手可用指令：

绑定 <喵滴Key>         - 绑定你的喵滴 API Key
路径 <书> <章> <标题>   - 设置默认保存路径
保存 <内容>             - 保存文本笔记
重置                    - 清空当前会话
帮助                    - 显示本帮助
`
```

### 与现有 Agent 集成

修改 `internal/service/agent.go` 的 `ProcessMessage`：

1. 对用户输入进行命令路由。
2. 若 `handled=true`，直接返回 `reply`。
3. 若 `handled=false`，走原有 LLM tool-call 流程。

### 命令解析规则

1. 不区分大小写。
2. 允许 `/` 前缀。
3. 参数按空格拆分。
4. `保存` 命令取第一个空格后的所有内容作为 `content`。
5. `绑定` 命令兼容自然语言前缀，提取最后一段非空字符串作为 key。
6. 路径命令要求至少 3 个参数，否则返回用法提示。

### 回执文本

- 绑定成功：`✅ 已绑定喵滴 Key` / `❌ 绑定失败：...`
- 设置路径：`✅ 已设置保存路径：《书》第 N 章《标题》`
- 保存文本：直接返回工具执行结果或简短成功/失败提示。
- 重置：`✅ 已清空当前会话`

## 不做的范围

1. 不新增数据库表或字段。
2. 不新增外部依赖。
3. 不改现有 tool executor、repository 和 handler 接口。
4. 不处理图片保存的快捷指令（保存图片仍需 LLM 解析图片 URL）。

## 测试策略

1. `command_router_test.go`：覆盖帮助、绑定、路径、保存、重置、参数错误、未命中命令。
2. `agent_test.go`：覆盖命中命令时不调用 LLM、未命中命令时调用 LLM。
3. 保持项目整体测试覆盖率 ≥ 90%。

## 验收标准

- [ ] 输入 `帮助` 返回帮助文本。
- [ ] 输入 `绑定 xxx` 或 `/绑定 xxx` 完成绑定并返回回执。
- [ ] 输入 `路径 书 章 标题` 设置路径并返回回执。
- [ ] 输入 `保存 内容` 保存文本并返回回执。
- [ ] 输入 `重置` 清空会话历史。
- [ ] 未知输入仍走 LLM tool-call 链路。
- [ ] `go test ./...` 通过，覆盖率 ≥ 90%。
