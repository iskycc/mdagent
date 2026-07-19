package service

import (
	"context"
	"fmt"
	"log"

	"miaodi-agent/internal/debuglog"
	"miaodi-agent/internal/model"
	"miaodi-agent/internal/timeutil"
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
	debuglog.Printf("agent process start user=%s conversation=%d message_id=%d content=%q", channelUserID, conversationID, payload.Message.ID, payload.Message.Content)

	user, err := a.userRepo.GetOrCreate(channelUserID)
	if err != nil {
		log.Printf("get or create user failed: %v", err)
		return a.debugReturn("agent user load failed", "系统内部错误，请稍后再试")
	}
	debuglog.Printf("agent user loaded user=%s status=%s book=%q chapter=%q title=%q", channelUserID, user.Status, user.Book, user.Chara, user.Title)

	if reply, handled := a.intentRouter.Route(user, channelUserID, conversationID, payload.Message.Content); handled {
		debuglog.Printf("agent local intent handled user=%s conversation=%d reply=%q", channelUserID, conversationID, reply)
		return a.debugReturn("agent local intent response", reply)
	}

	// 追加用户消息到历史
	userMsg := openai.ChatMessage{Role: "user", Content: payload.Message.Content}
	if err := a.convRepo.AppendMessage(channelUserID, conversationID, userMsg); err != nil {
		log.Printf("append user message failed: %v", err)
		debuglog.Printf("agent append user message failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
	} else {
		debuglog.Printf("agent appended user message user=%s conversation=%d", channelUserID, conversationID)
	}

	history, err := a.convRepo.GetMessages(channelUserID, conversationID)
	if err != nil {
		log.Printf("get messages failed: %v", err)
		debuglog.Printf("agent get history failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
		history = []openai.ChatMessage{}
	}
	debuglog.Printf("agent history loaded user=%s conversation=%d messages=%d", channelUserID, conversationID, len(history))

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
		debuglog.Printf(
			"agent llm round=%d model=%s messages_before=%d messages_sent=%d estimated_prompt_tokens=%d max_output_tokens=%d",
			i+1,
			a.model,
			len(messages),
			len(req.Messages),
			estimateBlockTokens(req.Messages)+estimateToolsTokens(tools),
			a.maxOutputTokens,
		)

		resp, err := a.llm.CreateChatCompletion(ctx, req)
		if err != nil {
			log.Printf("llm call failed: %v", err)
			return a.debugReturn("agent llm failed", fmt.Sprintf("AI 调用失败：%v", err))
		}
		if len(resp.Choices) == 0 {
			return a.debugReturn("agent llm empty choices", "AI 没有返回任何内容")
		}

		choice := resp.Choices[0]
		assistantMsg := choice.Message
		debuglog.Printf("agent llm choice round=%d finish_reason=%s content=%q tool_calls=%d", i+1, choice.FinishReason, assistantMsg.Content, len(assistantMsg.ToolCalls))

		if choice.FinishReason != "tool_calls" && len(assistantMsg.ToolCalls) == 0 {
			// 最终回复
			if err := a.convRepo.AppendMessage(channelUserID, conversationID, assistantMsg); err != nil {
				log.Printf("append assistant message failed: %v", err)
				debuglog.Printf("agent append assistant message failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
			}
			if assistantMsg.Content == "" {
				return a.debugReturn("agent empty assistant content", "收到空回复")
			}
			return a.debugReturn("agent final response", assistantMsg.Content)
		}

		// 执行工具调用
		toolResults := make([]openai.ChatMessage, 0, len(assistantMsg.ToolCalls))
		resetResult := ""
		resetDone := false
		for _, tc := range assistantMsg.ToolCalls {
			debuglog.Printf("agent tool call user=%s conversation=%d id=%s name=%s arguments=%s", channelUserID, conversationID, tc.ID, tc.Function.Name, tc.Function.Arguments)
			result := a.toolExec.Execute(user, channelUserID, conversationID, tc.Function.Name, tc.Function.Arguments)
			debuglog.Printf("agent tool result user=%s conversation=%d id=%s name=%s result=%q", channelUserID, conversationID, tc.ID, tc.Function.Name, result)
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
			return a.debugReturn("agent reset response", resetResult)
		}

		// 把 assistant 消息和 tool 结果加入上下文
		messages = append(messages, assistantMsg)
		messages = append(messages, toolResults...)

		// 持久化到数据库
		if err := a.convRepo.AppendMessages(channelUserID, conversationID, append([]openai.ChatMessage{assistantMsg}, toolResults...)...); err != nil {
			log.Printf("append tool round messages failed: %v", err)
			debuglog.Printf("agent append tool round failed user=%s conversation=%d error=%v", channelUserID, conversationID, err)
		} else {
			debuglog.Printf("agent appended tool round user=%s conversation=%d messages=%d", channelUserID, conversationID, 1+len(toolResults))
		}
	}

	return a.debugReturn("agent max tool rounds exceeded", "工具调用轮数超过限制，请简化请求")
}

func (a *Agent) debugReturn(reason, reply string) string {
	debuglog.Printf("%s reply=%q", reason, reply)
	return reply
}

// toToolDefinitions 转换工具定义
func toToolDefinitions() []openai.ToolDefinition {
	return ToolDefinitions()
}

func buildSystemPrompt(user *model.User) string {
	status := "未绑定"
	if user.Status == userStatusBound {
		status = "已绑定"
	}
	if user.Status == userStatusWaitingEmailCode {
		status = "等待邮箱验证码"
	}
	title := user.Title
	if title == "" || title == "null" {
		title = timeutil.Date()
	}

	return fmt.Sprintf(`你是“喵滴 AI 助手”。不要编造操作结果；用户要执行动作时必须调用工具。

当前状态：%s；默认路径：书本《%s》/ 章节《%s》/ 标题《%s》；今天：%s。

工具选择规则：
- 绑定/提供 key -> bind_miaodi_key
- 邮箱绑定/发送验证码 -> send_miaodi_email_code
- 用户提供邮箱验证码 -> bind_miaodi_by_email_code
- 设置保存位置/书/章/标题 -> set_save_path
- 查看绑定状态或路径 -> get_user_profile
- 获取当前绑定 key -> get_miaodi_key
- 年度报告/报告地址 -> get_miaodi_annual_report
- 解除绑定/解绑 -> unbind_miaodi_key
- 保存文字 -> save_text_note
- 保存图片链接 -> save_image_note，只入待上传队列
- 清空/重置/忘记对话 -> reset_conversation
- 帮助/怎么用/能做什么 -> show_help
- 最近保存了什么 -> list_recent_notes
- 某日期/今天/昨天保存了什么 -> query_notes_by_date，date 用 YYYY-MM-DD
- 当前准确时间/今天日期/星期几 -> get_current_time
- 算术计算 -> calculate
- 日期前后推算 -> date_calculate
- 随机数 -> random_number
- 随机选择/帮我选 -> choose_option
- 统计字数/文本长度 -> text_stats

未绑定时，保存类请求先提示绑定。最终回复简洁自然，200 字以内。`,
		status, user.Book, user.Chara, title, timeutil.Date())
}
