package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"miaodi-agent/internal/model"
	"miaodi-agent/pkg/openai"
)

const (
	defaultModelMaxTokens  = 8192
	defaultMaxOutputTokens = 1024
	defaultMaxToolRounds   = 3
)

// AgentOptions 控制 Agent 与模型交互时的资源预算。
type AgentOptions struct {
	ModelMaxTokens  int
	MaxOutputTokens int
}

// Agent 是 AI Agent 核心服务
type Agent struct {
	llm             LLMClient
	model           string
	userRepo        UserStore
	convRepo        ConversationStore
	toolExec        ToolRunner
	intentRouter    *IntentRouter
	modelMaxTokens  int
	maxOutputTokens int
}

// NewAgent 创建 Agent
func NewAgent(llm LLMClient, modelName string, userRepo UserStore, convRepo ConversationStore, toolExec ToolRunner) *Agent {
	return NewAgentWithOptions(llm, modelName, userRepo, convRepo, toolExec, AgentOptions{})
}

// NewAgentWithOptions 创建带资源预算的 Agent。
func NewAgentWithOptions(llm LLMClient, modelName string, userRepo UserStore, convRepo ConversationStore, toolExec ToolRunner, opts AgentOptions) *Agent {
	if opts.ModelMaxTokens <= 0 {
		opts.ModelMaxTokens = defaultModelMaxTokens
	}
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = defaultMaxOutputTokens
	}
	if opts.MaxOutputTokens >= opts.ModelMaxTokens {
		opts.MaxOutputTokens = defaultMaxOutputTokens
		if opts.MaxOutputTokens >= opts.ModelMaxTokens {
			opts.MaxOutputTokens = opts.ModelMaxTokens / 4
		}
	}

	return &Agent{
		llm:             llm,
		model:           modelName,
		userRepo:        userRepo,
		convRepo:        convRepo,
		toolExec:        toolExec,
		intentRouter:    NewIntentRouter(toolExec),
		modelMaxTokens:  opts.ModelMaxTokens,
		maxOutputTokens: opts.MaxOutputTokens,
	}
}

// ProcessMessage 处理一条 传送鸽 消息，返回最终文本回复
func (a *Agent) ProcessMessage(ctx context.Context, payload *model.CallbackPayload) string {
	channelUserID := payload.User.UserID
	conversationID := payload.Conversation.ID

	user, err := a.userRepo.GetOrCreate(channelUserID)
	if err != nil {
		log.Printf("get or create user failed: %v", err)
		return "系统内部错误，请稍后再试"
	}

	if reply, handled := a.intentRouter.Route(user, channelUserID, conversationID, payload.Message.Content); handled {
		return reply
	}

	// 追加用户消息到历史
	userMsg := openai.ChatMessage{Role: "user", Content: payload.Message.Content}
	if err := a.convRepo.AppendMessage(channelUserID, conversationID, userMsg); err != nil {
		log.Printf("append user message failed: %v", err)
	}

	history, err := a.convRepo.GetMessages(channelUserID, conversationID)
	if err != nil {
		log.Printf("get messages failed: %v", err)
		history = []openai.ChatMessage{}
	}

	systemMsg := openai.ChatMessage{
		Role:    "system",
		Content: buildSystemPrompt(user),
	}
	messages := append([]openai.ChatMessage{systemMsg}, history...)

	tools := toToolDefinitions()
	for i := 0; i < defaultMaxToolRounds; i++ {
		select {
		case <-ctx.Done():
			return "处理超时，请缩短消息或稍后再试"
		default:
		}

		req := openai.ChatCompletionRequest{
			Model:      a.model,
			Messages:   FitMessagesForTokenBudget(messages, tools, a.modelMaxTokens, a.maxOutputTokens),
			Tools:      tools,
			ToolChoice: "auto",
			MaxTokens:  a.maxOutputTokens,
		}

		resp, err := a.llm.CreateChatCompletion(ctx, req)
		if err != nil {
			log.Printf("llm call failed: %v", err)
			return fmt.Sprintf("AI 调用失败：%v", err)
		}
		if len(resp.Choices) == 0 {
			return "AI 没有返回任何内容"
		}

		choice := resp.Choices[0]
		assistantMsg := choice.Message

		if choice.FinishReason != "tool_calls" && len(assistantMsg.ToolCalls) == 0 {
			// 最终回复
			if err := a.convRepo.AppendMessage(channelUserID, conversationID, assistantMsg); err != nil {
				log.Printf("append assistant message failed: %v", err)
			}
			if assistantMsg.Content == "" {
				return "收到空回复"
			}
			return assistantMsg.Content
		}

		// 执行工具调用
		toolResults := make([]openai.ChatMessage, 0, len(assistantMsg.ToolCalls))
		resetResult := ""
		resetDone := false
		for _, tc := range assistantMsg.ToolCalls {
			result := a.toolExec.Execute(user, channelUserID, conversationID, tc.Function.Name, tc.Function.Arguments)
			toolResults = append(toolResults, openai.ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
			if tc.Function.Name == "reset_conversation" {
				resetDone = true
				resetResult = result
			}
		}

		// reset_conversation 是终止操作：会话历史已被清空，
		// 直接返回结果，不再持久化本轮消息，也不再调用 LLM。
		if resetDone {
			return resetResult
		}

		// 把 assistant 消息和 tool 结果加入上下文
		messages = append(messages, assistantMsg)
		messages = append(messages, toolResults...)

		// 持久化到数据库
		if err := a.convRepo.AppendMessages(channelUserID, conversationID, append([]openai.ChatMessage{assistantMsg}, toolResults...)...); err != nil {
			log.Printf("append tool round messages failed: %v", err)
		}
	}

	return "工具调用轮数超过限制，请简化请求"
}

// toToolDefinitions 转换工具定义
func toToolDefinitions() []openai.ToolDefinition {
	return ToolDefinitions()
}

func buildSystemPrompt(user *model.User) string {
	status := "未绑定"
	if user.Status == "bound" {
		status = "已绑定"
	}
	title := user.Title
	if title == "" || title == "null" {
		title = time.Now().Format("2006-01-02")
	}

	return fmt.Sprintf(`你是“喵滴 AI 助手”。不要编造操作结果；用户要执行动作时必须调用工具。

当前状态：%s；默认路径：书本《%s》/ 章节《%s》/ 标题《%s》；今天：%s。

工具选择规则：
- 绑定/提供 key -> bind_miaodi_key
- 设置保存位置/书/章/标题 -> set_save_path
- 查看绑定状态或路径 -> get_user_profile
- 保存文字 -> save_text_note
- 保存图片链接 -> save_image_note，只入待上传队列
- 清空/重置/忘记对话 -> reset_conversation
- 帮助/怎么用/能做什么 -> show_help
- 最近保存了什么 -> list_recent_notes
- 某日期/今天/昨天保存了什么 -> query_notes_by_date，date 用 YYYY-MM-DD

未绑定时，保存类请求先提示绑定。最终回复简洁自然，200 字以内。`,
		status, user.Book, user.Chara, title, time.Now().Format("2006-01-02"))
}
