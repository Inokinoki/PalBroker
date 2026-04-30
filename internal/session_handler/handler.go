// Package xurl provides multi-provider AI agent session reading in Go.
// Ported from https://github.com/Xuanwo/xurl (xurl-core Rust library).
//
// xurl reads session files from AI coding agents (Claude, Codex, Copilot,
// Cursor, Gemini, Amp, Kimi, OpenCode, Pi) using the agents:// URI scheme.
package session_handler

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProviderKind identifies an AI agent provider.
type ProviderKind string

const (
	ProviderClaude   ProviderKind = "claude"
	ProviderCodex    ProviderKind = "codex"
	ProviderCopilot  ProviderKind = "copilot"
	ProviderCursor   ProviderKind = "cursor"
	ProviderGemini   ProviderKind = "gemini"
	ProviderAmp      ProviderKind = "amp"
	ProviderKimi     ProviderKind = "kimi"
	ProviderOpenCode ProviderKind = "opencode"
	ProviderPi       ProviderKind = "pi"
)

// MessageRole is the role of a message author.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

// ThreadMessage represents a single message in a conversation thread.
type ThreadMessage struct {
	Role MessageRole `json:"role"`
	Text string      `json:"text"`
}

// ResolutionMeta contains metadata about how a thread was resolved.
type ResolutionMeta struct {
	Source         string   `json:"source"`
	CandidateCount int      `json:"candidate_count"`
	Warnings       []string `json:"warnings,omitempty"`
}

// ResolvedThread represents a resolved conversation thread on disk.
type ResolvedThread struct {
	Provider  ProviderKind   `json:"provider"`
	SessionID string         `json:"session_id"`
	Path      string         `json:"path"`
	Metadata  ResolutionMeta `json:"metadata"`
}

// Provider is the interface for reading AI agent sessions.
type Provider interface {
	// Kind returns the provider type.
	Kind() ProviderKind
	// Resolve finds and returns a resolved thread by session ID.
	Resolve(sessionID string) (*ResolvedThread, error)
	// ReadMessages reads all messages from a resolved thread.
	ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error)
}

// ThreadInfo is a lightweight summary discovered during query.
type ThreadInfo struct {
	SessionID string       `json:"session_id"`
	Provider  ProviderKind `json:"provider"`
	Path      string       `json:"path"`
	ModTime   time.Time    `json:"mod_time"`
}

// NewProvider creates a Provider for the given kind.
// rootDir overrides the default data directory for the provider (empty = auto-detect).
func NewProvider(kind ProviderKind, rootDir string) (Provider, error) {
	switch kind {
	case ProviderClaude:
		return newClaudeProvider(rootDir), nil
	case ProviderCodex:
		return newCodexProvider(rootDir), nil
	case ProviderCopilot:
		return newCopilotProvider(rootDir), nil
	case ProviderCursor:
		return newCursorProvider(rootDir), nil
	case ProviderGemini:
		return newGeminiProvider(rootDir), nil
	case ProviderAmp:
		return newAmpProvider(rootDir), nil
	case ProviderKimi:
		return newKimiProvider(rootDir), nil
	case ProviderOpenCode:
		return newOpenCodeProvider(rootDir), nil
	case ProviderPi:
		return newPiProvider(rootDir), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", kind)
	}
}

// ProviderKinds returns all supported provider kinds.
func ProviderKinds() []ProviderKind {
	return []ProviderKind{
		ProviderClaude, ProviderCodex, ProviderCopilot, ProviderCursor,
		ProviderGemini, ProviderAmp, ProviderKimi, ProviderOpenCode, ProviderPi,
	}
}

// ParseProviderKind parses a provider name (case-insensitive).
func ParseProviderKind(s string) (ProviderKind, bool) {
	switch ProviderKind(strings.ToLower(s)) {
	case ProviderClaude:
		return ProviderClaude, true
	case ProviderCodex:
		return ProviderCodex, true
	case ProviderCopilot:
		return ProviderCopilot, true
	case ProviderCursor:
		return ProviderCursor, true
	case ProviderGemini:
		return ProviderGemini, true
	case ProviderAmp:
		return ProviderAmp, true
	case ProviderKimi:
		return ProviderKimi, true
	case ProviderOpenCode:
		return ProviderOpenCode, true
	case ProviderPi:
		return ProviderPi, true
	default:
		return "", false
	}
}

// homeDir returns the user's home directory or a fallback.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/tmp"
}

// xdgDataHome returns XDG_DATA_HOME or default.
func xdgDataHome() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return xdg
	}
	return filepath.Join(homeDir(), ".local", "share")
}

// tempDir returns a provider-specific temp directory using FNV hash for isolation.
// Mirrors xurl-core's hash-based temp file strategy.
func tempDir(provider ProviderKind, rootPath string) string {
	h := fnv.New64a()
	h.Write([]byte(rootPath))
	return filepath.Join(os.TempDir(), "xurl-"+string(provider), fmt.Sprintf("%016x", h.Sum64()))
}

// decodeHexJSON decodes a hex-encoded JSON string.
func decodeHexJSON(hexStr string) ([]byte, error) {
	return hex.DecodeString(hexStr)
}

// selectLatestFile returns the most recently modified file from a list.
func selectLatestFile(files []string) string {
	if len(files) == 0 {
		return ""
	}
	if len(files) == 1 {
		return files[0]
	}

	var best string
	var bestTime time.Time
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			if best == "" || info.ModTime().After(bestTime) {
				best = f
				bestTime = info.ModTime()
			}
		}
	}
	if best == "" {
		return files[0]
	}
	return best
}
