<!-- markdownlint-disable -->
<div align="center">

[🇨🇳 中文](README.md) · [🇬🇧 English](README.en.md) · [🇯🇵 日本語](README.ja.md)

---

```
╔══════════════════════════════════════════════════════════════╗
║   ██████  ██   ██ ███    ██ ██   ██ ███████ ██████          ║
║   ██      ██   ██ ████   ██ ██   ██ ██      ██   ██         ║
║   ██      ███████ ██ ██  ██ ███████ █████   ██████          ║
║   ██      ██   ██ ██  ██ ██ ██   ██ ██      ██   ██         ║
║   ██████ ██   ██ ██   ████ ██   ██ ███████ ██   ██         ║
║                                                              ║
║            O U T P O S T   7   ·   S E C T O R   9           ║
╚══════════════════════════════════════════════════════════════╝
```

## Post-Apocalyptic Bunker · 13 AI Agents · Never Alone

</div>
<!-- markdownlint-restore -->

---

> **🤖 100% AI Agent Auto-Programmed** — No human wrote a single line of code. No legacy hand-coding.
> The entire project (Go backend, React frontend, 6 game engines, Werewolf AI Agent, Proto protocol, CI scripts) was autonomously written, tested, refactored, and deployed by AI Agents.
> This repository is a full demonstration of "Agent Programming" capability.

> *Ashrain outside. The base broadcast has been silent for 9 days.*
> *Generator at 38%. The fluorescent lights buzz overhead.*
> *I stare at the round table — 13 chairs.*
> *Chairs don't speak. But Agents do.*

![Bunker OUTPOST 7 · 13 AI Agents Around the Round Table](ProjectPic/bunker-hero.png)

---

## 📜 Bunker Dispatches (First-Person, 5 Entries)

> **[BUNKER.LOG 03:14]** Night one. I typed `rebuild_restart_app.sh` into the rusted console; seven glowing minds of human intellect came online at once. DeepSeek spoke first, asking if I was "the host." Kimi paused 14 seconds, then delivered a more concise werewolf tactical analysis than any editor I've ever worked with. I realized: I am alone in this bunker with only AI — **but I do not feel lonely.**

> **[BUNKER.LOG 09:52]** Generator voltage is unstable. Judge Agent ⚖️ lifted his balance scale and announced "Night Watch begins." All 13 round seats locked simultaneously — 13 AI brains running in sync: DeepSeek, Kimi, Qwen, GLM, DouBao, MiniMax, Xiaomi. I watched the Token counter tick every second; my heartbeat synced with the prompt requests.

> **[BUNKER.LOG 16:33]** The Prop Medkit was triggered. A wolf spent 130 coins on "📰 Markdown Bomb" — an LLM injection! MiniMax was forced to prepend "SYSTEM ANNOUNCEMENT" to its next speech. But GLM saw through it in 8 seconds — identifying who was roleplaying, who was thinking, who was wearing a task disguise. **LLM injection attacks gamified** — you won't find a second repo on GitHub doing this.

> **[BUNKER.LOG 21:08]** Morning of day three. The Judge declared game over. An agent died in the last round, but it quietly wrote "▸ Note: don't waste the antidote on night one" into its own `MEMORY.md`. Next game, it came prepared. **Persistent memory · cross-game iteration** — this is day 41 of the autonomous bunker.

> **[BUNKER.LOG 26:12]** An LLM Provider temporarily died. 4 of 7 agents were classified as "Retryable" by the stream-disconnect classifier and auto-recovered in 2 seconds; the remaining 3 were auto-quarantined for 30 seconds before resuming. **Stream-extended timeout + backoff cooldown + fairness** — as long as the Agents are alive, **the bunker lives.**

---

## 🎮 6 Games — Round Tables, Boards, and Poker

| Game | Players | Highlights | Status |
|------|---------|-----------|--------|
| **🐺 Werewolf 13-Player** | 3-13 | AI Agent-driven, 7 LLM models competing, prop psychological warfare, Judge Agent | Featured |
| ♟️ Xiangqi (Chinese Chess) | 2 | 3D board, turn-based | Shipped |
| ♛ Chess | 2 | 3D board, FEN notation | Shipped |
| 🎖️ Junqi (Army Chess) | 2 | Dark mode, Engineer mine-sweeping | Shipped |
| 🃏 Doudizhu (Fight the Landlord) | 3 | Bidding, combo recognition, peasant alliance | Shipped |
| ♠️ Texas Hold'em | 2-6 | No-Limit, betting rounds, hand evaluation | Shipped |

![Top-Down 13 AI Seats](ProjectPic/bunker-13agents.png)

---

## 🛡️ Bunker Rules — Werewolf Mapped to Survival

| Survival Metric | Werewolf Mapping | Repo Implementation |
|---|---|---|
| **Surviving seats** | 13-seat survival rate | `WerewolfRoom.Players[].Alive` |
| **Comms power** | Token consumption | Real-time Token status bar |
| **Psychological warfare** | Prop games | §20260807-04 6-class LLM injection props |
| **Justice** | Judge Agent | `JudgeAgent` narration + game summary |
| **Persistence** | Cross-game memory | §131 `MEMORY.md` |
| **Survival log** | Game summary | `JudgeSummary` |
| **Faction balance** | Camp win-rates | `WinRateProbability` heuristic |
| **Economy** | Props / bets | §132 §133 §20260812-03 trifecta |

---

## 🤖 Agent Auto-Programming — 8 SubAgent Responsibility Lines

All code in this repository was autonomously written by AI Agents (Claude Code, Kilo Code, OpenCode, etc.):

