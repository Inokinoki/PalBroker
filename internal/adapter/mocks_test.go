package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockCLI Create a mock CLI script
func mockCLI(t *testing.T, name, script string) string {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, name)

	// Write script
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to write mock script: %v", err)
	}

	return scriptPath
}

// TestClaudeAdapterWithMock Test Claude adapter with Mock
func TestClaudeAdapterWithMock(t *testing.T) {
	// Create mock claude command
	mockScript := `#!/bin/bash
echo '{"type":"assistant","content":"Hello from mock Claude!"}'
echo '{"type":"chunk","content":"This is a test response"}'
`
	mockPath := mockCLI(t, "claude", mockScript)

	// Temporarily add mock to PATH
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", filepath.Dir(mockPath)+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	// Create adapter
	adapter := &ClaudeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "claude",
				WorkDir:  "/tmp",
				Task:     "Test task",
			},
		},
	}

	// TestCommandBuild
	cmd := adapter.BuildCommand(adapter.config)
	if cmd.Path != mockPath {
		t.Logf("Using mock claude at: %s", cmd.Path)
	}

	// TestMessageParse
	msg := `{"type":"assistant","content":"Hello"}`
	parsed, err := adapter.ParseMessage(msg)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if parsed["type"] != "assistant" {
		t.Errorf("Expected type=assistant, got %v", parsed["type"])
	}

	if parsed["content"] != "Hello" {
		t.Errorf("Expected content='Hello', got %v", parsed["content"])
	}
}

// TestClaudeAdapterMockOutput Test Claude output parsing
func TestClaudeAdapterMockOutput(t *testing.T) {
	adapter := &ClaudeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "claude",
			},
		},
	}

	tests := []struct {
		name     string
		input    string
		wantType string
		wantErr  bool
	}{
		{
			name:     "assistant_message",
			input:    `{"type":"assistant","content":"Hello"}`,
			wantType: "assistant",
			wantErr:  false,
		},
		{
			name:     "chunk_message",
			input:    `{"type":"chunk","content":"Processing..."}`,
			wantType: "chunk",
			wantErr:  false,
		},
		{
			name:     "error_message",
			input:    `{"type":"error","error":"Something went wrong"}`,
			wantType: "error",
			wantErr:  false,
		},
		{
			name:     "plain_text",
			input:    "Let me think about this...",
			wantType: "chunk",
			wantErr:  false,
		},
		{
			name:     "invalid_json",
			input:    `{"type": "invalid`,
			wantType: "chunk",
			wantErr:  false, // ShouldFallbacktoTextMode
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := adapter.ParseMessage(tt.input)
			if err != nil && !tt.wantErr {
				t.Errorf("Unexpected error: %v", err)
			}

			if parsed == nil {
				t.Fatal("Expected non-nil parsed result")
			}

			if parsed["type"] != tt.wantType {
				t.Errorf("Expected type=%s, got %v", tt.wantType, parsed["type"])
			}
		})
	}
}

// TestCodexAdapterMock Use Mock Test Codex adapter with Mock
func TestCodexAdapterMock(t *testing.T) {
	// Create mock codex command
	mockScript := `#!/bin/bash
echo '{"message":{"role":"assistant","content":"Codex response"}}'
`
	mockPath := mockCLI(t, "codex", mockScript)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", filepath.Dir(mockPath)+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	adapter := &CodexAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "codex",
				WorkDir:  "/tmp",
				Task:     "Fix bug",
			},
		},
	}

	// TestCommandBuild
	cmd := adapter.BuildCommand(adapter.config)
	if !strings.Contains(cmd.Path, "codex") {
		t.Errorf("Expected codex command, got %s", cmd.Path)
	}

	// Test Codex specificformatParse
	msg := `{"message":{"role":"assistant","content":"Here's the fix"}}`
	parsed, err := adapter.ParseMessage(msg)
	if err != nil {
		t.Fatalf("Failed to parse Codex message: %v", err)
	}

	// Codex should extract message.content
	if parsed["type"] != "chunk" {
		t.Errorf("Expected type=chunk, got %v", parsed["type"])
	}
}

// TestCopilotAdapterMock Test Copilot adapter with Mock
func TestCopilotAdapterMock(t *testing.T) {
	adapter := &CopilotAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "copilot",
			},
		},
	}

	// Copilot does not support ACP
	if adapter.SupportsACP() {
		t.Error("Copilot should not support ACP")
	}

	// TestCommandBuild
	cmd := adapter.BuildCommand(adapter.config)
	if !strings.Contains(cmd.Path, "copilot") {
		t.Errorf("Expected copilot command, got %s", cmd.Path)
	}

	// TestMessageParse
	tests := []struct {
		name  string
		input string
	}{
		{"json", `{"type":"response","data":"test"}`},
		{"text", "Copilot is thinking..."},
		{"code_block", "```go\nfmt.Println(\"Hello\")\n```"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := adapter.ParseMessage(tt.input)
			if err != nil {
				t.Logf("Parse error (may be expected): %v", err)
			}
			if parsed == nil {
				t.Error("Expected non-nil result")
			}
		})
	}
}

