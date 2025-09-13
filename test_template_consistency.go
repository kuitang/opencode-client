//go:build ignore
// +build ignore

// Standalone test for template consistency
// Run with: go run test_template_consistency.go

package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"os"
	"strings"
)

//go:embed templates/*
var templatesFS embed.FS

func loadTemplates() (*template.Template, error) {
	// Define template functions
	funcMap := template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"sanitizeID": func(s string) string {
			return strings.ReplaceAll(s, " ", "-")
		},
	}

	return template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html", "templates/*/*.html")
}

func main() {
	templates, err := loadTemplates()
	if err != nil {
		fmt.Printf("Failed to load templates: %v\n", err)
		os.Exit(1)
	}

	// Test data for rendering
	testData := struct {
		FileCount   int
		LineCount   int
		Files       []FileNode
		CurrentPath string
	}{
		FileCount: 42,
		LineCount: 1337,
		Files: []FileNode{
			{Path: "main.go"},
			{Path: "test.go"},
		},
		CurrentPath: "main.go",
	}

	success := true

	// Test 1: File count consistency
	fmt.Println("Testing file count consistency...")
	var directFileBuf bytes.Buffer
	err = templates.ExecuteTemplate(&directFileBuf, "file-count-content", testData)
	if err != nil {
		fmt.Printf("  ❌ Failed to render file-count-content: %v\n", err)
		success = false
	} else {
		directFileHTML := strings.TrimSpace(directFileBuf.String())

		var withCountsBuf bytes.Buffer
		err = templates.ExecuteTemplate(&withCountsBuf, "file-options-with-counts", testData)
		if err != nil {
			fmt.Printf("  ❌ Failed to render file-options-with-counts: %v\n", err)
			success = false
		} else {
			withCountsHTML := withCountsBuf.String()
			startMarker := `<div id="file-count-container" hx-swap-oob="innerHTML">`
			endMarker := `</div>`

			startIdx := strings.Index(withCountsHTML, startMarker)
			if startIdx == -1 {
				fmt.Printf("  ❌ Could not find file-count-container\n")
				success = false
			} else {
				startIdx += len(startMarker)
				endIdx := strings.Index(withCountsHTML[startIdx:], endMarker)
				if endIdx == -1 {
					fmt.Printf("  ❌ Could not find closing div\n")
					success = false
				} else {
					extractedHTML := strings.TrimSpace(withCountsHTML[startIdx : startIdx+endIdx])

					if directFileHTML == extractedHTML {
						fmt.Printf("  ✅ File count HTML is consistent\n")
						fmt.Printf("     HTML: %q\n", directFileHTML)
					} else {
						fmt.Printf("  ❌ File count HTML mismatch\n")
						fmt.Printf("     Direct:    %q\n", directFileHTML)
						fmt.Printf("     Extracted: %q\n", extractedHTML)
						success = false
					}
				}
			}
		}
	}

	// Test 2: Line count consistency
	fmt.Println("\nTesting line count consistency...")
	var directLineBuf bytes.Buffer
	err = templates.ExecuteTemplate(&directLineBuf, "line-count-content", testData)
	if err != nil {
		fmt.Printf("  ❌ Failed to render line-count-content: %v\n", err)
		success = false
	} else {
		directLineHTML := strings.TrimSpace(directLineBuf.String())

		var withCountsBuf bytes.Buffer
		err = templates.ExecuteTemplate(&withCountsBuf, "file-options-with-counts", testData)
		if err != nil {
			fmt.Printf("  ❌ Failed to render file-options-with-counts: %v\n", err)
			success = false
		} else {
			withCountsHTML := withCountsBuf.String()
			startMarker := `<div id="line-count-container" hx-swap-oob="innerHTML">`
			endMarker := `</div>`

			startIdx := strings.Index(withCountsHTML, startMarker)
			if startIdx == -1 {
				fmt.Printf("  ❌ Could not find line-count-container\n")
				success = false
			} else {
				startIdx += len(startMarker)
				endIdx := strings.Index(withCountsHTML[startIdx:], endMarker)
				if endIdx == -1 {
					fmt.Printf("  ❌ Could not find closing div\n")
					success = false
				} else {
					extractedHTML := strings.TrimSpace(withCountsHTML[startIdx : startIdx+endIdx])

					if directLineHTML == extractedHTML {
						fmt.Printf("  ✅ Line count HTML is consistent\n")
						fmt.Printf("     HTML: %q\n", directLineHTML)
					} else {
						fmt.Printf("  ❌ Line count HTML mismatch\n")
						fmt.Printf("     Direct:    %q\n", directLineHTML)
						fmt.Printf("     Extracted: %q\n", extractedHTML)
						success = false
					}
				}
			}
		}
	}

	// Test 3: File options consistency
	fmt.Println("\nTesting file options consistency...")
	var directOptionsBuf bytes.Buffer
	err = templates.ExecuteTemplate(&directOptionsBuf, "file-options-content", testData)
	if err != nil {
		fmt.Printf("  ❌ Failed to render file-options-content: %v\n", err)
		success = false
	} else {
		directOptionsHTML := strings.TrimSpace(directOptionsBuf.String())

		var optionsBuf bytes.Buffer
		err = templates.ExecuteTemplate(&optionsBuf, "file-options", testData)
		if err != nil {
			fmt.Printf("  ❌ Failed to render file-options: %v\n", err)
			success = false
		} else {
			optionsHTML := strings.TrimSpace(optionsBuf.String())

			if directOptionsHTML == optionsHTML {
				fmt.Printf("  ✅ File options HTML is consistent\n")
				fmt.Printf("     Contains main.go: %v\n", strings.Contains(directOptionsHTML, "main.go"))
				fmt.Printf("     Contains test.go: %v\n", strings.Contains(directOptionsHTML, "test.go"))
				fmt.Printf("     Has selected: %v\n", strings.Contains(directOptionsHTML, "selected"))
			} else {
				fmt.Printf("  ❌ File options HTML mismatch\n")
				fmt.Printf("     Direct:       %q\n", directOptionsHTML)
				fmt.Printf("     Via template: %q\n", optionsHTML)
				success = false
			}
		}
	}

	// Test 4: OOB template uses factored templates
	fmt.Println("\nTesting OOB template structure...")
	var oobBuf bytes.Buffer
	err = templates.ExecuteTemplate(&oobBuf, "code-updates-oob", testData)
	if err != nil {
		fmt.Printf("  ❌ Failed to render code-updates-oob: %v\n", err)
		success = false
	} else {
		oobHTML := oobBuf.String()

		checks := []struct {
			name     string
			contains string
		}{
			{"Contains file count 42", "42"},
			{"Contains line count 1337", "1337"},
			{"Contains main.go", "main.go"},
			{"Contains test.go", "test.go"},
			{"Has OOB attributes", `hx-swap-oob="innerHTML"`},
			{"Has file-count-container", `id="file-count-container"`},
			{"Has line-count-container", `id="line-count-container"`},
		}

		allPassed := true
		for _, check := range checks {
			if strings.Contains(oobHTML, check.contains) {
				fmt.Printf("  ✅ %s\n", check.name)
			} else {
				fmt.Printf("  ❌ %s\n", check.name)
				allPassed = false
			}
		}

		if !allPassed {
			success = false
		}
	}

	fmt.Println("\n========================================")
	if success {
		fmt.Println("✅ All template consistency tests passed!")
		os.Exit(0)
	} else {
		fmt.Println("❌ Some tests failed")
		os.Exit(1)
	}
}

type FileNode struct {
	Path string
}
