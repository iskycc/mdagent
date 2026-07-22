package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/config"
	"miaodi-agent/internal/repository"
)

func TestRun_ConfigValidationError(t *testing.T) {
	ctx := context.Background()
	err := run(ctx, func() *config.Config { return &config.Config{} }, nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRun_DBOpenError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{OpenAIAPIKey: "k", DBUser: "u", DBName: "n", ModelMaxTokens: 8192, MaxOutputTokens: 1024}
	err := run(ctx, func() *config.Config { return cfg }, func(string) (*sql.DB, error) {
		return nil, errors.New("db open error")
	})
	if err == nil {
		t.Fatal("expected db open error")
	}
}

func TestRun_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users ADD COLUMN email").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users MODIFY COLUMN status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE agent_users").WithArgs(repository.DefaultBook, repository.DefaultChara).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_conversations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS pending_images").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS api_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS llm_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS persist_dead_letters").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS processed_messages").WillReturnResult(sqlmock.NewResult(0, 0))

	cfg := &config.Config{
		Port:            "0",
		DBUser:          "u",
		DBName:          "n",
		CallbackPath:    "/callback",
		OpenAIAPIKey:    "test",
		OpenAIBaseURL:   "http://localhost",
		OpenAIModel:     "test-model",
		ModelMaxTokens:  8192,
		MaxOutputTokens: 1024,
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, func() *config.Config { return cfg }, func(string) (*sql.DB, error) { return db, nil })
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return in time")
	}
}

func TestRunMain_ConfigValidationError(t *testing.T) {
	notifyCtx := func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}
	err := runMain(notifyCtx, func() *config.Config { return &config.Config{} }, nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRunMain_DBOpenError(t *testing.T) {
	notifyCtx := func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}
	cfg := &config.Config{OpenAIAPIKey: "k", DBUser: "u", DBName: "n", ModelMaxTokens: 8192, MaxOutputTokens: 1024}
	err := runMain(notifyCtx, func() *config.Config { return cfg }, func(string) (*sql.DB, error) {
		return nil, errors.New("db open error")
	})
	if err == nil {
		t.Fatal("expected db open error")
	}
}

func TestDefaultOpenDB(t *testing.T) {
	cfg := &config.Config{DBMaxOpen: 10, DBMaxIdle: 5}
	openDB := defaultOpenDB(cfg)
	db, err := openDB(cfg.DSN())
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	if db == nil {
		t.Fatal("expected non-nil db")
	}
	_ = db.Close()
}

func TestDefaultOpenDB_OpenError(t *testing.T) {
	cfg := &config.Config{DBMaxOpen: 10, DBMaxIdle: 5}
	openDB := defaultOpenDB(cfg)
	_, err := openDB("invalid dsn")
	if err == nil {
		t.Fatal("expected open error")
	}
}

func TestMain_Error(t *testing.T) {
	origLoad := loadConfig
	loadConfig = func() *config.Config { return &config.Config{} }
	defer func() { loadConfig = origLoad }()

	origNotify := notifyContext
	notifyContext = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}
	defer func() { notifyContext = origNotify }()

	var logged string
	origLog := logFatalf
	logFatalf = func(format string, v ...interface{}) {
		logged = fmt.Sprintf(format, v...)
	}
	defer func() { logFatalf = origLog }()

	main()
	if !strings.Contains(logged, "run failed") {
		t.Errorf("expected fatal log, got: %s", logged)
	}
}

func TestRunMain_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users ADD COLUMN email").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE agent_users MODIFY COLUMN status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE agent_users").WithArgs(repository.DefaultBook, repository.DefaultChara).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_conversations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS pending_images").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS api_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS llm_call_log").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS persist_dead_letters").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS processed_messages").WillReturnResult(sqlmock.NewResult(0, 0))

	cfg := &config.Config{
		Port:            "0",
		DBUser:          "u",
		DBName:          "n",
		CallbackPath:    "/callback",
		OpenAIAPIKey:    "test",
		OpenAIBaseURL:   "http://localhost",
		OpenAIModel:     "test-model",
		ModelMaxTokens:  8192,
		MaxOutputTokens: 1024,
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		notifyCtx := func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
			return ctx, cancel
		}
		errCh <- runMain(notifyCtx, func() *config.Config { return cfg }, func(string) (*sql.DB, error) { return db, nil })
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runMain did not return in time")
	}
}
