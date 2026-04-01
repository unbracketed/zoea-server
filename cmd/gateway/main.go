package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/brian/go-agent-gateway/internal/api"
	"github.com/brian/go-agent-gateway/internal/config"
	"github.com/brian/go-agent-gateway/internal/process"
	"github.com/brian/go-agent-gateway/internal/session"
)

func main() {
	cfg := config.LoadFromEnv()

	pm := process.NewRPCProcessManager(cfg.PiBinPath, cfg.PiArgs, cfg.SessionsBaseDir)
	sm := session.NewManager(pm)

	h := api.NewHandler(sm)
	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: h.Routes(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("gateway listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
