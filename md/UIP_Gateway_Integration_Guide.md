# UIP Gateway 接入指南

本文档描述外部 IM 系统如何接入 UIP Gateway，实现与 OpenClaw AI 的端到端通信。

## 一、架构概览

```
┌─────────────────┐                    ┌─────────────────┐                    ┌─────────────────┐
│                 │  1. POST /message  │                 │  2. Webhook        │                 │
│   外部 IM 系统   │ ─────────────────► │   UIP Gateway   │ ─────────────────► │    OpenClaw     │
│  (Slack/飞书等)  │                    │   :8080         │                    │    AI Engine    │
│                 │  4. Webhook推送    │                 │  3. AI Response    │                 │
│                 │ ◄───────────────── │                 │ ◄───────────────── │                 │
└─────────────────┘                    └─────────────────┘                    └─────────────────┘
     你的 IM                                                                     
     服务器                                                                    
```

### 消息流程

1. **Inbound (用户 → AI)**：外部 IM 发送消息到 Gateway 的 `/api/v1/local/message`
2. **Gateway → OpenClaw**：Gateway 转发消息到 OpenClaw Universal IM 插件
3. **AI 处理**：OpenClaw AI 引擎处理消息并生成响应
4. **Outbound (AI → 用户)**：Gateway 将 AI 响应推送到外部 IM 的 webhook

---

## 二、配置 Gateway

### 2.1 基础配置

编辑 `config.yaml`：

```yaml
server:
  http_port: 8080
  read_timeout: 30s
  write_timeout: 30s

clawdbot:
  endpoint: "http://localhost:18789"  # OpenClaw Gateway 地址
  timeout: 30s
  mode: "openclaw"
  universal_im:
    account_id: "default"
    transport: "webhook"

adapters:
  local:
    enabled: true
    http_path: "/api/v1/local"
```

### 2.2 启用 IM Webhook（关键配置）

为了让 Gateway 主动将 AI 响应推送到你的 IM 系统，需要配置 `im_webhook`：

```yaml
# IM webhook - AI 响应会主动推送到这里
im_webhook:
  enabled: true
  url: "http://your-im-server:3000/api/ai-response"  # 你的 IM 系统接收地址
  auth_header: "Bearer your-secret-token"            # 可选：认证 header
  timeout: 10s
  retry_count: 3
```

---

## 三、Inbound API - 发送消息到 Gateway

### 3.1 接口说明

| 项目 | 值 |
|------|-----|
| **URL** | `POST /api/v1/local/message` |
| **Content-Type** | `application/json` |

### 3.2 请求参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `sessionId` | string | 是 | 会话 ID，用于追踪对话上下文 |
| `userId` | string | 是 | 发送者用户 ID |
| `text` | string | 是 | 消息文本内容 |
| `channelId` | string | 否 | **外部 IM 的频道/群聊 ID**，用于路由 AI 响应 |
| `conversationType` | string | 否 | 对话类型：`direct`(私聊)、`group`(群组)、`channel`(频道) |
| `type` | string | 否 | 输入类型：`text`(默认)、`command`、`event` |

### 3.3 请求示例

```bash
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "my-session",
    "userId": "user123",
    "channelId": "slack-C12345678",
    "conversationType": "channel",
    "text": "你好 早上好"
  }'
```

### 3.4 响应示例

**成功响应：**
```json
{
  "success": true
}
```

**错误响应：**
```json
{
  "success": false,
  "error": {
    "code": "PROTOCOL_ERROR",
    "message": "Invalid request body"
  }
}
```

---

## 四、Outbound Webhook - 接收 AI 响应

当 AI 生成响应后，Gateway 会将消息推送到你配置的 `im_webhook.url`。

### 4.1 Webhook 请求格式

Gateway 会 POST 以下 JSON 到你的 IM 服务器：

```json
{
  "messageId": "ai-resp-1234567890123",
  "timestamp": 1769999761644,
  "to": "user:user123",
  "text": "你好！早上好！有什么我可以帮助你的吗？",
  "mediaUrl": "",
  "replyToId": "msg-001",
  "threadId": "",
  "routing": {
    "channelId": "slack-C12345678",
    "userId": "user123",
    "sessionId": "my-session"
  }
}
```

### 4.2 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `messageId` | string | AI 响应的唯一 ID |
| `timestamp` | number | Unix 时间戳（毫秒） |
| `to` | string | 目标格式：`user:{userId}` 或 `channel:{channelId}` |
| `text` | string | AI 响应文本 |
| `mediaUrl` | string | 可选：媒体附件 URL |
| `replyToId` | string | 可选：回复的原始消息 ID |
| `threadId` | string | 可选：线程 ID（用于线程回复） |
| `routing` | object | **路由信息（重要！）** |
| `routing.channelId` | string | **外部 IM 频道 ID** - 告诉你该发到哪个频道 |
| `routing.userId` | string | 原始发送者用户 ID |
| `routing.sessionId` | string | 会话 ID |

### 4.3 你的 IM 服务器实现示例

**Python Flask 示例：**

