package auth

import (
	"net/http"
	"testing"
)

func cfgNoAuth() *AuthConfig {
	return &AuthConfig{}
}

func cfgWithKeys() *AuthConfig {
	return &AuthConfig{
		APIKeys: []APIKey{
			{Name: "bridge", Key: "sk_abc123", Scopes: []string{"sessions.read", "sessions.write"}},
			{Name: "admin-key", Key: "sk_admin", Scopes: []string{"admin"}},
			{Name: "reader", Key: "sk_reader", Scopes: []string{"sessions.read"}},
		},
	}
}

func reqWithAuth(path, remoteAddr, bearer string) *http.Request {
	r, _ := http.NewRequest("GET", path, nil)
	r.RemoteAddr = remoteAddr
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	return r
}

func TestPublicPathAllowed(t *testing.T) {
	cfg := cfgWithKeys()
	r := reqWithAuth("/healthz", "1.2.3.4:1234", "")
	id, err := CheckAuth(cfg, r)
	if err != nil {
		t.Fatalf("expected allow for public path, got %v", err)
	}
	if id.Method != "anonymous" {
		t.Errorf("expected anonymous, got %s", id.Method)
	}
}

func TestLocalDevNoCredentials(t *testing.T) {
	cfg := cfgNoAuth()
	r := reqWithAuth("/v1/sessions", "127.0.0.1:1234", "")
	id, err := CheckAuth(cfg, r)
	if err != nil {
		t.Fatalf("expected allow for local dev, got %v", err)
	}
	if id.Method != "local-dev" {
		t.Errorf("expected local-dev, got %s", id.Method)
	}
	if !HasScope(id, "admin") {
		t.Error("expected admin scope for local-dev")
	}
}

func TestRemoteNoCredentials(t *testing.T) {
	cfg := cfgNoAuth()
	r := reqWithAuth("/v1/sessions", "192.168.1.5:1234", "")
	_, err := CheckAuth(cfg, r)
	if err == nil {
		t.Fatal("expected deny for remote with no credentials")
	}
}

func TestBehindProxyNoCredentials(t *testing.T) {
	cfg := cfgNoAuth()
	cfg.BehindProxy = true
	r := reqWithAuth("/v1/sessions", "127.0.0.1:1234", "")
	_, err := CheckAuth(cfg, r)
	if err == nil {
		t.Fatal("expected deny for behind-proxy with no credentials")
	}
}

func TestProxyHeadersForceRemote(t *testing.T) {
	cfg := cfgNoAuth()
	r := reqWithAuth("/v1/sessions", "127.0.0.1:1234", "")
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	_, err := CheckAuth(cfg, r)
	if err == nil {
		t.Fatal("expected deny when proxy headers present and no credentials")
	}
}

func TestValidAPIKey(t *testing.T) {
	cfg := cfgWithKeys()
	r := reqWithAuth("/v1/sessions", "1.2.3.4:1234", "sk_abc123")
	id, err := CheckAuth(cfg, r)
	if err != nil {
		t.Fatalf("expected allow for valid API key, got %v", err)
	}
	if id.Method != "api-key" {
		t.Errorf("expected api-key, got %s", id.Method)
	}
	if id.Subject != "bridge" {
		t.Errorf("expected subject bridge, got %s", id.Subject)
	}
	if !HasScope(id, "sessions.write") {
		t.Error("expected sessions.write scope")
	}
}

func TestInvalidAPIKey(t *testing.T) {
	cfg := cfgWithKeys()
	r := reqWithAuth("/v1/sessions", "1.2.3.4:1234", "sk_wrong")
	_, err := CheckAuth(cfg, r)
	if err == nil {
		t.Fatal("expected deny for invalid API key")
	}
}

func TestAPIKeyScopeInsufficient(t *testing.T) {
	cfg := cfgWithKeys()
	r := reqWithAuth("/v1/sessions", "1.2.3.4:1234", "sk_reader")
	id, err := CheckAuth(cfg, r)
	if err != nil {
		t.Fatalf("expected auth to pass, got %v", err)
	}
	if HasScope(id, "sessions.write") {
		t.Error("expected sessions.write to be denied for reader key")
	}
}

func TestAdminScopeCoversAll(t *testing.T) {
	cfg := cfgWithKeys()
	r := reqWithAuth("/v1/sessions", "1.2.3.4:1234", "sk_admin")
	id, err := CheckAuth(cfg, r)
	if err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	if !HasScope(id, "sessions.read") {
		t.Error("expected admin to cover sessions.read")
	}
	if !HasScope(id, "sessions.write") {
		t.Error("expected admin to cover sessions.write")
	}
}

func TestNoAuthHeader(t *testing.T) {
	cfg := cfgWithKeys()
	r := reqWithAuth("/v1/sessions", "1.2.3.4:1234", "")
	_, err := CheckAuth(cfg, r)
	if err == nil {
		t.Fatal("expected deny when no auth header and credentials configured")
	}
}

func TestWSTokenParam(t *testing.T) {
	cfg := cfgWithKeys()
	r, _ := http.NewRequest("GET", "/v1/sessions/s_000001/stream?token=sk_abc123", nil)
	r.RemoteAddr = "1.2.3.4:1234"
	id, err := CheckAuth(cfg, r)
	if err != nil {
		t.Fatalf("expected allow for WS token param, got %v", err)
	}
	if id.Method != "api-key" {
		t.Errorf("expected api-key, got %s", id.Method)
	}
}

func TestReadyzPublicPath(t *testing.T) {
	cfg := cfgWithKeys()
	r := reqWithAuth("/readyz", "1.2.3.4:1234", "")
	id, err := CheckAuth(cfg, r)
	if err != nil {
		t.Fatalf("expected allow for /readyz, got %v", err)
	}
	if id.Method != "anonymous" {
		t.Errorf("expected anonymous, got %s", id.Method)
	}
}
