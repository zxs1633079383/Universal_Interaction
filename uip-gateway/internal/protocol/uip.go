// Package protocol defines the Universal Interaction Protocol (UIP) data structures.
// UIP is an IM-agnostic protocol that enables any IM system to interact with Clawdbot.
package protocol

import (
	"time"

	"github.com/google/uuid"
)

// InputType represents the type of input in a Canonical Interaction Event.
type InputType string

const (
	InputTypeText    InputType = "text"
	InputTypeEvent   InputType = "event"
	InputTypeCommand InputType = "command"
)

// ParticipantType represents the type of participant in an interaction.
type ParticipantType string

const (
	ParticipantTypeHuman  ParticipantType = "human"
	ParticipantTypeSystem ParticipantType = "system"
)

// IntentType represents the type of interaction intent.
type IntentType string

const (
	IntentTypeReply  IntentType = "reply"
	IntentTypeAsk    IntentType = "ask"
	IntentTypeNotify IntentType = "notify"
	IntentTypeNoop   IntentType = "noop"
)

// Session represents a UIP virtual session.
type Session struct {
	// ExternalSessionID is the stable session identifier from the IM platform.
	ExternalSessionID string `json:"externalSessionId"`
	// UserID is the IM-native user identifier.
	UserID string `json:"userId"`
	// ParticipantType indicates if this is a human or system participant.
	ParticipantType ParticipantType `json:"participantType"`
}

// Input represents the input payload in a Canonical Interaction Event.
type Input struct {
	// Type is the input type (text, event, command).
	Type InputType `json:"type"`
	// Payload contains the actual input data.
	Payload map[string]interface{} `json:"payload"`
}

// TextPayload is a convenience struct for text input.
type TextPayload struct {
	Text string `json:"text"`
}

// SurfaceCapabilities declares what the IM platform can do.
type SurfaceCapabilities struct {
	// SupportsReply indicates if the platform supports direct replies.
	SupportsReply bool `json:"supportsReply"`
	// SupportsEdit indicates if the platform supports message editing.
	SupportsEdit bool `json:"supportsEdit"`
	// SupportsReaction indicates if the platform supports reactions.
	SupportsReaction bool `json:"supportsReaction"`
	// SupportsThread indicates if the platform supports threading.
	SupportsThread bool `json:"supportsThread"`
	// SupportsAttachment indicates if the platform supports file attachments.
	SupportsAttachment bool `json:"supportsAttachment"`
	// SupportsMarkdown indicates if the platform supports markdown formatting.
	SupportsMarkdown bool `json:"supportsMarkdown"`
}

// EventMeta contains metadata about an interaction event.
type EventMeta struct {
	// Timestamp is the Unix timestamp in milliseconds.
	Timestamp int64 `json:"timestamp"`
	// TraceID is the distributed tracing identifier.
	TraceID string `json:"traceId"`
	// Source identifies the originating gateway/adapter.
	Source string `json:"source"`
	// AdapterName is the name of the IM adapter that received this event.
	AdapterName string `json:"adapterName,omitempty"`
}

// CanonicalInteractionEvent (CIE) is the standard format for all inbound interactions.
// All IM platforms must translate their native events into this format.
type CanonicalInteractionEvent struct {
	// InteractionID is a globally unique, immutable identifier.
	InteractionID string `json:"interactionId"`
	// Session contains session information.
	Session Session `json:"session"`
	// Input contains the interaction input.
	Input Input `json:"input"`
	// Capabilities declares the IM platform's capabilities.
	Capabilities SurfaceCapabilities `json:"capabilities"`
	// Meta contains event metadata.
	Meta EventMeta `json:"meta"`
}