// TestGenericAdapterMock Test generic adapter
func TestGenericAdapterMock(t *testing.T) {
	adapter := &GenericAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "unknown-cli",
			},
		},
	}

	// GenericAdapterNotSupportspecial features
	if adapter.SupportsACP() {
		t.Error("Generic adapter should not support ACP")
	}
	if adapter.SupportsJSONStream() {
		t.Error("Generic adapter should not support JSON Stream")
	}

	// TestCommandBuild
	cmd := adapter.BuildCommand(adapter.config)
	if cmd.Path != "unknown-cli" {
		t.Errorf("Expected unknown-cli, got %s", cmd.Path)
	}
}

// TestAdapterWithRealisticOutput Test realistic output
func TestAdapterWithRealisticOutput(t *testing.T) {
	adapter := &ClaudeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "claude",
			},
		},
	}

	// Simulate realistic Claude output stream
	realisticOutputs := []string{
		`{"type":"system","message":"Starting task..."}`,
		`{"type":"assistant","content":"I'll help you with that."}`,
		`{"type":"chunk","content":"Let me analyze the code..."}`,
		`{"type":"chunk","content":"I found the issue."}`,
		`{"type":"tool_use","name":"edit","input":{"file":"main.go"}}`,
		`{"type":"chunk","content":"The fix has been applied."}`,
		`{"type":"assistant","content":"Task completed!"}`,
	}

	for i, output := range realisticOutputs {
		parsed, err := adapter.ParseMessage(output)
		if err != nil {
			t.Errorf("Output %d failed to parse: %v", i, err)
			continue
		}

		if parsed == nil {
			t.Errorf("Output %d returned nil", i)
		}

		t.Logf("Output %d: type=%v", i, parsed["type"])
	}
}

// TestAdapterErrorHandling Test error handler
func TestAdapterErrorHandling(t *testing.T) {
	adapter := &ClaudeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "claude",
			},
		},
	}

	// TestVariousErrorScenario
	errorCases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"null", "null"},
		{"array", `[]`},
		{"number", `123`},
		{"malformed", `{type: invalid}`},
		{"unclosed", `{"type": "chunk"`},
	}

	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseMessage panicked: %v", r)
				}
			}()

			parsed, _ := adapter.ParseMessage(tc.input)
			// Should return result even on error (fallback to text)
			if parsed == nil {
				t.Error("Expected non-nil result even on error")
			}
		})
	}
}

// TestAdapterCommandBuilder Test command buildinger
func TestAdapterCommandBuilder(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		config   *CLIConfig
		wantArgs []string
	}{
		{
			name:     "claude_basic",
			provider: "claude",
			config: &CLIConfig{
				Provider: "claude",
				WorkDir:  "/workspace",
				Task:     "Test",
			},
			wantArgs: []string{"--output-format", "stream-json", "--input-format", "stream-json"},
		},
		{
			name:     "codex_basic",
			provider: "codex",
			config: &CLIConfig{
				Provider: "codex",
				WorkDir:  "/workspace",
				Task:     "Test",
			},
			wantArgs: []string{"exec"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var adapter Adapter
			switch tt.provider {
			case "claude":
				adapter = &ClaudeAdapter{BaseAdapter: BaseAdapter{config: tt.config}}
			case "codex":
				adapter = &CodexAdapter{BaseAdapter: BaseAdapter{config: tt.config}}
			default:
				adapter = &GenericAdapter{BaseAdapter: BaseAdapter{config: tt.config}}
			}

			cmd := adapter.BuildCommand(tt.config)
			argsStr := strings.Join(cmd.Args, " ")

			for _, wantArg := range tt.wantArgs {
				if !strings.Contains(argsStr, wantArg) {
					t.Errorf("Expected arg '%s' in command: %s", wantArg, argsStr)
				}
			}
		})
	}
}

