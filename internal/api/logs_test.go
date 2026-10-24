package api

import (
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"managed-llama/internal/config"
)

func TestServerRestoresAndDeletesSavedLogs(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	first := New(config.Default(), configPath, embed.FS{}, nil)
	first.runner.Log("previous session")
	if err := first.Shutdown(); err != nil {
		t.Fatal(err)
	}
	second := New(config.Default(), configPath, embed.FS{}, nil)
	w := httptest.NewRecorder()
	second.logs(w, httptest.NewRequest(http.MethodGet, "/api/server/logs", nil))
	var response struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Lines) != 1 || !strings.Contains(response.Lines[0], "previous session") {
		t.Fatalf("restored response: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	second.clearLogs(w, httptest.NewRequest(http.MethodDelete, "/api/server/logs", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(configPath+".logs", "llama-server.log")); !os.IsNotExist(err) {
		t.Fatalf("log survived deletion: %v", err)
	}
}

func TestServerReportsSavedLogDeletionFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	path := filepath.Join(configPath+".logs", "llama-server.log", "child")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	s := New(config.Default(), configPath, embed.FS{}, nil)
	w := httptest.NewRecorder()
	s.clearLogs(w, httptest.NewRequest(http.MethodDelete, "/api/server/logs", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("delete failure hidden: %d", w.Code)
	}
}
