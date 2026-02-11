// Package imwebhook provides webhook notification to external IM systems.
package imwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zlc_ai/uip-gateway/internal/clawdbot"
)

// Config holds the IM webhook notifier configuration.
type Config struct {
	// URL is the webhook URL of your external IM system
	URL string
	// AuthHeader is the Authorization header value
	AuthHeader string
	// Timeout is the request timeout
	Timeout time.Duration
	// RetryCount is the number of retry attempts
	RetryCount int
}

// Notifier sends AI responses to external IM systems via webhook.
type Notifier struct {
	config     Config
	httpClient *http.Client
	logger     *zap.Logger
}

// OutboundMessage is the message format sent to external IM webhook.
type OutboundMessage struct {
	// MessageID is a unique identifier for this message
	MessageID string `json:"messageId"`
	// Timestamp is Unix timestamp in milliseconds
	Timestamp int64 `json:"timestamp"`
	// To is the target in format "user:userId" or "channel:channelId"
	To string `json:"to"`
	// Text is the AI response text
	Text string `json:"text"`
	// MediaUrl is optional media attachment URL
	MediaUrl string `json:"mediaUrl,omitempty"`
	// ReplyToId is the original message ID being replied to
	ReplyToId string `json:"replyToId,omitempty"`
	// ThreadId is the thread ID for threaded conversations
	ThreadId string `json:"threadId,omitempty"`
	// Routing contains information for routing to specific IM channel/user
	Routing RoutingInfo `json:"routing"`
}

// RoutingInfo contains routing information for external IM.
type RoutingInfo struct {
	// ChannelID is the external IM channel/group ID
	ChannelID string `json:"channelId,omitempty"`
	// UserID is the original sender's user ID
	UserID string `json:"userId,omitempty"`
	// SessionID is the conversation session ID
	SessionID string `json:"sessionId,omitempty"`
}

// NewNotifier creates a new IM webhook notifier.
func NewNotifier(config Config, logger *zap.Logger) *Notifier {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.RetryCount == 0 {
		config.RetryCount = 3
	}

	return &Notifier{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		logger: logger,
	}
}

// Notify sends the AI response to the external IM system.
func (n *Notifier) Notify(ctx context.Context, response *clawdbot.OutboundResponse) error {
	if n.config.URL == "" {
		return fmt.Errorf("IM webhook URL not configured")
	}

	msg := OutboundMessage{
		MessageID: fmt.Sprintf("ai-resp-%d", time.Now().UnixMilli()),
		Timestamp: time.Now().UnixMilli(),
		To:        response.To,
		Text:      response.Text,
		MediaUrl:  response.MediaUrl,
		ReplyToId: response.ReplyToId,
		ThreadId:  response.ThreadId,
		Routing: RoutingInfo{
			ChannelID: response.ChannelID,
			UserID:    response.UserID,
			SessionID: response.SessionID,
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Retry logic
	var lastErr error
	for attempt := 0; attempt <= n.config.RetryCount; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
			n.logger.Debug("Retrying IM webhook",
				zap.Int("attempt", attempt+1),
				zap.Error(lastErr))
		}

		err := n.doNotify(ctx, body)
		if err == nil {
			n.logger.Info("AI response sent to IM webhook",
				zap.String("channelId", response.ChannelID),
				zap.String("userId", response.UserID),
				zap.Int("textLen", len(response.Text)))
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("all retries exhausted: %w", lastErr)
}

func (n *Notifier) doNotify(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if n.config.AuthHeader != "" {
		req.Header.Set("Authorization", n.config.AuthHeader)
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("IM webhook error: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}
