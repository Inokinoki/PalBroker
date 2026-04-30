package session_handler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ampProvider reads Amp session files.
// Sessions: ~/.local/share/amp/threads/<session_id>.json
type ampProvider struct {
	rootDir string // ~/.local/share/amp
}

func newAmpProvider(rootDir string) *ampProvider {
	if rootDir == "" {
		rootDir = filepath.Join(xdgDataHome(), "amp")
	}
	return &ampProvider{rootDir: rootDir}
}

func (p *ampProvider) Kind() ProviderKind { return ProviderAmp }

func (p *ampProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	threadPath := filepath.Join(p.rootDir, "threads", sessionID+".json")
	if _, err := os.Stat(threadPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("amp session not found: %s", sessionID)
	}

	return &ResolvedThread{
		Provider:  ProviderAmp,
		SessionID: sessionID,
		Path:      threadPath,
		Metadata: ResolutionMeta{
			Source:         "direct_path",
			CandidateCount: 1,
		},
	}, nil
}

// ampSession represents Amp JSON session format.
type ampSession struct {
	Messages []ampMessage `json:"messages"`
}

type ampMessage struct {
	Role    string       `json:"role"`
	Content []ampContent `json:"content"`
}

type ampContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ReadMessages reads Amp JSON session.
// Format: {"messages": [{"role": "user|assistant", "content": [{"type":"text","text":"..."}]}]}
func (p *ampProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
	data, err := os.ReadFile(thread.Path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var session ampSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	messages := make([]ThreadMessage, 0, len(session.Messages))
	for _, msg := range session.Messages {
		var parts []string
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
		text := strings.Join(parts, "\n")
		if text == "" {
			continue
		}
		messages = append(messages, ThreadMessage{
			Role: MessageRole(msg.Role),
			Text: text,
		})
	}

	return messages, nil
}
