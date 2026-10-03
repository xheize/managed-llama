package models

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

var ErrWriteConflict = errors.New("model already exists or is being written")

var modelWrites = struct {
	sync.Mutex
	active map[string]bool
}{active: make(map[string]bool)}

// PendingWrite owns a unique temporary file and a destination reservation shared
// by uploads and all download clients. Close must be called on every exit path.
type PendingWrite struct {
	file                        *os.File
	temporary, destination, key string
	once                        sync.Once
}

func BeginWrite(destination string) (*PendingWrite, error) {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return nil, err
	}
	dir := filepath.Dir(destination)
	key := strings.ToLower(destination)
	modelWrites.Lock()
	if modelWrites.active[key] {
		modelWrites.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrWriteConflict, filepath.Base(destination))
	}
	modelWrites.active[key] = true
	modelWrites.Unlock()
	w := &PendingWrite{destination: destination, key: key}
	if _, err := os.Lstat(destination); err == nil {
		w.Close()
		return nil, fmt.Errorf("%w: %s", ErrWriteConflict, filepath.Base(destination))
	} else if !errors.Is(err, os.ErrNotExist) {
		w.Close()
		return nil, err
	}
	w.file, err = os.CreateTemp(dir, filepath.Base(destination)+"-*.part")
	if err != nil {
		w.Close()
		return nil, err
	}
	w.temporary = w.file.Name()
	return w, nil
}

func (w *PendingWrite) Write(data []byte) (int, error) { return w.file.Write(data) }

func (w *PendingWrite) Commit() error {
	if err := w.file.Sync(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	if _, valid := Inspect(w.temporary); !valid {
		return errors.New("file does not contain a supported GGUF header")
	}
	from, err := windows.UTF16PtrFromString(w.temporary)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(w.destination)
	if err != nil {
		return err
	}
	// No REPLACE_EXISTING flag: even a different process cannot have its newly
	// published model silently replaced between our initial check and this move.
	return windows.MoveFileEx(from, to, 0)
}

func (w *PendingWrite) Close() {
	w.once.Do(func() {
		if w.file != nil {
			_ = w.file.Close()
		}
		if w.temporary != "" {
			_ = os.Remove(w.temporary)
		}
		modelWrites.Lock()
		delete(modelWrites.active, w.key)
		modelWrites.Unlock()
	})
}
