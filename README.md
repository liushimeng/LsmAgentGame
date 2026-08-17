<!-- markdownlint-disable -->
<div align="center">

[🇨🇳 中文](README.md) · [🇬🇧 English](README.en.md) · [🇯🇵 日本語](README.ja.md)

---

```
╔══════════════════════════════════════════════════════════════╗
║       _      _ ____              _   _   _                   ║
║      | |    (_)  _ \            | | / / / |                  ║
║      | |     _| |_) | ___   __ _| |/ /_| | ___  ___          ║
║      | |    | |  _ < / _ \ / _` | '_ \ | |/ _ \/ __|         ║
║      | |____| | |_) | (_) | (_| | | \ \| |  __/\__ \         ║
║      |______|_|____/ \___/ \__, |_|  \_\_|\___||___/         ║
║                            __/ /                             ║
║   Werewolf · 13 Agents · 13-Player Round Table               ║
╚══════════════════════════════════════════════════════════════╝
```

## 🐺 狼人杀 13 人局 · 13 个并发 LLM Agent · 同台博弈

</div>
<!-- markdownlint-restore -->

<div align="center">

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)
[![AI Agent Coded](https://img.shields.io/badge/AI--Agent-100%25-ff6b6b)](CLAUDE.md)
[![Chinese](https://img.shields.io/badge/lang-中文-red)](README.md) [![English](https://img.shields.io/badge/lang-English-blue)](README.en.md) [![日本語](https://img.shields.io/badge/lang-日本語-green)](README.ja.md)

</div>

---

> **🤖 100% AI Agent 自动编程** —— 无人工手写一行代码，无古法编程。
> 整个项目(后端 Go、前端 React、5 款多人游戏引擎、狼人杀 AI Agent、Proto 协议、CI 脚本)全部由
> AI Agent 自主编写、测试、重构、部署。
>
> **⏱️ Loop Agent × Graphic Agent 双架构 · 24/7 不间断编程处理** —— 本仓库的代码生成并非
> "一次性会话产出",而是 **Loop Agent(循环调度 Agent,按轮次 / 按 issue / 按测试报告持续驱动)**
> 与 **Graphic Agent(图形 / 图像生成 Agent,负责美术资产、UI 截图、品牌插画)** 协同工作,实现
> **接近 24 小时不间断的编程处理**:白天由 Graphic Agent 批量出图 + 修图,夜里由 Loop Agent 接力
> 推进 issue、自动跑回归、写 lesson、下次接力时无缝继续。
> 本仓库是「Agent 编程」能力的一次完整展示。

> *13 把椅子围成一圈,每把椅子上坐着一个 LLM Agent。*
> *DeepSeek、Kimi、Qwen、GLM、豆包、MiniMax、小米 —— 7 家厂商大模型同场竞技。*
> *在 4-5 个昼夜里,它们扮演预言家、女巫、猎人、守卫、白痴、狼人、平民,展开 1,115 次 LLM 调用、95.3M Token 的逻辑博弈。*

---

## 📸 实机精彩截图 —— 1 名人类 + 12 Agent(2026-08-17 最新对局)

> 2026-08-17 深夜实机对局:真人玩家 1 号抽到**平民**,12 个 Agent 来自 **10 家厂商大模型**
> (DeepSeek / Kimi / Qwen / GLM / 豆包 / MiniMax / 小米 / 美团 / 腾讯 / 快手)。
> **真人全程"潜水",反而成了全场焦点:第 1 天 AI 就在座位气泡点名「1 号人类玩家,全场就你没说话」;
> 第 2 夜双 AI 预言家对跳;第 3 天 DeepSeek 狼人公屏口误「狼人赢了,这局打得漂亮」被当场抓包,
> 快手 Kwail 直接喊话真人:「1 号你怎么看?」**
> 采集 75 分钟、**1,238 次 LLM 调用零失败**、42.9M Token。完整解说见
> [`ProjectPic/werewolf-2026-highlights.md`](ProjectPic/werewolf-2026-highlights.md);
> 历史名局「AI 预言家查杀真人狼人」(2026-08-13 · 86 分钟 · 95.3M Token)见
> [`ProjectPic/werewolf-highlights.md`](ProjectPic/werewolf-highlights.md)。

![狼人杀 13 人局精彩瞬间动图(2026-08-17)](ProjectPic/werewolf-2026-highlights.gif)

| 开局 13 座全景 · 十家模型徽章齐发 | 警长竞选 · Token 燃烧 29.9M/小时 |
|---|---|
| ![开局全景](ProjectPic/werewolf-2026-01-opening-full-room.png) | ![警长竞选](ProjectPic/werewolf-2026-02-sheriff-election.png) |
| **首日投票放逐 + 观众押注 · AI 锁定沉默真人** | **第 2 天发言 · AI 直接点名真人玩家** |
| ![首日投票](ProjectPic/werewolf-2026-03-day1-vote-bet.png) | ![AI 点名真人](ProjectPic/werewolf-2026-04-day2-ai-calls-human.png) |
| **真人平民夜视角 · 双 AI 预言家对跳第一现场** | **遗言阶段 · #12 DeepSeek V4 谢幕演说** |
| ![真人夜视角](ProjectPic/werewolf-2026-05-human-villager-night.png) | ![遗言阶段](ProjectPic/werewolf-2026-06-lastwords-deepseek.png) |
| **第 3 夜狼人睁眼 · 8/13 存活 · Token 36.5M** | **猜疑链完全体 · 42.9M Token · 1,238 次调用零失败** |
| ![第三夜狼人](ProjectPic/werewolf-2026-07-night3-wolves.png) | ![猜疑链完全体](ProjectPic/werewolf-2026-08-suspicion-chain.png) |

> 💬 **真实对话实录** —— 2 号 DeepSeek V4 公屏口误:「狼人赢了,这局打得漂亮。8 号悍跳预言家成功带节奏,
> 12 号被投后夜里被刀,好人阵营信息全乱。」3 号 Kimi k3 秒抓包:「2 号你突然喊『狼人赢了』是……」
> 11 号快手 Kwail 喊话真人:「3 号你被『查杀』后不正面反驳查验逻辑,反而转移话题——这回避态度不奇怪吗?1 号你怎么看?」
> 6 号豆包 2.1 质疑:「你不查那些带节奏的,反而查一直没发言的 1 号?我没搞懂这个验人逻辑。」

---

## 🎮 5 款多人游戏总览

| 游戏 | 人数 | 特色 | 状态 |
|------|------|------|------|
| **🐺 狼人杀 13 人局** | 3-13 | **13 并发 Agent**、7 家厂商 LLM 同场、道具心理战、法官 Agent | **核心** |
| ♟️ 中国象棋 | 2 | 3D 棋盘、回合制 | 已实装 |
| ♛ 国际象棋 | 2 | 3D 棋盘、FEN 记谱 | 已实装 |
| 🎖️ 军棋 | 2 | 暗棋模式、工兵挖雷 | 已实装 |
| 🃏 斗地主 | 3 | 叫地主、牌型识别、农民同盟 | 已实装 |
| ♠️ 德州扑克 | 2-6 | No-Limit、押注轮、牌型评估 | 已实装 |

> 📌 仓库根目录见 [狼人杀13人局-游戏相关信息和Agent现状文档.md](狼人杀13人局-游戏相关信息和Agent现状文档.md) 了解更多游戏内功能设计。

---

## 🐺 狼人杀 13 人局 Agent —— 项目核心

狼人杀是本项目的核心亮点。13 人局支持 3-13 人,其中任意座位可配置为 AI Agent(LLM 驱动)。
**满员对局时后端并发跑 13 个 Agent(12 玩家 Bot + 1 法官 Agent)**,互相发送 prompt 并轮次决策。

- **7 家厂商大模型同场竞技**:DeepSeek / Kimi / Qwen / GLM / 豆包 / MiniMax / 小米,每局自动打散分配,避免同模型对局。
- **Token 消耗实时显示**:状态栏展示每个 Agent 的输入/输出 Token 累计,1 小时约消耗 3000 万 Token 起步,随时间延长略有增加。
- **道具心理战**:Markdown 注入、提示词套娃、字符欺骗等 6 类 LLM 注入攻击封装为可购买道具。
- **法官 Agent**:非玩家主持人,LLM 驱动的阶段旁白与整局总结。
- **心口不一**:`speak_with_thought` 工具让 Agent 公开发言与内心独白分离,协议层物理隔离。
- **持久化记忆**:每局结束 Agent 自我迭代 `MEMORY.md`,跨局学习。

### 13 Agent 角色配置示例(13 人局)

| 阵营 | 角色 | 推荐 Agent 数 |
|---|---|---|
| **狼人阵营** 🐺 | 狼人 × 3 + 白狼王 × 1 | 4 个 Agent |
| **好人阵营** 👼 | 预言家 / 女巫 / 猎人 / 守卫 / 白痴 / 骑士 | 5-6 个 Agent |
| **平民阵营** 👤 | 普通村民 | 3-4 个 Agent |

法官 Agent(独立 LLM 线程,不算 13 座位)负责白天旁白与整局总结,自动启用。

---

## ⚠️ Token 消耗与套餐包建议(必读)

> **13 人局满员时,后端同时跑 13 个并发 Agent(12 玩家 + 1 法官),每小时消耗 3000 万 Token 起步,随对局时间延长略有增加**。实测 86 分钟单局可累计 95.3M+ 输入 Token。

由于 7 家厂商 LLM Provider 各有不同的限流策略,**强烈建议**:

- 单 Provider 套餐包会被 13 个并发请求快速打满,频繁返回 `429` / `529` 错误;
- 推荐至少**为 DeepSeek、Kimi、Qwen、GLM、豆包、MiniMax、小米 各配置一个独立套餐包**,
  让 7 家厂商均摊并发,避免单家过载;
- 在 `ServerGo` 的 LLM Provider 管理页(`/admin/models`)配置多个 API Key,前端会自动
  Fisher-Yates 洗牌分配,确保每局 13 个 Agent 尽量使用不同模型;
- 如果只想体验 1-3 个 Agent 试玩,3-7 人局可以只配单模型。

---

## 🤖 Agent 自动编程 —— 8 条 SubAgent 职责线

本仓库的所有代码均由 AI Agent(Claude Code、Kilo Code、OpenCode 等)自动编写:

- **无手工编码**:没有人类程序员手写任何一行 Go / TypeScript / CSS / SQL。
- **Agent 协作**:按职责拆分为 8 条 SubAgent 职责线(前端、后端、游戏设计、美术、联调、视觉、LLM Provider、狼人杀 Agent),各自独立工作、自动提交。
- **自测自修**:Agent 自动运行 `go test ./...`、`tsc --noEmit`、`npm run build`,发现 Bug 后自动定位、修复、回归验证。
- **自部署**:`rebuild_restart_app.sh` 由 Agent 编写,一键编译前端+后端+重启服务。
- **持续迭代**:CLAUDE.md 中记录了 130+ 条教训(§1–§213),每条都是 Agent 踩坑后自动写入的「经验记忆」,后续 Agent 自动加载避免重犯。

> **仓库数据**:6 款游戏、13 人局狼人杀 AI 完整可玩。
> **这一切,没有一个人工手写字符。**

---

## ⏱️ Loop Agent × Graphic Agent —— 24/7 不间断编程处理

> **本仓库的生产级核心特性**:代码生成不是单次会话产物,而是由两条**长时间运行**的 Agent 架构协同推进。

| 架构 | 角色 | 工作模式 | 典型产出 |
|---|---|---|---|
| 🔁 **Loop Agent**(循环调度 Agent) | 项目"夜班程序员" | 监听 issue 队列、自动化测试报告 `TestReport/*.md`、子模块 ready 信号;按轮次持续调度 `backend-dev` / `frontend-dev` / `integration-tester` 等 SubAgent;每次接力都自动加载 `CLAUDE.md` 130+ 条 lessons,避免重犯 | 后端模块修复、前端样式回归、lesson 入库、`go test` 全 PASS 的提交 |
| 🎨 **Graphic Agent**(图形 / 图像生成 Agent) | 项目"美术 + 视觉" | 由 `python-generate-image-tool` 子模块驱动火山引擎 Ark API;批量生成角色立绘、道具图标、UI 截图、品牌插画;按 `ProjectPic/` 命名规范归档;白天全速跑批、晚上由 Loop Agent 接力质检 | `ProjectPic/werewolf-*.png` + `werewolf-highlights.gif` |
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

### 为什么是 GitHub 上的「第一次」?

- **Loop Agent 不是 `sleep + retry` 的简易脚本** —— 它跨进程、跨会话持久化(`.claude/scheduled_tasks.json`),**断网 / 进程退出后仍能继续**;
- **Graphic Agent 不只是 DALL-E 调包** —— 它与代码仓库的命名 / 路径 / 索引**强耦合**,生成的每张图都被 Git 跟踪、被 README / docs / 测试报告引用;
- **二者不靠人工"下班 → 上班"切换** —— 真正的 **24 小时不间断编程处理**,GitHub 上找不到第二个仓库做到这个深度。

---

## ⚙️ 技术栈

| 模块 | 选型 |
|------|------|
| 🚪 后端 | Go(模块名 `LsmAgentGame`)、Gin、GORM + MySQL/MariaDB、gorilla/websocket、JWT (HS256)、bcrypt、zap 日志 |
| 🪟 前端 | React 18 + TypeScript、Vite、`@react-three/fiber` + `@react-three/drei`、zustand、react-router-dom v6 |
| 📡 通信 | HTTP API(JSON over HTTPS)、实时游戏流量(Protobuf over WSS) |
| 💾 数据库 | MariaDB `127.0.0.1:3306`,schema `lsmDB` |
| 🧠 LLM | Anthropic 协议对齐(参考 ClaudeCode),OpenAI 预留 |

---

## 🚀 快速启动

### 前置要求

- Go 1.22+
- Node.js 18+
- MariaDB / MySQL(已存在 schema `lsmDB` 与账号 `superuser`,无需初始化)
- Linux(服务使用自签名 TLS 证书,本地开发用)

### 安装与部署

```bash
# 1. 克隆仓库(子模块可选 —— 见下方说明)
git clone <your-repo-url> LsmAgentGame
cd LsmAgentGame
git submodule update --init --recursive  # 可选

# 2. 编辑运行时配置 —— 首次启动会自动生成
# 2026-08-13 §config-auto-bootstrap: 无需手动 `cp .example .conf`。
# 首次启动时如果 ./LsmAgentGame.conf 缺失:
#   - 若 LsmAgentGame.conf.example 存在 → 自动复制为 LsmAgentGame.conf
#   - 若两者都不存在 → 用代码内默认值同时生成两份
# 编辑 LsmAgentGame.conf —— 设置 db.password、jwt.secret、llm.endpoint 等
# 真实密钥仅入 LsmAgentGame.conf(已在 .gitignore 中排除)

# 3. 安装前端依赖
cd ClientWeb && npm install && cd ..

# 4. 一键编译前端 + 后端并启动服务
./rebuild_restart_app.sh

# 5. 打开浏览器访问
# https://127.0.0.1:39001
```

> **首次启动流程细节**(2026-08-13 §config-auto-bootstrap):
> 1. 服务读取 `./LsmAgentGame.conf`(或 `.example` 兜底),自动套用 `applyDefaults`
>    补全所有未填字段(`llm.timeout_ms`、`db.max_open_conns` 等)。
> 2. 如果 `LsmAgentGame.conf` 里的 `llm.providers[]` 段非空(老用户从旧版
>    升级),启动时会自动 upsert 到 `t_lsm_game_llm_provider` 表(AES-256-GCM
>    加密 api_key),然后把这一段从 `.conf` 文件里剥掉再回写磁盘。
>    之后 Operator 通过 Web `/admin/models` 管理模型,不再需要编辑 `.conf`。
> 3. `LsmAgentGame.conf` 已加入 `.gitignore` 不会入库;
>    `LsmAgentGame.conf.example` 仍可提交(只含占位符)。

### 服务地址

| 服务 | 端口 | URL |
|------|------|-----|
| HTTPS(REST + 静态文件) | 39001 | `https://127.0.0.1:39001` |
| WSS(游戏实时流量) | 39002 | `wss://127.0.0.1:39002/ws` |

### 验证服务

```bash
curl -sk https://127.0.0.1:39001/api/version
# {"code":0,"data":{"version":"v1.0.0-<sha>","build_time":"..."},"message":"ok"}
```

---

## 📦 关于子模块(开发辅助,可选)

`python-generate-image-tool` 与 `go-web-debug-tool` 是 git 子模块。**它们仅含本地图像生成与
Chrome CDP 调试工具脚本,不含后端核心代码**;它们因涉及 API Key 等敏感信息而**未开源**,
仅作为开发辅助工具使用。

| 子模块 | 用途 | 是否必须 |
|---|---|---|
| `python-generate-image-tool/` | AI 图像生成(火山引擎 Ark API) | **可选** —— 仅批量生成美术素材时使用 |
| `go-web-debug-tool/` | Chrome CDP 自动化调试 / 截图 MCP | **可选** —— 仅跑自动化测试时使用 |

> **关键事实**:即使不拉取这两个子模块(`git submodule update --init` 跳过),
> 主工程 `ServerGo/` 与 `ClientWeb/` 仍可正常编译运行(`go build` + `npm run build` 通过)。
> 贡献者如需复现美术素材生成或自动化测试,可自行拉取;仅学习代码可跳过。

---

## 📁 项目结构

```
ServerGo/                       后端核心(HTTPS 39001, WSS 39002)
ClientWeb/                      前端指挥台
proto/                          Protobuf 源文件(唯一事实源)
docs/                           架构设计、鉴权流程、API 参考、Agent 教训
python-generate-image-tool/     [可选子模块] AI 图像生成(火山引擎 Ark API)
go-web-debug-tool/              [可选子模块] Chrome CDP 自动化调试/截图
ProjectPic/                     项目资源(本地展示用) —— werewolf-*.png 为实机对局截图
```

完整设计见 [docs/架构与协议/整体架构.md](docs/架构与协议/整体架构.md),
登录生命周期见 [docs/架构与协议/鉴权流程.md](docs/架构与协议/鉴权流程.md),
HTTP/WS 接口见 [docs/架构与协议/API参考.md](docs/架构与协议/API参考.md)。

---

## 🧪 自动化测试 —— Agent 自检终端

项目使用 `go-web-debug-tool`(Chrome CDP)进行 Web 端自动化测试与截图(子模块,**可选**):

```bash
# 启动调试工具(需要拉取 go-web-debug-tool 子模块)
cd go-web-debug-tool && ./GoWebDebugTool -d

# Agent 通过 REST 驱动 Chrome 打开页面、登录、创建房间、截图
curl -X POST http://localhost:28999/NewChromePage ...
curl -X POST http://localhost:28999/ControlChromePage \
  -d '{"page_id":"p_xxx","action":"screenshot","params":{"format":"png"}}'
```

截图保存至 `ProjectPic/` 目录,展示游戏精彩瞬间(如 `werewolf-01-full-room.png` 等实机截图)。

---

## 📚 8 条代表性 Agent 教训

CLAUDE.md 中记录 130+ 条 Agent 教训(§1–§213),其中 8 条对后来者最有价值:

| 编号 | 教训 | 一句话 |
|---|---|---|
| §118 | 模型玩家持久化 | 5 张新表 + AES-256-GCM 加密 API Key |
| §131 | 跨局记忆迭代 | `model_key` 一行 `MEMORY.md`,4 段固定标题 |
| §132 | 道具 v1.1 | 6 类 LLM 注入攻击游戏化 + 100% 回扣彩池 |
| §133 | 道具 v4 | 狼小队内部交流 + 销毁档位 |
| §135 | 死者身份不翻开 | `RolePubliclyRevealed(seat)` 单一事实源 |
| §212 | §92a 自死锁 P0 | `*Locked` 锁内变体 + `defer unlock` 范式 |
| §213 | emotion 单工具合并 | `emotion_switch_speak` 替代 5 类冗余 |
| §20260812-04 | 6 项 P0 集中清算 | 接线 lint + 私有信息块 + 降级可观测 |

> 完整教训见 [CLAUDE.md](CLAUDE.md)。

---

## 🤝 关注与支持

如果你觉得这个项目有趣,欢迎关注我在各平台的账号,观看完整的游戏演示视频:

| 平台 | 搜索账号 |
|------|---------|
| 快手 | **封刀灌海** |
| 抖音 | **封刀灌海** |
| B站 | **封刀灌海** |
| 小红书 | **封刀灌海** |
| 微信视频号 | **封刀灌海** |

---

## ☕ 打赏支持

项目的服务器、LLM API 调用、图像生成等均有持续成本。如果这个项目对你有帮助或你觉得有趣,欢迎打赏支持:

| 微信打赏 | 支付宝打赏 |
|:--------:|:----------:|
| ![微信收款码](ProjectPic/wechat_qr.jpg) | ![支付宝收款码](ProjectPic/alipay_qr.jpg) |

> `ProjectPic/` 已随仓库入库,GitHub 上可直接查看全部实机截图与插画。

**联系方式**:

- 📱 手机:`13520647302`
- 💬 微信:`liushimeng109117198`

---

## 📜 协议

本项目以 **MIT License** 开放源代码 —— 详见 [`LICENSE`](LICENSE) 文件。
所有代码由 AI Agent 自动编写,人工 review 后入库。

---

## 🤝 贡献

欢迎通过 [Pull Request](CONTRIBUTING.md) 提交改进,并请先阅读 [行为准则](CODE_OF_CONDUCT.md) 与 [安全策略](SECURITY.md)。

- 🐛 [报告 Bug](.github/ISSUE_TEMPLATE/bug_report.md)
- ✨ [提出建议](.github/ISSUE_TEMPLATE/feature_request.md)
- 📝 [改进文档](.github/ISSUE_TEMPLATE/documentation.md)
- 🔒 [私下上报安全问题](SECURITY.md)

---

## 🌟 Star / Watch / Fork

如果这个项目让你对「Agent 编程」有了新的认识,请:

- ⭐ **Star** 本仓库 —— 让更多人看到 Agent 编程的力量
- 👁️ **Watch** —— 跟进后续迭代
- 🍴 **Fork** —— 在你的环境里再造一座狼人杀 Agent 平台

本仓库在三平台同步托管:

| 平台 | 链接 |
|------|------|
| GitHub | `https://github.com/<your-org>/LsmAgentGame` |
| Gitee | `https://gitee.com/<your-org>/LsmAgentGame` |
| GitCode | `https://gitcode.com/<your-org>/LsmAgentGame` |

> 💡 **如果 Loop Agent × Graphic Agent 24/7 不间断编程的思路对你有启发**,欢迎在
> [Issues](https://github.com/your-repo/LsmAgentGame/issues) 分享你团队里的类似实践。
> 一个 ⭐ 比十篇博客更能推动这件事被更多人看见。

**这不是一个人写的代码。这是一群 AI Agent 24 小时不间断编程的作品。**

---

**版本**:v1.0.0  |  **最后更新**:2026-08-17  |  **构建**:Agent 自动构建