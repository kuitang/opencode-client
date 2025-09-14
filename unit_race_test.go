package main

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------- Race/Concurrency: UpdateRateLimiter ----------------

func TestUpdateRateLimiter_FirstUpdateImmediate(t *testing.T) {
	var executed int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	start := time.Now()
	limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&executed, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 1 {
		t.Error("First update should be immediate")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("First update took too long: %v", elapsed)
	}
}

func TestUpdateRateLimiter_SecondUpdateDelayed(t *testing.T) {
	var executed int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&executed, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 1 {
		t.Fatal("First update should have executed")
	}
	time.Sleep(50 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&executed, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 1 {
		t.Error("Second update should still be delayed at 150ms")
	}
	time.Sleep(80 * time.Millisecond)
	if atomic.LoadInt32(&executed) != 2 {
		t.Errorf("Second update should execute after interval, got %d executions", atomic.LoadInt32(&executed))
	}
}

func TestUpdateRateLimiter_UpdateAfterInterval(t *testing.T) {
	var executed []time.Time
	var mu sync.Mutex
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, time.Now()); mu.Unlock() })
	time.Sleep(250 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, time.Now()); mu.Unlock() })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 2 {
		t.Fatalf("Expected 2 executions, got %d", len(executed))
	}
	if gap := executed[1].Sub(executed[0]); gap < 240*time.Millisecond || gap > 310*time.Millisecond {
		t.Errorf("Gap should be ~250ms, got %v", gap)
	}
}

func TestUpdateRateLimiter_RapidUpdatesCoalesce(t *testing.T) {
	var lastValue int32
	var executionCount int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { atomic.StoreInt32(&lastValue, 1); atomic.AddInt32(&executionCount, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&executionCount) != 1 {
		t.Fatal("First update should have executed")
	}
	for i := 2; i <= 5; i++ {
		value := int32(i)
		limiter.TryUpdate(context.Background(), func() { atomic.StoreInt32(&lastValue, value); atomic.AddInt32(&executionCount, 1) })
		time.Sleep(25 * time.Millisecond)
	}
	if atomic.LoadInt32(&executionCount) != 1 {
		t.Error("Intermediate updates should not have executed yet")
	}
	time.Sleep(100 * time.Millisecond)
	if finalValue := atomic.LoadInt32(&lastValue); finalValue != 5 {
		t.Errorf("Expected last value 5, got %d", finalValue)
	}
	if finalCount := atomic.LoadInt32(&executionCount); finalCount != 2 {
		t.Errorf("Expected 2 total executions (first + coalesced), got %d", finalCount)
	}
}

func TestUpdateRateLimiter_ConcurrentUpdates(t *testing.T) {
	var counter int32
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); limiter.TryUpdate(context.Background(), func() { atomic.AddInt32(&counter, 1) }) }()
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	if count1 := atomic.LoadInt32(&counter); count1 != 1 {
		t.Errorf("Expected 1 immediate execution from concurrent updates, got %d", count1)
	}
	time.Sleep(200 * time.Millisecond)
	if count2 := atomic.LoadInt32(&counter); count2 != 2 {
		t.Errorf("Expected 2 total executions after interval (immediate + coalesced), got %d", count2)
	}
}

func TestUpdateRateLimiter_TimerCancellation(t *testing.T) {
	var executed []int
	var mu sync.Mutex
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, 1); mu.Unlock() })
	time.Sleep(50 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, 2); mu.Unlock() })
	time.Sleep(50 * time.Millisecond)
	limiter.TryUpdate(context.Background(), func() { mu.Lock(); executed = append(executed, 3); mu.Unlock() })
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 2 {
		t.Errorf("Expected 2 executions, got %d", len(executed))
	}
	if len(executed) == 2 && executed[1] != 3 {
		t.Errorf("Expected second execution to be 3, got %d", executed[1])
	}
}

