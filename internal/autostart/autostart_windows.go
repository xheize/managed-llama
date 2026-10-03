package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "Managed Llama"
)

// Manager controls the current user's Windows login startup entry.
type Manager struct {
	command      string
	setupManaged bool
}

func New(configPath, listen string) (*Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	manager := newManager(executable, configPath, listen)
	_, markerErr := os.Stat(filepath.Join(filepath.Dir(executable), "installed-by-setup"))
	manager.setupManaged = markerErr == nil
	return manager, nil
}

func newManager(executable, configPath, listen string) *Manager {
	command := quoteWindowsArgument(executable) + " -config " + quoteWindowsArgument(configPath) + " -listen " + quoteWindowsArgument(listen)
	return &Manager{command: command}
}

func (m *Manager) Enabled() (bool, error) {
	if m.setupManaged {
		return true, nil
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open Windows startup registry: %w", err)
	}
	defer key.Close()
	value, _, err := key.GetStringValue(valueName)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Windows startup registry: %w", err)
	}
	return strings.TrimSpace(value) != "", nil
}

func (m *Manager) Set(enabled bool) error {
	// Setup owns the all-users startup shortcut. The entry point checks the
	// service's boot policy before launching; no duplicate HKCU entry is needed.
	if m.setupManaged {
		return nil
	}
	if enabled {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("open Windows startup registry: %w", err)
		}
		defer key.Close()
		if err := key.SetStringValue(valueName, m.command); err != nil {
			return fmt.Errorf("register Windows startup: %w", err)
		}
		return nil
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err == registry.ErrNotExist {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Windows startup registry: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(valueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("remove Windows startup: %w", err)
	}
	return nil
}

func quoteWindowsArgument(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
