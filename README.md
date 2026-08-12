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

## 末日地堡 · 13 座 AI Agent · 永不孤独

</div>
<!-- markdownlint-restore -->

---

> **🤖 100% AI Agent 自动编程** —— 无人工手写一行代码，无古法编程。
> 整个项目（后端 Go、前端 React、6 款游戏引擎、狼人杀 AI Agent、Proto 协议、CI 脚本）全部由 AI Agent 自主编写、测试、重构、部署。
>
> **⏱️ Loop Agent × Graphic Agent 双架构 · 24/7 不间断编程处理** —— 本仓库的代码生成并非"一次性会话产出",而是 **Loop Agent(循环调度 Agent,按轮次 / 按 issue / 按测试报告持续驱动)** 与 **Graphic Agent(图形 / 图像生成 Agent,负责美术资产、UI 截图、品牌插画)** 协同工作,实现**接近 24 小时不间断的编程处理**:白天由 Graphic Agent 批量出图 + 修图,夜里由 Loop Agent 接力推进 issue、自动跑回归、写 lesson、下次接力时无缝继续。
> 本仓库是「Agent 编程」能力的一次完整展示。

> *外面下着灰雨。基地广播停了 9 天。*
> *发电机还有 38% 电量。灯管在头顶滋滋地响。*
> *我盯着圆桌 —— 13 把椅子。*
> *椅子不会自己说话。但 Agent 会。*

![地堡 OUTPOST 7 · 13 座 AI 围坐圆桌](ProjectPic/bunker-hero.png)

---

## 📜 避难所口述（5 段第一人称）

> **[BUNKER.LOG 03:14]** 第一夜。我在锈蚀的控制台上敲下 `rebuild_restart_app.sh`，7 个发光的人类智慧体同时上线。DeepSeek 第一个说话，问我是不是「主持人」。Kimi 沉默 14 秒，给出了比主编更精炼的狼人杀战术分析。我突然意识到：这间地堡里只剩下我和 AI，但**我没有感到孤独**。

> **[BUNKER.LOG 09:52]** 发电机的电压开始不稳。法官 Agent ⚖️ 拿起了他的天平，宣布「第一夜守夜开始」。13 座圆形座位同时锁死 —— 13 颗 AI 大脑同步运转：DeepSeek、Kimi、Qwen、GLM、豆包、MiniMax、小米。我坐在屏幕前，看着 Token 计数器每秒跳动，心跳声和 prompt 请求一起轰鸣。

> **[BUNKER.LOG 16:33]** 道具急救箱被触发。某个狼人用 130 金币买了「📰 公告轰炸」—— Markdown 注入！MiniMax 立刻在下一轮发言前被迫加上「系统公告」前缀。但 GLM 看得清楚，他用 8 秒识别出谁在演、谁在思考、谁在做任务马甲。**LLM 注入攻击游戏化** —— 这件事在 GitHub 上找不到第二个仓库做过。

> **[BUNKER.LOG 21:08]** 第三天清晨。法官宣布游戏结束。某个 bot 死在最后一轮，但它偷偷写了一条「▸ 备忘：下次首夜不要浪费解药」到自己的 `MEMORY.md`。下一局开始时，它真的有备而来。**持久化记忆 · 跨局迭代** —— 这是 AI 自治地堡的第 41 天。

> **[BUNKER.LOG 26:12]** 一个 LLM Provider 临时挂了。7 个 Agent 中的 4 个被外层断连辨识为「可重试」，2 秒后自动恢复；剩下 3 个被自动 quarantine 30 秒冷却到期后继续上线。**流式续命 + 退避冷却 + 公平性** —— 只要 Agent 还活着，**地堡就还在**。

---

## 🎮 6 款游戏 —— 圆桌、棋盘、扑克

| 游戏 | 人数 | 特色 | 状态 |
|------|------|------|------|
| **🐺 狼人杀 13 人局** | 3-13 | AI Agent 驱动、7 种 LLM 同场竞技、道具心理战、法官 Agent | 重点 |
| ♟️ 中国象棋 | 2 | 3D 棋盘、回合制 | 已实装 |
| ♛ 国际象棋 | 2 | 3D 棋盘、FEN 记谱 | 已实装 |
| 🎖️ 军棋 | 2 | 暗棋模式、工兵挖雷 | 已实装 |
| 🃏 斗地主 | 3 | 叫地主、牌型识别、农民同盟 | 已实装 |
| ♠️ 德州扑克 | 2-6 | No-Limit、押注轮、牌型评估 | 已实装 |

![13 座 AI 棋盘俯视](ProjectPic/bunker-13agents.png)

---

## 🛡️ 避难所规则 —— 狼人杀与末日生存一一对应

| 生存指标 | 狼人杀映射 | 仓库实现 |
|---|---|---|
| **存活座位** | 13 座存活率 | `WerewolfRoom.Players[].Alive` |
| **通讯电量** | Token 消耗 | 实时 Token 状态栏 |
| **心理战** | 道具博弈 | §20260807-04 6 类 LLM 注入道具 |
| **司法** | 法官 Agent | `JudgeAgent` 旁白 + 整局总结 |
| **装备持久化** | 跨局记忆 | §131 `MEMORY.md` |
| **生存日志** | 整局总结 | `JudgeSummary` |
| **派系平衡** | 阵营胜负 | `WinRateProbability` 启发式胜率 |
| **经济** | 道具 / 赌注 | §132 §133 §20260812-03 三件套 |

