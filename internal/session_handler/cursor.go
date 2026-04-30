package session_handler

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// cursorProvider reads Cursor IDE session files.
// Sessions stored as SQLite: ~/.cursor/chats/*/<session_id>/store.db
// Uses protobuf blob traversal with materialization to JSONL.
type cursorProvider struct {
	rootDir string // ~/.cursor
}

func (p *cursorProvider) QueryProviderThreads() ([]ThreadInfo, error) {
	return queryCursorThreads(p)
}

func newCursorProvider(rootDir string) *cursorProvider {
	if rootDir == "" {
		// Check CURSOR_DATA_DIR and CURSOR_CONFIG_DIR env vars
		for _, env := range []string{"CURSOR_DATA_DIR", "CURSOR_CONFIG_DIR"} {
			if dir := os.Getenv(env); dir != "" {
				rootDir = dir
				break
			}
		}
		if rootDir == "" {
			rootDir = filepath.Join(homeDir(), ".cursor")
		}
	}
	return &cursorProvider{rootDir: rootDir}
}

func (p *cursorProvider) Kind() ProviderKind { return ProviderCursor }

func (p *cursorProvider) Resolve(sessionID string) (*ResolvedThread, error) {
	chatsDir := filepath.Join(p.rootDir, "chats")
	if _, err := os.Stat(chatsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("cursor chats dir not found: %s", chatsDir)
	}

	// Walk chats directory looking for store.db files at depth 3
	var matches []string
	filepath.Walk(chatsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == "store.db" {
			// Check if path contains sessionID
			if strings.Contains(path, sessionID) {
				matches = append(matches, path)
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return nil, fmt.Errorf("cursor session not found: %s", sessionID)
	}

	return &ResolvedThread{
		Provider:  ProviderCursor,
		SessionID: sessionID,
		Path:      selectLatestFile(matches),
		Metadata: ResolutionMeta{
			Source:         "store.db_scan",
			CandidateCount: len(matches),
		},
	}, nil
}

// ReadMessages reads Cursor store.db and materializes to messages.
// Cursor stores data as SQLite with protobuf blobs.
// We read the meta table (hex-encoded JSON) and blobs table.
func (p *cursorProvider) ReadMessages(thread *ResolvedThread) ([]ThreadMessage, error) {
	db, err := sql.Open("sqlite3", thread.Path)
	if err != nil {
		return nil, fmt.Errorf("open store.db: %w", err)
	}
	defer db.Close()

	// Read meta table (key='0' contains hex-encoded chat metadata)
	var hexMeta string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = '0'`).Scan(&hexMeta)
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", err)
	}

	metaJSON, err := decodeHexJSON(hexMeta)
	if err != nil {
		return nil, fmt.Errorf("decode meta hex: %w", err)
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return nil, fmt.Errorf("parse meta JSON: %w", err)
	}

	// Find root blob and walk the tree
	rootBlobID, _ := meta["latestRootBlobId"].(string)
	if rootBlobID == "" {
		return nil, fmt.Errorf("no root blob id in meta")
	}

	// Walk protobuf blobs to extract text
	messages, err := p.walkBlobs(db, rootBlobID)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// walkBlobs traverses protobuf blob tree to extract messages.
func (p *cursorProvider) walkBlobs(db *sql.DB, rootBlobID string) ([]ThreadMessage, error) {
	// Materialize to temp JSONL like xurl-core does
	tmpDir := tempDir(ProviderCursor, p.rootDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	materializedPath := filepath.Join(tmpDir, rootBlobID+".jsonl")
	// Check if already materialized
	if info, err := os.Stat(materializedPath); err == nil && info.Size() > 0 {
		return readJSONLMessages(materializedPath)
	}

	// Walk blobs recursively and extract text
	var messages []ThreadMessage
	visited := make(map[string]bool)
	p.extractFromBlob(db, rootBlobID, visited, &messages, RoleUser)

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages extracted from blobs")
	}

	// Materialize for future reads
	if f, err := os.Create(materializedPath); err == nil {
		enc := json.NewEncoder(f)
		for _, m := range messages {
			enc.Encode(m)
		}
		f.Close()
	}

	return messages, nil
}

// extractFromBlob recursively extracts messages from protobuf blobs.
func (p *cursorProvider) extractFromBlob(db *sql.DB, blobID string, visited map[string]bool, messages *[]ThreadMessage, defaultRole MessageRole) {
	if visited[blobID] {
		return
	}
	visited[blobID] = true

	var data []byte
	err := db.QueryRow(`SELECT data FROM blobs WHERE id = ?`, blobID).Scan(&data)
	if err != nil {
		return
	}

	// Parse as protobuf wire format
	// Extract text segments and nested blob references
	texts, childBlobs := parseProtobufBlob(data)

	if len(texts) > 0 {
		role := defaultRole
		text := strings.Join(texts, "\n")
		*messages = append(*messages, ThreadMessage{Role: role, Text: text})
	}

	// Recursively walk child blobs
	for _, childID := range childBlobs {
		if len(childID) == 64 {
			// 32-byte hex blob reference
			p.extractFromBlob(db, childID, visited, messages, roleFromBlobID(childID))
		}
	}
}

// parseProtobufBlob does basic protobuf wire format parsing to extract text and blob references.
// This is a simplified parser - xurl-core's full implementation handles more edge cases.
func parseProtobufBlob(data []byte) (texts []string, blobRefs []string) {
	if len(data) == 0 {
		return
	}

	i := 0
	for i < len(data) {
		if i >= len(data) {
			break
		}
		// Read field tag (varint)
		tag, n := decodeVarint(data[i:])
		if n == 0 {
			i++
			continue
		}
		i += n

		wireType := tag & 0x07
		fieldNum := tag >> 3

		switch wireType {
		case 0: // varint
			_, n := decodeVarint(data[i:])
			i += n
		case 1: // 64-bit
			i += 8
		case 2: // length-delimited
			length, n := decodeVarint(data[i:])
			if n == 0 {
				i++
				continue
			}
			i += n
			if int(length) > len(data)-i {
				break
			}
			payload := data[i : i+int(length)]
			i += int(length)

			// Check if payload is a 32-byte blob reference (64 hex chars)
			hexStr := fmt.Sprintf("%x", payload)
			if len(payload) == 32 {
				blobRefs = append(blobRefs, hexStr)
			} else if fieldNum >= 1 && len(payload) > 0 {
				// Try to extract as UTF-8 text
				if isMostlyPrintable(payload) {
					text := strings.TrimSpace(string(payload))
					if text != "" && len(text) > 1 {
						texts = append(texts, text)
					}
				}
			}
		case 5: // 32-bit
			i += 4
		default:
			break // Unknown wire type, stop
		}
	}
	return
}

// decodeVarint decodes a protobuf varint.
func decodeVarint(data []byte) (uint64, int) {
	var result uint64
	var shift uint
	for i := 0; i < len(data) && i < 10; i++ {
		b := data[i]
		result |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1
		}
		shift += 7
	}
	return 0, 0
}

// isMostlyPrintable checks if bytes are mostly printable UTF-8 text.
func isMostlyPrintable(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	printable := 0
	for _, b := range data {
		if b >= 32 && b < 127 || b == '\n' || b == '\r' || b == '\t' || b >= 128 {
			printable++
		}
	}
	return float64(printable)/float64(len(data)) > 0.8
}

// roleFromBlobID tries to determine role from blob position (heuristic).
func roleFromBlobID(id string) MessageRole {
	return RoleAssistant // Default, real implementation alternates
}

// readJSONLMessages reads messages from a materialized JSONL file.
func readJSONLMessages(path string) ([]ThreadMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []ThreadMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var msg ThreadMessage
		if json.Unmarshal(scanner.Bytes(), &msg) == nil && msg.Text != "" {
			messages = append(messages, msg)
		}
	}
	return messages, scanner.Err()
}