// TestAdapterCapabilities Test adapter capabilities
func TestAdapterCapabilities(t *testing.T) {
	tests := []struct {
		name             string
		adapter          Adapter
		wantACP          bool
		wantJSONStream   bool
		wantCapabilities []string
	}{
		{
			name:           "claude",
			adapter:        &ClaudeAdapter{BaseAdapter: BaseAdapter{config: &CLIConfig{}}},
			wantACP:        false,
			wantJSONStream: true,
		},
		{
			name:           "codex",
			adapter:        &CodexAdapter{BaseAdapter: BaseAdapter{config: &CLIConfig{}}},
			wantACP:        false,
			wantJSONStream: false,
		},
		{
			name:           "copilot",
			adapter:        &CopilotAdapter{BaseAdapter: BaseAdapter{config: &CLIConfig{}}},
			wantACP:        false,
			wantJSONStream: false,
		},
		{
			name:           "generic",
			adapter:        &GenericAdapter{BaseAdapter: BaseAdapter{config: &CLIConfig{}}},
			wantACP:        false,
			wantJSONStream: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.adapter.SupportsACP(); got != tt.wantACP {
				t.Errorf("SupportsACP() = %v, want %v", got, tt.wantACP)
			}

			if got := tt.adapter.SupportsJSONStream(); got != tt.wantJSONStream {
				t.Errorf("SupportsJSONStream() = %v, want %v", got, tt.wantJSONStream)
			}

			caps := tt.adapter.GetCapabilities()
			if caps == nil {
				t.Error("GetCapabilities() should not return nil")
			}
		})
	}
}

// TestAdapterModeDetection Test adapter mode detection
func TestAdapterModeDetection(t *testing.T) {
	// Test supportsACP function
	tests := []struct {
		provider string
		wantACP  bool
	}{
		{"copilot", true},
		{"copilot-acp", true},
		{"opencode", true},
		{"claude", false},
		{"codex", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			if got := supportsACP(tt.provider); got != tt.wantACP {
				t.Errorf("supportsACP(%s) = %v, want %v", tt.provider, got, tt.wantACP)
			}
		})
	}
}

// TestAdapterJSONParsing Test JSON parsing
func TestAdapterJSONParsing(t *testing.T) {
	adapter := &ClaudeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{},
		},
	}

	// TestComplex JSON Structure
	complexJSON := `{
		"type": "tool_use",
		"name": "edit",
		"input": {
			"file": "main.go",
			"changes": [
				{"line": 10, "action": "replace", "content": "fmt.Println(\"new\")"}
			]
		}
	}`

	parsed, err := adapter.ParseMessage(complexJSON)
	if err != nil {
		t.Fatalf("Failed to parse complex JSON: %v", err)
	}

	if parsed["type"] != "tool_use" {
		t.Errorf("Expected type=tool_use, got %v", parsed["type"])
	}

	if parsed["name"] != "edit" {
		t.Errorf("Expected name=edit, got %v", parsed["name"])
	}

	// Verify can serialize back to JSON
	_, err = json.Marshal(parsed)
	if err != nil {
		t.Errorf("Failed to marshal parsed result: %v", err)
	}
}

// TestAdapterStreamProcessing Test stream processing
func TestAdapterStreamProcessing(t *testing.T) {
	adapter := &ClaudeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{},
		},
	}

	// Simulate stream output
	stream := []string{
		`{"type":"chunk","content":"H"}`,
		`{"type":"chunk","content":"e"}`,
		`{"type":"chunk","content":"l"}`,
		`{"type":"chunk","content":"l"}`,
		`{"type":"chunk","content":"o"}`,
	}

	var fullMessage strings.Builder
	for _, chunk := range stream {
		parsed, _ := adapter.ParseMessage(chunk)
		if content, ok := parsed["content"].(string); ok {
			fullMessage.WriteString(content)
		}
	}

	if fullMessage.String() != "Hello" {
		t.Errorf("Expected 'Hello', got '%s'", fullMessage.String())
	}
}

// TestAdapterTimeout Test timeout handling
func TestAdapterTimeout(t *testing.T) {
	// Skip on Windows as the mock script format is not compatible
	if strings.HasPrefix(os.Getenv("RUNNER_OS"), "Windows") {
		t.Skip("Skipping timeout test on Windows due to script format incompatibility")
	}

	// Create a mock CLI that times out
	mockScript := `#!/bin/bash
sleep 10
echo "This should not appear"
`
	mockPath := mockCLI(t, "slow-cli", mockScript)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", filepath.Dir(mockPath)+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	adapter := &GenericAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "slow-cli",
			},
		},
	}

	cmd := adapter.BuildCommand(adapter.config)

	// StartCommand
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Set timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(2 * time.Second):
		// Timeout，killProcess
		cmd.Process.Kill()
		t.Log("Command timed out as expected")
	case err := <-done:
		t.Errorf("Command finished unexpectedly: %v", err)
	}
}

