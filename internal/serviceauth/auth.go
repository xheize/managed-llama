// Package serviceauth protects the privileged Windows service control plane.
package serviceauth

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

const Header = "X-Managed-Llama-Key"

// RequireLocalHost prevents alternate DNS names from reaching the local control
// plane, including browser requests made after DNS rebinding.
func RequireLocalHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || !(strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()) {
			http.Error(w, "invalid local gateway host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validKey(key string) bool {
	decoded, err := hex.DecodeString(key)
	return err == nil && len(decoded) == 32
}

// RequireKey protects all APIs, including native llama routes and inference.
// Only read-only static assets are public so the dashboard can show a login.
// Bearer authentication is compatible with OpenAI clients. The legacy custom
// header remains supported for existing clients with a separate upstream key.
func RequireKey(key string, next http.Handler) http.Handler {
	return RequireLocalHost(requireKey(key, next))
}

func requireKey(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset := r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/app.js" ||
			r.URL.Path == "/style.css" || r.URL.Path == "/autostart.css" || r.URL.Path == "/token.css" || r.URL.Path == "/downloads.css" ||
			strings.HasPrefix(r.URL.Path, "/favicon/")
		public := (r.Method == http.MethodGet || r.Method == http.MethodHead) && asset
		supplied := r.Header.Get(Header)
		bearer := false
		if supplied == "" {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				supplied, bearer = parts[1], true
			}
		}
		if !public && (!validKey(key) || subtle.ConstantTimeCompare([]byte(supplied), []byte(key)) != 1) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("WWW-Authenticate", `Bearer realm="managed-llama"`)
			w.WriteHeader(http.StatusUnauthorized)
			message := "유효한 API 키가 필요합니다. Authorization: Bearer <API 키>로 인증하세요."
			if r.URL.Path == "/v1" || strings.HasPrefix(r.URL.Path, "/v1/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message, "type": "authentication_error", "code": "invalid_api_key"}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
			}
			return
		}
		r = r.Clone(r.Context())
		r.Header.Del(Header)
		if bearer {
			r.Header.Del("Authorization")
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
