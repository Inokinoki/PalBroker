package session_handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// openCodeProvider reads OpenCode session files.
// Sessions: ~/.local/share/opencode/opencode.db (SQLite)
// Tables: session, message, part
type openCodeProvider struct {
	rootDir string // ~/.local/share/opencode
}

func newOpenCodeProvider(rootDir string) *openCodeProvider {
	if rootDir == "" {
		rootDir = filepath.Join(xdgDataHome(), "opencode")
	}
	return &openCodeProvider{rootDir: rootDir}
}

func (p *openCodeProvider) Kind() ProviderKind { return ProviderOpenCode }

func (p *openCodeProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	dbPath := filepath.Join(p.rootDir, "opencode.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("opencode database not found: %s", dbPath)
	}

	// Verify session exists in database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM session WHERE id = ?`, sessionID).Scan(&count)
	if err != nil || count == 0 {
		return nil, fmt.Errorf("opencode session not found: %s", sessionID)
	}

	return &ResolvedThread{
		Provider:  ProviderOpenCode,
		SessionID: sessionID,
		Path:      dbPath,
		Metadata: ResolutionMeta{
			Source:         "sqlite_query",
			CandidateCount: 1,
		},
	}, nil
}

// openCodeMessage represents a message row from opencode.db.
type openCodeMessage struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	TimeCreated int64  `json:"time_created"`
	Data        string `json:"data"`
}

// openCodePart represents a part row from opencode.db.
type openCodePart struct {
	ID          string `json:"id"`
	MessageID   string `json:"message_id"`
	TimeCreated int64  `json:"time_created"`
	Data        string `json:"data"`
}

// ReadMessages reads from OpenCode SQLite database.
// Joins message + part tables to extract conversation text.
func (p *openCodeProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
	db, err := sql.Open("sqlite3", thread.Path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Fetch messages ordered by time
	rows, err := db.Query(`
		SELECT id, session_id, time_created, data
		FROM message
		WHERE session_id = ?
		ORDER BY time_created ASC, id ASC
	`, thread.SessionID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	msgs := make([]openCodeMessage, 0)
	for rows.Next() {
		var msg openCodeMessage
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.TimeCreated, &msg.Data); err != nil {
			continue
		}
		msgs = append(msgs, msg)
	}

	// Fetch all parts for this session
	partRows, err := db.Query(`
		SELECT id, message_id, time_created, data
		FROM part
		WHERE session_id = ?
		ORDER BY time_created ASC, id ASC
	`, thread.SessionID)
	if err != nil {
		return nil, fmt.Errorf("query parts: %w", err)
	}
	defer partRows.Close()

	// Group parts by message_id
	partsByMsg := make(map[string][]openCodePart)
	for partRows.Next() {
		var part openCodePart
		if err := partRows.Scan(&part.ID, &part.MessageID, &part.TimeCreated, &part.Data); err != nil {
			continue
		}
		partsByMsg[part.MessageID] = append(partsByMsg[part.MessageID], part)
	}

	// Extract messages
	messages := make([]ThreadMessage, 0, len(msgs))
	for _, msg := range msgs {
		var msgData map[string]interface{}
		if json.Unmarshal([]byte(msg.Data), &msgData) != nil {
			continue
		}

		role, _ := msgData["role"].(string)
		if role == "" {
			role = string(RoleAssistant)
		}

		// Try to get text from parts
		var textParts []string
		if parts, ok := partsByMsg[msg.ID]; ok {
			for _, part := range parts {
				var partData map[string]interface{}
				if json.Unmarshal([]byte(part.Data), &partData) != nil {
					continue
				}
				if t, ok := partData["text"].(string); ok && t != "" {
					textParts = append(textParts, t)
				}
			}
		}

		// Fallback to message data content
		if len(textParts) == 0 {
			if content, ok := msgData["content"]; ok {
				textParts = append(textParts, extractContentText(content))
			}
		}

		text := strings.Join(textParts, "\n")
		if text == "" {
			continue
		}

		messages = append(messages, ThreadMessage{
			Role: MessageRole(role),
			Text: text,
		})
	}

	return messages, nil
}

// QueryThreads discovers all sessions in OpenCode database.
func (p *openCodeProvider) QueryThreads() ([]ThreadInfo, error) {
	dbPath := filepath.Join(p.rootDir, "opencode.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id FROM session ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []ThreadInfo
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		threads = append(threads, ThreadInfo{
			SessionID: id,
			Provider:  ProviderOpenCode,
			Path:      dbPath,
		})
	}
	return threads, rows.Err()
}

// sortMessages sorts by time_created.
func sortMessages(msgs []ThreadMessage) {
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].Role < msgs[j].Role
	})
}
