// Package main provides a simple test client for UIP Gateway.
// This can be used to test the local adapter via HTTP and WebSocket.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type MessageRequest struct {
	SessionID string `json:"sessionId"`
	UserID    string `json:"userId"`
	Text      string `json:"text"`
	Type      string `json:"type,omitempty"`
}

type InteractionIntent struct {
	IntentID   string `json:"intentId"`
	IntentType string `json:"intentType"`
	Content    struct {
		Text string `json:"text"`
	} `json:"content"`
}

func main() {
	// Parse flags
	gatewayURL := flag.String("url", "http://localhost:8080", "Gateway URL")
	sessionID := flag.String("session", "", "Session ID (auto-generated if empty)")
	userID := flag.String("user", "test-user", "User ID")
	useWS := flag.Bool("ws", false, "Use WebSocket instead of HTTP")
	flag.Parse()

	if *sessionID == "" {
		*sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║         UIP Gateway Test Client                   ║")
	fmt.Println("╠═══════════════════════════════════════════════════╣")
	fmt.Printf("║  Gateway: %-40s ║\n", *gatewayURL)
	fmt.Printf("║  Session: %-40s ║\n", truncate(*sessionID, 40))
	fmt.Printf("║  User:    %-40s ║\n", *userID)
	fmt.Printf("║  Mode:    %-40s ║\n", modeStr(*useWS))
	fmt.Println("╚═══════════════════════════════════════════════════╝")
	fmt.Println()

	if *useWS {
		runWebSocket(*gatewayURL, *sessionID, *userID)
	} else {
		runHTTP(*gatewayURL, *sessionID, *userID)
	}
}

func runHTTP(gatewayURL, sessionID, userID string) {
	client := &http.Client{Timeout: 30 * time.Second}
	endpoint := fmt.Sprintf("%s/api/v1/local/message", gatewayURL)

	fmt.Println("Type your message and press Enter. Type 'quit' to exit.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if text == "quit" || text == "exit" {
			fmt.Println("Goodbye!")
			break
		}

		// Send message
		req := MessageRequest{
			SessionID: sessionID,
			UserID:    userID,
			Text:      text,
			Type:      "text",
		}

		body, _ := json.Marshal(req)
		resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Error: %s\n", string(respBody))
			continue
		}

		// Note: In async mode, the response is just acknowledgment
		// The actual response would come via WebSocket or webhook
		fmt.Printf("Clawdbot: (message sent, check WebSocket for response)\n")
	}
}

func runWebSocket(gatewayURL, sessionID, userID string) {
	// Convert HTTP URL to WebSocket URL
	u, err := url.Parse(gatewayURL)
	if err != nil {
		fmt.Printf("Invalid URL: %v\n", err)
		return
	}

	wsScheme := "ws"
	if u.Scheme == "https" {
		wsScheme = "wss"
	}

	wsURL := fmt.Sprintf("%s://%s/api/v1/local/ws?sessionId=%s&userId=%s",
		wsScheme, u.Host, url.QueryEscape(sessionID), url.QueryEscape(userID))

	fmt.Printf("Connecting to %s...\n", wsURL)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Printf("WebSocket connection failed: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Println("Connected! Type your message and press Enter. Type 'quit' to exit.")
	fmt.Println()

	// Handle interrupt
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Channel for user input
	inputCh := make(chan string)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			inputCh <- scanner.Text()
		}
		close(inputCh)
	}()

	// Channel for WebSocket messages
	messageCh := make(chan []byte)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					fmt.Printf("\nConnection closed: %v\n", err)
				}
				close(messageCh)
				return
			}
			messageCh <- message
		}
	}()

	fmt.Print("You: ")
	for {
		select {
		case text, ok := <-inputCh:
			if !ok {
				return
			}
			text = strings.TrimSpace(text)
			if text == "" {
				fmt.Print("You: ")
				continue
			}
			if text == "quit" || text == "exit" {
				fmt.Println("Goodbye!")
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}

			// Send message
			req := MessageRequest{
				Text: text,
				Type: "text",
			}
			if err := conn.WriteJSON(req); err != nil {
				fmt.Printf("\nSend error: %v\n", err)
				fmt.Print("You: ")
				continue
			}

		case message, ok := <-messageCh:
			if !ok {
				return
			}
			var intent InteractionIntent
			if err := json.Unmarshal(message, &intent); err != nil {
				fmt.Printf("\nReceived: %s\n", string(message))
			} else {
				fmt.Printf("\nClawdbot: %s\n", intent.Content.Text)
			}
			fmt.Print("You: ")

		case <-interrupt:
			fmt.Println("\nInterrupted, closing connection...")
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func modeStr(ws bool) string {
	if ws {
		return "WebSocket (real-time)"
	}
	return "HTTP (request/response)"
}