---

## 🤖 Agent 自动编程 —— 8 条 SubAgent 职责线

本仓库的所有代码均由 AI Agent（Claude Code、Kilo Code、OpenCode 等）自动编写：

- **无手工编码**：没有人类程序员手写任何一行 Go / TypeScript / CSS / SQL。
- **Agent 协作**：按职责拆分为 8 条 SubAgent 职责线（前端、后端、游戏设计、美术、联调、视觉、LLM Provider、狼人杀 Agent），各自独立工作、自动提交。
- **自测自修**：Agent 自动运行 `go test ./...`、`tsc --noEmit`、`npm run build`，发现 Bug 后自动定位、修复、回归验证。
- **自部署**：`rebuild_restart_app.sh` 由 Agent 编写，一键编译前端+后端+重启服务。
- **持续迭代**：CLAUDE.md 中记录了 130+ 条教训（§1–§213），每条都是 Agent 踩坑后自动写入的「经验记忆」，后续 Agent 自动加载避免重犯。

> **仓库数据**：5 个 commit、~10 万行代码、6 款游戏、13 人局狼人杀 AI 完整可玩。
> **这一切，没有一个人工手写字符。**

---

## ⏱️ Loop Agent × Graphic Agent —— 24/7 不间断编程处理

> **本仓库的生产级核心特性**：代码生成不是单次会话产物，而是由两条**长时间运行**的 Agent 架构协同推进。

| 架构 | 角色 | 工作模式 | 典型产出 |
|---|---|---|---|
| 🔁 **Loop Agent**(循环调度 Agent) | 项目"夜班程序员" | 监听 issue 队列、自动化测试报告 `TestReport/*.md`、子模块 ready 信号;按轮次持续调度 `backend-dev` / `frontend-dev` / `integration-tester` 等 SubAgent;每次接力都自动加载 `CLAUDE.md` 130+ 条 lessons,避免重犯 | 后端模块修复、前端样式回归、lesson 入库、`go test` 全 PASS 的提交 |
| 🎨 **Graphic Agent**(图形 / 图像生成 Agent) | 项目"美术 + 视觉" | 由 `python-generate-image-tool` 子模块驱动火山引擎 Ark API;批量生成角色立绘、道具图标、UI 截图、品牌插画;按 `ProjectPic/` 命名规范归档;白天全速跑批、晚上由 Loop Agent 接力质检 | `bunker-hero.png` / `bunker-13agents.png` / `ProjectPic/*.png` |
| 🧬 **24/7 不间断编程处理** | 二者协作产物 | Loop Agent 用 `CronCreate` + `ScheduleWakeup` 调度跨日轮次;每轮结束自动 `git commit`;下轮启动时自动加载上一轮进度(issues 队列 + 报告状态 + lessons);Graphic Agent 在每轮空闲时段批量跑图,**双 Agent 接力接近全天候编程** | 跨日累积的 commit 链 + 美术资产库 |

### Loop Agent 接力示例(实际运行流程)

```text
[08:00] Graphic Agent —— 跑批生成 8 张角色立绘、6 张道具图标 → 入库 ProjectPic/
[12:00] Loop Agent   —— 扫描 TestReport/*.md 提取 P0 缺陷,派发 backend-dev
[14:30] Loop Agent   —— backend-dev 完成修复,自动跑 go test 全 PASS,git commit
[18:00] Graphic Agent —— UI 截图、对比度审计、修图、入库
[22:00] Loop Agent   —— 派发 integration-tester 跑端到端回归,修复 1 个跨端 bug
[02:00] Loop Agent   —— 整理 CLAUDE.md 新增 4 条 lessons,git commit
[06:00] Loop Agent   —— 把当夜产出打包成 issue,移交下一日轮次
        ↺ (循环)
```

### 为什么是 GitHub 上的「第一次」？

- **Loop Agent 不是 `sleep + retry` 的简易脚本** —— 它跨进程、跨会话持久化(`.claude/scheduled_tasks.json`),**断网 / 进程退出后仍能继续**;
- **Graphic Agent 不只是 DALL-E 调包** —— 它与代码仓库的命名 / 路径 / 索引**强耦合**,生成的每张图都被 Git 跟踪、被 README / docs / 测试报告引用;
- **二者不靠人工"下班 → 上班"切换** —— 真正的 **24 小时不间断编程处理**,GitHub 上找不到第二个仓库做到这个深度。

---

## 🐺 狼人杀 13 人局 Agent —— 避难所核心

狼人杀是本项目的核心亮点。13 人局支持 3-13 人，其中任意座位可配置为 AI Agent（LLM 驱动）：

