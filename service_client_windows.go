package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"managed-llama/internal/api"
	"managed-llama/internal/runner"
	"managed-llama/internal/serviceauth"
)

var errServiceAuthentication = errors.New("대시보드를 열어 서비스 관리 키로 인증하세요")

type serviceClient struct {
	baseURL string
	client  *http.Client
	key     func() (string, error)
}

func newServiceClient(listen string) *serviceClient {
	return newServiceClientForURL(localDashboardURL(listen))
}

func newServiceClientForURL(baseURL string) *serviceClient {
	return &serviceClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 2 * time.Second},
	}
}

func (c *serviceClient) StartServer() error {
	return c.request(http.MethodPost, "/api/server/start", nil)
}
func (c *serviceClient) StopServer() error {
	return c.request(http.MethodPost, "/api/server/stop", nil)
}

func (c *serviceClient) RuntimeStatus() runner.Status {
	var status runner.Status
	if err := c.request(http.MethodGet, "/api/server/status", &status); err != nil {
		status.LastError = err.Error()
	}
	return status
}

func (c *serviceClient) CurrentState() api.GatewayState {
	var state api.GatewayState
	if err := c.request(http.MethodGet, "/api/state", &state); err != nil {
		if errors.Is(err, errServiceAuthentication) {
			return api.GatewayState{Phase: "service_auth_required", Message: err.Error()}
		}
		return api.GatewayState{Phase: "service_unavailable", Message: err.Error()}
	}
	return state
}

func (c *serviceClient) request(method, path string, target any) error {
	request, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.key != nil {
		key, err := c.key()
		if err != nil {
			return err
		}
		request.Header.Set(serviceauth.Header, key)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("Managed Llama 서비스에 연결할 수 없습니다: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return errServiceAuthentication
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body)
		if body.Error == "" {
			body.Error = response.Status
		}
		return fmt.Errorf("서비스 요청 실패: %s", body.Error)
	}
	if target == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("서비스 응답 해석 실패: %w", err)
	}
	return nil
}
