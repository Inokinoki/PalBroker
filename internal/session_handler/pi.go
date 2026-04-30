package session_handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// piProvider reads Pi agent session files.
// Sessions: ~/.pi/agent/sessions/<timestamp>_<session_id>.jsonl
type piProvider struct {
	rootDir string // ~/.pi/agent
}

func (p *piProvider) QueryProviderThreads() ([]ThreadInfo, error) {
	return queryPiThreads(p)
}

func newPiProvider(rootDir string) *piProvider {
	if rootDir == "" {
		if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
			rootDir = dir
		} else {
			rootDir = filepath.Join(homeDir(), ".pi", "agent")
		}
	}
	return &piProvider{rootDir: rootDir}
}

func (p *piProvider) Kind() ProviderKind { return ProviderPi }

func (p *piProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	sessionsDir := filepath.Join(p.rootDir, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("pi sessions dir not found: %s", sessionsDir)
	}

	var matches []string
	filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		// Check session header in first 20 lines
		if p.fileMatchesSession(path, sessionID) {
			matches = append(matches, path)
		}
		return nil
	})

	if len(matches) == 0 {
		return nil, fmt.Errorf("pi session not found: %s", sessionID)
	}

	return &ResolvedThread{
		Provider:  ProviderPi,
		SessionID: sessionID,
		Path:      selectLatestFile(matches),
		Metadata: ResolutionMeta{
			Source:         "header_scan",
			CandidateCount: len(matches),
		},
	}, nil
}

func (p *piProvider) fileMatchesSession(path, sessionID string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for i := 0; i < 20 && scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Type == "session" && strings.EqualFold(entry.ID, sessionID) {
			return true
		}
	}
	return false
}

// ReadMessages reads Pi JSONL session.
// Pi format:
//
//	{"type": "session", "version": 3, "id": "..."}
//	{"type": "message", "message": {"role": "user|assistant", "content": [{"type":"text","text":"..."}]}}
func (p *piProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
	var messages []ThreadMessage
	err := readJSONLFile(thread.Path, func(_ string, entry map[string]interface{}) {
		eventType, _ := entry["type"].(string)
		if eventType != "message" {
			return // Skip session headers and other events
		}

		msg := extractPiMessage(entry)
		if msg != nil {
			messages = append(messages, *msg)
		}
	})
	return messages, err
}

func extractPiMessage(entry map[string]interface{}) *ThreadMessage {
	msgObj, _ := entry["message"].(map[string]interface{})
	if msgObj == nil {
		return nil
	}

	role, _ := msgObj["role"].(string)
	if role == "" {
		return nil
	}

	text := extractContentText(msgObj["content"])
	if text == "" {
		// Try text_delta for streaming messages
		if delta, ok := entry["text_delta"].(string); ok {
			text = delta
		}
	}
	if text == "" {
		return nil
	}

	return &ThreadMessage{Role: MessageRole(role), Text: text}
}
