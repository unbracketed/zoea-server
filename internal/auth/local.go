package auth

import (
	"net"
	"net/http"
)

// proxyHeaders are HTTP headers that indicate the request was forwarded by a proxy.
var proxyHeaders = []string{
	"X-Forwarded-For",
	"X-Real-IP",
	"CF-Connecting-IP",
	"Forwarded",
}

// IsLocalConnection returns true only when ALL of these hold:
//  1. behindProxy is false
//  2. No proxy headers are present on the request
//  3. The TCP remote address is loopback (127.0.0.1 or ::1)
func IsLocalConnection(r *http.Request, behindProxy bool) bool {
	if behindProxy {
		return false
	}

	for _, h := range proxyHeaders {
		if r.Header.Get(h) != "" {
			return false
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// If we can't parse, try the raw value.
		host = r.RemoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
