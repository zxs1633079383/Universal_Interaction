// Package config handles configuration loading and validation.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration structure.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Clawdbot      ClawdbotConfig      `yaml:"clawdbot"`
	Adapters      AdaptersConfig      `yaml:"adapters"`
	Session       SessionConfig       `yaml:"session"`
	Observability ObservabilityConfig `yaml:"observability"`
	// IMWebhook is the configuration for notifying external IM systems
	IMWebhook IMWebhookConfig `yaml:"im_webhook"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	HTTPPort        int           `yaml:"http_port"`
	GRPCPort        int           `yaml:"grpc_port"`
	WebSocketPath   string        `yaml:"websocket_path"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// ClawdbotConfig holds OpenClaw client configuration.
type ClawdbotConfig struct {
	Endpoint    string            `yaml:"endpoint"`
	Timeout     time.Duration     `yaml:"timeout"`
	RetryPolicy RetryPolicyConfig `yaml:"retry_policy"`
	Insecure    bool              `yaml:"insecure"`
	// OpenClaw Universal IM configuration
	Mode        string `yaml:"mode"`         // "openclaw" (universal-im) or "legacy"
	Token       string `yaml:"token"`        // Legacy: Auth token
	EndpointID  string `yaml:"endpoint_id"`  // Legacy: Endpoint ID
	CallbackURL string `yaml:"callback_url"` // Our outbound URL for OpenClaw to post AI responses

	// Universal IM specific configuration
	UniversalIM UniversalIMConfig `yaml:"universal_im"`
}

// UniversalIMConfig holds OpenClaw Universal IM specific configuration.
type UniversalIMConfig struct {
	// AccountID is the account identifier in OpenClaw config (default: "default")
	AccountID string `yaml:"account_id"`
	// WebhookPath is the custom webhook path (default: "/universal-im/{account_id}/webhook")
	WebhookPath string `yaml:"webhook_path"`
	// Secret is the webhook secret for X-Webhook-Secret header authentication
	Secret string `yaml:"secret"`
	// Transport is the transport type: "webhook", "websocket", or "polling"
	Transport string `yaml:"transport"`
	// WebSocket configuration (for transport: websocket)
	WebSocket WebSocketConfig `yaml:"websocket"`
	// Polling configuration (for transport: polling)
	Polling PollingConfig `yaml:"polling"`
	// OutboundURL is where OpenClaw will POST AI responses (same as callback_url)
	OutboundURL string `yaml:"outbound_url"`
	// OutboundAuthHeader is the Authorization header value for outbound requests
	OutboundAuthHeader string `yaml:"outbound_auth_header"`
}

// WebSocketConfig holds WebSocket transport configuration.
type WebSocketConfig struct {
	// URL is the WebSocket server URL to connect to
	URL string `yaml:"url"`
	// ReconnectMs is the reconnection interval in milliseconds
	ReconnectMs int `yaml:"reconnect_ms"`
}

// PollingConfig holds Polling transport configuration.
type PollingConfig struct {
	// URL is the HTTP endpoint to poll for messages
	URL string `yaml:"url"`
	// IntervalMs is the polling interval in milliseconds
	IntervalMs int `yaml:"interval_ms"`
}

// RetryPolicyConfig holds retry policy configuration.
type RetryPolicyConfig struct {
	MaxRetries      int           `yaml:"max_retries"`
	Backoff         string        `yaml:"backoff"`
	InitialInterval time.Duration `yaml:"initial_interval"`
	MaxInterval     time.Duration `yaml:"max_interval"`
}

// AdaptersConfig holds adapter configurations.
type AdaptersConfig struct {
	Local  LocalAdapterConfig  `yaml:"local"`
	Slack  SlackAdapterConfig  `yaml:"slack"`
	WeChat WeChatAdapterConfig `yaml:"wechat"`
}

// LocalAdapterConfig holds local adapter configuration.
type LocalAdapterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	HTTPPath string `yaml:"http_path"`
}

