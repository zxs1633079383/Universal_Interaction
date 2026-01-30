# UIP Gateway 集成指南

**文档版本**: 2.0  
**最后更新**: 2026-01-30  
**作者**: Clawdbot Architecture Group

---

## 1. 概述

### 1.1 我们做了什么

我们实现了一个 **UIP Gateway (Universal Interaction Protocol Gateway)**，这是一个高性能、独立运行的交互网关，作为所有 IM 系统与 **Moltbot** 之间的桥梁。

Gateway 使用 Moltbot 的 **universal-im** 插件进行通信，这是 Moltbot 官方提供的通用 IM 接入方式。

```
┌─────────────┐                    ┌─────────────┐                    ┌─────────────┐
│   您的 IM   │  ───HTTP/WS────▶  │ UIP Gateway │  ──universal-im──▶│   Moltbot   │
│    系统     │  ◀───Intent────   │   (Go)      │  ◀───callback────  │  (Gateway)  │
└─────────────┘                    └─────────────┘                    └─────────────┘
```

### 1.2 核心价值

| 特性 | 说明 |
|------|------|
| **IM 无关性** | Gateway 抽象了所有 IM 差异，Clawdbot 只需要处理统一的 UIP 协议 |
| **独立进程** | Gateway 作为独立服务运行，不依赖任何 agent |
| **双向通信** | 支持同步请求-响应和异步 WebSocket 推送 |
| **可扩展** | 插件化适配器架构，可轻松添加新 IM 平台 |

---

## 2. 系统架构

### 2.1 完整数据流

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              数据流向                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  [用户 IM 消息]                                                              │
│       │                                                                      │
│       ▼                                                                      │
│  ┌─────────────┐                                                            │
│  │  IM 系统    │  (微信/Slack/Discord/自定义)                               │
│  └─────────────┘                                                            │
│       │                                                                      │
│       │ HTTP POST / WebSocket                                               │
│       ▼                                                                      │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        UIP Gateway                                   │   │
│  │  ┌─────────────┐   ┌────────────────┐   ┌──────────────────────┐   │   │
│  │  │ IM Adapter  │──▶│ Gateway Core   │──▶│ Clawdbot Client      │   │   │
│  │  │ (Local)     │   │ (事件处理队列) │   │ (HTTP/gRPC)          │   │   │
│  │  └─────────────┘   └────────────────┘   └──────────────────────┘   │   │
│  │       ▲                    │                       │                │   │
│  │       │                    │                       │                │   │
│  │       │     Intent         │                       │                │   │
│  │       └────────────────────┘                       │                │   │
│  └────────────────────────────────────────────────────│────────────────┘   │
│                                                       │                     │
│       │ HTTP POST (标准 API)                          │                     │
│       ▼                                               │                     │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        Clawdbot (moltbot)                            │   │
│  │  ┌─────────────┐   ┌────────────────┐   ┌──────────────────────┐   │   │
│  │  │ API 入口    │──▶│ Agent 处理逻辑 │──▶│ LLM / Tools          │   │   │
│  │  │ /api/v1/chat│   │                │   │                      │   │   │
│  │  └─────────────┘   └────────────────┘   └──────────────────────┘   │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       │ JSON Response                                                        │
│       ▼                                                                      │
│  [返回给 Gateway] ──▶ [转换为 Intent] ──▶ [推送给 IM 系统]                  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 组件职责

| 组件 | 职责 | 位置 |
|------|------|------|
| **IM 系统** | 接收用户消息，展示 AI 响应 | 您的系统 |
| **UIP Gateway** | 协议转换，路由，会话管理 | `uip-gateway/` |
| **Clawdbot** | AI 决策，生成响应 | moltbot 源码 |

---

## 3. API 规范

### 3.1 Gateway 对外接口 (IM 系统 → Gateway)

#### 3.1.1 HTTP 消息接口

**端点**: `POST http://gateway:8080/api/v1/local/message`

**请求体**:
```json
{
  "sessionId": "user-session-001",
  "userId": "user-12345",
  "text": "你好，请帮我写一段代码",
  "type": "text"
}
```

**字段说明**:
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `sessionId` | string | 是 | 会话ID，用于保持对话上下文 |
| `userId` | string | 是 | 用户唯一标识 |
| `text` | string | 是 | 用户消息内容 |
| `type` | string | 否 | 消息类型: `text`(默认), `command`, `event` |

**响应**:
```json
{
  "success": true
}
```

