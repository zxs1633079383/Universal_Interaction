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

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/zlc_ai/uip-gateway/internal/adapter/local"
	"github.com/zlc_ai/uip-gateway/internal/clawdbot"
	"github.com/zlc_ai/uip-gateway/internal/config"
	"github.com/zlc_ai/uip-gateway/internal/gateway"
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

	// Create Clawdbot client
	var clawdbotClient clawdbot.Client
	var moltbotClient *clawdbot.MoltbotClient
	
	if *useMock {
		logger.Info("Using mock Clawdbot client")
		clawdbotClient = clawdbot.NewMockClient(logger)
	} else if cfg.Clawdbot.Mode == "moltbot" {
		logger.Info("Using Moltbot universal-im client",
			zap.String("endpoint", cfg.Clawdbot.Endpoint),
			zap.String("endpointId", cfg.Clawdbot.EndpointID))
		moltbotClient, err = clawdbot.NewMoltbotClient(clawdbot.Config{
			Endpoint:   cfg.Clawdbot.Endpoint,
			Timeout:    cfg.Clawdbot.Timeout,
			MaxRetries: cfg.Clawdbot.RetryPolicy.MaxRetries,
			Insecure:   cfg.Clawdbot.Insecure,
		}, cfg.Clawdbot.Token, cfg.Clawdbot.EndpointID, logger)
		if err != nil {
			logger.Fatal("Failed to create Moltbot client", zap.Error(err))
		}
		clawdbotClient = moltbotClient
	} else {
		clawdbotClient, err = clawdbot.NewHTTPClient(clawdbot.Config{
			Endpoint:   cfg.Clawdbot.Endpoint,
			Timeout:    cfg.Clawdbot.Timeout,
			MaxRetries: cfg.Clawdbot.RetryPolicy.MaxRetries,
			Insecure:   cfg.Clawdbot.Insecure,
		}, logger)
		if err != nil {
			logger.Fatal("Failed to create Clawdbot client", zap.Error(err))
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

	// Moltbot callback endpoint - receives responses from Moltbot
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
		
		var callback clawdbot.MoltbotCallbackRequest
		if err := json.Unmarshal(body, &callback); err != nil {
			logger.Error("Failed to parse callback", zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		
		logger.Info("Received Moltbot callback",
			zap.String("chatId", callback.ChatID),
			zap.Int("textLen", len(callback.Text)))
		
		// Forward to Moltbot client if available
		if moltbotClient != nil {
			moltbotClient.HandleCallback(&callback)
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	})

	// API info endpoint
	mux.HandleFunc("/api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fmt.Sprintf(`{
			"name": "UIP Gateway",
			"version": "%s",
			"protocol": "UIP v1.0",
			"mode": "%s",
			"endpoints": {
				"local_message": "%s/message",
				"local_ws": "%s/ws",
				"callback": "/api/v1/callback",
				"health": "/health"
			}
		}`, version, cfg.Clawdbot.Mode, cfg.Adapters.Local.HTTPPath, cfg.Adapters.Local.HTTPPath)))
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
╔══════════════════════════════════════════════════════════════╗
║                     UIP Gateway v%s                       ║
║         Universal Interaction Protocol Gateway               ║
╠══════════════════════════════════════════════════════════════╣
║  HTTP Server:    http://localhost:%d                       ║
║  Local Adapter:  %s                                 ║
║  Moltbot:        %s                      ║
║  Mode:           %s                                      ║
╠══════════════════════════════════════════════════════════════╣
║  Endpoints:                                                  ║
║    POST %s/message    - Send message            ║
║    WS   %s/ws         - WebSocket connection    ║
║    POST /api/v1/callback           - Moltbot callback        ║
║    GET  /health                    - Health check            ║
╚══════════════════════════════════════════════════════════════╝
`
	fmt.Printf(banner, 
		version, 
		cfg.Server.HTTPPort,
		padRight(cfg.Adapters.Local.HTTPPath, 20),
		padRight(cfg.Clawdbot.Endpoint, 20),
		padRight(cfg.Clawdbot.Mode, 15),
		padRight(cfg.Adapters.Local.HTTPPath, 12),
		padRight(cfg.Adapters.Local.HTTPPath, 12),
	)
}

func padRight(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + string(make([]byte, length-len(s)))
}
