# 喵滴 AI Agent（传送鸽 Bot）

这是一个对接 传送鸽 Bot 回调与喵滴 API 的 AI Agent。收到用户消息后，它调用标准 OpenAI 格式的大模型 API（如 DeepSeek）进行 tool-call 决策，自动完成喵滴 Key 绑定、邮箱验证码绑定、保存文本笔记、图片落库等操作。

## 目录

项目独立在 `/opt/mdagent`，只依赖原仓库 `weixin-service/pkg/client/miaodi.go` 中的喵滴 API 客户端逻辑。

## 环境变量

| 变量 | 说明 | 默认值 |
|---|---|---|
| `PORT` | 服务端口 | `8080` |
| `HOST_PORT` | Docker Compose 暴露到宿主机的端口 | `8080` |
| `DB_HOST` | MySQL 主机 | `localhost` |
| `DB_PORT` | MySQL 端口 | `3306` |
| `DB_USER` | MySQL 用户名 | `root` |
| `DB_PASS` | MySQL 密码 | - |
| `DB_NAME` | MySQL 数据库名 | `miaodi_agent` |
| `DB_MAX_OPEN` | 最大打开连接数 | `50` |
| `DB_MAX_IDLE` | 最大空闲连接数 | `10` |
| `TZ` | 容器日志和应用默认时区 | `Asia/Shanghai` |
| `APP_DEBUG` | 打印 webhook 入参、程序回包、Agent 决策、工具调用和应用错误 | `false` |
| `OPENAI_API_KEY` | 大模型 API Key | - |
| `OPENAI_BASE_URL` | OpenAI 兼容 Base URL | `https://api.deepseek.com/v1` |
| `OPENAI_MODEL` | 模型名 | `deepseek-chat` |
| `OPENAI_MODEL_MAX_TOKENS` | 模型上下文窗口大小，用于裁剪历史避免溢出 | `8192` |
| `OPENAI_MAX_OUTPUT_TOKENS` | 单次模型最大输出 token 数 | `1024` |
| `OPENAI_DEBUG` | 打印模型请求体和错误响应，排查模型兼容问题时使用 | `false` |
| `CALLBACK_PATH` | 传送鸽回调路径 | `/callback` |

## 启动

```bash
cd /opt/mdagent
go mod tidy
go build -o miaodi-agent .

export OPENAI_API_KEY=sk-xxxx
export DB_USER=root
export DB_PASS=yourpass
export DB_NAME=miaodi_agent
export OPENAI_MODEL_MAX_TOKENS=8192
export OPENAI_MAX_OUTPUT_TOKENS=1024

./miaodi-agent
```

也可以通过 Docker Compose 启动应用。Compose 只启动 Agent 容器，MySQL 使用 `.env` 中配置的远程数据库：

```bash
cp .env.example .env
docker compose pull
docker compose up -d --remove-orphans
```

如果宿主机 `8080` 已被占用，可以临时指定其他端口：

```bash
HOST_PORT=18080 docker compose up -d --remove-orphans
```

当前服务没有 Redis 依赖：用户状态、会话历史、图片待上传队列和统计日志都写入 MySQL。之前 Compose 里的 Redis 是历史遗留配置，代码没有读写 Redis，所以已移除，避免启动无效组件。

服务启动时会自动建表：

- `agent_users`
- `agent_conversations`
- `pending_images`
- `api_call_log`

新用户默认保存路径为：书本《传送鸽》/ 章节《喵滴鸽》，标题默认使用当天日期。

应用内记录时间、默认标题日期、统计页面时间按北京时间（`Asia/Shanghai`）处理；MySQL 连接会话也会设置为 `+08:00`。

## 对接 传送鸽

1. 在 传送鸽 开发者后台注册一个 bot，把 `callbackUrl` 填为 `http://<你的域名>/callback`。
2. 用户订阅 bot 后发送消息，传送鸽会把消息 POST 到本服务。
3. 本服务在 10 秒内返回 `{"success": true, "reply": {"content": "..."}}`。

## 统计看板

服务启动后访问：

```text
http://localhost:8080/stats
```

页面展示：

