# 工具集扩展设计文档

## 背景

当前 `miaodi-agent` 已有 9 个 LLM 可见 tools，覆盖绑定、路径、保存、重置、帮助、历史查询等基础能力。为进一步提升自然语言交互的覆盖度，现基于本地数据库和现有喵滴 API（`Check`/`GetInfo`/`PutText`）扩展 8 个新工具。

## 目标

1. 新增 8 个 tools，全部通过自然语言触发。
2. 所有真实调用喵滴服务器的操作必须先执行 `miaodi.Check(key)` 校验。
3. 纯本地操作（解绑、查队列、查历史、查统计、切换模型记录）不调用喵滴服务器。
4. 保持测试覆盖率 ≥ 90%。

## 新增工具

| 工具名 | 类型 | 说明 |
|---|---|---|
| `unbind_miaodi_key()` | 本地 DB | 解绑当前喵滴 Key |
| `query_pending_images(status?)` | 本地 DB | 查询当前用户的图片上传队列 |
| `delete_pending_image(image_id)` | 本地 DB | 删除队列中指定图片 |
| `get_conversation_history(limit?)` | 本地 DB | 查看当前会话最近消息 |
| `get_user_stats(period?)` | 本地 DB | 查看个人使用统计 |
| `save_multiple_images(urls[])` | 本地 DB | 批量图片落库到待上传队列 |
| `check_key_validity(key)` | 喵滴 API | 校验 Key 是否有效 |
| `switch_model(model)` | 本地 DB | 设置用户偏好的模型 |

## 工具详情

### `unbind_miaodi_key()`

- 将当前用户的 `apikey` 清空，`status` 置为 `unbound`。
- 可选：把 `book`/`chara`/`title` 重置为默认值（"默认"/"微信"/""）。
- 返回："已解绑喵滴 Key。如需再次使用，请重新绑定。"

### `query_pending_images(status?)`

- 根据当前用户的 `apikey` 查询 `pending_images`。
- `status` 可选，默认 `"pending"`。
- 返回格式化列表：`ID | 图片 URL | 状态 | 时间`。

### `delete_pending_image(image_id)`

- 根据 `image_id` 和当前用户 `apikey` 删除 `pending_images` 记录。
- 必须校验所有权，防止删除他人图片。
- 返回成功/失败提示。

### `get_conversation_history(limit?)`

- 调用 `ConversationStore.GetMessages` 获取当前会话历史。
- `limit` 默认 10，最大 50。
- 返回最近 N 条消息的简化摘要（角色 + 内容前 100 字）。

### `get_user_stats(period?)`

- 基于 `api_call_log` 统计当前用户的操作次数。
- `period` 支持：`today` / `7d` / `30d` / `all`，默认 `7d`。
- 返回各 action 计数和总次数。

### `save_multiple_images(urls[])`

- 要求用户已绑定喵滴 Key。
- 将多个图片 URL 批量写入 `pending_images`。
- 对每个 URL 复用当前保存路径和标题逻辑。
- 返回成功数量、失败数量、失败原因列表。

### `check_key_validity(key)`

- 调用 `miaodi.Check(key)` 校验 Key。
- **必须先 Check**，本身就是 Check 调用。
- 返回："该 Key 有效" 或 "该 Key 无效/已过期"。

### `switch_model(model)`

- 将用户偏好的模型记录到 `agent_users.model`。
- `Agent.ProcessMessage` 在构造 system prompt / 选择模型时，优先使用 `user.Model`；若为空则回退到 `cfg.Model`。
- 返回："已切换模型为 xxx"。

## 数据层变更

### `agent_users` 表

新增字段 `model`：

```sql
ALTER TABLE agent_users ADD COLUMN model VARCHAR(64) DEFAULT '' AFTER title;
```

同时更新 `UserRepo.EnsureTable()` 的建表语句，加入 `model` 字段。

### `UserRepo`

- `UpdateToUnbound(channelUserID string) error`：解绑用户。
- `UpdateModel(channelUserID, model string) error`：更新模型偏好。
- `Get` 和 `GetOrCreate` 需要扫描 `model` 字段。

### `PendingImageRepo`

- `ListByAPIKey(apikey, status string) ([]model.PendingImage, error)`：按用户 API Key 和状态查询。
- `DeleteByIDAndAPIKey(id int64, apikey string) error`：按 ID 和 API Key 删除，确保所有权。

### `CallLogRepo`

- `StatsByUser(channelUserID, period string) (map[string]int64, error)`：按用户和时间段统计 action 次数。

### `ConversationRepo`

- 已具备 `GetMessages`，可直接使用。

## Agent 集成变更

1. `internal/service/miaodi_tool.go`：
   - `ToolDefinitions()` 新增 8 个工具定义。
   - `Execute` 增加对应分支。
   - 实现 8 个工具方法。

2. `internal/service/agent.go`：
   - 在 `ProcessMessage` 中，构造 LLM 请求时选择模型：
     ```go
     modelName := a.model
     if user.Model != "" {
         modelName = user.Model
     }
     ```
     后续调用 `a.llm.ChatCompletion(ctx, modelName, ...)` 时使用 `modelName`。
   - 更新 `buildSystemPrompt`，列出新增工具。

3. `internal/app/app.go`：
   - 确保 `ToolExecutor` 初始化参数不变。

## Check 校验规则

| 工具 | 是否调用喵滴 API | 是否需先 Check |
|---|---|---|
| `unbind_miaodi_key` | 否 | 否 |
| `query_pending_images` | 否 | 否 |
| `delete_pending_image` | 否 | 否 |
| `get_conversation_history` | 否 | 否 |
| `get_user_stats` | 否 | 否 |
| `save_multiple_images` | 否（仅落本地队列） | 否，但要求已绑定 |
| `check_key_validity` | 是（调用 `Check`） | 是（本身即是） |
| `switch_model` | 否 | 否 |

## 测试策略

1. `internal/repository/user_test.go`：新增 `UpdateToUnbound`、`UpdateModel` 测试。
2. `internal/repository/pending_image_test.go`：新增 `ListByAPIKey`、`DeleteByIDAndAPIKey` 测试。
3. `internal/repository/call_log_test.go`：新增 `StatsByUser` 测试。
4. `internal/service/miaodi_tool_test.go`：新增 8 个工具的单元测试。
5. `internal/service/agent_test.go`：新增 Agent 集成测试，验证自然语言意图可触发对应工具。
6. 保持覆盖率 ≥ 90%。

## 验收标准

- [ ] 8 个新工具均可在 `ToolDefinitions()` 中看到。
- [ ] `unbind` / `query_pending_images` / `delete_pending_image` / `get_conversation_history` / `get_user_stats` / `save_multiple_images` / `check_key_validity` / `switch_model` 均可在测试中正确执行。
- [ ] `check_key_validity` 调用喵滴 `Check`。
- [ ] `switch_model` 后，Agent 优先使用用户模型偏好。
- [ ] `go test ./...` 通过，覆盖率 ≥ 90%。
- [ ] README 更新，列出新增工具。
