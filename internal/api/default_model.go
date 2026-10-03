package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"managed-llama/internal/config"
)

func (s *Server) loadDefaultModel(cfg config.Config) {
	if cfg.SelectedModel == "" || strings.Contains(cfg.Arguments, "{model}") {
		return
	}
	targetModel := cfg.SelectedModel
	if !filepath.IsAbs(targetModel) {
		targetModel = filepath.Join(cfg.ModelsDir, filepath.FromSlash(targetModel))
	}
	targetModel, _ = filepath.Abs(targetModel)
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		endpoint, running := s.runner.Target()
		if !running {
			return
		}
		response, err := client.Get(endpoint + "/models")
		if err == nil && response.StatusCode == http.StatusOK {
			var payload struct {
				Data []struct {
					ID   string `json:"id"`
					Path string `json:"path"`
				} `json:"data"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&payload)
			response.Body.Close()
			if decodeErr == nil {
				for _, model := range payload.Data {
					if sameModelReference(model.ID, model.Path, targetModel, cfg.SelectedModel) {
						body, _ := json.Marshal(map[string]string{"model": model.ID})
						loadResponse, loadErr := client.Post(endpoint+"/models/load", "application/json", bytes.NewReader(body))
						if loadErr == nil {
							loadResponse.Body.Close()
							if loadResponse.StatusCode >= 200 && loadResponse.StatusCode < 300 {
								s.runner.Log("default model load requested: " + model.ID)
								return
							}
						}
					}
				}
			}
		} else if response != nil {
			response.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.runner.Log(fmt.Sprintf("default model was not discovered by router: %s", cfg.SelectedModel))
}

func sameModelReference(routerID, routerPath, absoluteTarget, selected string) bool {
	if routerPath != "" {
		routerAbsolute, _ := filepath.Abs(routerPath)
		if strings.EqualFold(filepath.Clean(routerAbsolute), filepath.Clean(absoluteTarget)) {
			return true
		}
		if strings.EqualFold(filepath.Base(routerPath), filepath.Base(selected)) {
			return true
		}
	}
	selectedID := filepath.ToSlash(filepath.Clean(selected))
	selectedID = strings.TrimSuffix(selectedID, filepath.Ext(selectedID))
	routerID = filepath.ToSlash(filepath.Clean(routerID))
	return strings.EqualFold(routerID, selectedID) || strings.EqualFold(filepath.Base(routerID), filepath.Base(selectedID))
}