// NewCanonicalInteractionEvent creates a new CIE with generated IDs.
func NewCanonicalInteractionEvent(
	sessionID, userID string,
	inputType InputType,
	payload map[string]interface{},
	capabilities SurfaceCapabilities,
	source string,
) *CanonicalInteractionEvent {
	return &CanonicalInteractionEvent{
		InteractionID: uuid.New().String(),
		Session: Session{
			ExternalSessionID: sessionID,
			UserID:            userID,
			ParticipantType:   ParticipantTypeHuman,
		},
		Input: Input{
			Type:    inputType,
			Payload: payload,
		},
		Capabilities: capabilities,
		Meta: EventMeta{
			Timestamp: time.Now().UnixMilli(),
			TraceID:   uuid.New().String(),
			Source:    source,
		},
	}
}

// IntentContent represents the content of an interaction intent.
type IntentContent struct {
	// Text is the primary text content.
	Text string `json:"text"`
	// Markdown is the markdown-formatted content (if supported).
	Markdown string `json:"markdown,omitempty"`
	// Attachments contains file attachments (if supported).
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment represents a file attachment in an intent.
type Attachment struct {
	// Type is the MIME type of the attachment.
	Type string `json:"type"`
	// URL is the URL to the attachment.
	URL string `json:"url,omitempty"`
	// Data is the base64-encoded attachment data.
	Data string `json:"data,omitempty"`
	// Name is the filename.
	Name string `json:"name,omitempty"`
}

// IntentConstraints defines constraints on how the intent should be executed.
type IntentConstraints struct {
	// RequiresAck indicates if the intent requires acknowledgment.
	RequiresAck bool `json:"requiresAck"`
	// Confidence is the confidence score of the intent (0.0 - 1.0).
	Confidence float64 `json:"confidence"`
	// Priority indicates the intent priority (higher = more urgent).
	Priority int `json:"priority,omitempty"`
	// ExpiresAt is the Unix timestamp when this intent expires.
	ExpiresAt int64 `json:"expiresAt,omitempty"`
}

// InteractionIntent is the response from Clawdbot runtime.
// The UIP Gateway translates these into IM-native actions.
type InteractionIntent struct {
	// IntentID is a unique identifier for this intent.
	IntentID string `json:"intentId"`
	// IntentType is the type of intent (reply, ask, notify, noop).
	IntentType IntentType `json:"intentType"`
	// Content is the intent content.
	Content IntentContent `json:"content"`
	// Constraints defines execution constraints.
	Constraints IntentConstraints `json:"constraints"`
	// TargetSessionID is the session this intent is for.
	TargetSessionID string `json:"targetSessionId"`
	// InReplyTo is the interaction ID this is responding to.
	InReplyTo string `json:"inReplyTo,omitempty"`
}

// NewInteractionIntent creates a new interaction intent.
func NewInteractionIntent(
	intentType IntentType,
	text string,
	targetSessionID string,
	inReplyTo string,
) *InteractionIntent {
	return &InteractionIntent{
		IntentID:   uuid.New().String(),
		IntentType: intentType,
		Content: IntentContent{
			Text: text,
		},
		Constraints: IntentConstraints{
			RequiresAck: false,
			Confidence:  1.0,
		},
		TargetSessionID: targetSessionID,
		InReplyTo:       inReplyTo,
	}
}

// UIPError represents a structured error in the UIP protocol.
type UIPError struct {
	// Code is the error code.
	Code string `json:"code"`
	// Message is the human-readable error message.
	Message string `json:"message"`
	// Details contains additional error details.
	Details map[string]interface{} `json:"details,omitempty"`
	// TraceID is the trace identifier for debugging.
	TraceID string `json:"traceId,omitempty"`
}

// Error codes
const (
	ErrCodeProtocolError = "PROTOCOL_ERROR"
	ErrCodeGatewayError  = "GATEWAY_ERROR"
	ErrCodeRuntimeError  = "RUNTIME_ERROR"
	ErrCodeTimeout       = "TIMEOUT"
	ErrCodeNotFound      = "NOT_FOUND"
)

// NewUIPError creates a new UIP error.
func NewUIPError(code, message string, traceID string) *UIPError {
	return &UIPError{
		Code:    code,
		Message: message,
		TraceID: traceID,
	}
}

func (e *UIPError) Error() string {
	return e.Message
}
