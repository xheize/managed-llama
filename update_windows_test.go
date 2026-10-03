package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeUpdateService struct {
	active        bool
	failStart     bool
	starts, stops int
}

func (s *fakeUpdateService) running() (bool, error) { return s.active, nil }
func (s *fakeUpdateService) stop() error            { s.stops++; s.active = false; return nil }
func (s *fakeUpdateService) start() error {
	s.starts++
	if s.failStart && s.starts == 1 {
		return errors.New("new version failed")
	}
	s.active = true
	return nil
}

func TestExecutableUpdate(t *testing.T) {
	for _, name := range []string{"desktop", "running service", "stopped service", "start failure", "replace failure"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target, staged := filepath.Join(dir, "managed-llama.exe"), filepath.Join(dir, "new.exe")
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(target, "old")
			write(staged, "new")
			write(filepath.Join(dir, "config.json"), "settings")
			write(filepath.Join(dir, "config.json.service-key"), "key")
			var controller updateService
			fake := &fakeUpdateService{active: name != "stopped service", failStart: name == "start failure"}
			if name != "desktop" {
				controller = fake
			}
			if name == "replace failure" {
				if err := os.Remove(staged); err != nil {
					t.Fatal(err)
				}
			}
			backup, err := applyExecutableUpdate(target, staged, controller)
			wantFailure := name == "start failure" || name == "replace failure"
			if (err != nil) != wantFailure {
				t.Fatalf("unexpected result: backup=%s err=%v", backup, err)
			}
			want := "new"
			if wantFailure {
				want = "old"
			}
			data, readErr := os.ReadFile(target)
			if readErr != nil || string(data) != want {
				t.Fatalf("target=%q err=%v", data, readErr)
			}
			if !wantFailure {
				data, err = os.ReadFile(backup)
				if err != nil || string(data) != "old" {
					t.Fatalf("backup=%q err=%v", data, err)
				}
			}
			if name == "stopped service" && (fake.starts != 0 || fake.stops != 0) {
				t.Fatal("stopped service was changed")
			}
			if controller != nil && name != "stopped service" && !fake.active {
				t.Fatal("previously running service was not restored")
			}
			for path, want := range map[string]string{"config.json": "settings", "config.json.service-key": "key"} {
				data, err := os.ReadFile(filepath.Join(dir, path))
				if err != nil || string(data) != want {
					t.Fatalf("%s changed", path)
				}
			}
		})
	}
}

func TestUpdateRejectsOwnExecutable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateManagedLlama(executable); err == nil || !strings.Contains(err.Error(), "different, existing") {
		t.Fatalf("self update not rejected: %v", err)
	}
}

func TestUpdateRejectsNonApplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.exe")
	if err := os.WriteFile(path, []byte("not an executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := updateManagedLlama(path); err == nil {
		t.Fatal("accepted invalid application")
	}
}

func TestStageUpdateCopy(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.exe")
	if err := os.WriteFile(source, []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	staged, err := stageUpdate(source, dir)
	if err != nil {
		t.Fatal(err)
	}
	if staged == source {
		t.Fatal("staging overwrote source")
	}
	data, err := os.ReadFile(staged)
	if err != nil || string(data) != "binary" {
		t.Fatalf("staged=%q err=%v", data, err)
	}
}
