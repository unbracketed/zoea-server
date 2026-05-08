package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/unbracketed/zoea-server/internal/api"
	"github.com/unbracketed/zoea-server/internal/auth"
	"github.com/unbracketed/zoea-server/internal/config"
	"github.com/unbracketed/zoea-server/internal/introspect"
	"github.com/unbracketed/zoea-server/internal/process"
	"github.com/unbracketed/zoea-server/internal/session"
	"github.com/unbracketed/zoea-server/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "status" {
		runStatus()
		return
	}

	cfg := config.LoadFromEnv()

	st, err := store.Open(cfg.StoreDriver, cfg.StoreDSN)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if err := st.Init(context.Background()); err != nil {
		log.Fatalf("init store: %v", err)
	}

	pm := process.NewRPCProcessManager(
		cfg.PiBinPath,
		cfg.PiArgs,
		cfg.SessionsBaseDir,
		cfg.DefaultWorkingDir,
	)
	sm := session.NewManager(pm, st)

	if err := sm.Init(context.Background()); err != nil {
		log.Fatalf("init session manager: %v", err)
	}

	// Boot-time introspection: spawn one Pi to capture the set of slash
	// commands and tools available for cfg.DefaultWorkingDir. Failures are
	// non-fatal — the server runs in degraded mode (clients see
	// available:false on /v1/config, autocomplete and the settings panel
	// stay empty). Bounded so a wedged Pi can't block startup.
	introspectCtx, cancelIntrospect := context.WithTimeout(context.Background(), 30*time.Second)
	piConfig, err := introspect.Run(introspectCtx, pm, cfg.DefaultWorkingDir)
	cancelIntrospect()
	if err != nil {
		log.Printf("zoea-server: boot-time introspection failed: %v", err)
		piConfig = nil
	} else {
		log.Printf("zoea-server: introspection captured %d commands, %d tools", len(piConfig.Commands), len(piConfig.Tools))
	}

	h := api.NewHandler(sm, cfg.DefaultWorkingDir, piConfig)

	// Build middleware chain: rate limit → auth → routes
	var handler http.Handler = h.Routes()
	handler = auth.Middleware(&cfg.Auth)(handler)
	handler = auth.RateLimitMiddleware(&cfg.Auth)(handler)

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

	log.Printf("zoea-server listening on %s (%s)", cfg.ListenAddr, authModeString(&cfg.Auth))
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
