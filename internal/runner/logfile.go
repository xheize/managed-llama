package runner

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const logFileLimit int64 = 10 * 1024 * 1024
const logFileBackups = 3

// NewWithLogFile restores recent output and appends future output to disk.
// New remains available for managers that only need an in-memory buffer.
func NewWithLogFile(path string) *Manager {
	m := New()
	if path == "" {
		return m
	}
	m.logPath = path
	for index := logFileBackups; index >= 0; index-- {
		if err := m.loadLogFile(m.logFileName(index)); err != nil {
			m.appendMemoryLocked("[manager] read log file error: " + err.Error())
		}
	}
	return m
}

func (m *Manager) logFileName(index int) string {
	if index == 0 {
		return m.logPath
	}
	return fmt.Sprintf("%s.%d", m.logPath, index)
}

func (m *Manager) loadLogFile(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		m.appendMemoryLocked(scanner.Text())
	}
	return scanner.Err()
}

func (m *Manager) prepareLogFileLocked() error {
	if m.logPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.logPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func (m *Manager) writeLogLocked(line string) error {
	if m.logPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.logPath), 0o700); err != nil {
		return err
	}
	info, err := os.Stat(m.logPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info != nil && info.Size() > 0 && info.Size()+int64(len(line)+1) > logFileLimit {
		if err := m.rotateLogFilesLocked(); err != nil {
			return err
		}
	}
	// No userspace buffer or long-lived handle: each captured line is written
	// and closed immediately, so app exit does not discard buffered output.
	f, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(f, line)
	return errors.Join(writeErr, f.Close())
}

func (m *Manager) rotateLogFilesLocked() error {
	if err := os.Remove(m.logFileName(logFileBackups)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for index := logFileBackups - 1; index >= 0; index-- {
		if err := os.Rename(m.logFileName(index), m.logFileName(index+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (m *Manager) clearLogFilesLocked() error {
	if m.logPath == "" {
		return nil
	}
	for index := 0; index <= logFileBackups; index++ {
		if err := os.Remove(m.logFileName(index)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
