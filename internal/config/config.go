package config

import (
	"os"
	"strings"

	"github.com/brian/go-agent-gateway/internal/auth"
)

type Config struct {
	ListenAddr      string
	PiBinPath       string
	PiArgs          []string
	SessionsBaseDir string
	Auth            auth.AuthConfig
	StoreDriver     string
	StoreDSN        string
}

func LoadFromEnv() Config {
	return Config{
		ListenAddr:      envOrDefault("GATEWAY_LISTEN_ADDR", ":8080"),
		PiBinPath:       envOrDefault("PI_BIN_PATH", "pi"),
		PiArgs:          splitArgs(envOrDefault("PI_DEFAULT_ARGS", "--mode rpc --no-session")),
		SessionsBaseDir: envOrDefault("SESSIONS_BASE_DIR", "./.gateway-sessions"),
		StoreDriver:     envOrDefault("STORE_DRIVER", "sqlite"),
		StoreDSN:        envOrDefault("STORE_DSN", "./.gateway.db"),
		Auth: auth.AuthConfig{
			APIKeys:     auth.ParseAPIKeys(os.Getenv("AUTH_API_KEYS")),
			JWKSUrl:     os.Getenv("AUTH_JWKS_URL"),
			JWTIssuer:   os.Getenv("AUTH_JWT_ISSUER"),
			JWTAudience: os.Getenv("AUTH_JWT_AUDIENCE"),
			BehindProxy: os.Getenv("GATEWAY_BEHIND_PROXY") != "",
		},
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
