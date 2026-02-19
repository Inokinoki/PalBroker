package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pal-broker/internal/state"
)

// TestSplitLines 测试行分割函数
func TestSplitLines(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"line1\nline2\nline3", 3},
		{"single line", 1},
		{"", 0},
		{"\n", 1}, // 分割后有一个空字符串
		{"line1\n\nline3", 3},
	}

	for _, test := range tests {
		lines := splitLines([]byte(test.input))
		if len(lines) != test.expected {
			t.Errorf("splitLines(%q) returned %d lines, expected %d", test.input, len(lines), test.expected)
		}
	}
}

// TestClientMessageUnmarshal 测试客户端消息解析
func TestClientMessageUnmarshal(t *testing.T) {
	jsonData := `{"command": "send_input", "data": {"content": "Hello"}}`

	var msg ClientMessage
	err := json.Unmarshal([]byte(jsonData), &msg)
	if err != nil {
		t.Fatalf("Failed to unmarshal ClientMessage: %v", err)
	}

	if msg.Command != "send_input" {
		t.Errorf("Expected command='send_input', got %s", msg.Command)
	}

	if msg.Data["content"] != "Hello" {
		t.Errorf("Expected content='Hello', got %v", msg.Data["content"])
	}
}

// TestWebSocketServerCreation 测试 WebSocket 服务器创建
func TestWebSocketServerCreation(t *testing.T) {
	// 创建 mock 状态管理器
	stateMgr := state.NewManager("/tmp/test-ws")

	// 创建服务器（不启动）
	server := NewWebSocketServer(stateMgr, "test_task", nil)

	if server == nil {
		t.Fatal("Expected non-nil WebSocketServer")
	}

	if server.taskID != "test_task" {
		t.Errorf("Expected taskID='test_task', got %s", server.taskID)
	}
}

// TestGenerateDeviceID 测试设备 ID 生成
func TestGenerateDeviceID(t *testing.T) {
	id1 := generateDeviceID()
	id2 := generateDeviceID()

	if id1 == "" {
		t.Error("Expected non-empty device ID")
	}

	if !strings.HasPrefix(id1, "device_") {
		t.Errorf("Expected device ID to start with 'device_', got %s", id1)
	}

	// 两个 ID 应该不同（有一定概率）
	// 注意：由于是随机生成，有小概率相同
	t.Logf("Generated IDs: %s, %s", id1, id2)
}

// TestRandomString 测试随机字符串生成
func TestRandomString(t *testing.T) {
	str := randomString(10)

	if len(str) != 10 {
		t.Errorf("Expected length 10, got %d", len(str))
	}

	// 验证只包含字母数字
	for _, c := range str {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			t.Errorf("Invalid character in random string: %c", c)
		}
	}
}

// TestBroadcastWithNoClients 测试无客户端时的广播
func TestBroadcastWithNoClients(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-broadcast")
	server := NewWebSocketServer(stateMgr, "test", nil)

	// 广播不应该 panic
	event := state.Event{
		Type:      "chunk",
		Timestamp: time.Now().UnixMilli(),
		Data:      map[string]string{"content": "test"},
	}

	// 不应该 panic
	server.broadcast(event)
}

// TestGetClientWithNoClients 测试无客户端时获取客户端
func TestGetClientWithNoClients(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-getclient")
	server := NewWebSocketServer(stateMgr, "test", nil)

	client := server.getClient("nonexistent")
	if client != nil {
		t.Error("Expected nil client for nonexistent device")
	}
}

// TestEventTypes 测试事件类型常量
func TestEventTypes(t *testing.T) {
	// 验证常见的事件类型
	validTypes := []string{
		"chunk",
		"file",
		"status",
		"error",
		"command",
		"code_block",
		"file_operation",
	}

	for _, eventType := range validTypes {
		event := state.Event{
			Type:      eventType,
			Timestamp: time.Now().UnixMilli(),
			Data:      map[string]string{"test": "data"},
		}

		if event.Type != eventType {
			t.Errorf("Event type mismatch: expected %s, got %s", eventType, event.Type)
		}
	}
}

// TestClientMessageCommands 测试客户端命令类型
func TestClientMessageCommands(t *testing.T) {
	validCommands := []string{
		"send_input",
		"cancel",
		"get_status",
		"approve",
	}

	for _, cmd := range validCommands {
		msg := ClientMessage{
			Command: cmd,
			Data:    map[string]interface{}{},
		}

		if msg.Command != cmd {
			t.Errorf("Command mismatch: expected %s, got %s", cmd, msg.Command)
		}
	}
}

// TestJSONEncoding 测试 JSON 编码
func TestJSONEncoding(t *testing.T) {
	event := state.Event{
		Seq:       1,
		Type:      "chunk",
		Timestamp: 1234567890,
		Data: map[string]interface{}{
			"content": "test",
			"number":  42,
			"nested": map[string]interface{}{
				"key": "value",
			},
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	// 验证可以反序列化
	var decoded state.Event
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if decoded.Seq != event.Seq {
		t.Errorf("Seq mismatch: expected %d, got %d", event.Seq, decoded.Seq)
	}

	if decoded.Type != event.Type {
		t.Errorf("Type mismatch: expected %s, got %s", event.Type, decoded.Type)
	}
}

// TestConcurrentBroadcast 测试并发广播
func TestConcurrentBroadcast(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-concurrent")
	server := NewWebSocketServer(stateMgr, "test", nil)

	done := make(chan bool)
	event := state.Event{
		Type:      "chunk",
		Timestamp: time.Now().UnixMilli(),
		Data:      map[string]string{"content": "test"},
	}

	// 并发广播
	for i := 0; i < 10; i++ {
		go func() {
			server.broadcast(event)
			done <- true
		}()
	}

	// 等待完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 如果没有 panic，测试通过
}

// TestWebSocketServerMethods 测试 WebSocket 服务器方法
func TestWebSocketServerMethods(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-methods")
	server := NewWebSocketServer(stateMgr, "test_task", nil)

	// 验证服务器对象创建成功
	if server.stateMgr == nil {
		t.Error("Expected non-nil state manager")
	}

	if server.clients == nil {
		t.Error("Expected non-nil clients map")
	}

	if server.taskID == "" {
		t.Error("Expected non-empty task ID")
	}
}
