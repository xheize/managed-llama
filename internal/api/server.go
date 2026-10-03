package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"managed-llama/internal/config"
	"managed-llama/internal/hf"
	"managed-llama/internal/models"
	"managed-llama/internal/resources"
	"managed-llama/internal/runner"
)

type Server struct {
	mu         sync.RWMutex
	cfg        config.Config
	configPath string
	autostart  AutostartController
	runner     *runner.Manager
	resources  *resources.Manager
	hf         *hf.Client
	web        embed.FS
	// ConfigPolicy is installed before serving requests in privileged mode.
	ConfigPolicy func(config.Config) error
	Updates      UpdateController
}

type AutostartController interface {
	Enabled() (bool, error)
	Set(enabled bool) error
}

func New(cfg config.Config, configPath string, web embed.FS, autostart AutostartController) *Server {
	return &Server{cfg: cfg, configPath: configPath, autostart: autostart, runner: runner.New(), resources: resources.New(), hf: hf.New(cfg.HuggingFaceBase, cfg.HuggingFaceToken), web: web}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/updates", s.updateStatus)
	mux.HandleFunc("POST /api/updates/check", s.checkUpdate)
	mux.HandleFunc("POST /api/updates/install", s.installUpdate)
	mux.HandleFunc("GET /health/gateway", s.gatewayHealth)
	mux.HandleFunc("GET /health/llama", s.llamaHealth)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("GET /api/autostart", s.getAutostart)
	mux.HandleFunc("PUT /api/autostart", s.putAutostart)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("PUT /api/config", s.putConfig)
	mux.HandleFunc("DELETE /api/config/hugging-face-token", s.clearHFToken)
	mux.HandleFunc("GET /api/command", s.command)
	mux.HandleFunc("GET /api/server/status", s.status)
	mux.HandleFunc("POST /api/server/start", s.start)
	mux.HandleFunc("POST /api/server/stop", s.stop)
	mux.HandleFunc("POST /api/server/restart", s.restart)
	mux.HandleFunc("GET /api/server/logs", s.logs)
	mux.HandleFunc("DELETE /api/server/logs", s.clearLogs)
	mux.HandleFunc("GET /api/resources/snapshot", s.resourceSnapshot)
	mux.HandleFunc("GET /api/resources/events", s.resourceEvents)
	mux.HandleFunc("POST /api/resources/processes/{pid}/terminate", s.terminateProcess)
	mux.HandleFunc("GET /api/models", s.listModels)
	mux.HandleFunc("POST /api/models/upload", s.uploadModel)
	mux.HandleFunc("DELETE /api/models/{path...}", s.deleteModel)
	mux.HandleFunc("GET /api/hf/search", s.searchHF)
	mux.HandleFunc("GET /api/hf/files", s.hfFiles)
	mux.HandleFunc("POST /api/hf/downloads", s.downloadHF)
	mux.HandleFunc("GET /api/hf/downloads", s.downloads)
	// Every current and future OpenAI-compatible llama.cpp route is forwarded
	// transparently, including SSE streaming responses.
	mux.Handle("/v1", s.openAIProxy())
	mux.Handle("/v1/", s.openAIProxy())
	// Native llama.cpp router and diagnostic endpoints are available under a
	// namespaced path: /llama/models, /llama/models/load, /llama/health, etc.
	mux.Handle("/llama/", http.StripPrefix("/llama", s.llamaProxy()))
	root, _ := fs.Sub(s.web, "web")
	mux.Handle("/", http.FileServer(http.FS(root)))
	return requestLogger(securityHeaders(sameOriginControl(mux)))
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	// The token is stored in config.json for now, but never echoed back to the browser.
	cfg.HuggingFaceToken = ""
	writeJSON(w, 200, cfg)
}

func (s *Server) getAutostart(w http.ResponseWriter, _ *http.Request) {
	if s.autostart == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("Windows autostart is unavailable"))
		return
	}
	enabled, err := s.autostart.Enabled()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

func (s *Server) putAutostart(w http.ResponseWriter, r *http.Request) {
	if s.autostart == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("Windows autostart is unavailable"))
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, w, &input); err != nil {
		return
	}
	if err := s.autostart.Set(input.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": input.Enabled})
}

