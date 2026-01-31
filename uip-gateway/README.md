# UIP Gateway

**Universal Interaction Protocol Gateway** - 高性能、IM无关的OpenClaw交互网关

## 概述

UIP Gateway 是一个独立进程的交互网关，实现了 Universal Interaction Protocol (UIP) 规范。它允许任何即时通讯(IM)系统与 OpenClaw 无缝交互，而无需了解 agents、LLMs、prompts 或 tools。

### 核心特性

- **IM无关性**: 统一的交互协议，支持任意IM平台接入
- **高性能**: Go语言实现，支持高并发处理
- **独立部署**: 作为独立进程运行，不依赖于特定agent
- **可扩展**: 插件化适配器架构，易于添加新IM平台
- **可观测**: 内置tracing、metrics和结构化日志
- **端到端通信**: 完整支持 OpenClaw Universal IM 插件的 Webhook、WebSocket、Polling 三种传输模式

## 快速开始

### 前置要求

- Go 1.22+
- OpenClaw Runtime 运行实例

### 安装与运行

```bash
# 1. 进入项目目录
cd uip-gateway

# 2. 下载依赖
go mod download

# 3. 构建
make build

# 4. 运行 (Mock模式，无需OpenClaw)
make mock

# 或者连接真实OpenClaw
make run
```

### 使用 Docker

```bash
# 构建镜像
make docker

# 运行容器
docker run -p 8080:8080 uip-gateway:0.1.0
```

## OpenClaw Universal IM 集成

UIP Gateway 与 OpenClaw 的 Universal IM 插件完全集成，支持端到端通信。

### 消息流程

```
┌─────────────────┐         ┌──────────────────┐         ┌─────────────────┐
│   Your IM       │ ──────▶ │  UIP Gateway     │ ──────▶ │    OpenClaw     │
│   System        │         │                  │         │  Universal IM   │
│                 │ ◀────── │                  │ ◀────── │                 │
└─────────────────┘         └──────────────────┘         └─────────────────┘
      │                            │                            │
      │  1. POST /local/message   │  2. POST webhook           │
      │                           │                            │
      │                           │  3. AI 处理                │
      │                           │                            │
      │  4. 返回响应              │  POST /openclaw/outbound   │
      ▼                           ▼                            ▼
```

### 配置 OpenClaw Universal IM

在 OpenClaw 的配置文件 (`~/.openclaw/config.yaml`) 中添加:

```yaml
channels:
  universal-im:
    enabled: true
    transport: webhook
    webhook:
      path: /universal-im/default/webhook
      secret: your-webhook-secret
    outbound:
      url: http://localhost:8080/api/v1/openclaw/outbound
      authHeader: Bearer your-token
    dmPolicy: open
    allowFrom:
      - "*"
```

### 配置 UIP Gateway

在 `config.yaml` 中配置 OpenClaw 连接:

```yaml
clawdbot:
  endpoint: "http://localhost:18789"
  timeout: 30s
  mode: "openclaw"
  callback_url: "http://localhost:8080/api/v1/openclaw/outbound"
  
  universal_im:
    account_id: "default"
    transport: "webhook"
    secret: "your-webhook-secret"
    outbound_url: "http://localhost:8080/api/v1/openclaw/outbound"
```

### 支持的传输模式

#### 1. Webhook (默认)

OpenClaw 通过 HTTP POST 发送 AI 响应到 UIP Gateway:

```yaml
universal_im:
  transport: "webhook"
  outbound_url: "http://localhost:8080/api/v1/openclaw/outbound"
```

#### 2. WebSocket

OpenClaw 通过 WebSocket 连接推送 AI 响应:

```yaml
universal_im:
  transport: "websocket"
  websocket:
    url: "ws://localhost:8080/api/v1/openclaw/ws"
    reconnect_ms: 5000
```

#### 3. Polling

OpenClaw 轮询 UIP Gateway 获取消息:

```yaml
universal_im:
  transport: "polling"
  polling:
    url: "http://localhost:8080/api/v1/openclaw/poll"
    interval_ms: 3000
```

## API 使用

### 发送消息 (HTTP REST)

```bash
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "session-001",
    "userId": "user-001",
    "text": "Hello, OpenClaw!"
  }'
```

### WebSocket 连接

```javascript
// JavaScript 示例
const ws = new WebSocket('ws://localhost:8080/api/v1/local/ws?sessionId=session-001&userId=user-001');

ws.onopen = () => {
  ws.send(JSON.stringify({
    text: "Hello via WebSocket!"
  }));
};

ws.onmessage = (event) => {
  const intent = JSON.parse(event.data);
  console.log('Received:', intent.content.text);
};
```