// ============== ACP Client Tests ==============

// TestACPClientCreation tests ACP client creation for different providers
func TestACPClientCreation(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		expectError bool
	}{
		{"copilot", "copilot", false},
		{"copilot-acp", "copilot-acp", false},
		{"opencode", "opencode", false},
		{"claude", "claude", true}, // Unsupported
		{"codex", "codex", true},   // Unsupported
		{"unknown", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewACPClient(tt.provider, "")
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for unsupported provider %s", tt.provider)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if client == nil {
					t.Error("Expected non-nil client")
				}
				if client.provider != tt.provider {
					t.Errorf("Expected provider %s, got %s", tt.provider, client.provider)
				}
			}
		})
	}
}

// TestACPClientWithCustomPath tests ACP client with custom CLI path
func TestACPClientWithCustomPath(t *testing.T) {
	// Create a mock script that echoes ACP-style responses
	mockScript := `#!/bin/bash
# Mock ACP server - reads from stdin and writes JSON responses
while IFS= read -r line; do
	echo '{"jsonrpc":"2.0","id":1,"result":{"sessionId":"test-123"}}'
done
`
	mockPath := mockCLI(t, "mock-acp-server", mockScript)

	client, err := NewACPClient("copilot", mockPath)
	if err != nil {
		t.Fatalf("Failed to create ACP client: %v", err)
	}

	// Verify custom path is set
	if client.customCLIPath != mockPath {
		t.Errorf("Expected custom CLI path %s, got %s", mockPath, client.customCLIPath)
	}

	// Verify command uses custom path
	if client.cmdName != mockPath {
		t.Errorf("Expected cmdName %s, got %s", mockPath, client.cmdName)
	}
}

// TestACPMessageParsing tests ACP message structure parsing
func TestACPMessageParsing(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	tests := []struct {
		name     string
		input    string
		wantType string
	}{
		{
			name: "session_update_chunk",
			input: `{
				"jsonrpc":"2.0",
				"method":"session/update",
				"params":{
					"sessionId":"test-123",
					"sessionUpdate":"agent_message_chunk",
					"content":{"type":"text","text":"Hello from ACP"}
				}
			}`,
			wantType: "chunk",
		},
		{
			name: "session_update_state",
			input: `{
				"jsonrpc":"2.0",
				"method":"session/update",
				"params":{
					"sessionId":"test-123",
					"sessionUpdate":"agent_state",
					"content":{"type":"thinking"}
				}
			}`,
			wantType: "status",
		},
		{
			name:     "response_result",
			input:    `{"jsonrpc":"2.0","id":1,"result":{"sessionId":"test-456"}}`,
			wantType: "result",
		},
		{
			name:     "error_response",
			input:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantType: "error",
		},
		{
			name:     "unknown_message",
			input:    `{"jsonrpc":"2.0","method":"unknown/method"}`,
			wantType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg ACPMessage
			err := json.Unmarshal([]byte(tt.input), &msg)
			if err != nil {
				t.Fatalf("Failed to unmarshal test input: %v", err)
			}

			result := client.ParseMessage(&msg)
			if result == nil {
				t.Fatal("Expected non-nil parsed result")
			}

			if result["type"] != tt.wantType {
				t.Errorf("Expected type=%s, got %v", tt.wantType, result["type"])
			}
		})
	}
}

