package serviceauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBearerAuthentication(t *testing.T) {
	key := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		auth   string
		status int
	}{
		{"Bearer " + key, 204}, {"bearer " + key, 204}, {"", 401}, {key, 401}, {"Basic " + key, 401}, {"Bearer wrong", 401}, {"Bearer " + key + " extra", 401},
	} {
		handler := RequireKey(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" || r.Header.Get(Header) != "" {
				t.Error("gateway credential leaked downstream")
			}
			w.WriteHeader(204)
		}))
		r := httptest.NewRequest("GET", "http://localhost:3030/v1/models", nil)
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("status %d want %d", w.Code, tc.status)
		}
		if tc.status == 401 {
			var body struct {
				Error struct{ Code, Type, Message string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != "invalid_api_key" {
				t.Errorf("invalid OpenAI error: %s", w.Body.String())
			}
			if w.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing Bearer challenge")
			}
		}
	}
}

func TestRemoteBearerAuthentication(t *testing.T) {
	key := strings.Repeat("ab", 32)
	wrap, err := RemoteAccess("llama.xheize.cc", key)
	if err != nil {
		t.Fatal(err)
	}
	h := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	r := httptest.NewRequest("GET", "https://llama.xheize.cc/v1/models", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("status %d", w.Code)
	}
}
