package persist

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"miaodi-agent/internal/repository"
	"miaodi-agent/pkg/openai"
)

// Queue 定义异步持久化队列。
type Queue interface {
	// EnqueueConv 将会话消息追加任务入队。返回 false 表示入队失败，调用方应同步回写 MySQL。
	EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) bool
	// EnqueueLog 将调用日志任务入队。返回 false 表示入队失败，调用方应同步回写 MySQL。
	EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string) bool
	Run(ctx context.Context)
	Flush(ctx context.Context) error
}

// PersistQueue 把会话消息和调用日志异步写回 MySQL。
type PersistQueue struct {
	convRepo       *repository.ConversationRepo
	callLogRepo    *repository.CallLogRepo
	deadLetterRepo *repository.DeadLetterRepo
	tasks          chan task
	inFlight       atomic.Int64
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
func NewPersistQueue(convRepo *repository.ConversationRepo, callLogRepo *repository.CallLogRepo, deadLetterRepo *repository.DeadLetterRepo, bufferSize int) *PersistQueue {
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	return &PersistQueue{
		convRepo:       convRepo,
		callLogRepo:    callLogRepo,
		deadLetterRepo: deadLetterRepo,
		tasks:          make(chan task, bufferSize),
	}
}

func (q *PersistQueue) EnqueueConv(ctx context.Context, channelUserID string, conversationID int64, msgs []repository.StoredChatMessage) bool {
	select {
	case q.tasks <- task{kind: taskKindConv, channelUserID: channelUserID, conversationID: conversationID, messages: msgs}:
		return true
	case <-ctx.Done():
		return false
	default:
		// channel 已满，立即失败，由调用方同步 fallback
		return false
	}
}

func (q *PersistQueue) EnqueueLog(ctx context.Context, channelUserID, apikey, channel, action string) bool {
	select {
	case q.tasks <- task{kind: taskKindLog, channelUserID: channelUserID, apikey: apikey, channel: channel, action: action}:
		return true
	case <-ctx.Done():
		return false
	default:
		return false
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
					q.process(t)
				}
			}
		}()
	}
}

func (q *PersistQueue) process(t task) {
	q.inFlight.Add(1)
	defer q.inFlight.Add(-1)

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
	if q.deadLetterRepo != nil {
		switch t.kind {
		case taskKindConv:
			if err := q.deadLetterRepo.RecordConv(t.channelUserID, t.conversationID, t.messages, lastErr); err != nil {
				log.Printf("record conv dead letter failed: %v", err)
			}
		case taskKindLog:
			if err := q.deadLetterRepo.RecordLog(t.channelUserID, t.apikey, t.channel, t.action, lastErr); err != nil {
				log.Printf("record log dead letter failed: %v", err)
			}
		}
	}
}

func (q *PersistQueue) Flush(ctx context.Context) error {
	for {
		select {
		case t := <-q.tasks:
			q.process(t)
		case <-ctx.Done():
			return ctx.Err()
		default:
			// 等待所有 worker 和本 goroutine 正在处理的任务完成
			for q.inFlight.Load() > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case t := <-q.tasks:
					q.process(t)
				default:
					time.Sleep(10 * time.Millisecond)
				}
			}
			return nil
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
