// Package adapter defines the interface for IM platform adapters.
// Each IM platform (Slack, WeChat, local, etc.) implements this interface.
package adapter

import (
	"context"

	"github.com/zlc_ai/uip-gateway/internal/protocol"
)

// EventHandler is a callback function for handling inbound CIE events.
type EventHandler func(event *protocol.CanonicalInteractionEvent)

// IMAdapter is the interface that all IM platform adapters must implement.
// This interface enables UIP Gateway to be completely IM-agnostic.
type IMAdapter interface {
	// Name returns the unique identifier for this adapter.
	// This is used in logging, metrics, and configuration.
	Name() string

	// Start initializes and starts the adapter.
	// The adapter should begin listening for events after this call.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the adapter.
	// The adapter should stop accepting new events and complete pending operations.
	Stop(ctx context.Context) error

	// OnEvent registers a callback handler for inbound CIE events.
	// The gateway calls this to receive events from the IM platform.
	OnEvent(handler EventHandler)

	// SendIntent delivers an interaction intent to the IM platform.
	// The adapter translates the intent into IM-native actions.
	SendIntent(ctx context.Context, intent *protocol.InteractionIntent) error

	// Capabilities returns the capabilities of this IM platform.
	// Used for capability negotiation and graceful degradation.
	Capabilities() *protocol.SurfaceCapabilities
}

// AdapterFactory creates an adapter instance from configuration.
type AdapterFactory func(config map[string]interface{}) (IMAdapter, error)

// Registry holds all registered adapter factories.
var Registry = make(map[string]AdapterFactory)

// RegisterAdapter registers an adapter factory with the given name.
func RegisterAdapter(name string, factory AdapterFactory) {
	Registry[name] = factory
}

// GetAdapter retrieves an adapter factory by name.
func GetAdapter(name string) (AdapterFactory, bool) {
	factory, ok := Registry[name]
	return factory, ok
}
