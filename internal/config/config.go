package config

import (
	"os"
	"strings"

	"github.com/unbracketed/zoea-server/internal/auth"
)

type Config struct {
	ListenAddr        string
	PiBinPath         string
	PiArgs            []string
	SessionsBaseDir   string
	DefaultWorkingDir string
	Auth              auth.AuthConfig
	StoreDriver       string
	StoreDSN          string
}

func LoadFromEnv() Config {
	return Config{
		ListenAddr:        listenAddrFromEnv(),
		PiBinPath:         envOrDefault("PI_BIN_PATH", "pi"),
		PiArgs:            splitArgs(envOrDefault("PI_DEFAULT_ARGS", "--mode rpc")),
		SessionsBaseDir:   envOrDefault("ZOEA_PI_SESSION_DIR", "./.zoea-sessions"),
		DefaultWorkingDir: strings.TrimSpace(os.Getenv("ZOEA_WORKING_DIR")),
		StoreDriver:       envOrDefault("STORE_DRIVER", "sqlite"),
		StoreDSN:          envOrDefault("ZOEA_STORE_DSN", "./.zoea.db"),
		Auth: auth.AuthConfig{
			APIKeys:     auth.ParseAPIKeys(os.Getenv("AUTH_API_KEYS")),
			JWKSUrl:     os.Getenv("AUTH_JWKS_URL"),
			JWTIssuer:   os.Getenv("AUTH_JWT_ISSUER"),
			JWTAudience: os.Getenv("AUTH_JWT_AUDIENCE"),
			BehindProxy: os.Getenv("ZOEA_BEHIND_PROXY") != "",
		},
	}
}

func listenAddrFromEnv() string {
	if addr := os.Getenv("ZOEA_LISTEN_ADDR"); addr != "" {
		return addr
	}
	return ":7777"
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
		return []string{"--mode", "rpc"}
	}
	return fields
}
