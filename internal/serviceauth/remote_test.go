package serviceauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteAccessHostAndAuthentication(t *testing.T) {
	key := strings.Repeat("ab", 32)
	wrap, err := RemoteAccess("llama.xheize.cc,192.168.50.10:3030", key)
	if err != nil {
		t.Fatal(err)
	}
	h := wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(Header) != "" {
			t.Error("key leaked downstream")
		}
		w.WriteHeader(204)
	}))
	for _, tc := range []struct {
		host, path, key string
		status          int
	}{
		{"llama.xheize.cc", "/", "", 204},
		{"llama.xheize.cc", "/api/config", "", 401},
		{"llama.xheize.cc", "/api/config", "wrong", 401},
		{"llama.xheize.cc", "/api/config", key, 204},
		{"192.168.50.10:3030", "/v1/models", key, 204},
		{"127.0.0.1:3030", "/api/config", "", 401},
		{"localhost:3030", "/api/config", key, 204},
		{"attacker.example", "/", key, 403},
		{"llama.xheize.cc.attacker.example", "/api/config", key, 403},
	} {
		r := httptest.NewRequest("GET", "http://"+tc.host+tc.path, nil)
		r.Header.Set(Header, tc.key)
		r.Header.Set("X-Forwarded-Host", "llama.xheize.cc")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s%s: got %d want %d", tc.host, tc.path, w.Code, tc.status)
		}
	}
}

func TestRemoteAccessRequiresExplicitConfiguration(t *testing.T) {
	for _, tc := range []struct{ hosts, key string }{
		{"", strings.Repeat("a", 64)}, {"*", strings.Repeat("a", 64)},
		{"https://llama.xheize.cc", strings.Repeat("a", 64)}, {"llama.xheize.cc", ""},
	} {
		if _, err := RemoteAccess(tc.hosts, tc.key); err == nil {
			t.Errorf("accepted invalid configuration %q", tc.hosts)
		}
	}
}
