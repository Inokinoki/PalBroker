package session_handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// copilotProvider reads GitHub Copilot CLI session files.
// Sessions: ~/.copilot/session-state/<session_id>/events.jsonl or <session_id>.jsonl
type copilotProvider struct {
	rootDir string // ~/.copilot
}

func newCopilotProvider(rootDir string) *copilotProvider {
	if rootDir == "" {
		rootDir = filepath.Join(homeDir(), ".copilot")
	}
	return &copilotProvider{rootDir: rootDir}
}

func (p *copilotProvider) Kind() ProviderKind { return ProviderCopilot }

func (p *copilotProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	stateDir := filepath.Join(p.rootDir, "session-state")
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("copilot session-state dir not found: %s", stateDir)
	}

	// Check both path patterns
	candidates := []string{
		filepath.Join(stateDir, sessionID, "events.jsonl"),
		filepath.Join(stateDir, sessionID+".jsonl"),
	}

	var matches []string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			matches = append(matches, c)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("copilot session not found: %s", sessionID)
	}

	return &ResolvedThread{
		Provider:  ProviderCopilot,
		SessionID: sessionID,
		Path:      selectLatestFile(matches),
		Metadata: ResolutionMeta{
			Source:         "direct_path",
			CandidateCount: len(matches),
		},
	}, nil
}

// ReadMessages reads Copilot JSONL session.
// Copilot format:
//
//	{"type": "assistant.message_delta", "data": {"deltaContent": "..."}}
//	{"type": "assistant.message", "data": {"content": "..."}}
func (p *copilotProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
	f, err := os.Open(thread.Path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	var messages []ThreadMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		msg := extractCopilotMessage(entry)
		if msg != nil {
			messages = append(messages, *msg)
		}
	}
	return messages, scanner.Err()
}

func extractCopilotMessage(entry map[string]interface{}) *ThreadMessage {
	eventType, _ := entry["type"].(string)
	data, _ := entry["data"].(map[string]interface{})
	if data == nil {
		data = entry
	}

	role := RoleAssistant
	if strings.HasPrefix(eventType, "user") {
		role = RoleUser
	}

	// Try deltaContent first (streaming), then content (complete)
	text, _ := data["deltaContent"].(string)
	if text == "" {
		text, _ = data["content"].(string)
	}
	if text == "" {
		text = extractContentText(data["content"])
	}
	if text == "" {
		return nil
	}

	return &ThreadMessage{Role: role, Text: text}
}
