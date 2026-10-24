package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"managed-llama/internal/config"
)

func TestPersistentLogsSurviveShutdownAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json.logs", "llama-server.log")
	m := NewWithLogFile(path)
	m.capture(strings.NewReader("first\nlast stdout\n"), "stdout")
	m.capture(strings.NewReader("last stderr\n"), "stderr")
	if err := m.Shutdown(); err != nil {
		t.Fatal(err)
	}
	restored := NewWithLogFile(path)
	lines := restored.Logs(0)
	if len(lines) != 3 || !strings.Contains(lines[0], "[stdout] first") || !strings.Contains(lines[2], "[stderr] last stderr") {
		t.Fatalf("restored logs: %#v", lines)
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.Fields(lines[0])[0]); err != nil {
		t.Fatalf("missing date and timezone: %v", err)
	}
	restored.Log("new session")
	if got := NewWithLogFile(path).Logs(0); len(got) != 4 {
		t.Fatalf("appending lost previous session: %#v", got)
	}
	if err := restored.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	if got := NewWithLogFile(path).Logs(0); len(got) != 0 {
		t.Fatalf("cleared logs restored: %#v", got)
	}
	restored.Log("after clear")
	if got := NewWithLogFile(path).Logs(0); len(got) != 1 || !strings.Contains(got[0], "after clear") {
		t.Fatalf("logging after clear failed: %#v", got)
	}
}

func TestPersistentLogRotationAndBoundedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llama-server.log")
	m := NewWithLogFile(path)
	for generation := 0; generation < 5; generation++ {
		// Fill the current file to its limit without generating enormous test output.
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(f, "generation %d\n", generation); err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(logFileLimit); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		m.Log(fmt.Sprintf("after rotation %d", generation))
	}
	for index := 1; index <= logFileBackups; index++ {
		data, err := os.ReadFile(m.logFileName(index))
		if err != nil || !strings.HasPrefix(string(data), fmt.Sprintf("generation %d\n", 5-index)) {
			t.Fatalf("backup %d incorrect: %v", index, err)
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Fatalf("unexpected fourth backup: %v", err)
	}
	if err := m.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= logFileBackups; index++ {
		if _, err := os.Stat(m.logFileName(index)); !os.IsNotExist(err) {
			t.Fatalf("backup %d survived clear: %v", index, err)
		}
	}
	var history strings.Builder
	for n := 0; n < 2100; n++ {
		fmt.Fprintf(&history, "line %d\n", n)
	}
	if err := os.WriteFile(path+".1", []byte(history.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("latest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := NewWithLogFile(path).Logs(0)
	if len(lines) != 2000 || lines[0] != "line 101" || lines[1999] != "latest" {
		t.Fatalf("incorrect restored tail: count=%d first=%q last=%q", len(lines), lines[0], lines[len(lines)-1])
	}
}

func TestPersistentLogFailuresAreVisible(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewWithLogFile(filepath.Join(blocked, "llama-server.log"))
	m.Log("still visible")
	if !strings.Contains(strings.Join(m.Logs(0), "\n"), "log file error") {
		t.Fatal("write failure was hidden")
	}
	cfg := config.Default()
	cfg.ModelsDir = t.TempDir()
	if err := m.Start(cfg); err == nil || !strings.Contains(err.Error(), "prepare model server log") {
		t.Fatalf("start ignored log failure: %v", err)
	}
	// A directory in place of the log must produce a clear failure, not success.
	path := filepath.Join(t.TempDir(), "llama-server.log")
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	m = NewWithLogFile(path)
	if err := m.ClearLogs(); err == nil {
		t.Fatal("clear failure was hidden")
	}
}

func TestPersistentLogsDrainChildOutput(t *testing.T) {
	t.Setenv("MANAGED_LLAMA_LOG_TEST_CHILD", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "llama-server.log")
	m := NewWithLogFile(path)
	cfg := config.Default()
	cfg.ServerPath = executable
	cfg.ModelsDir = t.TempDir()
	cfg.Arguments = "-test.run=^TestLogOutputChild$"
	if err := m.Start(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Shutdown() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = m.Status()
		_, _ = m.Target()
		_ = m.PID()
		if strings.Contains(strings.Join(m.Logs(0), "\n"), "[manager] exited normally") {
			lines := NewWithLogFile(path).Logs(0)
			joined := strings.Join(lines, "\n")
			if len(lines) != 202 || !strings.Contains(joined, "[stdout] stdout 99") || !strings.Contains(joined, "[stderr] stderr 99") {
				t.Fatalf("lost child output: %d lines", len(lines))
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("child output did not drain")
}

func TestLogOutputChild(t *testing.T) {
	mode := os.Getenv("MANAGED_LLAMA_LOG_TEST_CHILD")
	if mode == "" {
		return
	}
	for n := 0; n < 100; n++ {
		fmt.Fprintf(os.Stdout, "stdout %d\n", n)
		fmt.Fprintf(os.Stderr, "stderr %d\n", n)
	}
	if mode == "wait" {
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func TestShutdownPersistsExitRecordBeforeReturning(t *testing.T) {
	t.Setenv("MANAGED_LLAMA_LOG_TEST_CHILD", "wait")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "llama-server.log")
	m := NewWithLogFile(path)
	cfg := config.Default()
	cfg.ServerPath = executable
	cfg.ModelsDir = t.TempDir()
	cfg.Arguments = "-test.run=^TestLogOutputChild$"
	t.Cleanup(func() { _ = m.Shutdown() })
	for run := 0; run < 2; run++ {
		if err := m.Start(cfg); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(strings.Join(m.Logs(0), "\n"), "[stderr] stderr 99") {
			if time.Now().After(deadline) {
				t.Fatal("child did not produce output")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err := m.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if m.Status().Running || m.PID() != 0 {
			t.Fatal("shutdown left running state")
		}
		lines := NewWithLogFile(path).Logs(0)
		if !strings.Contains(lines[len(lines)-1], "stopped and model memory released") {
			t.Fatalf("shutdown returned before exit record: %q", lines[len(lines)-1])
		}
		if err := m.ClearLogs(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLogHistoryReusesBoundedBuffer(t *testing.T) {
	m := New()
	for i := 0; i < m.maxLogs; i++ {
		m.appendMemoryLocked("old")
	}
	allocations := testing.AllocsPerRun(100, func() { m.appendMemoryLocked("new") })
	if allocations != 0 {
		t.Fatalf("full log buffer allocates per line: %v", allocations)
	}
	if lines := m.Logs(0); len(lines) != m.maxLogs || lines[len(lines)-1] != "new" {
		t.Fatal("log tail was not retained")
	}
}
