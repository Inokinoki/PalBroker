package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"pal-broker/internal/adapter"
	"pal-broker/internal/state"
)

const (
	// 重连配置
	MaxReconnectAttempts = 5
	ReconnectDelayBase   = 1 * time.Second
	ReconnectDelayMax    = 30 * time.Second

	// WebSocket 配置
	WriteTimeout      = 10 * time.Second
	PongTimeout       = 60 * time.Second
	PingInterval      = 30 * time.Second
	MaxMessageSize    = 4096
	HeartbeatInterval = 10 * time.Second
)

// 错误定义
var (
	ErrClientNotFound     = errors.New("client not found")
	ErrConnectionClosed   = errors.New("connection closed")
	ErrWriteFailed        = errors.New("write failed")
	ErrReconnectFailed    = errors.New("reconnect failed")
	ErrMaxAttemptsReached = errors.New("max reconnect attempts reached")
)

// Device 设备信息（本地副本）
type Device struct {
	DeviceID        string `json:"device_id"`
	ConnectedAt     int64  `json:"connected_at"`
	LastActive      int64  `json:"last_active"`
	LastSeq         int64  `json:"last_seq"`
	ReconnectCount  int    `json:"reconnect_count"`
	LastReconnectAt int64  `json:"last_reconnect_at"`
}

// ClientConfig WebSocket 客户端配置
type ClientConfig struct {
	EnableCompression bool
	EnableHeartbeat   bool
	ReconnectEnabled  bool
}

// WebSocketClient WebSocket 客户端（带重连支持）
type WebSocketClient struct {
	Conn            *websocket.Conn
	DeviceID        string
	ConnectedAt     time.Time
	LastActive      time.Time
	LastSeq         int64
	ReconnectCount  int
	mu              sync.RWMutex
	isClosed        bool
	reconnectCtx    context.Context
	reconnectCancel context.CancelFunc
}

// IsClosed 检查客户端是否已关闭
func (c *WebSocketClient) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isClosed
}

// SetClosed 标记客户端为关闭状态
func (c *WebSocketClient) SetClosed() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isClosed = true
	if c.reconnectCancel != nil {
		c.reconnectCancel()
	}
}

// UpdateActivity 更新活动时间
func (c *WebSocketClient) UpdateActivity() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastActive = time.Now()
}

// IncrementReconnect 增加重连计数
func (c *WebSocketClient) IncrementReconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ReconnectCount++
}

// GetReconnectCount 获取重连计数
func (c *WebSocketClient) GetReconnectCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ReconnectCount
}

var upgrader = websocket.Upgrader{
	CheckOrigin:       func(r *http.Request) bool { return true },
	EnableCompression: true,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
}

