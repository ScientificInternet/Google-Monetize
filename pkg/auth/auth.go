package auth

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/ScientificInternet/Google-Monetize/pkg/supabaseauth"
)

var ErrUnauthenticated = errors.New("unauthenticated")

// ExtractUserID tries to infer the authenticated user id from standard headers in this order:
//  1. X-User-Id (explicit forward from trusted layer)
//  2. X-Endpoint-API-UserInfo (GCP API Gateway with Firebase) – base64 JSON, use `sub` or `id` or `email`
//  3. Authorization: Bearer <jwt> – verify RS256 with INTERNAL_JWT_PUBLIC_KEY; if not provided and
//     ALLOW_INSECURE_INTERNAL_JWT=true, parse without verification (dev-only)
func ExtractUserID(r *http.Request) (string, error) {
	if uid, err := ExtractSupabaseUserID(r.Context(), r); err == nil && strings.TrimSpace(uid) != "" {
		return uid, nil
	}
	// 1) Explicit header
	if uid := r.Header.Get("X-User-Id"); uid != "" {
		return uid, nil
	}

	// 2) GCP API Gateway user info header (ESP/Endpoints style)
	if ui := r.Header.Get("X-Endpoint-API-UserInfo"); ui != "" {
		if uid := parseUserInfo(ui); uid != "" {
			return uid, nil
		}
	}
	// 2b) API Gateway alt header (some deployments)
	if ui := r.Header.Get("X-Apigateway-Api-UserInfo"); ui != "" {
		if uid := parseUserInfo(ui); uid != "" {
			return uid, nil
		}
	}
	if ui := r.Header.Get("X-Apigateway-Api-Userinfo"); ui != "" { // case variance
		if uid := parseUserInfo(ui); uid != "" {
			return uid, nil
		}
	}
	// 2c) X-Goog-Authenticated-User-Id (format: issuer:uid)
	if xid := r.Header.Get("X-Goog-Authenticated-User-Id"); xid != "" {
		// typical format: accounts.google.com:1234567890 or firebase:uid
		if p := strings.SplitN(xid, ":", 2); len(p) == 2 && strings.TrimSpace(p[1]) != "" {
			return strings.TrimSpace(p[1]), nil
		}
	}

	// 3) JWT from Authorization
	if authz := r.Header.Get("Authorization"); authz != "" {
		parts := strings.SplitN(authz, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token := parts[1]
			// 3a) Try Firebase ID Token (validate iss/aud only; signature validation delegated to API Gateway)
			if uid := extractFirebaseUser(token); uid != "" {
				return uid, nil
			}
			// 3b) Try internal JWT (optional signature verification)
			if uid := extractUserFromJWT(token); uid != "" {
				return uid, nil
			}
		}
	}

	return "", ErrUnauthenticated
}

func parseUserInfo(b64 string) string {
	// X-Endpoint-API-UserInfo is base64-encoded JSON
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// also try URL encoding variant (no padding)
		data, err = base64.RawURLEncoding.DecodeString(b64)
		if err != nil {
			return ""
		}
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	// prefer `sub`, fallback `id` or `email`
	if v, ok := m["sub"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["id"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["email"].(string); ok && v != "" {
		return v
	}
	return ""
}

// Info reflects identity attributes extracted from Supabase JWTs.
type Info = supabaseauth.Claims

// ExtractInfo returns best-effort user id and email from standard headers.
// Order:
// 1) X-Endpoint-API-UserInfo (preferred: sub + email)
// 2) Authorization: Bearer <jwt> (RS256 verify when key provided; supports email claim)
// 3) X-User-Id (only user id; email may be empty)
func ExtractInfo(r *http.Request) (Info, error) {
	var out Info
	if info, err := ExtractSupabaseInfo(r.Context(), r); err == nil {
		return info, nil
	}
	// 1) GCP API Gateway user info header (contains both sub/email)
	if ui := r.Header.Get("X-Endpoint-API-UserInfo"); ui != "" {
		if id, email := parseUserInfoFull(ui); id != "" {
			out.UserID, out.Email = id, email
			return out, nil
		}
	}
	// 2) Authorization: Bearer
	if authz := r.Header.Get("Authorization"); authz != "" {
		parts := strings.SplitN(authz, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			// 2a) Firebase ID Token (iss/aud check)
			if sub := extractFirebaseUser(parts[1]); sub != "" {
				// Email may also be present; parse it best-effort
				_, email := extractFromJWT(parts[1])
				if email == "" {
					email = r.Header.Get("X-User-Email")
				}
				return Info{UserID: sub, Email: email}, nil
			}
			// 2b) Internal JWT (optional signature verification)
			sub, email := extractFromJWT(parts[1])
			if sub != "" {
				out.UserID, out.Email = sub, email
				return out, nil
			}
		}
	}
	// 3) Fallback explicit header
	if uid := r.Header.Get("X-User-Id"); uid != "" {
		out.UserID = uid
		return out, nil
	}
	return out, ErrUnauthenticated
}

func parseUserInfoFull(b64 string) (sub, email string) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(b64)
		if err != nil {
			return "", ""
		}
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return "", ""
	}
	if v, ok := m["sub"].(string); ok && v != "" {
		sub = v
	}
	if v, ok := m["email"].(string); ok && v != "" {
		email = v
	}
	if sub == "" {
		if v, ok := m["id"].(string); ok && v != "" {
			sub = v
		}
	}
	if sub == "" {
		if v, ok := m["user_id"].(string); ok && v != "" {
			sub = v
		}
	}
	return
}

