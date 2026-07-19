package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/config"
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
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_conversations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS pending_images").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS api_call_log").WillReturnResult(sqlmock.NewResult(0, 0))

	cfg := &config.Config{
		Port:          "0",
		DBUser:        "u",
		DBName:        "n",
		CallbackPath:  "/callback",
		OpenAIAPIKey:  "test",
		OpenAIBaseURL: "http://localhost",
		OpenAIModel:   "test-model",
		ModelMaxTokens: 8192,
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
