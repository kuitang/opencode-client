package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sseEvent represents a parsed SSE data payload. If the payload is valid JSON
// it is stored in Parsed; otherwise Raw holds the original string.
type sseEvent struct {
	Parsed interface{}
	Raw    string
}

// collectSSEEvents connects to an SSE endpoint and collects events for up to
// the given timeout duration. This is a pure HTTP test -- no browser required.
func collectSSEEvents(hostname string, port int, path string, timeout time.Duration) ([]sseEvent, error) {
	url := fmt.Sprintf("http://%s:%d%s", hostname, port, path)
	client := &http.Client{Timeout: 0} // no client-level timeout; we manage our own
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var events []sseEvent
	done := make(chan struct{})

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				raw := strings.TrimPrefix(line, "data: ")
				var parsed interface{}
				if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
					events = append(events, sseEvent{Parsed: parsed})
				} else {
					events = append(events, sseEvent{Raw: raw})
				}
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		resp.Body.Close() // force the scanner goroutine to exit
		<-done
	}

	return events, nil
}

func TestSSEStream(t *testing.T) {
	ssePort := os.Getenv("PLAYWRIGHT_SSE_PORT")
	if ssePort == "" {
		t.Skip("Set PLAYWRIGHT_SSE_PORT to validate SSE stream")
	}

	ssePath := os.Getenv("PLAYWRIGHT_SSE_PATH")
	if ssePath == "" {
		ssePath = "/event"
	}

	var port int
	_, err := fmt.Sscanf(ssePort, "%d", &port)
	require.NoError(t, err, "PLAYWRIGHT_SSE_PORT must be a valid integer")

	t.Run("returns structured SSE payloads", func(t *testing.T) {
		events, err := collectSSEEvents("localhost", port, ssePath, 10*time.Second)
		require.NoError(t, err)
		require.Greater(t, len(events), 0, "expected at least one SSE event")

		hasObject := false
		for _, ev := range events {
			if ev.Parsed != nil {
				if _, ok := ev.Parsed.(map[string]interface{}); ok {
					hasObject = true
					break
				}
			}
		}
		require.True(t, hasObject, "expected at least one structured (object) SSE payload")
	})
}
