package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Message - WebSocket message structure
type Message struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp int64                  `json:"timestamp"`
	Seq       int64                  `json:"seq,omitempty"`
}

// Config - CLI configuration
type Config struct {
	URL       string
	QuestID   string
	Provider  string
	Task      string
	Interactive bool
	Verbose   bool
}

func main() {
	// Parse flags
	url := flag.String("url", "ws://localhost:8765/ws", "WebSocket server URL")
	questID := flag.String("quest-id", "test-quest", "Quest ID")
	provider := flag.String("provider", "claude", "AI provider")
	task := flag.String("task", "", "Task description")
	interactive := flag.Bool("i", false, "Interactive mode")
	verbose := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	config := Config{
		URL:         *url,
		QuestID:     *questID,
		Provider:    *provider,
		Task:        *task,
		Interactive: *interactive,
		Verbose:     *verbose,
	}

	if config.Verbose {
		log.Printf("Connecting to: %s", config.URL)
		log.Printf("Quest ID: %s", config.QuestID)
		log.Printf("Provider: %s", config.Provider)
	}

	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(config.URL, nil)
	if err != nil {
		log.Fatalf("❌ Failed to connect: %v", err)
	}
	defer conn.Close()

	log.Printf("✅ Connected to %s", config.URL)

	// Handle interrupts
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Read messages from server
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("❌ Read error: %v", err)
				}
				return
			}

			if config.Verbose {
				log.Printf("📥 Received: %s", string(message))
			}

			// Pretty print JSON
			var msg Message
			if err := json.Unmarshal(message, &msg); err == nil {
				printMessage(msg)
			} else {
				fmt.Printf("📥 %s\n", string(message))
			}
		}
	}()

	// Send initial connection message
	if config.Task != "" {
		msg := Message{
			Type:      "start_task",
			Timestamp: time.Now().UnixMilli(),
			Data: map[string]interface{}{
				"quest_id": config.QuestID,
				"provider": config.Provider,
				"task":     config.Task,
			},
		}
		sendMessage(conn, msg)
	}

	// Interactive mode or exit
	if config.Interactive {
		log.Println("📝 Interactive mode - type messages and press Enter")
		log.Println("Commands:")
		log.Println("  /status - Request status")
		log.Println("  /quit   - Exit")
		log.Println("")

		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("> ")
			input, err := reader.ReadString('\n')
			if err != nil {
				log.Printf("❌ Read error: %v", err)
				return
			}

			input = strings.TrimSpace(input)

			// Handle commands
			if input == "/quit" || input == "/exit" {
				log.Println("👋 Goodbye!")
				return
			}

			if input == "/status" {
				msg := Message{
					Type:      "get_status",
					Timestamp: time.Now().UnixMilli(),
					Data: map[string]interface{}{
						"quest_id": config.QuestID,
					},
				}
				sendMessage(conn, msg)
				continue
			}

			// Send as input message
			if input != "" {
				msg := Message{
					Type:      "user_input",
					Timestamp: time.Now().UnixMilli(),
					Data: map[string]interface{}{
						"quest_id": config.QuestID,
						"content":  input,
					},
				}
				sendMessage(conn, msg)
			}
		}
	} else {
		// Wait for interrupt
		<-interrupt
		log.Println("\n👋 Disconnecting...")
	}
}

func sendMessage(conn *websocket.Conn, msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("❌ Marshal error: %v", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("❌ Send error: %v", err)
		return
	}

	log.Printf("📤 Sent: %s", msg.Type)
}

func printMessage(msg Message) {
	switch msg.Type {
	case "connected":
		fmt.Printf("🔗 Connected\n")
		if msg.Data != nil {
			if questID, ok := msg.Data["quest_id"].(string); ok {
				fmt.Printf("   Quest: %s\n", questID)
			}
		}

	case "status":
		fmt.Printf("📊 Status Update\n")
		if msg.Data != nil {
			if state, ok := msg.Data["state"].(string); ok {
				fmt.Printf("   State: %s\n", state)
			}
			if progress, ok := msg.Data["progress"].(float64); ok {
				fmt.Printf("   Progress: %.0f%%\n", progress)
			}
		}

	case "output":
		fmt.Printf("💬 Output\n")
		if msg.Data != nil {
			if content, ok := msg.Data["content"].(string); ok {
				fmt.Printf("   %s\n", content)
			}
		}

	case "error":
		fmt.Printf("❌ Error\n")
		if msg.Data != nil {
			if message, ok := msg.Data["message"].(string); ok {
				fmt.Printf("   %s\n", message)
			}
		}

	case "task_complete":
		fmt.Printf("✅ Task Complete\n")
		if msg.Data != nil {
			if result, ok := msg.Data["result"].(string); ok {
				fmt.Printf("   Result: %s\n", result)
			}
		}

	default:
		fmt.Printf("📨 %s\n", msg.Type)
	}
}
