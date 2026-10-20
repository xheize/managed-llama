package api

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"managed-llama/internal/config"
	"managed-llama/internal/serviceauth"
)

func TestRemoteOpenAIPreflightAndAuthentication(t *testing.T) {
	key := strings.Repeat("ab", 32)
	wrap, err := serviceauth.RemoteAccess("llama.example", key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ModelsDir = t.TempDir()
	server := New(cfg, filepath.Join(t.TempDir(), "config.json"), embed.FS{}, nil)
	defer server.Shutdown()
	handler := wrap(server.Handler())

	for _, tc := range []struct {
		name, host, path, method, origin, requestedMethod, key string
		status                                                 int
		cors                                                   bool
	}{
		{"chat preflight", "llama.example", "/v1/chat/completions", "OPTIONS", "https://client.example", "POST", "", 204, true},
		{"models preflight", "llama.example", "/v1/models", "OPTIONS", "https://client.example", "GET", "", 204, true},
		{"root preflight", "llama.example", "/v1", "OPTIONS", "https://client.example", "POST", "", 204, true},
		{"foreign host", "attacker.example", "/v1/chat/completions", "OPTIONS", "https://client.example", "POST", "", 403, false},
		{"management preflight", "llama.example", "/api/config", "OPTIONS", "https://client.example", "PUT", "", 401, false},
		{"native preflight", "llama.example", "/llama/models/load", "OPTIONS", "https://client.example", "POST", "", 401, false},
		{"similar prefix", "llama.example", "/v10/models", "OPTIONS", "https://client.example", "GET", "", 401, false},
		{"missing origin", "llama.example", "/v1/models", "OPTIONS", "", "GET", "", 401, false},
		{"missing requested method", "llama.example", "/v1/models", "OPTIONS", "https://client.example", "", "", 401, false},
		{"missing key", "llama.example", "/v1/chat/completions", "POST", "https://client.example", "POST", "", 401, false},
		{"wrong key", "llama.example", "/v1/chat/completions", "POST", "https://client.example", "", "wrong", 401, false},
		// With no model running, an authenticated request reaches the proxy
		// and returns its normal unavailable response with CORS headers.
		{"valid key", "llama.example", "/v1/chat/completions", "POST", "https://client.example", "", key, 503, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "https://"+tc.host+tc.path, nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Access-Control-Request-Method", tc.requestedMethod)
			r.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
			if tc.key != "" {
				r.Header.Set("Authorization", "Bearer "+tc.key)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if got := w.Header().Get("Access-Control-Allow-Origin"); (got == "*") != tc.cors {
				t.Fatalf("allow origin=%q want CORS=%v", got, tc.cors)
			}
			if tc.status == http.StatusNoContent {
				if got := w.Header().Get("Access-Control-Allow-Headers"); got != "authorization,content-type" {
					t.Fatalf("allow headers=%q", got)
				}
				if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), tc.requestedMethod) {
					t.Fatalf("requested method %s not allowed", tc.requestedMethod)
				}
			}
			if tc.status == http.StatusUnauthorized && strings.HasPrefix(tc.path, "/v1/") &&
				!strings.Contains(w.Body.String(), "invalid_api_key") {
				t.Fatalf("missing OpenAI auth error: %s", w.Body.String())
			}
		})
	}
}
