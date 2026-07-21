# Redis 缓存层实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 miaodi-agent 引入 Redis 缓存层，实现会话热数据缓存、异步 MySQL 持久化、高频读缓存预热及 Redis 故障降级。

**Architecture:** 新增 `internal/cache`（Redis 读写）与 `internal/persist`（异步 MySQL 持久化队列）；业务层显式调用 cache/persist，Redis 不可用时回源 MySQL。

**Tech Stack:** Go 1.26, `github.com/redis/go-redis/v9`, `github.com/alicebob/miniredis/v2`（测试）, MySQL, Docker Compose。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `internal/config/config.go` | 新增 Redis 环境变量配置 |
| `.env.example` | 新增 Redis 配置示例 |
| `internal/repository/conversation.go` | 导出 `StoredChatMessage`，新增 `ListActiveSince` |
| `internal/cache/cache.go` | `Cache` 接口定义 |
| `internal/cache/redis.go` | `RedisCache` 实现 |
| `internal/cache/redis_test.go` | Cache 单元测试（miniredis） |
| `internal/persist/persist.go` | `Queue` 接口与 `PersistQueue` 实现 |
| `internal/persist/persist_test.go` | 持久化队列单元测试（sqlmock） |
| `internal/app/app.go` | 初始化 Redis、Cache、PersistQueue、预加载 |
| `internal/service/agent.go` | 集成 cache/persist 处理会话 |
| `internal/service/miaodi_tool.go` | 集成 cache/persist 处理用户/日志 |
| `internal/service/interfaces.go` | 新增 `PersistQueue` 接口 |
| `docker-compose.yml` | 可选增加 Redis 服务 |

---

### Task 1: 配置 Redis 环境变量

**Files:**
- Modify: `internal/config/config.go`
- Modify: `.env.example`
- Test: `internal/config/config_test.go`（如存在则新增；当前无，暂不创建）

- [ ] **Step 1: 在 `Config` 结构体中新增 Redis 字段**

在 `internal/config/config.go` 的 `Config` 结构体中追加：

```go
RedisHost     string
RedisPort     string
RedisPassword string
RedisDB       int
RedisEnabled  bool
```

完整结构体应类似：

```go
type Config struct {
    Port            string
    DBHost          string
    DBPort          string
    DBUser          string
    DBPass          string
    DBName          string
    DBMaxOpen       int
    DBMaxIdle       int
    OpenAIAPIKey    string
    OpenAIBaseURL   string
    OpenAIModel     string
    ModelMaxTokens  int
    MaxOutputTokens int
    CallbackPath    string
    RedisHost       string
    RedisPort       string
    RedisPassword   string
    RedisDB         int
    RedisEnabled    bool
}
```

- [ ] **Step 2: 在 `Load()` 中读取 Redis 环境变量**

```go
RedisHost:     getEnv("REDIS_HOST", "localhost"),
RedisPort:     getEnv("REDIS_PORT", "6379"),
RedisPassword: getEnv("REDIS_PASSWORD", ""),
RedisDB:       getEnvInt("REDIS_DB", 0),
RedisEnabled:  getEnvBool("REDIS_ENABLED", true),
```

- [ ] **Step 3: 新增 `getEnvBool` 辅助函数**

在 `internal/config/config.go` 末尾追加：

```go
func getEnvBool(key string, defaultVal bool) bool {
    v := os.Getenv(key)
    if v == "" {
        return defaultVal
    }
    b, err := strconv.ParseBool(v)
    if err != nil {
        return defaultVal
    }
    return b
}
```

- [ ] **Step 4: 更新 `.env.example`**

在文件末尾追加：

```bash
# Redis 缓存（可选，默认启用）
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_ENABLED=true
```

- [ ] **Step 5: 运行构建验证**

```bash
cd /opt/mdagent
go build ./...
```

Expected: 编译通过。

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go .env.example
git commit -m "config: add redis configuration"
```

---

### Task 2: Repository 导出 StoredChatMessage 并新增 ListActiveSince

**Files:**
- Modify: `internal/repository/conversation.go`
- Test: `internal/repository/conversation_test.go`（如不存在则创建）

- [ ] **Step 1: 导出 `StoredChatMessage` 类型**

在 `internal/repository/conversation.go` 中，将：

```go
type storedChatMessage struct {
    openai.ChatMessage
    CreatedAt time.Time `json:"created_at"`
}
```

改为：

```go
// StoredChatMessage 是会话消息的持久化/缓存格式，带创建时间戳。
type StoredChatMessage struct {
    openai.ChatMessage
    CreatedAt time.Time `json:"created_at"`
}
```

- [ ] **Step 2: 全文件替换 `storedChatMessage` 为 `StoredChatMessage`**

使用替换工具或手动将 `internal/repository/conversation.go` 中所有 `storedChatMessage`（包括函数参数、返回值、局部变量类型）统一替换为 `StoredChatMessage`。

涉及函数：
- `decodeStoredMessages`
- `storedMessagesHavePayload`
- `chatMessagesToStored`
- `storedToChatMessages`（参数类型）
- `pruneStoredMessages`

- [ ] **Step 3: 新增 `ListActiveSince` 方法**

在 `internal/repository/conversation.go` 中，紧跟 `EnsureTable` 之后添加：

```go
// ListActiveSince 返回 updated_at 晚于 cutoff 的所有会话及消息。
func (r *ConversationRepo) ListActiveSince(cutoff time.Time) ([]struct {
    ChannelUserID  string
    ConversationID int64
    Messages       []StoredChatMessage
}, error) {
    rows, err := r.db.Query(`
        SELECT channel_user_id, conversation_id, messages, updated_at
        FROM agent_conversations
        WHERE updated_at >= ?`, cutoff)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    type row struct {
        ChannelUserID  string
        ConversationID int64
        Messages       []StoredChatMessage
    }
    var result []row
    for rows.Next() {
        var channelUserID string
        var conversationID int64
        var raw []byte
        var updatedAt time.Time
        if err := rows.Scan(&channelUserID, &conversationID, &raw, &updatedAt); err != nil {
            return nil, err
        }
        stored, err := decodeStoredMessages(raw, updatedAt)
        if err != nil {
            return nil, fmt.Errorf("decode conversation %s/%d failed: %w", channelUserID, conversationID, err)
        }
        result = append(result, row{
            ChannelUserID:  channelUserID,
            ConversationID: conversationID,
            Messages:       stored,
        })
    }
    return result, rows.Err()
}
```

- [ ] **Step 4: 运行现有测试确保未破坏行为**

```bash
cd /opt/mdagent
go test ./internal/repository/...
```

Expected: PASS（可能需要更新测试中的未导出类型引用）。

- [ ] **Step 5: Commit**

```bash
git add internal/repository/conversation.go
git commit -m "repository: export StoredChatMessage and add ListActiveSince"
```

---

### Task 3: 实现 Cache 接口与 RedisCache

**Files:**
- Create: `internal/cache/cache.go`
- Create: `internal/cache/redis.go`
- Create: `internal/cache/redis_test.go`

- [ ] **Step 1: 创建 `internal/cache/cache.go` 定义接口**

```go
package cache

