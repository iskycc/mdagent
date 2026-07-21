package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChatCompletionRequest_MarshalThinking(t *testing.T) {
	req := ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Thinking: &Thinking{Type: "disabled"},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"thinking":{"type":"disabled"}`) {
		t.Errorf("expected thinking disabled in request body, got %s", string(b))
	}
}

func TestCreateChatCompletion_NormalReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		resp := ChatCompletionResponse{
			Choices: []ChatCompletionChoice{
				{Message: ChatMessage{Role: "assistant", Content: "你好"}, FinishReason: "stop"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	resp, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "你好" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateChatCompletion_ToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatCompletionResponse{
			Choices: []ChatCompletionChoice{
				{
					Message: ChatMessage{
						Role: "assistant",
						ToolCalls: []ToolCall{
							{
								ID:   "call_1",
								Type: "function",
								Function: ToolCallFunction{
									Name:      "save_text_note",
									Arguments: `{"content":"hello"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	resp, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "保存笔记"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected tool call, got: %+v", resp)
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "save_text_note" {
		t.Fatalf("unexpected tool name: %s", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	}
}

func TestCreateChatCompletion_NonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	_, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateChatCompletion_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	_, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateChatCompletion_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatCompletionResponse{Error: &struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		}{Message: "bad request"}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	_, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSetTimeout(t *testing.T) {
	client := NewClient("key", "")
	client.SetTimeout(5 * time.Second)
	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("unexpected timeout: %v", client.httpClient.Timeout)
	}
}

func TestSetDebug(t *testing.T) {
	client := NewClient("key", "")
	client.SetDebug(true)
	if !client.debug {
		t.Fatal("expected debug enabled")
	}
	client.SetDebug(false)
	if client.debug {
		t.Fatal("expected debug disabled")
	}
}

func TestNewClient_DebugFromEnv(t *testing.T) {
	os.Setenv("OPENAI_DEBUG", "true")
	defer os.Unsetenv("OPENAI_DEBUG")

	client := NewClient("key", "")
	if !client.debug {
		t.Fatal("expected debug enabled from env")
	}
}

func TestNewClient_DefaultBaseURL(t *testing.T) {
	client := NewClient("key", "")
	if client.baseURL != "https://api.openai.com/v1" {
		t.Errorf("unexpected base url: %s", client.baseURL)
	}
}

func TestCreateChatCompletion_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.CreateChatCompletion(ctx, ChatCompletionRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateChatCompletion_MarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	_, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Model: "m",
		Tools: []ToolDefinition{
			{Function: FunctionDef{Parameters: map[string]interface{}{"x": make(chan int)}}},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
