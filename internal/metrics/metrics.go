// Package metrics 提供一个自研的、无外部 HTTP 依赖的接口性能指标采集器。
// 用于记录关键路径的调用次数、成功率和耗时分布（p50/p90/p95/p99/avg）。
package metrics

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MetricSnapshot 是单个指标在某一时刻的聚合快照。
type MetricSnapshot struct {
	Name        string  `json:"name"`
	Count       int64   `json:"count"`
	Success     int64   `json:"success"`
	Errors      int64   `json:"errors"`
	SuccessRate float64 `json:"success_rate"`
	AvgMs       float64 `json:"avg_ms"`
	P50Ms       float64 `json:"p50_ms"`
	P90Ms       float64 `json:"p90_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
}

const maxSamples = 10000

// Sample 是一条待持久化的指标采样记录。
type Sample struct {
	Name       string
	DurationMs float64
	Success    bool
	CreatedAt  time.Time
}

// Store 定义指标采样的持久化接口。
type Store interface {
	Save(samples []Sample) error
	LoadRecent(limit int) ([]Sample, error)
}

// SnapshotCache 定义指标快照的缓存接口。
// 实现通常由 Redis 等外部缓存提供，本包不依赖具体实现。
type SnapshotCache interface {
	SetMetricsSnapshot(ctx context.Context, snapshots []MetricSnapshot) error
	GetMetricsSnapshot(ctx context.Context) ([]MetricSnapshot, error)
}

// metric 是内部聚合结构。
type metric struct {
	mu       sync.Mutex
	count    int64
	success  int64
	duration []float64 // 毫秒
}

func (m *metric) record(d time.Duration, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count++
	if success {
		m.success++
	}
	ms := float64(d.Nanoseconds()) / 1e6
	if len(m.duration) >= maxSamples {
		// 保留最近 maxSamples 条，丢弃最老的样本
		m.duration = append(m.duration[1:], ms)
	} else {
		m.duration = append(m.duration, ms)
	}
}

func (m *metric) snapshot(name string) MetricSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := MetricSnapshot{Name: name, Count: m.count, Success: m.success}
	snap.Errors = m.count - m.success
	if m.count > 0 {
		snap.SuccessRate = float64(m.success) / float64(m.count)
	}

	if len(m.duration) > 0 {
		durations := make([]float64, len(m.duration))
		copy(durations, m.duration)
		sort.Float64s(durations)
		snap.P50Ms = percentile(durations, 0.50)
		snap.P90Ms = percentile(durations, 0.90)
		snap.P95Ms = percentile(durations, 0.95)
		snap.P99Ms = percentile(durations, 0.99)
		snap.AvgMs = avg(durations)
	}
	return snap
}

// Recorder 持有所有指标。
type Recorder struct {
	mu      sync.RWMutex
	metrics map[string]*metric

	store         Store
	snapshotCache atomic.Value
	pendingMu     sync.Mutex
	pending       []Sample
	flushInterval time.Duration // 仅在构造函数中设置，构造后不可变
	maxPending    int           // 仅在构造函数中设置，构造后不可变
	stopCh        chan struct{}
	stopOnce      sync.Once
	flushMu       sync.Mutex
	running       atomic.Bool
}

// NewRecorder 创建一个新的 Recorder。
func NewRecorder() *Recorder {
	return &Recorder{
		metrics:       make(map[string]*metric),
		flushInterval: 30 * time.Second,
		maxPending:    1000,
		stopCh:        make(chan struct{}),
	}
}

// NewRecorderWithStore 创建一个有持久化能力的 Recorder。
func NewRecorderWithStore(store Store) *Recorder {
	r := NewRecorder()
	r.store = store
	return r
}

func (r *Recorder) get(name string) *metric {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.metrics[name]
	if !ok {
		m = &metric{}
		r.metrics[name] = m
	}
	return m
}

// Record 记录一次调用耗时与结果。
// 先更新内存指标，再将样本追加到 pending 队列；即使当前没有 Store，样本也会暂存在
// pending 中等待后续 Flush。当 pending 达到 maxPending 时，会同步触发 Flush；
// Flush 失败时错误会被静默忽略（本包不依赖外部 logger），调用方如关注持久化结果
// 可主动调用 Flush()。
func (r *Recorder) Record(name string, d time.Duration, success bool) {
	r.get(name).record(d, success)

	r.pendingMu.Lock()
	r.pending = append(r.pending, Sample{
		Name:       name,
		DurationMs: float64(d.Nanoseconds()) / 1e6,
		Success:    success,
		CreatedAt:  time.Now(),
	})
	shouldFlush := len(r.pending) >= r.maxPending
	r.pendingMu.Unlock()

	if shouldFlush {
		// Synchronous flush; errors are silently ignored.
		_ = r.Flush()
	}
}

// Init 从 Store 加载最近的采样并恢复到内存统计中。
func (r *Recorder) Init() error {
	if r.store == nil {
		return nil
	}
	samples, err := r.store.LoadRecent(maxSamples)
	if err != nil {
		return fmt.Errorf("load metric samples failed: %w", err)
	}
	// LoadRecent 通常按时间降序返回（最新的在前）。需按时间升序回放，
	// 使最老的样本先被记录，保证内存中 duration 列表按时间先后排列。
	for i := len(samples) - 1; i >= 0; i-- {
		s := samples[i]
		r.get(s.Name).record(time.Duration(s.DurationMs*1e6), s.Success)
	}
	return nil
}

// Run 启动后台 goroutine，按 flushInterval 周期刷新 pending 样本到 Store。
// 多次调用 Run() 只会启动一个刷新 goroutine；当 store 为 nil 时不执行任何操作。
func (r *Recorder) Run(ctx context.Context) {
	if r.store == nil {
		return
	}
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.running.Store(false)
		ticker := time.NewTicker(r.flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				r.Flush()
				return
			case <-r.stopCh:
				r.Flush()
				return
			case <-ticker.C:
				r.Flush()
			}
		}
	}()
}

// FlushContext 与 Flush 相同，但可以通过 ctx 取消等待。
// 如果 ctx 在 flush 完成前取消，返回 ctx.Err()；实际的 Store.Save 仍在后台运行。
func (r *Recorder) FlushContext(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- r.Flush() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// Flush 将 pending 中的样本持久化到 Store；失败时将样本重新放回 pending。
func (r *Recorder) Flush() error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	if r.store == nil {
		return nil
	}

	r.pendingMu.Lock()
	if len(r.pending) == 0 {
		r.pendingMu.Unlock()
		return nil
	}
	toSave := make([]Sample, len(r.pending))
	copy(toSave, r.pending)
	r.pending = r.pending[:0]
	r.pendingMu.Unlock()

	if err := r.store.Save(toSave); err != nil {
		r.pendingMu.Lock()
		// 失败样本放回队列头部；若超过两倍 maxPending，则丢弃最老的样本，
		// 保留最新的样本。
		r.pending = append(toSave, r.pending...)
		if len(r.pending) > r.maxPending*2 {
			r.pending = r.pending[len(r.pending)-r.maxPending*2:]
		}
		r.pendingMu.Unlock()
		return err
	}
	return nil
}

// Stop 通知后台刷新 goroutine 退出；重复调用不会 panic。
func (r *Recorder) Stop() {
	r.stopOnce.Do(func() {
		if r.stopCh != nil {
			close(r.stopCh)
		}
	})
}

// Start 开始一个计时 Span，返回后调用者需调用 Finish。
func (r *Recorder) Start(name string) *Span {
	return &Span{recorder: r, name: name, start: time.Now()}
}

// SetSnapshotCache 设置用于缓存指标快照的 Cache。
func (r *Recorder) SetSnapshotCache(c SnapshotCache) {
	if c == nil {
		return
	}
	r.snapshotCache.Store(c)
}

// loadSnapshotCache 从 atomic.Value 中读取当前 SnapshotCache。
func (r *Recorder) loadSnapshotCache() SnapshotCache {
	v := r.snapshotCache.Load()
	if v == nil {
		return nil
	}
	return v.(SnapshotCache)
}

// Snapshot 返回当前所有指标快照。
// 如果配置了 SnapshotCache 且缓存命中（且非空），则直接返回缓存数据；
// 否则计算内存指标并尝试将结果写回缓存（错误被忽略，由调用方降级）。
// 空缓存被视为未命中，防止指标产生后仍返回过期的空快照。
func (r *Recorder) Snapshot() []MetricSnapshot {
	if c := r.loadSnapshotCache(); c != nil {
		cached, err := c.GetMetricsSnapshot(context.Background())
		if err == nil && len(cached) > 0 {
			return cached
		}
	}

	r.mu.RLock()
	names := make([]string, 0, len(r.metrics))
	for name := range r.metrics {
		names = append(names, name)
	}
	r.mu.RUnlock()

	result := make([]MetricSnapshot, 0, len(names))
	for _, name := range names {
		result = append(result, r.get(name).snapshot(name))
	}

	if c := r.loadSnapshotCache(); c != nil {
		_ = c.SetMetricsSnapshot(context.Background(), result)
	}
	return result
}

// Span 用于便捷计时。
type Span struct {
	recorder *Recorder
	name     string
	start    time.Time
}

// Finish 结束计时并记录结果。
func (s *Span) Finish(success bool) {
	if s == nil || s.recorder == nil {
		return
	}
	s.recorder.Record(s.name, time.Since(s.start), success)
}

// global 是进程级默认 Recorder。
var global = NewRecorder()

// Record 向默认 Recorder 记录一次调用。
func Record(name string, d time.Duration, success bool) {
	global.Record(name, d, success)
}

// Start 向默认 Recorder 开始一个计时 Span。
func Start(name string) *Span {
	return global.Start(name)
}

// Snapshot 返回默认 Recorder 的当前快照。
func Snapshot() []MetricSnapshot {
	return global.Snapshot()
}

// SetSnapshotCache 为默认 Recorder 设置快照缓存。
func SetSnapshotCache(c SnapshotCache) {
	global.SetSnapshotCache(c)
}

// Init 使用指定的 Store 初始化默认 Recorder 并恢复历史样本。
func Init(store Store) error {
	global.store = store
	return global.Init()
}

// Run 启动默认 Recorder 的后台刷新循环。
func Run(ctx context.Context) {
	global.Run(ctx)
}

// Flush 立即将默认 Recorder 的 pending 样本刷新到 Store。
func Flush() error {
	return global.Flush()
}

// FlushContext 立即刷新默认 Recorder，支持通过 ctx 取消等待。
func FlushContext(ctx context.Context) error {
	return global.FlushContext(ctx)
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	k := float64(len(sorted)-1) * p
	f := int(k)
	c := f + 1
	if c >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	return sorted[f] + (k-float64(f))*(sorted[c]-sorted[f])
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
