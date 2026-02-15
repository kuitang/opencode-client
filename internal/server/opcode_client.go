package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"opencode-chat/internal/models"
)

// opencodeGet performs a GET request to the OpenCode API and decodes the JSON response.
func (s *Server) opencodeGet(path string, result any) error {
	resp, err := http.Get(fmt.Sprintf("%s%s", s.Sandbox.OpencodeURL(), path))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// opencodePost performs a POST request to the OpenCode API with JSON payload.
func (s *Server) opencodePost(path string, payload any) (*http.Response, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return http.Post(
		fmt.Sprintf("%s%s", s.Sandbox.OpencodeURL(), path),
		"application/json",
		bytes.NewReader(jsonData),
	)
}

// loadProviders fetches provider configuration from the OpenCode sandbox.
func (s *Server) loadProviders() error {
	resp, err := http.Get(fmt.Sprintf("%s/config/providers", s.Sandbox.OpencodeURL()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var providersResp models.ProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&providersResp); err != nil {
		return err
	}

	s.providers = providersResp.Providers
	s.defaultModel = providersResp.Default
	return nil
}

// getAllModels returns a sorted list of all available models.
func (s *Server) getAllModels() []models.ModelOption {
	var modelList []models.ModelOption

	for _, provider := range s.providers {
		for _, model := range provider.Models {
			modelList = append(modelList, models.ModelOption{
				Value: fmt.Sprintf("%s/%s", provider.ID, model.ID),
				Label: fmt.Sprintf("%s - %s", provider.Name, model.Name),
			})
		}
	}

	sort.Slice(modelList, func(i, j int) bool {
		return modelList[i].Value < modelList[j].Value
	})

	return modelList
}

// executeShellCommand executes a shell command via OpenCode API and returns the output.
func (s *Server) executeShellCommand(sessionID, command string) (string, error) {
	shellURL := fmt.Sprintf("/session/%s/shell", sessionID)
	payload := map[string]string{"agent": "agent", "command": command}

	resp, err := s.opencodePost(shellURL, payload)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("command failed with status %d", resp.StatusCode)
	}

	var result models.MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	for _, part := range result.Parts {
		if part.Type == "tool" && part.Tool == "bash" {
			if output, ok := part.State["output"].(string); ok {
				return output, nil
			}
		}
	}
	return "", nil
}
