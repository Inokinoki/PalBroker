package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// setupMockSessionFiles creates mock Claude session files for testing session recovery.
// Files are created in the correct directory structure for the session_handler Claude provider.
func setupMockSessionFiles(t *testing.T, baseDir, sessionID string, claudeLines []string) func() {
	t.Helper()

	projectDir := filepath.Join(baseDir, "projects", "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project directory: %v", err)
	}

	sessionFile := filepath.Join(projectDir, sessionID+".jsonl")
	file, err := os.Create(sessionFile)
	if err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}
	defer file.Close()

	for _, line := range claudeLines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	// Write sessions-index.json
	index := map[string]interface{}{
		"entries": []map[string]interface{}{
			{
				"sessionId": sessionID,
				"fullPath":  sessionFile,
			},
		},
	}
	indexJSON, _ := json.MarshalIndent(index, "", "  ")
	os.WriteFile(filepath.Join(projectDir, "sessions-index.json"), indexJSON, 0644)

	return func() {
		os.RemoveAll(filepath.Join(baseDir, "projects"))
	}
}

// TestRecoverSessionFromCLISuccess tests successful session recovery
func TestRecoverSessionFromCLISuccess(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t)
	defer cleanup()

	// Set provider and session ID
	mgr.SetProvider("claude")
	mgr.SetSessionID("test-session-123")

	// Create mock session events using Claude's actual session format
	claudeLines := []string{
		`{"type":"user","message":{"role":"user","content":"Hello, I need help with my code"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'd be happy to help with your code!"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Could you please share what specific issue you're facing?"}]}}`,
	}

	// Create the expected directory structure for ClaudeSessionReader
	projectsDir := filepath.Join(tmpDir, "projects")
	projectDir := filepath.Join(projectsDir, "test-project")
	sessionFile := filepath.Join(projectDir, "test-session-123.jsonl")
	indexFile := filepath.Join(projectDir, "sessions-index.json")

	// Create projects directory structure
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project directory: %v", err)
	}

	// Write session events to JSONL file
	file, err := os.Create(sessionFile)
	if err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}
	defer file.Close()

	for _, line := range claudeLines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	// Write sessions-index.json
	index := map[string]interface{}{
		"entries": []map[string]interface{}{
			{
				"sessionId": "test-session-123",
				"fullPath":  sessionFile,
			},
		},
	}
	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal index: %v", err)
	}
	if err := os.WriteFile(indexFile, indexJSON, 0644); err != nil {
		t.Fatalf("Failed to write index file: %v", err)
	}

	// Cleanup function
	sessionCleanup := func() {
		os.RemoveAll(filepath.Join(tmpDir, "projects"))
	}
	defer sessionCleanup()

	// Test recovery
	recovered, err := mgr.RecoverSessionFromCLI()

	if err != nil {
		t.Fatalf("Failed to recover session: %v", err)
	}

	if len(recovered) != 3 {
		t.Errorf("Expected 3 events, got %d", len(recovered))
	}

	// Verify recovered events match expected roles (user, assistant, assistant)
	expectedTypes := []string{"user", "assistant", "assistant"}
	for i, event := range recovered {
		expectedSeq := int64(i + 1)
		if event.Seq != expectedSeq {
			t.Errorf("Event %d seq mismatch: expected %d, got %d", i, expectedSeq, event.Seq)
		}
		if i < len(expectedTypes) && event.Type != expectedTypes[i] {
			t.Errorf("Event %d type mismatch: expected %s, got %s", i, expectedTypes[i], event.Type)
		}
	}
}

// TestRecoverSessionFromCLIEmpty tests recovery from empty session
func TestRecoverSessionFromCLIEmpty(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t)
	defer cleanup()

	mgr.SetProvider("claude")
	mgr.SetSessionID("empty-session")

	sessionDir := tmpDir
	setupCleanup := setupMockSessionFiles(t, sessionDir, "empty-session", []string{})
	defer setupCleanup()

	recovered, err := mgr.RecoverSessionFromCLI()

	if err != nil {
		t.Fatalf("Failed to recover empty session: %v", err)
	}

	if len(recovered) != 0 {
		t.Errorf("Expected 0 events from empty session, got %d", len(recovered))
	}
}

// TestRecoverSessionFromCLINoProvider tests recovery when no provider is set
func TestRecoverSessionFromCLINoProvider(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Don't set provider or session ID
	recovered, err := mgr.RecoverSessionFromCLI()

	if err != nil {
		t.Fatalf("Expected no error when no provider/session, got: %v", err)
	}

	if recovered != nil {
		t.Error("Expected nil result when no provider/session")
	}
}

// TestRecoverSessionFromCLINonExistent tests recovery when session doesn't exist
func TestRecoverSessionFromCLINonExistent(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	mgr.SetProvider("claude")
	mgr.SetSessionID("non-existent-session")

	recovered, err := mgr.RecoverSessionFromCLI()

	// Should not error, just return nil
	if err != nil {
		t.Fatalf("Expected no error for non-existent session, got: %v", err)
	}

	if recovered != nil {
		t.Error("Expected nil result for non-existent session")
	}
}

