package model

import (
	"encoding/json"
	"testing"
)

func TestNewSuccessResponse(t *testing.T) {
	resp := NewSuccessResponse("hello")
	if !resp.Success {
		t.Error("expected success true")
	}
	if resp.Reply.Content != "hello" {
		t.Errorf("unexpected content: %s", resp.Reply.Content)
	}
}

func TestCallbackResponseJSON(t *testing.T) {
	resp := NewSuccessResponse("test")
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if string(b) == "" {
		t.Error("empty json")
	}
}
