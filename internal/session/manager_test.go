package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupTestManager creates a test manager with temp directory
func setupTestManager(t *testing.T, taskID, provider string) (*Manager, string, func()) {
	t.Helper()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pal-session-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	mgr := NewManager(tmpDir, taskID, provider)

	// Cleanup function
	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return mgr, tmpDir, cleanup
}

// TestNewManager tests session manager creation
func TestNewManager(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Verify manager is created
	if mgr == nil {
		t.Fatal("Expected non-nil manager")
	}

	// Verify session directory is created
	sessionDir := filepath.Join(tmpDir, "test_task")
	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		t.Error("Expected session directory to be created")
	}

	// Verify session file path
	expectedFile := filepath.Join(tmpDir, "test_task", ".claude-session.json")
	if mgr.sessionFile != expectedFile {
		t.Errorf("Expected session file %s, got %s", expectedFile, mgr.sessionFile)
	}
}

// TestSave tests saving session ID
func TestSave(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	sessionID := "test-session-12345"

	// Save session ID
	err := mgr.Save(sessionID)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Verify file exists
	sessionFile := filepath.Join(mgr.sessionDir, mgr.taskID, ".claude-session.json")
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		t.Error("Expected session file to exist")
	}

	// Verify content can be read back
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatalf("Failed to read session file: %v", err)
	}

	// Check file contains session ID (basic check)
	if len(data) == 0 {
		t.Error("Expected non-empty session file")
	}
}

// TestSaveEmptyID tests saving empty session ID
func TestSaveEmptyID(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Save empty session ID should fail
	err := mgr.Save("")
	if err == nil {
		t.Error("Expected error when saving empty session ID")
	}
}

// TestLoad tests loading session ID
func TestLoad(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
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

// TestLoadCaching tests session ID caching
func TestLoadCaching(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	expectedID := "cached-session-id"

	// Save session
	err := mgr.Save(expectedID)
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// First load (should read from file and cache)
	_, err = mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Second load (should return cached)
	cachedID, err := mgr.Load()
	if err != nil {
		t.Fatalf("Failed to load cached: %v", err)
	}
	if cachedID != expectedID {
		t.Errorf("Expected cached ID %s, got %s", expectedID, cachedID)
	}
}

// TestClear tests clearing session
func TestClear(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
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
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Clear non-existent session should not fail
	err := mgr.Clear()
	if err != nil {
		t.Errorf("Clear should not fail for non-existent session: %v", err)
	}
}

// TestExists tests session existence check
func TestExists(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
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

// TestGetSessionData tests getting full session data
func TestGetSessionData(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	sessionID := "test-session-data"

	// Save session
	err := mgr.Save(sessionID)
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Get session data
	data, err := mgr.GetSessionData()
	if err != nil {
		t.Fatalf("Failed to get session data: %v", err)
	}

	if data == nil {
		t.Fatal("Expected non-nil session data")
	}

	if data.SessionID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, data.SessionID)
	}

	if data.Provider != "claude" {
		t.Errorf("Expected provider 'claude', got %s", data.Provider)
	}

	if data.TaskID != "test_task" {
		t.Errorf("Expected task ID 'test_task', got %s", data.TaskID)
	}

	if data.CreatedAt == 0 {
		t.Error("Expected CreatedAt to be set")
	}

	if data.UpdatedAt == 0 {
		t.Error("Expected UpdatedAt to be set")
	}
}

// TestGetSessionDataNonExistent tests getting data for non-existent session
func TestGetSessionDataNonExistent(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	data, err := mgr.GetSessionData()
	if err != nil {
		t.Fatalf("GetSessionData should not fail: %v", err)
	}
	if data != nil {
		t.Error("Expected nil session data for non-existent session")
	}
}

// TestProviderMismatch tests provider mismatch detection
func TestProviderMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pal-session-mismatch-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create manager with claude provider and save session
	mgr1 := NewManager(tmpDir, "test_task", "claude")
	err = mgr1.Save("test-session")
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Verify session was saved
	if !mgr1.Exists() {
		t.Fatal("Expected session to exist after save")
	}

	// Manually corrupt the session file to have different provider
	sessionFile := filepath.Join(tmpDir, "test_task", ".claude-session.json")
	corruptData := `{
		"session_id": "test-session",
		"provider": "codex",
		"task_id": "test_task",
		"created_at": 1234567890,
		"updated_at": 1234567890
	}`
	err = os.WriteFile(sessionFile, []byte(corruptData), 0644)
	if err != nil {
		t.Fatalf("Failed to corrupt session file: %v", err)
	}

	// Create new manager with claude (reading corrupted file with codex provider)
	mgr2 := NewManager(tmpDir, "test_task", "claude")

	// GetSessionData should detect provider mismatch
	_, err = mgr2.GetSessionData()
	if err == nil {
		t.Error("Expected error when loading session with mismatched provider")
	} else if !strings.Contains(err.Error(), "provider mismatch") {
		t.Errorf("Expected provider mismatch error, got: %v", err)
	}
}

