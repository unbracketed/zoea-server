package auth

// HasScope returns true if the identity has the required scope or the "admin" scope.
func HasScope(identity AuthIdentity, required string) bool {
	for _, s := range identity.Scopes {
		if s == "admin" || s == required {
			return true
		}
	}
	return false
}