> ⚠️ **注意**: HTTP 接口是异步的，响应只表示消息已接收。实际的 AI 响应会通过 WebSocket 推送。

#### 3.1.2 WebSocket 实时通信

**端点**: `ws://gateway:8080/api/v1/local/ws?sessionId=xxx&userId=xxx`

**连接参数**:
| 参数 | 说明 |
|------|------|
| `sessionId` | 会话ID |
| `userId` | 用户ID |

**发送消息格式**:
```json
{
  "text": "你好，请帮我写一段代码",
  "type": "text"
}
```

**接收响应格式 (Interaction Intent)**:
```json
{
  "intentId": "intent-uuid-001",
  "intentType": "reply",
  "content": {
    "text": "好的，我来帮你写一段代码..."
  },
  "targetSessionId": "user-session-001",
  "inReplyTo": "interaction-uuid-001"
}
```

### 3.2 Gateway → Moltbot 接口 (universal-im)

#### 3.2.1 发送消息到 Moltbot

**端点**: `POST http://moltbot:18789/universal-im/webhook/:endpointId`

**请求头**:
```
Content-Type: application/json
Authorization: Bearer <token>
```

**请求体**:
```json
{
  "message_id": "interaction-uuid-001",
  "text": "你好，请帮我写一段代码",
  "from": {
    "id": "user-12345",
    "name": "用户名"
  },
  "chat": {
    "id": "session-001",
    "type": "private"
  }
}
```

**字段说明**:
| 字段 | 类型 | 说明 |
|------|------|------|
| `message_id` | string | 消息唯一ID |
| `text` | string | 用户消息 |
| `from.id` | string | 发送者ID |
| `from.name` | string | 发送者名称 |
| `chat.id` | string | 会话ID (用于上下文管理) |
| `chat.type` | string | `private` 或 `group` |

**即时响应** (消息已接收):
```json
{
  "ok": true,
  "messageId": "interaction-uuid-001",
  "replied": false,
  "elapsed": 50
}
```

#### 3.2.2 Moltbot 回调 (异步响应)

Moltbot 处理完消息后，会 POST 到 Gateway 的 callback URL：

**端点**: `POST http://gateway:8080/api/v1/callback`

**请求体**:
```json
{
  "chat_id": "session-001",
  "text": "好的，我来帮你写一段 Python 代码:\n\n```python\nprint('Hello, World!')\n```",
  "reply_to_message_id": "interaction-uuid-001"
}
```

**字段说明**:
| 字段 | 类型 | 说明 |
|------|------|------|
| `chat_id` | string | 目标会话ID |
| `text` | string | AI 生成的响应 |
| `reply_to_message_id` | string | 回复的原始消息ID |
| `media_url` | string | (可选) 媒体附件URL |

#### 3.2.3 健康检查

**端点**: `GET http://moltbot:18789/universal-im/health`

**响应**:
```json
{
  "ok": true,
  "plugin": "universal-im",
  "endpoints": {
    "total": 1,
    "enabled": 1
  }
}
```

---

## 4. Moltbot 配置

UIP Gateway 使用 Moltbot 的 **universal-im** 插件进行通信。以下是配置步骤：

### 4.1 启用 universal-im 插件

```bash
# 启用插件
pnpm moltbot plugins enable universal-im
```

### 4.2 配置 universal-im 端点

编辑 Moltbot 配置文件 (`~/.clawdbot/config.json`)，添加 UIP Gateway 端点：

```jsonc
{
  "channels": {
    "universal-im": {
      "enabled": true,
      "endpoints": {
        "uip-gateway": {
          "token": "uip-gateway-token",
          "callbackUrl": "http://localhost:8080/api/v1/callback",
          "name": "UIP Gateway",
          "enabled": true,
          "dmPolicy": "open"
        }
      }
    }
  }
}
```

**配置说明**:

| 字段 | 说明 |
|------|------|
| `token` | 认证 token，必须与 Gateway 配置中的 `clawdbot.token` 一致 |
| `callbackUrl` | Gateway 的回调地址，Moltbot 会将响应 POST 到这里 |
| `name` | 端点显示名称 |
| `dmPolicy` | 权限策略: `open`(开放), `pairing`(需配对), `allowlist`(白名单) |

### 4.3 重启 Moltbot Gateway

```bash
# 重启 gateway
pnpm moltbot gateway stop && sleep 2 && pnpm moltbot gateway run

# 或使用
pnpm moltbot gateway restart
```

### 4.4 验证配置

