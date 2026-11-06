package handlers

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/divs-lab/webhook-server/internal/jsonutil"
)

// RequireAuth returns HTTP middleware enforcing API key authorization.
// It bypasses the /health route so load balancers and orchestrators can check liveness without credentials.
func RequireAuth(apiKey string) func(http.Handler) http.Handler {
	expectedKeyBytes := []byte(apiKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				jsonutil.Error(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			token, found := strings.CutPrefix(auth, "Bearer ")
			if !found || strings.TrimSpace(token) == "" {
				jsonutil.Error(w, http.StatusUnauthorized, "invalid authorization header format")
				return
			}

			// ConstantTimeCompare takes the same duration regardless of matching bytes,
			// preventing attackers from discovering the key character-by-character via timing analysis.
			if subtle.ConstantTimeCompare([]byte(token), expectedKeyBytes) != 1 {
				jsonutil.Error(w, http.StatusUnauthorized, "invalid api key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
