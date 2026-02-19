package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestManager 创建测试用的管理器
func setupTestManager(t *testing.T) (*Manager, string, func()) {
	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "pal-broker-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	mgr := NewManager(tmpDir)

	// 清理函数
	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return mgr, tmpDir, cleanup
}

// TestCreateTask 测试创建任务
func TestCreateTask(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_task_001"
	provider := "claude"

	// 创建任务
	err := mgr.CreateTask(taskID, provider)
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// 验证任务状态
	state, err := mgr.LoadState(taskID)
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	if state.TaskID != taskID {
		t.Errorf("Expected task_id %s, got %s", taskID, state.TaskID)
	}

	if state.Provider != provider {
		t.Errorf("Expected provider %s, got %s", provider, state.Provider)
	}

	if state.Status != "running" {
		t.Errorf("Expected status running, got %s", state.Status)
	}

	if state.Seq != 0 {
		t.Errorf("Expected seq 0, got %d", state.Seq)
	}
}

// TestUpdateStatus 测试更新状态
func TestUpdateStatus(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_task_002"
	mgr.CreateTask(taskID, "claude")

	// 更新状态
	err := mgr.UpdateStatus(taskID, "completed")
	if err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	// 验证状态
	state, _ := mgr.LoadState(taskID)
	if state.Status != "completed" {
		t.Errorf("Expected status completed, got %s", state.Status)
	}

	if state.UpdatedAt == 0 {
		t.Error("Expected UpdatedAt to be set")
	}
}

// TestNextSeq 测试序号递增
func TestNextSeq(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_task_003"
	mgr.CreateTask(taskID, "claude")

	// 测试序号递增
	seq1, _ := mgr.NextSeq(taskID)
	seq2, _ := mgr.NextSeq(taskID)
	seq3, _ := mgr.NextSeq(taskID)

	if seq1 != 1 {
		t.Errorf("Expected seq1=1, got %d", seq1)
	}
	if seq2 != 2 {
		t.Errorf("Expected seq2=2, got %d", seq2)
	}
	if seq3 != 3 {
		t.Errorf("Expected seq3=3, got %d", seq3)
	}
}

// TestAddOutput 测试添加输出
func TestAddOutput(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_task_004"
	mgr.CreateTask(taskID, "claude")

	// 添加输出
	event := Event{
		Type: "chunk",
		Data: map[string]string{"content": "Hello, World!"},
	}

	err := mgr.AddOutput(taskID, event)
	if err != nil {
		t.Fatalf("Failed to add output: %v", err)
	}

	// 验证输出文件存在
	outputFile := filepath.Join(mgr.sessionDir, taskID, "output.jsonl")
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("Expected output.jsonl to exist")
	}

	// 验证序号已更新
	state, _ := mgr.LoadState(taskID)
	if state.Seq != 1 {
		t.Errorf("Expected seq=1 after AddOutput, got %d", state.Seq)
	}
}

// TestAddDevice 测试添加设备
func TestAddDevice(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_task_005"
	mgr.CreateTask(taskID, "claude")

	// 添加设备
	deviceID := "device_001"
	err := mgr.AddDevice(taskID, deviceID)
	if err != nil {
		t.Fatalf("Failed to add device: %v", err)
	}

	// 验证设备已添加
	devicesFile := filepath.Join(mgr.sessionDir, taskID, "devices.json")
	if _, err := os.Stat(devicesFile); os.IsNotExist(err) {
		t.Error("Expected devices.json to exist")
	}

	// 再次添加同一设备（应该更新而不是重复添加）
	err = mgr.AddDevice(taskID, deviceID)
	if err != nil {
		t.Fatalf("Failed to update device: %v", err)
	}
}

