# Redis 缓存层设计方案

## 背景与目标

当前 `miaodi-agent` 所有会话、用户状态、调用日志均直接读写 MySQL。随着并发增加，MySQL 的延迟和连接数容易成为瓶颈。本方案引入 Redis 作为热数据缓存层，目标：

1. 项目启动时把最近 24 小时活跃的会话及对应用户预加载到 Redis。
2. 运行期间，会话读写优先走 Redis；写 Redis 同步完成，写 MySQL 改为异步，不阻塞主流程。
3. 用户状态、最近笔记查询等高频/重复读操作，第一次读 MySQL 后写入 Redis，后续读 Redis。
4. Redis 不可用时自动降级读 MySQL，保证高可用。
5. 所有 Redis 数据统一设置 24 小时 TTL，与现有会话清理窗口保持一致。

## 设计决策

- **架构模式**：方案 B —— 独立 `internal/cache` 缓存层 + 独立 `internal/persist` 异步持久化层，业务层显式调用。
- **预加载范围**：仅预加载 `updated_at >= now - 24h` 的会话记录，以及这些会话关联的用户。
- **写失败策略**：Redis 写入失败时，主流程同步回退写 MySQL，并仍把该写任务放入异步队列兜底/重试。
- **Redis 客户端**：`github.com/redis/go-redis/v9`（社区主流，支持 Redis 7、cluster、sentinel）。
- **数据格式**：Redis 中统一使用 JSON 字符串存储复杂结构，简单字段使用 Redis Hash。

## Redis 配置

在 `internal/config.Config` 中新增：

```go
RedisHost     string // REDIS_HOST，默认 localhost
RedisPort     string // REDIS_PORT，默认 6379
RedisPassword string // REDIS_PASSWORD，默认空
RedisDB       int    // REDIS_DB，默认 0
RedisEnabled  bool   // REDIS_ENABLED，默认 true
```

`.env.example` 同步追加示例：

```bash
# Redis 缓存（可选，默认启用）
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_ENABLED=true
```

`REDIS_ENABLED=false` 时，Cache 实现返回固定错误，业务层统一降级到 MySQL。

## Key 设计

| 数据 | Key | Value | TTL |
|------|-----|-------|-----|
| 用户 | `md:user:{channel_user_id}` | JSON (`model.User`) | 24h |
| 会话 | `md:conv:{channel_user_id}:{conversation_id}` | JSON (`[]repository.StoredChatMessage`) | 24h |
| 最近日志 | `md:logs:{channel_user_id}` | JSON (`[]repository.UserCallLog`) | 24h |

说明：
- 会话在 Redis 中保存带时间戳的 `StoredChatMessage`，保证异步回写 MySQL 时不会丢失单条消息的 `created_at`。
- 每次读取/写入都会刷新 TTL（`EXPIRE`），保证活跃用户数据常驻 24h。

## 数据模型调整

将 `internal/repository/conversation.go` 中未导出的 `storedChatMessage` 导出为 `repository.StoredChatMessage`：

```go
package repository

type StoredChatMessage struct {
    openai.ChatMessage
    CreatedAt time.Time `json:"created_at"`
}
```

`Conversation` 表中的 `messages` JSON 格式不变；`cache` 包直接使用 `repository.StoredChatMessage`，避免缓存层与持久层出现两套相似类型。`model` 包保持不依赖 `pkg/openai`。

## Cache 层接口

```go
package cache

type Cache interface {
    Available(ctx context.Context) bool

    // 用户
    GetUser(ctx context.Context, channelUserID string) (*model.User, error)
    SetUser(ctx context.Context, user *model.User) error

    // 会话
    GetMessages(ctx context.Context, channelUserID string, conversationID int64) ([]repository.StoredChatMessage, error)
    SetMessages(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) error
    AppendMessages(ctx context.Context, channelUserID string, conversationID int64, msgs ...repository.StoredChatMessage) error
    ClearConversation(ctx context.Context, channelUserID string, conversationID int64) error

    // 调用日志（用于最近笔记查询）
    GetRecentLogs(ctx context.Context, channelUserID string) ([]repository.UserCallLog, error)
    SetRecentLogs(ctx context.Context, channelUserID string, logs []repository.UserCallLog) error
    AppendLog(ctx context.Context, channelUserID string, log repository.UserCallLog) error
}
```

