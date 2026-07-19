package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"miaodi-agent/internal/model"
	"miaodi-agent/pkg/openai"
)

type fakeUserStore struct {
	user *model.User
	err  error
}

func (f *fakeUserStore) GetOrCreate(channelUserID string) (*model.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

type fakeConversationStore struct {
	messages    []openai.ChatMessage
	err         error
	errOnAppend bool
}

func (f *fakeConversationStore) GetMessages(channelUserID string, conversationID int64) ([]openai.ChatMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.messages, nil
}

func (f *fakeConversationStore) AppendMessage(channelUserID string, conversationID int64, msg openai.ChatMessage) error {
	if f.errOnAppend {
		return errors.New("append error")
	}
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeConversationStore) AppendMessages(channelUserID string, conversationID int64, msgs ...openai.ChatMessage) error {
	if f.errOnAppend {
		return errors.New("append error")
	}
	f.messages = append(f.messages, msgs...)
	return nil
}

func (f *fakeConversationStore) Clear(channelUserID string, conversationID int64) error {
	f.messages = nil
	return nil
}

type fakeLLM struct {
	responses []*openai.ChatCompletionResponse
	callCount int
	err       error
	lastReq   openai.ChatCompletionRequest
}

func (f *fakeLLM) CreateChatCompletion(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	resp := f.responses[f.callCount]
	f.callCount++
	return resp, nil
}

type fakeToolRunner struct {
	result       string
	executedName string
	executedArgs string
}

func (f *fakeToolRunner) Execute(user *model.User, channelUserID string, conversationID int64, name, arguments string) string {
	f.executedName = name
	f.executedArgs = arguments
	return f.result
}

func newTestPayload() *model.CallbackPayload {
	return &model.CallbackPayload{
		EventType: "user_message",
		Bot:       model.Bot{ID: 1, Name: "喵滴助手"},
		Conversation: struct {
			ID int64 `json:"id"`
		}{ID: 100},
		User: struct {
			UserID   string `json:"userId"`
			Username string `json:"username"`
		}{UserID: "u1", Username: "*"},
		Message: model.CallbackMessage{ID: 1, Content: "hello", CreateTime: "2026-06-30 10:00:00"},
	}
}

func makeTextResponse(content string) *openai.ChatCompletionResponse {
	return &openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{Message: openai.ChatMessage{Role: "assistant", Content: content}, FinishReason: "stop"},
		},
	}
}

func makeToolResponse(name, args string) *openai.ChatCompletionResponse {
	return &openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatMessage{
					Role: "assistant",
					ToolCalls: []openai.ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: openai.ToolCallFunction{
							Name:      name,
							Arguments: args,
						},
					}},
				},
				FinishReason: "tool_calls",
			},
		},
	}
}

func TestAgent_ProcessMessage_FinalReply(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("你好")}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "你好" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_LocalIntentBypassesLLM(t *testing.T) {
	llm := &fakeLLM{}
	runner := &fakeToolRunner{result: "帮助内容"}
	payload := newTestPayload()
	payload.Message.Content = "帮助"
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)

	reply := agent.ProcessMessage(context.Background(), payload)

	if reply != "帮助内容" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 0 {
		t.Errorf("expected local intent to bypass llm, got %d calls", llm.callCount)
	}
	if runner.executedName != "show_help" {
		t.Errorf("expected show_help, got %s", runner.executedName)
	}
}

func TestAgent_ProcessMessage_UsesConfiguredTokenBudget(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	store := &fakeConversationStore{
		messages: []openai.ChatMessage{
			{Role: "user", Content: strings.Repeat("旧消息", 3000)},
		},
	}
	agent := NewAgentWithOptions(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, &fakeToolRunner{}, AgentOptions{
		ModelMaxTokens:  1200,
		MaxOutputTokens: 128,
	})

	reply := agent.ProcessMessage(context.Background(), newTestPayload())

	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.lastReq.MaxTokens != 128 {
		t.Errorf("expected max_tokens 128, got %d", llm.lastReq.MaxTokens)
	}
	if got := estimateBlockTokens(llm.lastReq.Messages); got >= 1200 {
		t.Errorf("expected trimmed prompt below model limit, got %d tokens", got)
	}
}

func TestAgent_ProcessMessage_ToolCallThenReply(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("get_user_profile", "{}"),
		makeTextResponse("done"),
	}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "ok"})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "done" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 2 {
		t.Errorf("expected 2 llm calls, got %d", llm.callCount)
	}
}

