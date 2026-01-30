# 🌐 Universal Interaction Protocol (UIP)

<p align="center">
  <strong>A universal protocol that enables any IM to connect with AI Agents</strong>
</p>

<p align="center">
  <a href="https://github.com/zxs1633079383/Universal_Interaction"><img src="https://img.shields.io/badge/GitHub-Universal_Interaction-181717?style=for-the-badge&logo=github" alt="GitHub"></a>
  <a href="https://github.com/zxs1633079383/moltbot"><img src="https://img.shields.io/badge/Moltbot-Fork-orange?style=for-the-badge&logo=github" alt="Moltbot"></a>
  <img src="https://img.shields.io/badge/Status-✅_MVP_Complete-success?style=for-the-badge" alt="Status">
  <img src="https://img.shields.io/badge/Lang-Go-00ADD8?style=for-the-badge&logo=go" alt="Go">
</p>

<p align="center">
  <a href="README_CN.md">🇨🇳 中文文档</a>
</p>

---

## 📖 Overview

**Universal Interaction Protocol (UIP)** is an IM-agnostic interaction protocol that enables any Instant Messaging system to seamlessly connect with [Moltbot](https://github.com/zxs1633079383/moltbot) AI Agent.

> ✅ **Status**: MVP Complete, End-to-end communication verified (2026-01-30)

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Any IM    │────▶│ UIP Gateway │────▶│   Moltbot   │
│  WeChat/    │◀────│    (Go)     │◀────│  AI Agent   │
│  Slack/etc  │     └─────────────┘     └─────────────┘
└─────────────┘           │
       │           Universal IM
       │            Protocol
       └──────────────────┘
```

## ✨ Key Features

| Feature | Description |
|---------|-------------|
| 🔌 **IM Agnostic** | One protocol for all IMs, abstracts platform differences |
| 🚀 **High Performance** | Go-based Gateway with native concurrency support |
| 🔄 **Bidirectional** | Supports both sync requests and async callbacks |
| 🧩 **Extensible** | Plugin-based adapter architecture |
| 🤝 **Moltbot Integration** | Reuse and further develop the `universal-im` plugin |

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Data Flow                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   [User IM]  ──HTTP──▶  [UIP Gateway]  ──webhook──▶  [Moltbot]              │
│       │                      │                           │                   │
│       │                      │                           │ AI Processing     │
│       │                      │                           ▼                   │
│       │                      │ ◀────callback────  [AI Response]             │
│       │                      │                                               │
│       │ ◀────Intent────     │                                               │
│       ▼                      │                                               │
│   [Display]                  │                                               │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 🚀 Quick Start

### 1. Clone the project

```bash
git clone https://github.com/zxs1633079383/Universal_Interaction.git
cd Universal_Interaction/uip-gateway
```

### 2. Build the Gateway

```bash
make build
```

### 3. Configure Moltbot

Ensure your Moltbot has the `universal-im` plugin configured. Edit `~/.clawdbot/moltbot.json`:

```json
{
  "channels": {
    "universal-im": {
      "enabled": true,
      "endpoints": {
        "uip-gateway": {
          "token": "your-secret-token",
          "callbackUrl": "http://localhost:8080/api/v1/callback",
          "enabled": true,
          "dmPolicy": "open"
        }
      }
    }
  }
}
```

### 4. Start the services

```bash
# Terminal 1: Start Moltbot Gateway
cd /path/to/moltbot
pnpm moltbot gateway

# Terminal 2: Start UIP Gateway
cd /path/to/Universal_Interaction/uip-gateway
./bin/uip-gateway -config config.yaml
```

### 5. Test

```bash
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{"sessionId": "test-001", "userId": "user1", "text": "Hello"}'
```

## 📁 Project Structure

```
Universal_Interaction/
├── md/                                    # Documentation
│   ├── im_agnostic_clawdbot_adapter_architecture.md  # Protocol spec
│   ├── UIP_Gateway_Integration_Guide.md              # Integration guide
│   ├── Development_Journey_Universal_Interaction.md  # Dev journey
│   └── Team_Share_Moltbot_Universal_IM_Vision.md     # Team sharing
│
├── uip-gateway/                           # Go Gateway implementation
│   ├── cmd/gateway/main.go               # Main entry
│   ├── internal/
│   │   ├── protocol/uip.go               # UIP protocol definitions
│   │   ├── gateway/gateway.go            # Gateway core logic
│   │   ├── adapter/local/local.go        # Local HTTP/WS adapter
│   │   └── clawdbot/client.go            # Moltbot client
│   ├── config.yaml                       # Default config
│   ├── Makefile                          # Build script
│   └── Dockerfile                        # Docker image
│
└── README.md                             # This file
```

## 📚 Documentation

| Document | Description |
|----------|-------------|
| [Protocol Spec](md/im_agnostic_clawdbot_adapter_architecture.md) | UIP protocol detailed definition |
| [Integration Guide](md/UIP_Gateway_Integration_Guide.md) | Complete integration steps and API reference |
| [Dev Journey](md/Development_Journey_Universal_Interaction.md) | Lessons learned + quick dev guide |
| [Team Sharing](md/Team_Share_Moltbot_Universal_IM_Vision.md) | Strategic value analysis |

## 🔗 Related Projects

| Project | Description |
|---------|-------------|
| [Moltbot (Fork)](https://github.com/zxs1633079383/moltbot) | AI Agent core, our Moltbot fork with universal-im |
| [Moltbot Official](https://github.com/moltbot/moltbot) | Official Moltbot repository |

## 🛠️ Development Commands

```bash
# Build
cd uip-gateway && make build

# Run (normal mode)
./bin/uip-gateway -config config.yaml

# Run (mock mode for testing)
./bin/uip-gateway -mock -config config.yaml

# Health check
curl http://localhost:8080/health

# View Moltbot logs
tail -f /tmp/moltbot/moltbot-$(date +%Y-%m-%d).log | grep "\[universal-im\]"
```

## 🗺️ Roadmap

- [x] UIP protocol design
- [x] Go Gateway implementation
- [x] Moltbot universal-im plugin integration
- [x] End-to-end verification
- [ ] WebSocket real-time push
- [ ] WeChat Work adapter
- [ ] Lark/Feishu adapter
- [ ] DingTalk adapter
- [ ] Kubernetes deployment

## 📄 License

MIT License

---

<p align="center">
  Made with ❤️ by <a href="https://github.com/zxs1633079383">zlc</a>
</p>