import (
    "context"

    "miaodi-agent/internal/model"
    "miaodi-agent/internal/repository"
)

// Cache 定义 Redis 缓存操作，所有方法遇到 Redis 不可用返回 error，由业务层降级。
type Cache interface {
    Available(ctx context.Context) bool

    GetUser(ctx context.Context, channelUserID string) (*model.User, error)
    SetUser(ctx context.Context, user *model.User) error

    GetMessages(ctx context.Context, channelUserID string, conversationID int64) ([]repository.StoredChatMessage, error)
    SetMessages(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) error
    AppendMessages(ctx context.Context, channelUserID string, conversationID int64, msgs ...repository.StoredChatMessage) error
    ClearConversation(ctx context.Context, channelUserID string, conversationID int64) error

    GetRecentLogs(ctx context.Context, channelUserID string) ([]repository.UserCallLog, error)
    SetRecentLogs(ctx context.Context, channelUserID string, logs []repository.UserCallLog) error
    AppendLog(ctx context.Context, channelUserID string, log repository.UserCallLog) error
}
```

- [ ] **Step 2: 创建 `internal/cache/redis.go` 实现 RedisCache**

```go
package cache

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"

    "miaodi-agent/internal/model"
    "miaodi-agent/internal/repository"
)

const ttl = 24 * time.Hour

// RedisCache 是基于 go-redis 的 Cache 实现。
type RedisCache struct {
    client  *redis.Client
    enabled bool
}

// NewRedisCache 创建缓存；enabled=false 时所有操作返回 error。
func NewRedisCache(addr, password string, db int, enabled bool) *RedisCache {
    if !enabled {
        return &RedisCache{enabled: false}
    }
    return &RedisCache{
        client:  redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db}),
        enabled: true,
    }
}

func (c *RedisCache) Available(ctx context.Context) bool {
    if !c.enabled || c.client == nil {
        return false
    }
    return c.client.Ping(ctx).Err() == nil
}

func (c *RedisCache) GetUser(ctx context.Context, channelUserID string) (*model.User, error) {
    if !c.enabled {
        return nil, fmt.Errorf("redis disabled")
    }
    raw, err := c.client.Get(ctx, userKey(channelUserID)).Bytes()
    if err != nil {
        return nil, err
    }
    var user model.User
    if err := json.Unmarshal(raw, &user); err != nil {
        return nil, err
    }
    return &user, nil
}

func (c *RedisCache) SetUser(ctx context.Context, user *model.User) error {
    if !c.enabled {
        return fmt.Errorf("redis disabled")
    }
    raw, err := json.Marshal(user)
    if err != nil {
        return err
    }
    return c.client.Set(ctx, userKey(user.ChannelUserID), raw, ttl).Err()
}

func (c *RedisCache) GetMessages(ctx context.Context, channelUserID string, conversationID int64) ([]repository.StoredChatMessage, error) {
    if !c.enabled {
        return nil, fmt.Errorf("redis disabled")
    }
    raw, err := c.client.Get(ctx, convKey(channelUserID, conversationID)).Bytes()
    if err != nil {
        return nil, err
    }
    var msgs []repository.StoredChatMessage
    if err := json.Unmarshal(raw, &msgs); err != nil {
        return nil, err
    }
    return msgs, nil
}

func (c *RedisCache) SetMessages(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) error {
    if !c.enabled {
        return fmt.Errorf("redis disabled")
    }
    raw, err := json.Marshal(msgs)
    if err != nil {
        return err
    }
    return c.client.Set(ctx, convKey(channelUserID, conversationID), raw, ttl).Err()
}

func (c *RedisCache) AppendMessages(ctx context.Context, channelUserID string, conversationID int64, msgs ...repository.StoredChatMessage) error {
    if !c.enabled {
        return fmt.Errorf("redis disabled")
    }
    existing, err := c.GetMessages(ctx, channelUserID, conversationID)
    if err != nil && err != redis.Nil {
        return err
    }
    combined := append(existing, msgs...)
    return c.SetMessages(ctx, channelUserID, conversationID, combined)
}