// WebSocketServer WebSocket 服务器
type WebSocketServer struct {
	stateMgr    *state.Manager
	taskID      string
	cli         *adapter.CLIProcess
	clients     map[string]*WebSocketClient
	mu          sync.RWMutex
	broadcastCh chan state.Event
	errorCh     chan error
	config      ClientConfig
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// ClientMessage 客户端消息
type ClientMessage struct {
	Command string                 `json:"command"`
	Data    map[string]interface{} `json:"data"`
}

// ServerEvent 服务器事件
type ServerEvent struct {
	state.Event
	DeviceID string `json:"device_id,omitempty"`
}

// NewWebSocketServer 创建 WebSocket 服务器
func NewWebSocketServer(stateMgr *state.Manager, taskID string, cli *adapter.CLIProcess) *WebSocketServer {
	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketServer{
		stateMgr:    stateMgr,
		taskID:      taskID,
		cli:         cli,
		clients:     make(map[string]*WebSocketClient),
		broadcastCh: make(chan state.Event, 100), // 缓冲通道
		errorCh:     make(chan error, 10),
		config: ClientConfig{
			EnableCompression: true,
			EnableHeartbeat:   true,
			ReconnectEnabled:  true,
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动服务器
func (s *WebSocketServer) Start(addr string) (int, error) {
	http.HandleFunc("/ws", s.handleWebSocket)
	http.HandleFunc("/health", s.handleHealth) // 健康检查端点

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("failed to listen: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		log.Printf("WebSocket server starting on port %d", port)
		if err := http.Serve(listener, nil); err != nil && err != http.ErrServerClosed {
			s.errorCh <- fmt.Errorf("WebSocket server error: %w", err)
		}
	}()

	// 启动广播处理器
	s.wg.Add(1)
	go s.broadcastHandler()

	// 启动错误处理器
	s.wg.Add(1)
	go s.errorHandler()

	// 启动心跳检查
	if s.config.EnableHeartbeat {
		s.wg.Add(1)
		go s.heartbeatChecker()
	}

	return port, nil
}

// Stop 停止服务器
func (s *WebSocketServer) Stop() error {
	log.Println("Stopping WebSocket server...")

	// 取消上下文
	s.cancel()

	// 关闭所有客户端连接
	s.mu.Lock()
	for deviceID, client := range s.clients {
		client.SetClosed()
		if client.Conn != nil {
			client.Conn.Close()
		}
		log.Printf("Disconnected client: %s", deviceID)
		delete(s.clients, deviceID)
	}
	s.mu.Unlock()

	// 关闭通道
	close(s.broadcastCh)
	close(s.errorCh)

	// 等待所有 goroutine 完成
	s.wg.Wait()

	log.Println("WebSocket server stopped")
	return nil
}

// broadcastHandler 处理广播
func (s *WebSocketServer) broadcastHandler() {
	defer s.wg.Done()

	for event := range s.broadcastCh {
		s.broadcastToClients(event)
	}
}

// errorHandler 处理错误
func (s *WebSocketServer) errorHandler() {
	defer s.wg.Done()

	for err := range s.errorCh {
		if err != nil {
			log.Printf("Server error: %v", err)

			// 记录到状态管理器
			s.stateMgr.AddOutput(s.taskID, state.Event{
				Type:      "error",
				Timestamp: time.Now().UnixMilli(),
				Data: map[string]interface{}{
					"message": err.Error(),
				},
			})
		}
	}
}

// heartbeatChecker 心跳检查
func (s *WebSocketServer) heartbeatChecker() {
	defer s.wg.Done()

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkHeartbeat()
		}
	}
}

// checkHeartbeat 检查客户端心跳
func (s *WebSocketServer) checkHeartbeat() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for deviceID, client := range s.clients {
		client.mu.RLock()
		lastActive := client.LastActive
		client.mu.RUnlock()

		// 如果超过 2 倍心跳间隔没有活动，断开连接
		if now.Sub(lastActive) > 2*HeartbeatInterval {
			log.Printf("Client %s heartbeat timeout, disconnecting", deviceID)
			s.removeClient(deviceID)
		}
	}
}

// ForwardOutput 转发 CLI 输出
func (s *WebSocketServer) ForwardOutput(stdout, stderr io.Reader) {
	s.wg.Add(2)

	// 转发 stdout
	go func() {
		defer s.wg.Done()
		s.forwardStream(stdout, "chunk")
	}()

	// 转发 stderr
	go func() {
		defer s.wg.Done()
		s.forwardStream(stderr, "error")
	}()
}

func (s *WebSocketServer) forwardStream(reader io.Reader, eventType string) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				s.errorCh <- fmt.Errorf("read error: %w", err)
			}
			break
		}

		if n == 0 {
			continue
		}

		lines := splitLines(buf[:n])
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}

			// 尝试解析 JSON
			var data interface{}
			if err := json.Unmarshal(line, &data); err != nil {
				// 非 JSON，作为文本
				data = map[string]interface{}{
					"content": string(line),
				}
			}

			// 添加到状态管理器
			event := state.Event{
				Type:      eventType,
				Timestamp: time.Now().UnixMilli(),
				Data:      data,
			}

			if err := s.stateMgr.AddOutput(s.taskID, event); err != nil {
				s.errorCh <- fmt.Errorf("failed to add output: %w", err)
			}

			// 发送到广播通道
			select {
			case s.broadcastCh <- event:
			default:
				// 通道已满，记录错误
				s.errorCh <- errors.New("broadcast channel full")
			}
		}
	}
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0

	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}

	if start < len(data) {
		lines = append(lines, data[start:])
	}

	return lines
}

func (s *WebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.errorCh <- fmt.Errorf("WebSocket upgrade failed: %w", err)
		return
	}

	// 配置连接
	conn.SetReadLimit(MaxMessageSize)
	conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(PongTimeout))
		return nil
	})

	deviceID := r.URL.Query().Get("device")
	if deviceID == "" {
		deviceID = generateDeviceID()
	}

	// 创建客户端
	client := &WebSocketClient{
		Conn:        conn,
		DeviceID:    deviceID,
		ConnectedAt: time.Now(),
		LastActive:  time.Now(),
		LastSeq:     0,
	}

	// 注册设备
	if err := s.stateMgr.AddDevice(s.taskID, deviceID); err != nil {
		s.errorCh <- fmt.Errorf("failed to add device: %w", err)
	}

	// 添加客户端
	s.addClient(deviceID, client)

	log.Printf("Device %s connected to task %s", deviceID, s.taskID)

	// 发送历史输出
	s.sendHistory(conn, deviceID)

	// 启动监听和心跳
	s.wg.Add(2)
	go s.listen(client)
	go s.sendPing(client)
}

