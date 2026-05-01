package config

import "testing"

func TestLoadFromEnvDefaultsListenAddr(t *testing.T) {
	t.Setenv("ZOEA_LISTEN_ADDR", "")
	t.Setenv("ZOEA_LISTEN_PORT", "")

	cfg := LoadFromEnv()
	if cfg.ListenAddr != ":8080" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
}

func TestLoadFromEnvUsesListenPortAlias(t *testing.T) {
	t.Setenv("ZOEA_LISTEN_ADDR", "")
	t.Setenv("ZOEA_LISTEN_PORT", "9095")

	cfg := LoadFromEnv()
	if cfg.ListenAddr != ":9095" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9095")
	}
}

func TestLoadFromEnvListenAddrTakesPrecedence(t *testing.T) {
	t.Setenv("ZOEA_LISTEN_ADDR", "127.0.0.1:9191")
	t.Setenv("ZOEA_LISTEN_PORT", "9095")

	cfg := LoadFromEnv()
	if cfg.ListenAddr != "127.0.0.1:9191" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:9191")
	}
}
