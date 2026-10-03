package hf

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
