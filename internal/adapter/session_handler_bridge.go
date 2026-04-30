package adapter

import (
	"fmt"

	"openpal/internal/session_handler"
)

// sessionHandlerReader wraps a session_handler.Provider to implement SessionReader.
// This enables reading sessions from any supported provider
// (Claude, Codex, Copilot, Cursor, Gemini, Amp, Kimi, OpenCode, Pi)
// through the existing SessionReader interface.
type sessionHandlerReader struct {
	provider session_handler.Provider
	kind     session_handler.ProviderKind
}

// SessionHandlerReader creates a SessionReader backed by a session_handler provider.
func SessionHandlerReader(provider session_handler.ProviderKind, rootDir string) SessionReader {
	p, err := session_handler.NewProvider(provider, rootDir)
	if err != nil {
		return nil
	}
	return &sessionHandlerReader{provider: p, kind: provider}
}

func (r *sessionHandlerReader) ReadSession(sessionID string) ([]SessionEvent, error) {
	thread, err := r.provider.Resolve(sessionID)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}

	messages, err := r.provider.ReadMessages(thread)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}

	events := make([]SessionEvent, 0, len(messages))
	for i, msg := range messages {
		events = append(events, SessionEvent{
			Seq:       int64(i + 1),
			Type:      string(msg.Role),
			Timestamp: msg.Timestamp,
			Data: map[string]interface{}{
				"type":    string(msg.Role),
				"content": msg.Text,
			},
			Raw: msg.Text,
		})
	}
	return events, nil
}

func (r *sessionHandlerReader) ListSessions() ([]string, error) {
	threads, err := session_handler.QueryThreadsByProvider(r.kind)
	if err != nil {
		return nil, err
	}
	sessions := make([]string, 0, len(threads))
	for _, t := range threads {
		sessions = append(sessions, t.SessionID)
	}
	return sessions, nil
}

func (r *sessionHandlerReader) GetSessionMetadata(sessionID string) (*SessionMetadata, error) {
	thread, err := r.provider.Resolve(sessionID)
	if err != nil {
		return nil, err
	}
	messages, err := r.provider.ReadMessages(thread)
	if err != nil {
		return nil, err
	}
	return &SessionMetadata{
		SessionID:    sessionID,
		Provider:     string(r.kind),
		MessageCount: len(messages),
	}, nil
}
