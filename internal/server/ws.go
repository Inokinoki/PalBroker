package server

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"pal-broker/internal/adapter"
	"pal-broker/internal/state"
)

// Device 设备信息（本地副本）
type Device struct {
	DeviceID    string `json:"device_id"`
	ConnectedAt int64  `json:"connected_at"`
	LastActive  int64  `json:"last_active"`
	LastSeq     int64  `json:"last_seq"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WebSocketServer WebSocket 服务器
type WebSocketServer struct {
	stateMgr *state.Manager
	taskID   string
	cli      *adapter.CLIProcess
	clients  map[string]*websocket.Conn
	mu       sync.RWMutex
}

// ClientMessage 客户端消息
type ClientMessage struct {
	Command string                 `json:"command"`
	Data    map[string]interface{} `json:"data"`
}

// NewWebSocketServer 创建 WebSocket 服务器
func NewWebSocketServer(stateMgr *state.Manager, taskID string, cli *adapter.CLIProcess) *WebSocketServer {
	return &WebSocketServer{
		stateMgr: stateMgr,
		taskID:   taskID,
		cli:      cli,
		clients:  make(map[string]*websocket.Conn),
	}
}

// Start 启动服务器
func (s *WebSocketServer) Start(addr string) (int, error) {
	http.HandleFunc("/ws", s.handleWebSocket)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		if err := http.Serve(listener, nil); err != nil {
			log.Printf("WebSocket server error: %v", err)
		}
	}()

	return port, nil
}

// ForwardOutput 转发 CLI 输出
func (s *WebSocketServer) ForwardOutput(stdout, stderr io.Reader) {
	// 转发 stdout
	go func() {
		s.forwardStream(stdout, "chunk")
	}()

	// 转发 stderr
	go func() {
		s.forwardStream(stderr, "error")
	}()
}

func (s *WebSocketServer) forwardStream(reader io.Reader, eventType string) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
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
			s.stateMgr.AddOutput(s.taskID, state.Event{
				Type: eventType,
				Data: data,
			})

			// 广播给所有客户端
			s.broadcast(state.Event{
				Type: eventType,
				Data: data,
			})
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
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	deviceID := r.URL.Query().Get("device")
	if deviceID == "" {
		deviceID = generateDeviceID()
	}

	// 注册设备
	if err := s.stateMgr.AddDevice(s.taskID, deviceID); err != nil {
		log.Printf("Failed to add device: %v", err)
	}

	s.mu.Lock()
	s.clients[deviceID] = conn
	s.mu.Unlock()

	log.Printf("Device %s connected to task %s", deviceID, s.taskID)

	// 发送历史输出
	s.sendHistory(conn, deviceID)

	// 监听消息
	go s.listen(conn, deviceID)
}

func (s *WebSocketServer) sendHistory(conn *websocket.Conn, deviceID string) {
	// 获取设备上次读取位置 - 从 devices.json 读取
	fromSeq := int64(0)

	// 获取增量输出
	events, err := s.stateMgr.GetIncrementalOutput(s.taskID, fromSeq)
	if err != nil {
		log.Printf("Failed to get incremental output: %v", err)
		return
	}

	// 发送历史事件
	for _, event := range events {
		if err := conn.WriteJSON(event); err != nil {
			log.Printf("Failed to send history: %v", err)
			return
		}
	}

	// 发送当前状态
	taskState, _ := s.stateMgr.LoadState(s.taskID)
	if taskState != nil {
		conn.WriteJSON(map[string]interface{}{
			"type": "status",
			"data": map[string]string{
				"status": taskState.Status,
			},
		})
	}
}

func (s *WebSocketServer) listen(conn *websocket.Conn, deviceID string) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, deviceID)
		s.mu.Unlock()
		conn.Close()
		log.Printf("Device %s disconnected", deviceID)
	}()

	lastSeq := int64(0)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		// 处理命令
		s.handleCommand(msg, deviceID)

		// 更新设备序号
		s.stateMgr.UpdateDeviceSeq(s.taskID, deviceID, lastSeq)
	}
}

func (s *WebSocketServer) handleCommand(msg ClientMessage, deviceID string) {
	switch msg.Command {
	case "send_input":
		// 发送输入到 CLI
		if s.cli != nil && s.cli.Stdin != nil {
			if content, ok := msg.Data["content"].(string); ok {
				s.cli.Stdin.Write([]byte(content + "\n"))
			}
		}

	case "cancel":
		// 取消任务
		if s.cli != nil {
			s.cli.Stop()
		}

	case "get_status":
		// 获取状态
		taskState, _ := s.stateMgr.LoadState(s.taskID)
		if taskState != nil {
			conn := s.getClient(deviceID)
			if conn != nil {
				conn.WriteJSON(map[string]interface{}{
					"type": "status",
					"data": map[string]interface{}{
						"status":     taskState.Status,
						"provider":   taskState.Provider,
						"seq":        taskState.Seq,
						"created_at": taskState.CreatedAt,
					},
				})
			}
		}
	}
}

func (s *WebSocketServer) broadcast(event state.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for deviceID, conn := range s.clients {
		if err := conn.WriteJSON(event); err != nil {
			log.Printf("Failed to broadcast to %s: %v", deviceID, err)
			// 不立即断开，让 listen goroutine 处理
		}
	}
}

func (s *WebSocketServer) getClient(deviceID string) *websocket.Conn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[deviceID]
}

func generateDeviceID() string {
	// 简单生成设备 ID
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
	// 简单随机字节生成
	return byte(42) // TODO: 使用 crypto/rand
}
