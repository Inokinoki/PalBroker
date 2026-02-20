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
	taskID     = flag.String("task", "", "task ID (alias: --quest-id)")
	questID    = flag.String("quest-id", "", "quest ID (alias: --task)")
	provider   = flag.String("provider", "claude", "AI provider (claude, codex, copilot)")
	workDir    = flag.String("work-dir", ".", "working directory")
	sessionDir = flag.String("session-dir", "/tmp/pal-broker", "session directory")
	portFlag   = flag.String("port", ":0", "WebSocket port (default: random, e.g., :8765)")
)

func main() {
	flag.Parse()

	// Support both --task and --quest-id flags
	id := *taskID
	if id == "" {
		id = *questID
	}

	if id == "" {
		log.Fatal("task/quest ID required (use --task or --quest-id)")
	}

	// Create session directory
	dir := filepath.Join(*sessionDir, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("Failed to create session directory %s: %v", dir, err)
	}

	log.Printf("Starting pal-broker for quest %s (provider: %s)", id, *provider)

	// Initialize state manager
	stateMgr := state.NewManager(*sessionDir)
	if err := stateMgr.CreateTask(id, *provider); err != nil {
		log.Fatalf("Failed to create task state: %v", err)
	}

	// Save pal-broker PID
	pidFile := filepath.Join(dir, "bridge.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		log.Printf("Warning: failed to write PID file: %v", err)
	}

	// Initialize CLI adapter
	cliAdapter := adapter.NewAdapter(*provider, *workDir)

	// Start AI CLI
	cli, err := cliAdapter.Start()
	if err != nil {
		log.Fatalf("Failed to start AI CLI: %v", err)
	}

	log.Printf("AI CLI started (PID: %d)", cli.Pid)

	// Save AI CLI PID
	cliPidFile := filepath.Join(dir, "cli.pid")
	if err := os.WriteFile(cliPidFile, []byte(fmt.Sprintf("%d", cli.Pid)), 0644); err != nil {
		log.Printf("Warning: failed to write CLI PID file: %v", err)
	}

	// Start WebSocket server
	wsServer := server.NewWebSocketServer(stateMgr, id, cli)
	port, err := wsServer.Start(*portFlag)
	if err != nil {
		log.Fatalf("Failed to start WebSocket server on %s: %v", *portFlag, err)
	}

	log.Printf("WebSocket server listening on port %d", port)

	// Save WebSocket port to file
	portFile := filepath.Join(dir, "ws_port")
	if err := os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0644); err != nil {
		log.Fatalf("Failed to write port file %s: %v", portFile, err)
	}
	log.Printf("Port file written to: %s", portFile)

	// Forward CLI output to state manager
	go wsServer.ForwardOutput(cli.Stdout, cli.Stderr)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received signal %v, shutting down...", sig)

	// Stop AI CLI
	if err := cli.Stop(); err != nil {
		log.Printf("Warning: failed to stop AI CLI: %v", err)
	}

	// Update task status
	stateMgr.UpdateStatus(id, "stopped")

	log.Println("Cleanup completed, exiting")
	os.Exit(0)
}
