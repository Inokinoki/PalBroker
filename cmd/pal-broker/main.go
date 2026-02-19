package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"pal-broker/internal/adapter"
	"pal-broker/internal/server"
	"pal-broker/internal/state"
)

var (
	taskID     = flag.String("task", "", "task ID")
	provider   = flag.String("provider", "claude", "CLI provider (claude, codex, copilot)")
	workDir    = flag.String("work-dir", ".", "working directory")
	sessionDir = flag.String("session-dir", "/tmp/pal-bridge", "session directory")
)

func main() {
	flag.Parse()

	if *taskID == "" {
		log.Fatal("task ID required (use --task)")
	}

	// 创建会话目录
	dir := filepath.Join(*sessionDir, *taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("Failed to create session dir: %v", err)
	}

	log.Printf("Starting pal-bridge for task %s (provider: %s)", *taskID, *provider)

	// 初始化状态管理器
	stateMgr := state.NewManager(*sessionDir)
	if err := stateMgr.CreateTask(*taskID, *provider); err != nil {
		log.Fatalf("Failed to create task state: %v", err)
	}

	// 保存 PID
	pidFile := filepath.Join(dir, "bridge.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		log.Printf("Warning: failed to write PID file: %v", err)
	}

	// 初始化 CLI 适配器
	cliAdapter := adapter.NewAdapter(*provider, *workDir)

	// 启动 CLI
	cli, err := cliAdapter.Start()
	if err != nil {
		log.Fatalf("Failed to start CLI: %v", err)
	}

	log.Printf("CLI started (PID: %d)", cli.Pid)

	// 保存 CLI PID
	cliPidFile := filepath.Join(dir, "cli.pid")
	os.WriteFile(cliPidFile, []byte(fmt.Sprintf("%d", cli.Pid)), 0644)

	// 启动 WebSocket 服务器
	wsServer := server.NewWebSocketServer(stateMgr, *taskID, cli)
	port, err := wsServer.Start(":0")
	if err != nil {
		log.Fatalf("Failed to start WebSocket server: %v", err)
	}

	log.Printf("WebSocket server listening on port %d", port)

	// 保存 WebSocket 端口
	portFile := filepath.Join(dir, "ws_port")
	os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0644)

	// 转发 CLI 输出到状态管理器
	go wsServer.ForwardOutput(cli.Stdout, cli.Stderr)

	// 优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received signal %v, shutting down...", sig)

	// 停止 CLI
	if err := cli.Stop(); err != nil {
		log.Printf("Warning: failed to stop CLI: %v", err)
	}

	// 更新状态
	stateMgr.UpdateStatus(*taskID, "stopped")

	log.Println("Cleanup completed, exiting")
	os.Exit(0)
}
