package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/sys/windows/svc/mgr"
)

func TestRegisteredServiceCustomEndpointUsedByClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/state" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	exe := `C:\Program Files\ManagedLlama\managed-llama.exe`
	command := `"` + exe + `" -service run -config "C:\Program Files\ManagedLlama\custom.json" -listen ` + strings.TrimPrefix(server.URL, "http://")
	settings, err := parseServiceSettings(command, exe)
	if err != nil {
		t.Fatal(err)
	}
	if settings.configPath != `C:\Program Files\ManagedLlama\custom.json` {
		t.Fatal(settings.configPath)
	}
	client := newServiceClient(settings.listen)
	if err := client.request("GET", "/api/state", nil); err != nil {
		t.Fatal(err)
	}
}

func TestServiceSettingsRejectUnsafeAndMismatchedCommands(t *testing.T) {
	exe := `C:\Program Files\ManagedLlama\managed-llama.exe`
	prefix := `"` + exe + `" -service run -config "C:\Program Files\ManagedLlama\config.json"`
	for _, command := range []string{
		prefix + " -listen 0.0.0.0:3030",
		prefix + " -listen 127.0.0.1:0",
		prefix + " -listen 127.0.0.1:70000",
		prefix + " -unknown value",
		strings.Replace(prefix, exe, `C:\Other\managed-llama.exe`, 1),
		`"` + exe + `" -service run -config relative.json`,
	} {
		if _, err := parseServiceSettings(command, exe); err == nil {
			t.Errorf("accepted %s", command)
		}
	}
	settings, err := parseServiceSettings(prefix+" -listen=[::1]:4040", exe)
	if err != nil || settings.listen != "[::1]:4040" {
		t.Fatalf("IPv6: %+v %v", settings, err)
	}
}

func TestInstalledStartupToggleKeepsServiceBackend(t *testing.T) {
	for _, initial := range []bool{true, false} {
		enabled := initial
		session := &userSession{setupManaged: true,
			startupEnabled: func() (bool, error) { return enabled, nil },
			elevate: func(action, _, _ string) error {
				want := "boot-enable"
				if initial {
					want = "boot-disable"
				}
				if action != want {
					t.Fatalf("action = %s", action)
				}
				enabled = !initial
				return nil
			},
			installed: func() (bool, error) { t.Fatal("must not use registration as boot policy"); return true, nil },
			start: func(string, string, bool) (*gatewayRuntime, error) {
				t.Fatal("must not fall back to unprivileged gateway")
				return nil, nil
			},
			loginSet: func(bool) error { t.Fatal("must preserve Setup startup shortcut"); return nil },
		}
		if err := session.setStartup(!initial); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		session.serveAutostart(w, httptest.NewRequest("GET", "/api/autostart", nil))
		var state struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || state.Enabled != !initial {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestInstalledStartupUACCancellationPreservesPolicy(t *testing.T) {
	cancelled := errors.New("UAC cancelled")
	session := &userSession{setupManaged: true,
		startupEnabled: func() (bool, error) { return true, nil },
		elevate:        func(string, string, string) error { return cancelled },
	}
	if err := session.setStartup(false); !errors.Is(err, cancelled) {
		t.Fatal(err)
	}
}

func TestServiceBootPolicyPreservesCustomRegistration(t *testing.T) {
	original := mgr.Config{BinaryPathName: `"C:\Program Files\ManagedLlama\managed-llama.exe" -service run -listen 127.0.0.1:4040`,
		ServiceStartName: "LocalSystem", StartType: mgr.StartAutomatic, DelayedAutoStart: true,
		DisplayName: "Managed Llama", Description: "custom description"}
	manual := serviceBootConfig(original, false)
	if manual.StartType != mgr.StartManual || manual.DelayedAutoStart {
		t.Fatal(manual)
	}
	if manual.BinaryPathName != original.BinaryPathName || manual.ServiceStartName != original.ServiceStartName || manual.Description != original.Description {
		t.Fatal("registration changed")
	}
	auto := serviceBootConfig(manual, true)
	if auto.StartType != mgr.StartAutomatic || !auto.DelayedAutoStart {
		t.Fatal(auto)
	}
}