// IMWebhookConfig holds the configuration for notifying external IM systems.
type IMWebhookConfig struct {
	// Enabled enables webhook notifications to external IM
	Enabled bool `yaml:"enabled"`
	// URL is the webhook URL of your external IM system
	URL string `yaml:"url"`
	// AuthHeader is the Authorization header value (e.g., "Bearer xxx")
	AuthHeader string `yaml:"auth_header"`
	// Timeout is the request timeout
	Timeout time.Duration `yaml:"timeout"`
	// RetryCount is the number of retry attempts
	RetryCount int `yaml:"retry_count"`
}

// SlackAdapterConfig holds Slack adapter configuration.
type SlackAdapterConfig struct {
	Enabled     bool   `yaml:"enabled"`
	WebhookPath string `yaml:"webhook_path"`
}

// WeChatAdapterConfig holds WeChat adapter configuration.
type WeChatAdapterConfig struct {
	Enabled     bool   `yaml:"enabled"`
	WebhookPath string `yaml:"webhook_path"`
}

// SessionConfig holds session management configuration.
type SessionConfig struct {
	TTL             time.Duration `yaml:"ttl"`
	MaxSessions     int           `yaml:"max_sessions"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

// ObservabilityConfig holds observability configuration.
type ObservabilityConfig struct {
	Tracing     bool   `yaml:"tracing"`
	MetricsPort int    `yaml:"metrics_port"`
	LogLevel    string `yaml:"log_level"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPPort:        8080,
			GRPCPort:        9090,
			WebSocketPath:   "/ws",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		Clawdbot: ClawdbotConfig{
			Endpoint: "http://localhost:18789", // OpenClaw gateway default port
			Timeout:  30 * time.Second,
			RetryPolicy: RetryPolicyConfig{
				MaxRetries:      3,
				Backoff:         "exponential",
				InitialInterval: 100 * time.Millisecond,
				MaxInterval:     5 * time.Second,
			},
			Insecure:    true,
			Mode:        "openclaw",                                       // Use OpenClaw universal-im
			CallbackURL: "http://localhost:8080/api/v1/openclaw/outbound", // Our outbound URL
			UniversalIM: UniversalIMConfig{
				AccountID:          "default",
				Transport:          "webhook", // Default to webhook transport
				Secret:             "",        // Webhook secret (X-Webhook-Secret)
				OutboundURL:        "http://localhost:8080/api/v1/openclaw/outbound",
				OutboundAuthHeader: "", // Optional auth header for outbound
				WebSocket: WebSocketConfig{
					ReconnectMs: 5000,
				},
				Polling: PollingConfig{
					IntervalMs: 5000,
				},
			},
		},
		Adapters: AdaptersConfig{
			Local: LocalAdapterConfig{
				Enabled:  true,
				HTTPPath: "/api/v1/local",
			},
			Slack: SlackAdapterConfig{
				Enabled: false,
			},
			WeChat: WeChatAdapterConfig{
				Enabled: false,
			},
		},
		Session: SessionConfig{
			TTL:             24 * time.Hour,
			MaxSessions:     10000,
			CleanupInterval: 5 * time.Minute,
		},
		Observability: ObservabilityConfig{
			Tracing:     true,
			MetricsPort: 9091,
			LogLevel:    "info",
		},
		IMWebhook: IMWebhookConfig{
			Enabled:    false, // Disabled by default
			URL:        "",    // Must be configured by user
			AuthHeader: "",
			Timeout:    10 * time.Second,
			RetryCount: 3,
		},
	}
}

// Load loads configuration from a YAML file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Use defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Server.HTTPPort <= 0 || c.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid HTTP port: %d", c.Server.HTTPPort)
	}

	if c.Clawdbot.Endpoint == "" {
		return fmt.Errorf("clawdbot endpoint is required")
	}

	if c.Clawdbot.Timeout <= 0 {
		return fmt.Errorf("clawdbot timeout must be positive")
	}

	return nil
}
