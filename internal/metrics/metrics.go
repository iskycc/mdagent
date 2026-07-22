// Package metrics 提供一个自研的、无外部 HTTP 依赖的接口性能指标采集器。
// 用于记录关键路径的调用次数、成功率和耗时分布（p50/p90/p95/p99/avg）。
package metrics

import (
	"sort"
	"sync"
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
	m.duration = append(m.duration, float64(d.Nanoseconds())/1e6)
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
}

// NewRecorder 创建一个新的 Recorder。
func NewRecorder() *Recorder {
	return &Recorder{metrics: make(map[string]*metric)}
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
func (r *Recorder) Record(name string, d time.Duration, success bool) {
	r.get(name).record(d, success)
}

// Start 开始一个计时 Span，返回后调用者需调用 Finish。
func (r *Recorder) Start(name string) *Span {
	return &Span{recorder: r, name: name, start: time.Now()}
}

// Snapshot 返回当前所有指标快照。
func (r *Recorder) Snapshot() []MetricSnapshot {
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
