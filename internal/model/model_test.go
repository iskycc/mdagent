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

func TestCallbackPayload_CreateTimeString(t *testing.T) {
	var payload CallbackPayload
	err := json.Unmarshal([]byte(`{
		"eventType":"user_message",
		"message":{"id":1,"content":"你好","createTime":"2026-06-30 10:00:00"}
	}`), &payload)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload.Message.CreateTime.String() != "2026-06-30 10:00:00" {
		t.Fatalf("unexpected createTime: %s", payload.Message.CreateTime)
	}
}

func TestCallbackPayload_CreateTimeNumber(t *testing.T) {
	var payload CallbackPayload
	err := json.Unmarshal([]byte(`{
		"eventType":"user_message",
		"message":{"id":3045,"content":"你好","createTime":1784474869617}
	}`), &payload)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload.Message.CreateTime.String() != "1784474869617" {
		t.Fatalf("unexpected createTime: %s", payload.Message.CreateTime)
	}
	if _, ok := payload.Message.CreateTime.UnixMilli(); !ok {
		t.Fatal("expected numeric createTime to parse as unix milliseconds")
	}
}

func TestCallbackPayload_CreateTimeNull(t *testing.T) {
	var payload CallbackPayload
	err := json.Unmarshal([]byte(`{"message":{"createTime":null}}`), &payload)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload.Message.CreateTime != "" {
		t.Fatalf("expected empty createTime, got %s", payload.Message.CreateTime)
	}
}

func TestCallbackPayload_CreateTimeInvalid(t *testing.T) {
	var payload CallbackPayload
	err := json.Unmarshal([]byte(`{"message":{"createTime":{}}}`), &payload)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestJSONString_MarshalJSON(t *testing.T) {
	s := JSONString("hello")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if string(b) != `"hello"` {
		t.Fatalf("unexpected marshal: %s", b)
	}
}

func TestJSONString_UnixMilli_Invalid(t *testing.T) {
	s := JSONString("not-a-number")
	if _, ok := s.UnixMilli(); ok {
		t.Fatal("expected false for non-numeric string")
	}
}