```bash
# 查看 channels 状态
pnpm moltbot channels status

# 检查 universal-im 健康状态
curl http://localhost:18789/universal-im/health
```

**预期输出**:
```json
{
  "ok": true,
  "plugin": "universal-im",
  "endpoints": {
    "total": 1,
    "enabled": 1
  }
}
```

---

## 5. 对接调试步骤

### 5.1 第一步：启动 Gateway (Mock 模式)

首先在不连接 Moltbot 的情况下测试 Gateway：

```bash
cd uip-gateway

# 构建
make build

# Mock 模式启动 (内置模拟响应)
./bin/uip-gateway -mock -config config.yaml
```

**验证**:
```bash
# 健康检查
curl http://localhost:8080/health

# 发送测试消息
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{"sessionId": "test", "userId": "user1", "text": "Hello"}'
```

### 5.2 第二步：配置并启动 Moltbot

**5.2.1 编辑 Moltbot 配置**

创建或编辑 `~/.clawdbot/config.json`：

```bash
# 如果文件不存在，可以通过 CLI 设置
pnpm moltbot config set channels.universal-im.enabled true
pnpm moltbot config set channels.universal-im.endpoints.uip-gateway.token "uip-gateway-token"
pnpm moltbot config set channels.universal-im.endpoints.uip-gateway.callbackUrl "http://localhost:8080/api/v1/callback"
```

或者直接编辑配置文件：

```json
{
  "channels": {
    "universal-im": {
      "enabled": true,
      "endpoints": {
        "uip-gateway": {
          "token": "uip-gateway-token",
          "callbackUrl": "http://localhost:8080/api/v1/callback",
          "enabled": true,
          "dmPolicy": "open"
        }
      }
    }
  }
}
```

**5.2.2 启用插件并启动 Gateway**

```bash
# 启用 universal-im 插件
pnpm moltbot plugins enable universal-im

# 启动 moltbot gateway
pnpm moltbot gateway run
```

**5.2.3 验证 Moltbot**

```bash
# 查看 channel 状态
pnpm moltbot channels status

# 检查 universal-im 健康
curl http://localhost:18789/universal-im/health

# 直接测试 universal-im webhook
curl -X POST http://localhost:18789/universal-im/webhook/uip-gateway \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer uip-gateway-token" \
  -d '{
    "message_id": "test-001",
    "text": "你好",
    "from": {"id": "user1", "name": "测试用户"},
    "chat": {"id": "chat1", "type": "private"}
  }'
```

### 5.3 第三步：配置并启动 UIP Gateway

**5.3.1 编辑 Gateway 配置** (`config.yaml`)：

```yaml
server:
  http_port: 8080

clawdbot:
  endpoint: "http://localhost:18789"  # Moltbot 默认端口
  timeout: 30s
  mode: "moltbot"
  token: "uip-gateway-token"          # 必须与 Moltbot 配置一致
  endpoint_id: "uip-gateway"
  callback_url: "http://localhost:8080/api/v1/callback"
```

**5.3.2 启动 Gateway**

```bash
./bin/uip-gateway -config config.yaml
```

### 5.4 第四步：端到端测试

```bash
# 通过 Gateway 发送消息给 Moltbot
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "e2e-test-001",
    "userId": "test-user",
    "text": "你好，请介绍一下你自己"
  }'
```

**完整消息流程**:
```
1. curl → Gateway (/api/v1/local/message)
2. Gateway → Moltbot (/universal-im/webhook/uip-gateway)
3. Moltbot 处理消息 (Agent/LLM)
4. Moltbot → Gateway (/api/v1/callback)
5. Gateway → 您的 IM 系统 (WebSocket/HTTP)
```

---

## 6. 您的 IM 系统如何接入

### 6.1 方案一：HTTP 轮询 (简单)

```
[IM 系统] ─POST─▶ [Gateway] ─▶ [Clawdbot]
                      │
                      │ (存储响应到队列)
                      ▼
[IM 系统] ◀─GET──  [响应队列]
```

**代码示例**:
```python
import requests
import time

GATEWAY_URL = "http://localhost:8080"

def send_message(session_id, user_id, text):
    """发送消息到 Gateway"""
    response = requests.post(
        f"{GATEWAY_URL}/api/v1/local/message",
        json={
            "sessionId": session_id,
            "userId": user_id,
            "text": text
        }
    )
    return response.json()

# 使用示例
result = send_message("session-001", "user-001", "你好")
print(result)  # {"success": true}
```

### 6.2 方案二：WebSocket 实时通信 (推荐)