func (c *RedisCache) ClearConversation(ctx context.Context, channelUserID string, conversationID int64) error {
    if !c.enabled {
        return fmt.Errorf("redis disabled")
    }
    return c.client.Del(ctx, convKey(channelUserID, conversationID)).Err()
}

func (c *RedisCache) GetRecentLogs(ctx context.Context, channelUserID string) ([]repository.UserCallLog, error) {
    if !c.enabled {
        return nil, fmt.Errorf("redis disabled")
    }
    raw, err := c.client.Get(ctx, logsKey(channelUserID)).Bytes()
    if err != nil {
        return nil, err
    }
    var logs []repository.UserCallLog
    if err := json.Unmarshal(raw, &logs); err != nil {
        return nil, err
    }
    return logs, nil
}

func (c *RedisCache) SetRecentLogs(ctx context.Context, channelUserID string, logs []repository.UserCallLog) error {
    if !c.enabled {
        return fmt.Errorf("redis disabled")
    }
    raw, err := json.Marshal(logs)
    if err != nil {
        return err
    }
    return c.client.Set(ctx, logsKey(channelUserID), raw, ttl).Err()
}

func (c *RedisCache) AppendLog(ctx context.Context, channelUserID string, log repository.UserCallLog) error {
    if !c.enabled {
        return fmt.Errorf("redis disabled")
    }
    logs, err := c.GetRecentLogs(ctx, channelUserID)
    if err != nil && err != redis.Nil {
        return err
    }
    logs = append([]repository.UserCallLog{log}, logs...)
    if len(logs) > 20 {
        logs = logs[:20]
    }
    return c.SetRecentLogs(ctx, channelUserID, logs)
}

func userKey(channelUserID string) string {
    return fmt.Sprintf("md:user:%s", channelUserID)
}

func convKey(channelUserID string, conversationID int64) string {
    return fmt.Sprintf("md:conv:%s:%d", channelUserID, conversationID)
}

func logsKey(channelUserID string) string {
    return fmt.Sprintf("md:logs:%s", channelUserID)
}
```

- [ ] **Step 3: 创建 `internal/cache/redis_test.go` 编写测试**

```go
package cache

import (
    "context"
    "testing"
    "time"

    "github.com/alicebob/miniredis/v2"

    "miaodi-agent/internal/model"
    "miaodi-agent/internal/repository"
    "miaodi-agent/pkg/openai"
)

func TestRedisCache_UserRoundTrip(t *testing.T) {
    s := miniredis.RunT(t)
    defer s.Close()

    c := NewRedisCache(s.Addr(), "", 0, true)
    ctx := context.Background()
    user := &model.User{ChannelUserID: "u1", APIKey: "k1", Status: "bound"}

    if err := c.SetUser(ctx, user); err != nil {
        t.Fatalf("set user: %v", err)
    }
    got, err := c.GetUser(ctx, "u1")
    if err != nil {
        t.Fatalf("get user: %v", err)
    }
    if got.APIKey != "k1" {
        t.Fatalf("unexpected api key: %s", got.APIKey)
    }
}

func TestRedisCache_MessagesAppend(t *testing.T) {
    s := miniredis.RunT(t)
    defer s.Close()

    c := NewRedisCache(s.Addr(), "", 0, true)
    ctx := context.Background()

    msg := repository.StoredChatMessage{ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()}
    if err := c.AppendMessages(ctx, "u1", 1, msg); err != nil {
        t.Fatalf("append: %v", err)
    }
    msgs, err := c.GetMessages(ctx, "u1", 1)
    if err != nil {
        t.Fatalf("get: %v", err)
    }
    if len(msgs) != 1 || msgs[0].Content != "hi" {
        t.Fatalf("unexpected messages: %+v", msgs)
    }
}

func TestRedisCache_Disabled(t *testing.T) {
    c := NewRedisCache("", "", 0, false)
    ctx := context.Background()
    if c.Available(ctx) {
        t.Fatal("expected not available")
    }
    if _, err := c.GetUser(ctx, "u1"); err == nil {
        t.Fatal("expected error")
    }
}
```

注意：`miniredis.RunT(t)` 是 miniredis 的标准测试 API；若版本不同可参考其文档调整。运行测试后根据实际 API 调整。

- [ ] **Step 4: 添加依赖并运行测试**

```bash
cd /opt/mdagent
go get github.com/redis/go-redis/v9
go get github.com/alicebob/miniredis/v2
go mod tidy
go test ./internal/cache/...
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/cache/ go.mod go.sum
git commit -m "cache: add redis cache implementation with tests"
```

---

### Task 4: 实现异步持久化队列 PersistQueue

**Files:**
- Create: `internal/persist/persist.go`
- Create: `internal/persist/persist_test.go`

- [ ] **Step 1: 创建 `internal/persist/persist.go`**

```go
package persist

import (
    "context"
    "log"
    "time"

    "miaodi-agent/internal/repository"
    "miaodi-agent/internal/timeutil"
    "miaodi-agent/pkg/openai"
)

// Queue 定义异步持久化队列。
type Queue interface {
    EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage)
    EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string)
    Run(ctx context.Context)
    Flush(ctx context.Context) error
}

// PersistQueue 把会话消息和调用日志异步写回 MySQL。
type PersistQueue struct {
    convRepo    *repository.ConversationRepo
    callLogRepo *repository.CallLogRepo
    tasks       chan task
}

