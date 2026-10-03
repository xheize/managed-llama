package api

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"managed-llama/internal/config"
	"managed-llama/internal/hf"
	"managed-llama/internal/runner"
)

func TestGatewayStateTransitions(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	modelsDir := t.TempDir()
	cfg := config.Default()
	cfg.ServerPath = executable
	cfg.ModelsDir = modelsDir
	server := &Server{cfg: cfg, runner: runner.New(), hf: hf.New("https://huggingface.co", "")}

	state := server.gatewayState()
	if state.Phase != "empty_library" || !state.GatewayOnly || state.HFEnabled {
		t.Fatalf("unexpected empty state: %#v", state)
	}

	modelPath := filepath.Join(modelsDir, "tiny.gguf")
	header := make([]byte, 8)
	copy(header, "GGUF")
	binary.LittleEndian.PutUint32(header[4:], 3)
	if err := os.WriteFile(modelPath, header, 0o644); err != nil {
		t.Fatal(err)
	}
	state = server.gatewayState()
	if state.Phase != "default_model_required" || state.ModelCount != 1 {
		t.Fatalf("unexpected model state: %#v", state)
	}

	server.cfg.SelectedModel = "tiny.gguf"
	state = server.gatewayState()
	if state.Phase != "ready" || !state.CanStart {
		t.Fatalf("unexpected ready state: %#v", state)
	}
}

func TestHuggingFaceEndpointsDisabledWithoutToken(t *testing.T) {
	server := &Server{cfg: config.Default(), runner: runner.New(), hf: hf.New("https://huggingface.co", "")}
	recorder := httptest.NewRecorder()
	server.searchHF(recorder, httptest.NewRequest("GET", "/api/hf/search?q=test", nil))
	if recorder.Code != 503 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSameModelPath(t *testing.T) {
	if !sameModelReference("Qwen", `C:\models\Qwen.gguf`, `C:\models\Qwen.gguf`, "Qwen.gguf") {
		t.Fatal("expected Windows model paths to match")
	}
	if !sameModelReference("stories260K-f32", "", `C:\models\stories260K-f32.gguf`, "stories260K-f32.gguf") {
		t.Fatal("expected extensionless router ID to match selected GGUF")
	}
}

func TestGatewayAndLlamaHealthAreIndependent(t *testing.T) {
	server := &Server{cfg: config.Default(), runner: runner.New(), hf: hf.New("https://huggingface.co", "")}

	gatewayRecorder := httptest.NewRecorder()
	server.gatewayHealth(gatewayRecorder, httptest.NewRequest(http.MethodGet, "/health/gateway", nil))
	if gatewayRecorder.Code != http.StatusOK {
		t.Fatalf("gateway health status=%d body=%s", gatewayRecorder.Code, gatewayRecorder.Body.String())
	}
	var gateway serviceHealth
	if err := json.Unmarshal(gatewayRecorder.Body.Bytes(), &gateway); err != nil || !gateway.Healthy || gateway.Service != "gateway" {
		t.Fatalf("unexpected gateway health: %#v err=%v", gateway, err)
	}

	llamaRecorder := httptest.NewRecorder()
	server.llamaHealth(llamaRecorder, httptest.NewRequest(http.MethodGet, "/health/llama", nil))
	if llamaRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("llama health status=%d body=%s", llamaRecorder.Code, llamaRecorder.Body.String())
	}
	var llama serviceHealth
	if err := json.Unmarshal(llamaRecorder.Body.Bytes(), &llama); err != nil || llama.Healthy || llama.Status != "stopped" {
		t.Fatalf("unexpected llama health: %#v err=%v", llama, err)
	}
}

func TestClearServerLogs(t *testing.T) {
	manager := runner.New()
	manager.Log("test line")
	server := &Server{cfg: config.Default(), runner: manager, hf: hf.New("https://huggingface.co", "")}
	recorder := httptest.NewRecorder()
	server.clearLogs(recorder, httptest.NewRequest(http.MethodDelete, "/api/server/logs", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("clear logs status=%d", recorder.Code)
	}
	if logs := manager.Logs(10); len(logs) != 0 {
		t.Fatalf("logs were not cleared: %#v", logs)
	}
}
