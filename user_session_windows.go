package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"managed-llama/internal/autostart"
	"managed-llama/internal/process"
	"managed-llama/internal/serviceauth"
)

// The interactive session owns UAC and HKCU. Its dashboard remains available
// while the backend transfers between this process and the Windows service.
type userSession struct {
	mu                 sync.Mutex
	configPath, listen string
	runtime            *gatewayRuntime
	elevate            func(string, string, string) error
	installed          func() (bool, error)
	loginSet           func(bool) error
	start              func(string, string, bool) (*gatewayRuntime, error)
	setupManaged       bool
	startupEnabled     func() (bool, error)
}

func runUserSession(configPath, listen string, installed bool) error {
	updateDone := make(chan struct{})
	defer close(updateDone)
	go watchUpdateShutdown(updateDone)
	login, err := autostart.New(configPath, listen)
	if err != nil {
		return err
	}
	session := &userSession{configPath: configPath, listen: listen, elevate: elevateService,
		installed: windowsServiceInstalled, loginSet: login.Set, start: startGateway}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	session.setupManaged = setupManagedExecutable(executable)
	session.startupEnabled = windowsServiceInstalled
	if session.setupManaged {
		session.startupEnabled = installedStartupEnabled
	}
	if !installed {
		runtime, err := startGateway(configPath, listen, false)
		if err != nil {
			return err
		}
		session.runtime = runtime
	}
	defer session.close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return listenError("127.0.0.1:0 (트레이 대시보드)", err)
	}
	target, _ := url.Parse(localDashboardURL(listen))
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		r.Host = target.Host
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", target.String())
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != listener.Addr().String() {
			http.Error(w, "invalid dashboard host", http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site request rejected", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host != r.Host {
				http.Error(w, "origin rejected", 403)
				return
			}
		}
		if r.URL.Path == "/api/autostart" {
			// An elevated tray must not become an unauthenticated UAC bypass.
			if windows.GetCurrentProcessToken().IsElevated() {
				key, err := serviceauth.ReadKey(configPath)
				if err != nil {
					http.Error(w, "service key unavailable", http.StatusServiceUnavailable)
					return
				}
				serviceauth.RequireKey(key, http.HandlerFunc(session.serveAutostart)).ServeHTTP(w, r)
				return
			}
			session.serveAutostart(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	// Always use the client so menu actions follow the currently active backend.
	client := newServiceClient(listen)
	client.key = func() (string, error) {
		installed, err := windowsServiceInstalled()
		if err != nil || (!installed && !windows.GetCurrentProcessToken().IsElevated()) {
			return "", err
		}
		if !windows.GetCurrentProcessToken().IsElevated() {
			return "", errServiceAuthentication
		}
		return serviceauth.ReadKey(configPath)
	}
	// Never inject the privileged key into browser proxy requests. The browser
	// must authenticate explicitly, even when this tray runs as administrator.
	runTray(client, listener.Addr().String(), func() {}, true)
	return nil
}

func (s *userSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime != nil {
		s.runtime.shutdown()
		s.runtime = nil
	}
}

func (s *userSession) serveAutostart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var err error
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err == nil {
			err = s.setStartup(input.Enabled)
		}
	default:
		w.WriteHeader(405)
		return
	}
	enabled, queryErr := s.startupEnabled()
	if err == nil {
		err = queryErr
	}
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

func (s *userSession) setStartup(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setupManaged {
		current, err := s.startupEnabled()
		if err != nil {
			return err
		}
		if current == enabled {
			return nil
		}
		action := "boot-disable"
		if enabled {
			action = "boot-enable"
		}
		return s.elevate(action, s.configPath, s.listen)
	}
	installed, err := s.installed()
	if err != nil {
		return err
	}
	if installed == enabled {
		return s.loginSet(enabled)
	}
	if enabled && !isLoopbackListen(s.listen) {
		return fmt.Errorf("자동 실행은 loopback 주소에서만 사용할 수 있습니다")
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	done := make(chan error, 1)
	go func() { done <- s.elevate(action, s.configPath, s.listen) }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil && !enabled {
				return err
			}
			// Restore a local gateway after disabling or a failed enable handoff.
			if !enabled || err != nil {
				if s.runtime == nil {
					recovered, recoveryErr := s.start(s.configPath, s.listen, false)
					if recoveryErr != nil {
						return fmt.Errorf("전환 후 게이트웨이 복구 실패: %w", recoveryErr)
					}
					s.runtime = recovered
				}
			}
			if err != nil {
				return err
			}
			return s.loginSet(enabled)
		case <-ticker.C:
			if enabled && s.runtime != nil {
				exists, _ := s.installed()
				if exists {
					s.runtime.shutdown()
					s.runtime = nil
				}
			}
		}
	}
}

func elevateService(action, configPath, listen string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	args := "-service " + action + " -config \"" + configPath + "\" -listen \"" + listen + "\""
	script := "$ErrorActionPreference='Stop'; $p=Start-Process -FilePath " + quote(executable) + " -ArgumentList " + quote(args) + " -Verb RunAs -WindowStyle Hidden -Wait -PassThru; exit $p.ExitCode"
	if output, err := process.HideConsole(exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)).CombinedOutput(); err != nil {
		return fmt.Errorf("관리자 승인이 취소되었거나 서비스 전환에 실패했습니다: %s (%w)", strings.TrimSpace(string(output)), err)
	}
	return nil
}
