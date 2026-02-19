package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TaskState 任务状态
type TaskState struct {
	TaskID    string `json:"task_id"`
	Provider  string `json:"provider"`
	Status    string `json:"status"` // running, completed, failed, stopped
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	Seq       int64  `json:"seq"` // 当前序号
}

// Device 连接的设备
type Device struct {
	DeviceID    string `json:"device_id"`
	ConnectedAt int64  `json:"connected_at"`
	LastActive  int64  `json:"last_active"`
	LastSeq     int64  `json:"last_seq"` // 最后读取的序号
}

// Event 输出事件
type Event struct {
	Seq       int64       `json:"seq"`
	Type      string      `json:"type"` // chunk, file, status, error
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// Manager 状态管理器
type Manager struct {
	sessionDir string
	mu         sync.RWMutex
}

// NewManager 创建状态管理器
func NewManager(sessionDir string) *Manager {
	return &Manager{sessionDir: sessionDir}
}

// CreateTask 创建任务
func (m *Manager) CreateTask(taskID, provider string) error {
	dir := filepath.Join(m.sessionDir, taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	state := TaskState{
		TaskID:    taskID,
		Provider:  provider,
		Status:    "running",
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
		Seq:       0,
	}

	return m.saveState(taskID, &state)
}

// LoadState 加载任务状态
func (m *Manager) LoadState(taskID string) (*TaskState, error) {
	data, err := os.ReadFile(filepath.Join(m.sessionDir, taskID, "state.json"))
	if err != nil {
		return nil, err
	}

	var state TaskState
	return &state, json.Unmarshal(data, &state)
}

// UpdateStatus 更新任务状态
func (m *Manager) UpdateStatus(taskID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.LoadState(taskID)
	if err != nil {
		return err
	}

	state.Status = status
	state.UpdatedAt = time.Now().UnixMilli()

	return m.saveState(taskID, state)
}

// NextSeq 获取下一个序号
func (m *Manager) NextSeq(taskID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.LoadState(taskID)
	if err != nil {
		return 0, err
	}

	state.Seq++
	state.UpdatedAt = time.Now().UnixMilli()

	if err := m.saveState(taskID, state); err != nil {
		return 0, err
	}

	return state.Seq, nil
}

// AddOutput 添加输出事件
func (m *Manager) AddOutput(taskID string, event Event) error {
	seq, err := m.NextSeq(taskID)
	if err != nil {
		return err
	}

	event.Seq = seq
	event.Timestamp = time.Now().UnixMilli()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// 追加写入 output.jsonl
	f, err := os.OpenFile(
		filepath.Join(m.sessionDir, taskID, "output.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// AddDevice 添加设备
func (m *Manager) AddDevice(taskID, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	devices, err := m.loadDevices(taskID)
	if err != nil {
		devices = []Device{}
	}

	// 检查是否存在
	for i, d := range devices {
		if d.DeviceID == deviceID {
			devices[i].LastActive = time.Now().UnixMilli()
			return m.saveDevices(taskID, devices)
		}
	}

	// 添加新设备
	devices = append(devices, Device{
		DeviceID:    deviceID,
		ConnectedAt: time.Now().UnixMilli(),
		LastActive:  time.Now().UnixMilli(),
		LastSeq:     0,
	})

	return m.saveDevices(taskID, devices)
}

// UpdateDeviceSeq 更新设备最后读取序号
func (m *Manager) UpdateDeviceSeq(taskID, deviceID string, seq int64) error {
	devices, err := m.loadDevices(taskID)
	if err != nil {
		return err
	}

	for i, d := range devices {
		if d.DeviceID == deviceID {
			devices[i].LastSeq = seq
			devices[i].LastActive = time.Now().UnixMilli()
		}
	}

	return m.saveDevices(taskID, devices)
}

// GetIncrementalOutput 获取增量输出
func (m *Manager) GetIncrementalOutput(taskID string, fromSeq int64) ([]Event, error) {
	path := filepath.Join(m.sessionDir, taskID, "output.jsonl")

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Event{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if event.Seq > fromSeq {
			events = append(events, event)
		}
	}

	return events, scanner.Err()
}

// ListTasks 列出所有任务
func (m *Manager) ListTasks() ([]string, error) {
	entries, err := os.ReadDir(m.sessionDir)
	if err != nil {
		return nil, err
	}

	var tasks []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "task_") {
			tasks = append(tasks, e.Name())
		}
	}

	return tasks, nil
}

func (m *Manager) saveState(taskID string, state *TaskState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(
		filepath.Join(m.sessionDir, taskID, "state.json"),
		data,
		0644,
	)
}

func (m *Manager) loadDevices(taskID string) ([]Device, error) {
	path := filepath.Join(m.sessionDir, taskID, "devices.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Device{}, nil
		}
		return nil, err
	}

	var devices []Device
	return devices, json.Unmarshal(data, &devices)
}

func (m *Manager) saveDevices(taskID string, devices []Device) error {
	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(
		filepath.Join(m.sessionDir, taskID, "devices.json"),
		data,
		0644,
	)
}
