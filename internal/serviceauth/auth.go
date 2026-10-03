// Package serviceauth protects the privileged Windows service control plane.
package serviceauth

import (
	"crypto/subtle"
	"encoding/hex"
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
// The key is distinct from llama's Authorization and is never forwarded to it.
func RequireKey(key string, next http.Handler) http.Handler {
	return RequireLocalHost(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset := r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/app.js" ||
			r.URL.Path == "/style.css" || r.URL.Path == "/autostart.css" || r.URL.Path == "/token.css" || r.URL.Path == "/downloads.css" ||
			strings.HasPrefix(r.URL.Path, "/favicon/")
		public := (r.Method == http.MethodGet || r.Method == http.MethodHead) && asset
		if !public && (!validKey(key) || subtle.ConstantTimeCompare([]byte(r.Header.Get(Header)), []byte(key)) != 1) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"서비스 관리 키가 필요합니다. 관리자 권한으로 config.json.service-key 파일을 확인하세요."}`))
			return
		}
		r = r.Clone(r.Context())
		r.Header.Del(Header)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	}))
}