func TestAgent_ProcessMessage_EmptyChoices(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{{Choices: nil}}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "AI 没有返回任何内容" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_LLMError(t *testing.T) {
	llm := &fakeLLM{err: errors.New("timeout")}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "AI 调用失败：timeout" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_UserError(t *testing.T) {
	agent := NewAgent(
		&fakeLLM{}, "model",
		&fakeUserStore{err: errors.New("db error")},
		&fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "系统内部错误，请稍后再试" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_ContextTimeout(t *testing.T) {
	llm := &fakeLLM{}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reply := agent.ProcessMessage(ctx, newTestPayload())
	if reply != "处理超时，请缩短消息或稍后再试" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_TooManyToolRounds(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("get_user_profile", "{}"),
		makeToolResponse("get_user_profile", "{}"),
		makeToolResponse("get_user_profile", "{}"),
	}}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{result: "ok"})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "工具调用轮数超过限制，请简化请求" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_GetMessagesError(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	store := &fakeConversationStore{err: errors.New("db error")}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_AppendMessageError(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	store := &fakeConversationStore{errOnAppend: true}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestToToolDefinitions(t *testing.T) {
	defs := toToolDefinitions()
	if len(defs) == 0 {
		t.Error("expected tool definitions")
	}
}

func TestAgent_ProcessMessage_ResetTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("reset_conversation", "{}"),
	}}
	store := &fakeConversationStore{}
	runner := &fakeToolRunner{result: "已清空当前会话，我们可以重新开始。"}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, runner)
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "已清空当前会话，我们可以重新开始。" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 1 {
		t.Errorf("expected 1 llm call, got %d", llm.callCount)
	}
	if runner.executedName != "reset_conversation" {
		t.Errorf("expected tool reset_conversation, got %s", runner.executedName)
	}
}

func TestAgent_ProcessMessage_HelpTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("show_help", "{}"),
		makeTextResponse("我可以帮你..."),
	}}
	runner := &fakeToolRunner{result: "帮助内容"}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "我可以帮你..." {
		t.Errorf("unexpected reply: %s", reply)
	}
	if runner.executedName != "show_help" {
		t.Errorf("expected tool show_help, got %s", runner.executedName)
	}
	if runner.executedArgs != "{}" {
		t.Errorf("unexpected tool args: %s", runner.executedArgs)
	}
}

func TestAgent_ProcessMessage_ListRecentNotesTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("list_recent_notes", `{"limit":5}`),
		makeTextResponse("最近你保存了..."),
	}}
	runner := &fakeToolRunner{result: "最近记录"}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "最近你保存了..." {
		t.Errorf("unexpected reply: %s", reply)
	}
	if runner.executedName != "list_recent_notes" {
		t.Errorf("expected tool list_recent_notes, got %s", runner.executedName)
	}
	if runner.executedArgs != `{"limit":5}` {
		t.Errorf("unexpected tool args: %s", runner.executedArgs)
	}
}

func TestAgent_ProcessMessage_QueryNotesByDateTool(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("query_notes_by_date", `{"date":"2026-06-30"}`),
		makeTextResponse("那天的记录是..."),
	}}
	runner := &fakeToolRunner{result: "日期记录"}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "那天的记录是..." {
		t.Errorf("unexpected reply: %s", reply)
	}
	if runner.executedName != "query_notes_by_date" {
		t.Errorf("expected tool query_notes_by_date, got %s", runner.executedName)
	}
	if runner.executedArgs != `{"date":"2026-06-30"}` {
		t.Errorf("unexpected tool args: %s", runner.executedArgs)
	}
}

func TestAgent_ProcessMessage_ResetTool_DoesNotPersist(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("reset_conversation", "{}"),
	}}
	store := &fakeConversationStore{}
	runner := &fakeToolRunner{result: "已清空当前会话，我们可以重新开始。"}
	agent := NewAgent(llm, "model", &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, runner)
	agent.ProcessMessage(context.Background(), newTestPayload())

	// reset_conversation 是终止操作：返回结果后直接结束，本轮的 assistant 与 tool
	// 消息不应被持久化到会话历史中（ProcessMessage 在工具循环前追加的 user 消息除外）。
	for _, m := range store.messages {
		if m.Role == "assistant" || m.Role == "tool" {
			t.Errorf("reset tool round should not be persisted, found %s message: %+v", m.Role, m)
		}
	}
}
