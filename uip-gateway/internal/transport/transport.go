// Package transport provides transport layer implementations for communication
// between UIP Gateway and OpenClaw Universal IM.
//
// When OpenClaw is configured to use WebSocket or Polling transport, this package
// provides the server-side implementations that OpenClaw connects to.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Message represents a message in the transport layer.
type Message struct {
	ID           string                 `json:"messageId"`
	Timestamp    int64                  `json:"timestamp"`
	Sender       Sender                 `json:"sender"`
	Conversation Conversation           `json:"conversation"`
	Text         string                 `json:"text,omitempty"`
	Attachments  []Attachment           `json:"attachments,omitempty"`
	Meta         map[string]interface{} `json:"meta,omitempty"`
}

// Sender represents message sender.
type Sender struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	IsBot    bool   `json:"isBot,omitempty"`
}

// Conversation represents conversation context.
type Conversation struct {
	Type     string `json:"type"` // "direct", "group", "channel"
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
}

// Attachment represents a message attachment.
type Attachment struct {
	Kind        string `json:"kind"`
	URL         string `json:"url,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	FileName    string `json:"fileName,omitempty"`
}

// MessageHandler is called when a message is received through any transport.
type MessageHandler func(msg *Message) error

// Transport defines the interface for transport implementations.
type Transport interface {
	// Start starts the transport.
	Start(ctx context.Context) error
	// Stop stops the transport.
	Stop(ctx context.Context) error
	// Send queues a message to be sent to OpenClaw.
	Send(msg *Message) error
	// SetHandler sets the handler for incoming messages.
	SetHandler(handler MessageHandler)
}

// WebSocketServer implements a WebSocket server for OpenClaw to connect to.
type WebSocketServer struct {
	logger   *zap.Logger
	upgrader websocket.Upgrader
	handler  MessageHandler

	// Active connections
	connMu sync.RWMutex
	conns  map[*websocket.Conn]bool

	// Message queue for outgoing messages
	outQueue chan *Message

	// Shutdown
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewWebSocketServer creates a new WebSocket server.
func NewWebSocketServer(logger *zap.Logger) *WebSocketServer {
	return &WebSocketServer{
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		conns:    make(map[*websocket.Conn]bool),
		outQueue: make(chan *Message, 100),
		stopCh:   make(chan struct{}),
	}
}

// Start starts the WebSocket server (run in background).
func (ws *WebSocketServer) Start(ctx context.Context) error {
	ws.wg.Add(1)
	go ws.broadcastLoop()
	ws.logger.Info("WebSocket server started")
	return nil
}

// Stop stops the WebSocket server.
func (ws *WebSocketServer) Stop(ctx context.Context) error {
	close(ws.stopCh)

	// Close all connections
	ws.connMu.Lock()
	for conn := range ws.conns {
		conn.Close()
	}
	ws.connMu.Unlock()

	ws.wg.Wait()
	ws.logger.Info("WebSocket server stopped")
	return nil
}

// Send queues a message to broadcast to all connected clients.
func (ws *WebSocketServer) Send(msg *Message) error {
	select {
	case ws.outQueue <- msg:
		return nil
	default:
		return fmt.Errorf("message queue full")
	}
}

// SetHandler sets the handler for incoming messages.
func (ws *WebSocketServer) SetHandler(handler MessageHandler) {
	ws.handler = handler
}

// HTTPHandler returns an http.Handler for WebSocket upgrade.
func (ws *WebSocketServer) HTTPHandler() http.Handler {
	return http.HandlerFunc(ws.handleConnection)
}

func (ws *WebSocketServer) handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		ws.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	ws.connMu.Lock()
	ws.conns[conn] = true
	ws.connMu.Unlock()

	ws.logger.Info("WebSocket client connected",
		zap.String("remoteAddr", r.RemoteAddr))

	ws.wg.Add(1)
	go ws.readLoop(conn)
}

func (ws *WebSocketServer) readLoop(conn *websocket.Conn) {
	defer ws.wg.Done()
	defer func() {
		ws.connMu.Lock()
		delete(ws.conns, conn)
		ws.connMu.Unlock()
		conn.Close()
		ws.logger.Info("WebSocket client disconnected")
	}()

	for {
		select {
		case <-ws.stopCh:
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				ws.logger.Error("WebSocket read error", zap.Error(err))
			}
			return
		}

		// Parse message
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			ws.logger.Warn("Failed to parse WebSocket message", zap.Error(err))
			continue
		}

		// Call handler
		if ws.handler != nil {
			if err := ws.handler(&msg); err != nil {
				ws.logger.Error("Message handler error", zap.Error(err))
			}
		}
	}
}

func (ws *WebSocketServer) broadcastLoop() {
	defer ws.wg.Done()

	for {
		select {
		case <-ws.stopCh:
			return
		case msg := <-ws.outQueue:
			ws.broadcast(msg)
		}
	}
}

func (ws *WebSocketServer) broadcast(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		ws.logger.Error("Failed to marshal message", zap.Error(err))
		return
	}

	ws.connMu.RLock()
	defer ws.connMu.RUnlock()

	for conn := range ws.conns {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			ws.logger.Warn("Failed to send message", zap.Error(err))
		}
	}
}

// ConnectionCount returns the number of active connections.
func (ws *WebSocketServer) ConnectionCount() int {
	ws.connMu.RLock()
	defer ws.connMu.RUnlock()
	return len(ws.conns)
}

// PollingServer implements an HTTP polling endpoint for OpenClaw.
type PollingServer struct {
	logger  *zap.Logger
	handler MessageHandler

	// Message queue for messages to be polled
	queueMu sync.RWMutex
	queue   []*Message

	// Track last poll time per client
	clientsMu sync.RWMutex
	clients   map[string]int64 // clientID -> lastPollTime
}

// NewPollingServer creates a new polling server.
func NewPollingServer(logger *zap.Logger) *PollingServer {
	return &PollingServer{
		logger:  logger,
		queue:   make([]*Message, 0),
		clients: make(map[string]int64),
	}
}

// Start starts the polling server.
func (ps *PollingServer) Start(ctx context.Context) error {
	ps.logger.Info("Polling server started")
	return nil
}

// Stop stops the polling server.
func (ps *PollingServer) Stop(ctx context.Context) error {
	ps.logger.Info("Polling server stopped")
	return nil
}

// Send queues a message to be polled by OpenClaw.
func (ps *PollingServer) Send(msg *Message) error {
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}

	ps.queueMu.Lock()
	ps.queue = append(ps.queue, msg)
	// Keep only last 1000 messages
	if len(ps.queue) > 1000 {
		ps.queue = ps.queue[len(ps.queue)-1000:]
	}
	ps.queueMu.Unlock()

	return nil
}

// SetHandler sets the handler for incoming messages.
func (ps *PollingServer) SetHandler(handler MessageHandler) {
	ps.handler = handler
}

// HTTPHandler returns an http.Handler for polling endpoint.
func (ps *PollingServer) HTTPHandler() http.Handler {
	return http.HandlerFunc(ps.handlePoll)
}

// InboundHandler returns an http.Handler for receiving messages from external IM.
func (ps *PollingServer) InboundHandler() http.Handler {
	return http.HandlerFunc(ps.handleInbound)
}

func (ps *PollingServer) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get since parameter
	sinceStr := r.URL.Query().Get("since")
	var since int64
	if sinceStr != "" {
		fmt.Sscanf(sinceStr, "%d", &since)
	}

	// Get messages since the given timestamp
	ps.queueMu.RLock()
	var messages []*Message
	for _, msg := range ps.queue {
		if msg.Timestamp > since {
			messages = append(messages, msg)
		}
	}
	ps.queueMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
	})
}

func (ps *PollingServer) handleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set timestamp if not provided
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}

	// Generate ID if not provided
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}

	// Call handler
	if ps.handler != nil {
		if err := ps.handler(&msg); err != nil {
			ps.logger.Error("Message handler error", zap.Error(err))
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":        true,
		"messageId": msg.ID,
	})
}

// QueueSize returns the number of messages in the queue.
func (ps *PollingServer) QueueSize() int {
	ps.queueMu.RLock()
	defer ps.queueMu.RUnlock()
	return len(ps.queue)
}
