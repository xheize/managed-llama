package hf

import (
	"bytes"
	"errors"
	"managed-llama/internal/models"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadAndDownloadCannotShareDestination(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	original := []byte("GGUF\x02\x00\x00\x00DOWNLOAD")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.Write(original)
	}))
	defer server.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	dir := t.TempDir()
	destination := filepath.Join(dir, "model.gguf")
	client := New(server.URL, "test")
	if _, err := client.StartDownload("org/model", "model.gguf", destination); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not start")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "MODEL.gguf")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("GGUF\x03\x00\x00\x00UPLOAD"))
	writer.Close()
	request := httptest.NewRequest("POST", "http://localhost/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1024); err != nil {
		t.Fatal(err)
	}
	defer request.MultipartForm.RemoveAll()
	if _, err := (models.Store{Dir: dir}).SaveUpload(request.MultipartForm.File["file"][0]); !errors.Is(err, models.ErrWriteConflict) {
		t.Fatalf("concurrent upload not rejected: %v", err)
	}
	// The reservation spans different clients, too.
	if _, err := New(server.URL, "test").StartDownload("org/other", "model.gguf", destination); !errors.Is(err, models.ErrWriteConflict) {
		t.Fatalf("second client not rejected: %v", err)
	}
	close(release)
	released = true
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state := client.Jobs()[0].State
		if state == "failed" {
			t.Fatalf("download failed: %+v", client.Jobs())
		}
		if state == "completed" {
			data, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(data, original) {
				t.Fatalf("download corrupted: %q %v", data, err)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("download did not complete")
}

func TestDownloadRespectsUploadReservation(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "model.gguf")
	upload, err := models.BeginWrite(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer upload.Close()
	client := New("http://127.0.0.1:1", "test")
	if _, err := client.StartDownload("org/model", "model.gguf", destination); !errors.Is(err, models.ErrWriteConflict) {
		t.Fatalf("download was allowed during upload: %v", err)
	}
	if len(client.Jobs()) != 0 {
		t.Fatal("rejected download created a job")
	}
}

func TestSearchFilesAndDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"org/model-GGUF","downloads":12}]`))
		case r.URL.Path == "/api/models/org/model-GGUF":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"siblings":[{"rfilename":"model-Q4.gguf","lfs":{"size":8}},{"rfilename":"README.md"}]}`))
		case strings.Contains(r.URL.Path, "/resolve/main/"):
			_, _ = w.Write([]byte{'G', 'G', 'U', 'F', 3, 0, 0, 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "")
	results, err := client.Search(t.Context(), "model", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("search: results=%#v err=%v", results, err)
	}
	files, err := client.Files(t.Context(), "org/model-GGUF")
	if err != nil || len(files) != 1 || files[0].Size != 8 {
		t.Fatalf("files: results=%#v err=%v", files, err)
	}
	dest := filepath.Join(t.TempDir(), "model-Q4.gguf")
	job, err := client.StartDownload("org/model-GGUF", files[0].Name, dest)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, current := range client.Jobs() {
			if current.ID == job.ID && current.State == "completed" {
				if _, err := os.Stat(dest); err != nil {
					t.Fatal(err)
				}
				return
			}
			if current.ID == job.ID && current.State == "failed" {
				t.Fatalf("download failed: %s", current.Error)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not complete")
}

func TestDownloadRejectsUnsupportedGGUFVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte{'G', 'G', 'U', 'F', 99, 0, 0, 0})
	}))
	defer server.Close()

	client := New(server.URL, "")
	dest := filepath.Join(t.TempDir(), "invalid.gguf")
	job, err := client.StartDownload("org/model", "invalid.gguf", dest)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, current := range client.Jobs() {
			if current.ID != job.ID || current.State != "failed" {
				continue
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatalf("invalid destination remains: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("invalid download was not rejected")
}

func TestConfigurePreservesDownloadJobs(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte{'G', 'G', 'U', 'F', 3, 0, 0, 0})
	}))
	defer server.Close()

	client := New(server.URL, "old-token")
	job, err := client.StartDownload("org/model", "model.gguf", filepath.Join(t.TempDir(), "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	client.Configure(server.URL, "new-token")
	if jobs := client.Jobs(); len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("jobs were lost during reconfiguration: %#v", jobs)
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, current := range client.Jobs() {
			if current.ID == job.ID && current.State == "completed" {
				return
			}
			if current.ID == job.ID && current.State == "failed" {
				t.Fatalf("download failed after reconfiguration: %s", current.Error)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not complete after reconfiguration")
}
