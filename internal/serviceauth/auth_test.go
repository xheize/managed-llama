package serviceauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrivilegedRoutesRequireKey(t *testing.T) {
	key := strings.Repeat("ab", 32)
	for _, route := range []string{"/api/config", "/api/server/start", "/api/resources/processes/123/terminate", "/v1", "/v1/models", "/v1/anything.css", "/llama/models/load", "/health/gateway"} {
		for _, method := range []string{"GET", "PUT", "POST", "DELETE", "OPTIONS"} {
			for _, supplied := range []string{"", "wrong", key} {
				t.Run(method+route+supplied[:min(2, len(supplied))], func(t *testing.T) {
					called := false
					handler := RequireKey(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						called = true
						if r.Header.Get(Header) != "" {
							t.Error("service key leaked to downstream")
						}
						if r.Header.Get("Authorization") != "Bearer inference-key" {
							t.Error("inference Authorization was changed")
						}
						w.WriteHeader(204)
					}))
					r := httptest.NewRequest(method, "http://127.0.0.1:3030"+route, nil)
					r.Header.Set(Header, supplied)
					r.Header.Set("Authorization", "Bearer inference-key")
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					if called != (supplied == key) {
						t.Fatalf("authentication bypass: status %d", w.Code)
					}
					if !called && w.Code != 401 {
						t.Fatalf("status = %d", w.Code)
					}
				})
			}
		}
	}
}

func TestServiceRejectsUntrustedHostAndInvalidServerKey(t *testing.T) {
	for _, test := range []struct{ host, key string }{{"attacker.example:3030", strings.Repeat("a", 64)}, {"127.0.0.1:3030", ""}} {
		h := RequireKey(test.key, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted request reached service") }))
		r := httptest.NewRequest("GET", "http://"+test.host+"/api/config", nil)
		r.Header.Set(Header, test.key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("status = %d", w.Code)
		}
	}
}

func TestLoginAssetsRemainAvailable(t *testing.T) {
	h := RequireKey(strings.Repeat("a", 64), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, path := range []string{"/", "/index.html", "/app.js", "/token.css"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:3030"+path, nil))
		if w.Code != 204 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}
