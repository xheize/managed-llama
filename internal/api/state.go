package api

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"managed-llama/internal/models"
)

type GatewayState struct {
	Phase            string `json:"phase"`
	Message          string `json:"message"`
	GatewayOnly      bool   `json:"gateway_only"`
	CanStart         bool   `json:"can_start"`
	HFEnabled        bool   `json:"hf_enabled"`
	HFDisabledReason string `json:"hf_disabled_reason,omitempty"`
	ServerAvailable  bool   `json:"server_available"`
	ModelCount       int    `json:"model_count"`
	DefaultModel     string `json:"default_model,omitempty"`
}

func (s *Server) gatewayState() GatewayState {
	cfg := s.config()
	state := GatewayState{
		GatewayOnly:     true,
		HFEnabled:       s.hfClient().Enabled(),
		ServerAvailable: executableAvailable(cfg.ServerPath),
		DefaultModel:    cfg.SelectedModel,
	}
	if !state.HFEnabled {
		state.HFDisabledReason = "Hugging Face 토큰이 설정되지 않아 관련 기능이 비활성화되었습니다."
	}

	localModels, modelErr := (models.Store{Dir: cfg.ModelsDir}).List()
	if modelErr == nil {
		for _, model := range localModels {
			if model.Valid {
				state.ModelCount++
			}
		}
	}
	runtime := s.runner.Status()
	if runtime.Running {
		state.GatewayOnly = false
		if runtime.Ready {
			state.Phase = "running"
			state.Message = "llama-server router가 요청을 처리할 준비가 되었습니다."
		} else {
			state.Phase = "starting"
			state.Message = "llama-server router가 시작 또는 모델 로딩 중입니다."
		}
		return state
	}
	if !state.ServerAvailable {
		state.Phase = "setup_required"
		state.Message = "LLAMA_SERVER_PATH에서 llama-server 실행 파일을 찾을 수 없습니다."
		return state
	}
	if modelErr != nil {
		state.Phase = "error"
		state.Message = "GGUF 모델 디렉터리를 읽을 수 없습니다: " + modelErr.Error()
		return state
	}
	if state.ModelCount == 0 {
		state.Phase = "empty_library"
		state.Message = "GGUF 모델이 없습니다. 게이트웨이만 실행 중입니다."
		return state
	}
	if cfg.SelectedModel == "" {
		state.Phase = "default_model_required"
		state.Message = "llama.cpp를 시작하기 전에 기본 모델을 선택하세요."
		return state
	}
	if !containsValidModel(localModels, cfg.SelectedModel) {
		state.Phase = "invalid_default_model"
		state.Message = "설정된 기본 모델이 없거나 유효한 GGUF가 아닙니다."
		return state
	}
	if runtime.LastError != "" {
		state.Phase = "error"
		state.Message = "마지막 llama-server 실행이 실패했습니다: " + runtime.LastError
		state.CanStart = true
		return state
	}
	state.Phase = "ready"
	state.Message = "기본 모델과 llama-server가 준비되었습니다."
	state.CanStart = true
	return state
}

func (s *Server) requireStartReady() error {
	cfg := s.config()
	if !executableAvailable(cfg.ServerPath) {
		return errors.New("setup_required: llama-server executable was not found")
	}
	localModels, err := (models.Store{Dir: cfg.ModelsDir}).List()
	if err != nil {
		return fmt.Errorf("model_library_error: %w", err)
	}
	validCount := 0
	for _, model := range localModels {
		if model.Valid {
			validCount++
		}
	}
	if validCount == 0 {
		return errors.New("empty_library: at least one valid GGUF model is required")
	}
	if cfg.SelectedModel == "" {
		return errors.New("default_model_required: select a default model first")
	}
	if !containsValidModel(localModels, cfg.SelectedModel) {
		return errors.New("invalid_default_model: the selected default model is unavailable")
	}
	return nil
}

func executableAvailable(path string) bool {
	if path == "" {
		return false
	}
	if filepath.IsAbs(path) || strings.ContainsAny(path, `/\\`) {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(path)
	return err == nil
}

func containsValidModel(list []models.Model, selected string) bool {
	selected = filepath.ToSlash(filepath.Clean(selected))
	for _, model := range list {
		if model.Valid && filepath.ToSlash(filepath.Clean(model.Path)) == selected {
			return true
		}
	}
	return false
}

func (s *Server) requireHF() error {
	if !s.hfClient().Enabled() {
		return errors.New("huggingface_disabled: configure a Hugging Face token first")
	}
	return nil
}