func TestUpdateRateLimiter_MultipleIntervals(t *testing.T) {
	var executionTimes []time.Time
	var mu sync.Mutex
	limiter := NewUpdateRateLimiter(200 * time.Millisecond)
	startTime := time.Now()
	timings := []time.Duration{0, 50 * time.Millisecond, 250 * time.Millisecond, 300 * time.Millisecond, 600 * time.Millisecond}
	for _, delay := range timings {
		time.Sleep(delay - time.Since(startTime))
		limiter.TryUpdate(context.Background(), func() { mu.Lock(); executionTimes = append(executionTimes, time.Now()); mu.Unlock() })
	}
	time.Sleep(700*time.Millisecond - time.Since(startTime))
	mu.Lock()
	defer mu.Unlock()
	if len(executionTimes) != 4 {
		t.Fatalf("Expected 4 executions, got %d", len(executionTimes))
	}
	tolerance := 60 * time.Millisecond
	if executionTimes[0].Sub(startTime) > tolerance {
		t.Errorf("First execution should be immediate, was at %v", executionTimes[0].Sub(startTime))
	}
	expected := 200 * time.Millisecond
	actual := executionTimes[1].Sub(startTime)
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Errorf("Second execution should be at ~200ms, was at %v", actual)
	}
	expected = 400 * time.Millisecond
	actual = executionTimes[2].Sub(startTime)
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Errorf("Third execution should be at ~400ms, was at %v", actual)
	}
	expected = 600 * time.Millisecond
	actual = executionTimes[3].Sub(startTime)
	if actual < expected-tolerance || actual > expected+tolerance {
		t.Errorf("Fourth execution should be at ~600ms, was at %v", actual)
	}
}

// ---------------- Race/Concurrency: SSE part updates and duplication ----------------

func TestSSEMessagePartNoDuplication(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_test123"
	partID := "prt_test456"
	if err := manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: "I'll analyze"}); err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}
	if err := manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: "I'll analyze OSCR's stock"}); err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}
	if err := manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: "I'll analyze OSCR's stock price over the last 6 months"}); err != nil {
		t.Fatalf("Failed to update part: %v", err)
	}
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected exactly 1 part, got %d parts", len(parts))
	}
	if parts[0].Content != "I'll analyze OSCR's stock price over the last 6 months" {
		t.Errorf("Expected final content, got: %s", parts[0].Content)
	}
	if parts[0].PartID != partID {
		t.Errorf("Expected partID %s, got %s", partID, parts[0].PartID)
	}
}

func TestSSEMultiplePartTypes(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_test789"
	manager.UpdatePart(messageID, "prt_text1", MessagePartData{Type: "text", Content: "Analyzing data"})
	manager.UpdatePart(messageID, "prt_tool1", MessagePartData{Type: "tool", Content: "Tool: webfetch\nStatus: running"})
	manager.UpdatePart(messageID, "prt_tool1", MessagePartData{Type: "tool", Content: "Tool: webfetch\nStatus: completed\nOutput: ..."})
	manager.UpdatePart(messageID, "prt_text2", MessagePartData{Type: "text", Content: "The analysis shows..."})
	parts := manager.GetParts(messageID)
	if len(parts) != 3 {
		t.Errorf("Expected 3 parts, got %d", len(parts))
	}
	expectedOrder := []string{"prt_text1", "prt_tool1", "prt_text2"}
	for i, part := range parts {
		if part.PartID != expectedOrder[i] {
			t.Errorf("Part %d: expected ID %s, got %s", i, expectedOrder[i], part.PartID)
		}
	}
	if parts[1].Content != "Tool: webfetch\nStatus: completed\nOutput: ..." {
		t.Errorf("Tool part not updated correctly: %s", parts[1].Content)
	}
}

func TestSSEHTMLGenerationNoDuplication(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_html_test"
	updates := []string{"The", "The file", "The file does", "The file does not", "The file does not exist"}
	partID := "prt_incremental"
	for _, content := range updates {
		manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: content})
	}
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected 1 part after incremental updates, got %d", len(parts))
	}
	part := parts[0]
	part.RenderedHTML = renderText(part.Content)
	htmlStr := string(part.RenderedHTML)
	if count := strings.Count(htmlStr, "The file does not exist"); count != 1 {
		t.Errorf("Text appears %d times in rendered HTML, expected 1", count)
		t.Logf("Rendered HTML: %s", htmlStr)
	}
}

func TestSSERapidUpdates(t *testing.T) {
	manager := NewMessagePartsManager()
	messageID := "msg_rapid"
	partID := "prt_rapid"
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			manager.UpdatePart(messageID, partID, MessagePartData{Type: "text", Content: strings.Repeat("a", i+1)})
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()
	<-done
	parts := manager.GetParts(messageID)
	if len(parts) != 1 {
		t.Errorf("Expected 1 part after rapid updates, got %d", len(parts))
	}
	if expectedLen := 100; len(parts[0].Content) != expectedLen {
		t.Errorf("Expected content length %d, got %d", expectedLen, len(parts[0].Content))
	}
}
