package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"miaodi-agent/internal/app"
	"miaodi-agent/internal/config"
)

// swappable in tests so main() can be exercised without touching process state.
var (
	loadConfig    = config.Load
	notifyContext = signal.NotifyContext
	logFatalf     = log.Fatalf
)

func main() {
	cfg := loadConfig()
	if err := runMain(notifyContext, func() *config.Config { return cfg }, defaultOpenDB(cfg)); err != nil {
		logFatalf("run failed: %v", err)
	}
}

// runMain contains the testable portion of main().
func runMain(
	notifyCtx func(context.Context, ...os.Signal) (context.Context, context.CancelFunc),
	loadConfig func() *config.Config,
	openDB func(string) (*sql.DB, error),
) error {
	ctx, stop := notifyCtx(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := loadConfig()
	return run(ctx, func() *config.Config { return cfg }, openDB)
}

func defaultOpenDB(cfg *config.Config) func(string) (*sql.DB, error) {
	return func(dsn string) (*sql.DB, error) {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(cfg.DBMaxOpen)
		db.SetMaxIdleConns(cfg.DBMaxIdle)
		db.SetConnMaxLifetime(time.Minute * 5)
		return db, nil
	}
}

func run(ctx context.Context, loadConfig func() *config.Config, openDB func(string) (*sql.DB, error)) error {
	cfg := loadConfig()
	if err := cfg.Validate(); err != nil {
		return err
	}

	db, err := openDB(cfg.DSN())
	if err != nil {
		return err
	}
	defer db.Close()

	return app.Run(ctx, db, cfg)
}
