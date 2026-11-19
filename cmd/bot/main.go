package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	ownerID := envOr("OWNER_ID", "user-default")

	a2aSvc := a2a.NewService(a2a.Config{
		BotID: cfg.BotID, BotName: cfg.BotName, PublicURL: cfg.BotPublicURL,
		BearerToken: cfg.A2ABearerToken, A2APath: cfg.A2APath, DB: db,
		HostRegistry:      cfg.A2AHostRegistry,
		MarketRecruitMode: cfg.A2AMarketRecruitMode,
		MarketLat:         cfg.MarketLat,
		MarketLng:         cfg.MarketLng,
		Registration: a2a.RegistrationConfig{
			Enabled:           cfg.A2AAutoRegister,
			RegistryURLs:      cfg.A2ARegistryURLs,
			HeartbeatInterval: time.Duration(cfg.A2ARegistryHeartbeatSec) * time.Second,
		},
		ProfileFn: func(ctx context.Context) string {
			p, err := mem.GetProfile(ctx, ownerID)
			if err != nil || p == nil {
				return ""
			}
			if p.Summary != "" {
				return p.Summary
			}
			return p.DisplayName
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := agent.NewRuntime(ctx, cfg, db, mem, a2aSvc, ownerID)
	if err != nil {
		log.Fatalf("agent: %v", err)
	}

	asrClient := asr.New(cfg.ASRBaseURL, cfg.ASRAPIKey, cfg.ASRModel)
	srv := server.New(rt, asrClient)

	// 启动后自动向 Registry 注册本 Agent Card，并保持心跳。
	a2aSvc.StartAutoRegister(ctx)

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		sig := <-ch
		log.Printf("received %v, shutting down", sig)
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		a2aSvc.StopAutoRegister(stopCtx)
		cancel()
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
