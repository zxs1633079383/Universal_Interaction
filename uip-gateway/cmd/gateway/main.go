// Package main is the entry point for UIP Gateway.
// UIP Gateway is a high-performance, IM-agnostic interaction gateway for Clawdbot.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/zlc_ai/uip-gateway/internal/adapter/local"
	"github.com/zlc_ai/uip-gateway/internal/clawdbot"
	"github.com/zlc_ai/uip-gateway/internal/config"
	"github.com/zlc_ai/uip-gateway/internal/gateway"
	"github.com/zlc_ai/uip-gateway/internal/transport"
)

var (
	version   = "0.1.0"
	buildTime = "unknown"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	useMock := flag.Bool("mock", false, "Use mock Clawdbot client (for testing)")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("UIP Gateway v%s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger := initLogger(cfg.Observability.LogLevel)
	defer logger.Sync()

	logger.Info("Starting UIP Gateway",
		zap.String("version", version),
		zap.String("config", *configPath))

	// Create OpenClaw client
	var clawdbotClient clawdbot.Client
	var openclawClient *clawdbot.OpenclawClient

	if *useMock {
		logger.Info("Using mock OpenClaw client")
		clawdbotClient = clawdbot.NewMockClient(logger)
	} else if cfg.Clawdbot.Mode == "openclaw" || cfg.Clawdbot.Mode == "moltbot" {
		// Support both "openclaw" (new) and "moltbot" (legacy) mode names
		logger.Info("Using OpenClaw universal-im client",
			zap.String("endpoint", cfg.Clawdbot.Endpoint),
			zap.String("accountId", cfg.Clawdbot.UniversalIM.AccountID),
			zap.String("transport", cfg.Clawdbot.UniversalIM.Transport))
		openclawClient, err = clawdbot.NewOpenclawClient(clawdbot.Config{
			Endpoint:   cfg.Clawdbot.Endpoint,
			Timeout:    cfg.Clawdbot.Timeout,
			MaxRetries: cfg.Clawdbot.RetryPolicy.MaxRetries,
			Insecure:   cfg.Clawdbot.Insecure,
		}, clawdbot.OpenclawClientConfig{
			Secret:      cfg.Clawdbot.UniversalIM.Secret,
			AccountID:   cfg.Clawdbot.UniversalIM.AccountID,
			WebhookPath: cfg.Clawdbot.UniversalIM.WebhookPath,
		}, logger)
		if err != nil {
			logger.Fatal("Failed to create OpenClaw client", zap.Error(err))
		}
		clawdbotClient = openclawClient
	} else {
		clawdbotClient, err = clawdbot.NewHTTPClient(clawdbot.Config{
			Endpoint:   cfg.Clawdbot.Endpoint,
			Timeout:    cfg.Clawdbot.Timeout,
			MaxRetries: cfg.Clawdbot.RetryPolicy.MaxRetries,
			Insecure:   cfg.Clawdbot.Insecure,
		}, logger)
		if err != nil {
			logger.Fatal("Failed to create OpenClaw client", zap.Error(err))
		}
	}

	// Create gateway
	gw := gateway.New(gateway.Config{
		WorkerCount: 10,
		QueueSize:   1000,
		SessionTTL:  cfg.Session.TTL,
	}, clawdbotClient, logger)

	// Register local adapter if enabled
	if cfg.Adapters.Local.Enabled {
		localAdapter, err := local.NewLocalAdapter(map[string]interface{}{
			"http_path": cfg.Adapters.Local.HTTPPath,
		})
		if err != nil {
			logger.Fatal("Failed to create local adapter", zap.Error(err))
		}
		if err := gw.RegisterAdapter(localAdapter); err != nil {
			logger.Fatal("Failed to register local adapter", zap.Error(err))
		}
	}

	// Start gateway
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := gw.Start(ctx); err != nil {
		logger.Fatal("Failed to start gateway", zap.Error(err))
	}

	// Create HTTP server
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy","version":"` + version + `"}`))
	})

	// Local adapter endpoints
	if cfg.Adapters.Local.Enabled {
		if adapter, ok := gw.GetAdapter("local"); ok {
			if localAdapter, ok := adapter.(*local.LocalAdapter); ok {
				mux.Handle(cfg.Adapters.Local.HTTPPath+"/",
					http.StripPrefix(cfg.Adapters.Local.HTTPPath, localAdapter.HTTPHandler()))
				logger.Info("Local adapter HTTP endpoints registered",
					zap.String("path", cfg.Adapters.Local.HTTPPath))
			}
		}
	}

	// OpenClaw outbound endpoint - receives AI responses from OpenClaw Universal IM
	// This endpoint handles the outbound payload from OpenClaw when AI generates a response
	mux.HandleFunc("/api/v1/openclaw/outbound", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Validate authorization header if configured
		if cfg.Clawdbot.UniversalIM.OutboundAuthHeader != "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != cfg.Clawdbot.UniversalIM.OutboundAuthHeader {
				logger.Warn("Invalid authorization header in outbound request")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("Failed to read outbound body", zap.Error(err))
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		var outbound clawdbot.OpenclawOutboundPayload
		if err := json.Unmarshal(body, &outbound); err != nil {
			logger.Error("Failed to parse outbound payload", zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		logger.Info("Received OpenClaw outbound",
			zap.String("to", outbound.To),
			zap.Int("textLen", len(outbound.Text)),
			zap.String("replyToId", outbound.ReplyToId),
			zap.String("text", outbound.Text))

		// Forward to OpenClaw client and get routing information
		var outboundResp *clawdbot.OutboundResponse
		if openclawClient != nil {
			outboundResp = openclawClient.HandleCallback(&outbound)
		}

		// Build response with routing information for external IM
		response := map[string]interface{}{
			"ok":        true,
			"messageId": fmt.Sprintf("outbound-%d", time.Now().UnixMilli()),
			"to":        outbound.To,
			"text":      outbound.Text,
		}

		// Include routing information if available
		if outboundResp != nil {
			response["routing"] = map[string]interface{}{
				"channelId": outboundResp.ChannelID,
				"userId":    outboundResp.UserID,
				"sessionId": outboundResp.SessionID,
			}
			logger.Info("Outbound with routing info",
				zap.String("channelId", outboundResp.ChannelID),
				zap.String("userId", outboundResp.UserID),
				zap.String("sessionId", outboundResp.SessionID))
		}

		if outbound.MediaUrl != "" {
			response["mediaUrl"] = outbound.MediaUrl
		}
		if outbound.ThreadId != "" {
			response["threadId"] = outbound.ThreadId
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Legacy callback endpoint for backward compatibility
	mux.HandleFunc("/api/v1/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("Failed to read callback body", zap.Error(err))
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Try to parse as new OpenClaw format first
		var outbound clawdbot.OpenclawOutboundPayload
		if err := json.Unmarshal(body, &outbound); err != nil {
			logger.Error("Failed to parse callback", zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		logger.Info("Received callback (legacy endpoint)",
			zap.String("to", outbound.To),
			zap.Int("textLen", len(outbound.Text)))

		// Forward to OpenClaw client if available
		if openclawClient != nil {
			openclawClient.HandleCallback(&outbound)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	})

	// Initialize transport servers for OpenClaw connectivity
	var wsServer *transport.WebSocketServer
	var pollingServer *transport.PollingServer

	// WebSocket server for OpenClaw WebSocket transport
	wsServer = transport.NewWebSocketServer(logger)
	if err := wsServer.Start(ctx); err != nil {
		logger.Fatal("Failed to start WebSocket server", zap.Error(err))
	}

	// Polling server for OpenClaw Polling transport
	pollingServer = transport.NewPollingServer(logger)
	if err := pollingServer.Start(ctx); err != nil {
		logger.Fatal("Failed to start Polling server", zap.Error(err))
	}

	// WebSocket endpoint for OpenClaw to connect to (for websocket transport)
	mux.Handle("/api/v1/openclaw/ws", wsServer.HTTPHandler())
	logger.Info("WebSocket endpoint registered", zap.String("path", "/api/v1/openclaw/ws"))

	// Polling endpoint for OpenClaw to poll messages (for polling transport)
	mux.Handle("/api/v1/openclaw/poll", pollingServer.HTTPHandler())
	logger.Info("Polling endpoint registered", zap.String("path", "/api/v1/openclaw/poll"))

	// Inbound message endpoint for polling transport
	mux.Handle("/api/v1/openclaw/inbound", pollingServer.InboundHandler())
	logger.Info("Inbound endpoint registered", zap.String("path", "/api/v1/openclaw/inbound"))

	// API info endpoint
	mux.HandleFunc("/api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":     "UIP Gateway",
			"version":  version,
			"protocol": "UIP v1.0",
			"mode":     cfg.Clawdbot.Mode,
			"openclaw": map[string]interface{}{
				"endpoint":  cfg.Clawdbot.Endpoint,
				"accountId": cfg.Clawdbot.UniversalIM.AccountID,
				"transport": cfg.Clawdbot.UniversalIM.Transport,
			},
			"endpoints": map[string]string{
				"local_message":     cfg.Adapters.Local.HTTPPath + "/message",
				"local_ws":          cfg.Adapters.Local.HTTPPath + "/ws",
				"openclaw_outbound": "/api/v1/openclaw/outbound",
				"openclaw_ws":       "/api/v1/openclaw/ws",
				"openclaw_poll":     "/api/v1/openclaw/poll",
				"openclaw_inbound":  "/api/v1/openclaw/inbound",
				"callback_legacy":   "/api/v1/callback",
				"health":            "/health",
			},
			"transports": map[string]interface{}{
				"websocket": map[string]interface{}{
					"connections": wsServer.ConnectionCount(),
				},
				"polling": map[string]interface{}{
					"queueSize": pollingServer.QueueSize(),
				},
			},
		})
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.HTTPPort),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Start HTTP server
	go func() {
		logger.Info("HTTP server starting",
			zap.Int("port", cfg.Server.HTTPPort))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("HTTP server error", zap.Error(err))
		}
	}()

	// Print startup banner
	printBanner(cfg, logger)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutdown signal received")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", zap.Error(err))
	}

	// Stop transport servers
	if wsServer != nil {
		if err := wsServer.Stop(shutdownCtx); err != nil {
			logger.Error("WebSocket server shutdown error", zap.Error(err))
		}
	}
	if pollingServer != nil {
		if err := pollingServer.Stop(shutdownCtx); err != nil {
			logger.Error("Polling server shutdown error", zap.Error(err))
		}
	}

	// Stop gateway
	if err := gw.Stop(shutdownCtx); err != nil {
		logger.Error("Gateway shutdown error", zap.Error(err))
	}

	logger.Info("UIP Gateway stopped")
}

