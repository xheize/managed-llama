package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"managed-llama/internal/api"
	"managed-llama/internal/autostart"
	"managed-llama/internal/config"
	"managed-llama/internal/serviceauth"
)

//go:embed web/*
var web embed.FS

var version = "dev"

func main() {
	printVersion := flag.Bool("version", false, "print the Managed Llama version and exit")
	listen := flag.String("listen", "127.0.0.1:3030", "dashboard listen address")
	configPath := flag.String("config", "", "configuration file (default: beside the executable)")
	setupAction := flag.String("setup", "", "installer operation: check, stop, install, resume, or remove")
	setupErrorFile := flag.String("setup-error-file", "", "installer diagnostic output file")
	installedStartup := flag.Bool("installed-startup", false, "start the installed tray only when service boot startup is enabled")
	serviceAction := flag.String("service", "", "Windows service action: install, uninstall, start, stop, status, or run")
	updateTarget := flag.String("update", "", "replace an existing Managed Llama executable with this executable")
	webUpdate := flag.Bool("update-web", false, "internal: apply a dashboard update and save its result")
	restartAfterUpdate := flag.Bool("update-restart", false, "internal: restart the desktop app after updating")
	flag.Parse()
	if *printVersion {
		fmt.Println(version)
		return
	}
	if *setupAction != "" {
		if *updateTarget != "" || *serviceAction != "" || *configPath == "" || flag.NArg() != 0 {
			log.Fatal("-setup requires -config and cannot be combined with -update or -service")
		}
		code, err := runSetupAction(*setupAction, *configPath, *listen)
		if err != nil {
			log.Print(err)
			if *setupErrorFile != "" {
				if writeErr := writeSetupError(*setupErrorFile, err); writeErr != nil {
					log.Print(writeErr)
				}
			}
			os.Exit(1)
		}
		os.Exit(code)
	}
	if *updateTarget != "" {
		if *serviceAction != "" || flag.NArg() != 0 {
			log.Fatal("-update cannot be combined with -service or positional arguments")
		}
		var message string
		var err error
		if *webUpdate {
			message, err = applyWebUpdate(*updateTarget, *configPath, *listen, *restartAfterUpdate)
		} else {
			message, err = updateManagedLlama(*updateTarget)
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(message)
		return
	}
	if *webUpdate || *restartAfterUpdate {
		log.Fatal("update helper flags require -update")
	}
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	resolvedConfigPath, err := resolveConfigPath(*configPath, executable)
	if err != nil {
		log.Fatal(err)
	}
	if *serviceAction != "" && *serviceAction != "run" {
		message, err := manageWindowsService(*serviceAction, resolvedConfigPath, *listen)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(message)
		return
	}
	isService, err := isWindowsService()
	if err != nil {
		log.Fatal(err)
	}
	if *serviceAction == "run" || isService {
		if err := runWindowsService(resolvedConfigPath, *listen); err != nil {
			log.Fatal(err)
		}
		return
	}
	serviceInstalled, err := windowsServiceInstalled()
	if err != nil {
		failStartup(err, true)
	}
	if *installedStartup && !serviceInstalled {
		return
	}
	if serviceInstalled {
		settings, err := readServiceSettings(executable)
		if err != nil {
			failStartup(err, true)
		}
		if *installedStartup && !settings.automatic {
			return
		}
		resolvedConfigPath, *listen = settings.configPath, settings.listen
		if setupManagedExecutable(executable) && !settings.running {
			if err := checkListenAvailable(*listen); err != nil {
				failStartup(err, true)
			}
			if err := elevateService("start", resolvedConfigPath, *listen); err != nil {
				failStartup(err, true)
			}
		}
	} else if setupManagedExecutable(executable) {
		failStartup(errors.New("설치된 서비스가 없습니다. Setup을 다시 실행해 복구하세요."), true)
	}
	if err := runUserSession(resolvedConfigPath, *listen, serviceInstalled); err != nil {
		failStartup(err, true)
	}
}

type gatewayRuntime struct {
	dashboard *api.Server
	server    *http.Server
	errors    chan error
	stopOnce  sync.Once
}

type gatewayOptions struct {
	policy func(config.Config) error
	wrap   func(http.Handler) http.Handler
}

func startGateway(configPath, listen string, enableLoginAutostart bool) (*gatewayRuntime, error) {
	// Elevated desktop execution has the same privilege boundary as the service.
	if windows.GetCurrentProcessToken().IsElevated() {
		return startServiceGateway(configPath, listen)
	}
	return startGatewayWithOptions(configPath, listen, enableLoginAutostart, gatewayOptions{})
}

func startGatewayWithOptions(configPath, listen string, enableLoginAutostart bool, options gatewayOptions) (*gatewayRuntime, error) {
	if !isLoopbackListen(listen) {
		return nil, errors.New("관리 게이트웨이는 loopback 주소에서만 실행할 수 있습니다")
	}
	if err := os.Chdir(filepath.Dir(configPath)); err != nil {
		return nil, err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	cfg = config.ApplyEnv(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if options.policy != nil {
		if err := options.policy(cfg); err != nil {
			return nil, err
		}
	}
	var autostartManager api.AutostartController
	if enableLoginAutostart {
		autostartManager, err = autostart.New(configPath, listen)
		if err != nil {
			return nil, err
		}
	}
	dashboard := api.New(cfg, configPath, web, autostartManager)
	dashboard.Updates = newWebUpdater(configPath, listen)
	dashboard.ConfigPolicy = options.policy
	handler := serviceauth.RequireLocalHost(dashboard.Handler())
	if options.wrap != nil {
		handler = options.wrap(handler)
	}
	server := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	runtime := &gatewayRuntime{dashboard: dashboard, server: server, errors: make(chan error, 1)}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, listenError(listen, err)
	}
	go func() { runtime.errors <- server.Serve(listener) }()
	return runtime, nil
}

func (r *gatewayRuntime) shutdown() {
	r.stopOnce.Do(func() {
		if err := r.dashboard.Shutdown(); err != nil {
			log.Printf("stop llama-server during shutdown: %v", err)
		}
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.server.Shutdown(shutdownContext); err != nil {
			log.Printf("shutdown HTTP gateway: %v", err)
			_ = r.server.Close()
		}
		log.Print("Managed Llama shutdown complete")
	})
}

func runDesktop(configPath, listen string) error {
	runtime, err := startGateway(configPath, listen, true)
	if err != nil {
		return err
	}
	fmt.Printf("Managed Llama dashboard: %s\n", localDashboardURL(listen))

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	go func() {
		select {
		case err := <-runtime.errors:
			if !errors.Is(err, http.ErrServerClosed) {
				log.Printf("HTTP gateway stopped: %v", err)
			}
		case <-signalContext.Done():
			log.Print("shutdown signal received")
		}
		systrayQuit()
	}()

	runTray(runtime.dashboard, listen, runtime.shutdown, false)
	return nil
}

func runServiceTray(listen string) {
	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() {
		<-signalContext.Done()
		systrayQuit()
	}()
	runTray(newServiceClient(listen), listen, func() {}, true)
}
