package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func assetFor(data string) Asset {
	digest := sha256.Sum256([]byte(data))
	return Asset{Name: "managed-llama-windows-amd64.exe", URL: "https://github.com/acme/managed-llama/releases/download/v1.10.0/managed-llama-windows-amd64.exe", Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(digest[:])}
}

func TestVersionComparison(t *testing.T) {
	for _, test := range []struct {
		next, current string
		want          bool
	}{
		{"v1.10.0", "v1.9.0", true}, {"v1.9.0", "v1.10.0", false}, {"v1.0.0", "v1.0.0", false}, {"v1.0.0", "dev", true},
	} {
		got, err := Newer(test.next, test.current)
		if err != nil || got != test.want {
			t.Fatalf("%+v: %v %v", test, got, err)
		}
	}
	for _, bad := range []string{"v1.2.3-rc1", "latest", "v01.2.3", "v18446744073709551616.0.0"} {
		if _, err := Newer(bad, "dev"); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestExampleRepositoryNeverRequestsNetwork(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected network request")
		return nil, errors.New("network")
	})}
	if _, err := Latest(context.Background(), client, ExampleRepository, "amd64"); err == nil {
		t.Fatal("placeholder accepted")
	}
}

func TestLatestValidation(t *testing.T) {
	for _, name := range []string{"valid", "wrong host", "wrong architecture", "missing digest", "oversized", "prerelease", "draft", "bad version"} {
		t.Run(name, func(t *testing.T) {
			release := Release{Tag: "v1.10.0", Assets: []Asset{assetFor("binary")}}
			switch name {
			case "wrong host":
				release.Assets[0].URL = "http://127.0.0.1/evil.exe"
			case "wrong architecture":
				release.Assets[0].Name = "managed-llama-windows-arm64.exe"
			case "missing digest":
				release.Assets[0].Digest = ""
			case "oversized":
				release.Assets[0].Size = MaxBinarySize + 1
			case "prerelease":
				release.Prerelease = true
			case "draft":
				release.Draft = true
			case "bad version":
				release.Tag = "latest"
			}
			body, _ := json.Marshal(release)
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.github.com/repos/acme/managed-llama/releases/latest" {
					t.Fatal(r.URL)
				}
				return response(string(body)), nil
			})}
			_, err := Latest(context.Background(), client, "https://github.com/acme/managed-llama", "amd64")
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDownloadVerificationAndCleanup(t *testing.T) {
	for _, body := range []string{"binary", "tamper", "binary-extra", "short"} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(body), nil })}
			file, err := Download(context.Background(), client, Candidate{Asset: assetFor("binary")}, dir, nil)
			if body == "binary" {
				if err != nil {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(file)
				if string(data) != body {
					t.Fatal("incorrect download")
				}
			} else {
				if err == nil {
					t.Fatal("accepted corrupt download")
				}
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatal("partial file leaked")
				}
			}
		})
	}
}

func TestRedirectPolicy(t *testing.T) {
	client := NewClient()
	for _, url := range []string{"http://github.com/file", "https://evil.example/file", "https://github.com:444/file", "https://name@github.com/file"} {
		req, _ := http.NewRequest("GET", url, nil)
		if client.CheckRedirect(req, nil) == nil {
			t.Fatalf("accepted %s", url)
		}
	}
}

func TestInstallRequiresCheckedVersionAndRepository(t *testing.T) {
	m := NewManager("v1.0.0", "amd64", filepath.Join(t.TempDir(), "app.exe"), func() error { t.Fatal("preflight called"); return nil }, nil)
	if err := m.Install("acme/repo", "v1.1.0"); err == nil {
		t.Fatal("unchecked install accepted")
	}
	m.status.Available = true
	m.status.Repository = "acme/repo"
	m.candidate.Version = "v1.1.0"
	if err := m.Install("other/repo", "v1.1.0"); err == nil {
		t.Fatal("changed repository accepted")
	}
	if err := m.Install("acme/repo", "v1.2.0"); err == nil {
		t.Fatal("changed version accepted")
	}
	m.status.Phase = "downloading"
	if err := m.Install("acme/repo", "v1.1.0"); err == nil {
		t.Fatal("duplicate install accepted")
	}
}

func TestRestartResultSurvivesProcessChange(t *testing.T) {
	target := filepath.Join(t.TempDir(), "app.exe")
	m := NewManager("v1.0.0", "amd64", target, nil, nil)
	m.status.Phase = "restarting"
	if err := SaveResult(target, Result{Phase: "failed", Message: "restored backup"}); err != nil {
		t.Fatal(err)
	}
	if got := m.Status(ExampleRepository); got.Phase != "failed" || got.Message != "restored backup" {
		t.Fatalf("%+v", got)
	}
	next := NewManager("v1.0.0", "amd64", target, nil, nil)
	if got := next.Status(ExampleRepository); got.Phase != "failed" {
		t.Fatalf("%+v", got)
	}
}

func TestAsyncInstallRejectsDuplicateAndNeverLaunchesCorruption(t *testing.T) {
	target := filepath.Join(t.TempDir(), "app.exe")
	started, release := make(chan struct{}), make(chan struct{})
	launched := make(chan string, 1)
	m := NewManager("v1.0.0", "amd64", target, func() error { return nil }, func(path string) error { launched <- path; return nil })
	m.status.Repository, m.status.Available = "acme/repo", true
	m.candidate = Candidate{Version: "v1.1.0", Asset: assetFor("binary")}
	m.client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return response("tamper"), nil
	})}
	if err := m.Install("acme/repo", "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	if err := m.Install("acme/repo", "v1.1.0"); err == nil {
		t.Fatal("duplicate download accepted")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for m.Status("acme/repo").Phase == "downloading" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if status := m.Status("acme/repo"); status.Phase != "failed" {
		t.Fatalf("%+v", status)
	}
	select {
	case path := <-launched:
		t.Fatalf("launched corrupt %s", path)
	default:
	}
}
