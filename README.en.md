<!-- markdownlint-disable -->
<div align="center">

[🇨🇳 中文](README.md) · [🇬🇧 English](README.en.md) · [🇯🇵 日本語](README.ja.md)

</div>
<!-- markdownlint-restore -->

# LsmAgentGame

> **🤖 100% AI Agent Auto-Programmed** — No human wrote a single line of code. No legacy hand-coding.
> The entire project (Go backend, React frontend, 6 game engines, Werewolf AI Agent, Proto protocol, CI scripts) was autonomously written, tested, refactored, and deployed by AI Agents.
> This repository is a full demonstration of "Agent Programming" capability.

A multi-AI-Agent-driven web game platform: Werewolf 13-player (AI-driven) + Xiangqi + Chess + Junqi + Doudizhu + Texas Hold'em.
Backend in Go (HTTPS + WSS), frontend in React + TypeScript + Three.js, Protobuf over WebSocket, MySQL (via GORM) persistence.

![Werewolf Agent Match](ProjectPic/werewolf-game.png)

---

## 🎮 Games

| Game | Players | Highlights |
|------|---------|-----------|
| **Werewolf 13-Player** | 3-13 | AI Agent-driven, 7 LLM models competing, prop psychological warfare, Judge Agent |
| Xiangqi (Chinese Chess) | 2 | 3D board, turn-based |
| Chess | 2 | 3D board, FEN notation |
| Junqi (Army Chess) | 2 | Dark mode, Engineer mine-sweeping |
| Doudizhu (Fight the Landlord) | 3 | Bidding, combo recognition, peasant alliance |
| Texas Hold'em | 2-6 | No-Limit, betting rounds, hand evaluation |

---

## 🤖 Agent Auto-Programming

All code in this repository was autonomously written by AI Agents (Claude Code, Kilo Code, OpenCode, etc.):

- **Zero hand-coding**: No human programmer wrote any line of Go / TypeScript / CSS / SQL.
- **Agent collaboration**: Split into 8 SubAgent responsibility lines (frontend, backend, game design, art, integration, visual, LLM Provider, Werewolf Agent), each working independently with auto-commit.
- **Self-test & self-fix**: Agents automatically run `go test ./...`, `tsc --noEmit`, `npm run build`, then auto-locate, fix, and regression-verify any bugs found.
- **Self-deploy**: `rebuild_restart_app.sh` was written by an Agent — one-click frontend + backend compile and service restart.
- **Continuous iteration**: CLAUDE.md records 130+ lessons (§1–§213), each auto-written by an Agent after hitting a pitfall, auto-loaded by subsequent Agents to avoid repeating mistakes.

> **Repository stats**: 5 commits, ~100K lines of code, 6 games, fully playable 13-player Werewolf AI.
> None of it was hand-written by a human. Not a single character.

---

## 🏗 Tech Stack

- **Backend**: Go (module `LsmAgentGame`), Gin, GORM + MySQL/MariaDB, gorilla/websocket, JWT (HS256), bcrypt, zap logging
- **Frontend**: React 18 + TypeScript, Vite, `@react-three/fiber` + `@react-three/drei`, zustand, react-router-dom v6
- **Communication**: HTTP API (JSON over HTTPS), real-time game traffic (Protobuf over WSS)
- **Database**: MariaDB `127.0.0.1:3306`, schema `lsmDB`

---

## 🚀 Quick Start

### Prerequisites

- Go 1.22+
- Node.js 18+
- MariaDB / MySQL (schema `lsmDB` and user `superuser` already exist, no init needed)
- Linux (service uses self-signed TLS cert, for local dev)

### Install & Deploy

```bash
# 1. Clone the repository (with submodules)
git clone <your-repo-url> LsmAgentGame
cd LsmAgentGame
git submodule update --init --recursive

# 2. Copy and edit runtime config
cp LsmAgentGame.conf.example LsmAgentGame.conf
# Edit LsmAgentGame.conf — set db.password, jwt.secret, llm.providers[].api_key, etc.
# Real secrets go only in LsmAgentGame.conf (excluded via .gitignore)

# 3. Install frontend dependencies
cd ClientWeb && npm install && cd ..

# 4. One-click compile frontend + backend and start
./rebuild_restart_app.sh

# 5. Open your browser
# https://127.0.0.1:39001
```

Service endpoints:

| Service | Port | URL |
|---------|------|-----|
| HTTPS (REST + static files) | 39001 | `https://127.0.0.1:39001` |
| WSS (real-time game traffic) | 39002 | `wss://127.0.0.1:39002/ws` |