```
[IM 系统] ◀──WebSocket──▶ [Gateway] ──▶ [Clawdbot]
          双向实时通信
```

**Python 代码示例**:
```python
import asyncio
import websockets
import json

async def chat_with_gateway():
    """通过 WebSocket 与 Gateway 通信"""
    uri = "ws://localhost:8080/api/v1/local/ws?sessionId=session-001&userId=user-001"
    
    async with websockets.connect(uri) as websocket:
        # 发送消息
        await websocket.send(json.dumps({
            "text": "你好，请帮我写一段代码",
            "type": "text"
        }))
        
        # 接收响应
        response = await websocket.recv()
        intent = json.loads(response)
        
        print(f"AI 响应: {intent['content']['text']}")

# 运行
asyncio.run(chat_with_gateway())
```

**JavaScript 代码示例**:
```javascript
// 浏览器或 Node.js
const ws = new WebSocket('ws://localhost:8080/api/v1/local/ws?sessionId=session-001&userId=user-001');

ws.onopen = () => {
    console.log('已连接到 Gateway');
    
    // 发送消息
    ws.send(JSON.stringify({
        text: '你好，请帮我写一段代码',
        type: 'text'
    }));
};

ws.onmessage = (event) => {
    const intent = JSON.parse(event.data);
    console.log('AI 响应:', intent.content.text);
    
    // 将响应展示给用户...
};

ws.onerror = (error) => {
    console.error('连接错误:', error);
};
```

### 6.3 方案三：自定义 IM 适配器 (高级)

如果您的 IM 系统需要特殊处理（如微信、钉钉等），可以实现自定义适配器：

```go
// internal/adapter/myim/myim.go

package myim

import (
    "context"
    "github.com/zlc_ai/uip-gateway/internal/adapter"
    "github.com/zlc_ai/uip-gateway/internal/protocol"
)

type MyIMAdapter struct {
    // 您的 IM SDK 客户端
}

func (a *MyIMAdapter) Name() string {
    return "myim"
}

func (a *MyIMAdapter) Start(ctx context.Context) error {
    // 启动 IM 监听
    return nil
}

func (a *MyIMAdapter) OnEvent(handler adapter.EventHandler) {
    // 当收到 IM 消息时，转换为 CIE 并调用 handler
}

func (a *MyIMAdapter) SendIntent(ctx context.Context, intent *protocol.InteractionIntent) error {
    // 将 AI 响应发送到 IM
    return nil
}

func (a *MyIMAdapter) Capabilities() *protocol.SurfaceCapabilities {
    return &protocol.SurfaceCapabilities{
        SupportsReply:    true,
        SupportsMarkdown: true,
        // ...
    }
}
```

---

## 7. 调试检查清单

### 7.1 Gateway 检查

- [ ] Gateway 启动成功，监听 8080 端口
- [ ] `curl http://localhost:8080/health` 返回 `{"status":"healthy"}`
- [ ] 日志显示 "Using Moltbot universal-im client"
- [ ] 日志显示 "Adapter registered" 和 "Gateway started"

### 7.2 Moltbot 检查

- [ ] Moltbot gateway 启动成功 (`pnpm moltbot gateway run`)
- [ ] Moltbot 监听 18789 端口
- [ ] `pnpm moltbot channels status` 显示 universal-im 已启用
- [ ] `curl http://localhost:18789/universal-im/health` 返回 OK
- [ ] endpoints 中有 "uip-gateway"

### 7.3 连通性检查

```bash
# 1. 检查 Moltbot 健康
curl http://localhost:18789/universal-im/health

# 2. 直接测试 Moltbot webhook (应该返回 ok: true)
curl -X POST http://localhost:18789/universal-im/webhook/uip-gateway \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer uip-gateway-token" \
  -d '{"message_id":"test","text":"hello","from":{"id":"u1"},"chat":{"id":"c1","type":"private"}}'

# 3. 检查 Gateway callback 端点
curl -X POST http://localhost:8080/api/v1/callback \
  -H "Content-Type: application/json" \
  -d '{"chat_id":"test","text":"hello"}'
```

### 7.4 端到端检查

- [ ] Gateway 日志显示 "Received message"
- [ ] Gateway 日志显示 "Message sent to Moltbot"
- [ ] Gateway 日志显示 "Received Moltbot callback"
- [ ] Gateway 日志显示 "Event processed successfully"
- [ ] WebSocket 客户端收到 Intent

### 7.5 常见问题

