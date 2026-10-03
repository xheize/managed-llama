package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"

	"managed-llama/internal/config"
	"managed-llama/internal/serviceauth"
	"net/http"
	"net/http/httptest"
	"strings"
)

func TestIsLoopbackListen(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:3030":   true,
		"[::1]:3030":       true,
		"localhost:3030":   true,
		":3030":            false,
		"0.0.0.0:3030":     false,
		"192.168.0.2:3030": false,
		"invalid":          false,
	}
	for address, want := range tests {
		if got := isLoopbackListen(address); got != want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", address, got, want)
		}
	}
}

func TestServiceStateLabel(t *testing.T) {
	if got := serviceStateLabel(svc.Running); got != "running" {
		t.Fatalf("running state label = %q", got)
	}
	if got := serviceStateLabel(svc.Stopped); got != "stopped" {
		t.Fatalf("stopped state label = %q", got)
	}
}

func TestServiceGatewayLeavesLlamaStoppedUntilAPIRequest(t *testing.T) {
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	temporaryDirectory := t.TempDir()
	configuration := config.Default()
	configuration.ServerPath = filepath.Join(temporaryDirectory, "llama-server-must-not-run.exe")
	configuration.ModelsDir = filepath.Join(temporaryDirectory, "models")
	configPath := filepath.Join(temporaryDirectory, "config.json")
	if err := config.Save(configPath, configuration); err != nil {
		t.Fatalf("save service configuration: %v", err)
	}

	// Test the production authenticated gateway without installing a privileged
	// service or weakening the ACL requirements for the test user's temp folder.
	runtime, err := startGatewayWithOptions(configPath, "127.0.0.1:0", false, gatewayOptions{
		wrap: func(next http.Handler) http.Handler { return serviceauth.RequireKey(strings.Repeat("a", 64), next) },
	})
	if err != nil {
		t.Fatalf("start service gateway: %v", err)
	}
	defer runtime.shutdown()
	request := httptest.NewRequest("POST", "http://127.0.0.1:3030/api/server/start", nil)
	recorder := httptest.NewRecorder()
	runtime.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated start status = %d", recorder.Code)
	}

	// Let the gateway listener settle without invoking the start API.
	time.Sleep(50 * time.Millisecond)
	status := runtime.dashboard.RuntimeStatus()
	if status.Running || status.Ready || status.PID != 0 {
		t.Fatalf("llama-server started without an API request: %+v", status)
	}
	if status.Command != "" || status.Endpoint != "" {
		t.Fatalf("idle runtime contains a launch command: %+v", status)
	}
}

func TestServiceRejectsUnprotectedInstallation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := startServiceGateway(path, "127.0.0.1:0"); err == nil {
		t.Fatal("service accepted a config outside its protected installation")
	}
}