type taskKind int

const (
    taskKindConv taskKind = iota
    taskKindLog
)

type task struct {
    kind           taskKind
    channelUserID  string
    conversationID int64
    messages       []repository.StoredChatMessage
    apikey         string
    channel        string
    action         string
}

// NewPersistQueue 创建队列，bufferSize 为内部 channel 容量。
func NewPersistQueue(convRepo *repository.ConversationRepo, callLogRepo *repository.CallLogRepo, bufferSize int) *PersistQueue {
    if bufferSize <= 0 {
        bufferSize = 1024
    }
    return &PersistQueue{
        convRepo:    convRepo,
        callLogRepo: callLogRepo,
        tasks:       make(chan task, bufferSize),
    }
}

func (q *PersistQueue) EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) {
    select {
    case q.tasks <- task{kind: taskKindConv, channelUserID: channelUserID, conversationID: conversationID, messages: msgs}:
    case <-ctx.Done():
    }
}

func (q *PersistQueue) EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string) {
    select {
    case q.tasks <- task{kind: taskKindLog, channelUserID: channelUserID, apikey: apikey, channel: channel, action: action}:
    case <-ctx.Done():
    }
}

func (q *PersistQueue) Run(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            case t := <-q.tasks:
                q.process(ctx, t)
            }
        }
    }()
}

func (q *PersistQueue) process(ctx context.Context, t task) {
    const maxRetries = 3
    backoff := 100 * time.Millisecond
    var lastErr error
    for i := 0; i < maxRetries; i++ {
        var err error
        switch t.kind {
        case taskKindConv:
            msgs := storedToChatMessages(t.messages)
            err = q.convRepo.AppendMessages(t.channelUserID, t.conversationID, msgs...)
        case taskKindLog:
            err = q.callLogRepo.Record(t.channelUserID, t.apikey, t.channel, t.action)
        }
        if err == nil {
            return
        }
        lastErr = err
        time.Sleep(backoff)
        backoff *= 2
    }
    log.Printf("persist task failed after retries: kind=%d user=%s conv=%d err=%v", t.kind, t.channelUserID, t.conversationID, lastErr)
}

func (q *PersistQueue) Flush(ctx context.Context) error {
    for {
        select {
        case t := <-q.tasks:
            q.process(ctx, t)
        default:
            return nil
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
    }
}

func storedToChatMessages(stored []repository.StoredChatMessage) []openai.ChatMessage {
    msgs := make([]openai.ChatMessage, 0, len(stored))
    for _, s := range stored {
        msgs = append(msgs, s.ChatMessage)
    }
    return msgs
}
```

- [ ] **Step 2: 创建 `internal/persist/persist_test.go`**

```go
package persist

import (
    "context"
    "testing"
    "time"

    "github.com/DATA-DOG/go-sqlmock"

    "miaodi-agent/internal/repository"
    "miaodi-agent/pkg/openai"
)

func TestPersistQueue_ConvTask(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("sqlmock: %v", err)
    }
    defer db.Close()

    convRepo := repository.NewConversationRepo(db)
    callLogRepo := repository.NewCallLogRepo(db)
    q := NewPersistQueue(convRepo, callLogRepo, 10)

    mock.ExpectBegin()
    mock.ExpectQuery("SELECT messages, updated_at FROM agent_conversations").
        WithArgs("u1", int64(1)).
        WillReturnRows(sqlmock.NewRows([]string{"messages", "updated_at"}).AddRow("[]", time.Now()))
    mock.ExpectExec("INSERT INTO agent_conversations").
        WithArgs("u1", int64(1), sqlmock.AnyArg(), sqlmock.AnyArg()).
        WillReturnResult(sqlmock.NewResult(0, 1))
    mock.ExpectCommit()

    ctx := context.Background()
    q.Run(ctx)
    q.EnqueueConv(ctx, "u1", 1, []repository.StoredChatMessage{
        {ChatMessage: openai.ChatMessage{Role: "user", Content: "hi"}, CreatedAt: time.Now()},
    })

    // 等待消费
    time.Sleep(200 * time.Millisecond)

    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("expectations: %v", err)
    }
}

func TestPersistQueue_LogTask(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("sqlmock: %v", err)
    }
    defer db.Close()

    convRepo := repository.NewConversationRepo(db)
    callLogRepo := repository.NewCallLogRepo(db)
    q := NewPersistQueue(convRepo, callLogRepo, 10)

    mock.ExpectExec("INSERT INTO api_call_log").
        WithArgs("u1", "k1", "miaodi", "put_text", sqlmock.AnyArg()).
        WillReturnResult(sqlmock.NewResult(1, 1))

    ctx := context.Background()
    q.Run(ctx)
    q.EnqueueLog(ctx, "u1", "k1", "miaodi", "put_text")

    time.Sleep(200 * time.Millisecond)

    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("expectations: %v", err)
    }
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /opt/mdagent
go test ./internal/persist/...
```

Expected: PASS（根据实际 SQL 参数顺序调整 `WithArgs`）。

- [ ] **Step 4: Commit**

```bash
git add internal/persist/
git commit -m "persist: add async mysql persistence queue"
```

---

### Task 5: app.go 集成 Redis、Cache、PersistQueue 与启动预加载

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/service/interfaces.go`
- Modify: `main.go`（可选，添加 Redis 地址构造）

- [ ] **Step 1: 在 `internal/service/interfaces.go` 新增 PersistQueue 接口**