```python
from flask import Flask, request, jsonify
import slack_sdk

app = Flask(__name__)
slack_client = slack_sdk.WebClient(token="xoxb-your-token")

@app.post('/api/ai-response')
def handle_ai_response():
    data = request.json
    
    # 获取路由信息
    channel_id = data['routing'].get('channelId')
    user_id = data['routing'].get('userId')
    text = data['text']
    
    # 根据 channelId 发送到正确的频道
    if channel_id:
        slack_client.chat_postMessage(channel=channel_id, text=text)
    else:
        # 私信给用户
        slack_client.chat_postMessage(channel=user_id, text=text)
    
    return jsonify({'ok': True})

if __name__ == '__main__':
    app.run(port=3000)
```

**Node.js Express 示例：**

```javascript
const express = require('express');
const { WebClient } = require('@slack/web-api');

const app = express();
const slack = new WebClient(process.env.SLACK_TOKEN);

app.use(express.json());

app.post('/api/ai-response', async (req, res) => {
  const { text, routing } = req.body;
  const { channelId, userId } = routing;
  
  try {
    // 发送到正确的频道或用户
    await slack.chat.postMessage({
      channel: channelId || userId,
      text: text
    });
    res.json({ ok: true });
  } catch (error) {
    console.error('Failed to send message:', error);
    res.status(500).json({ ok: false, error: error.message });
  }
});

app.listen(3000, () => console.log('IM server running on :3000'));
```

---

## 五、完整集成示例

### 5.1 消息流程演示

```
1. 用户在 Slack #general 频道发送 "你好 早上好"
      ↓
2. 你的 Slack Bot 捕获消息，POST 到 Gateway:
   POST http://localhost:8080/api/v1/local/message
   {
     "sessionId": "slack-conv-123",
     "userId": "U12345",
     "channelId": "C12345678",        ← Slack 频道 ID
     "conversationType": "channel",
     "text": "你好 早上好"
   }
      ↓
3. Gateway 返回 {"success": true}
      ↓
4. Gateway 转发给 OpenClaw AI
      ↓
5. AI 生成回复，通过 outbound 返回
      ↓
6. Gateway 推送到你的 webhook (http://your-server:3000/api/ai-response):
   {
     "text": "你好！早上好！有什么我可以帮你的？",
     "routing": {
       "channelId": "C12345678",      ← 告诉你发到哪个频道
       "userId": "U12345"
     }
   }
      ↓
7. 你的 Bot 根据 channelId="C12345678" 发送到 Slack #general 频道
```

### 5.2 测试命令

```bash
# 1. 启动 Gateway
./bin/uip-gateway -config config.yaml

# 2. 发送测试消息
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "my-session",
    "userId": "user123",
    "channelId": "slack-C12345678",
    "conversationType": "channel",
    "text": "你好 早上好"
  }'

# 3. 模拟 AI 响应（用于测试）
curl -X POST http://localhost:8080/api/v1/openclaw/outbound \
  -H "Content-Type: application/json" \
  -d '{
    "to": "user:user123",
    "text": "你好！这是 AI 的回复。"
  }'
```

---

## 六、关键概念说明

### 6.1 `channelId` 的重要性

`channelId` 是实现**消息路由**的关键字段：

- **发送时**：告诉 Gateway 这条消息来自哪个频道/群聊
- **接收时**：Gateway 在 `routing.channelId` 中返回，告诉你该把 AI 响应发到哪里

### 6.2 `sessionId` vs `channelId` vs `userId`

| 字段 | 用途 | 示例 |
|------|------|------|
| `sessionId` | 会话追踪，用于 AI 上下文管理 | `"conv-2024-001"` |
| `channelId` | IM 平台的频道/群聊 ID，用于消息路由 | `"C12345678"` (Slack) |
| `userId` | 消息发送者的用户 ID | `"U12345"` |

### 6.3 多 Agent 场景

对于多 Agent 协作场景，可以使用不同的 `channelId` 来区分不同的讨论频道：

```bash
# 发送到设计讨论频道
curl -X POST http://localhost:8080/api/v1/local/message \
  -d '{"sessionId":"design-session","userId":"alice","channelId":"design-channel","text":"讨论 UI 设计"}'

# 发送到技术讨论频道
curl -X POST http://localhost:8080/api/v1/local/message \
  -d '{"sessionId":"tech-session","userId":"bob","channelId":"tech-channel","text":"讨论技术方案"}'
```

---

## 七、错误处理

### 7.1 Gateway 返回错误

```json
{
  "success": false,
  "error": {
    "code": "GATEWAY_ERROR",
    "message": "OpenClaw connection failed"
  }
}
```

### 7.2 Webhook 推送失败

Gateway 会自动重试（根据 `retry_count` 配置），如果仍然失败，会在日志中记录：

```
{"level":"error","msg":"Failed to notify IM webhook","error":"connection refused"}
```

---

## 八、API 端点汇总

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v1/local/message` | POST | 接收外部 IM 消息 |
| `/api/v1/local/ws` | WebSocket | WebSocket 连接（实时通信） |
| `/api/v1/openclaw/outbound` | POST | 接收 OpenClaw AI 响应 |
| `/health` | GET | 健康检查 |
| `/api/v1/info` | GET | 获取 Gateway 信息 |

---

## 九、相关文档

- [Universal IM 插件开发指南](./Universal_IM_Plugin_Development_Guide.md)
- [Universal IM Transport 注册](./Universal_IM_Transport_Registration.md)
- [Universal IM CLI 集成指南](./Universal_IM_CLI_Integration_Guide.md)
