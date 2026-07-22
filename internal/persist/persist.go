package persist

import (
	"context"
	"log"
	"time"

	"miaodi-agent/internal/repository"
	"miaodi-agent/pkg/openai"
)

// Queue 定义异步持久化队列。
type Queue interface {
	EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage)
	EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string)
	Run(ctx context.Context)
	Flush(ctx context.Context) error
}

// PersistQueue 把会话消息和调用日志异步写回 MySQL。
type PersistQueue struct {
	convRepo    *repository.ConversationRepo
	callLogRepo *repository.CallLogRepo
	tasks       chan task
}

type taskKind int

const (
	taskKindConv taskKind = iota
	taskKindLog
)

const workerCount = 3

type task struct {
	kind           taskKind
	channelUserID  string
	conversationID int64
	messages       []repository.StoredChatMessage
	apikey         string
	channel        string
	action         string
}

// NewPersistQueue 创建队列，bufferSize 为内部 channel 容量。
func NewPersistQueue(convRepo *repository.ConversationRepo, callLogRepo *repository.CallLogRepo, bufferSize int) *PersistQueue {
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	return &PersistQueue{
		convRepo:    convRepo,
		callLogRepo: callLogRepo,
		tasks:       make(chan task, bufferSize),
	}
}

func (q *PersistQueue) EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) {
	select {
	case q.tasks <- task{kind: taskKindConv, channelUserID: channelUserID, conversationID: conversationID, messages: msgs}:
	case <-ctx.Done():
	}
}

func (q *PersistQueue) EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string) {
	select {
	case q.tasks <- task{kind: taskKindLog, channelUserID: channelUserID, apikey: apikey, channel: channel, action: action}:
	case <-ctx.Done():
	}
}

func (q *PersistQueue) Run(ctx context.Context) {
	for i := 0; i < workerCount; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-q.tasks:
					q.process(ctx, t)
				}
			}
		}()
	}
}

func (q *PersistQueue) process(ctx context.Context, t task) {
	const maxRetries = 3
	backoff := 100 * time.Millisecond
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		var err error
		switch t.kind {
		case taskKindConv:
			msgs := storedToChatMessages(t.messages)
			err = q.convRepo.AppendMessages(t.channelUserID, t.conversationID, msgs...)
		case taskKindLog:
			err = q.callLogRepo.Record(t.channelUserID, t.apikey, t.channel, t.action)
		}
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(backoff)
		backoff *= 2
	}
	log.Printf("persist task failed after retries: kind=%d user=%s conv=%d err=%v", t.kind, t.channelUserID, t.conversationID, lastErr)
}

func (q *PersistQueue) Flush(ctx context.Context) error {
	for {
		select {
		case t := <-q.tasks:
			q.process(ctx, t)
		default:
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func storedToChatMessages(stored []repository.StoredChatMessage) []openai.ChatMessage {
	msgs := make([]openai.ChatMessage, 0, len(stored))
	for _, s := range stored {
		msgs = append(msgs, s.ChatMessage)
	}
	return msgs
}
