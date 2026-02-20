package server

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"pal-broker/internal/state"
)

// TestWebSocketClientMethods 测试 WebSocketClient 方法
func TestWebSocketClientMethods(t *testing.T) {
	client := &WebSocketClient{
		DeviceID:    "test_device",
		ConnectedAt: time.Now(),
		LastActive:  time.Now(),
	}

	// 测试 IsClosed
	if client.IsClosed() {
		t.Error("Expected client to not be closed initially")
	}

	// 测试 SetClosed
	client.SetClosed()
	if !client.IsClosed() {
		t.Error("Expected client to be closed after SetClosed")
	}

	// 测试 UpdateActivity
	client2 := &WebSocketClient{
		DeviceID:   "test_device_2",
		LastActive: time.Now().Add(-time.Hour),
	}
	client2.UpdateActivity()
	if time.Since(client2.LastActive) > time.Second {
		t.Error("Expected LastActive to be updated")
	}

	// 测试 IncrementReconnect
	client3 := &WebSocketClient{}
	client3.IncrementReconnect()
	client3.IncrementReconnect()
	if client3.GetReconnectCount() != 2 {
		t.Errorf("Expected reconnect count 2, got %d", client3.GetReconnectCount())
	}
}

// TestWebSocketServerCreation 测试服务器创建
func TestWebSocketServerCreation(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-ws-creation")
	server := NewWebSocketServer(stateMgr, "test_task", nil)

	if server == nil {
		t.Fatal("Expected non-nil server")
	}

	if server.taskID != "test_task" {
		t.Errorf("Expected taskID='test_task', got %s", server.taskID)
	}

	if server.clients == nil {
		t.Error("Expected clients map to be initialized")
	}

	if server.broadcastCh == nil {
		t.Error("Expected broadcastCh to be initialized")
	}

	if server.errorCh == nil {
		t.Error("Expected errorCh to be initialized")
	}
}

// TestServerStartAndStop 测试服务器启动和停止
func TestServerStartAndStop(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-ws-startstop")
	server := NewWebSocketServer(stateMgr, "test_task", nil)

	// 启动服务器
	port, err := server.Start(":0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if port == 0 {
		t.Error("Expected non-zero port")
	}

	t.Logf("Server started on port %d", port)

	// 等待一下
	time.Sleep(100 * time.Millisecond)

	// 停止服务器
	err = server.Stop()
	if err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}

	t.Log("Server stopped successfully")
}

// TestBroadcastChannel 测试广播通道
func TestBroadcastChannel(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-broadcast-channel")
	server := NewWebSocketServer(stateMgr, "test", nil)

	// 启动广播处理器
	server.wg.Add(1)
	go server.broadcastHandler()

	// 发送事件到广播通道
	event := state.Event{
		Type:      "chunk",
		Timestamp: time.Now().UnixMilli(),
		Data:      map[string]string{"content": "test"},
	}

	// 不应该阻塞
	select {
	case server.broadcastCh <- event:
		t.Log("Event sent to broadcast channel")
	case <-time.After(time.Second):
		t.Error("Broadcast channel is blocked")
	}

	// 清理
	close(server.broadcastCh)
	server.wg.Wait()
}

// TestErrorHandler 测试错误处理
func TestErrorHandler(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-error-handler")
	server := NewWebSocketServer(stateMgr, "test", nil)

	// 启动错误处理器
	server.wg.Add(1)
	go server.errorHandler()

	// 发送错误
	testErr := "test error"
	select {
	case server.errorCh <- &testError{testErr}:
		t.Log("Error sent to error handler")
	case <-time.After(time.Second):
		t.Error("Error channel is blocked")
	}

	// 清理
	close(server.errorCh)
	server.wg.Wait()
}

