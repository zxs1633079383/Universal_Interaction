// Package local implements a local IM adapter for testing and local integrations.
// This adapter exposes HTTP and WebSocket endpoints for direct interaction.
package local

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/zlc_ai/uip-gateway/internal/adapter"
	"github.com/zlc_ai/uip-gateway/internal/protocol"
)

func init() {
	adapter.RegisterAdapter("local", NewLocalAdapter)
}

// Config holds the configuration for the local adapter.
type Config struct {
	HTTPPath string `json:"http_path" yaml:"http_path"`
}

// LocalAdapter implements the IMAdapter interface for local IM interactions.
// It provides both HTTP REST and WebSocket interfaces.
type LocalAdapter struct {
	name         string
	config       Config
	logger       *zap.Logger
	eventHandler adapter.EventHandler

	// WebSocket connections
	wsConnsMu sync.RWMutex
	wsConns   map[string]*wsConnection

	// HTTP server (managed externally, this just provides handlers)
	upgrader websocket.Upgrader

	// Capabilities
	capabilities *protocol.SurfaceCapabilities

	// State
	started bool
	mu      sync.RWMutex
}

type wsConnection struct {
	conn      *websocket.Conn
	sessionID string
	userID    string
	sendCh    chan []byte
	done      chan struct{}
}

// MessageRequest is the JSON structure for HTTP message requests.
type MessageRequest struct {
	SessionID        string `json:"sessionId"`
	UserID           string `json:"userId"`
	Text             string `json:"text"`
	Type             string `json:"type,omitempty"`             // text, command, event
	ChannelID        string `json:"channelId,omitempty"`        // External IM channel/group ID for routing outbound
	ConversationType string `json:"conversationType,omitempty"` // "direct", "group", "channel"
}

// MessageResponse is the JSON structure for HTTP message responses.
type MessageResponse struct {
	Success bool                        `json:"success"`
	Intent  *protocol.InteractionIntent `json:"intent,omitempty"`
	Error   *protocol.UIPError          `json:"error,omitempty"`
}

// NewLocalAdapter creates a new local IM adapter.
func NewLocalAdapter(config map[string]interface{}) (adapter.IMAdapter, error) {
	cfg := Config{
		HTTPPath: "/api/v1/local",
	}

	if path, ok := config["http_path"].(string); ok {
		cfg.HTTPPath = path
	}

	logger, _ := zap.NewProduction()

	return &LocalAdapter{
		name:    "local",
		config:  cfg,
		logger:  logger,
		wsConns: make(map[string]*wsConnection),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for local development
			},
		},
		capabilities: &protocol.SurfaceCapabilities{
			SupportsReply:      true,
			SupportsEdit:       true,
			SupportsReaction:   false,
			SupportsThread:     false,
			SupportsAttachment: false,
			SupportsMarkdown:   true,
		},
	}, nil
}

func (a *LocalAdapter) Name() string {
	return a.name
}

func (a *LocalAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return fmt.Errorf("adapter already started")
	}

	a.started = true
	a.logger.Info("Local adapter started", zap.String("path", a.config.HTTPPath))
	return nil
}

func (a *LocalAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.started {
		return nil
	}

	// Close all WebSocket connections
	a.wsConnsMu.Lock()
	for _, conn := range a.wsConns {
		close(conn.done)
		conn.conn.Close()
	}
	a.wsConns = make(map[string]*wsConnection)
	a.wsConnsMu.Unlock()

	a.started = false
	a.logger.Info("Local adapter stopped")
	return nil
}

func (a *LocalAdapter) OnEvent(handler adapter.EventHandler) {
	a.eventHandler = handler
}

func (a *LocalAdapter) SendIntent(ctx context.Context, intent *protocol.InteractionIntent) error {
	// Try to send via WebSocket if connection exists
	a.wsConnsMu.RLock()
	conn, exists := a.wsConns[intent.TargetSessionID]
	a.wsConnsMu.RUnlock()

	if exists {
		data, err := json.Marshal(intent)
		if err != nil {
			return fmt.Errorf("failed to marshal intent: %w", err)
		}

		select {
		case conn.sendCh <- data:
			a.logger.Debug("Intent sent via WebSocket",
				zap.String("intentId", intent.IntentID),
				zap.String("sessionId", intent.TargetSessionID))
		case <-ctx.Done():
			return ctx.Err()
		default:
			a.logger.Warn("WebSocket send buffer full, dropping message")
		}
	}

	return nil
}

func (a *LocalAdapter) Capabilities() *protocol.SurfaceCapabilities {
	return a.capabilities
}

// HTTPHandler returns an http.Handler for the REST API endpoint.
func (a *LocalAdapter) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/message", a.handleMessage)
	mux.HandleFunc("/ws", a.handleWebSocket)
	mux.HandleFunc("/health", a.handleHealth)
	return mux
}

