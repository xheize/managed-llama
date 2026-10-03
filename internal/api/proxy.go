package api

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func (s *Server) openAIProxy() http.Handler {
	return corsOpenAI(s.reverseProxy(true))
}

func (s *Server) llamaProxy() http.Handler {
	return s.reverseProxy(false)
}

func (s *Server) reverseProxy(openAIError bool) http.Handler {
	return dynamicReverseProxy(s.runner.Target, openAIError, func() string {
		state := s.gatewayState()
		return state.Phase + ": " + state.Message
	})
}

func dynamicReverseProxy(targetFn func() (string, bool), openAIError bool, unavailableMessage func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint, running := targetFn()
		if !running {
			message := "llama-server is not running"
			if unavailableMessage != nil {
				message = unavailableMessage()
			}
			proxyError(w, http.StatusServiceUnavailable, message, openAIError)
			return
		}
		target, err := url.Parse(endpoint)
		if err != nil {
			proxyError(w, http.StatusBadGateway, "invalid llama-server endpoint", openAIError)
			return
		}
		proxy := &httputil.ReverseProxy{
			Rewrite: func(request *httputil.ProxyRequest) {
				request.SetURL(target)
				request.SetXForwarded()
				request.Out.Host = target.Host
			},
			// Flush each SSE/token chunk instead of buffering it in the manager.
			FlushInterval: -1,
			ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, proxyErr error) {
				proxyError(rw, http.StatusBadGateway, proxyErr.Error(), openAIError)
			},
			ModifyResponse: func(response *http.Response) error {
				response.Header.Set("X-Managed-Llama-Target", target.Host)
				return nil
			},
		}
		proxy.ServeHTTP(w, r)
	})
}

func corsOpenAI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// The dashboard only listens on loopback by default. This permissive
			// OpenAI data-plane CORS behavior matches llama-server's default.
			w.Header().Set("Access-Control-Allow-Origin", "*")
			allowedHeaders := r.Header.Get("Access-Control-Request-Headers")
			if allowedHeaders == "" {
				allowedHeaders = "Authorization, Content-Type, OpenAI-Beta"
			}
			w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func proxyError(w http.ResponseWriter, status int, message string, openAI bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if openAI {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"message": message,
			"type":    "server_error",
			"param":   nil,
			"code":    "llama_server_unavailable",
		}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
