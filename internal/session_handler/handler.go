// Package xurl provides multi-provider AI agent session reading in Go.
// Ported from https://github.com/Xuanwo/xurl (xurl-core Rust library).
//
// xurl reads session files from AI coding agents (Claude, Codex, Copilot,
// Cursor, Gemini, Amp, Kimi, OpenCode, Pi) using the agents:// URI scheme.
package session_handler

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
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
	Role      MessageRole `json:"role"`
	Text      string      `json:"text"`
	Timestamp int64       `json:"timestamp,omitempty"` // Unix milliseconds, 0 if unknown
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

// providerFactory creates a Provider given a rootDir.
type providerFactory func(rootDir string) Provider

// providerRegistry maps ProviderKind to its factory.
// Each provider registers itself via init() — adding a new provider only
// requires creating its file and adding a single registerProvider call.
var providerRegistry = map[ProviderKind]providerFactory{}

// registerProvider adds a provider to the global registry.
func registerProvider(kind ProviderKind, factory providerFactory) {
	providerRegistry[kind] = factory
}

// NewProvider creates a Provider for the given kind.
// rootDir overrides the default data directory for the provider (empty = auto-detect).
func NewProvider(kind ProviderKind, rootDir string) (Provider, error) {
	factory, ok := providerRegistry[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", kind)
	}
	return factory(rootDir), nil
}

// ProviderKinds returns all supported provider kinds.
func ProviderKinds() []ProviderKind {
	kinds := make([]ProviderKind, 0, len(providerRegistry))
	for k := range providerRegistry {
		kinds = append(kinds, k)
	}
	return kinds
}

// ParseProviderKind parses a provider name (case-insensitive).
func ParseProviderKind(s string) (ProviderKind, bool) {
	kind := ProviderKind(strings.ToLower(s))
	_, ok := providerRegistry[kind]
	if !ok {
		return "", false
	}
	return kind, ok
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

// readJSONLFile reads a JSONL file and calls fn for each parsed JSON line.
// Handles file open/close, line trimming, empty line skipping, and JSON parsing.
// Returns the scanner error if any.
func readJSONLFile(path string, fn func(line string, entry map[string]interface{})) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

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
		fn(line, entry)
	}
	return scanner.Err()
}