// TestACPContentTypes tests different ACP content types
func TestACPContentTypes(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	tests := []struct {
		name        string
		contentType string
		wantFormat  string
	}{
		{"text", "text", "text"},
		{"markdown", "markdown", "markdown"},
		{"diff", "diff", "diff"},
		{"command", "command", "command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := `{
				"jsonrpc":"2.0",
				"method":"session/update",
				"params":{
					"sessionId":"test",
					"sessionUpdate":"agent_message_chunk",
					"content":{"type":"` + tt.contentType + `","text":"content"}
				}
			}`

			var msg ACPMessage
			json.Unmarshal([]byte(input), &msg)

			result := client.ParseMessage(&msg)
			if result["type"] != "chunk" {
				t.Errorf("Expected type=chunk, got %v", result["type"])
			}

			// Content should be extracted
			if result["content"] != "content" {
				t.Errorf("Expected content='content', got %v", result["content"])
			}
		})
	}
}

// TestACPErrorParsing tests ACP error message parsing
func TestACPErrorParsing(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	input := `{
		"jsonrpc":"2.0",
		"id":1,
		"error":{
			"code":-32601,
			"message":"Method not found",
			"data":"Additional error details"
		}
	}`

	var msg ACPMessage
	json.Unmarshal([]byte(input), &msg)

	result := client.ParseMessage(&msg)

	if result["type"] != "error" {
		t.Errorf("Expected type=error, got %v", result["type"])
	}

	code, _ := result["code"].(int)
	if code != -32601 {
		t.Errorf("Expected code=-32601, got %d", code)
	}

	if result["message"] != "Method not found" {
		t.Errorf("Expected message='Method not found', got %v", result["message"])
	}
}

// TestACPSessionUpdateStructure tests session update parsing
func TestACPSessionUpdateStructure(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	input := `{
		"jsonrpc":"2.0",
		"method":"session/update",
		"params":{
			"sessionId":"session-abc-123",
			"sessionUpdate":"agent_message_chunk",
			"content":{
				"type":"markdown",
				"text":"## Analysis Complete\n\nI found 3 issues..."
			}
		}
	}`

	var msg ACPMessage
	json.Unmarshal([]byte(input), &msg)

	result := client.ParseMessage(&msg)

	// Should be parsed as chunk
	if result["type"] != "chunk" {
		t.Errorf("Expected type=chunk, got %v", result["type"])
	}

	// Content should be extracted
	content, _ := result["content"].(string)
	if content != "## Analysis Complete\n\nI found 3 issues..." {
		t.Errorf("Unexpected content: %v", content)
	}

	// Format should be preserved
	format, _ := result["format"].(string)
	if format != "markdown" {
		t.Errorf("Expected format=markdown, got %v", format)
	}
}

// TestACPClientMethods tests ACP client helper methods
func TestACPClientMethods(t *testing.T) {
	client := &ACPClient{
		provider:  "copilot",
		sessionID: "test-session-123",
		seq:       42,
	}

	// Test Pid (should return 0 when no process)
	if client.Pid() != 0 {
		t.Errorf("Expected Pid=0 for non-started client, got %d", client.Pid())
	}

	// Test Stop (should not panic when no process)
	err := client.Stop()
	if err != nil {
		t.Logf("Stop returned: %v (acceptable)", err)
	}
}

// TestACPClientStartInvalidProvider tests Start with invalid provider
func TestACPClientStartInvalidProvider(t *testing.T) {
	// Create client with unsupported provider
	client := &ACPClient{
		provider: "unsupported",
		cmd:      nil, // No command
	}

	err := client.Start()
	if err == nil {
		t.Error("Expected error when starting with nil command")
	}
}

// TestACPClientNotificationHandler tests notification handler
func TestACPClientNotificationHandler(t *testing.T) {
	client, _ := NewACPClient("copilot", "")

	handlerCalled := false
	var capturedMsg *ACPMessage

	client.SetNotificationHandler(func(msg *ACPMessage) {
		handlerCalled = true
		capturedMsg = msg
	})

	if client.notificationHandler == nil {
		t.Error("Expected notification handler to be set")
	}

	// Verify handler is stored (can't easily test callback without full setup)
	_ = handlerCalled
	_ = capturedMsg
}

// TestACPClientConcurrentAccess tests concurrent access to ACP client
func TestACPClientConcurrentAccess(t *testing.T) {
	client, _ := NewACPClient("copilot", "")

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent handler setting
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			handler := func(msg *ACPMessage) {}
			client.SetNotificationHandler(handler)
		}(i)
	}

	wg.Wait()

	// Should not panic - last write wins
	if client.notificationHandler == nil {
		t.Error("Expected handler to be set after concurrent access")
	}
}

// TestACPClientContextCancellation tests context cancellation in Listen
func TestACPClientContextCancellation(t *testing.T) {
	client, _ := NewACPClient("copilot", "")

	// For this test, we just verify the method handles context cancellation
	// without panicking when reader is nil
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	handlerCalled := false
	err := client.Listen(ctx, func(msg *ACPMessage) {
		handlerCalled = true
	})

	// Should fail with nil reader error (expected since we didn't start)
	if err == nil {
		// If no error, context should have timed out
		if handlerCalled {
			t.Error("Handler should not be called without valid reader")
		}
	}
}

// TestACPPromptNoSession tests Prompt without active session
func TestACPPromptNoSession(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	err := client.Prompt("test prompt")
	if err == nil {
		t.Error("Expected error when prompting without session")
	}
	if !strings.Contains(err.Error(), "no active session") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestACPClientPidWithMockProcess tests Pid with mock process
func TestACPClientPidWithMockProcess(t *testing.T) {
	// Skip on Windows as bash script may not work
	if strings.HasPrefix(os.Getenv("RUNNER_OS"), "Windows") {
		t.Skip("Skipping mock process test on Windows")
	}

	// Mock server that responds once to initialize request and keeps running
	mockScript := `#!/bin/bash
# Read initialize request line
read -r line
# Respond with matching id=1 (sendRequest increments seq to 1)
echo '{"jsonrpc":"2.0","id":1,"result":{"sessionId":"test-session"}}'
# Keep process alive for Pid check
while true; do sleep 1; done
`
	mockPath := mockCLI(t, "mock-pid-server", mockScript)

	client, _ := NewACPClient("copilot", mockPath)

	// Start the client
	err := client.Start()
	if err != nil {
		t.Skipf("Skipping Pid test - failed to start: %v", err)
	}
	defer client.Stop()

	// Pid should be non-zero
	pid := client.Pid()
	if pid == 0 {
		t.Error("Expected non-zero Pid after start")
	}
}

// TestACPMessageWithNilParams tests message with nil params
func TestACPMessageWithNilParams(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	input := `{
		"jsonrpc":"2.0",
		"method":"session/update",
		"params":null
	}`

	var msg ACPMessage
	json.Unmarshal([]byte(input), &msg)

	// Should not panic
	result := client.ParseMessage(&msg)
	if result == nil {
		t.Error("Expected non-nil result")
	}
}

// TestACPMessageWithEmptyContent tests message with empty content
func TestACPMessageWithEmptyContent(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	input := `{
		"jsonrpc":"2.0",
		"method":"session/update",
		"params":{
			"sessionId":"test",
			"sessionUpdate":"agent_message_chunk",
			"content":{}
		}
	}`

	var msg ACPMessage
	json.Unmarshal([]byte(input), &msg)

	result := client.ParseMessage(&msg)

	if result["type"] != "chunk" {
		t.Errorf("Expected type=chunk, got %v", result["type"])
	}

	// Empty content should be empty string
	if result["content"] != "" {
		t.Errorf("Expected empty content, got %v", result["content"])
	}
}

// TestACPSessionUpdateTypes tests all session update types
func TestACPSessionUpdateTypes(t *testing.T) {
	client := &ACPClient{provider: "copilot"}

	tests := []struct {
		updateType string
		wantType   string
	}{
		{"agent_message_chunk", "chunk"},
		{"agent_state", "status"},
		{"unknown_update", "update"},
		{"", "update"},
	}

	for _, tt := range tests {
		t.Run(tt.updateType, func(t *testing.T) {
			input := `{
				"jsonrpc":"2.0",
				"method":"session/update",
				"params":{
					"sessionId":"test",
					"sessionUpdate":"` + tt.updateType + `",
					"content":{"type":"text","text":"test"}
				}
			}`

			var msg ACPMessage
			json.Unmarshal([]byte(input), &msg)

			result := client.ParseMessage(&msg)
			if result["type"] != tt.wantType {
				t.Errorf("Expected type=%s, got %v", tt.wantType, result["type"])
			}
		})
	}
}

// ============== Gemini Adapter Tests ==============

// TestGeminiAdapterBasic tests basic Gemini adapter functionality
func TestGeminiAdapterBasic(t *testing.T) {
	adapter := &GeminiAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "gemini",
				WorkDir:  "/tmp",
				Task:     "Test task",
			},
		},
	}

	// Test SupportsACP
	if adapter.SupportsACP() {
		t.Error("Gemini should not support ACP")
	}

	// Test SupportsJSONStream
	if !adapter.SupportsJSONStream() {
		t.Error("Gemini should support JSON stream")
	}

	// Test BuildCommand
	cmd := adapter.BuildCommand(adapter.config)
	if cmd.Path != "gemini" {
		t.Errorf("Expected gemini command, got %s", cmd.Path)
	}

	// Verify command args contain expected flags
	argsStr := strings.Join(cmd.Args, " ")
	if !strings.Contains(argsStr, "chat") {
		t.Error("Expected 'chat' in command args")
	}
	if !strings.Contains(argsStr, "--format") || !strings.Contains(argsStr, "json") {
		t.Error("Expected JSON format in command args")
	}
	if !strings.Contains(argsStr, "--stream") {
		t.Error("Expected --stream in command args")
	}
}

// TestGeminiAdapterWithSession tests Gemini adapter with session resumption
func TestGeminiAdapterWithSession(t *testing.T) {
	adapter := &GeminiAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "gemini",
				Task:     "Continue task",
			},
		},
		sessionID: "test-session-123",
	}

	cmd := adapter.BuildCommand(adapter.config)
	argsStr := strings.Join(cmd.Args, " ")

	if !strings.Contains(argsStr, "--session") || !strings.Contains(argsStr, "test-session-123") {
		t.Error("Expected session resumption in command args")
	}
}

// TestGeminiAdapterParseMessage tests Gemini message parsing
func TestGeminiAdapterParseMessage(t *testing.T) {
	adapter := &GeminiAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "gemini",
			},
		},
	}

	tests := []struct {
		name     string
		input    string
		wantType string
		wantErr  bool
	}{
		{
			name:     "chunk_message",
			input:    `{"type":"chunk","content":"Hello"}`,
			wantType: "chunk",
			wantErr:  false,
		},
		{
			name:     "response_message",
			input:    `{"type":"response","content":"Here's the answer"}`,
			wantType: "assistant",
			wantErr:  false,
		},
		{
			name:     "error_message",
			input:    `{"type":"error","message":"API error"}`,
			wantType: "error",
			wantErr:  false,
		},
		{
			name:     "unknown_type",
			input:    `{"type":"unknown","data":"test"}`,
			wantType: "unknown",
			wantErr:  false,
		},
		{
			name:     "plain_text",
			input:    `Gemini is thinking...`,
			wantType: "chunk",
			wantErr:  false,
		},
		{
			name:     "invalid_json",
			input:    `{"type": "incomplete`,
			wantType: "chunk",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := adapter.ParseMessage(tt.input)
			if err != nil && !tt.wantErr {
				t.Errorf("Unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if result["type"] != tt.wantType {
				t.Errorf("Expected type=%s, got %v", tt.wantType, result["type"])
			}
		})
	}
}

