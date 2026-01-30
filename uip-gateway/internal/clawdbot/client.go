// Package clawdbot provides a client for communicating with Clawdbot runtime.
// This client translates UIP events into Clawdbot-native requests and responses.
package clawdbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/zlc_ai/uip-gateway/internal/protocol"
)

// Client is the interface for Clawdbot communication.
type Client interface {
	// ProcessEvent sends a CIE to Clawdbot and returns the interaction intent.
	ProcessEvent(ctx context.Context, event *protocol.CanonicalInteractionEvent) (*protocol.InteractionIntent, error)

	// Close closes the client connection.
	Close() error

	// Health checks if Clawdbot is reachable.
	Health(ctx context.Context) error
}

// Config holds the configuration for the Clawdbot client.
type Config struct {
	// Endpoint is the Clawdbot server address.
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Timeout is the request timeout.
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `json:"max_retries" yaml:"max_retries"`
	// Insecure allows insecure connections (for local dev).
	Insecure bool `json:"insecure" yaml:"insecure"`
}

// DefaultConfig returns the default Clawdbot client configuration.
func DefaultConfig() Config {
	return Config{
		Endpoint:   "http://localhost:50051",
		Timeout:    30 * time.Second,
		MaxRetries: 3,
		Insecure:   true,
	}
}

// HTTPClient implements the Client interface using HTTP.
type HTTPClient struct {
	config     Config
	httpClient *http.Client
	logger     *zap.Logger
	mu         sync.RWMutex
	closed     bool
}

// MoltbotUniversalIMRequest is the request format for Moltbot universal-im webhook.
// This follows the universal-im plugin's expected format.
type MoltbotUniversalIMRequest struct {
	// MessageID is a unique identifier for the message.
	MessageID string `json:"message_id"`
	// Text is the message content.
	Text string `json:"text"`
	// From contains sender information.
	From MoltbotFrom `json:"from"`
	// Chat contains conversation information.
	Chat MoltbotChat `json:"chat"`
}

// MoltbotFrom represents the sender.
type MoltbotFrom struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// MoltbotChat represents the chat/conversation.
type MoltbotChat struct {
	ID    string `json:"id"`
	Type  string `json:"type"` // "private" or "group"
	Title string `json:"title,omitempty"`
}

// MoltbotWebhookResponse is the immediate response from the webhook.
type MoltbotWebhookResponse struct {
	OK        bool   `json:"ok"`
	MessageID string `json:"messageId,omitempty"`
	Replied   bool   `json:"replied,omitempty"`
	Elapsed   int64  `json:"elapsed,omitempty"`
	Error     string `json:"error,omitempty"`
}

// MoltbotCallbackRequest is the callback format from Moltbot to our endpoint.
// This is what Moltbot sends to our callbackUrl.
type MoltbotCallbackRequest struct {
	// ChatID is the target chat.
	ChatID string `json:"chat_id"`
	// Text is the response text.
	Text string `json:"text"`
	// ReplyToMessageID is the original message being replied to.
	ReplyToMessageID string `json:"reply_to_message_id,omitempty"`
	// MediaURL is optional media attachment.
	MediaURL string `json:"media_url,omitempty"`
}

// Legacy format support
type ClawdbotRequest struct {
	SessionID string                 `json:"sessionId"`
	UserID    string                 `json:"userId"`
	Message   string                 `json:"message"`
	Type      string                 `json:"type"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type ClawdbotResponse struct {
	Response  string                 `json:"response"`
	Type      string                 `json:"type,omitempty"`
	SessionID string                 `json:"sessionId,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Error     *ErrorInfo             `json:"error,omitempty"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewHTTPClient creates a new HTTP-based Clawdbot client.
func NewHTTPClient(config Config, logger *zap.Logger) (*HTTPClient, error) {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	return &HTTPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		logger: logger,
	}, nil
}

