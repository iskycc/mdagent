# Todo

本文档记录当前项目审查发现的问题、影响范围和建议修复方案。图片保存/上传相关能力当前不作为支持能力处理，相关约束见 `Agents.md`。

## 1. 回调接口缺少鉴权

### 问题

`internal/handler/callback.go` 的 `/callback` 只校验 HTTP method 和 `eventType`，没有校验签名、共享密钥、来源 IP 或请求时间戳。服务会直接信任 payload 中的 `user.userId`、`conversation.id` 和消息内容。

同时，工具层提供了 `get_miaodi_key`，会直接返回用户完整喵滴 API Key；`get_miaodi_annual_report` 也会把 Key 拼到年度报告 URL 中。如果回调地址暴露在公网，攻击者可以伪造指定 `userId` 的消息来读取或操作该用户的喵滴账号。

### 影响

- 任意伪造用户消息。
- 读取已绑定用户的完整 API Key。
- 伪造保存、解绑、查询等操作。
- 消耗 LLM 和喵滴 API 调用额度。

### 解决方案

1. 增加回调密钥配置，例如 `CALLBACK_SECRET`。
2. 要求传送鸽回调请求携带签名头，例如：
   - `X-MD-Timestamp`
   - `X-MD-Signature`
3. 使用 `HMAC-SHA256(secret, timestamp + "." + rawBody)` 校验签名。
4. 拒绝时间戳过旧的请求，建议窗口 5 分钟。
5. 在 `config.Config.Validate()` 中校验生产环境必须配置 `CALLBACK_SECRET`。
6. 增加测试：
   - 无签名返回 401。
   - 错签名返回 401。
   - 过期 timestamp 返回 401。
   - 正确签名正常处理。

### 建议优先级

P0。先修。

## 2. 统计接口没有访问控制

### 问题

`internal/handler/stats.go` 直接注册 `/stats` 和 `/api/stats`，没有任何认证。页面暴露用户数、绑定用户数、调用趋势、活跃用户、性能统计、LLM 调用和 token 消耗。

### 影响

- 业务数据泄露。
- 暴露服务调用量和活跃度。
- 暴露模型成本趋势。

### 解决方案

1. 增加 `STATS_TOKEN` 配置。
2. `/stats` 和 `/api/stats` 支持以下任一校验方式：
   - `Authorization: Bearer <token>`
   - 查询参数 `?token=<token>`，只用于临时排障，不建议长期使用。
3. 未配置 `STATS_TOKEN` 时：
   - 生产环境拒绝启动，或
   - 默认不注册统计路由。
4. 增加测试：
   - 无 token 返回 401。
   - 错误 token 返回 401。
   - 正确 token 返回 200。

### 建议优先级

P0。

## 3. 生产 Compose 默认开启敏感调试日志

### 问题

`docker-compose.yml` 当前设置：

```yaml
APP_DEBUG: "true"
OPENAI_DEBUG: "true"
```

`APP_DEBUG` 会打印 webhook 请求体、Agent 决策和工具参数。`OPENAI_DEBUG` 会打印完整模型请求体和错误响应。相关日志可能包含：

- 用户消息原文。
- 喵滴 API Key。
- 邮箱地址。
- 邮箱验证码。
- 保存的笔记内容。

代码中 `bindMiaodiByEmailCode` 在验证码失败时也会打印 `email` 和 `code`。

### 影响

- 敏感信息进入容器日志。
- 日志平台、运维终端、备份系统都可能持有用户凭证。
- 出现问题后难以彻底清理泄露数据。

### 解决方案

1. 修改 `docker-compose.yml` 默认值：
   - `APP_DEBUG: "false"`
   - `OPENAI_DEBUG: "false"`
2. 增加日志脱敏函数，例如 `debuglog.MaskSensitive()`。
3. 对以下字段做脱敏：
   - `key`
   - `apikey`
   - `api_key`
   - `code`
   - `email`
   - `Authorization`
   - `content`
4. OpenAI debug 日志不要打印完整 body，改为打印：
   - URL
   - model
   - messages count
   - tools count
   - max_tokens
   - estimated prompt tokens
5. 删除或脱敏验证码失败日志中的明文 code。
6. 增加测试覆盖脱敏行为。

### 建议优先级

P0。

## 4. `get_miaodi_key` 会返回完整 Key

### 问题

`internal/service/miaodi_tool.go` 中 `getMiaodiKey` 会把完整 API Key 返回给用户。当前回调鉴权缺失时，这是直接泄露凭证的入口。即使补了回调鉴权，完整返回 Key 仍然会增加聊天窗口、日志和第三方平台泄露风险。

### 影响

- Key 可能出现在聊天记录、Webhook 日志、LLM 上下文和调试日志中。
- 用户误转发消息时泄露完整 Key。

### 解决方案