type serviceHealth struct {
	Service  string `json:"service"`
	Status   string `json:"status"`
	Healthy  bool   `json:"healthy"`
	Message  string `json:"message,omitempty"`
	PID      int    `json:"pid,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

func (s *Server) gatewayHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, serviceHealth{Service: "gateway", Status: "ok", Healthy: true})
}

func (s *Server) llamaHealth(w http.ResponseWriter, _ *http.Request) {
	runtime := s.runner.Status()
	health := serviceHealth{Service: "llama", PID: runtime.PID, Endpoint: runtime.Endpoint}
	statusCode := http.StatusServiceUnavailable
	switch {
	case runtime.Ready:
		health.Status = "ok"
		health.Healthy = true
		health.Message = "llama-server is ready"
		statusCode = http.StatusOK
	case runtime.Running:
		health.Status = "starting"
		health.Message = "llama-server is running but not ready"
	case runtime.LastError != "":
		health.Status = "error"
		health.Message = runtime.LastError
	default:
		health.Status = "stopped"
		health.Message = "llama-server is not running"
	}
	writeJSON(w, statusCode, health)
}
func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := decodeJSON(r, w, &cfg); err != nil {
		return
	}
	s.mu.RLock()
	previousToken := s.cfg.HuggingFaceToken
	s.mu.RUnlock()
	if cfg.HuggingFaceToken == "" {
		cfg.HuggingFaceToken = previousToken
	}
	cfg = config.ApplyEnv(cfg)
	if err := cfg.Validate(); err != nil {
		writeError(w, 400, err)
		return
	}
	if s.ConfigPolicy != nil {
		if err := s.ConfigPolicy(cfg); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if err := config.Save(s.configPath, cfg); err != nil {
		writeError(w, 500, err)
		return
	}
	s.mu.Lock()
	s.cfg = cfg
	s.hf.Configure(cfg.HuggingFaceBase, cfg.HuggingFaceToken)
	s.mu.Unlock()
	writeJSON(w, 200, map[string]string{"message": "configuration saved"})
}
func (s *Server) clearHFToken(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	cfg.HuggingFaceToken = ""
	cfg = config.ApplyEnv(cfg)
	if err := config.Save(s.configPath, cfg); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.cfg = cfg
	s.hf.Configure(cfg.HuggingFaceBase, cfg.HuggingFaceToken)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"message": "Hugging Face token cleared"})
}
func (s *Server) command(w http.ResponseWriter, _ *http.Request) {
	cfg := s.config()
	preview, err := runner.Preview(cfg)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]string{"command": preview})
}
func (s *Server) state(w http.ResponseWriter, _ *http.Request)  { writeJSON(w, 200, s.gatewayState()) }
func (s *Server) status(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, s.runner.Status()) }
func (s *Server) start(w http.ResponseWriter, _ *http.Request) {
	if err := s.StartServer(); err != nil {
		writeError(w, 409, err)
		return
	}
	writeJSON(w, 202, map[string]string{"message": "llama-server started"})
}
func (s *Server) stop(w http.ResponseWriter, _ *http.Request) {
	if err := s.StopServer(); err != nil {
		writeError(w, 409, err)
		return
	}
	writeJSON(w, 202, map[string]string{"message": "llama-server stopped"})
}
func (s *Server) restart(w http.ResponseWriter, _ *http.Request) {
	if err := s.requireStartReady(); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	cfg := s.config()
	if s.ConfigPolicy != nil {
		if err := s.ConfigPolicy(cfg); err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}
	}
	if err := s.runner.Restart(cfg); err != nil {
		writeError(w, 409, err)
		return
	}
	go s.loadDefaultModel(cfg)
	writeJSON(w, 202, map[string]string{"message": "llama-server restarted"})
}
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail == 0 {
		tail = 300
	}
	writeJSON(w, 200, map[string]any{"lines": s.runner.Logs(tail)})
}
func (s *Server) clearLogs(w http.ResponseWriter, _ *http.Request) {
	s.runner.ClearLogs()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resourceSnapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.resources.Snapshot(s.runner.PID()))
}

func (s *Server) resourceEvents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"events": s.resources.Events()})
}

func (s *Server) terminateProcess(w http.ResponseWriter, r *http.Request) {
	pidValue, err := strconv.ParseUint(r.PathValue("pid"), 10, 32)
	if err != nil || pidValue == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("유효한 PID가 필요합니다"))
		return
	}
	var input struct {
		StartedAt time.Time `json:"started_at"`
		Tree      bool      `json:"tree"`
	}
	if err := decodeJSON(r, w, &input); err != nil {
		return
	}
	if err := s.resources.Terminate(uint32(pidValue), input.StartedAt, input.Tree, s.runner.PID()); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "프로세스 종료 요청을 처리했습니다"})
}

// StartServer starts llama-server after applying the same readiness checks used
// by the HTTP API. It is also used by the Windows system tray.
func (s *Server) StartServer() error {
	if err := s.requireStartReady(); err != nil {
		return err
	}
	cfg := s.config()
	if s.ConfigPolicy != nil {
		if err := s.ConfigPolicy(cfg); err != nil {
			return err
		}
	}
	if err := s.runner.Start(cfg); err != nil {
		return err
	}
	go s.loadDefaultModel(cfg)
	return nil
}

// StopServer stops the complete llama-server process tree.
func (s *Server) StopServer() error { return s.runner.Stop() }

// RuntimeStatus returns the current llama-server process and health state.
func (s *Server) RuntimeStatus() runner.Status { return s.runner.Status() }

// CurrentState returns the gateway readiness state for native UI integrations.
func (s *Server) CurrentState() GatewayState { return s.gatewayState() }

// Shutdown releases the llama-server process tree before the gateway exits.
func (s *Server) Shutdown() error { return s.runner.Shutdown() }
func (s *Server) listModels(w http.ResponseWriter, _ *http.Request) {
	list, err := s.store().List()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, list)
}
func (s *Server) uploadModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 100<<30)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, 400, err)
		return
	}
	_, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, 400, err)
		return
	}
	model, err := s.store().SaveUpload(header)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 201, model)
}
func (s *Server) deleteModel(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("path")
	if rel == "" {
		writeError(w, 400, fmt.Errorf("model path is required"))
		return
	}
	if err := s.store().Delete(rel); err != nil {
		writeError(w, 400, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) searchHF(w http.ResponseWriter, r *http.Request) {
	if err := s.requireHF(); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := s.hfClient().Search(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeError(w, 502, err)
		return
	}
	writeJSON(w, 200, result)
}
func (s *Server) hfFiles(w http.ResponseWriter, r *http.Request) {
	if err := s.requireHF(); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeError(w, 400, fmt.Errorf("repo is required"))
		return
	}
	result, err := s.hfClient().Files(r.Context(), repo)
	if err != nil {
		writeError(w, 502, err)
		return
	}
	writeJSON(w, 200, result)
}
func (s *Server) downloadHF(w http.ResponseWriter, r *http.Request) {
	if err := s.requireHF(); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	var input struct {
		Repo string `json:"repo"`
		File string `json:"file"`
	}
	if err := decodeJSON(r, w, &input); err != nil {
		return
	}
	dest, err := s.store().Destination(filepath.Base(input.File))
	if err != nil {
		writeError(w, 400, err)
		return
	}
	job, err := s.hfClient().StartDownload(input.Repo, input.File, dest)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 202, job)
}
func (s *Server) downloads(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.hfClient().Jobs())
}
func (s *Server) config() config.Config { s.mu.RLock(); defer s.mu.RUnlock(); return s.cfg }
func (s *Server) store() models.Store   { return models.Store{Dir: s.config().ModelsDir} }
func (s *Server) hfClient() *hf.Client  { s.mu.RLock(); defer s.mu.RUnlock(); return s.hf }

func decodeJSON(r *http.Request, w http.ResponseWriter, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		writeError(w, 400, err)
		return err
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
func sameOriginControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
				writeError(w, http.StatusForbidden, fmt.Errorf("cross-site control requests are not allowed"))
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, r.Host) {
					writeError(w, http.StatusForbidden, fmt.Errorf("control request origin does not match the dashboard"))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
