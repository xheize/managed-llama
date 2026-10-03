package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"managed-llama/internal/autostart"
	"managed-llama/internal/config"
	"managed-llama/internal/serviceauth"
)

const (
	managedLlamaServiceName = "ManagedLlama"
	managedLlamaDisplayName = "Managed Llama"
)

type windowsService struct {
	configPath string
	listen     string
}

func isWindowsService() (bool, error) { return svc.IsWindowsService() }

func windowsServiceInstalled() (bool, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, fmt.Errorf("open Windows service manager: %w", err)
	}
	defer windows.CloseServiceHandle(manager)
	name, err := windows.UTF16PtrFromString(managedLlamaServiceName)
	if err != nil {
		return false, err
	}
	service, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query Managed Llama service: %w", err)
	}
	defer windows.CloseServiceHandle(service)
	return true, nil
}

func runWindowsService(configPath, listen string) error {
	if !isLoopbackListen(listen) {
		return errors.New("Windows 서비스는 인증되지 않은 관리 API 노출을 막기 위해 loopback 주소에서만 실행할 수 있습니다")
	}
	return svc.Run(managedLlamaServiceName, &windowsService{configPath: configPath, listen: listen})
}

func startServiceGateway(configPath, listen string) (*gatewayRuntime, error) {
	// The Windows service owns the management API only. llama-server is started
	// exclusively by an explicit API request handled by the gateway.
	if !isLoopbackListen(listen) {
		return nil, errors.New("service requires loopback listen")
	}
	if err := validateServiceInstallation(configPath); err != nil {
		return nil, err
	}
	key, err := serviceauth.EnsureKey(configPath)
	if err != nil {
		return nil, err
	}
	return startGatewayWithOptions(configPath, listen, false, gatewayOptions{
		policy: func(cfg config.Config) error { return serviceauth.ValidateConfig(configPath, cfg) },
		wrap:   func(next http.Handler) http.Handler { return serviceauth.RequireKey(key, next) },
	})
}

func validateServiceInstallation(configPath string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Dir(executable), filepath.Dir(configPath)) {
		return errors.New("service executable and config.json must be in the same protected installation directory")
	}
	if err := serviceauth.ValidatePath(filepath.Dir(configPath)); err != nil {
		return err
	}
	if err := serviceauth.ValidatePath(executable); err != nil {
		return err
	}
	if err := serviceauth.ValidatePath(configPath); err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	return serviceauth.ValidateConfig(configPath, cfg)
}

func (s *windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending, WaitHint: 15000}

	logger, err := eventlog.Open(managedLlamaServiceName)
	if err == nil {
		defer logger.Close()
		_ = logger.Info(1, "Managed Llama service starting")
	}
	runtime, err := startServiceGateway(s.configPath, s.listen)
	if err != nil {
		serviceError(logger, fmt.Sprintf("start gateway: %v", err))
		return true, 1
	}
	changes <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending, WaitHint: 15000}
				runtime.shutdown()
				serviceInfo(logger, "Managed Llama service stopped")
				return false, 0
			}
		case serverErr := <-runtime.errors:
			if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
				serviceError(logger, fmt.Sprintf("HTTP gateway stopped: %v", serverErr))
				runtime.shutdown()
				return true, 2
			}
		}
	}
}

