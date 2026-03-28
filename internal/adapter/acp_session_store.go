package adapter

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	"openpal/internal/util"
)

// ACPSessionStore - SQLite-based session store for ACP providers
// Persists ACP session data in real-time for history recovery
type ACPSessionStore struct {
	db       *sql.DB
	provider string
	mu       sync.Mutex
}

// ACPSessionRecord - A single session record in SQLite
type ACPSessionRecord struct {
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
	Type      string `json:"type"` // chunk, assistant, user, system, etc.
	Timestamp int64  `json:"timestamp"`
	Data      string `json:"data"` // JSON-encoded
	Content   string `json:"content,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// NewACPSessionStore - Create a new ACP session store
func NewACPSessionStore(sessionDir, provider string) (*ACPSessionStore, error) {
	if sessionDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			sessionDir = "/tmp"
		} else {
			sessionDir = filepath.Join(home, ".openpal")
		}
	}

	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	dbPath := filepath.Join(sessionDir, fmt.Sprintf("acp_sessions_%s.db", provider))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	store := &ACPSessionStore{
		db:       db,
		provider: provider,
	}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	util.DebugLog("[DEBUG] ACP session store initialized: %s", dbPath)
	return store, nil
}

// initSchema - Initialize database schema
func (s *ACPSessionStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		type TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		data TEXT,
		content TEXT,
		created_at INTEGER NOT NULL,
		UNIQUE(session_id, seq)
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_session_id ON sessions(session_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_timestamp ON sessions(timestamp);
	CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions(created_at);
	`

	_, err := s.db.Exec(schema)
	return err
}

// RecordSession - Record a session event (called from notification handler)
func (s *ACPSessionStore) RecordSession(sessionID string, event *ACPMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()

	// Get next sequence number for this session
	seq, err := s.getNextSeq(sessionID)
	if err != nil {
		return fmt.Errorf("get next seq: %w", err)
	}

	var eventType, content string
	var dataBytes []byte

	// Parse session update notifications
	if event.Method == "session/update" {
		var update ACPSessionUpdate
		if err := json.Unmarshal(event.Params, &update); err != nil {
			return fmt.Errorf("parse update: %w", err)
		}

		eventType = "session_update"
		content = update.Content.Text

		dataBytes, _ = json.Marshal(map[string]interface{}{
			"session_id":     update.SessionID,
			"session_update": update.SessionUpdate,
			"content_type":   update.Content.Type,
			"content":        update.Content.Text,
		})
	} else {
		// Other ACP messages
		eventType = event.Method
		dataBytes, _ = json.Marshal(event)
	}

	data := string(dataBytes)

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO sessions (session_id, seq, type, timestamp, data, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, sessionID, seq, eventType, now, data, content, now)

	return err
}

// RecordContent - Record session content directly (for chat messages)
func (s *ACPSessionStore) RecordContent(sessionID, eventType, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()

	seq, err := s.getNextSeq(sessionID)
	if err != nil {
		return fmt.Errorf("get next seq: %w", err)
	}

	dataBytes, _ := json.Marshal(map[string]interface{}{
		"session_id": sessionID,
		"type":       eventType,
		"content":    content,
	})

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO sessions (session_id, seq, type, timestamp, data, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, sessionID, seq, eventType, now, string(dataBytes), content, now)

	return err
}

// getNextSeq - Get next sequence number for a session
func (s *ACPSessionStore) getNextSeq(sessionID string) (int64, error) {
	var maxSeq sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(seq) FROM sessions WHERE session_id = ?`, sessionID).Scan(&maxSeq)
	if err != nil {
		return 0, err
	}
	return maxSeq.Int64 + 1, nil
}

// ReadSession - Read all events from a session (for recovery)
func (s *ACPSessionStore) ReadSession(sessionID string) ([]SessionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT session_id, seq, type, timestamp, data, content, created_at
		FROM sessions
		WHERE session_id = ?
		ORDER BY seq ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SessionEvent
	for rows.Next() {
		var record ACPSessionRecord
		if err := rows.Scan(&record.SessionID, &record.Seq, &record.Type, &record.Timestamp, &record.Data, &record.Content, &record.CreatedAt); err != nil {
			return nil, err
		}

		// Parse data JSON
		var data interface{}
		if record.Data != "" {
			if err := json.Unmarshal([]byte(record.Data), &data); err != nil {
				data = record.Data
			}
		}

		events = append(events, SessionEvent{
			Seq:       record.Seq,
			Type:      record.Type,
			Timestamp: record.Timestamp,
			Data:      data,
			Raw:       record.Data,
		})
	}

	return events, rows.Err()
}

// ListSessions - List all available sessions for this provider
func (s *ACPSessionStore) ListSessions() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT DISTINCT session_id FROM sessions ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			continue
		}
		sessions = append(sessions, sessionID)
	}

	return sessions, rows.Err()
}

// GetSessionMetadata - Get metadata about a session
func (s *ACPSessionStore) GetSessionMetadata(sessionID string) (*SessionMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var minTimestamp, maxTimestamp, count int64
	err := s.db.QueryRow(`
		SELECT MIN(timestamp), MAX(timestamp), COUNT(*)
		FROM sessions
		WHERE session_id = ?
	`, sessionID).Scan(&minTimestamp, &maxTimestamp, &count)
	if err != nil {
		return nil, err
	}

	if count == 0 {
		return nil, fmt.Errorf("session not found")
	}

	return &SessionMetadata{
		SessionID:    sessionID,
		Provider:     s.provider,
		CreatedAt:    time.Unix(minTimestamp/1000, (minTimestamp%1000)*1000000),
		UpdatedAt:    time.Unix(maxTimestamp/1000, (maxTimestamp%1000)*1000000),
		MessageCount: int(count),
	}, nil
}

// DeleteSession - Delete a session and its events
func (s *ACPSessionStore) DeleteSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM sessions WHERE session_id = ?`, sessionID)
	return err
}

// Close - Close the database connection
func (s *ACPSessionStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
