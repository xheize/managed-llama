package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"managed-llama/internal/process"
	"managed-llama/internal/serviceauth"
	"managed-llama/internal/updates"
)

func newWebUpdater(configPath, listen string) *updates.Manager {
	target, _ := os.Executable()
	preflight := func() error {
		if !isLoopbackListen(listen) {
			return errors.New("웹 업데이트는 loopback 대시보드에서만 지원합니다")
		}
		if strings.Contains(filepath.ToSlash(target), "/go-build") {
			return errors.New("go run 대신 빌드된 실행 파일에서 업데이트하세요")
		}
		if err := validateUpdateBinary(target); err != nil {
			return err
		}
		service, closeService, err := serviceForUpdate(target)
		if err != nil {
			return err
		}
		defer closeService()
		if service != nil || windows.GetCurrentProcessToken().IsElevated() {
			if err := serviceauth.ValidatePath(target); err != nil {
				return err
			}
		}
		file, err := os.CreateTemp(filepath.Dir(target), ".update-write-check-*")
		if err != nil {
			return fmt.Errorf("설치 폴더에 업데이트 파일을 쓸 수 없습니다: %w", err)
		}
		file.Close()
		return os.Remove(file.Name())
	}
	launch := func(source string) error {
		if err := validateUpdateBinary(source); err != nil {
			return err
		}
		isService, err := isWindowsService()
		if err != nil {
			return err
		}
		if isService || windows.GetCurrentProcessToken().IsElevated() {
			if err := serviceauth.ValidatePath(source); err != nil {
				return err
			}
		}
		args := []string{"-update", target, "-update-web", "-config", configPath, "-listen", listen}
		if !isService {
			args = append(args, "-update-restart")
		}
		// This marker is also observed by service tray processes, which must exit
		// before Windows permits replacing the executable they are running.
		if err := updates.SaveResult(target, updates.Result{Phase: "restarting", Message: "업데이트 적용 중"}); err != nil {
			return err
		}
		command := process.HideConsole(exec.Command(source, args...))
		command.Dir = filepath.Dir(target)
		if err := command.Start(); err != nil {
			_ = updates.SaveResult(target, updates.Result{Phase: "failed", Message: err.Error()})
			return err
		}
		go command.Wait()
		return nil
	}
	return updates.NewManager(version, runtime.GOARCH, target, preflight, launch)
}

// Called by the downloaded executable. Give the browser time to receive the
// restarting status and the tray watchers time to finish graceful shutdown.
func applyWebUpdate(target, configPath, listen string, restart bool) (string, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	time.Sleep(5 * time.Second)
	message, updateErr := updateManagedLlamaWithWait(target, 45*time.Second)
	result := updates.Result{Phase: "completed", Message: message}
	if updateErr != nil {
		result.Phase, result.Message = "failed", updateErr.Error()
	}
	resultErr := updates.SaveResult(target, result)
	// Restart the old desktop binary as well after a recoverable update failure.
	// Service recovery is handled by the CLI transaction, never by spawning a
	// LocalSystem desktop process in session zero.
	var restartErr error
	if restart {
		command := process.HideConsole(exec.Command(target, "-config", configPath, "-listen", listen))
		command.Dir = filepath.Dir(target)
		restartErr = command.Start()
		if restartErr == nil {
			_ = command.Process.Release()
		}
	}
	return message, errors.Join(updateErr, resultErr, restartErr)
}

func watchUpdateShutdown(done <-chan struct{}) {
	target, err := os.Executable()
	if err != nil {
		return
	}
	started := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			path := target + ".update-result.json"
			info, err := os.Stat(path)
			if err != nil || info.ModTime().Before(started) {
				continue
			}
			data, err := os.ReadFile(path)
			var result updates.Result
			if err == nil && json.Unmarshal(data, &result) == nil && result.Phase == "restarting" {
				// Allow the browser to render the restart notice first.
				time.Sleep(2 * time.Second)
				data, err = os.ReadFile(path)
				if err != nil || json.Unmarshal(data, &result) != nil || result.Phase != "restarting" {
					continue
				}
				systrayQuit()
				return
			}
		}
	}
}
