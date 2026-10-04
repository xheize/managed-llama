package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultUpdateRepository(t *testing.T) {
	if got := Default().UpdateRepository; got != "https://github.com/xheize/managed-llama" {
		t.Fatalf("unexpected default update repository: %q", got)
	}
}

func TestTokenStorageEncodingAndRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.HuggingFaceToken = "hf_storage_test"
	for i := 0; i < 2; i++ {
		if err := Save(path, cfg); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var stored Config
		if err := json.Unmarshal(data, &stored); err != nil {
			t.Fatal(err)
		}
		want := "base64:" + base64.StdEncoding.EncodeToString([]byte("hf_storage_test"))
		if stored.HuggingFaceToken != want || strings.Contains(string(data), "hf_storage_test") {
			t.Fatal("unexpected token storage format")
		}
		cfg, err = Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.HuggingFaceToken != "hf_storage_test" {
			t.Fatal("token not restored")
		}
	}
	cfg.HuggingFaceToken = ""
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hugging_face_token") {
		t.Fatal("removed token still persisted")
	}
}

func TestLegacyTokenAndMalformedEncoding(t *testing.T) {
	for _, token := range []string{"hf_legacy_test", "base64:invalid!"} {
		t.Run(token, func(t *testing.T) {
			cfg := Default()
			cfg.HuggingFaceToken = token
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(path)
			if token == "hf_legacy_test" {
				if err != nil || loaded.HuggingFaceToken != token {
					t.Fatal("legacy token unreadable", err)
				}
			} else if err == nil || strings.Contains(err.Error(), token) {
				t.Fatal("invalid token must fail without exposing its value")
			}
		})
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Default()
	want.Port = 9090
	want.HuggingFaceToken = "hf_test_secret"
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestValidate(t *testing.T) {
	cfg := Default()
	cfg.Port = 70000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestSaveCleansTemporaryFileAfterRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// An existing directory makes replacement fail on Windows without mocking IO.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.HuggingFaceToken = "hf_temporary_test"
	if err := Save(path, cfg); err == nil {
		t.Fatal("expected replacement to fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" || !entries[0].IsDir() {
		t.Fatal("failed save left a temporary credential file")
	}
}

func TestSaveLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	if err := Save(filepath.Join(dir, "config.json"), Default()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("successful save left temporary files")
	}
}

func TestApplyEnv(t *testing.T) {
	t.Setenv("LLAMA_SERVER_PATH", `C:\llama\llama-server.exe`)
	t.Setenv("GGUF_MODELS_DIR", `D:\models`)
	t.Setenv("HF_ENDPOINT", "https://hf.example.test")

	cfg := ApplyEnv(Default())
	if cfg.ServerPath != `C:\llama\llama-server.exe` {
		t.Fatalf("unexpected server path: %s", cfg.ServerPath)
	}
	if cfg.ModelsDir != `D:\models` {
		t.Fatalf("unexpected models dir: %s", cfg.ModelsDir)
	}
	if cfg.HuggingFaceBase != "https://hf.example.test" {
		t.Fatalf("unexpected HF endpoint: %s", cfg.HuggingFaceBase)
	}
}
