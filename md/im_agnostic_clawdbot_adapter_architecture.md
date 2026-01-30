# RFC-0001: Universal Interaction Protocol (UIP)

**Category**: Standards Track  
**Status**: Draft  
**Author**: Clawdbot Architecture Group  
**Target Platform**: Clawdbot Runtime  
**Last Updated**: 2026-01

---

## 1. Abstract

This document defines the **Universal Interaction Protocol (UIP)**, a canonical, IM-agnostic interaction protocol that enables *any* Instant Messaging (IM) system to interact seamlessly with **Clawdbot**, without requiring awareness of agents, LLMs, prompts, or tools.

UIP standardizes **interaction semantics**, not intelligence semantics. It establishes a strict separation between **human interaction surfaces** (IM platforms) and **decision-making runtimes** (Clawdbot).

---

## 2. Motivation

Existing bot and agent integrations suffer from:

- Tight coupling between IM SDKs and agent logic
- IM-specific behavior leakage into runtime cores
- Non-replayable, non-auditable interaction flows
- Exponential integration cost as IM count grows

UIP addresses these issues by introducing a **single interaction contract** that:

- Abstracts all IM systems into a uniform interaction model
- Prevents IM platforms from directly interacting with agents
- Enables governance, replay, and deterministic execution

---

## 3. Design Principles

### 3.1 Interaction First

UIP models *interaction*, not conversation, intent, or intelligence.

### 3.2 Agent Opacity

IM systems MUST NOT:
- Know what an agent is
- Access prompts, tools, or reasoning traces

### 3.3 Deterministic Replayability

All UIP events MUST be serializable, replayable, and traceable.

### 3.4 Capability Awareness, Not Assumption

UIP explicitly models surface capabilities and enforces graceful degradation.

---

## 4. System Roles

### 4.1 IM Platform

A third-party system providing human interaction surfaces (e.g. Slack, WhatsApp, WeChat).

Responsibilities:
- Emit native interaction events
- Execute outbound interaction commands

### 4.2 UIP Gateway

A stateless or semi-stateful component translating IM-native events into UIP-compliant events.

Responsibilities:
- Event normalization
- Idempotency enforcement
- Capability declaration

### 4.3 Clawdbot Runtime

A deterministic interaction decision system.

Responsibilities:
- Consume UIP events
- Produce interaction intents

---

## 5. Canonical Interaction Event

All inbound interactions MUST be represented as a **Canonical Interaction Event (CIE)**.

```json
{
  "interactionId": "uuid",
  "session": {
    "externalSessionId": "string",
    "userId": "string",
    "participantType": "human"
  },
  "input": {
    "type": "text | event | command",
    "payload": {}
  },
  "capabilities": {
    "supportsReply": true,
    "supportsEdit": false,
    "supportsReaction": true
  },
  "meta": {
    "timestamp": 0,
    "traceId": "string",
    "source": "im-gateway"
  }
}
```

### 5.1 Required Fields

- `interactionId`: Globally unique, immutable
- `externalSessionId`: Stable session identifier
- `userId`: IM-native user identifier

---

## 6. Session Model

UIP defines a **Virtual Session**, decoupled from IM-native constructs.

- Channels, threads, DMs map to Virtual Sessions
- Session lifecycle is governed by Clawdbot

Session guarantees:
- Ordered interaction stream
- Deterministic replay

---

## 7. Outbound Interaction Intent

Clawdbot MUST respond with **Interaction Intents**, not IM commands.

```json
{
  "intentId": "uuid",
  "intentType": "reply | ask | notify | noop",
  "content": {
    "text": "string"
  },
  "constraints": {
    "requiresAck": false,
    "confidence": 0.0
  }
}
```

The UIP Gateway is responsible for translating intents into IM-native actions.

---

## 8. Capability Negotiation

Each interaction event MUST include declared IM capabilities.

Clawdbot MUST:
- Respect declared capabilities
- Apply downgrade policies when unsupported

Example:
- If `supportsEdit = false`, edit intents MUST degrade to replies

---

## 9. Error Handling

Errors are classified as:

- **Protocol Errors**: Invalid UIP structure
- **Gateway Errors**: Translation or delivery failure
- **Runtime Errors**: Decision failure

All errors MUST be emitted as structured events and traceable.

---

## 10. Security Considerations

- UIP events MUST be authenticated at the gateway boundary
- Sensitive metadata MUST NOT be exposed to IM platforms
- Trace identifiers MUST NOT leak internal topology

---

## 11. Compliance

An IM integration is **UIP-compliant** if and only if:

- All inbound events conform to CIE
- All outbound actions originate from Interaction Intents
- No agent-specific logic exists outside Clawdbot Runtime

---

## 12. Gateway Implementation Specification

### 12.1 Gateway Transport Layer

UIP Gateway MUST expose the following transport interfaces:

**Inbound (IM → Gateway)**:
- HTTP REST API: `POST /api/v1/events` for CIE submission
- WebSocket: `ws://gateway/ws` for real-time bidirectional communication

**Outbound (Gateway → Clawdbot)**:
- gRPC: Primary protocol for low-latency communication
- HTTP/2: Fallback for environments without gRPC support

### 12.2 Gateway Configuration

```yaml
server:
  http_port: 8080
  grpc_port: 9090
  websocket_path: "/ws"

clawdbot:
  endpoint: "clawdbot:50051"
  timeout: 30s
  retry_policy:
    max_retries: 3
    backoff: "exponential"

adapters:
  - name: "local-im"
    type: "http"
    enabled: true
  - name: "slack"
    type: "webhook"
    enabled: false

observability:
  tracing: true
  metrics_port: 9091
```

### 12.3 Adapter Interface Contract

Each IM adapter MUST implement:

```go
type IMAdapter interface {
    // Name returns the unique adapter identifier
    Name() string
    
    // Start initializes the adapter
    Start(ctx context.Context) error
    
    // Stop gracefully shuts down the adapter
    Stop(ctx context.Context) error
    
    // OnEvent registers a callback for inbound events
    OnEvent(handler func(event *CanonicalInteractionEvent))
    
    // SendIntent delivers an interaction intent to the IM
    SendIntent(ctx context.Context, intent *InteractionIntent) error
    
    // Capabilities returns supported IM capabilities
    Capabilities() *SurfaceCapabilities
}
```

### 12.4 Event Flow

```
┌─────────────┐     CIE      ┌─────────────┐     gRPC     ┌──────────────┐
│  IM Platform│────────────▶│ UIP Gateway │─────────────▶│   Clawdbot   │
│  (Adapter)  │             │             │              │   Runtime    │
└─────────────┘             └─────────────┘              └──────────────┘
       ▲                          │                            │
       │       Intent             │        Intent              │
       └──────────────────────────┴────────────────────────────┘
```

### 12.5 Session Management

Gateway maintains a session registry with:
- TTL-based expiration (default: 24h)
- LRU eviction policy
- Redis-compatible external storage option

---

## 13. Future Extensions

- Multi-participant interaction graphs
- Non-IM surfaces (Voice, UI, RPA)
- Interaction time-travel and audit replay
- Distributed gateway clustering
- Event sourcing with Kafka/NATS

---

## 14. Final Statement

> **UIP defines how systems interact, not how they think.**  
> **Any system that can interact can participate.**
