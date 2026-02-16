package server

import (
	"strings"
	"testing"

	"opencode-chat/internal/templates"
)

// TestUnitStreamingCSS verifies that the streaming animation CSS exists.
// This is a static file content check that is not suitable for property-based testing.
func TestUnitStreamingCSS(t *testing.T) {
	cssBytes, err := templates.StaticFS.ReadFile("static/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	if !strings.Contains(css, ".streaming") {
		t.Error("CSS should contain .streaming selector")
	}
	if !strings.Contains(css, "@keyframes dots") {
		t.Error("CSS should contain @keyframes dots animation")
	}
	if !strings.Contains(css, "::after") {
		t.Error("CSS should contain ::after pseudo-element for streaming animation")
	}
}
