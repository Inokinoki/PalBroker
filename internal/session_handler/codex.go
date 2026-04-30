package session_handler

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// codexProvider reads OpenAI Codex CLI session files.
// Sessions: ~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<session_id>.jsonl
// Index: ~/.codex/state*.sqlite (threads table)
type codexProvider struct {
	rootDir string // ~/.codex
}

func newCodexProvider(rootDir string) *codexProvider {
	if rootDir == "" {
		rootDir = filepath.Join(homeDir(), ".codex")
	}
	return &codexProvider{rootDir: rootDir}
}

func (p *codexProvider) Kind() ProviderKind { return ProviderCodex }

// rolloutPattern matches rollout filenames.
var rolloutPattern = regexp.MustCompile(`rollout-\d+-([a-zA-Z0-9_-]+)\.jsonl`)

func (p *codexProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	// Strategy 1: SQLite state index
	if path, meta := p.findFromSQLite(sessionID); path != "" {
		return &ResolvedThread{
			Provider:  ProviderCodex,
			SessionID: sessionID,
			Path:      path,
			Metadata:  meta,
		}, nil
	}

	// Strategy 2: filesystem scan
	sessionsDir := filepath.Join(p.rootDir, "sessions")
	archivedDir := filepath.Join(p.rootDir, "archived_sessions")

	for _, dir := range []string{sessionsDir, archivedDir} {
		if path, meta := p.findRolloutFile(dir, sessionID); path != "" {
			return &ResolvedThread{
				Provider:  ProviderCodex,
				SessionID: sessionID,
				Path:      path,
				Metadata:  meta,
			}, nil
		}
	}

	return nil, fmt.Errorf("codex session not found: %s", sessionID)
}

func (p *codexProvider) findFromSQLite(sessionID string) (string, ResolutionMeta) {
	// Find state*.sqlite files
	matches, _ := filepath.Glob(filepath.Join(p.rootDir, "state*.sqlite"))
	if len(matches) == 0 {
		return "", ResolutionMeta{}
	}

	for _, dbPath := range matches {
		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			continue
		}

		var rolloutPath string
		err = db.QueryRow(`SELECT rollout_path FROM threads WHERE id = ?`, sessionID).Scan(&rolloutPath)
		db.Close()

		if err == nil && rolloutPath != "" {
			if _, statErr := os.Stat(rolloutPath); statErr == nil {
				return rolloutPath, ResolutionMeta{
					Source:         "sqlite_state",
					CandidateCount: 1,
				}
			}
		}
	}
	return "", ResolutionMeta{}
}

func (p *codexProvider) findRolloutFile(dir, sessionID string) (string, ResolutionMeta) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", ResolutionMeta{}
	}

	var matches []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if rolloutPattern.MatchString(name) {
			sub := rolloutPattern.FindStringSubmatch(name)
			if len(sub) >= 2 && sub[1] == sessionID {
				matches = append(matches, path)
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return "", ResolutionMeta{}
	}
	return selectLatestFile(matches), ResolutionMeta{
		Source:         "filesystem_scan",
		CandidateCount: len(matches),
	}
}

// ReadMessages reads Codex JSONL rollout.
// Codex format:
//
//	{"type": "item.completed", "item": {"type": "agent_message", "text": "..."}}
func (p *codexProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
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

		msg := extractCodexMessage(entry)
		if msg != nil {
			messages = append(messages, *msg)
		}
	}
	return messages, scanner.Err()
}

func extractCodexMessage(entry map[string]interface{}) *ThreadMessage {
	eventType, _ := entry["type"].(string)

	switch eventType {
	case "item.completed":
		item, _ := entry["item"].(map[string]interface{})
		if item == nil {
			return nil
		}
		itemType, _ := item["type"].(string)
		role := RoleAssistant
		if itemType == "message" {
			if r, ok := item["role"].(string); ok {
				role = MessageRole(r)
			}
		}
		text, _ := item["text"].(string)
		if text == "" {
			text = extractContentText(item["content"])
		}
		if text == "" {
			return nil
		}
		return &ThreadMessage{Role: role, Text: text}

	case "thread.started":
		return nil // Skip metadata events
	}

	// Fallback: look for role + content
	if role, ok := entry["role"].(string); ok {
		text, _ := entry["content"].(string)
		if text == "" {
			text, _ = entry["text"].(string)
		}
		if text != "" {
			return &ThreadMessage{Role: MessageRole(role), Text: text}
		}
	}
	return nil
}
