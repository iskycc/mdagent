# Agents

本文档给后续在本仓库工作的 Agent 使用。进入项目后先读本文件，再读 `Todo.md`，避免重复发现已知问题或误动暂不支持的代码路径。

## 项目定位

这是一个 Go 实现的喵滴 AI Agent 服务，用于对接传送鸽 Bot 回调。服务收到用户消息后，正常情况下调用 OpenAI-compatible Chat Completions API，通过 tool-call 决定后续动作；只有当用户最新一条消息已经无法放入 LLM 输入预算时，才启用本地 `IntentRouter` 作为兜底。

当前主要能力：

- 绑定喵滴 API Key。
- 邮箱验证码绑定喵滴账号。
- 设置文本笔记保存路径。
- 保存文本笔记到喵滴。
- 查询最近保存记录和指定日期记录。
- 查询当前时间、做基础计算、日期推算、随机选择、文本统计和 token 统计。
- 统计看板 `/stats` 和 JSON 接口 `/api/stats`。

当前重点不是新增功能，而是修复 `Todo.md` 中记录的安全、可靠性和一致性问题。

## 关键文件

- `main.go`：进程入口，加载配置、打开 MySQL、启动应用。
- `internal/app/app.go`：应用装配，初始化 repository、Redis cache、异步持久化队列、Agent、HTTP routes 和清理任务。
- `internal/config/config.go`：环境变量加载、DSN 生成和配置校验。
- `internal/handler/callback.go`：传送鸽 webhook 入口。
- `internal/handler/stats.go`：统计页面和统计 JSON API。
- `internal/service/agent.go`：Agent 主流程，负责加载用户、维护会话历史、调用 LLM、执行工具、追加上下文。
- `internal/service/intent_router.go`：超长消息场景下的本地高置信度意图兜底。
- `internal/service/miaodi_tool.go`：喵滴业务工具执行器。
- `internal/service/common_tools.go`：通用工具，如时间、计算、随机、文本统计、token 统计。
- `internal/service/token_budget.go`：LLM 请求前的 token 预算和历史裁剪。
- `internal/cache/redis.go`：Redis cache 实现。
- `internal/persist/persist.go`：Redis 成功后异步写 MySQL 的持久化队列。
- `internal/repository/`：MySQL 表结构和数据访问。
- `pkg/openai/client.go`：OpenAI-compatible HTTP client。
- `pkg/client/miaodi.go`：喵滴 HTTP API client。
- `Todo.md`：已知问题、影响范围和建议修复方案。

## 运行与验证

常用命令：

```bash
go test ./...
go vet ./...
go build -o miaodi-agent .
```

修改代码后至少运行：

```bash
go test ./...
go vet ./...
```

如果只改文档，可以不跑测试，但最终回复中说明“仅文档变更，未运行测试”。

当前仓库使用 Go `1.26.3`。不要随意修改 `go.mod` 的 Go 版本或依赖，除非任务明确需要。

## 配置与部署现状

运行依赖：

- MySQL：持久化用户、会话、待处理图片、调用日志、LLM 调用日志。
- Redis：当前代码和 Compose 默认启用，用作用户、会话和最近日志缓存。
- OpenAI-compatible API：默认 base URL 是 DeepSeek 兼容接口。

注意：

- README 与当前 Redis 实现曾存在不一致，修复部署或文档问题时必须同步 `README.md`、`.env.example`、`docker-compose.yml` 和 `internal/config/config.go`。
- `docker-compose.yml` 当前包含具体部署值。修改时不要无意覆盖用户环境变量结构。
- 生产环境不应默认开启 `APP_DEBUG` 或 `OPENAI_DEBUG`。

## 数据流

Webhook 主流程：

1. `handler.CallbackHandler.handleCallback` 读取并解析传送鸽 payload。
2. `service.Agent.ProcessMessage` 根据 `channelUserID` 加载用户。
3. 构造 system prompt 和 tool definitions，并检查最新用户消息是否能放入输入预算。
4. 如果最新消息已经超出预算，才尝试 `IntentRouter.Route`；命中则直接执行工具，未命中则返回过长提示。
5. 最新消息可放入预算时，把用户消息加入历史。
6. 读取 24 小时内历史并按 token 预算裁剪旧消息。
7. 调用 LLM。
8. 如果模型返回 tool calls，执行 `ToolExecutor.Execute`。
9. 将 assistant/tool 消息追加到历史。
10. 返回最终回复。

缓存和持久化现状：

- 用户状态优先读 Redis，失败时回源 MySQL。
- 会话历史优先读写 Redis，Redis 写成功后异步写 MySQL。
- Redis 失败时部分路径同步 fallback 写 MySQL。
- 该设计有数据丢失窗口，详见 `Todo.md`。

## 安全优先级

后续改动优先处理以下事项：

1. Webhook 鉴权。
2. `/stats` 和 `/api/stats` 鉴权。
3. 关闭并脱敏生产调试日志。
4. 避免返回完整喵滴 API Key。
5. 修复 Redis 用户缓存一致性。
6. 增加 webhook 幂等处理。
7. 治理 API Key 明文存储和日志字段。

