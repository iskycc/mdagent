package app

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/config"
)

func TestRun_StartAndShutdown(t *testing.T) {
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
		CallbackPath:  "/callback",
		OpenAIAPIKey:  "test",
		OpenAIBaseURL: "http://localhost",
		OpenAIModel:   "test-model",
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, db, cfg)
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

func TestRun_ReturnsListenError(t *testing.T) {
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
		Port:          "bad port",
		CallbackPath:  "/callback",
		OpenAIAPIKey:  "test",
		OpenAIBaseURL: "http://localhost",
		OpenAIModel:   "test-model",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := Run(ctx, db, cfg); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestInitRepos_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS agent_users").WillReturnError(sqlmock.ErrCancelled)

	if err := initRepos(db); err == nil {
		t.Fatal("expected error")
	}
}
