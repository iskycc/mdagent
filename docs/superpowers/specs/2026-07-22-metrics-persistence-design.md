# Metrics 性能统计持久化设计

## 背景

当前 `internal/metrics/metrics.go` 的 `Recorder` 把所有性能样本保存在内存中。进程重启后，`/stats` 页面的性能统计 tab 会清空，无法观察长期趋势。

## 目标

1. 将性能统计原始样本持久化到 MySQL。
2. 启动时优先从 Redis 加载缓存；Redis 为空或不可用时，从 MySQL 回源并回填 Redis。
3. `/stats` 读取时优先使用 Redis 缓存的聚合快照；缓存缺失时从内存聚合并回写 Redis。
4. 保持现有 `metrics.Record/Start/Finish/Snapshot` 接口不变，对调用方透明。

## 架构

```
调用方 -> metrics.Record/Start/Finish -> 内存 metric -> 异步 flush -> MySQL metric_samples
                                                    |
                                                    v
                                              聚合快照 -> Redis (TTL 5min)
                                                    ^
/stats -> metrics.Snapshot -> 优先 Redis 缓存快照 --|
                              缺失：从内存聚合 -> 写 Redis
```

## 数据模型

新增表 `metric_samples`：

```sql
CREATE TABLE IF NOT EXISTS metric_samples (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(128) NOT NULL COMMENT '指标名',
    duration_ms DOUBLE NOT NULL COMMENT '耗时毫秒',
    success TINYINT(1) NOT NULL COMMENT '是否成功',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_name_created (name, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
```

## 组件

### 1. `internal/repository/metric_sample.go`

- `MetricSampleRepo`：操作 `metric_samples` 表。
- `EnsureTable()`：建表。
- `Save(samples []MetricSample) error`：批量插入样本。
- `LoadRecent(limit int) ([]MetricSample, error)`：加载最近 `limit` 条样本。

### 2. `internal/metrics/metrics.go` 改造

`Recorder` 增加：

- `store MetricStore`：持久化存储接口。
- `pending []MetricSample` + `pendingMu`：待持久化样本缓冲。
- `flushInterval time.Duration`：后台 flush 周期，默认 30 秒。
- `maxPending int`：缓冲上限，默认 1000 条。

新增 API：

- `Init(store MetricStore) error`：启动时加载历史样本。
- `Run(ctx context.Context)`：启动后台 flush goroutine。
- `Flush() error`：立即 flush 缓冲到 MySQL。
- `Snapshot() []MetricSnapshot`：优先返回 Redis 缓存快照；缺失时从内存聚合并回写 Redis。

内部流程：

- `Record(name, d, success)`：写内存 + 将样本加入 `pending`。
- 后台 flush：每 30 秒或 `pending` 满 1000 条时，批量写入 MySQL。
- `Init`：
  1. 尝试从 Redis 加载缓存快照/样本；命中则恢复内存。
  2. 未命中则从 MySQL `LoadRecent(maxSamples)` 加载，并回填 Redis。
- `Snapshot`：
  1. 如果 Redis 可用且缓存未过期，直接返回缓存的聚合快照。
  2. 否则从内存聚合，写入 Redis（TTL 5 分钟），再返回。

### 3. Redis 缓存键

- 样本列表：`md:metrics:samples:<name>`，List 结构，最多保留最近 10000 条。
- 聚合快照：`md:metrics:snapshot`，String（JSON），TTL 5 分钟。

### 4. `internal/app/app.go` 集成

- 创建 `MetricSampleRepo`。
- 建表：`metric_samples`。
- 启动时调用 `metrics.Init(repo)`。
- 启动后台 `metrics.Run(ctx)`。
- shutdown 时调用 `metrics.Flush()`。

## 边界与错误处理

- MySQL 写入失败：打印日志，保留 `pending` 样本，下次 flush 重试；若 buffer 满则丢弃最老样本。
- Redis 不可用：完全降级到 MySQL，不影响业务。
- 启动加载失败：打印日志，以空指标启动。
- 后台 flush goroutine 在 `ctx.Done()` 时退出。

## 测试

- `internal/repository/metric_sample_test.go`：验证 Save/LoadRecent。
- `internal/metrics/metrics_test.go`：验证持久化、加载、flush、有界化。
- `internal/app/app_test.go`：验证启动时 Init/Run/Flush 被调用。

## 文件变更

- 新增：`internal/repository/metric_sample.go`、`internal/repository/metric_sample_test.go`
- 修改：`internal/metrics/metrics.go`、`internal/metrics/metrics_test.go`、`internal/app/app.go`
- 文档：`README.md` 说明 metrics 持久化（可选）
