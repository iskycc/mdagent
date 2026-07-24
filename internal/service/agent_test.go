package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"miaodi-agent/internal/cache"
	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
	"miaodi-agent/internal/timeutil"
	"miaodi-agent/pkg/openai"
)

type nopPersistQueue struct{}

func (nopPersistQueue) EnqueueConv(context.Context, string, int64, []repository.StoredChatMessage) bool {
	return true
}
func (nopPersistQueue) EnqueueLog(context.Context, string, string, string, string) bool { return true }
func (nopPersistQueue) Run(context.Context)                                             {}
func (nopPersistQueue) Flush(context.Context) error                                     { return nil }

func newTestAgent(llm LLMClient, userStore UserStore, convStore ConversationStore, toolRunner ToolRunner) *Agent {
	return NewAgent(llm, "model", userStore, convStore, toolRunner, cache.NopCache{}, nopPersistQueue{})
}

func newTestAgentWithOptions(llm LLMClient, userStore UserStore, convStore ConversationStore, toolRunner ToolRunner, opts AgentOptions) *Agent {
	return NewAgentWithOptions(llm, "model", userStore, convStore, toolRunner, opts, cache.NopCache{}, nopPersistQueue{})
}

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

type fakeLLMLogger struct {
	records []struct {
		channelUserID    string
		model            string
		promptTokens     int
		completionTokens int
		totalTokens      int
	}
}

func (f *fakeLLMLogger) Record(channelUserID, model string, promptTokens, completionTokens, totalTokens int) error {
	f.records = append(f.records, struct {
		channelUserID    string
		model            string
		promptTokens     int
		completionTokens int
		totalTokens      int
	}{channelUserID, model, promptTokens, completionTokens, totalTokens})
	return nil
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

func (f *fakeConversationStore) GetStoredMessages(channelUserID string, conversationID int64) ([]repository.StoredChatMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return repository.ChatMessagesToStored(f.messages, time.Now()), nil
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "你好" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_NormalIntentGoesThroughLLM(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("LLM 帮助")}}
	runner := &fakeToolRunner{result: "本地帮助"}
	payload := newTestPayload()
	payload.Message.Content = "帮助"
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)

	reply := agent.ProcessMessage(context.Background(), payload)

	if reply != "LLM 帮助" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 1 {
		t.Errorf("expected normal intent to go through llm, got %d calls", llm.callCount)
	}
	if runner.executedName != "" {
		t.Errorf("expected local runner to be skipped, got %s", runner.executedName)
	}
}

func TestAgent_ProcessMessage_IntentRouterFallbackWhenLatestMessageTooLarge(t *testing.T) {
	llm := &fakeLLM{}
	runner := &fakeToolRunner{result: "帮助内容"}
	payload := newTestPayload()
	payload.Message.Content = "帮助"
	agent := newTestAgentWithOptions(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner, AgentOptions{
		ModelMaxTokens:  256,
		MaxOutputTokens: 128,
	})

	reply := agent.ProcessMessage(context.Background(), payload)

	if reply != "帮助内容" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 0 {
		t.Errorf("expected fallback intent to skip llm, got %d calls", llm.callCount)
	}
	if runner.executedName != "show_help" {
		t.Errorf("expected show_help, got %s", runner.executedName)
	}
}

func TestAgent_ProcessMessage_TooLargeUnknownMessageReturnsError(t *testing.T) {
	llm := &fakeLLM{}
	runner := &fakeToolRunner{result: "ok"}
	payload := newTestPayload()
	payload.Message.Content = strings.Repeat("这是一段很长但没有本地工具意图的内容", 200)
	agent := newTestAgentWithOptions(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner, AgentOptions{
		ModelMaxTokens:  256,
		MaxOutputTokens: 128,
	})

	reply := agent.ProcessMessage(context.Background(), payload)

	if reply != "消息过长，无法进入 AI 上下文，请缩短后再试" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.callCount != 0 {
		t.Errorf("expected too large message to skip llm, got %d calls", llm.callCount)
	}
	if runner.executedName != "" {
		t.Errorf("expected no local tool for unknown message, got %s", runner.executedName)
	}
}

func TestAgent_ProcessMessage_UsesConfiguredTokenBudget(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	store := &fakeConversationStore{
		messages: []openai.ChatMessage{
			{Role: "user", Content: strings.Repeat("旧消息", 3000)},
		},
	}
	agent := newTestAgentWithOptions(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, &fakeToolRunner{}, AgentOptions{
		ModelMaxTokens:  8192,
		MaxOutputTokens: 128,
	})

	reply := agent.ProcessMessage(context.Background(), newTestPayload())

	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.lastReq.MaxTokens != 128 {
		t.Errorf("expected max_tokens 128, got %d", llm.lastReq.MaxTokens)
	}
	tokenizer := newTokenCounter("model")
	inputBudget := 8192 - 128 - tokenizer.ToolsTokens(toToolDefinitions()) - tokenSafetyMargin
	if got := tokenizer.MessagesTokens(llm.lastReq.Messages); got > inputBudget {
		t.Errorf("expected trimmed prompt below input budget %d, got %d tokens", inputBudget, got)
	}
}

