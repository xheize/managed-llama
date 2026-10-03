package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type serviceSettings struct {
	configPath, listen string
	automatic, running bool
}

func setupManagedExecutable(executable string) bool {
	info, err := os.Stat(filepath.Join(filepath.Dir(executable), "installed-by-setup"))
	return err == nil && !info.IsDir()
}

func parseServiceSettings(command, executable string) (serviceSettings, error) {
	var settings serviceSettings
	args, err := windows.DecomposeCommandLine(command)
	if err != nil || len(args) == 0 {
		return settings, errors.New("invalid service command")
	}
	if !strings.EqualFold(filepath.Clean(args[0]), filepath.Clean(executable)) {
		return settings, errors.New("registered service belongs to another executable")
	}
	flags := flag.NewFlagSet("service", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	action := flags.String("service", "", "")
	flags.StringVar(&settings.configPath, "config", "", "")
	flags.StringVar(&settings.listen, "listen", "127.0.0.1:3030", "")
	if err := flags.Parse(args[1:]); err != nil {
		return settings, err
	}
	if *action != "run" || flags.NArg() != 0 || !filepath.IsAbs(settings.configPath) ||
		!strings.EqualFold(filepath.Dir(settings.configPath), filepath.Dir(executable)) {
		return settings, errors.New("invalid registered service configuration path or action")
	}
	_, port, err := net.SplitHostPort(settings.listen)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 1 || n > 65535 || !isLoopbackListen(settings.listen) {
		return settings, errors.New("registered service must use a valid loopback endpoint")
	}
	return settings, nil
}

// Tray processes only need read access; mgr.OpenService requests ALL_ACCESS.
func readServiceSettings(executable string) (serviceSettings, error) {
	var settings serviceSettings
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return settings, err
	}
	defer windows.CloseServiceHandle(manager)
	name, err := windows.UTF16PtrFromString(managedLlamaServiceName)
	if err != nil {
		return settings, err
	}
	handle, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_CONFIG|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return settings, err
	}
	service := &mgr.Service{Name: managedLlamaServiceName, Handle: handle}
	defer service.Close()
	cfg, err := service.Config()
	if err != nil {
		return settings, err
	}
	settings, err = parseServiceSettings(cfg.BinaryPathName, executable)
	if err != nil {
		return settings, err
	}
	status, err := service.Query()
	if err != nil {
		return settings, err
	}
	settings.automatic = cfg.StartType == mgr.StartAutomatic
	settings.running = status.State == svc.Running || status.State == svc.StartPending
	return settings, nil
}

func installedStartupEnabled() (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	settings, err := readServiceSettings(executable)
	return settings.automatic, err
}

func serviceBootConfig(cfg mgr.Config, enabled bool) mgr.Config {
	cfg.StartType = mgr.StartManual
	cfg.DelayedAutoStart = false
	if enabled {
		cfg.StartType = mgr.StartAutomatic
		cfg.DelayedAutoStart = true
	}
	return cfg
}

func setServiceBoot(enabled bool, configPath, listen string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if !setupManagedExecutable(executable) {
		return errors.New("boot policy operation requires a Setup installation")
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(managedLlamaServiceName)
	if err != nil {
		return err
	}
	defer service.Close()
	cfg, err := service.Config()
	if err != nil {
		return err
	}
	settings, err := parseServiceSettings(cfg.BinaryPathName, executable)
	if err != nil {
		return err
	}
	if !strings.EqualFold(settings.configPath, configPath) || settings.listen != listen {
		return fmt.Errorf("service settings changed; reopen the tray before changing startup")
	}
	// Preserve ImagePath, account and all other settings. Never stop/delete the
	// service: it remains the owner of protected configuration and model writes.
	return service.UpdateConfig(serviceBootConfig(cfg, enabled))
}
