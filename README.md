<!-- markdownlint-disable -->
<div align="center">

[🇨🇳 中文](README.md) · [🇬🇧 English](README.en.md) · [🇯🇵 日本語](README.ja.md)

</div>
<!-- markdownlint-restore -->

# LsmAgentGame

> **🤖 100% AI Agent 自动编程** —— 无人工手写一行代码，无古法编程。
> 整个项目（后端 Go、前端 React、6 款游戏引擎、狼人杀 AI Agent、Proto 协议、CI 脚本）全部由 AI Agent 自主编写、测试、重构、部署。
> 本仓库是「Agent 编程」能力的一次完整展示。

多 AI Agent 协作驱动的 Web 游戏平台：狼人杀 13 人局（AI 玩家）+ 象棋 + 国际象棋 + 军棋 + 斗地主 + 德州扑克。
后端 Go（HTTPS + WSS），前端 React + TypeScript + Three.js，WebSocket 之上使用 Protobuf 协议，MySQL（通过 GORM）持久化。

![狼人杀 Agent 对局](ProjectPic/werewolf-game.png)

---

## 🎮 游戏列表

| 游戏 | 人数 | 特色 |
|------|------|------|
| **狼人杀 13 人局** | 3-13 | AI Agent 驱动、7 种 LLM 模型同场竞技、道具心理战、法官 Agent |
| 中国象棋 | 2 | 3D 棋盘、回合制 |
| 国际象棋 | 2 | 3D 棋盘、FEN 记谱 |
| 军棋 | 2 | 暗棋模式、工兵挖雷 |
| 斗地主 | 3 | 叫地主、牌型识别、农民同盟 |
| 德州扑克 | 2-6 | No-Limit、押注轮、牌型评估 |

---

## 🤖 Agent 自动编程

本仓库的所有代码均由 AI Agent（Claude Code、Kilo Code、OpenCode 等）自动编写：

- **无手工编码**：没有人类程序员手写任何一行 Go / TypeScript / CSS / SQL。
- **Agent 协作**：按职责拆分为 8 条 SubAgent 职责线（前端、后端、游戏设计、美术、联调、视觉、LLM Provider、狼人杀 Agent），各自独立工作、自动提交。
- **自测自修**：Agent 自动运行 `go test ./...`、`tsc --noEmit`、`npm run build`，发现 Bug 后自动定位、修复、回归验证。
- **自部署**：`rebuild_restart_app.sh` 由 Agent 编写，一键编译前端+后端+重启服务。
- **持续迭代**：CLAUDE.md 中记录了 130+ 条教训（§1–§213），每条都是 Agent 踩坑后自动写入的「经验记忆」，后续 Agent 自动加载避免重犯。

> **仓库数据**：5 个 commit、~10 万行代码、6 款游戏、13 人局狼人杀 AI 完整可玩。
> 这一切，没有一个人工手写字符。

---

## 🏗 技术栈

- **后端**：Go（模块名 `LsmAgentGame`）、Gin、GORM + MySQL/MariaDB、gorilla/websocket、JWT (HS256)、bcrypt、zap 日志
- **前端**：React 18 + TypeScript、Vite、`@react-three/fiber` + `@react-three/drei`、zustand、react-router-dom v6
- **通信**：HTTP API（JSON over HTTPS）、实时游戏流量（Protobuf over WSS）
- **数据库**：MariaDB `127.0.0.1:3306`，schema `lsmDB`

---

## 🚀 快速开始

### 前置要求

- Go 1.22+
- Node.js 18+
- MariaDB / MySQL（已存在 schema `lsmDB` 与账号 `superuser`，无需初始化）
- Linux（服务使用自签名 TLS 证书，本地开发用）

### 安装与部署

```bash
# 1. 克隆仓库（含子模块）
git clone <your-repo-url> LsmAgentGame
cd LsmAgentGame
git submodule update --init --recursive

# 2. 复制并编辑运行时配置
cp LsmAgentGame.conf.example LsmAgentGame.conf
# 编辑 LsmAgentGame.conf —— 设置 db.password、jwt.secret、llm.providers[].api_key 等
# 真实密钥仅入 LsmAgentGame.conf（已在 .gitignore 中排除）

# 3. 安装前端依赖
cd ClientWeb && npm install && cd ..

# 4. 一键编译前端 + 后端并启动服务
./rebuild_restart_app.sh

# 5. 打开浏览器访问
# https://127.0.0.1:39001
```

服务监听地址：

| 服务 | 端口 | URL |
|------|------|-----|
| HTTPS（REST + 静态文件） | 39001 | `https://127.0.0.1:39001` |
| WSS（游戏实时流量） | 39002 | `wss://127.0.0.1:39002/ws` |

