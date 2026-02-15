package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"opencode-chat/internal/models"
	"opencode-chat/internal/sandbox"
)

func TestIntegrationTraceAuth(t *testing.T) {
	fmt.Println("=== AUTH CONFIG TRACE ===")

	// Step 1: Load auth config from home directory
	fmt.Println("STEP 1: Loading auth config from home directory")
	fmt.Println("------------------------------------------------")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	authPath := filepath.Join(homeDir, ".local", "share", "opencode", "auth.json")
	fmt.Printf("Auth file path: %s\n", authPath)

	if _, err := os.Stat(authPath); os.IsNotExist(err) {
		fmt.Printf("❌ Auth file does not exist at %s\n", authPath)
		t.Fatal("Auth file not found")
	}
	fmt.Println("✓ Auth file exists")

	authData, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("Failed to read auth file: %v", err)
	}
	fmt.Printf("✓ Auth file size: %d bytes\n", len(authData))

	var authConfig map[string]models.AuthConfig
	if err := json.Unmarshal(authData, &authConfig); err != nil {
		t.Fatalf("Failed to parse auth JSON: %v", err)
	}

	fmt.Printf("✓ Parsed auth config with %d providers:\n", len(authConfig))
	for provider, config := range authConfig {
		fmt.Printf("  - %s: type=%s", provider, config.Type)
		if config.Type == "api" && config.Key != "" {
			fmt.Printf(" (key=%d chars)", len(config.Key))
		} else if config.Type == "oauth" {
			hasAccess := config.Access != ""
			hasRefresh := config.Refresh != ""
			fmt.Printf(" (access=%v, refresh=%v)", hasAccess, hasRefresh)
		}
		fmt.Println()
	}

	// Step 2: Create temporary auth file
	fmt.Println("\nSTEP 2: Creating temporary auth file for sandbox")
	fmt.Println("------------------------------------------------")

	tmpFile, err := os.CreateTemp("", "opencode-auth-trace-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	fmt.Printf("Created temp file: %s\n", tmpFile.Name())

	authDataFormatted, err := json.MarshalIndent(authConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal auth config: %v", err)
	}

	if _, err := tmpFile.Write(authDataFormatted); err != nil {
		t.Fatalf("Failed to write temp auth file: %v", err)
	}
	tmpFile.Close()

	fmt.Printf("✓ Wrote %d bytes to temp file\n", len(authDataFormatted))

	// Step 3: Start a local Docker sandbox
	fmt.Println("\nSTEP 3: Starting Docker sandbox to test OpenCode")
	fmt.Println("------------------------------------------------")

	sb := sandbox.NewLocalDockerSandbox()

	fmt.Println("Starting sandbox with loaded auth config...")
	if err := sb.Start(authConfig); err != nil {
		t.Fatalf("Failed to start sandbox: %v", err)
	}
	defer func() {
		fmt.Println("\nCleaning up sandbox...")
		if err := sb.Stop(); err != nil {
			log.Printf("Warning: failed to stop sandbox: %v", err)
		}
	}()

	fmt.Printf("✓ Sandbox started on %s\n", sb.OpencodeURL())

	// Step 4: Query OpenCode for available providers
	fmt.Println("\nSTEP 4: Querying OpenCode for available providers")
	fmt.Println("------------------------------------------------")

	providersURL := fmt.Sprintf("%s/config/providers", sb.OpencodeURL())
	fmt.Printf("Fetching: %s\n", providersURL)

	resp, err := http.Get(providersURL)
	if err != nil {
		t.Fatalf("Failed to fetch providers: %v", err)
	}
	defer resp.Body.Close()

	var providersResp models.ProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&providersResp); err != nil {
		t.Fatalf("Failed to decode providers response: %v", err)
	}

	fmt.Printf("✓ OpenCode returned %d providers:\n", len(providersResp.Providers))
	fmt.Printf("  Default model: %s\n", providersResp.Default)

	for _, provider := range providersResp.Providers {
		fmt.Printf("\n  Provider: %s (%s)\n", provider.ID, provider.Name)
		fmt.Printf("    Models: %d available\n", len(provider.Models))
		for _, model := range provider.Models {
			fmt.Printf("      - %s: %s\n", model.ID, model.Name)
		}
	}

	// Step 5: Compare input vs output
	fmt.Println("\nSTEP 5: Analysis")
	fmt.Println("------------------------------------------------")

	authProviders := make(map[string]bool)
	for provider := range authConfig {
		authProviders[provider] = true
	}

	opencodeProviders := make(map[string]bool)
	for _, provider := range providersResp.Providers {
		opencodeProviders[provider.ID] = true
	}

	fmt.Printf("Auth config has %d providers\n", len(authProviders))
	fmt.Printf("OpenCode returned %d providers\n", len(opencodeProviders))

	fmt.Println("\nProviders in auth.json but NOT returned by OpenCode:")
	missingCount := 0
	for provider := range authProviders {
		if !opencodeProviders[provider] {
			fmt.Printf("  - %s\n", provider)
			missingCount++
		}
	}
	if missingCount == 0 {
		fmt.Println("  (none)")
	}

	fmt.Println("\nProviders returned by OpenCode but NOT in auth.json:")
	extraCount := 0
	for provider := range opencodeProviders {
		if !authProviders[provider] {
			fmt.Printf("  - %s\n", provider)
			extraCount++
		}
	}
	if extraCount == 0 {
		fmt.Println("  (none)")
	}

	// Step 6: Test actual model fetching
	fmt.Println("\nSTEP 6: Testing getAllModels() equivalent")
	fmt.Println("------------------------------------------------")

	var modelList []models.ModelOption
	for _, provider := range providersResp.Providers {
		for _, model := range provider.Models {
			modelList = append(modelList, models.ModelOption{
				Value: fmt.Sprintf("%s/%s", provider.ID, model.ID),
				Label: fmt.Sprintf("%s - %s", provider.Name, model.Name),
			})
		}
	}

	fmt.Printf("Total models available in dropdown: %d\n", len(modelList))
	if len(modelList) <= 10 {
		fmt.Println("All models:")
		for _, model := range modelList {
			fmt.Printf("  - %s: %s\n", model.Value, model.Label)
		}
	} else {
		fmt.Println("First 10 models:")
		for i := 0; i < 10 && i < len(modelList); i++ {
			fmt.Printf("  - %s: %s\n", modelList[i].Value, modelList[i].Label)
		}
		fmt.Printf("  ... and %d more\n", len(modelList)-10)
	}

	fmt.Println("\n=== TRACE COMPLETE ===")
}
