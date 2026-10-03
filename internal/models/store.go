package models

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Model struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Version  uint32    `json:"gguf_version,omitempty"`
	Valid    bool      `json:"valid"`
}

type Store struct{ Dir string }

func (s Store) Ensure() error { return os.MkdirAll(s.Dir, 0o755) }

func (s Store) List() ([]Model, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	var out []Model
	err := filepath.WalkDir(s.Dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gguf") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Dir, path)
		if err != nil {
			return err
		}
		version, valid := Inspect(path)
		out = append(out, Model{Path: filepath.ToSlash(rel), Name: entry.Name(), Size: info.Size(), Modified: info.ModTime(), Version: version, Valid: valid})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func (s Store) SaveUpload(header *multipart.FileHeader) (Model, error) {
	if !strings.EqualFold(filepath.Ext(header.Filename), ".gguf") {
		return Model{}, errors.New("only .gguf files are accepted")
	}
	if err := s.Ensure(); err != nil {
		return Model{}, err
	}
	src, err := header.Open()
	if err != nil {
		return Model{}, err
	}
	defer src.Close()
	name := filepath.Base(header.Filename)
	dest, err := s.resolve(name)
	if err != nil {
		return Model{}, err
	}
	out, err := BeginWrite(dest)
	if err != nil {
		return Model{}, err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return Model{}, err
	}
	if err := out.Commit(); err != nil {
		return Model{}, err
	}
	models, err := s.List()
	if err != nil {
		return Model{}, err
	}
	for _, model := range models {
		if model.Path == filepath.ToSlash(name) {
			return model, nil
		}
	}
	return Model{}, errors.New("uploaded model was not found after save")
}

func (s Store) Delete(rel string) error {
	path, err := s.resolve(rel)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(path), ".gguf") {
		return errors.New("only GGUF files can be deleted")
	}
	return os.Remove(path)
}

func (s Store) Destination(filename string) (string, error) {
	return s.resolve(filepath.Base(filename))
}

func (s Store) resolve(rel string) (string, error) {
	base, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(base, target)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes models directory")
	}
	return target, nil
}

// Inspect validates the GGUF magic and supported file-format version.
func Inspect(path string) (uint32, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	header := make([]byte, 8)
	if _, err := io.ReadFull(f, header); err != nil {
		return 0, false
	}
	if string(header[:4]) != "GGUF" {
		return 0, false
	}
	version := binary.LittleEndian.Uint32(header[4:])
	return version, version >= 1 && version <= 3
}