1. 默认只返回脱敏 Key，例如 `abcd***wxyz`。
2. 如果确实需要完整 Key，增加二次确认流程：
   - 用户发送“显示完整 key”。
   - 系统回复风险提示。
   - 用户再次确认后只在本轮返回。
3. 更推荐完全移除“查看完整 Key”能力，仅保留绑定状态和脱敏展示。
4. 年度报告 URL 如必须包含 Key，也应提示该链接包含敏感凭证，且避免写入持久化会话。

### 建议优先级

P0。

## 5. Redis 用户缓存与邮箱验证码绑定状态不一致

### 问题

`sendMiaodiEmailCode` 成功后只更新了 MySQL 和当前内存里的 `user` 对象：

- `email = args.Email`
- `status = waiting_email_code`

但没有调用 `cache.SetUser()` 刷新 Redis。Redis 默认启用时，下一条“验证码 123456”可能从 Redis 读到旧用户状态，导致本地意图路由无法识别验证码绑定流程，或 LLM 工具调用缺少邮箱上下文。

### 影响

- 邮箱验证码绑定流程不稳定。
- 用户明明刚发送邮箱，却被提示“请先提供邮箱获取验证码”。
- Redis 开启和关闭时行为不一致。

### 解决方案

1. 在 `sendMiaodiEmailCode` 成功更新 DB 后调用：

```go
if err := e.cache.SetUser(context.Background(), user); err != nil {
    debuglog.Printf("send email cache set failed user=%s error=%v", channelUserID, err)
}
```

2. 补充单元测试：
   - Redis 可用时发送邮箱后缓存被更新。
   - 下一条纯验证码能走 `bind_miaodi_by_email_code`。
3. 可进一步把用户状态更新封装成一个 helper，避免绑定 Key、邮箱、路径等路径遗漏缓存刷新。

### 建议优先级

P1。

## 6. 异步持久化存在丢数据窗口

### 问题

当前会话和调用日志正常路径是：

1. 先写 Redis。
2. 再通过 `PersistQueue` 异步写 MySQL。

`PersistQueue.EnqueueConv` 和 `EnqueueLog` 在 ctx 结束或队列无法写入时会静默放弃任务。`process` 写 MySQL 失败重试 3 次后只打印日志，不做死信、补偿或告警。

### 影响

- 请求返回成功，但 MySQL 中没有对应会话或调用日志。
- Redis TTL 到期后数据永久丢失。
- 进程退出时 worker 可能还有正在处理的任务，`Flush` 只能处理 channel 中剩余任务，不能等待 worker 当前任务完成。

### 解决方案

短期方案：

1. `EnqueueConv` 和 `EnqueueLog` 返回 `bool` 或 `error`，调用方感知入队失败。
2. 入队失败时同步 fallback 写 MySQL。
3. `process` 最终失败时写入 dead-letter 表，例如 `persist_dead_letters`。
4. 增加队列长度和失败次数指标。

中期方案：

1. 对关键数据采用 MySQL 优先写入，Redis 只做读缓存。
2. 会话写入可以合并批量写，但不能只依赖 Redis 成功来确认业务成功。
3. shutdown 时使用 `sync.WaitGroup` 等待 worker 完成当前任务。

### 建议优先级

P1。

## 7. Webhook 缺少幂等去重

### 问题

`payload.message.id` 只用于 debug 日志，没有落库，也没有唯一约束。Webhook 平台重试、网络超时、服务 9 秒超时返回前后重投，都可能让同一条用户消息被处理多次。

### 影响

- 同一文本笔记重复保存。
- 同一工具重复调用。
- 重复消耗 LLM 和外部 API。
- 用户看到不一致回复。

### 解决方案

1. 新增表 `processed_messages`：

```sql
CREATE TABLE processed_messages (
  channel_user_id VARCHAR(128) NOT NULL,
  conversation_id BIGINT NOT NULL,
  message_id BIGINT NOT NULL,
  reply TEXT,
  status VARCHAR(32) NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (channel_user_id, conversation_id, message_id)
);
```

2. 处理开始前插入 `status = processing`。
3. 如果主键冲突：
   - `done`：直接返回已保存 reply。
   - `processing` 且未超时：返回“正在处理，请稍后”或等待短时间。
   - `failed` 或超时 processing：允许重试。
4. 处理成功后更新 `status = done, reply = ?`。
5. 增加并发测试和重复消息测试。

### 建议优先级

P1。

## 8. README、Compose 和实际实现不一致

### 问题

README 说“当前服务没有 Redis 依赖”，但实际代码会创建 Redis cache，启动时 seed cache，Compose 也启动 Redis 并设置 `REDIS_ENABLED=true`。

### 影响

- 部署人员会误判 Redis 是否必须。
- 排障时会忽略缓存一致性问题。
- 文档说明与实际行为冲突。

### 解决方案

二选一，必须统一：

方案 A：保留 Redis。