实现 `RedisCache`：
- 所有方法先检查 `REDIS_ENABLED`；未启用或连接失败返回错误。
- 所有写入成功后执行 `EXPIRE` 刷新 TTL。
- `AppendMessages` 内部使用 `Get` + `append` + `Set` 实现；并发冲突时以最后一次写为准（业务上同一会话并发极低，可接受）。
- 读取 MySQL 后的回写由业务层调用 `Set` 完成，不在 Cache 内部隐式回源。

## 异步持久化层（PersistQueue）

由于用户状态变更已同步写 MySQL，用户类任务不需要异步队列。异步队列只处理两类数据：

1. **会话消息**：Redis 写入成功后，把新增消息追加到 MySQL。
2. **调用日志**：把调用记录写入 MySQL（原 `recordCall` 同步写改为异步，降低工具调用延迟）。

接口设计：

```go
package persist

type Queue interface {
    EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage)
    EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string)
    Run(ctx context.Context)
    Flush(ctx context.Context) error
}
```

实现 `PersistQueue`：
- 内部使用带缓冲 channel（容量 1024）。
- 单 goroutine worker 顺序消费，保证同一会话的追加按时间顺序执行。
- 消费时：
  - 会话任务 → 调用 `convRepo.AppendMessages(channelUserID, conversationID, msgs...)`，复用现有原子追加逻辑，天然幂等（消息按顺序追加，重复任务不会破坏顺序）。
  - 日志任务 → 调用 `callLogRepo.Record(...)`。
- 失败时指数退避重试最多 3 次；仍失败则记录错误日志，任务丢弃（非阻塞语义）。
- 应用关闭时，`Flush(ctx)` 在 5 秒超时内消费完队列中剩余任务。

**避免重复写 MySQL**：Redis 写入成功后才入队；若 Redis 写入失败，主流程已同步回退写 MySQL，此时不再入队。因此同一批消息不会既同步写 MySQL 又异步追加。用户状态变更始终同步写 MySQL，不进入本队列。

## 启动预加载

在 `app.Run` 中，Repository 初始化之后、Agent 组装之前：

```go
func seedCache(ctx context.Context, cache cache.Cache, convRepo *repository.ConversationRepo, userRepo *repository.UserRepo) error {
    // 1. 加载最近 24h 的会话
    conversations, err := convRepo.ListActiveSince(timeutil.Now().Add(-24 * time.Hour))
    if err != nil { return err }

    // 2. 收集涉及的用户 ID
    userIDs := collectUserIDs(conversations)

    // 3. 批量读取用户并写入 Redis
    for _, uid := range userIDs {
        user, err := userRepo.Get(uid)
        if err != nil { continue }
        _ = cache.SetUser(ctx, user)
    }

    // 4. 写入会话
    for _, conv := range conversations {
        _ = cache.SetMessages(ctx, conv.ChannelUserID, conv.ConversationID, conv.Messages)
    }
    return nil
}
```

预加载失败只记录日志，不中断启动（高可用要求）。

## 业务层集成点

### Agent.ProcessMessage

1. **加载用户**：
   ```go
   user, err := cache.GetUser(ctx, channelUserID)
   if err != nil {
       user, err = userRepo.GetOrCreate(channelUserID)
       if err != nil { ... }
       _ = cache.SetUser(ctx, user)
   }
   ```

2. **本地意图路由**：不变，仍直接调用 `ToolExecutor`。

3. **追加用户消息**：
   ```go
   stored := chatToStored(userMsg, timeutil.Now())
   if err := cache.AppendMessages(ctx, channelUserID, conversationID, stored); err != nil {
       // Redis 失败，同步写 MySQL，不再入队避免重复
       _ = convRepo.AppendMessage(channelUserID, conversationID, userMsg)
   } else {
       persist.EnqueueConv(ctx, channelUserID, conversationID, stored)
   }
   ```

4. **读取历史**：
   ```go
   msgs, err := cache.GetMessages(ctx, channelUserID, conversationID)
   if err != nil {
       msgs, err = convRepo.GetMessages(channelUserID, conversationID)
       if err != nil { ... }
       _ = cache.SetMessages(ctx, channelUserID, conversationID, storedFromChat(msgs, timeutil.Now()))
   }
   ```

5. **工具调用结果回写**：与步骤 3 相同，Redis 成功则入队异步追加到 MySQL；Redis 失败则同步写 MySQL。

6. **最终回复**：助手消息同步追加到 Redis，Redis 成功则入队异步追加到 MySQL；Redis 失败则同步写 MySQL。

### ToolExecutor