func (s *WebSocketServer) addClient(deviceID string, client *WebSocketClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果已有同名客户端，先关闭旧连接
	if oldClient, exists := s.clients[deviceID]; exists {
		log.Printf("Replacing existing client: %s", deviceID)
		oldClient.SetClosed()
		if oldClient.Conn != nil {
			oldClient.Conn.Close()
		}
	}

	s.clients[deviceID] = client
}

func (s *WebSocketServer) removeClient(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client, exists := s.clients[deviceID]; exists {
		client.SetClosed()
		if client.Conn != nil {
			client.Conn.Close()
		}
		delete(s.clients, deviceID)
	}
}

func (s *WebSocketServer) sendHistory(conn *websocket.Conn, deviceID string) {
	// 获取设备上次读取位置
	fromSeq := int64(0)

	// 获取增量输出
	events, err := s.stateMgr.GetIncrementalOutput(s.taskID, fromSeq)
	if err != nil {
		s.errorCh <- fmt.Errorf("failed to get incremental output: %w", err)
		return
	}

	// 发送历史事件
	for _, event := range events {
		conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		if err := conn.WriteJSON(event); err != nil {
			s.errorCh <- fmt.Errorf("failed to send history: %w", err)
			return
		}
	}

	// 发送当前状态
	taskState, err := s.stateMgr.LoadState(s.taskID)
	if err == nil && taskState != nil {
		conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		conn.WriteJSON(map[string]interface{}{
			"type": "status",
			"data": map[string]string{
				"status": taskState.Status,
			},
		})
	}
}

func (s *WebSocketServer) listen(client *WebSocketClient) {
	defer func() {
		s.wg.Done()
		s.removeClient(client.DeviceID)
		log.Printf("Device %s disconnected", client.DeviceID)
	}()

	lastSeq := int64(0)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		client.Conn.SetReadDeadline(time.Now().Add(PongTimeout))
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.errorCh <- fmt.Errorf("WebSocket error: %w", err)
			}

			// 尝试重连
			if s.config.ReconnectEnabled && client.GetReconnectCount() < MaxReconnectAttempts {
				s.attemptReconnect(client)
			}
			return
		}

		client.UpdateActivity()

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			s.errorCh <- fmt.Errorf("failed to parse message: %w", err)
			continue
		}

		// 处理命令
		s.handleCommand(msg, client)

		// 更新设备序号
		lastSeq++
		if err := s.stateMgr.UpdateDeviceSeq(s.taskID, client.DeviceID, lastSeq); err != nil {
			s.errorCh <- fmt.Errorf("failed to update device seq: %w", err)
		}
	}
}

func (s *WebSocketServer) sendPing(client *WebSocketClient) {
	defer s.wg.Done()

	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if client.IsClosed() {
				return
			}

			client.Conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				s.errorCh <- fmt.Errorf("failed to send ping: %w", err)
				return
			}
		}
	}
}

func (s *WebSocketServer) attemptReconnect(client *WebSocketClient) {
	count := client.GetReconnectCount()
	if count >= MaxReconnectAttempts {
		s.errorCh <- fmt.Errorf("%s: %s", client.DeviceID, ErrMaxAttemptsReached.Error())
		return
	}

	// 指数退避
	delay := time.Duration(1<<uint(count)) * ReconnectDelayBase
	if delay > ReconnectDelayMax {
		delay = ReconnectDelayMax
	}

	log.Printf("Attempting reconnect for %s in %v (attempt %d/%d)",
		client.DeviceID, delay, count+1, MaxReconnectAttempts)

	client.IncrementReconnect()

	// 记录重连时间
	if err := s.stateMgr.AddOutput(s.taskID, state.Event{
		Type:      "reconnect",
		Timestamp: time.Now().UnixMilli(),
		Data: map[string]interface{}{
			"device_id": client.DeviceID,
			"attempt":   count + 1,
			"delay_ms":  delay.Milliseconds(),
		},
	}); err != nil {
		s.errorCh <- fmt.Errorf("failed to record reconnect: %w", err)
	}
}

