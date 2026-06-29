package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/gin-gonic/gin"
	"github.com/ScientificInternet/Google-Monetize/pkg/apierrors"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
	usercfg "github.com/ScientificInternet/Google-Monetize/services/user/internal/config"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/repositories"
	"github.com/ScientificInternet/Google-Monetize/services/user/internal/services"
	tokencrypto "github.com/ScientificInternet/Google-Monetize/services/user/internal/crypto"
)

// OAuthHandler handles OAuth-related endpoints
type OAuthHandler struct {
	userService *services.UserService
}

// NewOAuthHandler creates a new OAuth handler
func NewOAuthHandler(userService *services.UserService) *OAuthHandler {
	return &OAuthHandler{
		userService: userService,
	}
}

// HandleOAuthURL generates OAuth authorization URL
// GET /api/v1/users/auth/oauth/url
func (h *OAuthHandler) HandleOAuthURL(c *gin.Context) {
	uidRaw := c.Value(middleware.UserIDKey)
	uid, _ := uidRaw.(string)
	if uid == "" {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	cfg, err := usercfg.LoadAdsCreds(c.Request.Context())
	if err != nil {
		apiErr := apierrors.InternalError("Failed to load ads credentials")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	redirect := chooseRedirectURL(c.Request)
	if redirect == "" {
		apiErr := apierrors.InternalError("ADS_OAUTH_REDIRECT_URL(S) not set")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	oc := &oauth2.Config{
		ClientID:     cfg.OAuthClientID,
		ClientSecret: cfg.OAuthClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{"https://www.googleapis.com/auth/adwords"},
		RedirectURL:  redirect,
	}

	state := signState(uid)
	url := oc.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	_ = json.NewEncoder(c.Writer).Encode(map[string]string{"authUrl": url})
}

// HandleOAuthCallback processes OAuth callback
// GET /api/v1/users/auth/oauth/callback
func (h *OAuthHandler) HandleOAuthCallback(c *gin.Context) {
	ctx := c.Request.Context()
	code := c.Query("code")
	state := c.Query("state")
	loginCID := strings.TrimSpace(c.Query("login_customer_id"))

	if code == "" || state == "" {
		apiErr := apierrors.InvalidRequest("param", "Invalid callback params")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	uid, ok := verifyState(state)
	if !ok || uid == "" {
		apiErr := apierrors.InvalidRequest("param", "Invalid state param")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	creds, err := usercfg.LoadAdsCreds(ctx)
	if err != nil {
		apiErr := apierrors.InternalError("Failed to load ads credentials")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	redirect := chooseRedirectURL(c.Request)
	if redirect == "" {
		apiErr := apierrors.InternalError("ADS_OAUTH_REDIRECT_URL(S) not set")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	oc := &oauth2.Config{
		ClientID:     creds.OAuthClientID,
		ClientSecret: creds.OAuthClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{"https://www.googleapis.com/auth/adwords"},
		RedirectURL:  redirect,
	}

	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		apiErr := apierrors.InvalidRequest("code", "Exchange code failed")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	if tok.RefreshToken == "" {
		apiErr := apierrors.InvalidRequest("param", "No refresh token returned")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	// Encrypt and store refresh token
	keyB64 := strings.TrimSpace(os.Getenv("REFRESH_TOKEN_ENC_KEY_B64"))
	key, _ := base64.StdEncoding.DecodeString(keyB64)
	enc := tok.RefreshToken
	if len(key) == 32 {
		if crypted, err := tokencrypto.Encrypt(key, tok.RefreshToken); err == nil {
			enc = crypted
		}
	}

	// Store the refresh token using user service
	err = h.userService.StoreAdsRefreshToken(ctx, uid, loginCID, enc)
	if err != nil {
		apiErr := apierrors.InternalError("Failed to store refresh token")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	// Redirect back to frontend
	frontendURL := getFrontendURL(c.Request)
	if frontendURL == "" {
		frontendURL = "https://localhost:3000" // fallback for development
	}

	// Redirect to ads center page with success message
	redirectURL := frontendURL + "/adscenter?oauth=success"
	c.Redirect(http.StatusFound, redirectURL)
}

// HandleOAuthRevoke revokes OAuth tokens
// POST /api/v1/users/auth/oauth/revoke
func (h *OAuthHandler) HandleOAuthRevoke(c *gin.Context) {
	uidRaw := c.Value(middleware.UserIDKey)
	uid, _ := uidRaw.(string)
	if uid == "" {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	err := h.userService.RevokeAdsRefreshToken(c.Request.Context(), uid)
	if err != nil {
		apiErr := apierrors.InternalError("Failed to revoke refresh token")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	_ = json.NewEncoder(c.Writer).Encode(map[string]string{"status": "ok"})
}

// HandleOAuthTokens gets stored OAuth tokens for the user
// GET /api/v1/users/auth/oauth/tokens
func (h *OAuthHandler) HandleOAuthTokens(c *gin.Context) {
	uidRaw := c.Value(middleware.UserIDKey)
	uid, _ := uidRaw.(string)
	if uid == "" {
		apiErr := apierrors.Unauthorized("Unauthorized")
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	tokens, err := h.userService.GetAdsTokens(c.Request.Context(), uid)
	if err != nil {
		apiErr := apierrors.InternalError("Failed to get OAuth tokens")
		apiErr.Details = map[string]interface{}{"error": err.Error()}
		apiErr.WriteJSON(c.Writer, c.Request)
		return
	}

	_ = json.NewEncoder(c.Writer).Encode(tokens)
}

// --- Helper functions ---

// signState creates a signed state parameter for OAuth
func signState(uid string) string {
	secret := []byte(strings.TrimSpace(os.Getenv("OAUTH_STATE_SECRET")))
	if len(secret) == 0 {
		// Fallback to a default secret for development
		secret = []byte("dev-oauth-state-secret")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(uid))
	sig := mac.Sum(nil)
	return base64.URLEncoding.EncodeToString([]byte(uid + "." + base64.RawURLEncoding.EncodeToString(sig)))
}

// verifyState verifies of signed state parameter
func verifyState(state string) (string, bool) {
	b, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return "", false
	}

	parts := strings.Split(string(b), ".")
	if len(parts) != 2 {
		return "", false
	}

	uid := parts[0]
	sigRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}

	secret := []byte(strings.TrimSpace(os.Getenv("OAUTH_STATE_SECRET")))
	if len(secret) == 0 {
		secret = []byte("dev-oauth-state-secret")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(uid))

	if !hmac.Equal(mac.Sum(nil), sigRaw) {
		return "", false
	}

	return uid, true
}

// chooseRedirectURL selects a proper redirect URL based on request host and configured envs
func chooseRedirectURL(r *http.Request) string {
	if v := strings.TrimSpace(os.Getenv("ADS_OAUTH_REDIRECT_URL")); v != "" {
		return v
	}

	urlsEnv := strings.TrimSpace(os.Getenv("ADS_OAUTH_REDIRECT_URLS"))
	if urlsEnv == "" {
		return ""
	}

	// split by comma/newline
	var list []string
	for _, part := range strings.FieldsFunc(urlsEnv, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	}) {
		s := strings.TrimSpace(part)
		if s != "" {
			list = append(list, s)
		}
	}

	if len(list) == 0 {
		return ""
	}

	reqHost := r.Header.Get("X-Forwarded-Host")
	if reqHost == "" {
		reqHost = r.Host
	}

	// strict host match using url.Parse
	normalize := func(h string) string {
		if strings.HasPrefix(h, "www.") {
			return h[4:]
		}
		return h
	}

	reqHost = normalize(reqHost)
	for _, u := range list {
		if pu, err := neturl.Parse(u); err == nil {
			if normalize(pu.Host) == reqHost {
				return u
			}
		}
	}

	// fallback: substring contains
	for _, u := range list {
		if strings.Contains(u, reqHost) {
			return u
		}
	}

	return list[0]
}

// getFrontendURL constructs the frontend URL based on environment and request
func getFrontendURL(r *http.Request) string {
	// Check for explicit frontend URL configuration
	if frontendURL := strings.TrimSpace(os.Getenv("FRONTEND_URL")); frontendURL != "" {
		return frontendURL
	}

	// Derive from request host for development
	scheme := "https"
	if r.Header.Get("X-Forwarded-Proto") != "" {
		scheme = r.Header.Get("X-Forwarded-Proto")
	} else if r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	} else if r.TLS != nil {
		scheme = "https"
	} else {
		scheme = "http"
	}

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	return scheme + "://" + host
}