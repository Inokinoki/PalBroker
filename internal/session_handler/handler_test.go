package session_handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestProviderKinds verifies all provider kinds are defined.
func TestProviderKinds(t *testing.T) {
	kinds := ProviderKinds()
	if len(kinds) != 9 {
		t.Errorf("Expected 9 provider kinds, got %d", len(kinds))
	}

	expected := map[ProviderKind]bool{
		ProviderClaude:   true,
		ProviderCodex:    true,
		ProviderCopilot:  true,
		ProviderCursor:   true,
		ProviderGemini:   true,
		ProviderAmp:      true,
		ProviderKimi:     true,
		ProviderOpenCode: true,
		ProviderPi:       true,
	}

	for _, k := range kinds {
		if !expected[k] {
			t.Errorf("Unexpected provider kind: %s", k)
		}
	}
}

// TestParseProviderKind tests provider name parsing.
func TestParseProviderKind(t *testing.T) {
	tests := []struct {
		input    string
		expected ProviderKind
		ok       bool
	}{
		{"claude", ProviderClaude, true},
		{"Claude", ProviderClaude, true},
		{"CLAUDE", ProviderClaude, true},
		{"codex", ProviderCodex, true},
		{"copilot", ProviderCopilot, true},
		{"cursor", ProviderCursor, true},
		{"gemini", ProviderGemini, true},
		{"amp", ProviderAmp, true},
		{"kimi", ProviderKimi, true},
		{"opencode", ProviderOpenCode, true},
		{"pi", ProviderPi, true},
		{"unknown", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		got, ok := ParseProviderKind(tt.input)
		if got != tt.expected || ok != tt.ok {
			t.Errorf("ParseProviderKind(%q) = (%s, %v), want (%s, %v)", tt.input, got, ok, tt.expected, tt.ok)
		}
	}
}

// TestNewProvider tests provider creation.
func TestNewProvider(t *testing.T) {
	for _, kind := range ProviderKinds() {
		p, err := NewProvider(kind, "/tmp/test-xurl")
		if err != nil {
			t.Errorf("NewProvider(%s) error: %v", kind, err)
		}
		if p.Kind() != kind {
			t.Errorf("Provider.Kind() = %s, want %s", p.Kind(), kind)
		}
	}

	// Unsupported provider
	_, err := NewProvider("unknown", "")
	if err == nil {
		t.Error("Expected error for unsupported provider")
	}
}

// TestClaudeProviderResolve tests Claude session resolution with mock files.
func TestClaudeProviderResolve(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects", "test-hash")
	os.MkdirAll(projectsDir, 0755)

	sessionID := "test-session-123"
	sessionFile := filepath.Join(projectsDir, sessionID+".jsonl")

	// Write a session file
	f, _ := os.Create(sessionFile)
	json.NewEncoder(f).Encode(map[string]interface{}{
		"type":      "assistant",
		"sessionId": sessionID,
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Hello from Claude"},
			},
		},
	})
	f.Close()

	p := newClaudeProvider(tmpDir)

	// Test Resolve
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if thread.SessionID != sessionID {
		t.Errorf("SessionID = %s, want %s", thread.SessionID, sessionID)
	}
	if thread.Provider != ProviderClaude {
		t.Errorf("Provider = %s, want %s", thread.Provider, ProviderClaude)
	}

	// Test ReadMessages
	messages, err := p.ReadMessages(thread)
	if err != nil {
		t.Fatalf("ReadMessages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Messages count = %d, want 1", len(messages))
	}
	if messages[0].Role != RoleAssistant {
		t.Errorf("Message role = %s, want %s", messages[0].Role, RoleAssistant)
	}
	if messages[0].Text != "Hello from Claude" {
		t.Errorf("Message text = %s, want 'Hello from Claude'", messages[0].Text)
	}
}

// TestClaudeProviderFromIndex tests Claude resolution via sessions-index.json.
func TestClaudeProviderFromIndex(t *testing.T) {
	tmpDir := t.TempDir()
	projectsDir := filepath.Join(tmpDir, "projects", "test-hash")
	os.MkdirAll(projectsDir, 0755)

	sessionID := "index-session-456"
	sessionFile := filepath.Join(projectsDir, sessionID+".jsonl")

	// Write session file
	f, _ := os.Create(sessionFile)
	json.NewEncoder(f).Encode(map[string]interface{}{
		"type":      "user",
		"sessionId": sessionID,
		"message": map[string]interface{}{
			"role":    "user",
			"content": "Test message",
		},
	})
	f.Close()

	// Write sessions-index.json
	idxFile := filepath.Join(projectsDir, "sessions-index.json")
	idxData := map[string]interface{}{
		"entries": []interface{}{
			map[string]interface{}{
				"sessionId": sessionID,
				"fullPath":  sessionFile,
			},
		},
	}
	idxBytes, _ := json.Marshal(idxData)
	os.WriteFile(idxFile, idxBytes, 0644)

	p := newClaudeProvider(tmpDir)
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if thread.Metadata.Source != "sessions-index.json" {
		t.Errorf("Source = %s, want 'sessions-index.json'", thread.Metadata.Source)
	}
}

