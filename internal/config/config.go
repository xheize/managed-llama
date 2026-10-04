package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"managed-llama/internal/updates"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const tokenEncodingPrefix = "base64:"

type Config struct {
	UpdateRepository string `json:"update_repository"`
	ServerPath       string `json:"server_path"`
	ModelsDir        string `json:"models_dir"`
	SelectedModel    string `json:"selected_model"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	ContextSize      int    `json:"context_size"`
	GPULayers        int    `json:"gpu_layers"`
	Threads          int    `json:"threads"`
	Parallel         int    `json:"parallel"`
	ModelsMax        int    `json:"models_max"`
	Arguments        string `json:"arguments"`
	HuggingFaceBase  string `json:"hugging_face_base"`
	HuggingFaceToken string `json:"hugging_face_token,omitempty"`
}

func Default() Config {
	return Config{
		UpdateRepository: "https://github.com/xheize/managed-llama",
		ServerPath:       "llama-server.exe",
		ModelsDir:        "models",
		Host:             "127.0.0.1",
		Port:             8080,
		ContextSize:      4096,
		GPULayers:        -1,
		Threads:          0,
		Parallel:         1,
		ModelsMax:        1,
		Arguments:        "--models-dir {models_dir} --models-max {models_max} --host {host} --port {port} --ctx-size {ctx} --n-gpu-layers {gpu_layers} --parallel {parallel}",
		HuggingFaceBase:  "https://huggingface.co",
	}
}

// ApplyEnv applies deployment-time overrides. Environment values always win
// over config.json and values submitted through the dashboard.
func ApplyEnv(cfg Config) Config {
	if value := os.Getenv("LLAMA_SERVER_PATH"); value != "" {
		cfg.ServerPath = value
	}
	if value := os.Getenv("GGUF_MODELS_DIR"); value != "" {
		cfg.ModelsDir = value
	}
	if value := os.Getenv("HF_ENDPOINT"); value != "" {
		cfg.HuggingFaceBase = value
	}
	return cfg
}

func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, Save(path, cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("decode config: %w", err)
	}
	// Decode only explicitly marked disk values; legacy plaintext remains valid.
	if strings.HasPrefix(cfg.HuggingFaceToken, tokenEncodingPrefix) {
		token, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(cfg.HuggingFaceToken, tokenEncodingPrefix))
		if err != nil {
			cfg.HuggingFaceToken = ""
			return cfg, errors.New("invalid Base64 encoding for hugging_face_token")
		}
		cfg.HuggingFaceToken = string(token)
	}
	const legacyArguments = "--model {model} --host {host} --port {port} --ctx-size {ctx} --n-gpu-layers {gpu_layers} --parallel {parallel}"
	if cfg.Arguments == legacyArguments {
		cfg.Arguments = Default().Arguments
	}
	cfg = ApplyEnv(cfg)
	return cfg, cfg.Validate()
}

func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	// Encode a copy for storage only. API inputs and runtime credentials remain
	// plaintext. Base64 is reversible encoding, not encryption.
	stored := cfg
	if stored.HuggingFaceToken != "" {
		stored.HuggingFaceToken = tokenEncodingPrefix + base64.StdEncoding.EncodeToString([]byte(stored.HuggingFaceToken))
	}
	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	// Unique files prevent concurrent saves from sharing or truncating a token
	// file. Keep it beside the destination so rename stays on the same volume.
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (c Config) Validate() error {
	if c.UpdateRepository != "" {
		if _, err := updates.Repository(c.UpdateRepository); err != nil {
			return err
		}
	}
	if c.ServerPath == "" || c.ModelsDir == "" {
		return errors.New("server_path and models_dir are required")
	}
	if c.Host == "" || c.Port < 1 || c.Port > 65535 {
		return errors.New("host and a valid port are required")
	}
	if c.ContextSize < 1 || c.Parallel < 1 || c.ModelsMax < 1 {
		return errors.New("context_size, parallel, and models_max must be positive")
	}
	if c.Arguments == "" {
		return errors.New("arguments template is required")
	}
	hfURL, err := url.Parse(c.HuggingFaceBase)
	if err != nil || hfURL.Host == "" || (hfURL.Scheme != "http" && hfURL.Scheme != "https") {
		return errors.New("hugging_face_base must be an absolute HTTP(S) URL")
	}
	return nil
}
