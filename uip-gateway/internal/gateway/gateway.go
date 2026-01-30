// Package gateway implements the core UIP Gateway logic.
// It coordinates between IM adapters and Clawdbot runtime.
package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/zlc_ai/uip-gateway/internal/adapter"
	"github.com/zlc_ai/uip-gateway/internal/clawdbot"
	"github.com/zlc_ai/uip-gateway/internal/protocol"
)

// Gateway is the core UIP Gateway that coordinates interactions.
type Gateway struct {
	adapters       map[string]adapter.IMAdapter
	clawdbot       clawdbot.Client
	logger         *zap.Logger
	
	// Session management
	sessions       *SessionRegistry
	
	// Event processing
	eventQueue     chan *eventContext
	workerCount    int
	
	// State
	started        bool
	mu             sync.RWMutex
	wg             sync.WaitGroup
	stopCh         chan struct{}
}

// eventContext wraps an event with its processing context.
type eventContext struct {
	event       *protocol.CanonicalInteractionEvent
	adapterName string
	receivedAt  time.Time
}

// Config holds the Gateway configuration.
type Config struct {
	// WorkerCount is the number of event processing workers.
	WorkerCount int `json:"worker_count" yaml:"worker_count"`
	// QueueSize is the event queue buffer size.
	QueueSize int `json:"queue_size" yaml:"queue_size"`
	// SessionTTL is the session time-to-live.
	SessionTTL time.Duration `json:"session_ttl" yaml:"session_ttl"`
}

// DefaultConfig returns the default Gateway configuration.
func DefaultConfig() Config {
	return Config{
		WorkerCount: 10,
		QueueSize:   1000,
		SessionTTL:  24 * time.Hour,
	}
}

// New creates a new UIP Gateway.
func New(cfg Config, clawdbotClient clawdbot.Client, logger *zap.Logger) *Gateway {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	
	return &Gateway{
		adapters:    make(map[string]adapter.IMAdapter),
		clawdbot:    clawdbotClient,
		logger:      logger,
		sessions:    NewSessionRegistry(cfg.SessionTTL),
		eventQueue:  make(chan *eventContext, cfg.QueueSize),
		workerCount: cfg.WorkerCount,
		stopCh:      make(chan struct{}),
	}
}

// RegisterAdapter adds an IM adapter to the gateway.
func (g *Gateway) RegisterAdapter(a adapter.IMAdapter) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if g.started {
		return fmt.Errorf("cannot register adapter after gateway started")
	}
	
	name := a.Name()
	if _, exists := g.adapters[name]; exists {
		return fmt.Errorf("adapter %s already registered", name)
	}
	
	// Set up event handler
	a.OnEvent(func(event *protocol.CanonicalInteractionEvent) {
		g.handleEvent(event, name)
	})
	
	g.adapters[name] = a
	g.logger.Info("Adapter registered", zap.String("adapter", name))
	return nil
}

// Start starts the gateway and all registered adapters.
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return fmt.Errorf("gateway already started")
	}
	g.started = true
	g.mu.Unlock()
	
	g.logger.Info("Starting UIP Gateway",
		zap.Int("workers", g.workerCount),
		zap.Int("adapters", len(g.adapters)))
	
	// Start event processing workers
	for i := 0; i < g.workerCount; i++ {
		g.wg.Add(1)
		go g.eventWorker(i)
	}
	
	// Start session cleanup
	g.wg.Add(1)
	go g.sessionCleanup()
	
	// Start all adapters
	for name, a := range g.adapters {
		if err := a.Start(ctx); err != nil {
			g.logger.Error("Failed to start adapter",
				zap.String("adapter", name),
				zap.Error(err))
			return fmt.Errorf("failed to start adapter %s: %w", name, err)
		}
	}
	
	g.logger.Info("UIP Gateway started successfully")
	return nil
}

// Stop gracefully shuts down the gateway.
func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.Lock()
	if !g.started {
		g.mu.Unlock()
		return nil
	}
	g.started = false
	g.mu.Unlock()
	
	g.logger.Info("Stopping UIP Gateway")
	
	// Signal workers to stop
	close(g.stopCh)
	
	// Stop all adapters
	for name, a := range g.adapters {
		if err := a.Stop(ctx); err != nil {
			g.logger.Error("Failed to stop adapter",
				zap.String("adapter", name),
				zap.Error(err))
		}
	}
	
	// Close event queue
	close(g.eventQueue)
	
	// Wait for workers to finish
	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		g.logger.Info("UIP Gateway stopped gracefully")
	case <-ctx.Done():
		g.logger.Warn("UIP Gateway shutdown timed out")
	}
	
	// Close Clawdbot client
	if err := g.clawdbot.Close(); err != nil {
		g.logger.Error("Failed to close Clawdbot client", zap.Error(err))
	}
	
	return nil
}

// handleEvent is called by adapters when they receive an event.
func (g *Gateway) handleEvent(event *protocol.CanonicalInteractionEvent, adapterName string) {
	ctx := &eventContext{
		event:       event,
		adapterName: adapterName,
		receivedAt:  time.Now(),
	}
	
	select {
	case g.eventQueue <- ctx:
		g.logger.Debug("Event queued",
			zap.String("interactionId", event.InteractionID),
			zap.String("adapter", adapterName))
	default:
		g.logger.Warn("Event queue full, dropping event",
			zap.String("interactionId", event.InteractionID))
	}
}