func manageWindowsService(action, configPath, listen string) (string, error) {
	if action == "boot-enable" || action == "boot-disable" {
		return "서비스 부팅 자동 실행 설정을 변경했습니다.", setServiceBoot(action == "boot-enable", configPath, listen)
	}
	if action == "enable" {
		installed, err := windowsServiceInstalled()
		if err != nil {
			return "", err
		}
		if !installed {
			if _, err := manageWindowsService("install-machine", configPath, listen); err != nil {
				return "", err
			}
		}
		deadline := time.Now().Add(45 * time.Second)
		for {
			connection, err := net.DialTimeout("tcp", listen, 200*time.Millisecond)
			if err != nil {
				break
			}
			connection.Close()
			if time.Now().After(deadline) {
				if !installed {
					_, _ = manageWindowsService("uninstall-machine", configPath, listen)
				}
				return "", errors.New("사용자 게이트웨이가 종료되지 않았습니다")
			}
			time.Sleep(100 * time.Millisecond)
		}
		message, startErr := manageWindowsService("start", configPath, listen)
		if startErr != nil && !installed {
			_, _ = manageWindowsService("stop", configPath, listen)
			_, _ = manageWindowsService("uninstall-machine", configPath, listen)
		}
		return message, startErr
	}
	if action == "disable" {
		if _, err := manageWindowsService("stop", configPath, listen); err != nil {
			return "", err
		}
		return manageWindowsService("uninstall-machine", configPath, listen)
	}
	machineOnly := action == "install-machine" || action == "uninstall-machine"
	action = strings.TrimSuffix(action, "-machine")
	if action == "install" && !isLoopbackListen(listen) {
		return "", errors.New("Windows 서비스는 loopback listen 주소로만 등록할 수 있습니다")
	}
	manager, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("connect to Windows service manager (run as Administrator): %w", err)
	}
	defer manager.Disconnect()

	switch action {
	case "install":
		if err := validateServiceInstallation(configPath); err != nil {
			return "", fmt.Errorf("service installation requires an administrator-owned directory (see README): %w", err)
		}
		if _, err := serviceauth.EnsureKey(configPath); err != nil {
			return "", err
		}
		if info, err := os.Stat(configPath); err != nil || info.IsDir() {
			if err == nil {
				err = errors.New("path is a directory")
			}
			return "", fmt.Errorf("service config file is unavailable: %w", err)
		}
		executable, err := os.Executable()
		if err != nil {
			return "", err
		}
		executable, err = filepath.Abs(executable)
		if err != nil {
			return "", err
		}
		service, err := manager.CreateService(managedLlamaServiceName, executable, mgr.Config{
			DisplayName:      managedLlamaDisplayName,
			Description:      "Starts and manages the local llama.cpp server before user logon.",
			StartType:        mgr.StartAutomatic,
			ErrorControl:     mgr.ErrorNormal,
			DelayedAutoStart: true,
		}, "-service", "run", "-config", configPath, "-listen", listen)
		if err != nil {
			return "", fmt.Errorf("install service: %w", err)
		}
		defer service.Close()
		if err := service.SetRecoveryActions([]mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: time.Minute},
			{Type: mgr.ServiceRestart, Delay: 2 * time.Minute},
			{Type: mgr.ServiceRestart, Delay: 5 * time.Minute},
		}, 24*60*60); err != nil {
			_ = service.Delete()
			return "", fmt.Errorf("configure service recovery: %w", err)
		}
		if err := eventlog.InstallAsEventCreate(managedLlamaServiceName, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
			_ = service.Delete()
			return "", fmt.Errorf("install service event source: %w", err)
		}
		if machineOnly {
			return "서비스 등록 완료", nil
		}
		loginStartup, err := autostart.New(configPath, listen)
		if err != nil {
			_ = eventlog.Remove(managedLlamaServiceName)
			_ = service.Delete()
			return "", fmt.Errorf("prepare login tray startup: %w", err)
		}
		if err := loginStartup.Set(true); err != nil {
			_ = eventlog.Remove(managedLlamaServiceName)
			_ = service.Delete()
			return "", fmt.Errorf("register login tray startup: %w", err)
		}
		return "Managed Llama 서비스를 등록했습니다. 서비스는 부팅 시, 트레이는 로그인 시 자동으로 시작됩니다.", nil
	case "uninstall":
		service, err := manager.OpenService(managedLlamaServiceName)
		if err != nil {
			return "", fmt.Errorf("open service: %w", err)
		}
		defer service.Close()
		status, err := service.Query()
		if err != nil {
			return "", err
		}
		if status.State != svc.Stopped {
			return "", errors.New("서비스가 실행 중입니다. 먼저 -service stop을 실행하세요")
		}
		if err := service.Delete(); err != nil {
			return "", fmt.Errorf("delete service: %w", err)
		}
		_ = eventlog.Remove(managedLlamaServiceName)
		if machineOnly {
			return "서비스 제거 완료", nil
		}
		loginStartup, startupErr := autostart.New(configPath, listen)
		if startupErr == nil {
			startupErr = loginStartup.Set(false)
		}
		if startupErr != nil {
			return "", fmt.Errorf("서비스는 제거했지만 로그인 트레이 시작 항목을 제거하지 못했습니다: %w", startupErr)
		}
		return "Managed Llama 서비스와 로그인 트레이 시작 항목을 제거했습니다.", nil
	case "start":
		service, err := manager.OpenService(managedLlamaServiceName)
		if err != nil {
			return "", fmt.Errorf("open service: %w", err)
		}
		defer service.Close()
		status, err := service.Query()
		if err != nil {
			return "", err
		}
		if status.State != svc.Running && status.State != svc.StartPending {
			if err := service.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
				return "", fmt.Errorf("start service: %w", err)
			}
		}
		if err := waitForServiceState(service, svc.Running, 20*time.Second); err != nil {
			return "", err
		}
		return "Managed Llama 서비스가 실행 중입니다.", nil
	case "stop":
		service, err := manager.OpenService(managedLlamaServiceName)
		if err != nil {
			return "", fmt.Errorf("open service: %w", err)
		}
		defer service.Close()
		status, err := service.Query()
		if err != nil {
			return "", err
		}
		if status.State == svc.Stopped {
			return "Managed Llama 서비스가 이미 중지되어 있습니다.", nil
		}
		if _, err := service.Control(svc.Stop); err != nil {
			return "", fmt.Errorf("stop service: %w", err)
		}
		if err := waitForServiceState(service, svc.Stopped, 20*time.Second); err != nil {
			return "", err
		}
		return "Managed Llama 서비스를 중지했습니다.", nil
	case "status":
		service, err := manager.OpenService(managedLlamaServiceName)
		if err != nil {
			return "", fmt.Errorf("open service: %w", err)
		}
		defer service.Close()
		status, err := service.Query()
		if err != nil {
			return "", err
		}
		return "Managed Llama 서비스 상태: " + serviceStateLabel(status.State), nil
	default:
		return "", fmt.Errorf("unknown service action %q (use install, uninstall, start, stop, or status)", action)
	}
}

func waitForServiceState(service *mgr.Service, target svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == target {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service did not reach %s within %s", serviceStateLabel(target), timeout)
}

func serviceStateLabel(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start pending"
	case svc.StopPending:
		return "stop pending"
	case svc.Running:
		return "running"
	case svc.Paused:
		return "paused"
	default:
		return fmt.Sprintf("unknown (%d)", state)
	}
}

func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func serviceInfo(logger *eventlog.Log, message string) {
	log.Print(message)
	if logger != nil {
		_ = logger.Info(1, message)
	}
}

func serviceError(logger *eventlog.Log, message string) {
	log.Print(message)
	if logger != nil {
		_ = logger.Error(1, message)
	}
}
