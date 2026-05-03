package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/unbracketed/zoea-server/internal/api"
	"github.com/unbracketed/zoea-server/internal/auth"
	"github.com/unbracketed/zoea-server/internal/config"
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
		resolvePublicURL(cfg),
	)
	sm := session.NewManager(pm, st)

	if err := sm.Init(context.Background()); err != nil {
		log.Fatalf("init session manager: %v", err)
	}

	h := api.NewHandler(sm)

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

// resolvePublicURL returns the URL clients (and capability subprocesses
// spawned inside Pi) should use to reach this Zoea instance. Honors the
// explicit ``ZOEA_PUBLIC_URL`` config first; falls back to a best-guess
// derived from ListenAddr (good enough for local dev where everything's
// on 127.0.0.1).
func resolvePublicURL(cfg config.Config) string {
	if cfg.PublicURL != "" {
		return strings.TrimRight(cfg.PublicURL, "/")
	}
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		// ListenAddr is just a port (e.g. ``:8080``); SplitHostPort
		// returns an error in that case. Treat the addr as the port.
		port = strings.TrimPrefix(cfg.ListenAddr, ":")
		host = ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
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
