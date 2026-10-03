package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func listenError(address string, err error) error {
	if errors.Is(err, windows.WSAEADDRINUSE) {
		return fmt.Errorf("%s 주소를 이미 다른 프로그램이 사용 중이어서 시작하지 못했습니다. 기존 Managed Llama 실행 여부를 확인하고 해당 포트를 사용하는 프로그램을 종료하거나 -listen 설정을 변경하세요. 포트는 자동으로 변경하지 않습니다: %w", address, err)
	}
	if errors.Is(err, windows.WSAEACCES) {
		return fmt.Errorf("%s 주소에 바인드할 권한이 없습니다. Windows의 포트 예약 또는 보안 정책과 -listen 설정을 확인하세요: %w", address, err)
	}
	return fmt.Errorf("%s 주소에서 서버를 시작하지 못했습니다. 주소와 포트 설정을 확인하세요: %w", address, err)
}

// A preflight is diagnostic only; the real bind can still fail afterwards.
func checkListenAvailable(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return listenError(address, err)
	}
	return listener.Close()
}

func showStartupError(err error) {
	message, _ := windows.UTF16PtrFromString("Managed Llama를 시작하지 못했습니다.\n\n" + err.Error())
	title, _ := windows.UTF16PtrFromString("Managed Llama — 실행 오류")
	if message != nil {
		_, _ = windows.MessageBox(0, message, title, windows.MB_OK|windows.MB_ICONERROR)
	}
}

// The installer supplies a fresh file in its private temporary directory.
// Exclusive creation prevents overwriting an existing file through this flag.
func writeSetupError(path string, err error) error {
	if !filepath.IsAbs(path) {
		return errors.New("setup error file must be absolute")
	}
	f, writeErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if writeErr != nil {
		return writeErr
	}
	_, writeErr = f.WriteString("\xef\xbb\xbf" + err.Error())
	return errors.Join(writeErr, f.Close())
}

func failStartup(err error, interactive bool) {
	log.Print(err)
	if interactive {
		showStartupError(err)
	}
	os.Exit(1)
}
