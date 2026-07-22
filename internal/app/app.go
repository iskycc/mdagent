package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"miaodi-agent/internal/cache"
	"miaodi-agent/internal/config"
	"miaodi-agent/internal/debuglog"
	"miaodi-agent/internal/handler"
	"miaodi-agent/internal/model"
	"miaodi-agent/internal/persist"
	"miaodi-agent/internal/repository"
	"miaodi-agent/internal/service"
	"miaodi-agent/internal/timeutil"
	"miaodi-agent/pkg/client"
	"miaodi-agent/pkg/openai"
)

// Run 启动应用，阻塞直到 ctx 被取消
func Run(ctx context.Context, db *sql.DB, cfg *config.Config) error {
	// 防御性设置：避免某个依赖库使用 http.DefaultClient 时因无超时而永久挂住。
	http.DefaultClient.Timeout = 30 * time.Second

	if err := initRepos(db); err != nil {
		return fmt.Errorf("init repositories failed: %w", err)
	}

	miaodi := client.NewMiaodiClient()
	llm := openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIBaseURL)
	llm.SetTimeout(8 * time.Second)

	userRepo := repository.NewUserRepo(db)
	convRepo := repository.NewConversationRepo(db)
	pendingRepo := repository.NewPendingImageRepo(db)
	callLogRepo := repository.NewCallLogRepo(db)
	llmCallLogRepo := repository.NewLLMCallLogRepo(db)
	startConversationCleanup(ctx, convRepo, time.Hour)
	startCallLogCleanup(ctx, callLogRepo, time.Hour)
	startLLMCallLogCleanup(ctx, llmCallLogRepo, time.Hour)

	redisAddr := cfg.RedisHost + ":" + cfg.RedisPort
	redisCache := cache.NewRedisCache(redisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisEnabled)
	persistQueue := persist.NewPersistQueue(convRepo, callLogRepo, 1024)
	persistQueue.Run(ctx)

	if err := seedCache(ctx, redisCache, convRepo, userRepo); err != nil {
		log.Printf("seed cache failed: %v", err)
	}

	toolExec := service.NewToolExecutor(miaodi, userRepo, convRepo, pendingRepo, callLogRepo, redisCache, persistQueue)
	toolExec.SetModel(cfg.OpenAIModel)
	agent := service.NewAgentWithLogger(llm, cfg.OpenAIModel, userRepo, convRepo, toolExec, service.AgentOptions{
		ModelMaxTokens:  cfg.ModelMaxTokens,
		MaxOutputTokens: cfg.MaxOutputTokens,
	}, redisCache, persistQueue, llmCallLogRepo)
	callbackHandler := handler.NewCallbackHandler(agent, cfg.CallbackSecret)

	mux := http.NewServeMux()
	callbackHandler.RegisterRoutes(mux, cfg.CallbackPath)

	if cfg.StatsToken != "" {
		statsSvc := service.NewStatsService(userRepo, convRepo, callLogRepo, llmCallLogRepo)
		statsHandler := handler.NewStatsHandler(statsSvc, cfg.StatsToken)
		statsHandler.RegisterRoutes(mux)
	}

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("miaodi-agent listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("listen failed: %v", err)
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		return fmt.Errorf("listen failed: %w", err)
	}
	log.Println("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := persistQueue.Flush(shutdownCtx); err != nil {
		log.Printf("flush persist queue failed: %v", err)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}
	log.Println("server exited")
	return nil
}

type conversationCleaner interface {
	CleanupExpiredMessages(cutoff time.Time) (int, error)
}

type callLogCleaner interface {
	CleanupOlderThan(days int) (int64, error)
}

func startConversationCleanup(ctx context.Context, convRepo conversationCleaner, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed, err := convRepo.CleanupExpiredMessages(timeutil.Now().Add(-24 * time.Hour))
				if err != nil {
					log.Printf("cleanup expired conversation messages failed: %v", err)
					debuglog.Printf("cleanup expired conversation messages failed error=%v", err)
					continue
				}
				if removed > 0 {
					debuglog.Printf("cleanup expired conversation messages removed=%d", removed)
				}
			}
		}
	}()
}

func startCallLogCleanup(ctx context.Context, callLogRepo callLogCleaner, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				deleted, err := callLogRepo.CleanupOlderThan(30)
				if err != nil {
					log.Printf("cleanup call log failed: %v", err)
					debuglog.Printf("cleanup call log failed error=%v", err)
					continue
				}
				if deleted > 0 {
					debuglog.Printf("cleanup call log deleted=%d", deleted)
				}
			}
		}
	}()
}

func startLLMCallLogCleanup(ctx context.Context, llmCallLogRepo callLogCleaner, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				deleted, err := llmCallLogRepo.CleanupOlderThan(30)
				if err != nil {
					log.Printf("cleanup llm call log failed: %v", err)
					debuglog.Printf("cleanup llm call log failed error=%v", err)
					continue
				}
				if deleted > 0 {
					debuglog.Printf("cleanup llm call log deleted=%d", deleted)
				}
			}
		}
	}()
}

type conversationSeeder interface {
	ListActiveSince(cutoff time.Time) ([]repository.ConversationWithMessages, error)
}

type userSeeder interface {
	Get(channelUserID string) (*model.User, error)
}

func seedCache(ctx context.Context, c cache.Cache, convRepo conversationSeeder, userRepo userSeeder) error {
	if !c.Available(ctx) {
		return fmt.Errorf("redis not available")
	}
	conversations, err := convRepo.ListActiveSince(timeutil.Now().Add(-24 * time.Hour))
	if err != nil {
		return err
	}

	userIDSet := make(map[string]struct{})
	for _, conv := range conversations {
		userIDSet[conv.ChannelUserID] = struct{}{}
	}

	for uid := range userIDSet {
		user, err := userRepo.Get(uid)
		if err != nil {
			log.Printf("seed cache get user %s failed: %v", uid, err)
			continue
		}
		if err := c.SetUser(ctx, user); err != nil {
			log.Printf("seed cache set user %s failed: %v", uid, err)
		}
	}

	for _, conv := range conversations {
		if err := c.SetMessages(ctx, conv.ChannelUserID, conv.ConversationID, conv.Messages); err != nil {
			log.Printf("seed cache set conv %s/%d failed: %v", conv.ChannelUserID, conv.ConversationID, err)
		}
	}
	return nil
}

func initRepos(db *sql.DB) error {
	repos := []struct {
		name string
		fn   func() error
	}{
		{"agent_users", repository.NewUserRepo(db).EnsureTable},
		{"agent_conversations", repository.NewConversationRepo(db).EnsureTable},
		{"pending_images", repository.NewPendingImageRepo(db).EnsureTable},
		{"api_call_log", repository.NewCallLogRepo(db).EnsureTable},
		{"llm_call_log", repository.NewLLMCallLogRepo(db).EnsureTable},
	}
	for _, r := range repos {
		if err := r.fn(); err != nil {
			return fmt.Errorf("ensure table %s failed: %w", r.name, err)
		}
	}
	return nil
}
