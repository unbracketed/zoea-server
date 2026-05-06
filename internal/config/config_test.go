package config

import "testing"

func TestLoadFromEnvDefaultsListenAddr(t *testing.T) {
	t.Setenv("ZOEA_LISTEN_ADDR", "")

	cfg := LoadFromEnv()
	if cfg.ListenAddr != ":7777" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, ":7777")
	}
}

func TestLoadFromEnvListenAddrTakesPrecedence(t *testing.T) {
	t.Setenv("ZOEA_LISTEN_ADDR", "127.0.0.1:9191")

	cfg := LoadFromEnv()
	if cfg.ListenAddr != "127.0.0.1:9191" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:9191")
	}
}

func TestLoadFromEnvDefaultWorkingDir(t *testing.T) {
	t.Setenv("ZOEA_WORKING_DIR", " /tmp/project ")

	cfg := LoadFromEnv()
	if cfg.DefaultWorkingDir != "/tmp/project" {
		t.Fatalf("DefaultWorkingDir = %q, want %q", cfg.DefaultWorkingDir, "/tmp/project")
	}
}