### 健康检查

```bash
curl http://localhost:8080/health
```

### API 信息

```bash
curl http://localhost:8080/api/v1/info
```

返回:
```json
{
  "name": "UIP Gateway",
  "version": "0.1.0",
  "protocol": "UIP v1.0",
  "mode": "openclaw",
  "openclaw": {
    "endpoint": "http://localhost:18789",
    "accountId": "default",
    "transport": "webhook"
  },
  "endpoints": {
    "local_message": "/api/v1/local/message",
    "local_ws": "/api/v1/local/ws",
    "openclaw_outbound": "/api/v1/openclaw/outbound",
    "openclaw_ws": "/api/v1/openclaw/ws",
    "openclaw_poll": "/api/v1/openclaw/poll",
    "health": "/health"
  }
}
```

## 架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        UIP Gateway                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────┐  CIE   ┌──────────────┐        ┌───────────┐ │
│   │ IM Adapters │───────▶│   Gateway    │───────▶│ OpenClaw  │ │
│   │             │        │    Core      │        │  Client   │ │
│   │ - Local     │◀───────│              │◀───────│           │ │
│   │ - Slack     │ Intent │              │        │           │ │
│   │ - WeChat    │        └──────────────┘        └───────────┘ │
│   └─────────────┘               │                       │      │
│                                 │                       │      │
│   ┌─────────────┐        ┌──────▼──────┐         ┌──────▼────┐ │
│   │ Transports  │        │   Session   │         │ OpenClaw  │ │
│   │ - WebSocket │        │  Registry   │         │Universal  │ │
│   │ - Polling   │        └─────────────┘         │    IM     │ │
│   └─────────────┘                                └───────────┘ │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## OpenClaw Universal IM 消息格式

### 入站消息格式 (发送到 OpenClaw)

UIP Gateway 发送到 OpenClaw 的消息格式:

```json
{
  "messageId": "unique-id",
  "timestamp": 1706745600000,
  "sender": {
    "id": "user-123",
    "name": "John Doe",
    "username": "johndoe",
    "isBot": false
  },
  "conversation": {
    "type": "direct",
    "id": "conv-123",
    "name": "General",
    "threadId": "thread-456"
  },
  "text": "Hello world",
  "attachments": [
    {
      "kind": "image",
      "url": "https://...",
      "contentType": "image/png"
    }
  ],
  "meta": {
    "traceId": "trace-id"
  }
}
```

### 出站消息格式 (从 OpenClaw 接收)

OpenClaw 发送到 UIP Gateway 的响应格式:

```json
{
  "to": "user:user-123",
  "text": "AI response here",
  "mediaUrl": "https://...",
  "replyToId": "msg-id",
  "threadId": "thread-456"
}
```

## 配置参考

完整的 `config.yaml` 配置示例:

```yaml
server:
  http_port: 8080
  grpc_port: 9090
  websocket_path: "/ws"
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 10s

clawdbot:
  endpoint: "http://localhost:18789"
  timeout: 30s
  retry_policy:
    max_retries: 3
    backoff: "exponential"
    initial_interval: 100ms
    max_interval: 5s
  insecure: true
  mode: "openclaw"
  callback_url: "http://localhost:8080/api/v1/openclaw/outbound"
  
  universal_im:
    account_id: "default"
    transport: "webhook"
    secret: ""
    outbound_url: "http://localhost:8080/api/v1/openclaw/outbound"
    outbound_auth_header: ""
    websocket:
      url: ""
      reconnect_ms: 5000
    polling:
      url: ""
      interval_ms: 5000

adapters:
  local:
    enabled: true
    http_path: "/api/v1/local"
  slack:
    enabled: false
    webhook_path: "/api/v1/slack"
  wechat:
    enabled: false
    webhook_path: "/api/v1/wechat"

session:
  ttl: 24h
  max_sessions: 10000
  cleanup_interval: 5m

observability:
  log_level: "info"
  tracing: true
  metrics_port: 9091
```

## UIP 协议

### Canonical Interaction Event (CIE)

所有入站交互都必须表示为 CIE:

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

### Interaction Intent

OpenClaw 的响应:

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

## 扩展适配器

实现 `IMAdapter` 接口来添加新的IM平台支持:

```go
type IMAdapter interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    OnEvent(handler EventHandler)
    SendIntent(ctx context.Context, intent *InteractionIntent) error
    Capabilities() *SurfaceCapabilities
}
```

## 开发

```bash
# 格式化代码
make fmt

# 运行测试
make test

# 代码检查
make lint
```

## License

MIT License