// testError 实现 error 接口
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestAddRemoveClient 测试添加和移除客户端
func TestAddRemoveClient(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-client-mgmt")
	server := NewWebSocketServer(stateMgr, "test", nil)

	client := &WebSocketClient{
		DeviceID:    "test_device",
		ConnectedAt: time.Now(),
		LastActive:  time.Now(),
	}

	// 添加客户端
	server.addClient("test_device", client)

	if server.GetClientCount() != 1 {
		t.Errorf("Expected 1 client, got %d", server.GetClientCount())
	}

	// 获取客户端信息
	info, err := server.GetClientInfo("test_device")
	if err != nil {
		t.Fatalf("Failed to get client info: %v", err)
	}

	if info.DeviceID != "test_device" {
		t.Errorf("Expected device_id='test_device', got %s", info.DeviceID)
	}

	// 移除客户端
	server.removeClient("test_device")

	if server.GetClientCount() != 0 {
		t.Errorf("Expected 0 clients after removal, got %d", server.GetClientCount())
	}
}

// TestConcurrentClientAccess 测试并发客户端访问
func TestConcurrentClientAccess(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-concurrent-access")
	server := NewWebSocketServer(stateMgr, "test", nil)

	var wg sync.WaitGroup
	numGoroutines := 10

	// 并发添加客户端
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := &WebSocketClient{
				DeviceID:    "device_" + string(rune('A'+id)),
				ConnectedAt: time.Now(),
				LastActive:  time.Now(),
			}
			server.addClient("device_"+string(rune('A'+id)), client)
		}(i)
	}

	wg.Wait()

	if server.GetClientCount() != numGoroutines {
		t.Errorf("Expected %d clients, got %d", numGoroutines, server.GetClientCount())
	}

	// 并发移除客户端
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			server.removeClient("device_" + string(rune('A'+id)))
		}(i)
	}

	wg.Wait()

	if server.GetClientCount() != 0 {
		t.Errorf("Expected 0 clients after concurrent removal, got %d", server.GetClientCount())
	}
}

// TestBroadcastToClients 测试广播到客户端
func TestBroadcastToClients(t *testing.T) {
	// 这个测试需要真实的 WebSocket 连接，这里只测试逻辑
	stateMgr := state.NewManager("/tmp/test-broadcast")
	server := NewWebSocketServer(stateMgr, "test", nil)

	event := state.Event{
		Type:      "chunk",
		Timestamp: time.Now().UnixMilli(),
		Data:      map[string]string{"content": "test"},
	}

	// 没有客户端时不应该 panic
	server.broadcastToClients(event)
	t.Log("Broadcast to empty clients succeeded")
}

// TestSplitLines 测试行分割
func TestSplitLines(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"line1\nline2\nline3", 3},
		{"single line", 1},
		{"", 0},
		{"\n", 1},
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

// TestServerEvent 测试服务器事件
func TestServerEvent(t *testing.T) {
	event := ServerEvent{
		Event: state.Event{
			Type: "chunk",
			Data: map[string]string{"content": "test"},
		},
		DeviceID: "test_device",
	}

	if event.DeviceID != "test_device" {
		t.Errorf("Expected device_id='test_device', got %s", event.DeviceID)
	}
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
	t.Logf("Generated IDs: %s, %s", id1, id2)
}

