package session_handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudeProvider reads Claude Code session files.
// Sessions: ~/.claude/projects/<project_hash>/sessions/<session_id>.jsonl
type claudeProvider struct {
	rootDir string // ~/.claude
}

func newClaudeProvider(rootDir string) *claudeProvider {
	if rootDir == "" {
		rootDir = filepath.Join(homeDir(), ".claude")
	}
	return &claudeProvider{rootDir: rootDir}
}

func (p *claudeProvider) Kind() ProviderKind { return ProviderClaude }

// claudeSessionIndex mirrors sessions-index.json structure.
type claudeSessionIndex struct {
	Entries []struct {
		SessionID string `json:"sessionId"`
		FullPath  string `json:"fullPath"`
	} `json:"entries"`
}

func (p *claudeProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	projectsRoot := filepath.Join(p.rootDir, "projects")
	if _, err := os.Stat(projectsRoot); os.IsNotExist(err) {
		return nil, fmt.Errorf("claude projects dir not found: %s", projectsRoot)
	}

	// Strategy 1: sessions-index.json
	if path, meta := p.findFromIndex(projectsRoot, sessionID); path != "" {
		return &ResolvedThread{
			Provider:  ProviderClaude,
			SessionID: sessionID,
			Path:      path,
			Metadata:  meta,
		}, nil
	}

	// Strategy 2: direct filename match (<session_id>.jsonl)
	if path, meta := p.findByFilename(projectsRoot, sessionID); path != "" {
		return &ResolvedThread{
			Provider:  ProviderClaude,
			SessionID: sessionID,
			Path:      path,
			Metadata:  meta,
		}, nil
	}

	// Strategy 3: header scan (first 30 lines for sessionId)
	if path, meta := p.findByHeaderScan(projectsRoot, sessionID); path != "" {
		return &ResolvedThread{
			Provider:  ProviderClaude,
			SessionID: sessionID,
			Path:      path,
			Metadata:  meta,
		}, nil
	}

	return nil, fmt.Errorf("claude session not found: %s", sessionID)
}

func (p *claudeProvider) findFromIndex(projectsRoot, sessionID string) (string, ResolutionMeta) {
	var matches []string
	filepath.Walk(projectsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "sessions-index.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var idx claudeSessionIndex
		if json.Unmarshal(data, &idx) != nil {
			return nil
		}
		for _, e := range idx.Entries {
			if e.SessionID == sessionID && e.FullPath != "" {
				if _, err := os.Stat(e.FullPath); err == nil {
					matches = append(matches, e.FullPath)
				}
			}
		}
		return nil
	})
	if len(matches) == 0 {
		return "", ResolutionMeta{}
	}
	return selectLatestFile(matches), ResolutionMeta{
		Source:         "sessions-index.json",
		CandidateCount: len(matches),
	}
}

func (p *claudeProvider) findByFilename(projectsRoot, sessionID string) (string, ResolutionMeta) {
	var matches []string
	needle := sessionID + ".jsonl"
	filepath.Walk(projectsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == needle {
			matches = append(matches, path)
		}
		return nil
	})
	if len(matches) == 0 {
		return "", ResolutionMeta{}
	}
	return selectLatestFile(matches), ResolutionMeta{
		Source:         "filename_match",
		CandidateCount: len(matches),
	}
}

func (p *claudeProvider) findByHeaderScan(projectsRoot, sessionID string) (string, ResolutionMeta) {
	var matches []string
	filepath.Walk(projectsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if p.fileContainsSessionID(path, sessionID) {
			matches = append(matches, path)
		}
		return nil
	})
	if len(matches) == 0 {
		return "", ResolutionMeta{}
	}
	return selectLatestFile(matches), ResolutionMeta{
		Source:         "header_scan",
		CandidateCount: len(matches),
	}
}

func (p *claudeProvider) fileContainsSessionID(path, sessionID string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for i := 0; i < 30 && scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(line), &data) != nil {
			continue
		}
		if sid, ok := data["sessionId"].(string); ok && sid == sessionID {
			return true
		}
	}
	return false
}

// ReadMessages reads Claude JSONL session and extracts messages.
// Claude format:
//
//	{"type": "user|assistant|system", "message": {"role": "...", "content": [{"type":"text","text":"..."}]}}
func (p *claudeProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
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

		msg := extractClaudeMessage(entry)
		if msg != nil {
			messages = append(messages, *msg)
		}
	}
	return messages, scanner.Err()
}

func extractClaudeMessage(entry map[string]interface{}) *ThreadMessage {
	eventType, _ := entry["type"].(string)

	// Skip non-message types
	switch eventType {
	case "tool_use", "tool_result", "tool_call", "function_call", "function_result", "function_response":
		return nil
	}

	// Extract message object
	msgObj, _ := entry["message"].(map[string]interface{})
	if msgObj == nil {
		// Try direct content
		if text, ok := entry["content"].(string); ok && eventType != "" {
			role := roleFromType(eventType)
			return &ThreadMessage{Role: role, Text: text}
		}
		return nil
	}

	roleStr, _ := msgObj["role"].(string)
	if roleStr == "" {
		roleStr = string(roleFromType(eventType))
	}

	text := extractContentText(msgObj["content"])
	if text == "" {
		return nil
	}

	return &ThreadMessage{Role: MessageRole(roleStr), Text: text}
}

// roleFromType maps event type to message role.
func roleFromType(t string) MessageRole {
	switch t {
	case "user":
		return RoleUser
	case "assistant":
		return RoleAssistant
	case "system":
		return RoleSystem
	default:
		return MessageRole(t)
	}
}

// extractContentText extracts text from Claude content field.
// Content can be a string or an array of content blocks.
func extractContentText(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, item := range c {
			if block, ok := item.(map[string]interface{}); ok {
				if block["type"] == "text" {
					if text, ok := block["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
