package auth

// ValidateJWT validates a JWT token against a JWKS endpoint.
// This is a placeholder — JWT support will be implemented when needed.
// Requires a JWKS library like github.com/MicahParks/keyfunc/v3.
func ValidateJWT(jwksURL, issuer, audience, bearer string) (AuthIdentity, error) {
	return AuthIdentity{}, ErrUnauthorized
}
