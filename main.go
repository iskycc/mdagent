package main

import (
	"context"
	"database/sql"
	"log"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"miaodi-agent/internal/app"
	"miaodi-agent/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	openDB := func(dsn string) (*sql.DB, error) {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(cfg.DBMaxOpen)
		db.SetMaxIdleConns(cfg.DBMaxIdle)
		db.SetConnMaxLifetime(time.Minute * 5)
		return db, nil
	}

	if err := run(ctx, func() *config.Config { return cfg }, openDB); err != nil {
		log.Fatalf("run failed: %v", err)
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