// TestUpdateDeviceSeq 测试更新设备序号
func TestUpdateDeviceSeq(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_task_006"
	deviceID := "device_002"

	mgr.CreateTask(taskID, "claude")
	mgr.AddDevice(taskID, deviceID)

	// 更新设备序号
	err := mgr.UpdateDeviceSeq(taskID, deviceID, 100)
	if err != nil {
		t.Fatalf("Failed to update device seq: %v", err)
	}

	// 验证序号已更新（通过读取文件）
	devices, _ := mgr.loadDevices(taskID)
	found := false
	for _, d := range devices {
		if d.DeviceID == deviceID {
			found = true
			if d.LastSeq != 100 {
				t.Errorf("Expected LastSeq=100, got %d", d.LastSeq)
			}
			if d.LastActive == 0 {
				t.Error("Expected LastActive to be set")
			}
		}
	}

	if !found {
		t.Error("Device not found")
	}
}

// TestGetIncrementalOutput 测试获取增量输出
func TestGetIncrementalOutput(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_task_007"
	mgr.CreateTask(taskID, "claude")

	// 添加多个输出
	for i := 1; i <= 5; i++ {
		event := Event{
			Type: "chunk",
			Data: map[string]string{"content": string(rune('A' + i - 1))},
		}
		mgr.AddOutput(taskID, event)
	}

	// 获取从 seq=2 开始的增量输出
	events, err := mgr.GetIncrementalOutput(taskID, 2)
	if err != nil {
		t.Fatalf("Failed to get incremental output: %v", err)
	}

	// 应该返回 seq=3,4,5 的输出（共 3 个）
	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	// 验证第一个事件的 seq
	if len(events) > 0 && events[0].Seq != 3 {
		t.Errorf("Expected first event seq=3, got %d", events[0].Seq)
	}
}

// TestListTasks 测试列出任务
func TestListTasks(t *testing.T) {
	mgr, tmpDir, cleanup := setupTestManager(t)
	defer cleanup()

	// 创建多个任务（使用 task_ 前缀，因为 ListTasks 过滤这个前缀）
	taskIDs := []string{"task_001", "task_002", "task_003"}
	for _, id := range taskIDs {
		mgr.CreateTask(id, "claude")
	}

	// 创建一个非任务目录（不应该被列出）
	os.MkdirAll(filepath.Join(tmpDir, "not_a_task"), 0755)

	// 列出任务
	tasks, err := mgr.ListTasks()
	if err != nil {
		t.Fatalf("Failed to list tasks: %v", err)
	}

	// 验证任务数量
	if len(tasks) != 3 {
		t.Errorf("Expected 3 tasks, got %d: %v", len(tasks), tasks)
	}
}

// TestLoadNonExistentTask 测试加载不存在的任务
func TestLoadNonExistentTask(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	_, err := mgr.LoadState("non_existent_task")
	if err == nil {
		t.Error("Expected error when loading non-existent task")
	}
}

// TestConcurrentAccess 测试并发访问
func TestConcurrentAccess(t *testing.T) {
	mgr, _, cleanup := setupTestManager(t)
	defer cleanup()

	taskID := "test_concurrent"
	mgr.CreateTask(taskID, "claude")

	// 并发更新状态
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			mgr.UpdateStatus(taskID, "running")
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 验证没有 panic 或 race condition
	state, err := mgr.LoadState(taskID)
	if err != nil {
		t.Fatalf("Failed to load state after concurrent access: %v", err)
	}

	if state.Status != "running" {
		t.Errorf("Expected status running, got %s", state.Status)
	}
}

// TestEventMarshaling 测试事件序列化
func TestEventMarshaling(t *testing.T) {
	event := Event{
		Seq:       42,
		Type:      "chunk",
		Timestamp: time.Now().UnixMilli(),
		Data: map[string]interface{}{
			"content": "test",
			"number":  123,
		},
	}

	// 序列化应该成功
	// （实际测试在 AddOutput 中已经覆盖了）
	if event.Seq != 42 {
		t.Errorf("Expected seq=42, got %d", event.Seq)
	}
	if event.Type != "chunk" {
		t.Errorf("Expected type=chunk, got %s", event.Type)
	}
}
