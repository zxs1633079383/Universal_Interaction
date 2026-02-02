// Package clawdbot provides a client for communicating with OpenClaw (formerly Clawdbot) runtime.
// This client translates UIP events into OpenClaw Universal IM requests and responses.
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

// Client is the interface for OpenClaw communication.
type Client interface {
	// ProcessEvent sends a CIE to OpenClaw and returns the interaction intent.
	ProcessEvent(ctx context.Context, event *protocol.CanonicalInteractionEvent) (*protocol.InteractionIntent, error)

	// Close closes the client connection.
	Close() error

	// Health checks if OpenClaw is reachable.
	Health(ctx context.Context) error
}

// Config holds the configuration for the OpenClaw client.
type Config struct {
	// Endpoint is the OpenClaw gateway server address.
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// Timeout is the request timeout.
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `json:"max_retries" yaml:"max_retries"`
	// Insecure allows insecure connections (for local dev).
	Insecure bool `json:"insecure" yaml:"insecure"`
}

// DefaultConfig returns the default OpenClaw client configuration.
func DefaultConfig() Config {
	return Config{
		Endpoint:   "http://localhost:18789",
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

// OpenclawUniversalIMRequest is the request format for OpenClaw universal-im webhook.
// This follows the universal-im plugin's Custom Provider expected format.
type OpenclawUniversalIMRequest struct {
	// MessageID is a unique identifier for the message.
	MessageID string `json:"messageId,omitempty"`
	// Timestamp is Unix timestamp in milliseconds.
	Timestamp int64 `json:"timestamp,omitempty"`
	// Sender contains sender information.
	Sender OpenclawSender `json:"sender"`
	// Conversation contains conversation information.
	Conversation OpenclawConversation `json:"conversation"`
	// Text is the message content.
	Text string `json:"text,omitempty"`
	// Attachments is an optional array of attachments.
	Attachments []OpenclawAttachment `json:"attachments,omitempty"`
	// Mentions is an optional array of mentioned user IDs.
	Mentions []string `json:"mentions,omitempty"`
	// Meta is optional provider-specific metadata.
	Meta map[string]interface{} `json:"meta,omitempty"`
}

// OpenclawSender represents the sender.
type OpenclawSender struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	IsBot    bool   `json:"isBot,omitempty"`
}

// OpenclawConversation represents the conversation context.
type OpenclawConversation struct {
	Type     string `json:"type"` // "direct", "group", or "channel"
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	TeamID   string `json:"teamId,omitempty"`
}

// OpenclawAttachment represents an attachment.
type OpenclawAttachment struct {
	Kind        string `json:"kind"` // "image", "audio", "video", "document", "unknown"
	URL         string `json:"url,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

// OpenclawWebhookResponse is the immediate response from the webhook.
type OpenclawWebhookResponse struct {
	OK        bool   `json:"ok"`
	MessageID string `json:"messageId,omitempty"`
	Error     string `json:"error,omitempty"`
}

// OpenclawOutboundPayload is the format OpenClaw sends to our outbound URL.
// This is what OpenClaw posts to our callback endpoint when AI responds.
type OpenclawOutboundPayload struct {
	// To is the target in format "user:userId" or "channel:channelId"
	To string `json:"to"`
	// Text is the AI response text.
	Text string `json:"text"`
	// MediaUrl is optional media attachment URL.
	MediaUrl string `json:"mediaUrl,omitempty"`
	// ReplyToId is the original message ID being replied to.
	ReplyToId string `json:"replyToId,omitempty"`
	// ThreadId is the thread ID for threaded conversations.
	ThreadId string `json:"threadId,omitempty"`
}

// Legacy type aliases for backward compatibility
type MoltbotCallbackRequest = OpenclawOutboundPayload

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

// PendingContext stores context for pending requests
type PendingContext struct {
	ResponseCh chan *protocol.InteractionIntent
	ChannelID  string // External IM channel ID for routing
	UserID     string // Original user ID
	SessionID  string // Original session ID
}

// OpenclawClient implements the Client interface for OpenClaw's universal-im plugin.
// This client sends messages to the /universal-im/{accountId}/webhook endpoint.
type OpenclawClient struct {
	config      Config
	httpClient  *http.Client
	logger      *zap.Logger
	secret      string // Webhook secret for authentication
	accountID   string // Account ID in OpenClaw config (default: "default")
	webhookPath string // Custom webhook path (optional)
	mu          sync.RWMutex
	closed      bool

	// Pending responses - key is conversation_id (for sync mode)
	pendingMu sync.RWMutex
	pending   map[string]*PendingContext

	// Session context store - key is sessionId (for async webhook mode)
	// This stores routing info for longer periods to handle async callbacks
	sessionCtxMu sync.RWMutex
	sessionCtx   map[string]*PendingContext

	// Outbound callback function for external IM routing
	outboundCallback OutboundCallback
}

// OutboundCallback is called when AI response is received for routing to external IM
type OutboundCallback func(response *OutboundResponse)

// OutboundResponse contains the AI response with routing information
type OutboundResponse struct {
	To        string `json:"to"`        // Target in format "user:userId" or "channel:channelId"
	Text      string `json:"text"`      // AI response text
	MediaUrl  string `json:"mediaUrl"`  // Optional media attachment
	ReplyToId string `json:"replyToId"` // Original message ID
	ThreadId  string `json:"threadId"`  // Thread ID for threaded conversations
	ChannelID string `json:"channelId"` // External IM channel ID for routing
	UserID    string `json:"userId"`    // Original user ID
	SessionID string `json:"sessionId"` // Original session ID
}

// OpenclawClientConfig holds additional configuration for OpenclawClient
type OpenclawClientConfig struct {
	Secret      string // Webhook secret (X-Webhook-Secret header)
	AccountID   string // Account ID (default: "default")
	WebhookPath string // Custom webhook path (default: "/universal-im/{accountId}/webhook")
}

// NewOpenclawClient creates a new OpenClaw universal-im client.
func NewOpenclawClient(config Config, opts OpenclawClientConfig, logger *zap.Logger) (*OpenclawClient, error) {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	accountID := opts.AccountID
	if accountID == "" {
		accountID = "default"
	}

	return &OpenclawClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		logger:      logger,
		secret:      opts.Secret,
		accountID:   accountID,
		webhookPath: opts.WebhookPath,
		pending:     make(map[string]*PendingContext),
		sessionCtx:  make(map[string]*PendingContext),
	}, nil
}

// SetOutboundCallback sets the callback for routing AI responses to external IM
func (c *OpenclawClient) SetOutboundCallback(callback OutboundCallback) {
	c.outboundCallback = callback
}

// ClearSessionContext clears a specific session context (call after processing outbound)
func (c *OpenclawClient) ClearSessionContext(sessionID string) {
	c.sessionCtxMu.Lock()
	defer c.sessionCtxMu.Unlock()

	if ctx, exists := c.sessionCtx[sessionID]; exists {
		// Also remove by userId if it points to the same context
		for key, val := range c.sessionCtx {
			if val == ctx && key != sessionID {
				delete(c.sessionCtx, key)
			}
		}
		delete(c.sessionCtx, sessionID)
	}
}

// GetSessionContext returns the routing context for a session
func (c *OpenclawClient) GetSessionContext(sessionID string) *PendingContext {
	c.sessionCtxMu.RLock()
	defer c.sessionCtxMu.RUnlock()
	return c.sessionCtx[sessionID]
}

// NewMoltbotClient is a legacy alias for NewOpenclawClient
func NewMoltbotClient(config Config, token string, endpointID string, logger *zap.Logger) (*OpenclawClient, error) {
	return NewOpenclawClient(config, OpenclawClientConfig{
		Secret:    token,
		AccountID: endpointID,
	}, logger)
}

func (c *OpenclawClient) ProcessEvent(ctx context.Context, event *protocol.CanonicalInteractionEvent) (*protocol.InteractionIntent, error) {
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

	// Extract attachments if any
	var attachments []OpenclawAttachment
	if payload := event.Input.Payload; payload != nil {
		if atts, ok := payload["attachments"].([]interface{}); ok {
			for _, att := range atts {
				if attMap, ok := att.(map[string]interface{}); ok {
					attachment := OpenclawAttachment{
						Kind: getString(attMap, "kind", "unknown"),
						URL:  getString(attMap, "url", ""),
					}
					attachments = append(attachments, attachment)
				}
			}
		}
	}

	// Determine conversation type from input payload
	convType := "direct"
	if payload := event.Input.Payload; payload != nil {
		if ct, ok := payload["conversationType"].(string); ok {
			convType = ct
		}
	}

	// Extract channelId from payload if provided
	channelID := ""
	if payload := event.Input.Payload; payload != nil {
		if cid, ok := payload["channelId"].(string); ok {
			channelID = cid
		}
	}

	// Build OpenClaw universal-im request (Custom Provider format)
	req := OpenclawUniversalIMRequest{
		MessageID: event.InteractionID,
		Timestamp: time.Now().UnixMilli(),
		Sender: OpenclawSender{
			ID:   event.Session.UserID,
			Name: event.Session.UserID, // Can be enhanced with actual name
		},
		Conversation: OpenclawConversation{
			Type: convType,
			ID:   event.Session.ExternalSessionID,
		},
		Text:        text,
		Attachments: attachments,
		Meta: map[string]interface{}{
			"traceId":      event.Meta.TraceID,
			"capabilities": event.Capabilities,
			"channelId":    channelID, // Include channelId in meta for tracking
		},
	}

	// Create pending response context with channelId for routing
	conversationKey := event.Session.ExternalSessionID
	pendingCtx := &PendingContext{
		ResponseCh: make(chan *protocol.InteractionIntent, 1),
		ChannelID:  channelID,
		UserID:     event.Session.UserID,
		SessionID:  event.Session.ExternalSessionID,
	}
	c.pendingMu.Lock()
	c.pending[conversationKey] = pendingCtx
	c.pendingMu.Unlock()

	// Also store in sessionCtx for async webhook mode (keyed by both sessionId and userId)
	// This allows outbound callbacks to find routing info even after sync timeout
	c.sessionCtxMu.Lock()
	c.sessionCtx[conversationKey] = pendingCtx
	c.sessionCtx[event.Session.UserID] = pendingCtx // Also key by userId for "user:xxx" format
	c.sessionCtxMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, conversationKey)
		c.pendingMu.Unlock()
		// Note: We don't delete from sessionCtx immediately - let it persist for async callbacks
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

		err := c.sendToOpenclaw(ctx, req, event)
		if err == nil {
			break
		}

		lastErr = err
		c.logger.Warn("OpenClaw request failed, retrying",
			zap.Int("attempt", attempt+1),
			zap.Error(err))
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
	}

	// Chat Completions API is synchronous, so the response should already be in the channel
	select {
	case intent := <-pendingCtx.ResponseCh:
		return intent, nil
	case <-time.After(100 * time.Millisecond):
		// If no response in channel, something went wrong
		c.logger.Warn("No response received from OpenClaw",
			zap.String("conversationId", conversationKey))
		return nil, fmt.Errorf("no response from OpenClaw")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ChatCompletionsRequest is the OpenAI-compatible request format.
type ChatCompletionsRequest struct {
	Model    string                   `json:"model"`
	Messages []ChatCompletionsMessage `json:"messages"`
	Stream   bool                     `json:"stream,omitempty"`
}

// ChatCompletionsMessage is a single message in the chat.
type ChatCompletionsMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionsResponse is the OpenAI-compatible response format.
type ChatCompletionsResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (c *OpenclawClient) sendToOpenclaw(ctx context.Context, req OpenclawUniversalIMRequest, event *protocol.CanonicalInteractionEvent) error {
	// Try webhook first (for test-server or properly configured OpenClaw)
	err := c.sendViaWebhook(ctx, req, event)
	if err != nil {
		c.logger.Debug("Webhook failed, trying Chat Completions API",
			zap.Error(err))
		// Fallback to Chat Completions API
		return c.sendViaChatCompletions(ctx, req, event)
	}
	return nil
}

// sendViaWebhook sends message via Universal IM webhook endpoint
func (c *OpenclawClient) sendViaWebhook(ctx context.Context, req OpenclawUniversalIMRequest, event *protocol.CanonicalInteractionEvent) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Use universal-im webhook endpoint
	// Format: /universal-im/{accountId}/webhook (accountId defaults to "default")
	var url string
	if c.webhookPath != "" {
		url = fmt.Sprintf("%s%s", c.config.Endpoint, c.webhookPath)
	} else {
		// Default path with accountId
		url = fmt.Sprintf("%s/universal-im/%s/webhook", c.config.Endpoint, c.accountID)
	}

	c.logger.Debug("Sending webhook request",
		zap.String("url", url),
		zap.String("accountId", c.accountID),
		zap.Int("bodyLen", len(body)))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		httpReq.Header.Set("X-Webhook-Secret", c.secret)
	}
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

	var webhookResp OpenclawWebhookResponse
	if err := json.Unmarshal(respBody, &webhookResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !webhookResp.OK {
		return fmt.Errorf("webhook error: %s", webhookResp.Error)
	}

	c.logger.Info("Message sent via webhook",
		zap.String("messageId", req.MessageID),
		zap.String("endpoint", url))

	// Webhook mode: response comes via outbound callback, deliver a placeholder
	c.deliverResponse(event.Session.ExternalSessionID,
		"消息已发送到 OpenClaw，等待 AI 响应...", req.MessageID)

	return nil
}

// sendViaChatCompletions sends message via OpenAI-compatible Chat Completions API
func (c *OpenclawClient) sendViaChatCompletions(ctx context.Context, req OpenclawUniversalIMRequest, event *protocol.CanonicalInteractionEvent) error {
	chatReq := ChatCompletionsRequest{
		Model: "default",
		Messages: []ChatCompletionsMessage{
			{
				Role:    "user",
				Content: req.Text,
			},
		},
		Stream: false,
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/chat/completions", c.config.Endpoint)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.secret)
	}
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

	var chatResp ChatCompletionsResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if chatResp.Error != nil {
		return fmt.Errorf("chat completions error: %s", chatResp.Error.Message)
	}

	// Extract the response text
	if len(chatResp.Choices) > 0 {
		responseText := chatResp.Choices[0].Message.Content
		c.logger.Info("Received AI response via Chat Completions",
			zap.String("messageId", req.MessageID),
			zap.Int("responseLen", len(responseText)))

		c.deliverResponse(event.Session.ExternalSessionID, responseText, req.MessageID)
	}

	return nil
}

// deliverResponse delivers the AI response to the pending channel.
func (c *OpenclawClient) deliverResponse(conversationID, text, replyToID string) {
	c.pendingMu.RLock()
	pendingCtx, exists := c.pending[conversationID]
	c.pendingMu.RUnlock()

	if !exists {
		c.logger.Warn("No pending channel for response",
			zap.String("conversationId", conversationID))
		return
	}

	intent := protocol.NewInteractionIntent(
		protocol.IntentTypeReply,
		text,
		conversationID,
		replyToID,
	)

	select {
	case pendingCtx.ResponseCh <- intent:
		c.logger.Debug("Response delivered",
			zap.String("conversationId", conversationID))
	default:
		c.logger.Warn("Response channel full",
			zap.String("conversationId", conversationID))
	}
}

// HandleCallback processes the callback from OpenClaw.
// This should be called when OpenClaw posts to our outbound URL.
// Returns the OutboundResponse with routing information for external IM.
func (c *OpenclawClient) HandleCallback(callback *OpenclawOutboundPayload) *OutboundResponse {
	// Parse the "to" field to extract conversation ID
	// Format: "user:userId" or "channel:channelId" or "group:groupId"
	conversationID := callback.To
	toType := ""
	if len(callback.To) > 0 {
		// Extract the ID part after the colon
		for i := 0; i < len(callback.To); i++ {
			if callback.To[i] == ':' {
				toType = callback.To[:i]
				conversationID = callback.To[i+1:]
				break
			}
		}
	}

	// Build outbound response with routing information
	outboundResp := &OutboundResponse{
		To:        callback.To,
		Text:      callback.Text,
		MediaUrl:  callback.MediaUrl,
		ReplyToId: callback.ReplyToId,
		ThreadId:  callback.ThreadId,
	}

	// Try to find pending context (sync mode)
	c.pendingMu.RLock()
	pendingCtx, exists := c.pending[conversationID]
	c.pendingMu.RUnlock()

	// If not found in pending, try sessionCtx (async webhook mode)
	if !exists {
		c.sessionCtxMu.RLock()
		pendingCtx, exists = c.sessionCtx[conversationID]
		c.sessionCtxMu.RUnlock()
	}

	if exists {
		// Fill in routing information from context
		outboundResp.ChannelID = pendingCtx.ChannelID
		outboundResp.UserID = pendingCtx.UserID
		outboundResp.SessionID = pendingCtx.SessionID

		intent := protocol.NewInteractionIntent(
			protocol.IntentTypeReply,
			callback.Text,
			conversationID,
			callback.ReplyToId,
		)

		// Add media URL as attachment if present
		if callback.MediaUrl != "" {
			intent.Content.Attachments = append(intent.Content.Attachments, protocol.Attachment{
				Type: "media",
				URL:  callback.MediaUrl,
			})
		}

		select {
		case pendingCtx.ResponseCh <- intent:
			c.logger.Debug("Callback processed",
				zap.String("conversationId", conversationID),
				zap.String("channelId", pendingCtx.ChannelID))
		default:
			// Channel might be full or closed (webhook async mode)
			c.logger.Debug("Callback response channel not available (async mode)",
				zap.String("conversationId", conversationID))
		}

		c.logger.Info("Outbound callback with routing",
			zap.String("to", callback.To),
			zap.String("toType", toType),
			zap.String("conversationId", conversationID),
			zap.String("channelId", pendingCtx.ChannelID),
			zap.String("userId", pendingCtx.UserID),
			zap.String("sessionId", pendingCtx.SessionID))
	} else {
		c.logger.Warn("Received callback for unknown conversation (no routing info)",
			zap.String("to", callback.To),
			zap.String("conversationId", conversationID))
	}

	// Call the outbound callback if set
	if c.outboundCallback != nil {
		c.outboundCallback(outboundResp)
	}

	return outboundResp
}

func (c *OpenclawClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	c.httpClient.CloseIdleConnections()
	return nil
}

func (c *OpenclawClient) Health(ctx context.Context) error {
	// Check OpenClaw health endpoint
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

// Helper function to get string from map
func getString(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

// MoltbotClient is a legacy type alias
type MoltbotClient = OpenclawClient

// MockClient is a mock implementation for testing and development.
type MockClient struct {
	logger   *zap.Logger
	delay    time.Duration
	response string
}

// NewMockClient creates a mock OpenClaw client for testing.
func NewMockClient(logger *zap.Logger) *MockClient {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	return &MockClient{
		logger:   logger,
		delay:    100 * time.Millisecond,
		response: "Hello! I'm OpenClaw. I received your message: ",
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