涉及安全改动时，不要只改业务逻辑，还要补测试。

## 敏感信息处理

以下信息视为敏感：

- 喵滴 API Key。
- OpenAI API Key。
- 邮箱地址。
- 邮箱验证码。
- 用户消息原文和笔记内容。
- 年度报告 URL，因为 URL 中包含 Key。
- Authorization header。

规则：

1. 不要新增明文敏感日志。
2. Debug 日志必须脱敏。
3. 用户主动查询绑定状态时默认展示脱敏 Key。
4. 不要把完整 Key 写入普通调用日志。
5. 错误消息返回给用户时不要包含内部 DSN、header、token 或完整外部响应体。

## 图片相关代码状态

当前图片存储/上传方案不支持，相关能力视为废弃路径，但暂时保留代码，不要删除。

涉及范围包括但不限于：

- `save_image_note` 工具。
- `pending_images` 表和 `PendingImageRepo`。
- `MiaodiClient.UpImage`。
- README 中关于 pending image 后台扫描上传的说明。

后续处理规则：

1. 不要把图片保存能力作为当前支持功能宣传。
2. 不要围绕图片上传链路做优先级修复。
3. 不要删除图片相关代码，避免破坏历史数据或未来恢复计划。
4. 如必须修改相邻代码，应保持图片路径行为不扩大、不新增依赖、不引入新的承诺。
5. 用户侧文案应避免表达“图片已保存成功”，最多表达为“不支持图片保存”或“图片能力暂不可用”。
6. 如果任务要求“禁用图片能力”，优先从路由和工具返回文案层面标记不可用，不做表删除和代码大规模清理。

## 常见改动注意事项

### 修改 webhook

- 保留 10 秒回调限制内的处理思路。
- 当前 handler 使用 9 秒 context timeout。
- 增加鉴权时要基于 raw body 做签名校验，不能先 JSON unmarshal 再签。
- 增加 body size limit 时要处理 413 测试。

### 修改 Agent 流程

- 不要破坏 tool-call 多轮循环。
- `reset_conversation` 是终止操作，执行后不应继续调用 LLM。
- 修改历史消息结构时要兼容 repository 中已有 JSON。
- token budget 改动需要覆盖未知模型、tool definitions 和超长消息。

### 修改缓存

- 用户状态变更后必须同步更新 Redis cache，或明确失效缓存。
- Redis 不可用时业务应能降级到 MySQL。
- 不要让 Redis 成功成为关键数据“已持久化”的唯一依据。
- 如果新增缓存 key，写清 TTL 和失效策略。

### 修改持久化

- repository 当前承担建表逻辑。若新增表，短期可沿用 `EnsureTable`，但中长期建议迁移到版本化 migrations。
- 会话追加使用事务和 `SELECT ... FOR UPDATE`，不要改成非原子读写。
- 新增字段时考虑已有生产表的 `ALTER TABLE` 兼容。

### 修改工具

- 工具参数必须做 JSON 解析错误处理。
- 用户未绑定时不要调用喵滴写入接口。
- 外部 API 失败时记录失败 action，但不要把敏感信息带入日志。
- 新增工具时同步更新：
  - `ToolDefinitions()`
  - `ToolExecutor.Execute`
  - `buildSystemPrompt`
  - 本地意图路由和测试，如有必要
  - README 能力说明，如功能对用户可见

### 修改统计

- `/stats` 和 `/api/stats` 应先补鉴权。
- `metrics` 当前保存全部耗时样本，有内存增长问题。
- 统计页面展示的数据不要包含用户可识别信息或完整 Key。

## 测试约定

优先添加单元测试，现有测试覆盖较全，常用模式包括：

- `httptest` 测 handler。
- `sqlmock` 测 repository 和工具数据库分支。
- `miniredis` 测 Redis cache。
- fake LLM / fake tool runner 测 Agent 流程。

新增或修改以下逻辑时必须补测试：

- 鉴权。
- 敏感信息脱敏。
- 用户状态和缓存一致性。
- webhook 幂等。
- 持久化队列 fallback。
- token budget。
- 本地意图路由。

## 编辑约束

- 不要直接删除历史功能代码，除非用户明确要求。
- 不要使用破坏性 git 命令。
- 不要回滚用户已有改动。
- 对敏感字段新增日志时必须脱敏。
- 修改行为时同步相关 README、`.env.example` 和测试。
- 图片相关路径按废弃保留处理。
- 完成代码改动后运行：

```bash
go test ./...
go vet ./...
```

## 当前优先任务

以 `Todo.md` 为准。推荐顺序：

1. P0：回调鉴权、统计鉴权、关闭敏感 debug、Key 返回脱敏。
2. P1：修 Redis 用户缓存、Webhook 幂等、异步持久化补偿、文档与 Compose 对齐、Key 存储治理。
3. P2：请求体大小限制、metrics 有界化。
4. P3：喵滴 endpoint 配置化、数据库迁移体系。
