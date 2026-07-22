package metrics

import (
	"testing"
	"time"
)

func TestRecorder_RecordAndSnapshot(t *testing.T) {
	r := NewRecorder()
	r.Record("test", 100*time.Millisecond, true)
	r.Record("test", 200*time.Millisecond, true)
	r.Record("test", 300*time.Millisecond, false)

	snapshots := r.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	s := snapshots[0]
	if s.Name != "test" {
		t.Errorf("expected name test, got %s", s.Name)
	}
	if s.Count != 3 {
		t.Errorf("expected count 3, got %d", s.Count)
	}
	if s.Success != 2 {
		t.Errorf("expected success 2, got %d", s.Success)
	}
	if s.Errors != 1 {
		t.Errorf("expected errors 1, got %d", s.Errors)
	}
	if s.SuccessRate != 2.0/3.0 {
		t.Errorf("expected success rate %v, got %v", 2.0/3.0, s.SuccessRate)
	}
	if s.P50Ms < 100 || s.P50Ms > 200 {
		t.Errorf("unexpected p50: %v", s.P50Ms)
	}
	if s.P99Ms < 200 || s.P99Ms > 300 {
		t.Errorf("unexpected p99: %v", s.P99Ms)
	}
}

func TestSpan_Finish(t *testing.T) {
	r := NewRecorder()
	span := r.Start("span-test")
	time.Sleep(5 * time.Millisecond)
	span.Finish(true)

	snapshots := r.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Count != 1 {
		t.Errorf("expected count 1, got %d", snapshots[0].Count)
	}
	if snapshots[0].AvgMs <= 0 {
		t.Errorf("expected positive avg, got %v", snapshots[0].AvgMs)
	}
}

func TestRecorder_MaxSamples(t *testing.T) {
	r := NewRecorder()
	for i := 0; i < maxSamples+100; i++ {
		r.Record("test", time.Duration(i)*time.Millisecond, true)
	}
	snapshots := r.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Count != int64(maxSamples+100) {
		t.Errorf("expected count %d, got %d", maxSamples+100, snapshots[0].Count)
	}
	// 只保留最近 maxSamples 条；最早 100 条被丢弃，因此最小值应为 100
	if snapshots[0].P50Ms < 100 {
		t.Errorf("expected min >= 100 after eviction, got p50=%v", snapshots[0].P50Ms)
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{10, 20, 30, 40, 50}
	if percentile(sorted, 0.50) != 30 {
		t.Errorf("expected p50 30, got %v", percentile(sorted, 0.50))
	}
	if got := percentile(sorted, 0.90); got != 46 {
		t.Errorf("expected p90 46, got %v", got)
	}
}
