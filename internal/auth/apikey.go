package auth

import (
	"crypto/subtle"
)

// ValidateAPIKey checks the bearer token against configured API keys using
// constant-time comparison. Returns the matching key and true, or zero-value and false.
func ValidateAPIKey(keys []APIKey, bearer string) (APIKey, bool) {
	if bearer == "" {
		return APIKey{}, false
	}
	for _, k := range keys {
		if subtle.ConstantTimeCompare([]byte(k.Key), []byte(bearer)) == 1 {
			return k, true
		}
	}
	return APIKey{}, false
}