func initLogger(level string) *zap.Logger {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	config := zap.Config{
		Level:       zap.NewAtomicLevelAt(zapLevel),
		Development: false,
		Encoding:    "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			FunctionKey:    zapcore.OmitKey,
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, _ := config.Build()
	return logger
}

func printBanner(cfg *config.Config, logger *zap.Logger) {
	banner := `
╔══════════════════════════════════════════════════════════════════╗
║                     UIP Gateway v%s                           ║
║         Universal Interaction Protocol Gateway                   ║
╠══════════════════════════════════════════════════════════════════╣
║  HTTP Server:    http://localhost:%-5d                          ║
║  Local Adapter:  %-20s                          ║
║  OpenClaw:       %-20s                          ║
║  Account ID:     %-20s                          ║
║  Transport:      %-20s                          ║
╠══════════════════════════════════════════════════════════════════╣
║  Endpoints:                                                      ║
║    POST %-20s  - Send message                  ║
║    WS   %-20s  - WebSocket connection          ║
║    POST /api/v1/openclaw/outbound   - OpenClaw AI response       ║
║    GET  /health                     - Health check               ║
╚══════════════════════════════════════════════════════════════════╝
`
	fmt.Printf(banner,
		version,
		cfg.Server.HTTPPort,
		padRight(cfg.Adapters.Local.HTTPPath, 20),
		padRight(cfg.Clawdbot.Endpoint, 20),
		padRight(cfg.Clawdbot.UniversalIM.AccountID, 20),
		padRight(cfg.Clawdbot.UniversalIM.Transport, 20),
		padRight(cfg.Adapters.Local.HTTPPath+"/message", 20),
		padRight(cfg.Adapters.Local.HTTPPath+"/ws", 20),
	)
}

func padRight(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + string(make([]byte, length-len(s)))
}
