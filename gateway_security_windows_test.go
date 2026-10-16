package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayRejectsForeignHostBeforeRouting(t *testing.T) {
	t.Setenv("MANAGED_LLAMA_REMOTE_HOSTS", "")
	t.Setenv("MANAGED_LLAMA_REMOTE_KEY", "")
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	runtime, err := startGatewayWithOptions(filepath.Join(t.TempDir(), "config.json"), "127.0.0.1:0", false, gatewayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.shutdown()
	for _, host := range []string{"attacker.example:3030", "127.0.0.1.attacker.example:3030", "192.168.1.1:3030"} {
		for _, route := range []string{"/api/config", "/api/server/start", "/v1/models", "/llama/models/load", "/"} {
			for _, method := range []string{"GET", "PUT", "POST"} {
				r := httptest.NewRequest(method, "http://"+host+route, strings.NewReader(`{}`))
				r.Header.Set("Origin", "http://"+host)
				r.Header.Set("Sec-Fetch-Site", "same-origin")
				w := httptest.NewRecorder()
				runtime.server.Handler.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden {
					t.Fatalf("%s %s host %s: %d", method, route, host, w.Code)
				}
			}
		}
	}
	for _, host := range []string{"127.0.0.1:3030", "localhost:3030", "[::1]:3030"} {
		w := httptest.NewRecorder()
		runtime.server.Handler.ServeHTTP(w, httptest.NewRequest("GET", "http://"+host+"/api/config", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("local host %s: %d", host, w.Code)
		}
	}
}

func TestGatewayRefusesPublicBind(t *testing.T) {
	t.Setenv("MANAGED_LLAMA_REMOTE_HOSTS", "")
	t.Setenv("MANAGED_LLAMA_REMOTE_KEY", "")
	for _, listen := range []string{":3030", "0.0.0.0:3030", "[::]:3030", "192.168.1.1:3030"} {
		if _, err := startGatewayWithOptions("unused.json", listen, false, gatewayOptions{}); err == nil {
			t.Fatalf("accepted public listen address %q", listen)
		}
	}
}

func TestRemoteGatewayBindsAndEnforcesKeyAndOrigin(t *testing.T) {
	key := strings.Repeat("a", 64)
	t.Setenv("MANAGED_LLAMA_REMOTE_HOSTS", "llama.xheize.cc")
	t.Setenv("MANAGED_LLAMA_REMOTE_KEY", key)
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	runtime, err := startGatewayWithOptions(filepath.Join(t.TempDir(), "config.json"), "0.0.0.0:0", false, gatewayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.shutdown()
	for _, tc := range []struct {
		method, path, key, origin string
		status                    int
	}{
		{"GET", "/", "", "", 200},
		{"GET", "/health/gateway", "", "", 401},
		{"GET", "/health/gateway", key, "", 200},
		{"POST", "/api/server/start", key, "https://attacker.example", 403},
	} {
		r := httptest.NewRequest(tc.method, "https://llama.xheize.cc"+tc.path, nil)
		r.Header.Set("X-Managed-Llama-Key", tc.key)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		runtime.server.Handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
}
