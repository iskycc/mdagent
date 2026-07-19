package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"miaodi-agent/internal/model"
)

type fakeAgent struct {
	reply string
}

func (f *fakeAgent) ProcessMessage(ctx context.Context, payload *model.CallbackPayload) string {
	return f.reply
}

func TestHandleCallback_UserMessage(t *testing.T) {
	agent := &fakeAgent{reply: "收到，已保存"}
	h := NewCallbackHandler(agent)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{
		"eventType": "user_message",
		"bot": {"id": 1, "name": "喵滴助手"},
		"conversation": {"id": 100},
		"user": {"userId": "u123", "username": "*"},
		"message": {"id": 1, "content": "保存一段笔记", "createTime": "2026-06-30 10:00:00"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var resp model.CallbackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if !resp.Success || resp.Reply.Content != "收到，已保存" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleCallback_UnknownEvent(t *testing.T) {
	agent := &fakeAgent{reply: "should not use"}
	h := NewCallbackHandler(agent)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{"eventType": "subscribe"}`
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var resp model.CallbackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success for unknown event")
	}
}

func TestHandleCallback_InvalidJSON(t *testing.T) {
	agent := &fakeAgent{reply: "should not use"}
	h := NewCallbackHandler(agent)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCallback_MethodNotAllowed(t *testing.T) {
	agent := &fakeAgent{}
	h := NewCallbackHandler(agent)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	h := NewCallbackHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("unexpected health response: %d %s", rec.Code, rec.Body.String())
	}
}
