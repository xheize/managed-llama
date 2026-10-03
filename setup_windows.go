package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"managed-llama/internal/config"
	"managed-llama/internal/serviceauth"
)

// Explicit relative paths remain relative to the caller; the default never
// depends on a shortcut's working directory or the service manager's cwd.
func resolveConfigPath(value, executable string) (string, error) {
	if value == "" {
		value = filepath.Join(filepath.Dir(executable), "config.json")
	}
	return filepath.Abs(value)
}

func setupDefaults(root string) config.Config {
	cfg := config.Default()
	cfg.ServerPath = filepath.Join(root, "runtime", "llama-server.exe")
	cfg.ModelsDir = filepath.Join(root, "models")
	return cfg
}

func ensureSetupConfig(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return config.Save(path, setupDefaults(filepath.Dir(path)))
}

// Validate before copying anything into an existing privileged installation.
// Never repair permissions recursively on an arbitrary user-supplied directory.
func checkSetupDirectory(root string) error {
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return serviceauth.ValidatePath(filepath.Dir(root))
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return serviceauth.ValidatePath(path)
	})
}

// Exit 10 from stop means a running service was stopped and must be resumed
// if Setup is cancelled. Other successful operations return zero.
func runSetupAction(action, configPath, listen string) (int, error) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return 0, errors.New("installer operation requires administrator privileges")
	}
	if !filepath.IsAbs(configPath) || !strings.EqualFold(filepath.Base(configPath), "config.json") {
		return 0, errors.New("installer requires an absolute config.json path")
	}
	root := filepath.Dir(configPath)
	if err := checkSetupDirectory(root); err != nil {
		return 0, err
	}
	target := filepath.Join(root, "managed-llama.exe")
	service, closeService, err := serviceForUpdate(target)
	if err != nil {
		return 0, err
	}
	defer closeService()
	switch action {
	case "check":
		return 0, nil
	case "stop":
		if service == nil {
			return 0, nil
		}
		running, err := service.running()
		if err != nil || !running {
			return 0, err
		}
		if err := service.stop(); err != nil {
			return 0, err
		}
		return 10, nil
	case "resume":
		if service == nil {
			return 0, nil
		}
		running, err := service.running()
		if err != nil || running {
			return 0, err
		}
		return 0, service.start()
	case "remove":
		if service == nil {
			return 0, nil
		}
		if err := service.stop(); err != nil {
			return 0, err
		}
		_, err := manageWindowsService("uninstall-machine", configPath, listen)
		return 0, err
	case "install":
		executable, err := os.Executable()
		if err != nil {
			return 0, err
		}
		if !strings.EqualFold(executable, target) {
			return 0, errors.New("install must run from the installed executable")
		}
		if err := os.MkdirAll(filepath.Join(root, "models"), 0755); err != nil {
			return 0, err
		}
		if err := ensureSetupConfig(configPath); err != nil {
			return 0, err
		}
		if err := validateServiceInstallation(configPath); err != nil {
			return 0, err
		}
		if _, err := serviceauth.EnsureKey(configPath); err != nil {
			return 0, err
		}
		if service != nil {
			running, err := service.running()
			if err != nil || running {
				return 0, err
			}
			settings, err := readServiceSettings(target)
			if err != nil {
				return 0, err
			}
			if err := checkListenAvailable(settings.listen); err != nil {
				return 0, err
			}
			return 0, service.start()
		}
		if err := checkListenAvailable(listen); err != nil {
			return 0, err
		}
		if _, err := manageWindowsService("install-machine", configPath, listen); err != nil {
			return 0, err
		}
		if _, err := manageWindowsService("start", configPath, listen); err != nil {
			_, stopErr := manageWindowsService("stop", configPath, listen)
			_, removeErr := manageWindowsService("uninstall-machine", configPath, listen)
			return 0, errors.Join(err, stopErr, removeErr)
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("unknown installer operation %q", action)
	}
}