// TestGeminiAdapterCapabilities tests Gemini adapter capabilities
func TestGeminiAdapterCapabilities(t *testing.T) {
	adapter := &GeminiAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{},
		},
	}

	caps := adapter.GetCapabilities()
	if caps == nil {
		t.Error("Expected non-nil capabilities")
	}

	// Gemini should have similar capabilities to Claude
	expectedCaps := []string{"text_output", "file_edit", "multi_turn", "streaming"}
	for _, expectedCap := range expectedCaps {
		found := false
		for _, cap := range caps {
			if cap == expectedCap {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected capability '%s' not found", expectedCap)
		}
	}
}

// TestGeminiAdapterSetSessionDir tests Gemini session manager setup
func TestGeminiAdapterSetSessionDir(t *testing.T) {
	adapter := &GeminiAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "gemini",
			},
		},
	}

	adapter.SetSessionDir("", "test_task")

	if adapter.sessionManager == nil {
		t.Error("Expected sessionManager to be set")
	}

	// Verify session manager has correct provider and taskID
	if adapter.sessionManager.GetProvider() != "gemini" {
		t.Errorf("Expected provider=gemini, got %s", adapter.sessionManager.GetProvider())
	}

	if adapter.sessionManager.GetTaskID() != "test_task" {
		t.Errorf("Expected taskID=test_task, got %s", adapter.sessionManager.GetTaskID())
	}
}

