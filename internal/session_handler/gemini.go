package session_handler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// geminiProvider reads Gemini CLI session files.
// Sessions: ~/.gemini/tmp/<project_hash>/chats/session-<timestamp>.json
// Format: JSON with messages array
type geminiProvider struct {
	rootDir string // ~/.gemini
}

func (p *geminiProvider) QueryProviderThreads() ([]ThreadInfo, error) {
	return queryGeminiThreads(p)
}

func newGeminiProvider(rootDir string) Provider {
	if rootDir == "" {
		for _, env := range []string{"GEMINI_CLI_HOME"} {
			if dir := os.Getenv(env); dir != "" {
				rootDir = filepath.Join(dir, ".gemini")
				break
			}
		}
		if rootDir == "" {
			rootDir = filepath.Join(homeDir(), ".gemini")
		}
	}
	return &geminiProvider{rootDir: rootDir}
}

func init() {
	registerProvider(ProviderGemini, newGeminiProvider)
}

func (p *geminiProvider) Kind() ProviderKind { return ProviderGemini }

func (p *geminiProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	// Gemini stores in tmp/<project_hash>/chats/session-*.json
	tmpDir := filepath.Join(p.rootDir, "tmp")
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("gemini tmp dir not found: %s", tmpDir)
	}

	var matches []string
	filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasPrefix(info.Name(), "session-") || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		// Read file and check sessionId field
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var session struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(data, &session) != nil {
			return nil
		}
		// Case-insensitive match
		if strings.EqualFold(session.SessionID, sessionID) {
			matches = append(matches, path)
		}
		return nil
	})

	if len(matches) == 0 {
		return nil, fmt.Errorf("gemini session not found: %s", sessionID)
	}

	return &ResolvedThread{
		Provider:  ProviderGemini,
		SessionID: sessionID,
		Path:      selectLatestFile(matches),
		Metadata: ResolutionMeta{
			Source:         "session_scan",
			CandidateCount: len(matches),
		},
	}, nil
}

// geminiSession represents the Gemini JSON session format.
type geminiSession struct {
	SessionID   string      `json:"sessionId"`
	ProjectHash string      `json:"projectHash"`
	StartTime   string      `json:"startTime"`
	LastUpdated string      `json:"lastUpdated"`
	Messages    []geminiMsg `json:"messages"`
}

type geminiMsg struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// ReadMessages reads Gemini JSON session.
// Format: {"messages": [{"type": "user|gemini", "content": "..."}]}
func (p *geminiProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
	data, err := os.ReadFile(thread.Path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var session geminiSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	messages := make([]ThreadMessage, 0, len(session.Messages))
	for _, msg := range session.Messages {
		role := RoleUser
		if msg.Type == "gemini" {
			role = RoleAssistant
		}
		if msg.Content == "" {
			continue
		}
		messages = append(messages, ThreadMessage{
			Role: role,
			Text: msg.Content,
		})
	}

	return messages, nil
}
