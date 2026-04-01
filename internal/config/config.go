package config

import (
	"os"
	"strings"
)

type Config struct {
	ListenAddr      string
	PiBinPath       string
	PiArgs          []string
	SessionsBaseDir string
}

func LoadFromEnv() Config {
	return Config{
		ListenAddr:      envOrDefault("GATEWAY_LISTEN_ADDR", ":8080"),
		PiBinPath:       envOrDefault("PI_BIN_PATH", "pi"),
		PiArgs:          splitArgs(envOrDefault("PI_DEFAULT_ARGS", "--mode rpc --no-session")),
		SessionsBaseDir: envOrDefault("SESSIONS_BASE_DIR", "./.gateway-sessions"),
	}
}

func envOrDefault(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func splitArgs(raw string) []string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return []string{"--mode", "rpc", "--no-session"}
	}
	return fields
}
