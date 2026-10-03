package process

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestHideConsolePreservesFlagsAndOutput(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/c", "echo hidden-output")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	attributes := cmd.SysProcAttr
	if HideConsole(cmd) != cmd || cmd.SysProcAttr != attributes {
		t.Fatal("command or existing attributes replaced")
	}
	if !attributes.HideWindow || attributes.CreationFlags&windows.CREATE_NO_WINDOW == 0 || attributes.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("unexpected process attributes: %+v", attributes)
	}
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "hidden-output" {
		t.Fatalf("hidden command output = %q, error = %v", output, err)
	}
}

func TestHideConsoleInitializesAttributes(t *testing.T) {
	cmd := HideConsole(exec.Command("unused.exe"))
	if !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("console suppression was not configured")
	}
}
