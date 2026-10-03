package runner

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"managed-llama/internal/config"
	background "managed-llama/internal/process"
)

type Status struct {
	Running   bool       `json:"running"`
	Ready     bool       `json:"ready"`
	PID       int        `json:"pid,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Command   string     `json:"command"`
	Endpoint  string     `json:"endpoint"`
	LastError string     `json:"last_error,omitempty"`
}

type Manager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	startedAt time.Time
	command   string
	endpoint  string
	lastError string
	stopping  bool
	logs      []string
	maxLogs   int
}

func New() *Manager { return &Manager{maxLogs: 2000} }

func Build(cfg config.Config) (string, []string, string, error) {
	absModelsDir, err := filepath.Abs(cfg.ModelsDir)
	if err != nil {
		return "", nil, "", err
	}
	if err := os.MkdirAll(absModelsDir, 0o755); err != nil {
		return "", nil, "", err
	}
	absModel := ""
	if strings.Contains(cfg.Arguments, "{model}") {
		if cfg.SelectedModel == "" {
			return "", nil, "", errors.New("select a GGUF model first")
		}
		model := cfg.SelectedModel
		if !filepath.IsAbs(model) {
			model = filepath.Join(cfg.ModelsDir, model)
		}
		absModel, err = filepath.Abs(model)
		if err != nil {
			return "", nil, "", err
		}
		if info, statErr := os.Stat(absModel); statErr != nil || info.IsDir() {
			return "", nil, "", fmt.Errorf("model not found: %s", absModel)
		}
	}
	replacements := map[string]string{
		"{model}": absModel, "{host}": cfg.Host, "{port}": strconv.Itoa(cfg.Port),
		"{ctx}": strconv.Itoa(cfg.ContextSize), "{gpu_layers}": strconv.Itoa(cfg.GPULayers),
		"{threads}": strconv.Itoa(cfg.Threads), "{parallel}": strconv.Itoa(cfg.Parallel),
		"{models_dir}": absModelsDir, "{models_max}": strconv.Itoa(cfg.ModelsMax),
	}
	argsText := cfg.Arguments
	for key, value := range replacements {
		argsText = strings.ReplaceAll(argsText, key, quote(value))
	}
	args, err := SplitArgs(argsText)
	if err != nil {
		return "", nil, "", err
	}
	return cfg.ServerPath, args, displayCommand(cfg.ServerPath, args), nil
}

func Preview(cfg config.Config) (string, error) {
	exe, _, display, err := Build(cfg)
	if err == nil {
		return display, nil
	}
	// A useful preview is still available before a model is downloaded.
	if cfg.SelectedModel == "" && strings.Contains(cfg.Arguments, "{model}") {
		copy := cfg
		copy.SelectedModel = "<select-a-model.gguf>"
		copy.ModelsDir = "."
		exe = copy.ServerPath
		text := copy.Arguments
		values := map[string]string{"{model}": copy.SelectedModel, "{host}": copy.Host, "{port}": strconv.Itoa(copy.Port), "{ctx}": strconv.Itoa(copy.ContextSize), "{gpu_layers}": strconv.Itoa(copy.GPULayers), "{threads}": strconv.Itoa(copy.Threads), "{parallel}": strconv.Itoa(copy.Parallel), "{models_dir}": copy.ModelsDir, "{models_max}": strconv.Itoa(copy.ModelsMax)}
		for k, v := range values {
			text = strings.ReplaceAll(text, k, quote(v))
		}
		args, splitErr := SplitArgs(text)
		if splitErr != nil {
			return "", splitErr
		}
		return displayCommand(exe, args), nil
	}
	return "", err
}

func (m *Manager) Start(cfg config.Config) error {
	exe, args, display, err := Build(cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.cmd != nil && m.cmd.ProcessState == nil {
		m.mu.Unlock()
		return errors.New("llama-server is already running")
	}
	cmd := background.HideConsole(exec.Command(exe, args...))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("start llama-server: %w", err)
	}
	m.cmd, m.startedAt, m.command = cmd, time.Now(), display
	m.endpoint = fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)
	m.lastError = ""
	m.stopping = false
	m.appendLocked("[manager] started: " + display)
	m.mu.Unlock()
	go m.capture(stdout, "stdout")
	go m.capture(stderr, "stderr")
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if m.stopping {
			m.lastError = ""
			m.appendLocked("[manager] stopped and model memory released")
		} else if err != nil {
			m.lastError = err.Error()
			m.appendLocked("[manager] exited: " + err.Error())
		} else {
			m.appendLocked("[manager] exited normally")
		}
		m.stopping = false
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.cmd == nil || m.cmd.Process == nil || m.cmd.ProcessState != nil {
		m.mu.Unlock()
		return errors.New("llama-server is not running")
	}
	process := m.cmd.Process
	m.stopping = true
	m.appendLocked("[manager] stopping process")
	m.mu.Unlock()
	// Windows-only release: terminate the complete process tree so router-mode
	// model workers cannot keep VRAM or model files alive after an unload-all.
	stop := background.HideConsole(exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F"))
	if output, err := stop.CombinedOutput(); err != nil {
		// The process may have exited between the status check and taskkill.
		m.mu.Lock()
		exited := m.cmd == nil || m.cmd.ProcessState != nil
		if !exited {
			m.stopping = false
		}
		m.mu.Unlock()
		if !exited {
			return fmt.Errorf("stop llama-server process tree: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		exited := m.cmd == nil || m.cmd.ProcessState != nil
		m.mu.Unlock()
		if exited {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	m.mu.Lock()
	m.stopping = false
	m.mu.Unlock()
	return errors.New("llama-server did not exit within 10 seconds")
}

// Shutdown is an idempotent process cleanup used when the gateway exits.
func (m *Manager) Shutdown() error {
	m.mu.Lock()
	running := m.cmd != nil && m.cmd.Process != nil && m.cmd.ProcessState == nil
	m.mu.Unlock()
	if !running {
		return nil
	}
	if err := m.Stop(); err != nil {
		m.mu.Lock()
		stillRunning := m.cmd != nil && m.cmd.Process != nil && m.cmd.ProcessState == nil
		m.mu.Unlock()
		if stillRunning {
			return err
		}
	}
	return nil
}

func (m *Manager) Restart(cfg config.Config) error {
	m.mu.Lock()
	running := m.cmd != nil && m.cmd.ProcessState == nil
	m.mu.Unlock()
	if running {
		if err := m.Stop(); err != nil {
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			done := m.cmd.ProcessState != nil
			m.mu.Unlock()
			if done {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return m.Start(cfg)
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	running := m.cmd != nil && m.cmd.ProcessState == nil
	status := Status{Running: running, Command: m.command, Endpoint: m.endpoint, LastError: m.lastError}
	if running {
		status.PID = m.cmd.Process.Pid
		startedAt := m.startedAt
		status.StartedAt = &startedAt
	}
	m.mu.Unlock()
	if running && status.Endpoint != "" {
		client := http.Client{Timeout: 700 * time.Millisecond}
		resp, err := client.Get(status.Endpoint + "/health")
		if err == nil {
			resp.Body.Close()
			status.Ready = healthReady(resp.StatusCode)
		}
	}
	return status
}

func healthReady(statusCode int) bool { return statusCode == http.StatusOK }

func (m *Manager) Logs(tail int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tail <= 0 || tail > len(m.logs) {
		tail = len(m.logs)
	}
	return append([]string(nil), m.logs[len(m.logs)-tail:]...)
}

func (m *Manager) ClearLogs() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = nil
}

// Target returns the active llama-server endpoint without performing a health
// request. It is intended for the streaming reverse proxy hot path.
func (m *Manager) Target() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	running := m.cmd != nil && m.cmd.ProcessState == nil
	return m.endpoint, running && m.endpoint != ""
}

// PID returns the managed process ID without performing a health request.
func (m *Manager) PID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.cmd.Process == nil || m.cmd.ProcessState != nil {
		return 0
	}
	return m.cmd.Process.Pid
}

func (m *Manager) Log(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendLocked("[manager] " + message)
}

func (m *Manager) capture(pipe interface{ Read([]byte) (int, error) }, source string) {
	s := bufio.NewScanner(pipe)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 1024*1024)
	for s.Scan() {
		m.mu.Lock()
		m.appendLocked("[" + source + "] " + s.Text())
		m.mu.Unlock()
	}
	if err := s.Err(); err != nil {
		m.mu.Lock()
		m.appendLocked("[" + source + "] capture error: " + err.Error())
		m.mu.Unlock()
	}
}

func (m *Manager) appendLocked(line string) {
	m.logs = append(m.logs, time.Now().Format("15:04:05")+" "+line)
	if len(m.logs) > m.maxLogs {
		m.logs = append([]string(nil), m.logs[len(m.logs)-m.maxLogs:]...)
	}
}

func SplitArgs(input string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quoteRune rune
	started := false
	flush := func() {
		if started {
			args = append(args, b.String())
			b.Reset()
			started = false
		}
	}
	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && quoteRune == '"' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
			b.WriteRune(runes[i+1])
			i++
			started = true
			continue
		}
		if r == '\'' || r == '"' {
			if quoteRune == 0 {
				quoteRune = r
				started = true
				continue
			}
			if quoteRune == r {
				quoteRune = 0
				continue
			}
		}
		if (r == ' ' || r == '\t' || r == '\n') && quoteRune == 0 {
			flush()
			continue
		}
		b.WriteRune(r)
		started = true
	}
	if quoteRune != 0 {
		return nil, errors.New("unterminated quote in arguments")
	}
	flush()
	return args, nil
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
func displayCommand(exe string, args []string) string {
	parts := []string{quote(exe)}
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\"") {
			parts = append(parts, quote(arg))
		} else {
			parts = append(parts, arg)
		}
	}
	return strings.Join(parts, " ")
}
