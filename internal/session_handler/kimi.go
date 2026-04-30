package session_handler

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// kimiProvider reads Kimi session files.
// Sessions: ~/.kimi/sessions/<md5(workdir)>/<session_id>/context.jsonl
// Metadata: ~/.kimi/kimi.json
type kimiProvider struct {
	rootDir string // ~/.kimi
}

func (p *kimiProvider) QueryProviderThreads() ([]ThreadInfo, error) {
	return queryKimiThreads(p)
}

func newKimiProvider(rootDir string) *kimiProvider {
	if rootDir == "" {
		if dir := os.Getenv("KIMI_SHARE_DIR"); dir != "" {
			rootDir = dir
		} else {
			rootDir = filepath.Join(homeDir(), ".kimi")
		}
	}
	return &kimiProvider{rootDir: rootDir}
}

func (p *kimiProvider) Kind() ProviderKind { return ProviderKimi }

// kimiMeta represents kimi.json metadata.
type kimiMeta struct {
	WorkDirs []kimiWorkDir `json:"work_dirs"`
}

type kimiWorkDir struct {
	Path          string `json:"path"`
	LastSessionID string `json:"last_session_id"`
}

func (p *kimiProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	sessionsDir := filepath.Join(p.rootDir, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("kimi sessions dir not found: %s", sessionsDir)
	}

	// Strategy 1: Use kimi.json metadata to find by workdir hash
	if path, meta := p.findFromMetadata(sessionID); path != "" {
		return &ResolvedThread{
			Provider:  ProviderKimi,
			SessionID: sessionID,
			Path:      path,
			Metadata:  meta,
		}, nil
	}

	// Strategy 2: Walk sessions dir (depth 2) for matching session ID
	var matches []string
	filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == "context.jsonl" {
			// Check if parent dir matches session ID
			parentDir := filepath.Base(filepath.Dir(path))
			if parentDir == sessionID {
				matches = append(matches, path)
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return nil, fmt.Errorf("kimi session not found: %s", sessionID)
	}

	return &ResolvedThread{
		Provider:  ProviderKimi,
		SessionID: sessionID,
		Path:      selectLatestFile(matches),
		Metadata: ResolutionMeta{
			Source:         "filesystem_scan",
			CandidateCount: len(matches),
		},
	}, nil
}

func (p *kimiProvider) findFromMetadata(sessionID string) (string, ResolutionMeta) {
	metaPath := filepath.Join(p.rootDir, "kimi.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return "", ResolutionMeta{}
	}

	var meta kimiMeta
	if json.Unmarshal(data, &meta) != nil {
		return "", ResolutionMeta{}
	}

	for _, wd := range meta.WorkDirs {
		hash := md5Hash(wd.Path)
		contextPath := filepath.Join(p.rootDir, "sessions", hash, sessionID, "context.jsonl")
		if _, err := os.Stat(contextPath); err == nil {
			return contextPath, ResolutionMeta{
				Source:         "kimi.json",
				CandidateCount: 1,
			}
		}
	}
	return "", ResolutionMeta{}
}

// md5Hash returns MD5 hex hash of a string (matches xurl-core's Python-compatible MD5).
func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// ReadMessages reads Kimi JSONL session.
// Format: {"role": "user|assistant", "content": "..."}
func (p *kimiProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
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
		var entry struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Content == "" {
			continue
		}
		messages = append(messages, ThreadMessage{
			Role: MessageRole(entry.Role),
			Text: entry.Content,
		})
	}
	return messages, scanner.Err()
}