func (c *HTTPClient) ProcessEvent(ctx context.Context, event *protocol.CanonicalInteractionEvent) (*protocol.InteractionIntent, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.RUnlock()

	// Extract text from payload
	text := ""
	if payload := event.Input.Payload; payload != nil {
		if t, ok := payload["text"].(string); ok {
			text = t
		}
	}

	// Build request
	req := ClawdbotRequest{
		SessionID: event.Session.ExternalSessionID,
		UserID:    event.Session.UserID,
		Message:   text,
		Type:      string(event.Input.Type),
		Metadata: map[string]interface{}{
			"interactionId": event.InteractionID,
			"traceId":       event.Meta.TraceID,
			"timestamp":     event.Meta.Timestamp,
			"capabilities":  event.Capabilities,
		},
	}

	// Execute with retry
	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		intent, err := c.doRequest(ctx, req, event)
		if err == nil {
			return intent, nil
		}

		lastErr = err
		c.logger.Warn("Clawdbot request failed, retrying",
			zap.Int("attempt", attempt+1),
			zap.Error(err))
	}

	return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
}

func (c *HTTPClient) doRequest(ctx context.Context, req ClawdbotRequest, event *protocol.CanonicalInteractionEvent) (*protocol.InteractionIntent, error) {
	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/api/v1/chat", c.config.Endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Trace-ID", event.Meta.TraceID)
	httpReq.Header.Set("X-Session-ID", event.Session.ExternalSessionID)

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error: %d - %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var clawdbotResp ClawdbotResponse
	if err := json.Unmarshal(respBody, &clawdbotResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for error in response
	if clawdbotResp.Error != nil {
		return nil, fmt.Errorf("clawdbot error: %s - %s", clawdbotResp.Error.Code, clawdbotResp.Error.Message)
	}

	// Convert to InteractionIntent
	intentType := protocol.IntentTypeReply
	if clawdbotResp.Type == "ask" {
		intentType = protocol.IntentTypeAsk
	} else if clawdbotResp.Type == "notify" {
		intentType = protocol.IntentTypeNotify
	}

	intent := protocol.NewInteractionIntent(
		intentType,
		clawdbotResp.Response,
		event.Session.ExternalSessionID,
		event.InteractionID,
	)

	c.logger.Debug("Received Clawdbot response",
		zap.String("intentId", intent.IntentID),
		zap.String("sessionId", event.Session.ExternalSessionID))

	return intent, nil
}

func (c *HTTPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	c.httpClient.CloseIdleConnections()
	return nil
}

func (c *HTTPClient) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", c.config.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}

	return nil
}

// MoltbotClient implements the Client interface for Moltbot's universal-im plugin.
// This client sends messages to the /universal-im/webhook endpoint.
type MoltbotClient struct {
	config     Config
	httpClient *http.Client
	logger     *zap.Logger
	token      string
	endpointID string
	mu         sync.RWMutex
	closed     bool

	// Pending responses - key is chat_id
	pendingMu sync.RWMutex
	pending   map[string]chan *protocol.InteractionIntent
}

// NewMoltbotClient creates a new Moltbot universal-im client.
func NewMoltbotClient(config Config, token string, endpointID string, logger *zap.Logger) (*MoltbotClient, error) {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	return &MoltbotClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		logger:     logger,
		token:      token,
		endpointID: endpointID,
		pending:    make(map[string]chan *protocol.InteractionIntent),
	}, nil
}

