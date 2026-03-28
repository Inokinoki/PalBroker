package session

import (
	"fmt"
	"sync"
)

// Manager - In-memory session manager for a specific task/provider
// Session state is managed by the CLI agents themselves (Claude Code, Gemini, etc.)
// This only caches the session ID in memory for quick access during the application lifetime
type Manager struct {
	provider  string // claude, copilot, codex, gemini, etc.
	taskID    string
	sessionID string // In-memory cached session ID
	mu        sync.RWMutex
}

// NewManager - Create a new in-memory session manager for a task
func NewManager(sessionDir, taskID, provider string) *Manager {
	// Note: sessionDir is ignored - kept for API compatibility
	// Session state is managed by CLI agents, we only cache in memory
	return &Manager{
		provider: provider,
		taskID:   taskID,
	}
}

// Save - Save session ID to in-memory cache
func (m *Manager) Save(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessionID = sessionID
	return nil
}

// Load - Load session ID from in-memory cache
func (m *Manager) Load() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessionID, nil
}

// Clear - Clear in-memory session cache (for cleanup or reset)
func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessionID = ""
	return nil
}

// Exists - Check if session ID exists in memory
func (m *Manager) Exists() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessionID != ""
}

// GetProvider - Get the provider name for this session manager
func (m *Manager) GetProvider() string {
	return m.provider
}

// GetTaskID - Get the task ID for this session manager
func (m *Manager) GetTaskID() string {
	return m.taskID
}