func (s *WebSocketServer) handleCommand(msg ClientMessage, client *WebSocketClient) {
	switch msg.Command {
	case "send_input":
		// 发送输入到 CLI
		if s.cli != nil && s.cli.Stdin != nil {
			if content, ok := msg.Data["content"].(string); ok {
				if _, err := s.cli.Stdin.Write([]byte(content + "\n")); err != nil {
					s.errorCh <- fmt.Errorf("failed to send input: %w", err)
				}
			}
		}

	case "cancel":
		// 取消任务
		if s.cli != nil {
			if err := s.cli.Stop(); err != nil {
				s.errorCh <- fmt.Errorf("failed to cancel task: %w", err)
			}
		}

	case "get_status":
		// 获取状态
		taskState, err := s.stateMgr.LoadState(s.taskID)
		if err == nil && taskState != nil {
			s.sendToClient(client.DeviceID, map[string]interface{}{
				"type": "status",
				"data": map[string]interface{}{
					"status":     taskState.Status,
					"provider":   taskState.Provider,
					"seq":        taskState.Seq,
					"created_at": taskState.CreatedAt,
				},
			})
		}

	case "approve":
		// 批准操作（用于 AI 权限请求）
		if s.cli != nil && s.cli.Stdin != nil {
			if _, err := s.cli.Stdin.Write([]byte("y\n")); err != nil {
				s.errorCh <- fmt.Errorf("failed to send approval: %w", err)
			}
		}

	case "reject":
		// 拒绝操作
		if s.cli != nil && s.cli.Stdin != nil {
			if _, err := s.cli.Stdin.Write([]byte("n\n")); err != nil {
				s.errorCh <- fmt.Errorf("failed to send rejection: %w", err)
			}
		}
	}
}

func (s *WebSocketServer) broadcastToClients(event state.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for deviceID, client := range s.clients {
		if client.IsClosed() {
			continue
		}

		client.Conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		if err := client.Conn.WriteJSON(event); err != nil {
			s.errorCh <- fmt.Errorf("failed to broadcast to %s: %w", deviceID, err)
			// 不立即断开，让 listen goroutine 处理
		}
	}
}

func (s *WebSocketServer) broadcast(event state.Event) {
	select {
	case s.broadcastCh <- event:
	default:
		s.errorCh <- errors.New("broadcast channel full")
	}
}

func (s *WebSocketServer) sendToClient(deviceID string, data interface{}) {
	s.mu.RLock()
	client, exists := s.clients[deviceID]
	s.mu.RUnlock()

	if !exists {
		s.errorCh <- fmt.Errorf("%s: %w", deviceID, ErrClientNotFound)
		return
	}

	client.Conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	if err := client.Conn.WriteJSON(data); err != nil {
		s.errorCh <- fmt.Errorf("failed to send to %s: %w", deviceID, err)
	}
}

func (s *WebSocketServer) getClient(deviceID string) *WebSocketClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[deviceID]
}

// GetClientCount 获取客户端数量
func (s *WebSocketServer) GetClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// GetClientInfo 获取客户端信息
func (s *WebSocketServer) GetClientInfo(deviceID string) (*Device, error) {
	s.mu.RLock()
	client, exists := s.clients[deviceID]
	s.mu.RUnlock()

	if !exists {
		return nil, ErrClientNotFound
	}

	client.mu.RLock()
	defer client.mu.RUnlock()

	return &Device{
		DeviceID:        client.DeviceID,
		ConnectedAt:     client.ConnectedAt.UnixMilli(),
		LastActive:      client.LastActive.UnixMilli(),
		LastSeq:         client.LastSeq,
		ReconnectCount:  client.ReconnectCount,
		LastReconnectAt: client.LastActive.UnixMilli(),
	}, nil
}

// handleHealth 健康检查端点
func (s *WebSocketServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	clientCount := len(s.clients)
	s.mu.RUnlock()

	response := map[string]interface{}{
		"status":       "healthy",
		"task_id":      s.taskID,
		"client_count": clientCount,
		"uptime_ms":    time.Since(time.Now()).Milliseconds(), // 需要记录启动时间
		"cli_pid":      0,
	}

	if s.cli != nil {
		response["cli_pid"] = s.cli.Pid
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func generateDeviceID() string {
	return "device_" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[int(randomByte())%len(letters)]
	}
	return string(b)
}

func randomByte() byte {
	// 使用简单的随机字节生成
	// 生产环境应该使用 crypto/rand
	return byte(time.Now().UnixNano() % 256)
}