func (c *MoltbotClient) ProcessEvent(ctx context.Context, event *protocol.CanonicalInteractionEvent) (*protocol.InteractionIntent, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.RUnlock()

	// Extract text from payload
	text := ""
	if payload := event.Input.Payload; payload != nil {
		if t, ok := payload["text"].(string); ok {
			text = t
		}
	}

	// Build Moltbot universal-im request
	req := MoltbotUniversalIMRequest{
		MessageID: event.InteractionID,
		Text:      text,
		From: MoltbotFrom{
			ID:   event.Session.UserID,
			Name: event.Session.UserID,
		},
		Chat: MoltbotChat{
			ID:   event.Session.ExternalSessionID,
			Type: "private",
		},
	}

	// Create pending response channel
	respCh := make(chan *protocol.InteractionIntent, 1)
	c.pendingMu.Lock()
	c.pending[event.Session.ExternalSessionID] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, event.Session.ExternalSessionID)
		c.pendingMu.Unlock()
	}()

	// Execute with retry
	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		err := c.sendToMoltbot(ctx, req, event)
		if err == nil {
			break
		}

		lastErr = err
		c.logger.Warn("Moltbot request failed, retrying",
			zap.Int("attempt", attempt+1),
			zap.Error(err))
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
	}

	// Wait for callback response with timeout
	select {
	case intent := <-respCh:
		return intent, nil
	case <-time.After(c.config.Timeout):
		// If we timeout waiting for callback, return a timeout error
		// The callback may still arrive later
		c.logger.Warn("Timeout waiting for Moltbot callback",
			zap.String("sessionId", event.Session.ExternalSessionID))
		return nil, fmt.Errorf("timeout waiting for Moltbot response")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *MoltbotClient) sendToMoltbot(ctx context.Context, req MoltbotUniversalIMRequest, event *protocol.CanonicalInteractionEvent) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL - use universal-im webhook endpoint
	// Note: Don't use path parameter - Moltbot identifies endpoint by token
	url := fmt.Sprintf("%s/universal-im/webhook", c.config.Endpoint)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("X-Trace-ID", event.Meta.TraceID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server error: %d - %s", resp.StatusCode, string(respBody))
	}

	var webhookResp MoltbotWebhookResponse
	if err := json.Unmarshal(respBody, &webhookResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !webhookResp.OK {
		return fmt.Errorf("moltbot error: %s", webhookResp.Error)
	}

	c.logger.Debug("Message sent to Moltbot",
		zap.String("messageId", req.MessageID),
		zap.Bool("replied", webhookResp.Replied))

	return nil
}

// HandleCallback processes the callback from Moltbot.
// This should be called when Moltbot posts to our callback URL.
func (c *MoltbotClient) HandleCallback(callback *MoltbotCallbackRequest) {
	c.pendingMu.RLock()
	respCh, exists := c.pending[callback.ChatID]
	c.pendingMu.RUnlock()

	if !exists {
		c.logger.Warn("Received callback for unknown session",
			zap.String("chatId", callback.ChatID))
		return
	}

	intent := protocol.NewInteractionIntent(
		protocol.IntentTypeReply,
		callback.Text,
		callback.ChatID,
		callback.ReplyToMessageID,
	)

	select {
	case respCh <- intent:
		c.logger.Debug("Callback processed",
			zap.String("chatId", callback.ChatID))
	default:
		c.logger.Warn("Callback channel full",
			zap.String("chatId", callback.ChatID))
	}
}

func (c *MoltbotClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	c.httpClient.CloseIdleConnections()
	return nil
}

func (c *MoltbotClient) Health(ctx context.Context) error {
	// Check moltbot universal-im health endpoint
	url := fmt.Sprintf("%s/universal-im/health", c.config.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}

	return nil
}

// MockClient is a mock implementation for testing and development.
type MockClient struct {
	logger   *zap.Logger
	delay    time.Duration
	response string
}

// NewMockClient creates a mock Clawdbot client for testing.
func NewMockClient(logger *zap.Logger) *MockClient {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	return &MockClient{
		logger:   logger,
		delay:    100 * time.Millisecond,
		response: "Hello! I'm Clawdbot. I received your message: ",
	}
}

func (c *MockClient) ProcessEvent(ctx context.Context, event *protocol.CanonicalInteractionEvent) (*protocol.InteractionIntent, error) {
	// Simulate processing delay
	select {
	case <-time.After(c.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Extract text from payload
	text := ""
	if payload := event.Input.Payload; payload != nil {
		if t, ok := payload["text"].(string); ok {
			text = t
		}
	}

	// Generate mock response
	response := c.response + text

	intent := protocol.NewInteractionIntent(
		protocol.IntentTypeReply,
		response,
		event.Session.ExternalSessionID,
		event.InteractionID,
	)

	c.logger.Debug("Mock Clawdbot response",
		zap.String("intentId", intent.IntentID),
		zap.String("response", response))

	return intent, nil
}

func (c *MockClient) Close() error {
	return nil
}

func (c *MockClient) Health(ctx context.Context) error {
	return nil
}

// SetResponse sets the mock response prefix.
func (c *MockClient) SetResponse(response string) {
	c.response = response
}

// SetDelay sets the simulated processing delay.
func (c *MockClient) SetDelay(delay time.Duration) {
	c.delay = delay
}
