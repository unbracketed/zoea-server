package auth

import (
	"net/http"
	"testing"
)

func makeRequest(remoteAddr string, headers map[string]string) *http.Request {
	r, _ := http.NewRequest("GET", "/test", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestLoopbackIPv4(t *testing.T) {
	r := makeRequest("127.0.0.1:12345", nil)
	if !IsLocalConnection(r, false) {
		t.Error("expected local connection for 127.0.0.1")
	}
}

func TestLoopbackIPv6(t *testing.T) {
	r := makeRequest("[::1]:12345", nil)
	if !IsLocalConnection(r, false) {
		t.Error("expected local connection for ::1")
	}
}

func TestNonLoopback(t *testing.T) {
	r := makeRequest("192.168.1.5:12345", nil)
	if IsLocalConnection(r, false) {
		t.Error("expected remote connection for 192.168.1.5")
	}
}

func TestXForwardedFor(t *testing.T) {
	r := makeRequest("127.0.0.1:12345", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if IsLocalConnection(r, false) {
		t.Error("expected remote when X-Forwarded-For is present")
	}
}

func TestXRealIP(t *testing.T) {
	r := makeRequest("127.0.0.1:12345", map[string]string{"X-Real-IP": "1.2.3.4"})
	if IsLocalConnection(r, false) {
		t.Error("expected remote when X-Real-IP is present")
	}
}

func TestCFConnectingIP(t *testing.T) {
	r := makeRequest("127.0.0.1:12345", map[string]string{"CF-Connecting-IP": "1.2.3.4"})
	if IsLocalConnection(r, false) {
		t.Error("expected remote when CF-Connecting-IP is present")
	}
}

func TestForwardedHeader(t *testing.T) {
	r := makeRequest("127.0.0.1:12345", map[string]string{"Forwarded": "for=1.2.3.4"})
	if IsLocalConnection(r, false) {
		t.Error("expected remote when Forwarded is present")
	}
}

func TestBehindProxyFlag(t *testing.T) {
	r := makeRequest("127.0.0.1:12345", nil)
	if IsLocalConnection(r, true) {
		t.Error("expected remote when behindProxy is true")
	}
}
