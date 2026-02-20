package adapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexAdapterPure Pure Go Implement Codex Adapter（NotUse Node.js SDK）
type CodexAdapterPure struct {
	config     *CLIConfig
	threadID   string
	sessionDir string
}

// NewCodexAdapterPure CreatePure Go Codex Adapter
func NewCodexAdapterPure(config *CLIConfig, sessionDir string) *CodexAdapterPure {
	return &CodexAdapterPure{
		config:     config,
		sessionDir: sessionDir,
	}
}

// Start Start Codex Session
func (a *CodexAdapterPure) Start() error {
	// TryLoad thread_id（RestoreSession）
	return a.loadThreadID()
}

// BuildCommand Build command
func (a *CodexAdapterPure) BuildCommand(task string) *exec.Cmd {
	args := []string{"exec"}

	// CheckIfSupport --json
	if a.supportsJSON() {
		args = append(args, "--json")
	}

	// Ifhas thread_id，RestoreSession
	if a.threadID != "" {
		args = append(args, "resume", "--last")
	}

	args = append(args, task)

	cmd := exec.Command("codex", args...)
	cmd.Dir = a.config.WorkDir
	return cmd
}

// SaveThreadID Save thread_id
func (a *CodexAdapterPure) SaveThreadID(id string) error {
	return os.WriteFile(
		filepath.Join(a.sessionDir, "codex_thread_id.txt"),
		[]byte(id),
		0644,
	)
}

// loadThreadID Load thread_id
func (a *CodexAdapterPure) loadThreadID() error {
	data, err := os.ReadFile(
		filepath.Join(a.sessionDir, "codex_thread_id.txt"),
	)
	if err != nil {
		return err
	}
	a.threadID = strings.TrimSpace(string(data))
	return nil
}

// supportsJSON CheckIfSupport --json Output
func (a *CodexAdapterPure) supportsJSON() bool {
	cmd := exec.Command("codex", "exec", "--help")
	output, _ := cmd.CombinedOutput()
	return strings.Contains(string(output), "--json")
}

// ParseOutputLine ParseOutputLine
func (a *CodexAdapterPure) ParseOutputLine(line string) map[string]interface{} {
	// Try to parse JSON
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err == nil {
		// JSON Mode
		if _, ok := msg["type"]; !ok {
			msg["type"] = "chunk"
		}

		// Extract thread_id
		if id, ok := msg["thread_id"].(string); ok {
			a.threadID = id
			a.SaveThreadID(id)
		}

		return msg
	}

	// Text mode
	return a.parseTextLine(line)
}

// parseTextLine ParseTextOutput
func (a *CodexAdapterPure) parseTextLine(line string) map[string]interface{} {
	// Extract thread_id
	if strings.HasPrefix(line, "thread_id:") {
		id := strings.TrimSpace(strings.TrimPrefix(line, "thread_id:"))
		a.threadID = id
		a.SaveThreadID(id)
		return map[string]interface{}{
			"type":      "thread_init",
			"thread_id": id,
		}
	}

	// Identify file operations
	lower := strings.ToLower(line)
	if strings.Contains(lower, "editing") || strings.Contains(lower, "creating") {
		return map[string]interface{}{
			"type":    "file_operation",
			"content": line,
		}
	}

	if strings.Contains(lower, "running") || strings.Contains(lower, "executing") {
		return map[string]interface{}{
			"type":    "command",
			"content": line,
		}
	}

	return map[string]interface{}{
		"type":    "chunk",
		"content": line,
	}
}