// TestGetIncrementalOutputWithRecovery tests incremental output with session recovery
func TestGetIncrementalOutputWithRecovery(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "recovery_task"
	mgr.CreateTask(taskID, "claude")

	// Set up session recovery
	mgr.SetProvider("claude")
	mgr.SetSessionID("recovery-test-session")

	// Create mock session with existing events using Claude's real format
	claudeLines := []string{
		`{"type":"user","message":{"role":"user","content":"First message"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Response to first message"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Continuation of response"}]}}`,
	}

	// Create the expected directory structure for ClaudeSessionReader
	projectsDir := filepath.Join(tmpDir, "projects")
	projectDir := filepath.Join(projectsDir, "recovery-test-project")
	sessionFile := filepath.Join(projectDir, "recovery-test-session.jsonl")
	indexFile := filepath.Join(projectDir, "sessions-index.json")

	// Create projects directory structure
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project directory: %v", err)
	}

	// Write session events to JSONL file
	file, err := os.Create(sessionFile)
	if err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}
	defer file.Close()

	for _, line := range claudeLines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	// Write sessions-index.json
	index := map[string]interface{}{
		"entries": []map[string]interface{}{
			{
				"sessionId": "recovery-test-session",
				"fullPath":  sessionFile,
			},
		},
	}
	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal index: %v", err)
	}
	if err := os.WriteFile(indexFile, indexJSON, 0644); err != nil {
		t.Fatalf("Failed to write index file: %v", err)
	}

	// Cleanup function
	cleanupFunc := func() {
		os.RemoveAll(filepath.Join(tmpDir, "projects"))
	}
	defer cleanupFunc()

	// Test 1: Get all events from beginning (recovery only, no cache)
	// Clear any existing cache first
	mgr.outputCache = make(map[string]*outputCache)

	events1, err := mgr.GetIncrementalOutput(taskID, 0)
	if err != nil {
		t.Fatalf("Failed to get incremental output: %v", err)
	}

	if len(events1) != 3 {
		t.Errorf("Expected 3 events from recovery, got %d", len(events1))
	}

	// Debug: Print recovered events
	t.Logf("Recovered %d events:", len(events1))
	for i, event := range events1 {
		t.Logf("  Event %d: seq=%d, type=%s", i, event.Seq, event.Type)
	}

	// Test 2: Add new events to cache and test incremental output
	// First, check current task state
	state, err := mgr.LoadState(taskID)
	if err != nil {
		t.Fatalf("Failed to load task state: %v", err)
	}
	t.Logf("Current task state: seq=%d", state.Seq)

	newEvent1 := Event{
		Type: "chunk",
		Data: map[string]string{"content": "New message 1"},
	}
	if err := mgr.AddOutput(taskID, newEvent1); err != nil {
		t.Fatalf("Failed to add first new event: %v", err)
	}
	t.Logf("Added first new event, new seq=%d", newEvent1.Seq)

	newEvent2 := Event{
		Type: "chunk",
		Data: map[string]string{"content": "New message 2"},
	}
	if err := mgr.AddOutput(taskID, newEvent2); err != nil {
		t.Fatalf("Failed to add second new event: %v", err)
	}
	t.Logf("Added second new event, new seq=%d", newEvent2.Seq)

	// Debug: Print cached events
	mgr.cacheMu.RLock()
	cache := mgr.outputCache[taskID]
	if cache != nil {
		t.Logf("Cache now has %d events:", len(cache.events))
		for i, event := range cache.events {
			t.Logf("  Cache event %d: seq=%d, type=%s", i, event.Seq, event.Type)
		}
	}
	mgr.cacheMu.RUnlock()

	// Test 3: Get incremental output after last known sequence (seq=3)
	// Should return only the new events (seq=4, 5)
	events2, err := mgr.GetIncrementalOutput(taskID, 3)
	if err != nil {
		t.Fatalf("Failed to get incremental output: %v", err)
	}

	t.Logf("Got %d events for GetIncrementalOutput(taskID, 3):", len(events2))
	for i, event := range events2 {
		t.Logf("  Event %d: seq=%d, type=%s", i, event.Seq, event.Type)
	}

	if len(events2) != 2 {
		t.Errorf("Expected 2 new events, got %d", len(events2))
	}

	// Check sequence numbers
	if len(events2) > 0 && events2[0].Seq != 4 {
		t.Errorf("Expected first new event seq=4, got %d", events2[0].Seq)
	}
	if len(events2) > 1 && events2[1].Seq != 5 {
		t.Errorf("Expected second new event seq=5, got %d", events2[1].Seq)
	}
}