| 问题 | 可能原因 | 解决方案 |
|------|----------|----------|
| "Endpoint not found" | token 不匹配或端点未启用 | 检查 Moltbot config.json 中的 token 和 enabled |
| "Invalid signature" | 签名验证失败 | 确保没有配置全局 secret，或者配置正确 |
| 回调超时 | Moltbot 找不到 callback URL | 检查 callbackUrl 是否可访问 |
| Gateway 连接失败 | Moltbot 未启动或端口错误 | 确认 Moltbot 在 18789 端口运行 |
| WebSocket 收不到响应 | sessionId/chatId 不匹配 | 确保 chat.id 和 sessionId 一致 |

### 7.6 日志调试

```bash
# Gateway 详细日志
./bin/uip-gateway -config config.yaml 2>&1 | jq .

# Moltbot 日志
pnpm moltbot gateway run --verbose
```

---

## 8. 配置参考

### 8.1 Gateway 完整配置 (config.yaml)

```yaml
server:
  http_port: 8080
  grpc_port: 9090
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 10s

clawdbot:
  # Moltbot gateway 地址 (默认端口 18789)
  endpoint: "http://localhost:18789"
  timeout: 30s
  retry_policy:
    max_retries: 3
    backoff: "exponential"
  insecure: true
  
  # Moltbot universal-im 集成配置
  mode: "moltbot"                     # "moltbot" 或 "legacy"
  token: "uip-gateway-token"          # 认证 token
  endpoint_id: "uip-gateway"          # Moltbot 端点 ID
  callback_url: "http://localhost:8080/api/v1/callback"

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
```

### 8.2 Moltbot 配置 (~/.clawdbot/config.json)

```jsonc
{
  "channels": {
    "universal-im": {
      "enabled": true,
      "endpoints": {
        "uip-gateway": {
          // 必须与 Gateway config.yaml 中的 token 一致
          "token": "uip-gateway-token",
          
          // Gateway 的回调地址
          "callbackUrl": "http://localhost:8080/api/v1/callback",
          
          // 可选配置
          "name": "UIP Gateway",
          "enabled": true,
          "dmPolicy": "open",
          
          // 如果 Gateway 需要认证回调请求
          "callbackAuth": {
            "type": "bearer",
            "token": "callback-auth-token"
          }
        }
      }
    }
  }
}
```

### 8.3 快速配置脚本

```bash
#!/bin/bash
# setup-moltbot-integration.sh

# 设置 Moltbot universal-im 端点
pnpm moltbot config set channels.universal-im.enabled true
pnpm moltbot config set channels.universal-im.endpoints.uip-gateway.token "uip-gateway-token"
pnpm moltbot config set channels.universal-im.endpoints.uip-gateway.callbackUrl "http://localhost:8080/api/v1/callback"
pnpm moltbot config set channels.universal-im.endpoints.uip-gateway.enabled true
pnpm moltbot config set channels.universal-im.endpoints.uip-gateway.dmPolicy "open"

# 启用插件
pnpm moltbot plugins enable universal-im

# 重启 gateway
pnpm moltbot gateway restart

echo "Moltbot 配置完成！"
```

### 8.4 环境变量覆盖

```bash
# Gateway 环境变量
export CLAWDBOT_ENDPOINT="http://moltbot-host:18789"
export CLAWDBOT_TOKEN="your-secret-token"
export LOG_LEVEL="debug"
```

---

## 9. 下一步

1. **启动 Gateway Mock 模式** - 验证基础设施
2. **实现 Clawdbot /api/v1/chat 接口** - 按照 4.1 节规范
3. **端到端测试** - 按照第 5 节步骤
4. **接入您的 IM 系统** - 选择方案二 WebSocket 推荐
5. **生产部署** - 使用 Docker/K8s

如有问题，请检查 Gateway 日志 (`log_level: debug`) 和 Clawdbot 日志进行排查。

---

## 附录：项目文件清单

```
uip-gateway/
├── cmd/gateway/main.go           # 主程序入口
├── internal/
│   ├── protocol/uip.go           # UIP 协议定义
│   ├── adapter/
│   │   ├── adapter.go            # 适配器接口
│   │   └── local/local.go        # 本地 HTTP/WS 适配器
│   ├── clawdbot/client.go        # Clawdbot HTTP 客户端
│   ├── gateway/gateway.go        # 网关核心逻辑
│   └── config/config.go          # 配置加载
├── config.yaml                   # 默认配置
├── Makefile                      # 构建脚本
└── README.md                     # 使用文档
```
