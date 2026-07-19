package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"miaodi-agent/internal/model"
)

// MessageProcessor 处理回调消息的接口
type MessageProcessor interface {
	ProcessMessage(ctx context.Context, payload *model.CallbackPayload) string
}

// CallbackHandler 传送鸽回调处理器
type CallbackHandler struct {
	agent MessageProcessor
}

// NewCallbackHandler 创建处理器
func NewCallbackHandler(agent MessageProcessor) *CallbackHandler {
	return &CallbackHandler{agent: agent}
}

// RegisterRoutes 注册路由
func (h *CallbackHandler) RegisterRoutes(mux *http.ServeMux, callbackPath string) {
	mux.HandleFunc(callbackPath, h.handleCallback)
	mux.HandleFunc("/health", h.handleHealth)
}

func (h *CallbackHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload model.CallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, model.NewSuccessResponse("请求格式错误"))
		return
	}

	if payload.EventType != "user_message" {
		writeJSON(w, http.StatusOK, model.NewSuccessResponse(""))
		return
	}

	// 9 秒超时，保证在传送鸽 10 秒限制内返回
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	reply := h.agent.ProcessMessage(ctx, &payload)
	writeJSON(w, http.StatusOK, model.NewSuccessResponse(reply))
}

func (h *CallbackHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