func TestAgent_ProcessMessage_ToolCallThenReply(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{
		makeToolResponse("get_user_profile", "{}"),
		makeTextResponse("done"),
	}}
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "ok"})
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "AI 没有返回任何内容" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_LLMError(t *testing.T) {
	llm := &fakeLLM{err: errors.New("timeout")}
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "AI 调用失败：timeout" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_UserError(t *testing.T) {
	agent := newTestAgent(
		&fakeLLM{},
		&fakeUserStore{err: errors.New("db error")},
		&fakeConversationStore{}, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "系统内部错误，请稍后再试" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_ContextTimeout(t *testing.T) {
	llm := &fakeLLM{}
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{})
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{}}, &fakeConversationStore{}, &fakeToolRunner{result: "ok"})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "工具调用轮数超过限制，请简化请求" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_GetMessagesError(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	store := &fakeConversationStore{err: errors.New("db error")}
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, &fakeToolRunner{})
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAgent_ProcessMessage_AppendMessageError(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	store := &fakeConversationStore{errOnAppend: true}
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, &fakeToolRunner{})
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, runner)
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, runner)
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
	agent := newTestAgent(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, store, runner)
	agent.ProcessMessage(context.Background(), newTestPayload())

	// reset_conversation 是终止操作：返回结果后直接结束，本轮的 assistant 与 tool
	// 消息不应被持久化到会话历史中（ProcessMessage 在工具循环前追加的 user 消息除外）。
	for _, m := range store.messages {
		if m.Role == "assistant" || m.Role == "tool" {
			t.Errorf("reset tool round should not be persisted, found %s message: %+v", m.Role, m)
		}
	}
}

func TestAgent_ProcessMessage_DeepSeekV4_DisablesThinking(t *testing.T) {
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	agent := NewAgentWithOptions(
		llm,
		"deepseek-v4-flash",
		&fakeUserStore{user: &model.User{ChannelUserID: "u1"}},
		&fakeConversationStore{},
		&fakeToolRunner{},
		AgentOptions{},
		cache.NopCache{},
		nopPersistQueue{},
	)

	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if llm.lastReq.Thinking == nil {
		t.Fatal("expected thinking field to be set")
	}
	if llm.lastReq.Thinking.Type != "disabled" {
		t.Errorf("expected thinking disabled, got %q", llm.lastReq.Thinking.Type)
	}
}

func TestNewAgentWithOptions_OutputTokenClamp(t *testing.T) {
	llm := &fakeLLM{}
	agent := NewAgentWithOptions(
		llm,
		"model",
		&fakeUserStore{},
		&fakeConversationStore{},
		&fakeToolRunner{},
		AgentOptions{ModelMaxTokens: 100, MaxOutputTokens: 200},
		cache.NopCache{},
		nopPersistQueue{},
	)
	if agent.maxOutputTokens >= agent.modelMaxTokens {
		t.Fatalf("expected output tokens clamped below model tokens, got %d/%d", agent.maxOutputTokens, agent.modelMaxTokens)
	}
}

func TestSystemPromptCache_HitMissExpired(t *testing.T) {
	c := newSystemPromptCache(time.Millisecond)
	user := &model.User{Status: userStatusBound, Book: "b", Chara: "c", Title: "t"}
	if _, ok := c.get(user); ok {
		t.Fatal("expected miss on empty cache")
	}
	c.set(user, "prompt1")
	if got, ok := c.get(user); !ok || got != "prompt1" {
		t.Fatalf("expected hit, got %q %v", got, ok)
	}
	time.Sleep(2 * time.Millisecond)
	if _, ok := c.get(user); ok {
		t.Fatal("expected expired entry to miss")
	}
}

func TestBuildSystemPrompt_WaitingEmailCode(t *testing.T) {
	prompt := buildSystemPrompt(&model.User{Status: userStatusWaitingEmailCode, Book: "b", Chara: "c", Title: "null"})
	if !strings.Contains(prompt, "等待邮箱验证码") {
		t.Fatalf("expected waiting email code status, got %s", prompt)
	}
	if !strings.Contains(prompt, timeutil.Date()) {
		t.Fatalf("expected date fallback title, got %s", prompt)
	}
}