```go
// PersistQueue 是异步持久化队列接口。
type PersistQueue interface {
    EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage)
    EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string)
    Run(ctx context.Context)
    Flush(ctx context.Context) error
}
```

需要导入 `context`、`miaodi-agent/internal/repository`。

- [ ] **Step 2: 修改 `internal/app/app.go` 初始化 Redis 与队列**

在 `Run` 函数开头，Repository 初始化之后添加：

```go
redisAddr := cfg.RedisHost + ":" + cfg.RedisPort
redisCache := cache.NewRedisCache(redisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisEnabled)
persistQueue := persist.NewPersistQueue(convRepo, callLogRepo, 1024)
persistQueue.Run(ctx)

if err := seedCache(ctx, redisCache, convRepo, userRepo); err != nil {
    log.Printf("seed cache failed: %v", err)
}
```

并在 `Run` 结尾 shutdown 前添加 Flush：

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := persistQueue.Flush(shutdownCtx); err != nil {
    log.Printf("flush persist queue failed: %v", err)
}
```

- [ ] **Step 3: 在 `internal/app/app.go` 添加 seedCache 函数**

```go
func seedCache(ctx context.Context, c cache.Cache, convRepo *repository.ConversationRepo, userRepo *repository.UserRepo) error {
    if !c.Available(ctx) {
        return fmt.Errorf("redis not available")
    }
    conversations, err := convRepo.ListActiveSince(timeutil.Now().Add(-24 * time.Hour))
    if err != nil {
        return err
    }

    userIDSet := make(map[string]struct{})
    for _, conv := range conversations {
        userIDSet[conv.ChannelUserID] = struct{}{}
    }

    for uid := range userIDSet {
        user, err := userRepo.Get(uid)
        if err != nil {
            log.Printf("seed cache get user %s failed: %v", uid, err)
            continue
        }
        if err := c.SetUser(ctx, user); err != nil {
            log.Printf("seed cache set user %s failed: %v", uid, err)
        }
    }

    for _, conv := range conversations {
        if err := c.SetMessages(ctx, conv.ChannelUserID, conv.ConversationID, conv.Messages); err != nil {
            log.Printf("seed cache set conv %s/%d failed: %v", conv.ChannelUserID, conv.ConversationID, err)
        }
    }
    return nil
}
```

- [ ] **Step 4: 更新 import**

在 `internal/app/app.go` 中新增 import：

```go
"miaodi-agent/internal/cache"
"miaodi-agent/internal/persist"
```

- [ ] **Step 5: 将 cache/persist 注入 Agent 和 ToolExecutor**

修改 `Run` 中的依赖组装：

```go
toolExec := service.NewToolExecutor(miaodi, userRepo, convRepo, pendingRepo, callLogRepo, redisCache, persistQueue)
agent := service.NewAgentWithOptions(llm, cfg.OpenAIModel, userRepo, convRepo, toolExec, service.AgentOptions{
    ModelMaxTokens:  cfg.ModelMaxTokens,
    MaxOutputTokens: cfg.MaxOutputTokens,
}, redisCache, persistQueue)
```

对应地，需要修改 `NewAgentWithOptions` 和 `NewToolExecutor` 签名（见 Task 6/7）。

- [ ] **Step 6: 编译验证**

```bash
cd /opt/mdagent
go build ./...
```

Expected: 此时因 Agent/ToolExecutor 签名未改而报错；继续 Task 6/7 后应通过。

- [ ] **Step 7: Commit**

```bash
git add internal/app/app.go internal/service/interfaces.go
git commit -m "app: wire redis cache and persist queue"
```

---

### Task 6: Agent 集成 Cache 与 PersistQueue

**Files:**
- Modify: `internal/service/agent.go`
- Modify: `internal/service/agent_test.go`（更新构造函数调用）

- [ ] **Step 1: 修改 Agent 结构体**

在 `internal/service/agent.go` 的 `Agent` 结构体中新增：

```go
cache        cache.Cache
persistQueue service.PersistQueue
```

- [ ] **Step 2: 修改构造函数签名**

```go
func NewAgentWithOptions(
    llm LLMClient,
    modelName string,
    userRepo UserStore,
    convRepo ConversationStore,
    toolExec ToolRunner,
    opts AgentOptions,
    cache cache.Cache,
    persistQueue service.PersistQueue,
) *Agent {
```

在返回的 `&Agent{...}` 中新增：

```go
cache:        cache,
persistQueue: persistQueue,
```

- [ ] **Step 3: 修改 `NewAgent` 以透传 cache/persistQueue**

```go
func NewAgent(llm LLMClient, modelName string, userRepo UserStore, convRepo ConversationStore, toolExec ToolRunner, cache cache.Cache, persistQueue service.PersistQueue) *Agent {
    return NewAgentWithOptions(llm, modelName, userRepo, convRepo, toolExec, AgentOptions{}, cache, persistQueue)
}
```

- [ ] **Step 4: 修改用户加载逻辑**

在 `ProcessMessage` 中，将：

```go
user, err := a.userRepo.GetOrCreate(channelUserID)
```

改为：

```go
user, err := a.cache.GetUser(ctx, channelUserID)
if err != nil {
    user, err = a.userRepo.GetOrCreate(channelUserID)
    if err != nil {
        log.Printf("get or create user failed: %v", err)
        return a.debugReturn("agent user load failed", "系统内部错误，请稍后再试")
    }
    if setErr := a.cache.SetUser(ctx, user); setErr != nil {
        debuglog.Printf("cache set user failed user=%s error=%v", channelUserID, setErr)
    }
}
```

- [ ] **Step 5: 修改会话追加逻辑**

将原来的：

```go
if err := a.convRepo.AppendMessage(channelUserID, conversationID, userMsg); err != nil {
```

改为：

```go
storedUserMsg := repository.ChatMessageToStored(userMsg, timeutil.Now())
if err := a.cache.AppendMessages(ctx, channelUserID, conversationID, storedUserMsg); err != nil {
    log.Printf("append user message to cache failed: %v", err)
    debuglog.Printf("agent append user message to cache failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
    if dbErr := a.convRepo.AppendMessage(channelUserID, conversationID, userMsg); dbErr != nil {
        log.Printf("append user message to db fallback failed: %v", dbErr)
        debuglog.Printf("agent append user message fallback failed user=%s conversation=%d error=%v", channelUserID, conversationID, dbErr)
    }
} else {
    a.persistQueue.EnqueueConv(ctx, channelUserID, conversationID, []repository.StoredChatMessage{storedUserMsg})
    debuglog.Printf("agent appended user message user=%s conversation=%d", channelUserID, conversationID)
}
```

需要在 `internal/repository/conversation.go` 中导出 `ChatMessageToStored` 函数（当前为 `chatMessagesToStored`），新增：

```go
func ChatMessageToStored(msg openai.ChatMessage, createdAt time.Time) StoredChatMessage {
    return StoredChatMessage{
        ChatMessage: msg,
        CreatedAt:   createdAt.In(timeutil.BeijingLocation()),
    }
}
```

并将原 `chatMessagesToStored` 改为使用它：

```go
func chatMessagesToStored(messages []openai.ChatMessage, createdAt time.Time) []StoredChatMessage {
    stored := make([]StoredChatMessage, 0, len(messages))
    for _, msg := range messages {
        stored = append(stored, ChatMessageToStored(msg, createdAt))
    }
    return stored
}
```

- [ ] **Step 6: 修改历史读取逻辑**

将原来的：

```go
history, err := a.convRepo.GetMessages(channelUserID, conversationID)
```

改为：

```go
historyStored, err := a.cache.GetMessages(ctx, channelUserID, conversationID)
if err != nil {
    debuglog.Printf("agent cache get history failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
    history, err := a.convRepo.GetMessages(channelUserID, conversationID)
    if err != nil {
        log.Printf("get messages failed: %v", err)
        debuglog.Printf("agent get history failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
        history = []openai.ChatMessage{}
    }
    _ = a.cache.SetMessages(ctx, channelUserID, conversationID, repository.ChatMessagesToStored(history, timeutil.Now()))
    historyStored = repository.ChatMessagesToStored(history, timeutil.Now())
}
history := repository.StoredToChatMessages(historyStored)
```

需要在 `internal/repository/conversation.go` 中导出 `ChatMessagesToStored` 和 `StoredToChatMessages`：

```go
func ChatMessagesToStored(messages []openai.ChatMessage, createdAt time.Time) []StoredChatMessage {
    stored := make([]StoredChatMessage, 0, len(messages))
    for _, msg := range messages {
        stored = append(stored, ChatMessageToStored(msg, createdAt))
    }
    return stored
}

func StoredToChatMessages(stored []StoredChatMessage) []openai.ChatMessage {
    msgs := make([]openai.ChatMessage, 0, len(stored))
    for _, s := range stored {
        msgs = append(msgs, s.ChatMessage)
    }
    return msgs
}
```

原 `storedToChatMessages` 改为调用 `StoredToChatMessages`。

- [ ] **Step 7: 修改助手消息和工具结果持久化逻辑**

在 `ProcessMessage` 中，找到最终回复时的：

```go
if err := a.convRepo.AppendMessage(channelUserID, conversationID, assistantMsg); err != nil {
```

改为：

```go
storedAssistantMsg := repository.ChatMessageToStored(assistantMsg, timeutil.Now())
if err := a.cache.AppendMessages(ctx, channelUserID, conversationID, storedAssistantMsg); err != nil {
    log.Printf("append assistant message to cache failed: %v", err)
    debuglog.Printf("agent append assistant message to cache failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
    if dbErr := a.convRepo.AppendMessage(channelUserID, conversationID, assistantMsg); dbErr != nil {
        log.Printf("append assistant message fallback failed: %v", dbErr)
    }
} else {
    a.persistQueue.EnqueueConv(ctx, channelUserID, conversationID, []repository.StoredChatMessage{storedAssistantMsg})
}
```

同样地，找到工具调用结果回写处：

```go
if err := a.convRepo.AppendMessages(channelUserID, conversationID, append([]openai.ChatMessage{assistantMsg}, toolResults...)...); err != nil {
```

改为：

```go
roundMsgs := append([]openai.ChatMessage{assistantMsg}, toolResults...)
storedRoundMsgs := repository.ChatMessagesToStored(roundMsgs, timeutil.Now())
if err := a.cache.AppendMessages(ctx, channelUserID, conversationID, storedRoundMsgs...); err != nil {
    log.Printf("append tool round messages to cache failed: %v", err)
    debuglog.Printf("agent append tool round to cache failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
    if dbErr := a.convRepo.AppendMessages(channelUserID, conversationID, roundMsgs...); dbErr != nil {
        log.Printf("append tool round fallback failed: %v", dbErr)
    }
} else {
    a.persistQueue.EnqueueConv(ctx, channelUserID, conversationID, storedRoundMsgs)
    debuglog.Printf("agent appended tool round user=%s conversation=%d messages=%d", channelUserID, conversationID, len(roundMsgs))
}
```

- [ ] **Step 8: 更新 import**

在 `internal/service/agent.go` 中新增：

```go
"miaodi-agent/internal/cache"
"miaodi-agent/internal/repository"
```

- [ ] **Step 9: 更新 `agent_test.go` 中的构造函数调用**

所有 `NewAgent` / `NewAgentWithOptions` 调用需补充最后两个参数：`cache` 和 `persistQueue`。

由于测试通常不需要真实 Redis，可创建一个 `internal/cache` 包中的 `NopCache` 或直接在测试中传入 nil 并判断 nil。更简单：修改 `Agent` 方法中允许 `cache` 为 nil，但当前实现中 cache 方法被直接调用会 panic。

推荐在 `internal/cache/cache.go` 中新增 NopCache：

```go
// NopCache 是一个始终返回 error 的 Cache，用于测试。
// 它确保业务层在测试环境下总是回源 MySQL，保持现有测试行为不变。
type NopCache struct{}

func (NopCache) Available(context.Context) bool { return false }
func (NopCache) GetUser(context.Context, string) (*model.User, error) { return nil, fmt.Errorf("nop") }
func (NopCache) SetUser(context.Context, *model.User) error { return fmt.Errorf("nop") }
func (NopCache) GetMessages(context.Context, string, int64) ([]repository.StoredChatMessage, error) { return nil, fmt.Errorf("nop") }
func (NopCache) SetMessages(context.Context, string, int64, []repository.StoredChatMessage) error { return fmt.Errorf("nop") }
func (NopCache) AppendMessages(context.Context, string, int64, ...repository.StoredChatMessage) error { return fmt.Errorf("nop") }
func (NopCache) ClearConversation(context.Context, string, int64) error { return fmt.Errorf("nop") }
func (NopCache) GetRecentLogs(context.Context, string) ([]repository.UserCallLog, error) { return nil, fmt.Errorf("nop") }
func (NopCache) SetRecentLogs(context.Context, string, []repository.UserCallLog) error { return fmt.Errorf("nop") }
func (NopCache) AppendLog(context.Context, string, repository.UserCallLog) error { return fmt.Errorf("nop") }
```

测试中传入 `cache.NopCache{}` 和 `&nopPersistQueue{}`（在测试文件中定义）。

- [ ] **Step 10: 运行测试**

```bash
cd /opt/mdagent
go test ./internal/service/...
```

Expected: PASS。

- [ ] **Step 11: Commit**

```bash
git add internal/service/agent.go internal/service/agent_test.go internal/cache/cache.go internal/repository/conversation.go
git commit -m "agent: integrate redis cache and async persistence"
```

---

### Task 7: ToolExecutor 集成 Cache 与 PersistQueue

**Files:**
- Modify: `internal/service/miaodi_tool.go`
- Modify: `internal/service/miaodi_tool_test.go`

- [ ] **Step 1: 修改 ToolExecutor 结构体**

在 `internal/service/miaodi_tool.go` 的 `ToolExecutor` 结构体中新增：

```go
cache        cache.Cache
persistQueue service.PersistQueue
```

- [ ] **Step 2: 修改 `NewToolExecutor` 签名**

```go
func NewToolExecutor(
    miaodi MiaodiClient,
    userRepo *repository.UserRepo,
    convRepo ConversationStore,
    pendingRepo *repository.PendingImageRepo,
    callLogRepo *repository.CallLogRepo,
    cache cache.Cache,
    persistQueue service.PersistQueue,
) *ToolExecutor {
```

返回时赋值新增字段。

- [ ] **Step 3: 用户绑定/路径/解绑后同步更新 Redis**

在 `bindMiaodiKey`、`bindMiaodiByEmailCode`、`setSavePath`、`unbindMiaodiKey` 方法中，MySQL 更新成功后立即调用 `cache.SetUser`：

例如 `bindMiaodiKey`：

```go
if err := e.userRepo.UpdateAPIKeyAndStatus(channelUserID, args.Key, userStatusBound); err != nil {
    return "绑定失败：数据库错误"
}
user.APIKey = args.Key
user.Status = userStatusBound
if err := e.cache.SetUser(context.Background(), user); err != nil {
    debuglog.Printf("bind key cache set failed user=%s error=%v", channelUserID, err)
}
e.recordCall(channelUserID, args.Key, "bind_key")
return "绑定成功，你现在可以保存笔记和图片了"
```

其他三个方法类似：先更新内存中的 `user` 字段，再 `e.cache.SetUser`。

- [ ] **Step 4: 重置会话时同步清理 Redis**

在 `resetConversation` 中：

```go
if err := e.cache.ClearConversation(context.Background(), channelUserID, conversationID); err != nil {
    debuglog.Printf("reset clear cache failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
}
if err := e.convRepo.Clear(channelUserID, conversationID); err != nil {
    return fmt.Sprintf("重置失败：%v", err)
}
return "已清空当前会话，我们可以重新开始。"
```

- [ ] **Step 5: 修改 `recordCall` 为 Redis 缓存 + 异步 MySQL**

```go
func (e *ToolExecutor) recordCall(channelUserID, apikey, action string) {
    if e.callLogRepo == nil {
        return
    }
    ctx := context.Background()
    log := repository.UserCallLog{Action: action, CreatedAt: timeutil.Now()}
    if err := e.cache.AppendLog(ctx, channelUserID, log); err != nil {
        debuglog.Printf("append log to cache failed user=%s action=%s error=%v", channelUserID, action, err)
        _ = e.callLogRepo.Record(channelUserID, apikey, "miaodi", action)
        return
    }
    e.persistQueue.EnqueueLog(ctx, channelUserID, apikey, "miaodi", action)
}
```

需要新增 `time` import（`repository.UserCallLog` 使用 `time.Time`）。

- [ ] **Step 6: 最近笔记查询优先读 Redis**

在 `listRecentNotes` 中：

```go
logs, err := e.cache.GetRecentLogs(context.Background(), channelUserID)
if err != nil {
    debuglog.Printf("recent logs cache miss user=%s error=%v", channelUserID, err)
    logs, err = e.callLogRepo.RecentByUser(channelUserID, args.Limit)
    if err != nil {
        return fmt.Sprintf("查询失败：%v", err)
    }
    _ = e.cache.SetRecentLogs(context.Background(), channelUserID, logs)
}
```

- [ ] **Step 7: 按日期查询后回写 Redis 缓存**

在 `queryNotesByDate` 中，查询 `callLogRepo.ByDate` 后：

```go
logs, err := e.callLogRepo.ByDate(channelUserID, args.Date)
if err != nil {
    return fmt.Sprintf("查询失败：%v", err)
}
_ = e.cache.SetRecentLogs(context.Background(), channelUserID, logs)
```

- [ ] **Step 8: 更新 import**

在 `internal/service/miaodi_tool.go` 中新增：

```go
"context"
"time"

"miaodi-agent/internal/cache"
```

- [ ] **Step 9: 更新 `miaodi_tool_test.go` 中的构造函数调用**

所有 `NewToolExecutor` 调用需补充 `cache` 和 `persistQueue`。使用 `cache.NopCache{}` 和测试用的 `nopPersistQueue`：

```go
type nopPersistQueue struct{}

func (nopPersistQueue) EnqueueConv(context.Context, string, int64, []repository.StoredChatMessage) {}
func (nopPersistQueue) EnqueueLog(context.Context, string, string, string, string) {}
func (nopPersistQueue) Run(context.Context) {}
func (nopPersistQueue) Flush(context.Context) error { return nil }
```

- [ ] **Step 10: 运行测试**

```bash
cd /opt/mdagent
go test ./internal/service/...
```

Expected: PASS。

- [ ] **Step 11: Commit**

```bash
git add internal/service/miaodi_tool.go internal/service/miaodi_tool_test.go
git commit -m "tool: integrate cache and async persistence for users/logs"
```

---

### Task 8: 更新 main.go / docker-compose.yml（可选）

**Files:**
- Modify: `docker-compose.yml`
- Modify: `README.md`（可选，补充 Redis 说明）

- [ ] **Step 1: 在 `docker-compose.yml` 中增加 Redis 服务**

```yaml
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: redis-server --appendonly yes
    volumes:
      - redis_data:/data
    networks:
      - miaodi

volumes:
  redis_data:
```

并确保 app 服务 `depends_on` 包含 `redis`，环境变量默认使用服务名 `redis`：

```yaml
    environment:
      REDIS_HOST: redis
      REDIS_PORT: 6379
```

- [ ] **Step 2: 运行 Docker Compose 语法检查**

```bash
cd /opt/mdagent
docker compose config
```

Expected: 配置有效（不需要实际启动）。

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "docker: add redis service"
```

---

### Task 9: 全量测试与验证

**Files:**
- All

- [ ] **Step 1: 运行全部单元测试**

```bash
cd /opt/mdagent
go test ./... -cover
```

Expected: 所有包 PASS，覆盖率不低于改造前（约 80%+）。

- [ ] **Step 2: 检查 Redis 未启用时的降级行为**

临时设置环境变量：

```bash
REDIS_ENABLED=false go test ./internal/service/... ./internal/app/...
```

Expected: PASS（业务层应正确回源 MySQL）。

- [ ] **Step 3: 编译最终产物**

```bash
cd /opt/mdagent
go build -o miaodi-agent .
```

Expected: 编译成功。

- [ ] **Step 4: 运行 go vet / staticcheck（如有）**

```bash
cd /opt/mdagent
go vet ./...
```

Expected: 无严重问题。

- [ ] **Step 5: Commit 任何修复**

```bash
git add -A
git commit -m "test: verify redis cache integration and fallback"
```

---

## 计划自审

### Spec 覆盖检查

| 需求 | 对应任务 |
|------|----------|
| 启动时从 MySQL 拉最近 24h 数据到 Redis | Task 2 (`ListActiveSince`) + Task 5 (`seedCache`) |
| 每个对话同步写 Redis、异步写 MySQL | Task 6 (`Agent.ProcessMessage` 三段 append) |
| Redis 连不上读 MySQL | Task 3 (Cache 出错) + Task 6/7 (业务层 fallback) |
| 高频重复读首次写 Redis | Task 6 (用户/会话读 miss 回写) + Task 7 (最近日志) |
| 24h TTL | Task 3 (`const ttl = 24 * time.Hour`) |
| 不阻塞主流程 | Task 4 (PersistQueue) + Task 6/7 (enqueue 不阻塞) |

### Placeholder 检查

- 无 TBD/TODO。
- 所有代码块包含实际代码。
- 所有命令包含具体路径和预期结果。

### 类型一致性检查

- `StoredChatMessage` 统一使用 `repository.StoredChatMessage`。
- `Cache` 接口与 `RedisCache` 方法签名一致。
- `PersistQueue` 与 `Queue` 接口方法签名一致。
- 构造函数传参顺序在 Task 5/6/7 中保持一致。
