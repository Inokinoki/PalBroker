package state

import (
	"sync"
	"time"
)

// AgentStatus - Current status of the AI agent
type AgentStatus struct {
	State         string   `json:"state"`        // running, completed, failed, stopped
	Provider      string   `json:"provider"`     // claude, codex, copilot
	QuestID       string   `json:"quest_id"`     // Quest identifier
	PID           int      `json:"pid"`          // openpal PID
	CLIPID        int      `json:"cli_pid"`      // AI CLI PID
	StartTime     int64    `json:"start_time"`   // Unix timestamp (ms)
	UpdateTime    int64    `json:"update_time"`  // Last update time (ms)
	Seq           int64    `json:"seq"`          // Current sequence number
	Capabilities  []string `json:"capabilities"` // Agent capabilities
	WorkDir       string   `json:"work_dir"`     // Working directory
	WebSocketPort int      `json:"ws_port"`      // WebSocket port
}

// TaskProgress - Current task progress
type TaskProgress struct {
	QuestID        string   `json:"quest_id"`
	State          string   `json:"state"`          // pending, running, completed, failed
	Progress       int      `json:"progress"`       // 0-100
	CurrentAction  string   `json:"current_action"` // Current action description
	FilesModified  []string `json:"files_modified"` // List of modified files
	LastOutput     string   `json:"last_output"`    // Last output line
	LastOutputTime int64    `json:"last_output_time"`
	StartTime      int64    `json:"start_time"`
	UpdateTime     int64    `json:"update_time"`
}

// StatusManager - Manages agent status and progress (in-memory only, no file persistence)
type StatusManager struct {
	mu            sync.RWMutex
	status        *AgentStatus
	progress      *TaskProgress
	filesModified map[string]struct{} // Map for O(1) duplicate check
}

// NewStatusManager - Create new status manager
func NewStatusManager(sessionDir string) *StatusManager {
	return &StatusManager{
		status:        &AgentStatus{},
		progress:      &TaskProgress{},
		filesModified: make(map[string]struct{}),
	}
}

// Initialize - Initialize status (in-memory only)
func (m *StatusManager) Initialize(questID, provider string, pid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()

	m.status = &AgentStatus{
		State:     "initializing",
		Provider:  provider,
		QuestID:   questID,
		PID:       pid,
		StartTime: now,
		UpdateTime: now,
	}

	m.progress = &TaskProgress{
		QuestID:    questID,
		State:      "pending",
		Progress:   0,
		StartTime:  now,
		UpdateTime: now,
	}

	return nil
}

// UpdateAgentStatus - Update agent status (in-memory only)
func (m *StatusManager) UpdateAgentStatus(state string, cliPID int, wsPort int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.status.State = state
	m.status.CLIPID = cliPID
	m.status.WebSocketPort = wsPort
	m.status.UpdateTime = time.Now().UnixMilli()
}

// UpdateProgress - Update task progress (in-memory only)
func (m *StatusManager) UpdateProgress(progress int, action string) {
	m.mu.Lock()
	m.progress.Progress = progress
	m.progress.CurrentAction = action
	m.progress.UpdateTime = time.Now().UnixMilli()
	m.mu.Unlock()
}

// AddFileModified - Record a modified file (in-memory only)
func (m *StatusManager) AddFileModified(filePath string) {
	m.mu.Lock()

	if _, exists := m.filesModified[filePath]; exists {
		m.mu.Unlock()
		return
	}

	m.filesModified[filePath] = struct{}{}
	m.progress.FilesModified = append(m.progress.FilesModified, filePath)
	m.progress.UpdateTime = time.Now().UnixMilli()
	m.mu.Unlock()
}

// UpdateLastOutput - Update last output (in-memory only)
func (m *StatusManager) UpdateLastOutput(output string) {
	m.mu.Lock()
	m.progress.LastOutput = output
	m.progress.LastOutputTime = time.Now().UnixMilli()
	m.progress.UpdateTime = time.Now().UnixMilli()
	m.mu.Unlock()
}

// SetCompleted - Mark task as completed
func (m *StatusManager) SetCompleted() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()
	m.status.State = "completed"
	m.progress.State = "completed"
	m.progress.Progress = 100
	m.status.UpdateTime = now
	m.progress.UpdateTime = now
}

// SetFailed - Mark task as failed
func (m *StatusManager) SetFailed(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()
	m.status.State = "failed"
	m.progress.State = "failed"
	m.progress.LastOutput = reason
	m.status.UpdateTime = now
	m.progress.UpdateTime = now
}

// SetStopped - Mark task as stopped
func (m *StatusManager) SetStopped() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()
	m.status.State = "stopped"
	m.progress.State = "stopped"
	m.status.UpdateTime = now
	m.progress.UpdateTime = now
}

// GetStatus - Get current status
func (m *StatusManager) GetStatus() (*AgentStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status, nil
}

// GetProgress - Get current progress
func (m *StatusManager) GetProgress() (*TaskProgress, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.progress, nil
}

// Flush - No-op (kept for API compatibility)
func (m *StatusManager) Flush() {
	// No-op: no file persistence
}

// Close - No-op (kept for API compatibility)
func (m *StatusManager) Close() {
	// No-op: no file persistence
}