// TestConcurrentSaveLoad tests concurrent save and load operations
func TestConcurrentSaveLoad(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
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
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
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
				mgr.GetSessionData()
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

// TestSavePreservesCreatedAt tests that Save preserves CreatedAt timestamp
func TestSavePreservesCreatedAt(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	// Save first time
	err := mgr.Save("test-session")
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Get first data
	data1, err := mgr.GetSessionData()
	if err != nil {
		t.Fatalf("Failed to get data: %v", err)
	}

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Save second time (update)
	err = mgr.Save("test-session-updated")
	if err != nil {
		t.Fatalf("Failed to save update: %v", err)
	}

	// Get updated data
	data2, err := mgr.GetSessionData()
	if err != nil {
		t.Fatalf("Failed to get updated data: %v", err)
	}

	// CreatedAt should be preserved
	if data1.CreatedAt != data2.CreatedAt {
		t.Error("Expected CreatedAt to be preserved on update")
	}

	// UpdatedAt should be newer
	if data2.UpdatedAt <= data1.UpdatedAt {
		t.Error("Expected UpdatedAt to be updated")
	}
}

// TestMultipleProviders tests multiple providers for same task
func TestMultipleProviders(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pal-multi-provider-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create managers for different providers
	claudeMgr := NewManager(tmpDir, "test_task", "claude")
	codexMgr := NewManager(tmpDir, "test_task", "codex")
	copilotMgr := NewManager(tmpDir, "test_task", "copilot")

	// Save sessions for each provider
	err = claudeMgr.Save("claude-session-123")
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

	// Each manager should load its own session
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

// TestSessionFileFormat tests session file JSON format
func TestSessionFileFormat(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t, "test_task", "claude")
	defer cleanup()

	sessionID := "test-session-format"
	err := mgr.Save(sessionID)
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Read raw file content
	sessionFile := filepath.Join(tmpDir, "test_task", ".claude-session.json")
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatalf("Failed to read session file: %v", err)
	}

	// Parse JSON to verify format
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse session JSON: %v", err)
	}

	// Verify required fields
	if parsed["session_id"] != sessionID {
		t.Errorf("Expected session_id %s, got %v", sessionID, parsed["session_id"])
	}
	if parsed["provider"] != "claude" {
		t.Errorf("Expected provider claude, got %v", parsed["provider"])
	}
	if parsed["task_id"] != "test_task" {
		t.Errorf("Expected task_id test_task, got %v", parsed["task_id"])
	}
}

// TestCreatedAtPreservedAcrossRestarts tests that CreatedAt persists across manager restarts
func TestCreatedAtPreservedAcrossRestarts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pal-restart-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create and save with first manager
	mgr1 := NewManager(tmpDir, "test_task", "claude")
	err = mgr1.Save("persistent-session")
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	data1, _ := mgr1.GetSessionData()
	createdAt := data1.CreatedAt

	// Wait to ensure different timestamp
	time.Sleep(10 * time.Millisecond)

	// Create new manager for same task/provider
	mgr2 := NewManager(tmpDir, "test_task", "claude")

	// Load should return cached empty, then read from file
	sessionID, err := mgr2.Load()
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}
	if sessionID != "persistent-session" {
		t.Errorf("Expected persistent-session, got %s", sessionID)
	}

	// Get data and verify CreatedAt preserved
	data2, _ := mgr2.GetSessionData()
	if data2.CreatedAt != createdAt {
		t.Error("Expected CreatedAt to be preserved across restarts")
	}
}

// TestSessionDataConstants tests session data structure
func TestSessionDataConstants(t *testing.T) {
	data := &SessionData{
		SessionID: "test-123",
		Provider:  "claude",
		TaskID:    "test_task",
		CreatedAt: 1234567890,
		UpdatedAt: 1234567891,
	}

	if data.SessionID != "test-123" {
		t.Errorf("Expected SessionID test-123, got %s", data.SessionID)
	}
	if data.Provider != "claude" {
		t.Errorf("Expected Provider claude, got %s", data.Provider)
	}
	if data.TaskID != "test_task" {
		t.Errorf("Expected TaskID test_task, got %s", data.TaskID)
	}
	if data.CreatedAt != 1234567890 {
		t.Errorf("Expected CreatedAt 1234567890, got %d", data.CreatedAt)
	}
	if data.UpdatedAt != 1234567891 {
		t.Errorf("Expected UpdatedAt 1234567891, got %d", data.UpdatedAt)
	}
}
