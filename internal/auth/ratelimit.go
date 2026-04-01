package auth

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	rateLimitWindow  = 60 * time.Second
	rateLimitMax     = 30
	cleanupInterval  = 5 * time.Minute
)

type ipEntry struct {
	timestamps []time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
}

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{entries: make(map[string]*ipEntry)}
	go rl.cleanup()
	return rl
}

// allow checks if the IP is within the rate limit. Returns (allowed, retryAfter).
func (rl *rateLimiter) allow(ip string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)

	entry, ok := rl.entries[ip]
	if !ok {
		entry = &ipEntry{}
		rl.entries[ip] = entry
	}

	// Prune old timestamps.
	valid := entry.timestamps[:0]
	for _, t := range entry.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	entry.timestamps = valid

	if len(entry.timestamps) >= rateLimitMax {
		oldest := entry.timestamps[0]
		retryAfter := oldest.Add(rateLimitWindow).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return false, retryAfter
	}

	entry.timestamps = append(entry.timestamps, now)
	return true, 0
}

func (rl *rateLimiter) cleanup() {
	for {
		time.Sleep(cleanupInterval)
		rl.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-rateLimitWindow)
		for ip, entry := range rl.entries {
			valid := entry.timestamps[:0]
			for _, t := range entry.timestamps {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}
			entry.timestamps = valid
			if len(entry.timestamps) == 0 {
				delete(rl.entries, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// clientIP extracts the client IP, respecting proxy headers when behind a proxy.
func clientIP(r *http.Request, behindProxy bool) string {
	if behindProxy {
		for _, h := range []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"} {
			if v := r.Header.Get(h); v != "" {
				// X-Forwarded-For can be comma-separated; take the first.
				if h == "X-Forwarded-For" {
					parts := net.ParseIP(v)
					if parts == nil {
						// Try first entry
						for _, p := range splitCSV(v) {
							if ip := net.ParseIP(p); ip != nil {
								return ip.String()
							}
						}
					}
					return v
				}
				return v
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func splitCSV(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			v := trimSpace(s[start:i])
			if v != "" {
				result = append(result, v)
			}
			start = i + 1
		}
	}
	v := trimSpace(s[start:])
	if v != "" {
		result = append(result, v)
	}
	return result
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	j := len(s)
	for j > i && s[j-1] == ' ' {
		j--
	}
	return s[i:j]
}

// RateLimitMiddleware applies per-IP rate limiting to unauthenticated requests.
// It should be placed before the auth middleware.
func RateLimitMiddleware(behindProxy bool) func(http.Handler) http.Handler {
	rl := newRateLimiter()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, behindProxy)
			allowed, retryAfter := rl.allow(ip)
			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":       "too many requests",
					"retry_after": int(retryAfter.Seconds()),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