func (a *LocalAdapter) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.sendErrorResponse(w, protocol.ErrCodeProtocolError, "Invalid request body", "")
		return
	}

	// Validate required fields
	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}
	if req.UserID == "" {
		req.UserID = "anonymous"
	}
	if req.Type == "" {
		req.Type = "text"
	}

	// Determine conversation type
	convType := req.ConversationType
	if convType == "" {
		if req.ChannelID != "" {
			convType = "channel"
		} else {
			convType = "direct"
		}
	}

	// Create CIE
	inputType := protocol.InputType(req.Type)
	payload := map[string]interface{}{
		"text":             req.Text,
		"channelId":        req.ChannelID,
		"conversationType": convType,
	}

	// Use channelId as sessionId if provided (for routing outbound responses)
	sessionID := req.SessionID
	if req.ChannelID != "" && sessionID == "" {
		sessionID = req.ChannelID
	}

	event := protocol.NewCanonicalInteractionEvent(
		sessionID,
		req.UserID,
		inputType,
		payload,
		*a.capabilities,
		"local-adapter",
	)
	event.Meta.AdapterName = a.name

	a.logger.Debug("Received HTTP message",
		zap.String("sessionId", sessionID),
		zap.String("userId", req.UserID),
		zap.String("channelId", req.ChannelID),
		zap.String("conversationType", convType),
		zap.String("text", req.Text))

	// Emit event to gateway
	if a.eventHandler != nil {
		a.eventHandler(event)
	}

	// Return success (async processing)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MessageResponse{
		Success: true,
	})
}

func (a *LocalAdapter) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		a.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	// Get session ID from query params or generate one
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		userID = "ws-user-" + sessionID[:8]
	}

	wsConn := &wsConnection{
		conn:      conn,
		sessionID: sessionID,
		userID:    userID,
		sendCh:    make(chan []byte, 256),
		done:      make(chan struct{}),
	}

	a.wsConnsMu.Lock()
	a.wsConns[sessionID] = wsConn
	a.wsConnsMu.Unlock()

	a.logger.Info("WebSocket connection established",
		zap.String("sessionId", sessionID),
		zap.String("userId", userID))

	// Start read and write goroutines
	go a.wsReadPump(wsConn)
	go a.wsWritePump(wsConn)
}

func (a *LocalAdapter) wsReadPump(wsConn *wsConnection) {
	defer func() {
		a.wsConnsMu.Lock()
		delete(a.wsConns, wsConn.sessionID)
		a.wsConnsMu.Unlock()
		close(wsConn.done)
		wsConn.conn.Close()
		a.logger.Info("WebSocket connection closed", zap.String("sessionId", wsConn.sessionID))
	}()

	wsConn.conn.SetReadLimit(65536)
	wsConn.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	wsConn.conn.SetPongHandler(func(string) error {
		wsConn.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := wsConn.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				a.logger.Error("WebSocket read error", zap.Error(err))
			}
			return
		}

		// Parse message as MessageRequest
		var req MessageRequest
		if err := json.Unmarshal(message, &req); err != nil {
			a.logger.Warn("Invalid WebSocket message", zap.Error(err))
			continue
		}

		// Use connection's session ID and user ID if not provided
		if req.SessionID == "" {
			req.SessionID = wsConn.sessionID
		}
		if req.UserID == "" {
			req.UserID = wsConn.userID
		}
		if req.Type == "" {
			req.Type = "text"
		}

		// Determine conversation type
		convType := req.ConversationType
		if convType == "" {
			if req.ChannelID != "" {
				convType = "channel"
			} else {
				convType = "direct"
			}
		}

		// Create CIE
		inputType := protocol.InputType(req.Type)
		payload := map[string]interface{}{
			"text":             req.Text,
			"channelId":        req.ChannelID,
			"conversationType": convType,
		}

		// Use channelId as sessionId if provided
		sessionID := req.SessionID
		if req.ChannelID != "" && sessionID == "" {
			sessionID = req.ChannelID
		}

		event := protocol.NewCanonicalInteractionEvent(
			sessionID,
			req.UserID,
			inputType,
			payload,
			*a.capabilities,
			"local-adapter-ws",
		)
		event.Meta.AdapterName = a.name

		a.logger.Debug("Received WebSocket message",
			zap.String("sessionId", sessionID),
			zap.String("channelId", req.ChannelID),
			zap.String("text", req.Text))

		// Emit event to gateway
		if a.eventHandler != nil {
			a.eventHandler(event)
		}
	}
}

func (a *LocalAdapter) wsWritePump(wsConn *wsConnection) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		wsConn.conn.Close()
	}()

	for {
		select {
		case message, ok := <-wsConn.sendCh:
			wsConn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				wsConn.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := wsConn.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				a.logger.Error("WebSocket write error", zap.Error(err))
				return
			}

		case <-ticker.C:
			wsConn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := wsConn.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-wsConn.done:
			return
		}
	}
}

func (a *LocalAdapter) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "healthy",
		"adapter":     a.name,
		"connections": len(a.wsConns),
	})
}

func (a *LocalAdapter) sendErrorResponse(w http.ResponseWriter, code, message, traceID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(MessageResponse{
		Success: false,
		Error:   protocol.NewUIPError(code, message, traceID),
	})
}
