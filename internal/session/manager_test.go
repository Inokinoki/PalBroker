package session

import (
	"sync"
	"testing"
)

// setupTestManager creates a test manager for in-memory session caching
func setupTestManager(t *testing.T, taskID, provider string) (*Manager, func()) {
	t.Helper()

	mgr := NewManager("", taskID, provider)

	// Cleanup function
	cleanup := func() {
		mgr.Clear()
	}

	return mgr, cleanup
}

// TestNewManager tests session manager creation
func TestNewManager(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Verify manager is created
	if mgr == nil {
		t.Fatal("Expected non-nil manager")
	}

	// Verify provider is set
	if mgr.GetProvider() != "claude" {
		t.Errorf("Expected provider 'claude', got %s", mgr.GetProvider())
	}

	// Verify taskID is set
	if mgr.GetTaskID() != "test_task" {
		t.Errorf("Expected taskID 'test_task', got %s", mgr.GetTaskID())
	}
}

// TestSave tests saving session ID to in-memory cache
func TestSave(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	sessionID := "test-session-12345"

	// Save session ID
	err := mgr.Save(sessionID)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Verify session exists
	if !mgr.Exists() {
		t.Error("Expected session to exist after save")
	}
}

// TestSaveEmptyID tests saving empty session ID
func TestSaveEmptyID(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Save empty session ID should fail
	err := mgr.Save("")
	if err == nil {
		t.Error("Expected error when saving empty session ID")
	}
}

// TestLoad tests loading session ID from in-memory cache
func TestLoad(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Load non-existent session should return empty
	sessionID, err := mgr.Load()
	if err != nil {
		t.Fatalf("Load should not fail for non-existent session: %v", err)
	}
	if sessionID != "" {
		t.Errorf("Expected empty session ID, got %s", sessionID)
	}

	// Save and load back
	expectedID := "test-session-67890"
	err = mgr.Save(expectedID)
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	loadedID, err := mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}
	if loadedID != expectedID {
		t.Errorf("Expected session ID %s, got %s", expectedID, loadedID)
	}
}

// TestLoadCaching tests session ID caching (in-memory)
func TestLoadCaching(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	expectedID := "cached-session-id"

	// Save session
	err := mgr.Save(expectedID)
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// First load
	_, err = mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Second load (should return same cached value)
	cachedID, err := mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load cached: %v", err)
	}
	if cachedID != expectedID {
		t.Errorf("Expected cached ID %s, got %s", expectedID, cachedID)
	}
}

// TestClear tests clearing in-memory session cache
func TestClear(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Save session
	err := mgr.Save("test-session")
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Verify session exists
	if !mgr.Exists() {
		t.Error("Expected session to exist")
	}

	// Clear session
	err = mgr.Clear()
	if err != nil {
		t.Fatalf("Failed to clear: %v", err)
	}

	// Verify session no longer exists
	if mgr.Exists() {
		t.Error("Expected session to not exist after clear")
	}
}

// TestClearNonExistent tests clearing non-existent session
func TestClearNonExistent(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Clear non-existent session should not fail
	err := mgr.Clear()
	if err != nil {
		t.Errorf("Clear should not fail for non-existent session: %v", err)
	}
}

// TestExists tests session existence check
func TestExists(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Should not exist initially
	if mgr.Exists() {
		t.Error("Expected session to not exist initially")
	}

	// Save session
	err := mgr.Save("test-session")
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Should exist after save
	if !mgr.Exists() {
		t.Error("Expected session to exist after save")
	}

	// Clear and check again
	mgr.Clear()
	if mgr.Exists() {
		t.Error("Expected session to not exist after clear")
	}
}

// TestConcurrentSaveLoad tests concurrent save and load operations
func TestConcurrentSaveLoad(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent saves
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sessionID := "session-" + string(rune('A'+id))
			mgr.Save(sessionID)
		}(i)
	}

	wg.Wait()

	// Load should return some valid session ID
	sessionID, err := mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load after concurrent saves: %v", err)
	}
	if sessionID == "" {
		t.Error("Expected non-empty session ID after concurrent saves")
	}
}

// TestConcurrentAccess tests concurrent read/write access
func TestConcurrentAccess(t *testing.T) {
	mgr, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Initialize session
	err := mgr.Save("initial-session")
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	var wg sync.WaitGroup
	numGoroutines := 20

	// Mixed concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				// Even goroutines: read
				mgr.Load()
				mgr.Exists()
			} else {
				// Odd goroutines: write
				sessionID := "session-" + string(rune('A'+id%26))
				mgr.Save(sessionID)
			}
		}(i)
	}

	wg.Wait()

	// Final state should be consistent
	sessionID, err := mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load after concurrent access: %v", err)
	}
	if sessionID == "" {
		t.Error("Expected non-empty session ID after concurrent access")
	}
}

// TestMultipleProviders tests multiple providers for same task
func TestMultipleProviders(t *testing.T) {
	// Create managers for different providers
	claudeMgr := NewManager("", "test_task", "claude")
	codexMgr := NewManager("", "test_task", "codex")
	copilotMgr := NewManager("", "test_task", "copilot")

	// Save sessions for each provider
	err := claudeMgr.Save("claude-session-123")
	if err != nil {
		t.Fatalf("Failed to save claude session: %v", err)
	}

	err = codexMgr.Save("codex-session-456")
	if err != nil {
		t.Fatalf("Failed to save codex session: %v", err)
	}

	err = copilotMgr.Save("copilot-session-789")
	if err != nil {
		t.Fatalf("Failed to save copilot session: %v", err)
	}

	// Each manager should return its own session
	claudeID, _ := claudeMgr.Load()
	codexID, _ := codexMgr.Load()
	copilotID, _ := copilotMgr.Load()

	if claudeID != "claude-session-123" {
		t.Errorf("Expected claude session, got %s", claudeID)
	}
	if codexID != "codex-session-456" {
		t.Errorf("Expected codex session, got %s", codexID)
	}
	if copilotID != "copilot-session-789" {
		t.Errorf("Expected copilot session, got %s", copilotID)
	}
}
