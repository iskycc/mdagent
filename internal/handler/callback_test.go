package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"miaodi-agent/internal/model"
)

type fakeAgent struct {
	reply string
}

func (f *fakeAgent) ProcessMessage(ctx context.Context, payload *model.CallbackPayload) string {
	return f.reply
}

func TestHandleCallback_UserMessage(t *testing.T) {
	t.Setenv("APP_DEBUG", "true")
	agent := &fakeAgent{reply: "收到，已保存"}
	h := NewCallbackHandler(agent, "", false)
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
	h := NewCallbackHandler(agent, "", false)
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
	h := NewCallbackHandler(agent, "", false)
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
	h := NewCallbackHandler(agent, "", false)
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
	h := NewCallbackHandler(nil, "", false)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("unexpected health response: %d %s", rec.Code, rec.Body.String())
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestHandleCallback_ReadBodyError(t *testing.T) {
	agent := &fakeAgent{reply: "should not use"}
	h := NewCallbackHandler(agent, "", false)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	req := httptest.NewRequest(http.MethodPost, "/callback", errReader{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

type failingResponseWriter struct {
	headers http.Header
	status  int
}

func (f *failingResponseWriter) Header() http.Header {
	if f.headers == nil {
		f.headers = make(http.Header)
	}
	return f.headers
}

func (f *failingResponseWriter) WriteHeader(status int) {
	f.status = status
}

func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteJSON_Error(t *testing.T) {
	resp := model.NewSuccessResponse("hello")
	w := &failingResponseWriter{}
	writeJSON(w, http.StatusOK, resp)
	if w.status != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.status)
	}
}

func TestMustJSON_Error(t *testing.T) {
	result := mustJSON(make(chan int))
	if !strings.Contains(result, "marshal failed") {
		t.Fatalf("expected marshal failed message, got %s", result)
	}
}

func signedCallbackRequest(t *testing.T, secret, body string) *http.Request {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signCallbackBody(secret, ts, []byte(body))
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	req.Header.Set(callbackTimestampHeader, ts)
	req.Header.Set(callbackSignatureHeader, sig)
	return req
}

func TestHandleCallback_SignatureRequired(t *testing.T) {
	agent := &fakeAgent{reply: "收到"}
	h := NewCallbackHandler(agent, "my-secret", true)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{"eventType":"user_message","bot":{"id":1,"name":"b"},"conversation":{"id":1},"user":{"userId":"u1","username":"*"},"message":{"id":1,"content":"hi","createTime":"1"}}`
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleCallback_SignatureMismatch(t *testing.T) {
	agent := &fakeAgent{reply: "收到"}
	h := NewCallbackHandler(agent, "my-secret", true)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{"eventType":"user_message","bot":{"id":1,"name":"b"},"conversation":{"id":1},"user":{"userId":"u1","username":"*"},"message":{"id":1,"content":"hi","createTime":"1"}}`
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	req.Header.Set(callbackTimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set(callbackSignatureHeader, "bad-signature")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleCallback_SignatureExpired(t *testing.T) {
	agent := &fakeAgent{reply: "收到"}
	h := NewCallbackHandler(agent, "my-secret", true)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{"eventType":"user_message","bot":{"id":1,"name":"b"},"conversation":{"id":1},"user":{"userId":"u1","username":"*"},"message":{"id":1,"content":"hi","createTime":"1"}}`
	ts := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	sig := signCallbackBody("my-secret", ts, []byte(body))
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	req.Header.Set(callbackTimestampHeader, ts)
	req.Header.Set(callbackSignatureHeader, sig)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestHandleCallback_SignatureSuccess(t *testing.T) {
	agent := &fakeAgent{reply: "收到"}
	h := NewCallbackHandler(agent, "my-secret", true)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{"eventType":"user_message","bot":{"id":1,"name":"b"},"conversation":{"id":1},"user":{"userId":"u1","username":"*"},"message":{"id":1,"content":"hi","createTime":"1"}}`
	req := signedCallbackRequest(t, "my-secret", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp model.CallbackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if !resp.Success || resp.Reply.Content != "收到" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleCallback_SignatureBodyTampered(t *testing.T) {
	agent := &fakeAgent{reply: "收到"}
	h := NewCallbackHandler(agent, "my-secret", true)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{"eventType":"user_message","bot":{"id":1,"name":"b"},"conversation":{"id":1},"user":{"userId":"u1","username":"*"},"message":{"id":1,"content":"hi","createTime":"1"}}`
	req := signedCallbackRequest(t, "my-secret", body)
	// 篡改请求体但保留原签名
	req.Body = io.NopCloser(strings.NewReader(`{"eventType":"user_message"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestValidateCallbackSignature_SkipWhenNoSecret(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader("body"))
	if err := validateCallbackSignature("", req, []byte("body")); err != nil {
		t.Fatalf("expected no error without secret, got %v", err)
	}
}

func TestValidateCallbackSignature_MissingHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader("body"))
	if err := validateCallbackSignature("secret", req, []byte("body")); err == nil {
		t.Fatal("expected error for missing headers")
	}
}

func TestValidateCallbackSignature_InvalidTimestamp(t *testing.T) {
	body := []byte("body")
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(string(body)))
	req.Header.Set(callbackTimestampHeader, "not-a-number")
	req.Header.Set(callbackSignatureHeader, "sig")
	if err := validateCallbackSignature("secret", req, body); err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
}

func TestValidateCallbackSignature_ExpiredTimestamp(t *testing.T) {
	body := []byte("body")
	ts := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	sig := signCallbackBody("secret", ts, body)
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(string(body)))
	req.Header.Set(callbackTimestampHeader, ts)
	req.Header.Set(callbackSignatureHeader, sig)
	if err := validateCallbackSignature("secret", req, body); err == nil {
		t.Fatal("expected error for expired timestamp")
	}
}

func TestValidateCallbackSignature_Success(t *testing.T) {
	body := []byte("body")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signCallbackBody("secret", ts, body)
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(string(body)))
	req.Header.Set(callbackTimestampHeader, ts)
	req.Header.Set(callbackSignatureHeader, sig)
	if err := validateCallbackSignature("secret", req, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleCallback_AuthDisabledWithSecret(t *testing.T) {
	agent := &fakeAgent{reply: "收到"}
	h := NewCallbackHandler(agent, "my-secret", false)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/callback")

	body := `{"eventType":"user_message","bot":{"id":1,"name":"b"},"conversation":{"id":1},"user":{"userId":"u1","username":"*"},"message":{"id":1,"content":"hi","createTime":"1"}}`
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when auth disabled, got %d", rec.Code)
	}
}

func TestValidateCallbackSignature_FutureTimestamp(t *testing.T) {
	body := []byte("body")
	ts := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	sig := signCallbackBody("secret", ts, body)
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(string(body)))
	req.Header.Set(callbackTimestampHeader, ts)
	req.Header.Set(callbackSignatureHeader, sig)
	if err := validateCallbackSignature("secret", req, body); err == nil {
		t.Fatal("expected error for future timestamp")
	}
}
