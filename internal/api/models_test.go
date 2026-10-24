package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"managed-llama/internal/config"
)

func TestEmptyModelLibraryReturnsArray(t *testing.T) {
	cfg := config.Default()
	cfg.ModelsDir = t.TempDir()
	s := &Server{cfg: cfg}
	w := httptest.NewRecorder()
	s.listModels(w, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty library: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUploadClosesMultipartFile(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		contents []byte
		exists   bool
		status   int
	}{
		{"success", []byte("GGUF\x03\x00\x00\x00"), false, http.StatusCreated},
		{"invalid", []byte("invalid model"), false, http.StatusBadRequest},
		{"conflict", []byte("GGUF\x03\x00\x00\x00"), true, http.StatusConflict},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.ModelsDir = t.TempDir()
			if scenario.exists {
				if err := os.WriteFile(filepath.Join(cfg.ModelsDir, "model.gguf"), scenario.contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "model.gguf")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(scenario.contents); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/models/upload", &body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			// Force disk-backed multipart storage without a 32 MiB fixture.
			if err := r.ParseMultipartForm(1); err != nil {
				t.Fatal(err)
			}
			defer r.MultipartForm.RemoveAll()
			// A GC finalizer must not hide an unclosed file in the handler.
			previousGC := debug.SetGCPercent(-1)
			defer debug.SetGCPercent(previousGC)
			w := httptest.NewRecorder()
			s := &Server{cfg: cfg}
			s.uploadModel(w, r)
			if w.Code != scenario.status {
				t.Fatalf("upload: status=%d body=%s", w.Code, w.Body.String())
			}
			// net/http performs this cleanup when the request finishes. On
			// Windows it fails if FormFile's returned handle is still open.
			if err := r.MultipartForm.RemoveAll(); err != nil {
				t.Fatalf("multipart cleanup after upload: %v", err)
			}
		})
	}
}