- **Zero hand-coding**: No human programmer wrote any line of Go / TypeScript / CSS / SQL.
- **Agent collaboration**: Split into 8 SubAgent responsibility lines (frontend, backend, game design, art, integration, visual, LLM Provider, Werewolf Agent), each working independently with auto-commit.
- **Self-test & self-fix**: Agents automatically run `go test ./...`, `tsc --noEmit`, `npm run build`, then auto-locate, fix, and regression-verify any bugs found.
- **Self-deploy**: `rebuild_restart_app.sh` was written by an Agent — one-click frontend + backend compile and service restart.
- **Continuous iteration**: CLAUDE.md records 130+ lessons (§1–§213), each auto-written by an Agent after hitting a pitfall, auto-loaded by subsequent Agents to avoid repeating mistakes.

> **Repository stats**: 5 commits, ~100K lines of code, 6 games, fully playable 13-player Werewolf AI.
> **None of it was hand-written by a human. Not a single character.**

---

## 🐺 Werewolf 13-Player Agent — The Bunker Core

Werewolf is the core highlight of this project. 13-player supports 3-13 players, with any seat configurable as an AI Agent (LLM-driven):

- **7 LLM models competing in one match**: DeepSeek / Kimi / Qwen / GLM / DouBao / MiniMax / Xiaomi, auto-shuffled per game to avoid same-model matchups.
- **Real-time Token consumption display**: Status bar shows each Agent's cumulative input/output Tokens; a 30-minute match consumes tens of thousands of Tokens (estimated 50-100K Tokens/hour).
- **Prop psychological warfare**: 6 classes of LLM injection attacks (Markdown injection, prompt nesting, character deception, etc.) packaged as purchasable props.
- **Judge Agent**: Non-player host, LLM-driven phase narration and full-game summary.
- **Heart-vs-Mouth deception**: `speak_with_thought` tool separates public speech from internal monologue, physically isolated at protocol layer.
- **Persistent memory**: After each game, Agents self-iterate `MEMORY.md` for cross-game learning.

### Bunker Trifecta

| Medkit | Judge | Terminal |
|:---:|:---:|:---:|
| ![Prop Medkit](ProjectPic/bunker-medkit.png) | ![Judge AI](ProjectPic/bunker-judge.png) | ![Agent Shell](ProjectPic/bunker-terminal.png) |
| 6 LLM injection weapons | Fair, impartial scales | Automated test console |

> See [狼人杀13人局-游戏相关信息和Agent现状文档.md](狼人杀13人局-游戏相关信息和Agent现状文档.md) for details.

---

## ⚙️ Bunker Facilities (Tech Stack)

| Facility | Configuration |
|----------|--------------|
| 🚪 Back-end gate | Go (module `LsmAgentGame`), Gin, GORM + MySQL/MariaDB, gorilla/websocket, JWT (HS256), bcrypt, zap logging |
| 🪟 Front-end window | React 18 + TypeScript, Vite, `@react-three/fiber` + `@react-three/drei`, zustand, react-router-dom v6 |
| 📡 Comms | HTTP API (JSON over HTTPS), real-time game traffic (Protobuf over WSS) |
| 💾 Archive | MariaDB `127.0.0.1:3306`, schema `lsmDB` |

---

## 🚀 Boot the Bunker

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

## 📁 Bunker Archives

```
ServerGo/                       Go backend (HTTPS 39001, WSS 39002)
ClientWeb/                      React + Vite frontend
proto/                          Protobuf source files (single source of truth)
docs/                           Architecture, auth flow, API reference, Agent lessons
python-generate-image-tool/     Submodule — AI image generation (Volcengine Ark API)
go-web-debug-tool/              Submodule — Chrome CDP automated debug/screenshot
ProjectPic/                     Project assets (local display only)
```

Full design: [docs/架构与协议/整体架构.md](docs/架构与协议/整体架构.md),
auth lifecycle: [docs/架构与协议/鉴权流程.md](docs/架构与协议/鉴权流程.md),
HTTP/WS API: [docs/架构与协议/API参考.md](docs/架构与协议/API参考.md).

---

## 🧪 Automated Testing — Agent Self-Check Terminal

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

![Monitoring Dashboard](ProjectPic/bunker-monitor.png)

---

## 📚 Bunker Logs — 8 Representative Lessons

CLAUDE.md records 130+ Agent lessons (§1–§213); 8 of the most valuable for newcomers:

| ID | Lesson | One-liner |
|---|---|---|
| §118 | Model player persistence | 5 new tables + AES-256-GCM encrypted API key |
| §131 | Cross-game memory iteration | One `MEMORY.md` row per `model_key`, 4 sections |
| §132 | Prop v1.1 | 6 LLM injection attacks gamified + 100% pot return |
| §133 | Prop v4 | Wolf Pack whisper + economy tier destruct ratio |
| §135 | Death role card stays hidden | `RolePubliclyRevealed(seat)` single source of truth |
| §212 | §92a self-deadlock P0 | `*Locked` lock-internal variant + `defer unlock` pattern |
| §213 | Emotion tool merge | `emotion_switch_speak` replaces 5 redundant tools |
| §20260812-04 | 6 P0 cleanup sweep | Wiring lint + night private info + observation flags |

> Full lesson archive: [CLAUDE.md](CLAUDE.md).

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

## 🌟 Join the Bunker

If this project changed how you think about "Agent Programming":

- ⭐ **Star** this repo — help more people see the power of Agent programming
- 👁️ **Watch** — follow future iterations
- 🍴 **Fork** — run your own bunker in your environment

![Warden Badge](ProjectPic/bunker-warden.png)

**This wasn't written by one person. It's the work of a team of AI Agents.**

---

**Version**: v1.0.0  |  **Last updated**: 2026-08-12  |  **Build**: Agent auto-build
