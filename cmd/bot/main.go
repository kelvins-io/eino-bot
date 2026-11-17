package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/kelvins-io/eino-bot/internal/a2a"
	"github.com/kelvins-io/eino-bot/internal/agent"
	"github.com/kelvins-io/eino-bot/internal/asr"
	"github.com/kelvins-io/eino-bot/internal/config"
	"github.com/kelvins-io/eino-bot/internal/memory"
	"github.com/kelvins-io/eino-bot/internal/server"
	"github.com/kelvins-io/eino-bot/internal/store"
)

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load(".env.local")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	db, err := store.Open(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("sqlite: %v", err)
	}
	defer db.Close()

	mem := memory.New(db)
	a2aClient := a2a.NewClient(cfg.BotID, cfg.BotName, cfg.BotPublicURL, cfg.A2ASharedSecret, db)
	ownerID := envOr("OWNER_ID", "user-default")

	ctx := context.Background()
	rt, err := agent.NewRuntime(ctx, cfg, db, mem, a2aClient, ownerID)
	if err != nil {
		log.Fatalf("agent: %v", err)
	}

	asrClient := asr.New(cfg.ASRBaseURL, cfg.ASRAPIKey, cfg.ASRModel)
	srv := server.New(rt, asrClient)

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		sig := <-ch
		log.Printf("received %v, shutting down", sig)
		os.Exit(0)
	}()

	srv.Run()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
