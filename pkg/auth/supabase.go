package auth

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/ScientificInternet/Google-Monetize/pkg/supabaseauth"
)

// Re-export common error variables for backwards compatibility.
var (
	ErrSupabaseTokenInvalid = supabaseauth.ErrInvalidToken
	ErrSupabaseJWKSFetch    = supabaseauth.ErrJWKSFetch
)

// SupabaseVerifier is kept for compatibility with existing code. Prefer using supabaseauth.Verifier directly.
type SupabaseVerifier = supabaseauth.Verifier

// NewSupabaseVerifier constructs a verifier using the provided project URL.
func NewSupabaseVerifier(projectURL string) *SupabaseVerifier {
	return supabaseauth.NewVerifier(supabaseauth.WithProjectURL(projectURL))
}

var (
	globalVerifier     *SupabaseVerifier
	globalVerifierOnce sync.Once
)

// GetSupabaseVerifier returns a lazily initialised verifier singleton configured via environment variables.
func GetSupabaseVerifier() *SupabaseVerifier {
	globalVerifierOnce.Do(func() {
		globalVerifier = supabaseauth.DefaultVerifier()
	})
	return globalVerifier
}

// ExtractSupabaseUserID extracts the user ID from an HTTP request Authorization header.
func ExtractSupabaseUserID(ctx context.Context, r *http.Request) (string, error) {
	claims, err := ExtractSupabaseInfo(ctx, r)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}

// ExtractSupabaseInfo parses Supabase claims (user id/email/role) from the request Authorization header.
func ExtractSupabaseInfo(ctx context.Context, r *http.Request) (Info, error) {
	verifier := GetSupabaseVerifier()
	claims, err := verifier.VerifyRequest(ctx, r)
	if err != nil {
		return Info{}, err
	}
	return claims, nil
}

// ExtractSupabaseInfoFromToken verifies the provided bearer token string.
func ExtractSupabaseInfoFromToken(ctx context.Context, token string) (Info, error) {
	verifier := GetSupabaseVerifier()
	return verifier.Verify(ctx, token)
}

// ExtractSupabaseUserIDFromToken returns the subject from a JWT token string.
func ExtractSupabaseUserIDFromToken(ctx context.Context, token string) (string, error) {
	claims, err := ExtractSupabaseInfoFromToken(ctx, token)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}

// ParseSupabaseServiceKey inspects a service key and returns its project URL.
func ParseSupabaseServiceKey(serviceKey string) (string, error) {
	return supabaseauth.ParseProjectFromServiceKey(serviceKey)
}

// RewriteDatabaseURL rewrites the database name inside a postgres connection URL.
func RewriteDatabaseURL(originalURL, newDBName string) string {
	return supabaseauth.RewriteDatabaseURL(originalURL, newDBName)
}

// HasRole helper for backwards compatibility.
func HasRole(ctx context.Context, role string) bool {
	claims, ok := supabaseauth.ClaimsFromContext(ctx)
	if !ok {
		return false
	}
	return strings.EqualFold(claims.Role, role)
}
