package main

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"managed-llama/internal/serviceauth"
)

// The new executable is the updater, so neither a shell script nor an installed
// helper needs to execute after replacing the old executable.
func updateManagedLlama(target string) (string, error) {
	return updateManagedLlamaWithWait(target, 0)
}

func updateManagedLlamaWithWait(target string, wait time.Duration) (string, error) {
	source, err := os.Executable()
	if err != nil {
		return "", err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	old, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	current, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	if !old.Mode().IsRegular() || os.SameFile(old, current) {
		return "", errors.New("-update requires a different, existing Managed Llama executable; run the new executable from a separate directory")
	}
	if err := validateUpdateBinary(target); err != nil {
		return "", err
	}

	service, closeService, err := serviceForUpdate(target)
	if err != nil {
		return "", err
	}
	defer closeService()
	if service != nil || windows.GetCurrentProcessToken().IsElevated() {
		if err := serviceauth.ValidatePath(target); err != nil {
			return "", err
		}
	}
	// Serialize updates without overwriting or deleting a previous backup.
	lock, err := os.OpenFile(target+".update-lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("acquire update lock: %w", err)
	}
	defer os.Remove(lock.Name())
	defer lock.Close()
	staged, err := stageUpdate(source, filepath.Dir(target))
	if err != nil {
		return "", err
	}
	defer os.Remove(staged)
	if err := validateUpdateBinary(staged); err != nil {
		return "", err
	}
	if service != nil || windows.GetCurrentProcessToken().IsElevated() {
		if err := serviceauth.ValidatePath(staged); err != nil {
			return "", err
		}
	}
	backup, err := applyExecutableUpdateWithWait(target, staged, service, wait)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Managed Llama 업데이트 완료. 이전 실행 파일: %s\n트레이는 업데이트된 실행 파일로 다시 실행하세요.", backup), nil
}

func validateUpdateBinary(path string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("invalid Managed Llama executable %s: %w", path, err)
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if info.Path != "managed-llama" || settings["GOOS"] != "windows" || settings["GOARCH"] != runtime.GOARCH {
		return fmt.Errorf("%s must be a Windows Managed Llama executable for %s", path, runtime.GOARCH)
	}
	return nil
}

func stageUpdate(source, directory string) (string, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(directory, ".managed-llama-update-*.exe")
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

type updateService interface {
	running() (bool, error)
	stop() error
	start() error
}

type updateWindowsService struct{ service *mgr.Service }

func (s updateWindowsService) running() (bool, error) {
	status, err := s.service.Query()
	if err != nil {
		return false, err
	}
	if status.State != svc.Running && status.State != svc.Stopped {
		return false, fmt.Errorf("service is %s; retry when running or stopped", serviceStateLabel(status.State))
	}
	return status.State == svc.Running, nil
}
func (s updateWindowsService) stop() error {
	status, err := s.service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	if status.State != svc.StopPending {
		if _, err := s.service.Control(svc.Stop); err != nil {
			return err
		}
	}
	return waitForServiceState(s.service, svc.Stopped, 30*time.Second)
}
func (s updateWindowsService) start() error {
	if err := s.service.Start(); err != nil {
		return err
	}
	return waitForServiceState(s.service, svc.Running, 30*time.Second)
}

func serviceForUpdate(target string) (updateService, func(), error) {
	noop := func() {}
	installed, err := windowsServiceInstalled()
	if err != nil || !installed {
		return nil, noop, err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return nil, noop, fmt.Errorf("query installed service; run as Administrator: %w", err)
	}
	service, err := manager.OpenService(managedLlamaServiceName)
	if err != nil {
		manager.Disconnect()
		return nil, noop, err
	}
	closeService := func() { service.Close(); manager.Disconnect() }
	configuration, err := service.Config()
	if err != nil {
		closeService()
		return nil, noop, err
	}
	args, err := windows.DecomposeCommandLine(configuration.BinaryPathName)
	if err != nil || len(args) == 0 {
		closeService()
		return nil, noop, errors.New("cannot parse installed service executable path")
	}
	if !strings.EqualFold(filepath.Clean(args[0]), target) {
		closeService()
		return nil, noop, fmt.Errorf("installed service uses %s; use that path as -update target", args[0])
	}
	return updateWindowsService{service}, closeService, nil
}

func applyExecutableUpdate(target, staged string, service updateService) (string, error) {
	return applyExecutableUpdateWithWait(target, staged, service, 0)
}

func applyExecutableUpdateWithWait(target, staged string, service updateService, wait time.Duration) (string, error) {
	wasRunning := false
	if service != nil {
		var err error
		wasRunning, err = service.running()
		if err != nil {
			return "", err
		}
		if wasRunning {
			if err := service.stop(); err != nil {
				return "", fmt.Errorf("stop service before update: %w", err)
			}
		}
	}
	restart := func() error {
		if wasRunning {
			return service.start()
		}
		return nil
	}
	// Windows allows some running images to be renamed. Require write access
	// first so an old tray cannot silently keep running after a successful update.
	file, err := os.OpenFile(target, os.O_RDWR, 0)
	deadline := time.Now().Add(wait)
	for err != nil && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		file, err = os.OpenFile(target, os.O_RDWR, 0)
	}
	if err != nil {
		return "", errors.Join(fmt.Errorf("close all Managed Llama tray/app instances before updating: %w", err), restart())
	}
	file.Close()
	placeholder, err := os.CreateTemp(filepath.Dir(target), "managed-llama-backup-*.exe")
	if err != nil {
		return "", errors.Join(err, restart())
	}
	backup := placeholder.Name()
	placeholder.Close()
	if err := os.Rename(target, backup); err != nil {
		os.Remove(backup)
		return "", errors.Join(err, restart())
	}
	if err := os.Rename(staged, target); err != nil {
		restoreErr := os.Rename(backup, target)
		if restoreErr != nil {
			return "", fmt.Errorf("update failed: %v; restore failed: %v; recover executable from %s", err, restoreErr, backup)
		}
		return "", errors.Join(err, restart())
	}
	if err := restart(); err != nil {
		// Never replace a new service binary until SCM confirms it has stopped.
		if stopErr := service.stop(); stopErr != nil {
			return "", fmt.Errorf("new service failed: %v; cannot stop for rollback: %v; backup: %s", err, stopErr, backup)
		}
		if restoreErr := os.Rename(backup, target); restoreErr != nil {
			return "", fmt.Errorf("new service failed: %v; rollback failed: %v; backup: %s", err, restoreErr, backup)
		}
		return "", errors.Join(fmt.Errorf("new service failed; restored previous executable: %w", err), restart())
	}
	return backup, nil
}
