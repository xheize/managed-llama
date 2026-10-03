package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOccupiedPortReportsAddressAndRecovery(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	err = checkListenAvailable(listener.Addr().String())
	if err == nil {
		t.Fatal("occupied port accepted")
	}
	for _, fragment := range []string{listener.Addr().String(), "시작하지 못했습니다", "설정"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("missing %q: %v", fragment, err)
		}
	}
}

func TestListenErrorClassifiesWrappedWindowsErrors(t *testing.T) {
	for _, tc := range []struct {
		cause error
		want  string
	}{
		{windows.WSAEADDRINUSE, "이미 다른 프로그램"},
		{windows.WSAEACCES, "포트 예약"},
		{errors.New("invalid address"), "주소와 포트 설정"},
	} {
		err := listenError("127.0.0.1:3030", &net.OpError{Op: "listen", Net: "tcp", Err: tc.cause})
		if !errors.Is(err, tc.cause) || !strings.Contains(err.Error(), tc.want) {
			t.Fatal(err)
		}
	}
}

func TestSetupErrorFilePreservesUnicodeAndDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error.txt")
	err := errors.New("127.0.0.1:3030 포트를 사용 중입니다")
	if writeErr := writeSetupError(path, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	want := "\xef\xbb\xbf" + err.Error()
	if writeErr := writeSetupError(path, errors.New("replacement")); writeErr == nil {
		t.Fatal("existing file overwritten")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != want {
		t.Fatalf("%q %v", got, readErr)
	}
}