### 验证服务

```bash
curl -sk https://127.0.0.1:39001/api/version
# {"code":0,"data":{"version":"v1.0.0-<sha>","build_time":"..."},"message":"ok"}
```

---

## 📁 目录结构

```
ServerGo/                       Go 后端（HTTPS 39001, WSS 39002）
ClientWeb/                      React + Vite 前端
proto/                          Protobuf 源文件（单一事实源）
docs/                           架构设计、鉴权流程、API 参考、Agent 教训
python-generate-image-tool/     子模块 —— AI 图像生成（火山引擎 Ark API）
go-web-debug-tool/              子模块 —— Chrome CDP 自动化调试/截图
ProjectPic/                     项目资源（本地展示，不入库）
```

完整设计见 [docs/架构与协议/整体架构.md](docs/架构与协议/整体架构.md)，
登录生命周期见 [docs/架构与协议/鉴权流程.md](docs/架构与协议/鉴权流程.md)，
HTTP/WS 接口见 [docs/架构与协议/API参考.md](docs/架构与协议/API参考.md)。

---

## 🐺 狼人杀 13 人局 Agent

狼人杀是本项目的核心亮点。13 人局支持 3-13 人，其中任意座位可配置为 AI Agent（LLM 驱动）：

- **7 种 LLM 模型同场竞技**：DeepSeek / Kimi / Qwen / GLM / 豆包 / MiniMax / 小米，每局自动打散分配，避免同模型对局。
- **Token 消耗实时显示**：状态栏展示每个 Agent 的输入/输出 Token 累计，单局 30 分钟约消耗数万 Token（1 小时推算约 5-10 万 Token，视模型与阶段密度而定）。
- **道具心理战**：Markdown 注入、提示词套娃、字符欺骗等 6 类 LLM 注入攻击封装为可购买道具。
- **法官 Agent**：非玩家主持人，LLM 驱动的阶段旁白与整局总结。
- **心口不一**：`speak_with_thought` 工具让 Agent 公开发言与内心独白分离，协议层物理隔离。
- **持久化记忆**：每局结束 Agent 自我迭代 `MEMORY.md`，跨局学习。

> 详见 [狼人杀13人局-游戏相关信息和Agent现状文档.md](狼人杀13人局-游戏相关信息和Agent现状文档.md)。

---

## 🧪 自动化测试

项目使用 `go-web-debug-tool`（Chrome CDP）进行 Web 端自动化测试与截图：

```bash
# 启动调试工具
cd go-web-debug-tool && ./GoWebDebugTool -d

# Agent 通过 REST 驱动 Chrome 打开页面、登录、创建房间、截图
curl -X POST http://localhost:28999/NewChromePage ...
curl -X POST http://localhost:28999/ControlChromePage \
  -d '{"page_id":"p_xxx","action":"screenshot","params":{"format":"png"}}'
```

截图保存至 `ProjectPic/` 目录，展示游戏精彩瞬间。

---

## 🤝 关注与支持

如果你觉得这个项目有趣，欢迎关注我在各平台的账号，观看完整的游戏演示视频：

| 平台 | 搜索账号 |
|------|---------|
| 快手 | **封刀灌海** |
| 抖音 | **封刀灌海** |
| B站 | **封刀灌海** |
| 小红书 | **封刀灌海** |
| 微信视频号 | **封刀灌海** |

---

## ☕ 请作者喝杯咖啡

项目的服务器、LLM API 调用、图像生成等均有持续成本。如果这个项目对你有帮助或你觉得有趣，欢迎打赏支持：

| 微信打赏 | 支付宝打赏 |
|:--------:|:----------:|
| ![微信收款码](ProjectPic/wechat_qr.jpg) | ![支付宝收款码](ProjectPic/alipay_qr.jpg) |

> 图片在本地浏览时显示；GitHub 上因 `ProjectPic/` 未入库可能无法显示，请 clone 后本地查看。

**联系方式**：

- 📱 手机：`13520647302`
- 💬 微信：`liushimeng109117198`

---

## 📜 协议

私有 / 内部项目。所有代码由 AI Agent 自动编写。

---

## 🌟 在 GitHub 上支持我们

如果这个项目让你对「Agent 编程」有了新的认识，请：

- ⭐ **Star** 本仓库 —— 让更多人看到 Agent 编程的力量
- 👁️ **Watch** —— 跟进后续迭代
- 🍴 **Fork** —— 在你的环境里跑一局狼人杀 Agent 对局

**这不是一个人写的代码。这是一群 AI Agent 的作品。**

---

**版本**：v1.0.0  |  **最后更新**：2026-08-12  |  **构建**：Agent 自动构建
