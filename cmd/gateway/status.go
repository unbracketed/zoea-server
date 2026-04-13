package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/brian/go-agent-gateway/internal/config"
	gatewaystore "github.com/brian/go-agent-gateway/internal/store"
)

func runStatus() {
	cfg := config.LoadFromEnv()
	ok := true

	fmt.Println("Gateway Status")
	fmt.Println("==============")
	fmt.Println()

	// --- Server ---
	fmt.Println("Server")
	fmt.Printf("  Listen address : %s\n", cfg.ListenAddr)

	host, port, _ := net.SplitHostPort(cfg.ListenAddr)
	if host == "" {
		host = "localhost"
	}
	healthURL := fmt.Sprintf("http://%s/healthz", net.JoinHostPort(host, port))

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		fmt.Printf("  Health         : ✗ unreachable (%s)\n", healthURL)
		ok = false
	} else {
		defer resp.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if resp.StatusCode == 200 && body["ok"] == true {
			fmt.Printf("  Health         : ✓ ok\n")
		} else {
			fmt.Printf("  Health         : ✗ unhealthy (status %d)\n", resp.StatusCode)
			ok = false
		}
	}

	// --- Auth ---
	fmt.Println()
	fmt.Println("Auth")
	if !cfg.Auth.IsEnabled() {
		fmt.Println("  Mode           : disabled (local-only access)")
	} else {
		if len(cfg.Auth.APIKeys) > 0 {
			fmt.Printf("  API keys       : %d configured\n", len(cfg.Auth.APIKeys))
		}
		if cfg.Auth.JWKSUrl != "" {
			fmt.Printf("  JWKS           : %s\n", cfg.Auth.JWKSUrl)
		}
	}
	if cfg.Auth.BehindProxy {
		fmt.Println("  Behind proxy   : yes")
	}

	// --- Database ---
	fmt.Println()
	fmt.Println("Database")
	fmt.Printf("  Driver         : %s\n", cfg.StoreDriver)

	dsn := cfg.StoreDSN
	absDSN, err := filepath.Abs(dsn)
	if err == nil && dsn != ":memory:" {
		dsn = absDSN
	}
	fmt.Printf("  Location       : %s\n", dsn)

	if dsn != ":memory:" {
		if info, err := os.Stat(dsn); err == nil {
			fmt.Printf("  File size      : %s\n", formatBytes(info.Size()))
		} else if os.IsNotExist(err) {
			fmt.Printf("  File           : not yet created (will be created on first run)\n")
		}
	}

	st, err := gatewaystore.Open(cfg.StoreDriver, cfg.StoreDSN)
	if err != nil {
		fmt.Printf("  Status         : ✗ cannot open (%v)\n", err)
		ok = false
	} else {
		defer st.Close()
		if initErr := st.Init(context.Background()); initErr != nil {
			fmt.Printf("  Status         : ✗ schema error (%v)\n", initErr)
			ok = false
		} else {
			count, _ := st.CountSessions(context.Background())
			if count > 0 {
				fmt.Printf("  Status         : ✓ ok (%d sessions)\n", count)
			} else {
				fmt.Printf("  Status         : ✓ ok (empty)\n")
			}
		}
	}

	// --- Pi binary ---
	fmt.Println()
	fmt.Println("Pi")
	fmt.Printf("  Binary         : %s\n", cfg.PiBinPath)

	piPath, err := exec.LookPath(cfg.PiBinPath)
	if err != nil {
		fmt.Printf("  Status         : ✗ not found in PATH\n")
		ok = false
	} else {
		fmt.Printf("  Path           : %s\n", piPath)
		fmt.Printf("  Status         : ✓ found\n")
	}
	fmt.Printf("  Default args   : %v\n", cfg.PiArgs)
	fmt.Printf("  Sessions dir   : %s\n", cfg.SessionsBaseDir)

	fmt.Println()
	if ok {
		fmt.Println("All checks passed ✓")
	} else {
		fmt.Println("Some checks failed ✗")
		os.Exit(1)
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