// eventWorker processes events from the queue.
func (g *Gateway) eventWorker(id int) {
	defer g.wg.Done()
	
	g.logger.Debug("Event worker started", zap.Int("workerId", id))
	
	for {
		select {
		case ctx, ok := <-g.eventQueue:
			if !ok {
				g.logger.Debug("Event worker stopping", zap.Int("workerId", id))
				return
			}
			g.processEvent(ctx)
			
		case <-g.stopCh:
			g.logger.Debug("Event worker received stop signal", zap.Int("workerId", id))
			return
		}
	}
}

// processEvent handles a single interaction event.
func (g *Gateway) processEvent(ctx *eventContext) {
	event := ctx.event
	
	// Log processing start
	g.logger.Info("Processing event",
		zap.String("interactionId", event.InteractionID),
		zap.String("sessionId", event.Session.ExternalSessionID),
		zap.String("adapter", ctx.adapterName),
		zap.Duration("queueTime", time.Since(ctx.receivedAt)))
	
	// Update session
	g.sessions.Touch(event.Session.ExternalSessionID, event.Session)
	
	// Create processing context with timeout
	processCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Send to Clawdbot
	intent, err := g.clawdbot.ProcessEvent(processCtx, event)
	if err != nil {
		g.logger.Error("Clawdbot processing failed",
			zap.String("interactionId", event.InteractionID),
			zap.Error(err))
		
		// Create error intent
		intent = protocol.NewInteractionIntent(
			protocol.IntentTypeReply,
			"Sorry, I encountered an error processing your request. Please try again.",
			event.Session.ExternalSessionID,
			event.InteractionID,
		)
	}
	
	// Apply capability-based degradation
	g.applyDegradation(event, intent)
	
	// Route intent back to adapter
	g.mu.RLock()
	adapter, exists := g.adapters[ctx.adapterName]
	g.mu.RUnlock()
	
	if !exists {
		g.logger.Error("Adapter not found for response",
			zap.String("adapter", ctx.adapterName))
		return
	}
	
	// Send intent
	if err := adapter.SendIntent(processCtx, intent); err != nil {
		g.logger.Error("Failed to send intent",
			zap.String("intentId", intent.IntentID),
			zap.String("adapter", ctx.adapterName),
			zap.Error(err))
		return
	}
	
	g.logger.Info("Event processed successfully",
		zap.String("interactionId", event.InteractionID),
		zap.String("intentId", intent.IntentID),
		zap.Duration("totalTime", time.Since(ctx.receivedAt)))
}

// applyDegradation modifies the intent based on IM capabilities.
func (g *Gateway) applyDegradation(event *protocol.CanonicalInteractionEvent, intent *protocol.InteractionIntent) {
	caps := event.Capabilities
	
	// If markdown not supported, strip markdown
	if !caps.SupportsMarkdown {
		intent.Content.Markdown = ""
	}
	
	// If attachments not supported, remove them
	if !caps.SupportsAttachment {
		intent.Content.Attachments = nil
	}
	
	// If edit not supported and this is an edit intent, convert to reply
	if !caps.SupportsEdit && intent.IntentType == protocol.IntentTypeNotify {
		// For now, we just keep as reply
	}
}

// sessionCleanup periodically cleans up expired sessions.
func (g *Gateway) sessionCleanup() {
	defer g.wg.Done()
	
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			count := g.sessions.Cleanup()
			if count > 0 {
				g.logger.Info("Cleaned up expired sessions", zap.Int("count", count))
			}
			
		case <-g.stopCh:
			return
		}
	}
}

// GetAdapter returns an adapter by name.
func (g *Gateway) GetAdapter(name string) (adapter.IMAdapter, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	a, ok := g.adapters[name]
	return a, ok
}

// SessionRegistry manages active sessions.
type SessionRegistry struct {
	sessions map[string]*sessionEntry
	ttl      time.Duration
	mu       sync.RWMutex
}

type sessionEntry struct {
	session   protocol.Session
	createdAt time.Time
	lastSeen  time.Time
}

// NewSessionRegistry creates a new session registry.
func NewSessionRegistry(ttl time.Duration) *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[string]*sessionEntry),
		ttl:      ttl,
	}
}

// Touch updates the last seen time for a session.
func (r *SessionRegistry) Touch(id string, session protocol.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	now := time.Now()
	if entry, exists := r.sessions[id]; exists {
		entry.lastSeen = now
		entry.session = session
	} else {
		r.sessions[id] = &sessionEntry{
			session:   session,
			createdAt: now,
			lastSeen:  now,
		}
	}
}

// Get retrieves a session by ID.
func (r *SessionRegistry) Get(id string) (protocol.Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if entry, exists := r.sessions[id]; exists {
		return entry.session, true
	}
	return protocol.Session{}, false
}

// Cleanup removes expired sessions and returns the count.
func (r *SessionRegistry) Cleanup() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	count := 0
	cutoff := time.Now().Add(-r.ttl)
	
	for id, entry := range r.sessions {
		if entry.lastSeen.Before(cutoff) {
			delete(r.sessions, id)
			count++
		}
	}
	
	return count
}

// Count returns the number of active sessions.
func (r *SessionRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}