// TestCodexProviderResolve tests Codex session resolution.
func TestCodexProviderResolve(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2024", "01", "01")
	os.MkdirAll(sessionsDir, 0755)

	sessionID := "codex-session-789"
	rolloutFile := filepath.Join(sessionsDir, "rollout-1704067200-"+sessionID+".jsonl")

	f, _ := os.Create(rolloutFile)
	json.NewEncoder(f).Encode(map[string]interface{}{
		"type": "item.completed",
		"item": map[string]interface{}{
			"type": "agent_message",
			"text": "Codex response",
		},
	})
	f.Close()

	p := newCodexProvider(tmpDir)
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if thread.SessionID != sessionID {
		t.Errorf("SessionID = %s, want %s", thread.SessionID, sessionID)
	}

	messages, err := p.ReadMessages(thread)
	if err != nil {
		t.Fatalf("ReadMessages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Messages count = %d, want 1", len(messages))
	}
	if messages[0].Text != "Codex response" {
		t.Errorf("Text = %s, want 'Codex response'", messages[0].Text)
	}
}

// TestCopilotProviderResolve tests Copilot session resolution.
func TestCopilotProviderResolve(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "session-state", "copilot-session")
	os.MkdirAll(sessionDir, 0755)

	sessionID := "copilot-session"
	eventsFile := filepath.Join(sessionDir, "events.jsonl")

	f, _ := os.Create(eventsFile)
	json.NewEncoder(f).Encode(map[string]interface{}{
		"type": "assistant.message",
		"data": map[string]interface{}{
			"content": "Copilot says hello",
		},
	})
	f.Close()

	p := newCopilotProvider(tmpDir)
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	messages, err := p.ReadMessages(thread)
	if err != nil {
		t.Fatalf("ReadMessages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Messages count = %d, want 1", len(messages))
	}
	if messages[0].Text != "Copilot says hello" {
		t.Errorf("Text = %s, want 'Copilot says hello'", messages[0].Text)
	}
}

// TestGeminiProviderResolve tests Gemini session resolution.
func TestGeminiProviderResolve(t *testing.T) {
	tmpDir := t.TempDir()
	chatsDir := filepath.Join(tmpDir, "tmp", "abc123", "chats")
	os.MkdirAll(chatsDir, 0755)

	sessionID := "gemini-session-000"
	sessionFile := filepath.Join(chatsDir, "session-1704067200.json")

	sessionData := map[string]interface{}{
		"sessionId": sessionID,
		"messages": []interface{}{
			map[string]interface{}{"type": "user", "content": "Hello Gemini"},
			map[string]interface{}{"type": "gemini", "content": "Hi there!"},
		},
	}
	data, _ := json.Marshal(sessionData)
	os.WriteFile(sessionFile, data, 0644)

	p := newGeminiProvider(tmpDir)
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	messages, err := p.ReadMessages(thread)
	if err != nil {
		t.Fatalf("ReadMessages error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("Messages count = %d, want 2", len(messages))
	}
	if messages[0].Role != RoleUser {
		t.Errorf("Message[0] role = %s, want user", messages[0].Role)
	}
	if messages[1].Role != RoleAssistant {
		t.Errorf("Message[1] role = %s, want assistant", messages[1].Role)
	}
}

// TestAmpProviderResolve tests Amp session resolution.
func TestAmpProviderResolve(t *testing.T) {
	tmpDir := t.TempDir()
	threadsDir := filepath.Join(tmpDir, "threads")
	os.MkdirAll(threadsDir, 0755)

	sessionID := "amp-session-111"
	threadFile := filepath.Join(threadsDir, sessionID+".json")

	sessionData := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "Fix this bug"},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "I'll fix it"},
				},
			},
		},
	}
	data, _ := json.Marshal(sessionData)
	os.WriteFile(threadFile, data, 0644)

	p := newAmpProvider(tmpDir)
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	messages, err := p.ReadMessages(thread)
	if err != nil {
		t.Fatalf("ReadMessages error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("Messages count = %d, want 2", len(messages))
	}
	if messages[0].Text != "Fix this bug" {
		t.Errorf("Text = %s, want 'Fix this bug'", messages[0].Text)
	}
}