// TestHealthEndpoint 测试健康检查端点
func TestHealthEndpoint(t *testing.T) {
	t.Skip("Skipping: HTTP handler registration conflict in tests")

	stateMgr := state.NewManager("/tmp/test-health")
	server := NewWebSocketServer(stateMgr, "test_task", nil)

	// 启动服务器
	port, err := server.Start(":0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 可以通过 HTTP 测试健康端点
	// 这里只测试逻辑
	t.Logf("Health endpoint available at http://localhost:%d/health", port)
}

// TestClientReconnect 测试客户端重连逻辑
func TestClientReconnect(t *testing.T) {
	client := &WebSocketClient{
		DeviceID:       "test_device",
		ReconnectCount: 0,
	}

	// 模拟多次重连
	for i := 0; i < MaxReconnectAttempts; i++ {
		client.IncrementReconnect()
		t.Logf("Reconnect attempt %d/%d", client.GetReconnectCount(), MaxReconnectAttempts)
	}

	if client.GetReconnectCount() != MaxReconnectAttempts {
		t.Errorf("Expected reconnect count %d, got %d", MaxReconnectAttempts, client.GetReconnectCount())
	}
}

// TestReconnectDelay 测试重连延迟
func TestReconnectDelay(t *testing.T) {
	// 测试指数退避
	delays := []time.Duration{}
	for i := 0; i < MaxReconnectAttempts; i++ {
		delay := time.Duration(1<<uint(i)) * ReconnectDelayBase
		if delay > ReconnectDelayMax {
			delay = ReconnectDelayMax
		}
		delays = append(delays, delay)
		t.Logf("Attempt %d: delay=%v", i+1, delay)
	}

	// 验证延迟递增
	for i := 1; i < len(delays)-1; i++ {
		if delays[i] <= delays[i-1] {
			t.Errorf("Expected increasing delays: %v <= %v", delays[i], delays[i-1])
		}
	}

	// 验证不超过最大值
	if delays[len(delays)-1] > ReconnectDelayMax {
		t.Errorf("Max delay exceeded: %v > %v", delays[len(delays)-1], ReconnectDelayMax)
	}
}

// TestErrorDefinitions 测试错误定义
func TestErrorDefinitions(t *testing.T) {
	errors := []error{
		ErrClientNotFound,
		ErrConnectionClosed,
		ErrWriteFailed,
		ErrReconnectFailed,
		ErrMaxAttemptsReached,
	}

	for _, err := range errors {
		if err == nil {
			t.Error("Expected non-nil error")
		}
		t.Logf("Error: %v", err)
	}
}

// TestServerConfig 测试服务器配置
func TestServerConfig(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-config")
	server := NewWebSocketServer(stateMgr, "test", nil)

	// 验证默认配置
	if !server.config.EnableCompression {
		t.Error("Expected EnableCompression to be true")
	}

	if !server.config.EnableHeartbeat {
		t.Error("Expected EnableHeartbeat to be true")
	}

	if !server.config.ReconnectEnabled {
		t.Error("Expected ReconnectEnabled to be true")
	}
}

// TestConcurrentBroadcast 测试并发广播
func TestConcurrentBroadcast(t *testing.T) {
	stateMgr := state.NewManager("/tmp/test-concurrent-broadcast")
	server := NewWebSocketServer(stateMgr, "test", nil)

	// 启动广播处理器
	server.wg.Add(1)
	go server.broadcastHandler()

	event := state.Event{
		Type:      "chunk",
		Timestamp: time.Now().UnixMilli(),
		Data:      map[string]string{"content": "test"},
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	// 并发发送广播
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.broadcast(event)
		}()
	}

	wg.Wait()

	// 清理
	close(server.broadcastCh)
	server.wg.Wait()
}

// TestWebSocketConstants 测试 WebSocket 常量
func TestWebSocketConstants(t *testing.T) {
	// 验证常量定义合理
	if MaxReconnectAttempts <= 0 {
		t.Error("MaxReconnectAttempts should be positive")
	}

	if ReconnectDelayBase <= 0 {
		t.Error("ReconnectDelayBase should be positive")
	}

	if ReconnectDelayMax < ReconnectDelayBase {
		t.Error("ReconnectDelayMax should be >= ReconnectDelayBase")
	}

	if PingInterval <= 0 {
		t.Error("PingInterval should be positive")
	}

	if WriteTimeout <= 0 {
		t.Error("WriteTimeout should be positive")
	}

	t.Logf("MaxReconnectAttempts: %d", MaxReconnectAttempts)
	t.Logf("ReconnectDelayBase: %v", ReconnectDelayBase)
	t.Logf("ReconnectDelayMax: %v", ReconnectDelayMax)
	t.Logf("PingInterval: %v", PingInterval)
	t.Logf("WriteTimeout: %v", WriteTimeout)
}