- 总用户数、已绑定用户数、总会话数、近 7 天请求数
- 近 30 天 / 近 7 天请求趋势折线图
- 近 30 天接口调用类型占比饼图
- 7 天 / 30 天活跃用户数

同时提供 JSON 接口：

```text
GET /api/stats
```

## 支持的模型能力（tools）

- `bind_miaodi_key(key)`：绑定喵滴 API Key。
- `send_miaodi_email_code(email)`：向喵滴账号邮箱发送验证码。
- `bind_miaodi_by_email_code(email?, code)`：使用邮箱验证码换取喵滴 API Key 并绑定。
- `set_save_path(book, chapter, title)`：设置保存路径。
- `get_user_profile()`：查看绑定状态和保存路径。
- `get_miaodi_key()`：查看当前绑定的喵滴 API Key。
- `get_miaodi_annual_report()`：获取喵滴年度报告链接。
- `unbind_miaodi_key()`：解除当前喵滴绑定。
- `save_text_note(content, title?)`：保存文本笔记。
- `save_image_note(image_url, title?)`：保存图片到待上传队列。
- `reset_conversation()`：清空当前会话历史。
- `show_help()`：返回 Bot 能力说明。
- `list_recent_notes(limit?)`：列出最近保存的笔记摘要。
- `query_notes_by_date(date)`：按日期查询已保存笔记。

你可以直接用自然语言与 Bot 交流，例如：

- "绑定我的喵滴 key：xxxxx"
- "用邮箱 user@example.com 绑定喵滴"
- "验证码 123456"
- "查看当前绑定 key"
- "年度报告地址"
- "解除绑定"
- "把后续内容保存到《日记》第 3 章《今天》"
- "帮我清空刚才的对话"
- "最近我保存了什么？"
- "2026-06-30 那天我保存了哪些笔记？"

Bot 会通过 tool-call 自动调用合适的工具完成操作。

为了兼容低参数量模型，服务会先在本地识别高置信度意图（帮助、重置、绑定 Key、邮箱验证码绑定、解绑、年度报告、查看 Key、设置路径、保存文本/图片、查询最近或指定日期记录），命中后直接执行工具；未命中时再进入 LLM tool-call 流程。LLM 请求前会根据 `OPENAI_MODEL_MAX_TOKENS` 和 `OPENAI_MAX_OUTPUT_TOKENS` 自动裁剪旧会话历史，降低 token 溢出概率。

排查真机问题时可以临时开启应用层调试日志：

```bash
APP_DEBUG=true
```

开启后会打印 webhook 请求体、程序响应、Agent 处理流程、本地意图路由、工具调用参数、工具结果和关键错误。

排查模型兼容问题时可以同时开启模型请求日志：

```bash
OPENAI_DEBUG=true
```

开启后日志会打印模型请求 URL、请求体、错误状态码和错误响应体。上述日志可能包含用户消息内容、绑定 key 或其他敏感参数，生产环境排查完成后应关闭。

## 图片处理说明

和 `weixin-service` 一样，图片不直接上传到喵滴，而是写入 `pending_images` 表：

```sql
SELECT * FROM pending_images WHERE status = 'pending' ORDER BY created_at ASC;
```

你需要单独实现一个定时任务（或复用原仓库的扫描逻辑）消费该表，调用 `MiaodiClient.UpImage` 上传并把 `status` 更新为 `done`/`failed`。

## 测试回调

```bash
curl -X POST http://localhost:8080/callback \
  -H "Content-Type: application/json" \
  -d '{
    "eventType": "user_message",
    "bot": {"id": 1, "name": "喵滴助手"},
    "conversation": {"id": 100},
    "user": {"userId": "user_abc", "username": "*"},
    "message": {"id": 1, "content": "绑定我的喵滴 key：xxxxx", "createTime": "2026-06-30 10:00:00"}
  }'
```

## 关键约束

- 传送鸽回调超时 10 秒，本服务对每次 LLM 请求设 8 秒超时，整体回调处理 9 秒超时。
- `clientSecret` 不需要配置在本服务；本服务只作为被动回调端。
- 建议在生产环境配置 HTTPS、日志和监控。