// TestKimiProviderResolve tests Kimi session resolution.
func TestKimiProviderResolve(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "kimi-session-222"
	// Create sessions dir with MD5 hash of workdir
	contextDir := filepath.Join(tmpDir, "sessions", "abc123", sessionID)
	os.MkdirAll(contextDir, 0755)

	contextFile := filepath.Join(contextDir, "context.jsonl")
	f, _ := os.Create(contextFile)
	json.NewEncoder(f).Encode(map[string]interface{}{
		"role":    "user",
		"content": "Help me code",
	})
	json.NewEncoder(f).Encode(map[string]interface{}{
		"role":    "assistant",
		"content": "Sure, what do you need?",
	})
	f.Close()

	p := newKimiProvider(tmpDir)
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	messages, err := p.ReadMessages(thread)
	if err != nil {
		t.Fatalf("ReadMessages error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("Messages count = %d, want 2", len(messages))
	}
}

// TestPiProviderResolve tests Pi session resolution.
func TestPiProviderResolve(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0755)

	sessionID := "pi-session-333"
	sessionFile := filepath.Join(sessionsDir, "1704067200_"+sessionID+".jsonl")

	f, _ := os.Create(sessionFile)
	// Pi header
	json.NewEncoder(f).Encode(map[string]interface{}{
		"type":    "session",
		"version": 3,
		"id":      sessionID,
	})
	// Pi message
	json.NewEncoder(f).Encode(map[string]interface{}{
		"type": "message",
		"message": map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Hello Pi"},
			},
		},
	})
	f.Close()

	p := newPiProvider(tmpDir)
	thread, err := p.Resolve(sessionID)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	messages, err := p.ReadMessages(thread)
	if err != nil {
		t.Fatalf("ReadMessages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Messages count = %d, want 1", len(messages))
	}
	if messages[0].Text != "Hello Pi" {
		t.Errorf("Text = %s, want 'Hello Pi'", messages[0].Text)
	}
}

// TestResolveThread tests the service function.
func TestResolveThread(t *testing.T) {
	// Test with non-existent session (should fail gracefully)
	_, err := ResolveThread(ProviderClaude, "nonexistent-session")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
}

// TestReadThread tests the convenience function.
func TestReadThread(t *testing.T) {
	_, err := ReadThread(ProviderClaude, "nonexistent-session")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
}

// TestRenderThreadMarkdown tests markdown rendering.
func TestRenderThreadMarkdown(t *testing.T) {
	messages := []ThreadMessage{
		{Role: RoleUser, Text: "Hello"},
		{Role: RoleAssistant, Text: "Hi there"},
	}

	md := RenderThreadMarkdown(messages)
	if md == "" {
		t.Error("Expected non-empty markdown")
	}
	if !contains(md, "User") {
		t.Error("Expected 'User' in markdown")
	}
	if !contains(md, "Assistant") {
		t.Error("Expected 'Assistant' in markdown")
	}
}

// TestExtractContentText tests content extraction.
func TestExtractContentText(t *testing.T) {
	// String content
	if text := extractContentText("hello"); text != "hello" {
		t.Errorf("String content = %s, want 'hello'", text)
	}

	// Array content
	arr := []interface{}{
		map[string]interface{}{"type": "text", "text": "part1"},
		map[string]interface{}{"type": "text", "text": "part2"},
	}
	if text := extractContentText(arr); text != "part1\npart2" {
		t.Errorf("Array content = %s, want 'part1\\npart2'", text)
	}

	// Non-text block (should be skipped)
	arr2 := []interface{}{
		map[string]interface{}{"type": "image", "url": "..."},
		map[string]interface{}{"type": "text", "text": "only-text"},
	}
	if text := extractContentText(arr2); text != "only-text" {
		t.Errorf("Filtered content = %s, want 'only-text'", text)
	}
}

// TestDecodeHexJSON tests hex decoding.
func TestDecodeHexJSON(t *testing.T) {
	// "hello" in hex
	result, err := decodeHexJSON("68656c6c6f")
	if err != nil {
		t.Fatalf("decodeHexJSON error: %v", err)
	}
	if string(result) != "hello" {
		t.Errorf("Result = %s, want 'hello'", string(result))
	}
}

// TestSelectLatestFile tests file selection.
func TestSelectLatestFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with different times
	f1 := filepath.Join(tmpDir, "file1.txt")
	f2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(f1, []byte("first"), 0644)
	os.WriteFile(f2, []byte("second"), 0644)

	result := selectLatestFile([]string{f1, f2})
	if result == "" {
		t.Error("Expected non-empty result")
	}

	// Empty list
	if result := selectLatestFile([]string{}); result != "" {
		t.Error("Expected empty result for empty list")
	}
}

// TestProviderInterface verifies all providers implement Provider.
func TestProviderInterface(t *testing.T) {
	var _ Provider = (*claudeProvider)(nil)
	var _ Provider = (*codexProvider)(nil)
	var _ Provider = (*copilotProvider)(nil)
	var _ Provider = (*cursorProvider)(nil)
	var _ Provider = (*geminiProvider)(nil)
	var _ Provider = (*ampProvider)(nil)
	var _ Provider = (*kimiProvider)(nil)
	var _ Provider = (*openCodeProvider)(nil)
	var _ Provider = (*piProvider)(nil)
}

// contains is a helper for string contains check.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
