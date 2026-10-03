package main

import (
	"os"
	"path/filepath"
	"testing"

	"managed-llama/internal/config"
)

func TestDefaultConfigIndependentOfWorkingDirectory(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "managed-llama.exe")
	got, err := resolveConfigPath("", exe)
	if err != nil || got != filepath.Join(filepath.Dir(exe), "config.json") {
		t.Fatalf("got %q, %v", got, err)
	}
	want, _ := filepath.Abs("custom.json")
	got, err = resolveConfigPath("custom.json", exe)
	if err != nil || got != want {
		t.Fatalf("explicit path: %q, %v", got, err)
	}
}

func TestSetupCreatesConfigAndPreservesExistingBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	if err := ensureSetupConfig(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerPath != filepath.Join(root, "runtime", "llama-server.exe") || cfg.ModelsDir != filepath.Join(root, "models") {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	// Even malformed/unknown fields must never be silently replaced on repair.
	original := []byte(`{"future_setting":true,"hugging_face_token":"keep-me"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureSetupConfig(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatalf("existing config changed: %q, %v", got, err)
	}
}
