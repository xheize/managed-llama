package main

import (
	"managed-llama/internal/serviceauth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceClientControlsGateway(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/server/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"running":true,"ready":true,"pid":321}`))
		case "/api/state":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"phase":"running","can_start":false}`))
		case "/api/server/start", "/api/server/stop":
			requests <- request.Method + " " + request.URL.Path
			response.WriteHeader(http.StatusAccepted)
			_, _ = response.Write([]byte(`{"message":"ok"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := newServiceClientForURL(server.URL)
	if status := client.RuntimeStatus(); !status.Ready || status.PID != 321 {
		t.Fatalf("unexpected runtime status: %+v", status)
	}
	if state := client.CurrentState(); state.Phase != "running" {
		t.Fatalf("unexpected gateway state: %+v", state)
	}
	if err := client.StartServer(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	if err := client.StopServer(); err != nil {
		t.Fatalf("stop server: %v", err)
	}
	if got := <-requests; got != "POST /api/server/start" {
		t.Fatalf("first control request = %q", got)
	}
	if got := <-requests; got != "POST /api/server/stop" {
		t.Fatalf("second control request = %q", got)
	}
}

func TestServiceClientReportsUnavailableService(t *testing.T) {
	client := newServiceClientForURL("http://127.0.0.1:1")
	if status := client.RuntimeStatus(); status.LastError == "" {
		t.Fatal("runtime status did not report connection failure")
	}
	state := client.CurrentState()
	if state.Phase != "service_unavailable" || state.CanStart {
		t.Fatalf("unexpected unavailable state: %+v", state)
	}
}

func TestServiceClientReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusConflict)
		_, _ = response.Write([]byte(`{"error":"already running"}`))
	}))
	defer server.Close()

	err := newServiceClientForURL(server.URL).StartServer()
	if err == nil || err.Error() != "서비스 요청 실패: already running" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceClientSendsProtectedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(serviceauth.Header) != "test-service-key" {
			t.Error("missing service key")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client := newServiceClientForURL(server.URL)
	client.key = func() (string, error) { return "test-service-key", nil }
	if err := client.StartServer(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceClientDistinguishesAuthenticationFromOutage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newServiceClientForURL(server.URL)
	state := client.CurrentState()
	if state.Phase != "service_auth_required" || state.CanStart {
		t.Fatalf("unexpected authentication state: %+v", state)
	}
	client = newServiceClientForURL("http://127.0.0.1:1")
	client.key = func() (string, error) { return "", errServiceAuthentication }
	if state := client.CurrentState(); state.Phase != "service_auth_required" {
		t.Fatalf("key failure was treated as an outage: %+v", state)
	}
}
