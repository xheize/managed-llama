package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"managed-llama/internal/updates"
)

type UpdateController interface {
	Status(string) updates.Status
	Check(context.Context, string) (updates.Status, error)
	Install(string, string) error
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	if !localUpdateRequest(w, r) {
		return
	}
	if s.Updates == nil {
		writeError(w, 503, errors.New("이 실행 환경에서는 업데이트를 사용할 수 없습니다"))
		return
	}
	status := s.Updates.Status(s.config().UpdateRepository)
	if status.Phase == "restarting" {
		w.Header().Set("X-Managed-Llama-Update", "restarting")
	}
	writeJSON(w, 200, status)
}
func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request) {
	if !localUpdateRequest(w, r) {
		return
	}
	if s.Updates == nil {
		writeError(w, 503, errors.New("업데이트를 사용할 수 없습니다"))
		return
	}
	status, err := s.Updates.Check(r.Context(), s.config().UpdateRepository)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 200, status)
}
func (s *Server) installUpdate(w http.ResponseWriter, r *http.Request) {
	if !localUpdateRequest(w, r) {
		return
	}
	if s.Updates == nil {
		writeError(w, 503, errors.New("업데이트를 사용할 수 없습니다"))
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := decodeJSON(r, w, &input); err != nil {
		return
	}
	if err := s.Updates.Install(s.config().UpdateRepository, input.Version); err != nil {
		writeError(w, 409, err)
		return
	}
	writeJSON(w, 202, s.Updates.Status(s.config().UpdateRepository))
}

func localUpdateRequest(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	if (!strings.EqualFold(host, "localhost") && !net.ParseIP(host).IsLoopback()) || !net.ParseIP(remote).IsLoopback() {
		writeError(w, http.StatusForbidden, errors.New("업데이트는 로컬 대시보드에서만 가능합니다"))
		return false
	}
	return true
}