func extractFromJWT(token string) (sub, email string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	_, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	payloadB, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	sigB, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return "", ""
	}
	// verify if public key provided
	if pub := os.Getenv("INTERNAL_JWT_PUBLIC_KEY"); pub != "" {
		if !verifyRS256([]byte(parts[0]+"."+parts[1]), sigB, pub) {
			return "", ""
		}
	} else if strings.ToLower(os.Getenv("ALLOW_INSECURE_INTERNAL_JWT")) != "true" {
		return "", ""
	}
	var claims map[string]any
	if json.Unmarshal(payloadB, &claims) != nil {
		return "", ""
	}
	if v, ok := claims["sub"].(string); ok && v != "" {
		sub = v
	}
	if v, ok := claims["email"].(string); ok && v != "" {
		email = v
	}
	return
}

// extractFirebaseUser parses a Firebase ID token payload without verifying the signature
// and returns the uid when iss/aud are consistent with the configured project id.
// This is safe when the API Gateway has already verified the token and the service
// only needs to recover the uid downstream. For local/dev, it also works without Gateway.
func extractFirebaseUser(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payloadB, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(payloadB, &claims) != nil {
		return ""
	}
	// Determine expected project id from environment
	proj := os.Getenv("NEXT_PUBLIC_FIREBASE_PROJECT_ID")
	if proj == "" {
		proj = os.Getenv("FIREBASE_PROJECT_ID")
	}
	if proj == "" {
		proj = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if proj == "" {
		return ""
	}
	iss := asString(claims["iss"]) // https://securetoken.google.com/<project>
	aud := asString(claims["aud"]) // <project>
	// Basic checks
	if !strings.HasSuffix(iss, "/"+proj) {
		return ""
	}
	if aud != proj {
		return ""
	}
	// uid
	if uid := asString(claims["user_id"]); uid != "" {
		return uid
	}
	if uid := asString(claims["sub"]); uid != "" {
		return uid
	}
	return ""
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func extractUserFromJWT(token string) string {
	// Expect JWT: header.payload.signature (base64url)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	_, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	payloadB, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	sigB, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return ""
	}
	// Verify if public key is provided
	if pub := os.Getenv("INTERNAL_JWT_PUBLIC_KEY"); pub != "" {
		if !verifyRS256([]byte(parts[0]+"."+parts[1]), sigB, pub) {
			return ""
		}
	} else if strings.ToLower(os.Getenv("ALLOW_INSECURE_INTERNAL_JWT")) != "true" {
		// Disallow unverified parsing by default
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(payloadB, &claims) != nil {
		return ""
	}
	if sub, ok := claims["sub"].(string); ok && sub != "" {
		return sub
	}
	return ""
}

func verifyRS256(signingInput, signature []byte, pubPEM string) bool {
	block, _ := pem.Decode([]byte(pubPEM))
	if block == nil {
		return false
	}
	pubIfc, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false
	}
	pub, ok := pubIfc.(*rsa.PublicKey)
	if !ok {
		return false
	}
	h := sha256.Sum256(signingInput)
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], signature) == nil
}
