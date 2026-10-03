package models

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestListInspectsGGUF(t *testing.T) {
	dir := t.TempDir()
	header := make([]byte, 8)
	copy(header, "GGUF")
	binary.LittleEndian.PutUint32(header[4:], 3)
	if err := os.WriteFile(filepath.Join(dir, "tiny.gguf"), header, 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := (Store{Dir: dir}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Valid || list[0].Version != 3 {
		t.Fatalf("unexpected result: %#v", list)
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	if _, err := (Store{Dir: t.TempDir()}).resolve("../outside.gguf"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
