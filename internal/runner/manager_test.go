package runner

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"managed-llama/internal/config"
)

func TestSplitArgs(t *testing.T) {
	got, err := SplitArgs(`--model "C:\Models\my model.gguf" --port 8080 --flag ''`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", `C:\Models\my model.gguf`, "--port", "8080", "--flag", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestBuildRouterCommandWithoutSelectedModel(t *testing.T) {
	cfg := config.Default()
	cfg.ModelsDir = t.TempDir()
	cfg.SelectedModel = ""
	_, args, command, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--models-dir") || !strings.Contains(joined, "--models-max 1") {
		t.Fatalf("unexpected args: %q", joined)
	}
	if !strings.Contains(command, "llama-server.exe") {
		t.Fatalf("unexpected command: %q", command)
	}
}

func TestSplitArgsRejectsOpenQuote(t *testing.T) {
	if _, err := SplitArgs(`--model "unfinished`); err == nil {
		t.Fatal("expected an error")
	}
}

func TestHealthReadyOnlyAcceptsOK(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusBadRequest, http.StatusNotFound, http.StatusServiceUnavailable} {
		if healthReady(status) {
			t.Fatalf("status %d must not be ready", status)
		}
	}
	if !healthReady(http.StatusOK) {
		t.Fatal("status 200 must be ready")
	}
}

func TestLogCaptureClearAndIdleShutdown(t *testing.T) {
	manager := New()
	manager.capture(strings.NewReader("first\nsecond\n"), "stdout")
	logs := manager.Logs(10)
	if len(logs) != 2 || !strings.Contains(logs[0], "[stdout] first") || !strings.Contains(logs[1], "[stdout] second") {
		t.Fatalf("unexpected captured logs: %#v", logs)
	}
	manager.ClearLogs()
	if logs := manager.Logs(10); len(logs) != 0 {
		t.Fatalf("logs were not cleared: %#v", logs)
	}
	if err := manager.Shutdown(); err != nil {
		t.Fatalf("idle shutdown returned an error: %v", err)
	}
}

func TestIdleStatusOmitsStartedAt(t *testing.T) {
	status := New().Status()
	if status.StartedAt != nil {
		t.Fatalf("idle status started_at = %v, want nil", status.StartedAt)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "started_at") {
		t.Fatalf("idle status must omit started_at: %s", encoded)
	}
}