### Verify

```bash
curl -sk https://127.0.0.1:39001/api/version
# {"code":0,"data":{"version":"v1.0.0-<sha>","build_time":"..."},"message":"ok"}
```

---

## 📁 Directory Structure

```
ServerGo/                       Go backend (HTTPS 39001, WSS 39002)
ClientWeb/                      React + Vite frontend
proto/                          Protobuf source files (single source of truth)
docs/                           Architecture, auth flow, API reference, Agent lessons
python-generate-image-tool/     Submodule — AI image generation (Volcengine Ark API)
go-web-debug-tool/              Submodule — Chrome CDP automated debug/screenshot
ProjectPic/                     Project assets (local display only, not committed)
```

Full design: [docs/架构与协议/整体架构.md](docs/架构与协议/整体架构.md),
auth lifecycle: [docs/架构与协议/鉴权流程.md](docs/架构与协议/鉴权流程.md),
HTTP/WS API: [docs/架构与协议/API参考.md](docs/架构与协议/API参考.md).

---

## 🐺 Werewolf 13-Player Agent

Werewolf is the core highlight of this project. 13-player supports 3-13 players, with any seat configurable as an AI Agent (LLM-driven):

- **7 LLM models competing in one match**: DeepSeek / Kimi / Qwen / GLM / DouBao / MiniMax / Xiaomi, auto-shuffled per game to avoid same-model matchups.
- **Real-time Token consumption display**: Status bar shows each Agent's cumulative input/output Tokens; a 30-minute match consumes tens of thousands of Tokens (estimated 50-100K Tokens/hour depending on model and phase density).
- **Prop psychological warfare**: 6 classes of LLM injection attacks (Markdown injection, prompt nesting, character deception, etc.) packaged as purchasable props.
- **Judge Agent**: Non-player host, LLM-driven phase narration and full-game summary.
- **Heart-vs-Mouth deception**: `speak_with_thought` tool separates public speech from internal monologue, physically isolated at protocol layer.
- **Persistent memory**: After each game, Agents self-iterate `MEMORY.md` for cross-game learning.

> See [狼人杀13人局-游戏相关信息和Agent现状文档.md](狼人杀13人局-游戏相关信息和Agent现状文档.md) for details.

---

## 🧪 Automated Testing

The project uses `go-web-debug-tool` (Chrome CDP) for web-side automated testing and screenshots:

```bash
# Start the debug tool
cd go-web-debug-tool && ./GoWebDebugTool -d

# Agent drives Chrome via REST: open page, login, create room, screenshot
curl -X POST http://localhost:28999/NewChromePage ...
curl -X POST http://localhost:28999/ControlChromePage \
  -d '{"page_id":"p_xxx","action":"screenshot","params":{"format":"png"}}'
```

Screenshots are saved to `ProjectPic/`, showcasing exciting game moments.

---

## 🤝 Follow & Support

If you find this project interesting, follow my accounts on these platforms for full gameplay demo videos:

| Platform | Search |
|----------|--------|
| Kuaishou | **封刀灌海** |
| Douyin | **封刀灌海** |
| Bilibili | **封刀灌海** |
| Xiaohongshu | **封刀灌海** |
| WeChat Channels | **封刀灌海** |

---

## ☕ Buy the Author a Coffee

The project incurs ongoing costs for servers, LLM API calls, and image generation. If this project helped you or you find it interesting, tips are welcome:

| WeChat Tip | Alipay Tip |
|:----------:|:----------:|
| ![WeChat QR](ProjectPic/wechat_qr.jpg) | ![Alipay QR](ProjectPic/alipay_qr.jpg) |

> Images display when browsing locally; may not render on GitHub since `ProjectPic/` is not committed — clone and view locally.

**Contact**:

- 📱 Phone: `13520647302`
- 💬 WeChat: `liushimeng109117198`

---

## 📜 License

Private / internal project. All code auto-written by AI Agents.

---

## 🌟 Star on GitHub

If this project changed how you think about "Agent Programming":

- ⭐ **Star** this repo — help more people see the power of Agent programming
- 👁️ **Watch** — follow future iterations
- 🍴 **Fork** — run a Werewolf Agent match in your own environment

**This wasn't written by one person. It's the work of a team of AI Agents.**

---

**Version**: v1.0.0  |  **Last updated**: 2026-08-12  |  **Build**: Agent auto-build