func TestAgent_ProcessMessage_CacheUserHit(t *testing.T) {
	fakeCache := &fakeCache{
		user: &model.User{ChannelUserID: "u1", Status: userStatusBound, Book: "b", Chara: "c"},
	}
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	agent := NewAgentWithOptions(
		llm,
		"model",
		&fakeUserStore{err: errors.New("should not call")},
		&fakeConversationStore{},
		&fakeToolRunner{},
		AgentOptions{},
		fakeCache,
		nopPersistQueue{},
	)
	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
}

type fakeCache struct {
	cache.NopCache
	user *model.User
}

func (f *fakeCache) GetUser(ctx context.Context, channelUserID string) (*model.User, error) {
	if f.user != nil {
		return f.user, nil
	}
	return nil, errors.New("not found")
}

func TestAgent_RecordLLMCall(t *testing.T) {
	logger := &fakeLLMLogger{}
	a := &Agent{model: "deepseek-v4", llmCallRepo: logger}

	a.recordLLMCall("u1", &openai.ChatCompletionResponse{Usage: struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}}, nil)

	if len(logger.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(logger.records))
	}
	rec := logger.records[0]
	if rec.channelUserID != "u1" || rec.model != "deepseek-v4" || rec.totalTokens != 150 {
		t.Errorf("unexpected record: %+v", rec)
	}

	// nil repo should not panic
	a.llmCallRepo = nil
	a.recordLLMCall("u1", nil, errors.New("llm failed"))
}

func TestAgent_ProcessMessage_RecordsLLMCall(t *testing.T) {
	logger := &fakeLLMLogger{}
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	llm.responses[0].Usage.PromptTokens = 10
	llm.responses[0].Usage.CompletionTokens = 5
	llm.responses[0].Usage.TotalTokens = 15

	agent := NewAgentWithLogger(
		llm,
		"model",
		&fakeUserStore{user: &model.User{ChannelUserID: "u1", Status: userStatusBound, Book: "b", Chara: "c"}},
		&fakeConversationStore{},
		&fakeToolRunner{},
		AgentOptions{},
		cache.NopCache{},
		nopPersistQueue{},
		logger,
	)

	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "ok" {
		t.Errorf("unexpected reply: %s", reply)
	}
	if len(logger.records) != 1 {
		t.Fatalf("expected 1 llm record, got %d", len(logger.records))
	}
	if logger.records[0].totalTokens != 15 {
		t.Errorf("unexpected total tokens: %d", logger.records[0].totalTokens)
	}
}

type fakeProcessedMessageRepo struct {
	records     []string
	shouldAllow bool
	prevReply   string
	err         error
	markedDone  []string
	markedFail  []string
}

func (f *fakeProcessedMessageRepo) StartProcessing(channelUserID string, conversationID, messageID int64, processingTimeout time.Duration) (bool, string, error) {
	f.records = append(f.records, "start")
	return f.shouldAllow, f.prevReply, f.err
}

func (f *fakeProcessedMessageRepo) MarkDone(channelUserID string, conversationID, messageID int64, reply string) error {
	f.markedDone = append(f.markedDone, reply)
	return nil
}

func (f *fakeProcessedMessageRepo) MarkFailed(channelUserID string, conversationID, messageID int64) error {
	f.markedFail = append(f.markedFail, "fail")
	return nil
}

func TestAgent_ProcessMessage_DuplicateReturnsSavedReply(t *testing.T) {
	repo := &fakeProcessedMessageRepo{shouldAllow: false, prevReply: "already done"}
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("new reply")}}
	agent := newTestAgentWithOptions(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "ok"}, AgentOptions{
		ProcessedMessageRepo: repo,
	})

	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "already done" {
		t.Fatalf("expected saved reply, got %q", reply)
	}
	if len(repo.markedDone) != 0 {
		t.Fatal("expected no MarkDone for duplicate")
	}
}

func TestAgent_ProcessMessage_NewMessageMarksDone(t *testing.T) {
	repo := &fakeProcessedMessageRepo{shouldAllow: true}
	llm := &fakeLLM{responses: []*openai.ChatCompletionResponse{makeTextResponse("ok")}}
	agent := newTestAgentWithOptions(llm, &fakeUserStore{user: &model.User{ChannelUserID: "u1"}}, &fakeConversationStore{}, &fakeToolRunner{result: "tool ok"}, AgentOptions{
		ProcessedMessageRepo: repo,
	})

	reply := agent.ProcessMessage(context.Background(), newTestPayload())
	if reply != "ok" {
		t.Fatalf("expected llm reply, got %q", reply)
	}
	if len(repo.markedDone) != 1 || repo.markedDone[0] != "ok" {
		t.Fatalf("expected MarkDone with reply, got %v", repo.markedDone)
	}
}
