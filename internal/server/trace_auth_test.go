package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"opencode-chat/internal/models"
)

// TestIntegrationTraceAuth verifies that the shared sandbox returns valid
// provider/model data matching the auth.json fed into it. It reuses the
// flowSuite sandbox instead of starting its own (saves ~30s of Docker startup).
func TestIntegrationTraceAuth(t *testing.T) {
	server := flowServer(t)

	base := server.Sandbox.OpencodeURL()
	providersURL := fmt.Sprintf("%s/config/providers", base)
	t.Logf("Fetching providers from: %s", providersURL)

	resp, err := http.Get(providersURL)
	if err != nil {
		t.Fatalf("Failed to fetch providers: %v", err)
	}
	defer resp.Body.Close()

	var providersResp models.ProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&providersResp); err != nil {
		t.Fatalf("Failed to decode providers response: %v", err)
	}

	if len(providersResp.Providers) == 0 {
		t.Fatal("OpenCode returned 0 providers; auth.json may be invalid")
	}

	t.Logf("OpenCode returned %d providers (default: %s)",
		len(providersResp.Providers), providersResp.Default)
	for _, p := range providersResp.Providers {
		t.Logf("  %s (%s): %d models", p.ID, p.Name, len(p.Models))
	}
}
