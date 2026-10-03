package models

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestModelWriteReservationAndCleanup(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "model.gguf")
	w, err := BeginWrite(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if other, err := BeginWrite(filepath.Join(dir, "MODEL.gguf")); !errors.Is(err, ErrWriteConflict) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("duplicate reservation: %v", err)
	}
	w.Write([]byte("invalid"))
	if err := w.Commit(); err == nil {
		t.Fatal("invalid GGUF accepted")
	}
	w.Close()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("failed write left files: %v", entries)
	}
	retry, err := BeginWrite(destination)
	if err != nil {
		t.Fatalf("reservation was not released: %v", err)
	}
	retry.Close()
}

func TestModelWriteDoesNotReplaceExternalFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "model.gguf")
	w, err := BeginWrite(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("GGUF\x03\x00\x00\x00new"))
	external := []byte("GGUF\x02\x00\x00\x00external")
	if err := os.WriteFile(destination, external, 0600); err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(); err == nil {
		t.Fatal("replaced a file created by another writer")
	}
	w.Close()
	data, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(data, external) {
		t.Fatalf("external file changed: %q %v", data, err)
	}
	if next, err := BeginWrite(destination); !errors.Is(err, ErrWriteConflict) {
		if next != nil {
			next.Close()
		}
		t.Fatalf("existing file accepted: %v", err)
	}
}
