package main

import (
	"errors"
	"managed-llama/internal/api"
	"managed-llama/internal/config"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnableReleasesGatewayBeforeServiceStarts(t *testing.T) {
	var installed atomic.Bool
	released := make(chan struct{})
	server := &http.Server{}
	server.RegisterOnShutdown(func() { close(released) })
	runtime := &gatewayRuntime{server: server, dashboard: api.New(config.Default(), "", web, nil)}
	loginRegistered := false
	session := &userSession{listen: "127.0.0.1:3030", runtime: runtime,
		installed: func() (bool, error) { return installed.Load(), nil },
		elevate: func(string, string, string) error {
			installed.Store(true)
			select {
			case <-released:
				return nil
			case <-time.After(3 * time.Second):
				return errors.New("gateway never released")
			}
		},
		loginSet: func(enabled bool) error { loginRegistered = enabled; return nil },
	}
	if err := session.setStartup(true); err != nil {
		t.Fatal(err)
	}
	if session.runtime != nil || !loginRegistered {
		t.Fatal("service handoff incomplete")
	}
}

func TestStartupCancellationKeepsUserGateway(t *testing.T) {
	runtime := &gatewayRuntime{}
	cancelled := errors.New("UAC cancelled")
	session := &userSession{listen: "127.0.0.1:3030", runtime: runtime,
		installed: func() (bool, error) { return false, nil },
		elevate: func(action, config, listen string) error {
			if action != "enable" {
				t.Errorf("action=%s", action)
			}
			return cancelled
		},
		loginSet: func(bool) error { t.Error("login entry changed after cancellation"); return nil },
	}
	if err := session.setStartup(true); !errors.Is(err, cancelled) {
		t.Fatalf("error=%v", err)
	}
	if session.runtime != runtime {
		t.Fatal("original gateway lost")
	}
}

func TestDisableCancellationKeepsServiceMode(t *testing.T) {
	cancelled := errors.New("UAC cancelled")
	session := &userSession{listen: "127.0.0.1:3030",
		installed: func() (bool, error) { return true, nil },
		elevate:   func(string, string, string) error { return cancelled },
		start: func(string, string, bool) (*gatewayRuntime, error) {
			t.Error("started conflicting local gateway")
			return nil, nil
		},
	}
	if err := session.setStartup(false); !errors.Is(err, cancelled) {
		t.Fatalf("error=%v", err)
	}
}

func TestDisableReturnsToUserGateway(t *testing.T) {
	runtime := &gatewayRuntime{}
	loginRemoved := false
	session := &userSession{listen: "127.0.0.1:3030",
		installed: func() (bool, error) { return true, nil },
		elevate: func(action, config, listen string) error {
			if action != "disable" {
				t.Errorf("action=%s", action)
			}
			return nil
		},
		start:    func(string, string, bool) (*gatewayRuntime, error) { return runtime, nil },
		loginSet: func(enabled bool) error { loginRemoved = !enabled; return nil },
	}
	if err := session.setStartup(false); err != nil {
		t.Fatal(err)
	}
	if session.runtime != runtime || !loginRemoved {
		t.Fatal("user mode or login cleanup missing")
	}
}

func TestStartupRejectsPublicListen(t *testing.T) {
	session := &userSession{listen: "0.0.0.0:3030", installed: func() (bool, error) { return false, nil }}
	if err := session.setStartup(true); err == nil {
		t.Fatal("public service permitted")
	}
}
