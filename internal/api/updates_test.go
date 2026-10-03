package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"managed-llama/internal/config"
	"managed-llama/internal/serviceauth"
	"managed-llama/internal/updates"
)

type fakeUpdates struct{ installs int }

func (f *fakeUpdates) Status(repo string) updates.Status {
	return updates.Status{Current: "v1.0.0", Repository: repo, Phase: "idle"}
}
func (f *fakeUpdates) Check(_ context.Context, repo string) (updates.Status, error) {
	return f.Status(repo), nil
}
func (f *fakeUpdates) Install(repo, version string) error { f.installs++; return nil }

func TestUpdateControlBoundary(t *testing.T) {
	for _, name := range []string{"local", "remote", "rebound host", "cross origin", "missing service key", "authenticated service"} {
		t.Run(name, func(t *testing.T) {
			controller := &fakeUpdates{}
			s := &Server{cfg: config.Default(), Updates: controller}
			handler := sameOriginControl(http.HandlerFunc(s.installUpdate))
			key := strings.Repeat("a", 64)
			if strings.Contains(name, "service") {
				handler = serviceauth.RequireKey(key, handler)
			}
			r := httptest.NewRequest("POST", "http://127.0.0.1:3030/api/updates/install", strings.NewReader(`{"version":"v1.1.0"}`))
			r.RemoteAddr = "127.0.0.1:12000"
			switch name {
			case "remote":
				r.RemoteAddr = "192.168.1.2:12000"
			case "rebound host":
				r.Host = "evil.example:3030"
			case "cross origin":
				r.Header.Set("Origin", "https://evil.example")
			case "authenticated service":
				r.Header.Set(serviceauth.Header, key)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			allowed := name == "local" || name == "authenticated service"
			if (w.Code == 202) != allowed || (controller.installs == 1) != allowed {
				t.Fatalf("status=%d installs=%d", w.Code, controller.installs)
			}
		})
	}
}
