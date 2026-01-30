# 🌐 Universal Interaction Protocol (UIP)

<p align="center">
  <strong>让任何 IM 都能接入 AI Agent 的通用协议</strong>
</p>

<p align="center">
  <a href="https://github.com/zxs1633079383/Universal_Interaction"><img src="https://img.shields.io/badge/GitHub-Universal_Interaction-181717?style=for-the-badge&logo=github" alt="GitHub"></a>
  <a href="https://github.com/zxs1633079383/moltbot"><img src="https://img.shields.io/badge/Moltbot-Fork-orange?style=for-the-badge&logo=github" alt="Moltbot"></a>
  <img src="https://img.shields.io/badge/Status-✅_MVP_Complete-success?style=for-the-badge" alt="Status">
  <img src="https://img.shields.io/badge/Lang-Go-00ADD8?style=for-the-badge&logo=go" alt="Go">
</p>

---

## 📖 项目简介

**Universal Interaction Protocol (UIP)** 是一套 IM 无关的交互协议，让任何即时通讯系统都能无缝接入 [Moltbot](https://github.com/zxs1633079383/moltbot) AI Agent。

> ✅ **状态**: MVP 完成，端到端通信已验证通过 (2026-01-30)

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  任意 IM    │────▶│ UIP Gateway │────▶│   Moltbot   │
│  微信/飞书/  │◀────│   (Go)      │◀────│  AI Agent   │
│  钉钉/Slack │     └─────────────┘     └─────────────┘
└─────────────┘           │
       │           Universal IM
       │            Protocol
       └──────────────────┘
```

## ✨ 核心特性

| 特性 | 说明 |
|------|------|
| 🔌 **IM 无关性** | 一套协议接入所有 IM，屏蔽平台差异 |
| 🚀 **高性能** | Go 实现的 Gateway，原生并发支持 |
| 🔄 **双向通信** | 支持同步请求和异步回调 |
| 🧩 **可扩展** | 插件化适配器架构 |
| 🤝 **Moltbot 深度集成** | 复用官方 `universal-im` 插件 |

## 🏗️ 架构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              数据流向                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   [用户 IM]  ──HTTP──▶  [UIP Gateway]  ──webhook──▶  [Moltbot]              │
│       │                      │                           │                   │
│       │                      │                           │ AI 处理           │
│       │                      │                           ▼                   │
│       │                      │ ◀────callback────  [AI Response]             │
│       │                      │                                               │
│       │ ◀────Intent────     │                                               │
│       ▼                      │                                               │
│   [显示响应]                 │                                               │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 🚀 快速开始

### 1. 克隆项目

```bash
git clone https://github.com/zxs1633079383/Universal_Interaction.git
cd Universal_Interaction/uip-gateway
```

### 2. 构建 Gateway

```bash
make build
```

### 3. 配置 Moltbot

确保你的 Moltbot 已配置 `universal-im` 插件。编辑 `~/.clawdbot/moltbot.json`：

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

### 4. 启动服务

```bash
# 终端 1: 启动 Moltbot Gateway
cd /path/to/moltbot
pnpm moltbot gateway

# 终端 2: 启动 UIP Gateway
cd /path/to/Universal_Interaction/uip-gateway
./bin/uip-gateway -config config.yaml
```

### 5. 测试

```bash
curl -X POST http://localhost:8080/api/v1/local/message \
  -H "Content-Type: application/json" \
  -d '{"sessionId": "test-001", "userId": "user1", "text": "你好"}'
```

## 📁 项目结构

```
Universal_Interaction/
├── md/                                    # 文档
│   ├── im_agnostic_clawdbot_adapter_architecture.md  # 协议规范
│   ├── UIP_Gateway_Integration_Guide.md              # 集成指南
│   ├── Development_Journey_Universal_Interaction.md  # 开发历程
│   └── Team_Share_Moltbot_Universal_IM_Vision.md     # 团队分享
│
├── uip-gateway/                           # Go Gateway 实现
│   ├── cmd/gateway/main.go               # 主程序入口
│   ├── internal/
│   │   ├── protocol/uip.go               # UIP 协议定义
│   │   ├── gateway/gateway.go            # 网关核心逻辑
│   │   ├── adapter/local/local.go        # 本地 HTTP/WS 适配器
│   │   └── clawdbot/client.go            # Moltbot 客户端
│   ├── config.yaml                       # 默认配置
│   ├── Makefile                          # 构建脚本
│   └── Dockerfile                        # Docker 镜像
│
└── README.md                             # 本文件
```

## 📚 文档

| 文档 | 说明 |
|------|------|
| [协议规范](md/im_agnostic_clawdbot_adapter_architecture.md) | UIP 协议详细定义 |
| [集成指南](md/UIP_Gateway_Integration_Guide.md) | 完整的集成步骤和 API 参考 |
| [开发历程](md/Development_Journey_Universal_Interaction.md) | 踩坑记录 + 快速开发指南 |
| [团队分享](md/Team_Share_Moltbot_Universal_IM_Vision.md) | 战略价值分析 |

## 🔗 相关项目

| 项目 | 说明 |
|------|------|
| [Moltbot](https://github.com/zxs1633079383/moltbot) | AI Agent 核心，本项目的 Moltbot Fork |
| [Moltbot 官方](https://github.com/moltbot/moltbot) | Moltbot 官方仓库 |

## 🛠️ 开发命令

```bash
# 构建
cd uip-gateway && make build

# 运行 (正常模式)
./bin/uip-gateway -config config.yaml

# 运行 (Mock 模式，用于测试)
./bin/uip-gateway -mock -config config.yaml

# 健康检查
curl http://localhost:8080/health

# 查看 Moltbot 日志
tail -f /tmp/moltbot/moltbot-$(date +%Y-%m-%d).log | grep "\[universal-im\]"
```

## 🗺️ 路线图

- [x] UIP 协议设计
- [x] Go Gateway 实现
- [x] Moltbot universal-im 插件集成
- [x] 端到端验证
- [ ] WebSocket 实时推送
- [ ] 企业微信适配器
- [ ] 飞书适配器
- [ ] 钉钉适配器
- [ ] Kubernetes 部署方案

## 📄 License

MIT License

---

<p align="center">
  Made with ❤️ by <a href="https://github.com/zxs1633079383">zlc</a>
</p>
