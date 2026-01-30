# UIP Gateway

**Universal Interaction Protocol Gateway** - 高性能、IM无关的Clawdbot交互网关

## 概述

UIP Gateway 是一个独立进程的交互网关，实现了 Universal Interaction Protocol (UIP) 规范。它允许任何即时通讯(IM)系统与 Clawdbot 无缝交互，而无需了解 agents、LLMs、prompts 或 tools。

### 核心特性

- **IM无关性**: 统一的交互协议，支持任意IM平台接入
- **高性能**: Go语言实现，支持高并发处理
- **独立部署**: 作为独立进程运行，不依赖于特定agent
- **可扩展**: 插件化适配器架构，易于添加新IM平台
- **可观测**: 内置tracing、metrics和结构化日志

## 快速开始

### 前置要求

- Go 1.22+
- (可选) Clawdbot Runtime 运行实例

### 安装与运行

```bash
# 1. 进入项目目录
cd uip-gateway

# 2. 下载依赖
go mod download

# 3. 构建
make build

# 4. 运行 (Mock模式，无需Clawdbot)
make mock

# 或者连接真实Clawdbot
make run
```

### 使用 Docker

```bash
# 构建镜像
make docker

# 运行容器
docker run -p 8080:8080 uip-gateway:0.1.0
```

## API 使用

### 发送消息 (HTTP REST)

```bash
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "session-001",
    "userId": "user-001",
    "text": "Hello, Clawdbot!"
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

## 架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        UIP Gateway                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────┐  CIE   ┌──────────────┐        ┌───────────┐ │
│   │ IM Adapters │───────▶│   Gateway    │───────▶│ Clawdbot  │ │
│   │             │        │    Core      │        │  Client   │ │
│   │ - Local     │◀───────│              │◀───────│           │ │
│   │ - Slack     │ Intent │              │        │           │ │
│   │ - WeChat    │        └──────────────┘        └───────────┘ │
│   └─────────────┘               │                       │      │
│                                 │                       │      │
│                          ┌──────▼──────┐         ┌──────▼────┐ │
│                          │   Session   │         │ Clawdbot  │ │
│                          │  Registry   │         │  Runtime  │ │
│                          └─────────────┘         └───────────┘ │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## 配置

配置文件 `config.yaml`:

```yaml
server:
  http_port: 8080
  grpc_port: 9090
  websocket_path: "/ws"

clawdbot:
  endpoint: "http://localhost:50051"
  timeout: 30s
  retry_policy:
    max_retries: 3
    backoff: "exponential"

adapters:
  local:
    enabled: true
    http_path: "/api/v1/local"

session:
  ttl: 24h
  max_sessions: 10000

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

Clawdbot 的响应:

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

## Clawdbot 集成

Gateway 期望 Clawdbot 提供以下 API:

```
POST /api/v1/chat
{
  "sessionId": "string",
  "userId": "string",
  "message": "string",
  "type": "text",
  "metadata": {}
}

Response:
{
  "response": "string",
  "type": "reply",
  "sessionId": "string"
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
