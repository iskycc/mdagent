package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"miaodi-agent/internal/debuglog"
	"miaodi-agent/internal/metrics"
	"miaodi-agent/internal/model"
)

// MessageProcessor 处理回调消息的接口
type MessageProcessor interface {
	ProcessMessage(ctx context.Context, payload *model.CallbackPayload) string
}

// CallbackHandler 传送鸽回调处理器
type CallbackHandler struct {
	agent          MessageProcessor
	callbackSecret string
}

// NewCallbackHandler 创建处理器
func NewCallbackHandler(agent MessageProcessor, callbackSecret string) *CallbackHandler {
	return &CallbackHandler{agent: agent, callbackSecret: callbackSecret}
}

// RegisterRoutes 注册路由
func (h *CallbackHandler) RegisterRoutes(mux *http.ServeMux, callbackPath string) {
	mux.HandleFunc(callbackPath, h.handleCallback)
	mux.HandleFunc("/health", h.handleHealth)
}

func (h *CallbackHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	debuglog.Printf("callback request method=%s path=%s remote=%s", r.Method, r.URL.Path, r.RemoteAddr)

	if r.Method != http.MethodPost {
		debuglog.Printf("callback rejected status=%d reason=method_not_allowed elapsed=%s", http.StatusMethodNotAllowed, time.Since(start))
		metrics.Record("callback_handler", time.Since(start), false)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("read callback body failed: %v", err)
		resp := model.NewSuccessResponse("请求读取失败")
		debuglog.Printf("callback response status=%d body=%s elapsed=%s", http.StatusBadRequest, mustJSON(resp), time.Since(start))
		metrics.Record("callback_handler", time.Since(start), false)
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}
	debuglog.Printf("callback request body=%s", string(body))

	if err := validateCallbackSignature(h.callbackSecret, r, body); err != nil {
		debuglog.Printf("callback signature invalid elapsed=%s error=%v", time.Since(start), err)
		metrics.Record("callback_handler", time.Since(start), false)
		writeJSON(w, http.StatusUnauthorized, model.NewErrorResponse("请求签名验证失败"))
		return
	}

	var payload model.CallbackPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("decode callback body failed: %v", err)
		resp := model.NewSuccessResponse("请求格式错误")
		debuglog.Printf("callback response status=%d body=%s elapsed=%s", http.StatusBadRequest, mustJSON(resp), time.Since(start))
		metrics.Record("callback_handler", time.Since(start), false)
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}

	if payload.EventType != "user_message" {
		resp := model.NewSuccessResponse("")
		debuglog.Printf("callback ignored event_type=%s response=%s elapsed=%s", payload.EventType, mustJSON(resp), time.Since(start))
		metrics.Record("callback_handler", time.Since(start), true)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// 9 秒超时，保证在传送鸽 10 秒限制内返回
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	reply := h.agent.ProcessMessage(ctx, &payload)
	resp := model.NewSuccessResponse(reply)
	debuglog.Printf("callback response status=%d body=%s elapsed=%s", http.StatusOK, mustJSON(resp), time.Since(start))
	metrics.Record("callback_handler", time.Since(start), true)
	writeJSON(w, http.StatusOK, resp)
}

func (h *CallbackHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json response failed: %v", err)
	}
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<marshal failed: " + err.Error() + ">"
	}
	return string(b)
}
