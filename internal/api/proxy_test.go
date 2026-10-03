package api

import (
	"bufio"
	"fmt"
	"managed-llama/internal/serviceauth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeControlRejectsCrossSiteBeforeProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	proxy := dynamicReverseProxy(func() (string, bool) { return upstream.URL, true }, false, nil)
	handler := serviceauth.RequireLocalHost(sameOriginControl(http.StripPrefix("/llama", proxy)))
	for _, route := range []string{"/llama/models/load", "/llama/models/unload"} {
		for _, test := range []struct {
			origin, site string
			want         int
		}{
			{"https://untrusted.example", "cross-site", http.StatusForbidden},
			{"https://untrusted.example", "", http.StatusForbidden},
			{"", "cross-site", http.StatusForbidden},
			{"http://127.0.0.1:3030", "same-origin", http.StatusNoContent},
			{"", "", http.StatusNoContent},
		} {
			r := httptest.NewRequest("POST", "http://127.0.0.1:3030"+route, strings.NewReader(`{"model":"example"}`))
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Sec-Fetch-Site", test.site)
			r.Header.Set("Content-Type", "text/plain")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("%s origin=%q site=%q: %d", route, test.origin, test.site, w.Code)
			}
		}
	}
}

func TestOpenAIProxyPreservesPathBodyAndStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.URL.Query().Get("trace") != "1" {
			t.Errorf("unexpected upstream URL: %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("authorization header was not forwarded")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, chunk := range []string{"data: one\n\n", "data: [DONE]\n\n"} {
			_, _ = fmt.Fprint(w, chunk)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	handler := corsOpenAI(dynamicReverseProxy(func() (string, bool) { return upstream.URL, true }, true, nil))
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?trace=1", strings.NewReader(`{"stream":true}`))
	request.Header.Set("Authorization", "Bearer test")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	scanner := bufio.NewScanner(recorder.Body)
	if !scanner.Scan() || scanner.Text() != "data: one" {
		t.Fatalf("unexpected SSE body: %q", recorder.Body.String())
	}
}

func TestOpenAIProxyUnavailableAndPreflight(t *testing.T) {
	handler := corsOpenAI(dynamicReverseProxy(func() (string, bool) { return "", false }, true, nil))

	offline := httptest.NewRecorder()
	handler.ServeHTTP(offline, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if offline.Code != http.StatusServiceUnavailable || !strings.Contains(offline.Body.String(), "llama_server_unavailable") {
		t.Fatalf("unexpected offline response: %d %s", offline.Code, offline.Body.String())
	}

	preflightRequest := httptest.NewRequest(http.MethodOptions, "/v1/responses", nil)
	preflightRequest.Header.Set("Origin", "https://app.example")
	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, preflightRequest)
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("unexpected preflight response: %d %#v", preflight.Code, preflight.Header())
	}
}

func TestSameOriginControlRejectsCrossSiteMutation(t *testing.T) {
	called := false
	handler := sameOriginControl(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3030/api/server/stop", nil)
	request.Header.Set("Origin", "https://attacker.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || called {
		t.Fatalf("cross-site request status=%d called=%v", recorder.Code, called)
	}

	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3030/api/server/stop", nil)
	request.Header.Set("Origin", "http://127.0.0.1:3030")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("same-origin request status=%d called=%v", recorder.Code, called)
	}
}
