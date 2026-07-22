package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newMetricSampleRepoMock(t *testing.T) (*MetricSampleRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewMetricSampleRepo(db), mock
}

func TestMetricSampleRepo_EnsureTable(t *testing.T) {
	r, mock := newMetricSampleRepoMock(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS metric_samples").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.EnsureTable(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMetricSampleRepo_Save(t *testing.T) {
	r, mock := newMetricSampleRepoMock(t)
	samples := []MetricSample{
		{Name: "op1", DurationMs: 1.5, Success: true, CreatedAt: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)},
		{Name: "op2", DurationMs: 2.5, Success: false, CreatedAt: time.Date(2026, 7, 22, 10, 1, 0, 0, time.UTC)},
	}
	mock.ExpectExec("INSERT INTO metric_samples").WithArgs(
		"op1", 1.5, 1, samples[0].CreatedAt,
		"op2", 2.5, 0, samples[1].CreatedAt,
	).WillReturnResult(sqlmock.NewResult(2, 2))
	if err := r.Save(samples); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMetricSampleRepo_Save_Empty(t *testing.T) {
	r, _ := newMetricSampleRepoMock(t)
	if err := r.Save(nil); err != nil {
		t.Errorf("unexpected error for nil slice: %v", err)
	}
	if err := r.Save([]MetricSample{}); err != nil {
		t.Errorf("unexpected error for empty slice: %v", err)
	}
}

func TestMetricSampleRepo_LoadRecent(t *testing.T) {
	r, mock := newMetricSampleRepoMock(t)
	rows := sqlmock.NewRows([]string{"name", "duration_ms", "success", "created_at"}).
		AddRow("op2", 2.5, 0, time.Date(2026, 7, 22, 10, 1, 0, 0, time.UTC)).
		AddRow("op1", 1.5, 1, time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC))
	mock.ExpectQuery("SELECT name, duration_ms, success, created_at FROM metric_samples").WithArgs(10).WillReturnRows(rows)

	samples, err := r.LoadRecent(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(samples))
	}
	if samples[0].Name != "op2" || samples[0].DurationMs != 2.5 || samples[0].Success {
		t.Errorf("unexpected first sample: %+v", samples[0])
	}
	if samples[1].Name != "op1" || samples[1].DurationMs != 1.5 || !samples[1].Success {
		t.Errorf("unexpected second sample: %+v", samples[1])
	}
}

func TestMetricSampleRepo_LoadRecent_Empty(t *testing.T) {
	r, mock := newMetricSampleRepoMock(t)
	mock.ExpectQuery("SELECT name, duration_ms, success, created_at FROM metric_samples").WithArgs(5).WillReturnRows(
		sqlmock.NewRows([]string{"name", "duration_ms", "success", "created_at"}))

	samples, err := r.LoadRecent(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if samples == nil || len(samples) != 0 {
		t.Fatalf("expected empty slice, got %+v", samples)
	}
}

func TestMetricSampleRepo_LoadRecent_QueryError(t *testing.T) {
	r, mock := newMetricSampleRepoMock(t)
	mock.ExpectQuery("SELECT name, duration_ms, success, created_at FROM metric_samples").WithArgs(5).WillReturnError(sqlmock.ErrCancelled)
	_, err := r.LoadRecent(5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMetricSampleRepo_LoadRecent_ScanError(t *testing.T) {
	r, mock := newMetricSampleRepoMock(t)
	rows := sqlmock.NewRows([]string{"name", "duration_ms", "success", "created_at"}).AddRow("op1", "not-float", 1, time.Now())
	mock.ExpectQuery("SELECT name, duration_ms, success, created_at FROM metric_samples").WithArgs(5).WillReturnRows(rows)
	_, err := r.LoadRecent(5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Errorf("expected 1 for true")
	}
	if boolToInt(false) != 0 {
		t.Errorf("expected 0 for false")
	}
}
