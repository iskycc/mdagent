package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"miaodi-agent/internal/service"
)

type fakeStatsProvider struct {
	data      *service.StatsData
	err       error
	toJSONErr error
}

func (f *fakeStatsProvider) GetStats() (*service.StatsData, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.data, nil
}

func (f *fakeStatsProvider) ToJSON(data *service.StatsData) (string, error) {
	if f.toJSONErr != nil {
		return "", f.toJSONErr
	}
	return `{"total_users":10}`, nil
}

func TestHandleStatsPage(t *testing.T) {
	h := NewStatsHandler(&fakeStatsProvider{data: &service.StatsData{TotalUsers: 10}})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "喵滴 Agent 统计看板") {
		t.Fatalf("page title not found in body")
	}
	if !strings.Contains(body, "total_users") {
		t.Fatalf("stats data not injected")
	}
}

func TestHandleStatsPage_GetStatsError(t *testing.T) {
	h := NewStatsHandler(&fakeStatsProvider{err: errors.New("db error")})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleStatsPage_ToJSONError(t *testing.T) {
	h := NewStatsHandler(&fakeStatsProvider{data: &service.StatsData{TotalUsers: 10}, toJSONErr: errors.New("json error")})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleStatsAPI(t *testing.T) {
	h := NewStatsHandler(&fakeStatsProvider{data: &service.StatsData{TotalUsers: 10}})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"total_users":10`) {
		t.Fatalf("unexpected api response: %s", rec.Body.String())
	}
}

func TestHandleStatsAPI_Error(t *testing.T) {
	h := NewStatsHandler(
		&fakeStatsProvider{err: errors.New("db error")})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}
