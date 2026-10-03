package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"managed-llama/internal/config"
	"managed-llama/internal/hf"
	"managed-llama/internal/runner"
)

type fakeAutostart struct {
	enabled bool
	err     error
}

func (f *fakeAutostart) Enabled() (bool, error) { return f.enabled, f.err }
func (f *fakeAutostart) Set(enabled bool) error {
	if f.err != nil {
		return f.err
	}
	f.enabled = enabled
	return nil
}

func TestGetConfigDoesNotExposeHFToken(t *testing.T) {
	cfg := config.Default()
	cfg.HuggingFaceToken = "hf_secret_value"
	server := &Server{cfg: cfg, runner: runner.New(), hf: hf.New(cfg.HuggingFaceBase, cfg.HuggingFaceToken)}
	recorder := httptest.NewRecorder()
	server.getConfig(recorder, httptest.NewRequest("GET", "/api/config", nil))
	if strings.Contains(recorder.Body.String(), "hf_secret_value") || strings.Contains(recorder.Body.String(), "hugging_face_token") {
		t.Fatalf("token leaked in config response: %s", recorder.Body.String())
	}
}

func TestPutConfigPreservesStoredTokenWhenFieldIsBlank(t *testing.T) {
	cfg := config.Default()
	cfg.HuggingFaceToken = "hf_existing_secret"
	configPath := filepath.Join(t.TempDir(), "config.json")
	server := &Server{cfg: cfg, configPath: configPath, runner: runner.New(), hf: hf.New(cfg.HuggingFaceBase, cfg.HuggingFaceToken)}
	requestConfig := cfg
	requestConfig.HuggingFaceToken = ""
	body, err := json.Marshal(requestConfig)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	originalHFClient := server.hf
	server.putConfig(recorder, httptest.NewRequest("PUT", "/api/config", bytes.NewReader(body)))
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.HuggingFaceToken != "hf_existing_secret" {
		t.Fatalf("stored token was not preserved: %q", stored.HuggingFaceToken)
	}
	if server.hf != originalHFClient {
		t.Fatal("saving configuration replaced the download manager")
	}
}

func TestAutostartHandlers(t *testing.T) {
	controller := &fakeAutostart{}
	server := &Server{autostart: controller}

	put := httptest.NewRecorder()
	server.putAutostart(put, httptest.NewRequest("PUT", "/api/autostart", strings.NewReader(`{"enabled":true}`)))
	if put.Code != 200 || !controller.enabled {
		t.Fatalf("PUT status=%d enabled=%v body=%s", put.Code, controller.enabled, put.Body.String())
	}

	get := httptest.NewRecorder()
	server.getAutostart(get, httptest.NewRequest("GET", "/api/autostart", nil))
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"enabled":true`) {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
}

func TestConfigPolicyRejectsUnsafeServicePathsBeforeSaving(t *testing.T) {
	cfg := config.Default()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, configPath: path, hf: hf.New(cfg.HuggingFaceBase, ""),
		ConfigPolicy: func(config.Config) error { return errors.New("untrusted service path") },
	}
	next := cfg
	next.ServerPath = `C:\Users\Public\untrusted.exe`
	data, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.putConfig(w, httptest.NewRequest("PUT", "/api/config", bytes.NewReader(data)))
	if w.Code != 400 {
		t.Fatalf("status=%d", w.Code)
	}
	stored, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored != cfg || s.cfg != cfg {
		t.Fatal("rejected configuration changed disk or runtime")
	}
}