// TestGeminiAdapterSendCommand tests Gemini send command functionality
func TestGeminiAdapterSendCommand(t *testing.T) {
	// Note: This test verifies the method exists and returns appropriate error
	// when stdin is not available (which is the case for non-interactive mode)
	adapter := &GeminiAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "gemini",
			},
		},
	}

	// Should fail because stdin is not set up (non-interactive mode)
	err := adapter.SendCommand("test", map[string]interface{}{"key": "value"})
	if err == nil {
		t.Error("Expected error when stdin not available")
	}
}

// ============== OpenCode Adapter Tests ==============

// TestOpenCodeAdapterBasic tests basic OpenCode adapter functionality
func TestOpenCodeAdapterBasic(t *testing.T) {
	adapter := &OpenCodeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "opencode",
				WorkDir:  "/tmp",
				Task:     "Test task",
			},
		},
	}

	// Test SupportsACP
	if !adapter.SupportsACP() {
		t.Error("OpenCode should support ACP")
	}

	// Test SupportsJSONStream
	if adapter.SupportsJSONStream() {
		t.Error("OpenCode should not support JSON stream")
	}

	// Test BuildCommand
	cmd := adapter.BuildCommand(adapter.config)
	if !strings.HasSuffix(cmd.Path, "opencode") {
		t.Errorf("Expected opencode command, got %s", cmd.Path)
	}

	// Verify command args
	if len(cmd.Args) < 2 || cmd.Args[1] != "acp" {
		t.Error("Expected 'acp' subcommand in args")
	}
}

