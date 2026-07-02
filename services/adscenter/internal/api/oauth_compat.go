package api

import (
	"database/sql"
	"net/http"
	"os"
	"strings"

	"github.com/ScientificInternet/Google-Monetize/services/adscenter/internal/crypto"
)

// DecryptWithRotation attempts to decrypt a stored token, trying the current and
// previous token-encryption keys (supporting key rotation). Returns (plaintext, true)
// on success, or ("", false) if no key is configured or none can decrypt the value
// (in which case callers fall back to using the raw value).
//
// NOTE: server-side token custody is being phased out in favor of local per-user
// OAuth (token stays on the user's machine). This remains only to decrypt any
// legacy stored tokens during the transition.
func DecryptWithRotation(enc string) (string, bool) {
	if strings.TrimSpace(enc) == "" {
		return "", false
	}
	for _, env := range []string{"ADS_TOKEN_ENC_KEY", "ADS_TOKEN_ENC_KEY_PREV"} {
		key := strings.TrimSpace(os.Getenv(env))
		if key == "" {
			continue
		}
		if pt, err := crypto.Decrypt([]byte(key), enc); err == nil {
			return pt, true
		}
	}
	return "", false
}

// OAuthHandler handles the Google Ads OAuth endpoints.
//
// NOTE: this is a placeholder pending the OAuth redesign. The previous server-side
// callback flow (which stored user tokens in our database) is being replaced by a
// CC-style local authorization flow (localhost callback + PKCE, token stored on the
// user's own machine, zero server-side retention).
type OAuthHandler struct {
	db *sql.DB
}

// NewOAuthHandler creates an OAuthHandler.
func NewOAuthHandler(db *sql.DB) *OAuthHandler {
	return &OAuthHandler{db: db}
}

// HandleOAuthURL is a placeholder pending the local-OAuth redesign.
func (h *OAuthHandler) HandleOAuthURL(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "OAuth authorization is being redesigned (local/PKCE flow)", http.StatusNotImplemented)
}

// HandleOAuthCallback is a placeholder pending the local-OAuth redesign.
func (h *OAuthHandler) HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "OAuth authorization is being redesigned (local/PKCE flow)", http.StatusNotImplemented)
}