// TestCachePopulationFromSession tests populating cache with recovered events
func TestCachePopulationFromSession(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "cache_pop_test"
	mgr.CreateTask(taskID, "claude")

	// Set up session recovery
	mgr.SetProvider("claude")
	mgr.SetSessionID("cache-pop-session")

	// Create session with many events using Claude's real format
	numEvents := 100

	// Create the expected directory structure for ClaudeSessionReader
	projectsDir := filepath.Join(tmpDir, "projects")
	projectDir := filepath.Join(projectsDir, "cache-pop-project")
	sessionFile := filepath.Join(projectDir, "cache-pop-session.jsonl")
	indexFile := filepath.Join(projectDir, "sessions-index.json")

	// Create projects directory structure
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project directory: %v", err)
	}

	// Write session events to JSONL file
	file, err := os.Create(sessionFile)
	if err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}
	defer file.Close()

	for i := 1; i <= numEvents; i++ {
		line := fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Event %d"}]}}`, i)
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatalf("Failed to write event: %v", err)
		}
	}

	// Write sessions-index.json
	index := map[string]interface{}{
		"entries": []map[string]interface{}{
			{
				"sessionId": "cache-pop-session",
				"fullPath":  sessionFile,
			},
		},
	}
	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal index: %v", err)
	}
	if err := os.WriteFile(indexFile, indexJSON, 0644); err != nil {
		t.Fatalf("Failed to write index file: %v", err)
	}

	// Cleanup function
	sessionCleanup := func() {
		os.RemoveAll(filepath.Join(tmpDir, "projects"))
	}
	defer sessionCleanup()

	// Trigger cache population by calling GetIncrementalOutput
	_, _ = mgr.GetIncrementalOutput(taskID, 0)

	// Verify cache is populated
	mgr.cacheMu.RLock()
	cache := mgr.outputCache[taskID]
	mgr.cacheMu.RUnlock()

	if cache == nil || len(cache.events) != numEvents {
		if cache == nil {
			t.Error("Expected cache to be populated, got nil")
		} else {
			t.Errorf("Expected %d events in cache, got %d", numEvents, len(cache.events))
		}
	}
}

// TestMixedSourceRecovery tests that recovery populates cache and subsequent reads use cache
func TestMixedSourceRecovery(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "mixed_source_test"
	mgr.CreateTask(taskID, "claude")

	// Set up session recovery
	mgr.SetProvider("claude")
	mgr.SetSessionID("mixed-source-session")

	// Create session events using Claude format
	claudeLines := []string{
		`{"type":"user","message":{"role":"user","content":"Message 1"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Response 1"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Response 2"}]}}`,
	}

	setupCleanup := setupMockSessionFiles(t, tmpDir, "mixed-source-session", claudeLines)
	defer setupCleanup()

	// First call: triggers recovery from CLI session
	events1, err := mgr.GetIncrementalOutput(taskID, 0)
	if err != nil {
		t.Fatalf("Failed to get incremental output: %v", err)
	}

	if len(events1) != 3 {
		t.Errorf("Expected 3 events from recovery, got %d", len(events1))
	}

	// Add new events to cache (seq continues from where recovery left off)
	for i := 4; i <= 5; i++ {
		event := Event{
			Type: "chunk",
			Data: map[string]string{"content": fmt.Sprintf("New message %d", i)},
		}
		mgr.AddOutput(taskID, event)
	}

	// Second call: should use cache (no recovery needed)
	events2, err := mgr.GetIncrementalOutput(taskID, 3)
	if err != nil {
		t.Fatalf("Failed to get incremental output: %v", err)
	}

	if len(events2) != 2 {
		t.Errorf("Expected 2 new events from cache, got %d", len(events2))
	}
}

// TestRecoveryWithCacheLimits tests session recovery with cache limits
func TestRecoveryWithCacheLimits(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t)
	defer cleanup()

	// Set strict cache limits
	mgr.maxEventsPerTask = 10
	mgr.maxTaskCount = 5
	mgr.maxTotalMemory = 1024 * 1024 // 1MB

	taskID := "limited_recovery_test"
	mgr.CreateTask(taskID, "claude")

	// Set up session recovery
	mgr.SetProvider("claude")
	mgr.SetSessionID("limited-session")

	// Create session with many events (more than cache can hold) using Claude format
	var claudeLines []string
	for i := 1; i <= 20; i++ {
		claudeLines = append(claudeLines, fmt.Sprintf(
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"This is a longer message number %d with more content to consume memory"}]}}`,
			i,
		))
	}

	setupCleanup := setupMockSessionFiles(t, tmpDir, "limited-session", claudeLines)
	defer setupCleanup()

	// Trigger recovery and cache population
	_, _ = mgr.GetIncrementalOutput(taskID, 0)

	// Should be limited by cache size
	mgr.cacheMu.RLock()
	cache := mgr.outputCache[taskID]
	mgr.cacheMu.RUnlock()

	if len(cache.events) > 10 {
		t.Errorf("Expected cache limited to 10 events, got %d", len(cache.events))
	}

	// Verify only the most recent events are kept
	if len(cache.events) > 0 {
		if cache.events[0].Seq != 11 { // Should have seq 11-20
			t.Errorf("Expected to keep most recent events, first event has seq %d", cache.events[0].Seq)
		}
	}
}