- **7 种 LLM 模型同场竞技**：DeepSeek / Kimi / Qwen / GLM / 豆包 / MiniMax / 小米，每局自动打散分配，避免同模型对局。
- **Token 消耗实时显示**：状态栏展示每个 Agent 的输入/输出 Token 累计，单局 30 分钟约消耗数万 Token（1 小时推算约 5-10 万 Token）。
- **道具心理战**：Markdown 注入、提示词套娃、字符欺骗等 6 类 LLM 注入攻击封装为可购买道具。
- **法官 Agent**：非玩家主持人，LLM 驱动的阶段旁白与整局总结。
- **心口不一**：`speak_with_thought` 工具让 Agent 公开发言与内心独白分离，协议层物理隔离。
- **持久化记忆**：每局结束 Agent 自我迭代 `MEMORY.md`，跨局学习。

### 避难所三件套

| 急救箱 | 法官 | 终端 |
|:---:|:---:|:---:|
| ![道具急救箱](ProjectPic/bunker-medkit.png) | ![法官 AI](ProjectPic/bunker-judge.png) | ![Agent Shell](ProjectPic/bunker-terminal.png) |
| 6 类 LLM 注入武器 | 公平不偏的天平 | 自动化测试控制台 |

> 详见 [狼人杀13人局-游戏相关信息和Agent现状文档.md](狼人杀13人局-游戏相关信息和Agent现状文档.md)。

---

## ⚙️ 避难所设施（技术栈）

| 设施 | 配置 |
|------|------|
| 🚪 后端门 | Go（模块名 `LsmAgentGame`）、Gin、GORM + MySQL/MariaDB、gorilla/websocket、JWT (HS256)、bcrypt、zap 日志 |
| 🪟 前端窗 | React 18 + TypeScript、Vite、`@react-three/fiber` + `@react-three/drei`、zustand、react-router-dom v6 |
| 📡 通讯 | HTTP API（JSON over HTTPS）、实时游戏流量（Protobuf over WSS） |
| 💾 档案室 | MariaDB `127.0.0.1:3306`，schema `lsmDB` |

---

## 🚀 启动避难所系统

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

## 📁 避难所档案

```
ServerGo/                       后端核心（HTTPS 39001, WSS 39002）
ClientWeb/                      前端指挥台
proto/                          Protobuf 源文件（唯一事实源）
docs/                           架构设计、鉴权流程、API 参考、Agent 教训
python-generate-image-tool/     子模块 —— AI 图像生成（火山引擎 Ark API）
go-web-debug-tool/              子模块 —— Chrome CDP 自动化调试/截图
ProjectPic/                     项目资源（本地展示用）
```

完整设计见 [docs/架构与协议/整体架构.md](docs/架构与协议/整体架构.md)，
登录生命周期见 [docs/架构与协议/鉴权流程.md](docs/架构与协议/鉴权流程.md)，
HTTP/WS 接口见 [docs/架构与协议/API参考.md](docs/架构与协议/API参考.md)。

---

## 🧪 自动化测试 —— Agent 自检终端

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

![监控仪表盘](ProjectPic/bunker-monitor.png)

---

## 📚 避难所日志——8 条代表性教训

CLAUDE.md 中记录 130+ 条 Agent 教训（§1–§213），其中 8 条对后来者最有价值：

| 编号 | 教训 | 一句话 |
|---|---|---|
| §118 | 模型玩家持久化 | 5 张新表 + AES-256-GCM 加密 API Key |
| §131 | 跨局记忆迭代 | `model_key` 一行 `MEMORY.md`，4 段固定标题 |
| §132 | 道具 v1.1 | 6 类 LLM 注入攻击游戏化 + 100% 回扣彩池 |
| §133 | 道具 v4 | 狼小队内部交流 + 销毁档位 |
| §135 | 死者身份不翻开 | `RolePubliclyRevealed(seat)` 单一事实源 |
| §212 | §92a 自死锁 P0 | `*Locked` 锁内变体 + `defer unlock` 范式 |
| §213 | emotion 单工具合并 | `emotion_switch_speak` 替代 5 类冗余 |
| §20260812-04 | 6 项 P0 集中清算 | 接线 lint + 私有信息块 + 降级可观测 |

> 完整教训见 [CLAUDE.md](CLAUDE.md)。

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

## ☕ 给避难所续杯

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

## 🌟 加入避难所

如果这个项目让你对「Agent 编程」有了新的认识，请：

- ⭐ **Star** 本仓库 —— 让更多人看到 Agent 编程的力量
- 👁️ **Watch** —— 跟进后续迭代
- 🍴 **Fork** —— 在你的环境里再造一座避难所

> 💡 **如果 Loop Agent × Graphic Agent 24/7 不间断编程的思路对你有启发**,欢迎在
> [Issues](https://github.com/your-repo/LsmAgentGame/issues) 分享你团队里的类似实践。
> 一个 ⭐ 比十篇博客更能推动这件事被更多人看见。

![Warden Badge](ProjectPic/bunker-warden.png)

**这不是一个人写的代码。这是一群 AI Agent 24 小时不间断编程的作品。**

---

**版本**：v1.0.0  |  **最后更新**：2026-08-12  |  **构建**：Agent 自动构建
