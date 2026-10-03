package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type Status struct {
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Repository string `json:"repository"`
	Configured bool   `json:"configured"`
	Available  bool   `json:"available"`
	Phase      string `json:"phase"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Message    string `json:"message,omitempty"`
}

type Result struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

func SaveResult(target string, result Result) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".update-result-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), target+".update-result.json")
}

type Manager struct {
	mu           sync.Mutex
	status       Status
	candidate    Candidate
	client       *http.Client
	arch, target string
	preflight    func() error
	launch       func(string) error
}

func NewManager(current, arch, target string, preflight func() error, launch func(string) error) *Manager {
	m := &Manager{client: NewClient(), arch: arch, target: target, preflight: preflight, launch: launch, status: Status{Current: current, Phase: "idle"}}
	if data, err := os.ReadFile(target + ".update-result.json"); err == nil {
		var result Result
		if json.Unmarshal(data, &result) == nil && (result.Phase == "completed" || result.Phase == "failed") {
			m.status.Phase, m.status.Message = result.Phase, result.Message
		}
	}
	return m
}

func (m *Manager) Status(repository string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	if !m.busy() || s.Phase == "restarting" {
		if data, err := os.ReadFile(m.target + ".update-result.json"); err == nil {
			var result Result
			if json.Unmarshal(data, &result) == nil && (result.Phase == "completed" || result.Phase == "failed") && (s.Phase == "restarting" || (s.Phase == "idle" && s.Latest == "")) {
				s.Phase, s.Message = result.Phase, result.Message
				m.status.Phase, m.status.Message = result.Phase, result.Message
			}
		}
	}
	s.Configured = Configured(repository)
	if s.Repository != repository {
		s.Available = false
		s.Latest = ""
	}
	s.Repository = repository
	return s
}

func (m *Manager) busy() bool {
	return m.status.Phase == "checking" || m.status.Phase == "downloading" || m.status.Phase == "restarting"
}

func (m *Manager) Check(ctx context.Context, repository string) (Status, error) {
	m.mu.Lock()
	if m.busy() {
		m.mu.Unlock()
		return Status{}, errors.New("업데이트 작업이 이미 진행 중입니다")
	}
	m.status.Phase, m.status.Message = "checking", ""
	m.status.Available, m.status.Latest = false, ""
	m.status.Repository = repository
	m.candidate = Candidate{}
	m.mu.Unlock()
	candidate, err := Latest(ctx, m.client, repository, m.arch)
	m.mu.Lock()
	defer m.mu.Unlock()
	available := false
	if err == nil {
		available, err = Newer(candidate.Version, m.status.Current)
	}
	if err != nil {
		m.status.Phase, m.status.Message = "failed", err.Error()
		return m.status, err
	}
	m.candidate = candidate
	m.status.Phase, m.status.Latest, m.status.Available = "idle", candidate.Version, available
	m.status.Configured = true
	return m.status, nil
}

func (m *Manager) Install(repository, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy() {
		return errors.New("업데이트 작업이 이미 진행 중입니다")
	}
	if !m.status.Available || m.status.Repository != repository || version == "" || version != m.candidate.Version {
		return errors.New("새 버전을 다시 확인한 뒤 업데이트하세요")
	}
	if err := m.preflight(); err != nil {
		return err
	}
	candidate := m.candidate
	m.status.Phase, m.status.Message = "downloading", ""
	m.status.Downloaded, m.status.Total = 0, candidate.Asset.Size
	go func() {
		path, err := Download(context.Background(), m.client, candidate, filepath.Dir(m.target), func(n int64) {
			m.mu.Lock()
			m.status.Downloaded = n
			m.mu.Unlock()
		})
		if err == nil {
			err = m.launch(path)
			if err != nil {
				os.Remove(path)
			}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			m.status.Phase, m.status.Message = "failed", err.Error()
			return
		}
		m.status.Phase = "restarting"
		m.status.Message = "업데이트를 적용하고 있습니다. 재시작 후 트레이에서 대시보드를 다시 열어주세요."
	}()
	return nil
}