- `bindMiaodiKey` / `bindMiaodiByEmailCode` / `setSavePath` / `unbindMiaodiKey`：
  1. 同步写 MySQL 保证一致性。
  2. `cache.SetUser(ctx, user)` 同步更新 Redis（Redis 失败只记录日志，不影响主流程）。
  3. 不需要进入异步持久化队列（MySQL 已同步写入）。

- `resetConversation`：
  1. `cache.ClearConversation(ctx, ...)` 同步删除 Redis。
  2. `convRepo.Clear(...)` 同步删除 MySQL（数据量小，且为简化实现）。

- `recordCall`（绑定、保存、解绑等工具都会调用）：
  1. `cache.AppendLog(ctx, channelUserID, log)` 同步更新 Redis 中的最近日志缓存。
  2. `persist.EnqueueLog(ctx, channelUserID, apikey, channel, action)` 异步写入 MySQL。
  3. 若 Redis 更新失败，直接同步调用 `callLogRepo.Record(...)`，不入队避免重复。

- `listRecentNotes` / `queryNotesByDate`：
  - 先从 `cache.GetRecentLogs` 读取；miss 时读 `callLogRepo`，然后 `cache.SetRecentLogs`。
  - `queryNotesByDate` 按日期精确查询目前仍直接走 `callLogRepo.ByDate`（日期组合太多，不适合全缓存）；结果可写回 `cache.SetRecentLogs` 作为该用户的最近日志缓存。

### StatsService

统计面板读取的是聚合数据（COUNT、GROUP BY），当前不命中单用户缓存，且访问频率较低。本阶段不做 Redis 改造，仍直接读 MySQL；后续如需要可再增加独立缓存。

## 故障降级与高可用

| 场景 | 行为 |
|------|------|
| Redis 启动连接失败 | 记录日志，继续启动；所有缓存读取返回错误，业务层回源 MySQL。 |
| 运行中 Redis 断开 | `cache.Available()` 返回 false；业务层每个读取点都回源 MySQL，写操作同步写 MySQL。 |
| Redis 写入成功，异步 MySQL 失败 | 后台队列重试 3 次，仍失败则记录错误并丢弃。 |
| Redis 写入失败 | 主流程同步写 MySQL，任务仍入队兜底。 |
| 应用关闭 | 5 秒超时 flush 异步队列。 |

## 测试策略

1. **单元测试**：
   - `internal/cache`：使用 `miniredis` 模拟 Redis，测试 Get/Set/Append/Expire 行为。
   - `internal/persist`：使用 `go-sqlmock` 验证任务消费时调用的 SQL。
   - `internal/repository`：迁移 `StoredChatMessage` 后确保现有测试通过。

2. **集成测试**：
   - `docker compose` 中增加一个 `redis` 服务，跑 `go test ./...` 验证 Redis + MySQL 同时存在时的行为。

3. **降级测试**：
   - 配置 `REDIS_ENABLED=false` 或指向一个不可用的 Redis 地址，验证主流程仍能回源 MySQL 完成对话。

## 变更文件清单

- `go.mod` / `go.sum`：新增 `github.com/redis/go-redis/v9`、`github.com/alicebob/miniredis/v2`（测试）。
- `.env.example`：新增 Redis 环境变量。
- `internal/config/config.go`：新增 Redis 配置字段。
- `internal/model/model.go`：新增 `StoredChatMessage`。
- `internal/repository/conversation.go`：导出 `StoredChatMessage`，新增 `ListActiveSince`。
- `internal/cache/cache.go`、`internal/cache/redis.go`：Cache 接口与实现。
- `internal/persist/persist.go`：异步持久化队列。
- `internal/app/app.go`：初始化 Redis、Cache、PersistQueue，调用 `seedCache`。
- `internal/service/agent.go`：显式集成 cache/persist。
- `internal/service/miaodi_tool.go`：显式集成 cache/persist。
- `internal/service/interfaces.go`：可能需要新增 `PersistQueue` 接口。
- `docker-compose.yml`：可选增加 Redis 服务。

## 风险与后续优化

- **并发写覆盖**：Redis 中 `AppendMessages` 非原子；高并发同一 conversation 可能出现后写覆盖先写。当前业务是单用户对话，同一会话并发概率低；如后续需要强一致性，可改用 Redis Stream 或 Lua 脚本原子追加。
- **缓存预热失败**：启动时 MySQL 查询大量会话可能耗时；可后续改为分批加载或懒加载。
- **内存增长**：24h TTL 依赖 Redis 自动过期；如用户量极大，可后续设置最大内存策略为 `allkeys-lru`。