1. README 明确 Redis 是默认启用缓存。
2. 说明 `REDIS_ENABLED=false` 时会降级 MySQL。
3. Compose 保留 Redis，但关闭 debug。

方案 B：移除 Redis 运行依赖。

1. 默认 `REDIS_ENABLED=false`。
2. Compose 移除 Redis。
3. 代码中保留可选缓存，但默认不启用。

当前代码已经深度接入 Redis，建议短期选择方案 A。

### 建议优先级

P1。

## 9. API Key 明文存储且字段长度不一致

### 问题

`agent_users.apikey` 使用 `VARCHAR(128)` 保存完整 Key。`pending_images.apikey` 也保存完整 Key。`api_call_log.apikey` 是 `VARCHAR(64)`，与用户表长度不一致。

### 影响

- 数据库泄露时所有用户 Key 泄露。
- 日志表可能因 Key 超长导致截断或写入失败。
- 敏感数据散落多张表，清理和轮换困难。

### 解决方案

1. 不再在调用日志中保存完整 Key：
   - 改为保存 `apikey_hash` 或 `key_id`。
   - 展示统计不依赖完整 Key。
2. 用户表中如必须保存 Key，使用应用层加密：
   - 增加 `KEY_ENCRYPTION_SECRET`。
   - AES-GCM 加密后落库。
   - 启动时校验 secret 存在。
3. 增加 Key 轮换脚本：
   - 读取旧明文。
   - 写入新密文字段。
   - 验证后废弃旧字段。
4. 字段长度统一，避免截断。

### 建议优先级

P1。

## 10. 回调请求体没有大小限制

### 问题

`callback.go` 使用 `io.ReadAll(r.Body)` 读取完整请求体，没有 `http.MaxBytesReader` 限制。

### 影响

- 大请求体会占用内存。
- 可被低成本请求拖垮服务。

### 解决方案

1. 增加配置 `MAX_CALLBACK_BODY_BYTES`，默认例如 `1MB`。
2. 在读取 body 前包一层：

```go
r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxCallbackBodyBytes)
```

3. 超限返回 `413 Request Entity Too Large`。
4. 增加单元测试覆盖超大 body。

### 建议优先级

P2。

## 11. 内存指标无限增长

### 问题

`internal/metrics/metrics.go` 中每次调用都会把耗时 append 到 `duration []float64`，没有上限、窗口或采样。

### 影响

- 进程长期运行后内存持续增长。
- `/stats` 计算百分位时复制和排序全部历史，数据量大时会变慢。

### 解决方案

1. 每个指标只保留最近 N 条，例如 10,000 条。
2. 或使用直方图桶统计，不保存每条样本。
3. `/stats` 页面明确展示“最近窗口”而不是“进程启动以来全部”。
4. 增加压力测试或单元测试确认样本数量不会无限增长。

### 建议优先级

P2。

## 12. 外部喵滴 API 客户端不可配置

### 问题

`pkg/client/miaodi.go` 中喵滴 API URL 全部硬编码，生产、测试、灰度和故障切换都不方便。

### 影响

- 无法通过环境变量切换 endpoint。
- 故障排查和压测需要改代码。
- 单元测试虽然可替换 HTTP client，但运行时不可配置。

### 解决方案

1. 增加配置：
   - `MIAODI_API_BASE_URL`
   - `MIAODI_MAIL_API_URL`
   - `MIAODI_PICTURE_API_URL`
2. `MiaodiClient` 增加 options 或 config 构造函数。
3. 默认值保持当前线上 URL。
4. 增加配置加载和客户端 URL 拼接测试。

### 建议优先级

P3。

## 13. 自动建表能力不等于完整迁移体系

### 问题

仓库层 `EnsureTable` 使用 `CREATE TABLE IF NOT EXISTS` 和少量 `ALTER TABLE`。这适合早期项目，但随着字段加密、幂等表、鉴权配置和日志结构变更，缺少版本化迁移会增加生产变更风险。

### 影响

- 表结构演进不可追踪。
- 回滚困难。
- 多环境 schema 可能不一致。

### 解决方案

1. 引入迁移工具，例如 `golang-migrate`。
2. 新建 `migrations/`：
   - `0001_init.sql`
   - `0002_callback_dedup.sql`
   - `0003_key_encryption.sql`
3. 启动时只执行迁移，不在 repository 中散落 schema 变更逻辑。
4. CI 中增加迁移验证。

### 建议优先级

P3。

## 推荐执行顺序

1. P0：回调鉴权、统计鉴权、关闭敏感 debug、Key 返回脱敏。✅
2. P1：修 Redis 用户缓存、Webhook 幂等、异步持久化补偿、文档与 Compose 对齐、Key 存储治理。
3. P2：请求体大小限制、metrics 有界化。
4. P3：喵滴 endpoint 配置化、数据库迁移体系。

每个任务完成后至少运行：

```bash
go test ./...
go vet ./...
```