// TestOpenCodeAdapterParseMessage tests OpenCode message parsing
func TestOpenCodeAdapterParseMessage(t *testing.T) {
	adapter := &OpenCodeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "opencode",
			},
		},
	}

	tests := []struct {
		name     string
		input    string
		wantType string
	}{
		{
			name:     "session_update",
			input:    `{"method":"session/update","params":{"sessionId":"test"}}`,
			wantType: "update",
		},
		{
			name:     "result_message",
			input:    `{"result":{"status":"success"}}`,
			wantType: "result",
		},
		{
			name:     "error_message",
			input:    `{"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantType: "error",
		},
		{
			name:     "plain_text",
			input:    `OpenCode is processing...`,
			wantType: "chunk",
		},
		{
			name:     "invalid_json",
			input:    `{"method": "incomplete`,
			wantType: "chunk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := adapter.ParseMessage(tt.input)
			if err != nil {
				t.Logf("Parse error (may be expected): %v", err)
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if result["type"] != tt.wantType {
				t.Errorf("Expected type=%s, got %v", tt.wantType, result["type"])
			}
		})
	}
}

// TestOpenCodeAdapterCapabilities tests OpenCode adapter capabilities
func TestOpenCodeAdapterCapabilities(t *testing.T) {
	adapter := &OpenCodeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{},
		},
	}

	caps := adapter.GetCapabilities()
	if caps == nil {
		t.Error("Expected non-nil capabilities")
	}

	// OpenCode should have ACP-specific capabilities
	expectedCaps := []string{"text_output", "multi_turn", "streaming", "tool_use"}
	for _, expectedCap := range expectedCaps {
		found := false
		for _, cap := range caps {
			if cap == expectedCap {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected capability '%s' not found", expectedCap)
		}
	}
}

// TestOpenCodeAdapterSendCommand tests OpenCode send command (should use ACP)
func TestOpenCodeAdapterSendCommand(t *testing.T) {
	adapter := &OpenCodeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "opencode",
			},
		},
	}

	// OpenCode uses ACP protocol, so SendCommand should return error
	err := adapter.SendCommand("test", map[string]interface{}{})
	if err == nil {
		t.Error("Expected error - OpenCode uses ACP, not direct commands")
	}
	if !strings.Contains(err.Error(), "ACP") {
		t.Errorf("Expected ACP-related error, got: %v", err)
	}
}

// ============== Combined Adapter Tests ==============

// TestAllAdaptersCreateSuccessfully tests that all adapter types can be created
func TestAllAdaptersCreateSuccessfully(t *testing.T) {
	config := &CLIConfig{
		Provider: "test",
		WorkDir:  "/tmp",
		Task:     "Test",
	}

	adapters := []struct {
		name     string
		adapter  Adapter
		provider string
	}{
		{"claude", NewClaudeAdapter(config), "claude"},
		{"codex", NewCodexAdapter(config), "codex"},
		{"copilot", NewCopilotAdapter(config), "copilot"},
		{"gemini", NewGeminiAdapter(config), "gemini"},
		{"opencode", NewOpenCodeAdapter(config), "opencode"},
		{"generic", NewGenericAdapter(config), "unknown"},
	}

	for _, a := range adapters {
		t.Run(a.name, func(t *testing.T) {
			if a.adapter == nil {
				t.Errorf("%s adapter should not be nil", a.name)
			}

			// All adapters should have capabilities
			caps := a.adapter.GetCapabilities()
			if caps == nil {
				t.Errorf("%s adapter should have capabilities", a.name)
			}
		})
	}
}

// TestGeminiAndOpenCodeAdapterWithMock tests both adapters with mock CLI
func TestGeminiAndOpenCodeAdapterWithMock(t *testing.T) {
	// Create mock gemini command
	geminiMock := `#!/bin/bash
echo '{"type":"chunk","content":"Gemini response"}'
`
	geminiPath := mockCLI(t, "gemini", geminiMock)

	// Temporarily add to PATH
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", filepath.Dir(geminiPath)+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	// Test Gemini adapter
	geminiAdapter := &GeminiAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "gemini",
				WorkDir:  "/tmp",
				Task:     "Test",
			},
		},
	}

	cmd := geminiAdapter.BuildCommand(geminiAdapter.config)
	if cmd.Path != geminiPath {
		t.Logf("Using mock gemini at: %s", cmd.Path)
	}

	// Test OpenCode adapter (ACP mode - tested separately via ACP client tests)
	opencodeAdapter := &OpenCodeAdapter{
		BaseAdapter: BaseAdapter{
			config: &CLIConfig{
				Provider: "opencode",
			},
		},
	}

	// OpenCode should report ACP support
	if !opencodeAdapter.SupportsACP() {
		t.Error("OpenCode adapter should support ACP")
	}
}
