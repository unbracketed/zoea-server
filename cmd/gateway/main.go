package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/brian/go-agent-gateway/internal/api"
	"github.com/brian/go-agent-gateway/internal/auth"
	"github.com/brian/go-agent-gateway/internal/config"
	"github.com/brian/go-agent-gateway/internal/process"
	"github.com/brian/go-agent-gateway/internal/session"
)

func main() {
	cfg := config.LoadFromEnv()

	pm := process.NewRPCProcessManager(cfg.PiBinPath, cfg.PiArgs, cfg.SessionsBaseDir)
	sm := session.NewManager(pm)

	h := api.NewHandler(sm)

	// Build middleware chain: rate limit → auth → routes
	var handler http.Handler = h.Routes()
	handler = auth.Middleware(&cfg.Auth)(handler)
	handler = auth.RateLimitMiddleware(cfg.Auth.BehindProxy)(handler)

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("gateway listening on %s (%s)", cfg.ListenAddr, authModeString(&cfg.Auth))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func authModeString(cfg *auth.AuthConfig) string {
	if !cfg.IsEnabled() {
		return "auth: disabled, local-only access"
	}
	parts := []string{}
	if len(cfg.APIKeys) > 0 {
		parts = append(parts, fmt.Sprintf("api-key, %d keys configured", len(cfg.APIKeys)))
	}
	if cfg.JWKSUrl != "" {
		parts = append(parts, "jwt")
	}
	return "auth: " + joinStrings(parts, " + ")
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return "enabled"
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}
