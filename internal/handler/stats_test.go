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

type failingStatsWriter struct {
	headers http.Header
	status  int
}

func (f *failingStatsWriter) Header() http.Header {
	if f.headers == nil {
		f.headers = make(http.Header)
	}
	return f.headers
}

func (f *failingStatsWriter) WriteHeader(status int) {
	f.status = status
}

func (f *failingStatsWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestHandleStatsPage_ExecuteError(t *testing.T) {
	h := NewStatsHandler(&fakeStatsProvider{data: &service.StatsData{TotalUsers: 10}})
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := &failingStatsWriter{}
	h.handleStatsPage(w, req)
	if w.headers.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("expected html content type, got %v", w.headers)
	}
}
