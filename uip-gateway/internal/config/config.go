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
	Server       ServerConfig       `yaml:"server"`
	Clawdbot     ClawdbotConfig     `yaml:"clawdbot"`
	Adapters     AdaptersConfig     `yaml:"adapters"`
	Session      SessionConfig      `yaml:"session"`
	Observability ObservabilityConfig `yaml:"observability"`
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

// ClawdbotConfig holds Clawdbot client configuration.
type ClawdbotConfig struct {
	Endpoint    string           `yaml:"endpoint"`
	Timeout     time.Duration    `yaml:"timeout"`
	RetryPolicy RetryPolicyConfig `yaml:"retry_policy"`
	Insecure    bool             `yaml:"insecure"`
	// Moltbot specific configuration
	Mode        string           `yaml:"mode"`         // "moltbot" or "legacy"
	Token       string           `yaml:"token"`        // Auth token for moltbot universal-im
	EndpointID  string           `yaml:"endpoint_id"`  // Moltbot endpoint ID
	CallbackURL string           `yaml:"callback_url"` // Our callback URL for moltbot to post responses
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
	Local   LocalAdapterConfig  `yaml:"local"`
	Slack   SlackAdapterConfig  `yaml:"slack"`
	WeChat  WeChatAdapterConfig `yaml:"wechat"`
}

// LocalAdapterConfig holds local adapter configuration.
type LocalAdapterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	HTTPPath string `yaml:"http_path"`
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
			Endpoint: "http://localhost:18789",  // Moltbot default port
			Timeout:  30 * time.Second,
			RetryPolicy: RetryPolicyConfig{
				MaxRetries:      3,
				Backoff:         "exponential",
				InitialInterval: 100 * time.Millisecond,
				MaxInterval:     5 * time.Second,
			},
			Insecure:    true,
			Mode:        "moltbot",                           // Use moltbot universal-im
			Token:       "uip-gateway-token",                 // Token for auth
			EndpointID:  "uip-gateway",                       // Endpoint ID in moltbot config
			CallbackURL: "http://localhost:8080/api/v1/callback", // Our callback URL
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
