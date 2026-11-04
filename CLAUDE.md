# LsmAgentGame — AI 代理项目规则

> 规范规则文件，唯一事实来源。`AGENTS.md` 是指向本文件的符号链接，请在此更新，勿改符号链接。

## 0. 开源工程说明

开源项目（**MIT License**），GitHub / Gitee / GitCode 三平台同步托管。`CONTRIBUTING.md`（+EN/JA）；`CODE_OF_CONDUCT.md`；`SECURITY.md`；`.github/` 模板与 Dependabot。

**给贡献者的提示**：
- 所有 §编号（`§1` / `§20260812-04` 等）均为内部 **lesson 标记**，引用写 `CLAUDE.md §<编号>` 即可；外部贡献者无需理解全部 §编号（参见 `CONTRIBUTING.md`）。
- 文档/注释改动遵循 §3 命名规约与 §4 单文件 ≤ 1800 行硬上限；跨端联调走集成测试 SubAgent（§13.1 职责线 5）。

## 1. 技术栈

- **后端**：Go（模块名 `LsmAgentGame`）、Gin、GORM + MySQL/MariaDB、gorilla/websocket、JWT (HS256)、bcrypt、zap 日志。
- **前端**：React 18 + TypeScript、Vite（生产构建内部使用 Rollup）、`@react-three/fiber` + `@react-three/drei`、zustand、react-router-dom v6。
- **通信协议**：HTTP API 为 JSON over HTTPS；实时游戏流量为 Protobuf (proto3) over WSS。
- **数据库**：MariaDB，`127.0.0.1:3306`，schema `lsmDB`，账号 `superuser`。密码从 `LsmAgentGame.conf` 加载——**切勿硬编码**。

## 2. 目录结构

```
ServerGo/                   Go 后端（HTTPS 39001, WSS 39002）
ClientWeb/                  React + Vite 前端
proto/                      .proto 源文件（唯一事实来源）
lag_docs/                   架构设计、鉴权流程、API 参考（独立子模块）
python-generate-image-tool/ 子模块 —— AI 图像生成
go-web-debug-tool/          子模块 —— Chrome CDP 自动化调试服务
```

### 2.1 前端目录约定（`ClientWeb/src`）

```
ClientWeb/src/
├── engine3d/            通用 WebGL/3D 渲染引擎（游戏无关）
├── shared/utils/        跨游戏工具（balance / ui-storage / format / time）
├── components/
│   ├── chat/            ChatPanel(大厅) + GameChatPanel(房间，多游戏共用)
│   ├── common/ ui/ layout/ auth/ lobby/ rules/ wallet/ ...   共享界面
│   └── <game>/          各游戏私有组件（xiangqi/chess/junqi/doudizhu/
│                        texasholdem/werewolf/virtualCity）
├── hooks/ store/ types/ api/ pages/ scenes/ services/ i18n/ rules/
├── styles/              globals.css 为唯一入口，@import 顺序即级联优先级
└── assets/images/<game>/index.ts
```

**五条硬约束**：
1. **禁止跨游戏 import** —— `components/<gameA>/` 不得引用 `components/<gameB>/`；多游戏共用组件必须上移 `components/chat|common|ui/` 等共享目录。
2. **游戏私有代码必须落在 `<game>/` 内** —— 判据「引用方是否 100% 属于该游戏」；共享目录出现单一游戏的 i18n 键或类型即违规。
3. **共享工具只有一处**：`shared/utils/`，不得再新建 `util/` / `utils/`。
4. **`styles/globals.css` 的 `@import` 顺序不可调整** —— 存在同优先级选择器覆盖链，改序即样式回归。CSS 超 §4 行数上限时**只能整段搬移 + 原位置插入 `@import`**，以「构建产物 CSS 字节一致」验证零回归。
5. **`engine3d/` 禁止 import 游戏私有模块** —— 引擎只依赖 three / @react-three/fiber / @react-three/drei / three-stdlib；游戏私有资产路径拼接留在游戏侧薄适配层；游戏组件一律经 `@/engine3d` 单点引入。

### 2.5 lag_docs/ 知识库索引

完整索引见 [`lag_docs/README.md`](lag_docs/README.md)；新增/迁移文档前先读 §3。速查：`虚拟城市/`（**当前主战场，§27~§30**）、`LLM与Agent/`（Provider/协议）、`架构与协议/`（API/WS/鉴权/观战者）、`通用功能/`（i18n/测试/子代理）、`注入攻击演示/`。其它游戏（狼人杀 / 德扑 / 辩论 / 棋类）文档已成熟冻结，按需检索即可。

## 3. 文件命名规范

- `ServerGo/models/` 的 **GORM 模型文件**用前缀 `t_lsm_game_*.go`——**唯一允许**此前缀的目录。
- `ServerGo/` **其它所有 Go 文件**一律 `snake_case.go`（如 `user_login.go`）；非模型文件切勿 `TLsmGame_xxx.go`。
- **Markdown 规则文件**（CLAUDE.md、AGENTS.md）≤ 800 行，过长按主题拆分到 `lag_docs/`。

## 4. 代码约束

- **单文件硬上限 ≤ 1800 行（所有语言）**，超限必须按功能/业务模块化拆分（仅纯代码搬移，不改逻辑/签名/导出命名）：
  - **Go**：拆到**同 package** 多个 `snake_case.go`（如 `room.go` → `room_lifecycle.go` / `room_watchdog.go`）。
  - **TS/TSX**：按职责拆独立模块/组件文件，`import` 聚合；**CSS**：按主题拆多个 `.css`，入口仅留 `@import` 且顺序与原级联顺序完全一致。
  - 拆分后必须通过编译与测试（Go: `go build` + `go test`；前端: `tsc --noEmit` + `npm run build`）。
- 编译命令：`ServerGo/` 下 `go build -o LsmAgentGame main.go`。
- 涉及 `ServerGo/` 的提交前 `go test ./...` 必须通过；涉及 `ClientWeb/` 的提交前 `tsc --noEmit` 和 `npm run build` 必须成功。

## 5. 安全

- **禁止硬编码密钥**：数据库密码、JWT 密钥等存放于 `LsmAgentGame.conf`（已 gitignore）；发布仅提供 `LsmAgentGame.conf.example` 占位值。
- **强制 HTTPS**：所有 HTTP 端点跑 HTTPS 监听器，不回退 HTTP。
- **CORS** 白名单在 `LsmAgentGame.conf` → `cors.allowed_origins`，新增来源在此配置，勿在中间件硬编码。
- **密码** bcrypt 存储（`util/password.go`）；**JWT** HS256，密钥从配置读取，默认有效期 7200 秒，签发者 `LsmAgentGame`。

## 6. 网络

| 服务                     | 默认端口 | URL                          |
| ------------------------ | -------- | ---------------------------- |
| HTTPS（REST + 静态文件） | 39001    | `https://127.0.0.1:39001`    |
| WSS（游戏流量）          | 39002    | `wss://127.0.0.1:39002/ws`   |
| MySQL                    | 3306     | `127.0.0.1:3306`（仅限本地） |

TLS 证书 `./server.crt`、`./server.key`（相对项目根）；生产用环境变量/外部挂载覆盖，**切勿**提交生产证书。

## 7. 错误处理与日志

- 返回客户端的错误统一用 `errcode/errcode.go` 全局错误码表，格式 `Result { code, message }`。
- 关键操作（登录、注册、WS 连接/断开）及**所有**错误用 `logger`（zap）记录，尽可能带 `request_id` / `user_id`。

### 7.1 前端错误展示规范（Web 页面/API 操作）

> **硬约束**：Web 页面操作对应的服务端 API 失败，**必须**在当前页面以**最高层级**展示，绝不允许吞进 `console` 或 3 秒自动消失的背景 toast。

**两层表面（按优先级）**：
1. **弹窗/表单内联错误**（首选）：弹窗内部渲染红色错误条（`formError` state），失败时**不关闭弹窗**，就地看到原因并重试。
2. **全局顶层 toast**（兜底）：`services/globalError.ts` 的 `reportGlobalError({ message, severity })`；组件 `components/common/GlobalToast.tsx` 已挂于 `AppLayout`（`z-index:1000` > modal=200）。

**每个 `catch` 块自检**：(1) `isSessionExpiredError(e)` 类会重走登录弹层的不再重复展示；(2) 其它错误至少「`setErr` 当前页最高可见位置渲染」或「`reportGlobalError` 上报全局 toast」其一（推荐都做）；(3) 纯 best-effort 静默失败可免。

## 8. 前端打包工具

Vite（生产构建内部使用 Rollup）。不得引入 Webpack 配置文件；未经书面决策不得迁移 Next.js。

## 9. 子模块初始化

克隆后**必须** `git submodule update --init --recursive`：`python-generate-image-tool/` + `go-web-debug-tool/`。

## 10. 工作流程

1. 从计划选取一个阶段标记 `in_progress`；编译 → 测试 → 小步提交，尽可能一个逻辑阶段一次提交。
2. 非平凡更改前重读相关 `lag_docs/` 文件；结构性变更同步更新文档。
3. 切勿在提交中重写 `LsmAgentGame.conf`、`server.crt`、`server.key`。
4. commit message 只写业务描述，**禁止任何 AI 署名 trailer**（§10.3）。

### 10.1 所有改动保持在 `main` 分支

> **2026-07-10 §122 起硬约束**：所有新功能/优化/修改/debug **必须**直接在 `main` 进行（除非用户显式要求）；`main` 是唯一长期维护代码线与 CI 默认基线。

- 会话第一条命令必须是 `git branch --show-current`，不在 `main` 立即 `git checkout main`。
- **禁止**创建新分支（`git checkout -b` / `git switch -c`）；服务（`./rebuild_restart_app.sh`）与单测（`go test ./...`）始终跑 `main` HEAD。

### 10.2 AI Agent Tools 规则文件同步

`CLAUDE.md` 是多 Agent 工具的「系统词」单一事实来源：Claude Code 读 `CLAUDE.md`；Kilo Code / OpenCode / pi / OpenClaw / Hermes / Codex / WorkBuddy 等读 `AGENTS.md`。

- `AGENTS.md` **必须是** `ln -s CLAUDE.md AGENTS.md` 符号链接——修改一次全工具同步生效。
- 若发现是普通文件：立刻 `rm AGENTS.md && ln -s CLAUDE.md AGENTS.md` 修正。

### 10.3 Git 提交消息规范（禁止 AI 署名）

> **2026-10-10 起硬约束**，适用于主仓库 `main` 与所有子模块。commit message 只承载业务描述：

- 禁止任何 `Co-Authored-By:` 行（含 AI 名+邮箱变体）。
- 禁止 `"Generated with Claude Code"` / `"🤖 Generated with..."` 等 AI 生成署名（中英文、emoji 变体）。
- 禁止任何 AI attribution 尾部注释（`Co-authored-by` / `Generated with` / `Signed-off-by: AI` / 模型名等）。

**正确形态**：

```
feat(虚拟城市/批次81): <业务变更摘要>

- <改动要点 1>
- <改动要点 2>
```

正文出现 `Claude Code`、`§<编号>`、`AGENTS.md` 等**词条本身不算署名**（是业务上下文），被禁的是署名行/trailer 形态。用户显式要求加署名才例外，默认一律不加。

## 11. 常见陷阱

- **子模块本地 file:// 路径** —— 当前网络无问题；局域网外克隆需把 `.gitmodules` 更新为真实远程。
- **生成的 proto 文件已 gitignore** —— 编辑任何 `.proto` 后重跑 `proto/gen.sh`。
- **Vite dev 代理** `/api` `/ws` 到 `127.0.0.1` HTTPS，`secure: false` 接受自签证书。
- **MariaDB 默认数据库** —— GORM 驱动 `mysql`（兼容 MySQL/MariaDB）；未经书面决策不得切到 Postgres-only 功能。

## 12. 国际化与命名补充

i18n、`t_lsm_game_user.language` 字段、`/api/user/*` 偏好接口，以及测试文件 `test_*_test.go`、临时文件 `temp_*.go` 的命名约定，统一见 [`lag_docs/通用功能/国际化与命名规范.md`](lag_docs/通用功能/国际化与命名规范.md)。涉及前先读该文档；新增/删除语言时前后端 `SUPPORTED`/`SupportedLanguages` 必须同步。

## 13. SubAgent 分工协作规则

> **核心原则：按职责拆分，每条职责线 = 一个独立 SubAgent。** 覆盖不到的工作面必须新建独立 SubAgent 并登记到 [`lag_docs/通用功能/子代理角色.md`](lag_docs/通用功能/子代理角色.md)。

### 13.1 8 条职责线

| #   | 职责线                    | SubAgent 代号          | 工作面                                                                         |
| --- | ------------------------- | ---------------------- | ------------------------------------------------------------------------------ |
| 1   | `ClientWeb/` 前端游戏设计 | `frontend-dev`         | 仅修改 `ClientWeb/`                                                            |
| 2   | `ServerGo/` 后端游戏设计  | `backend-dev`          | 仅修改 `ServerGo/`                                                             |
| 3   | 游戏规则与产品设计        | `game-designer`        | 仅产出 markdown 文档与契约                                                     |
| 4   | 界面设计与图像生成        | `art-designer`         | `python-generate-image-tool` + `ClientWeb/src/assets/` + `3d_script/` + models |
| 5   | 跨端联调 / 复杂规则       | `integration-tester`   | 同时涉及 `ClientWeb/` 与 `ServerGo/`                                           |
| 6   | 游戏策划视觉设计          | `game-visual-designer` | 仅产出 `lag_docs/design/**` 设计文档                                           |
| 7   | **LLM Provider 模块**     | `llm-provider`         | `ServerGo/llm/`, `api/llm_api.go`, `config.LLMConfig`                          |
| 8   | **Agent 驱动**            | `werewolf-agent`       | `ServerGo/agent/` 与游戏侧 Agent 接入（虚拟城市 Agent 同此线）                 |

### 13.2 派遣硬约束

- ✅ **单一职责**（每任务只派一个最匹配 SubAgent）/ **最小权限**（只改工作面内文件）/ **新建而非复用**（覆盖不到必须新建）。
- ❌ **不得越俎代庖**：主 Agent 不直接修改 `ClientWeb/`、`ServerGo/`、`python-generate-image-tool/`、`go-web-debug-tool/`。
- 🔁 **显式交接**：跨职责线按 `game-designer → game-visual-designer → art-designer → frontend-dev / backend-dev → integration-tester` 串联。

## 14. LLM Provider 模块（Anthropic 协议 + OpenAI 预留）

> 统一 `LLMProvider` 接口调用大模型，详见 [`lag_docs/LLM与Agent/LLM供应商设计.md`](lag_docs/LLM与Agent/LLM供应商设计.md)。

- **`ServerGo/llm/types/`**（leaf）：Anthropic wire 类型 + `LLMProvider` 接口 + `ModelInfo`。
- **`ServerGo/llm/sysprompt/`**（leaf）：三段式提示词权威文本 + `EnsureHead`（§14.3）。
- **`ServerGo/llm/anthropic/`**：真实 provider，`Authorization: Bearer <key>` + `anthropic-version: 2023-06-01`，5xx/429 重试。
- **`ServerGo/llm/registry.go`**：`NewRegistry` / `Get` / `List`（key-free）/ `SetUserAgent` / `SetBillingHeader`。
- **`config.LLMConfig`** 顶级 `llm{}` 段：`endpoint/timeout_ms/max_retries/providers[]`；**真实 key 仅入 `LsmAgentGame.conf`**（example 用占位符）。
- **8 个默认 seed 模型**（`defaultProvidersForSeed()`，表空时注入）：LongCat-2.0 / 豆包 2.0 / DeepSeek V4-Pro / GLM-5.2 / Kimi 2.7 / MiniMax M3 / Qwen 3.7-Plus-and-Max / mimo-v2.5-pro，均 `anthropic` 类型；运行时 provider 可超 8 个（运维经 `/api/admin/llm/providers` 接入）。

### 14.1 Anthropic 协议对齐

> 出站请求体严格遵循 ClaudeCode 的 Messages API 字段顺序与命名。**权威 wire 用例**：项目根目录 `CluadeCode的Anthropic协议-{Request,Respose}Body-数据用例0{1,2,3}.json`。

- **ContentBlock wire 形状必须按 Type 收敛**——本仓库反复出 Bug 的根因：`text` 只许 `{"type","text"}`；`tool_use` 只许 `{"type","id","name","input"}`（值空也必须产出）；`tool_result` 只许 `{"type","tool_use_id","content","is_error"}`。
- **messages 数组 user/assistant 严格交替**——禁止连续 2 条及以上 `role=user`；`SanitizeMessagesForAnthropic` 末尾合并相邻 user 消息。
- 每条 message 的 `content` 必须是 **content-block 数组**；`system` 必须是 **SystemBlock 数组**。
- **`stream:true` 返回 SSE 字节流**（`ParseSSE`）；**§197 流式续命**：长上下文调用必带流式 + 字节刷新超时。

### 14.3 Anthropic 三段式 system 提示词（全部 Agent 统一）

> 每个出站请求 `system[]` 最前固定三段：① 计费元数据头 → ② 基础身份声明 → ③ 核心行为规则。

- **唯一事实来源**：`ServerGo/llm/sysprompt/`。`texts.go` 持权威文本；`Head(agentClass)` / `EnsureHead(body, agentClass)` 产出与合并。
- **单一注入点（禁止 Agent 侧拼接）**：`llm/anthropic` 与 `llm/openai/convert.go` 在序列化前调 `sysprompt.EnsureHead` ⇒ 新增 Agent 自动带三段，零配置。
- **`cc_is_subagent` 判定**：`req.AgentClassName != ""` ⇒ `true` 并追加 `cc_agent_name=<AgentClassName>`。
- **改动纪律**：三段文本逐字节稳定（全体 Agent 共享 prompt cache 前缀）；改动前须读规范文档并同步 `sysprompt` 测试锚点。

## 16. 聊天系统架构

| 作用域  | 说明                       |
| ------- | -------------------------- |
| `lobby` | 全局大厅聊天（非游戏页面） |
| `room`  | 房间内聊天（对局页面）     |

- **组件**：`ChatPanel`（大厅）与 `GameChatPanel`（房间）在 `components/chat/`；`useChat`（`hooks/useChat.ts`）；`ChatService`（`ServerGo/ws/chat_service.go`）。
- **WS**：`wss://HOST:39001/ws?token=<jwt>`。帧：`chat.subscribe / unsubscribe / send / history`；消息持久化 `t_lsm_game_chat_message`。
- **虚拟城市房间聊天已删除**（批次 23）：居民公开发话改在 3D 地图头顶冒泡。

## 17. 历史教训索引（§编号 → 文档路径）

> §编号是内部 lesson 标记。本节只保留须**熟记**的核心教训；完整索引按主题归档在 `lag_docs/`。

| §        | 教训                                                               | 速记                                     |
| -------- | ------------------------------------------------------------------ | ---------------------------------------- |
| **§92a** | `sync.Mutex` 不可重入：`Action_*` 必须建 `*Locked` 锁内变体        | 改 `Action_*` 先 grep `BuildClientState` |
| **§130** | 「声明了却从不接线」：新 helper / 字段 / 角色必须有 grep 验证接线  | 写完新字段立即 `git grep "<新字段名>"`   |
| **§134** | 守卫等角色「全链路补全」                                           | 上卡池前先 grep 该角色全部用例           |
| **§135** | 死者身份公开由房间级开关决定；服务端权威下发 `my_role` / `my_seat` | 涉身份字段必查 `RolePubliclyRevealed`    |
| **§197** | 流式续命——「接收到字节即刷新超时」                                 | 长上下文 LLM 调用必带流式 + 字节刷新     |

## 19.5 观战者 (Spectator)

任何登录用户可进入任意活跃房间以观战者身份实时观看，**不消耗座位**；底层由 `Hub.rooms` 与 `Hub.spectators` 两组互不相交广播集合隔离（详见 [`lag_docs/架构与协议/观战者架构.md`](lag_docs/架构与协议/观战者架构.md)）。玩家输入帧在观战者身上后端硬性拒绝 → `ErrSpectatorInputForbidden = 30011`。

## 21. Agent 自动化测试账号

> **2026-08-25 安全加固：CAPTCHA 旁路已全部删除**，所有账号登录一律需要 `captcha_id` / `captcha_answer`，无绕过路径。

本地开发自动化可用 [`lag_docs/通用功能/测试账号凭证.md`](lag_docs/通用功能/测试账号凭证.md) 预置账号（`test_01`~`test_04` 等，密码读仓库根 `test_account.json`，已 gitignore）。仅限本地/开发环境，**严禁**生产复用。

## 22. 自动化测试与修复处理流程

> 仅保留流程规约。**§20260915-01**：独立 Debug 流程已合并入各游戏提示词（`AutoTestAndDebug_<Game>`），测试后在同一会话内直接修复 + 提交推送。

- **检索入口**：主工程 `TestReport/<游戏>自动化测试报告_*.md`；子工程 `go-web-debug-tool/UseReport/*.md`。
- **处理入口**：`AutoTestAndDebug_{Werewolf,TexasPoker,Debate,Wealth}.sh`（首选 Claude Code CLI，公共库 `agent_cli_common.sh`）。
- **报告清理**：修复完成后删除已处理的 `TestReport/*.md`；无问题报告加 `_无问题` 后缀归档。
- **绝对禁止**自动修复流程写入 `CLAUDE.md` / `AGENTS.md`。

## 24. AgentClassName 与 User-Agent 拼装约定

> **所有 Agent 都必须设置** `AgentClassName`，统一登记在 [`ServerGo/agent/class_names.go`](ServerGo/agent/class_names.go)。命名 `<Game>-<Role>`。

**已注册**：`LsmAgentGame-Werewolf-{Player,Judge,MemoryIter,ProfileIter,Recall,Commentator,MemoryCompact}`、`LsmAgentGame-TexasHoldem-{Player,Judge,ProfileIter,MemoryCompact}`、`LsmAgentGame-TexasPoker-MemoryIter`、`LsmAgentGame-Debate-{Player,Judge,MemoryCompact,Commentator}`、`LsmAgentGame-City-{Human,Chrome,System,WebGL-Show}`、`LsmAgentGame-Knowledge-Extractor`。

- **User-Agent 出站**：`<AgentClassName>/<AppVersion> <buildDateTime>`；版本来自 `main.go` ldflags。
- **接入新 Agent 两步**：(1) `class_names.go` 追加常量 + `AllAgentClassNames()`；(2) `llm.LLMRequest{...}` 填 `AgentClassName`，并加单测断言 `!= ""`（防「声明了却从不接线」）。

## 26. 前端 UI 颜色对比度与可读性规范

> 全文见 [`lag_docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md`](lag_docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md)（通用规约，适用全前端）。

- **暗色主题硬阈值**：正文/列表/表单 ≥ **4.5:1**；大字体 ≥ **4.0:1**；状态徽章 ≥ **5.0:1**；selected/active 焦点态 ≥ **6.0:1**。
- **五大反模式（一票否决）**：① 同色相叠加；② 浅色文字 + 25% 透明背景（`rgba(...,0.45)` 起跳）；③ `color: inherit` + 半透明背景；④ 选中/未选差异 < 1.5 倍明度；⑤ `opacity: 0.4` 表 disabled。
- **三件套规约**：后端枚举拼 className 的样式必须**同提交**完成 JSX + CSS 规则 + `@keyframes + prefers-reduced-motion` 兜底；写完 JSX 立即 `grep -rn "<新class前缀>" ClientWeb/src/styles/`，零命中即 P1。

## 27. Blender 真实 3D 模型工作流

> 2026-09-23 §20260923-01：虚拟城市高视觉冲击组件从程序化几何升级为 Blender headless 导出 `.glb`。完整规约见 [`lag_docs/虚拟城市/已实现/19-Blender3D模型集成/02-架构设计.md`](lag_docs/虚拟城市/已实现/19-Blender3D模型集成/02-架构设计.md) 与 [`03-建模生产线规范.md`](lag_docs/虚拟城市/已实现/19-Blender3D模型集成/03-建模生产线规范.md)。

### 27.0 建模生产线纪律（最高优先级）

1. **开工前先检索真实参考**（真实比例/结构/材质，结论写进脚本注释）。
2. **禁止一次性完成整个复杂模型** —— 按资产逐个交付。
3. **统一色调 + 经年磨损** —— 颜色取 `__common__.py::CITY_PALETTE`，表面用 `weathered_pbr()`。
4. **可拉 SubAgent 分工**（结构调研/建模/材质/灯光渲染）。
5. **质量优先于时间与 credit**。
6. **每批资产渲染检查** —— 出图 → 真的看一眼 → 不达标即返工。

> 门禁顺序不可跳：**调研 → 单资产建模 → 出图验收 → `verify_glb_aabb.py` → 合批入 git**。

### 27.1 数据流与硬约束

`.blend`（不入库）→ `3d_script/build_<X>.py`（入 git）→ `ClientWeb/src/assets/models/<dir>/<name>.glb`（15-64 KB）→ `engine3d/modelCache.ts` → `engine3d/Model.tsx`（`scene.clone(true)`）→ `ServerGo/static/assets/`（embed.FS）→ `GET /static/...`。

1. **`.blend` 不入库**；单 `.glb` ≤ 500 KB（CI 检查）。
2. **保留程序化几何 fallback**（`<Model url={...}>{fallback}</Model>`）。
3. **单位 = 世界单位（1 单位 = 10 m）**，按 `cityScale.METERS_PER_UNIT` 口径。
4. **Blender 按 Z-up 摆放**（导出转 Yup 后 three.js 直立）。
5. **节点变换必须 identity，尺寸只能烘焙进顶点**（原点 `minY=0` 贴地、X/Z 居中）；尺寸挂节点 `scale` 会被消费端 `rootInv` **静默抵消**。
6. **GLB ≡ 程序化 fallback 同尺寸**；尺寸唯一事实来源 = `components/virtualCity/cityScale.ts::REAL_DIMS_M`（三侧同步）。
7. **`.py` 生产源全部入库**。

> **两个可执行护栏**：`3d_script/verify_glb_aabb.py`（全量 GLB 校验，新增/重导后必跑）；`ClientWeb/src/engine3d/glbSizeGuard.ts`（dev 态载入即校验，只告警不改几何）。

## 28. 虚拟城市 · 实时看板（批次 69）

> 右侧栏统一为**单一看板**「虚拟城市 - 实时看板」，两级 Tab 架构。规范见 [`lag_docs/虚拟城市/已实现/69-实时看板统一重构/01-方案设计.md`](lag_docs/虚拟城市/已实现/69-实时看板统一重构/01-方案设计.md)。

- **前端静态目录 = 渲染权威**：`components/virtualCity/RealtimeBoard/dashboardCatalog.ts`（11+ 维度，首屏不依赖网络）。
- **后端目录 = 校验 / 补充**：`GET /api/virtual_city/dashboard` 的 `dimensions[]`，未知 id 忽略。
- **看板快照**：`schema_version` / `generated_at` / `dimensions[]` / `data_sources[]` / `realtime`。⚠️ 通道离线时 `code=35053` 但 `data.dashboard` 仍完整（前端 `http<T>()` 对 35053 走**裸 fetch 兜底**）。
- **硬约束**：不得双标题（`behavior`/`macro`/`insurance` 纯标题 CSS 隐藏，保险「本月保费」保留）；刷新失败一看板内联红条；通道徽章只反映外部行情通道。
- **批次 77 采集遥测**：`MissionRecord` 环（cap 20）+ `GET /api/virtual_city/realtime_board` 的 `data.missions[]`；⚠️ WS `game.realtime_feed` 帧**不含** missions。
- **批次 80 真实世界事件**：`FeedStore` → `DeriveWorldEvents`（R1–R10 纯函数、零 RNG）→ `applyScenarioToWorld` 注入 → `WorldEventLog`（环 ≤50）；前端第 11 子 Tab「事件流」。

## 29. 虚拟城市 · City-System 主 Agent 与「系统对话」（批次 81）

> 规范见 [`lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md`](lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md)。

**定位**：虚拟城市是人类（观察者 + 条件触发者）用**自然语言**控制的城市模拟系统。
「场景推演台」表单已废弃，改为 **`💬 系统对话`** 浮动窗（16:9、默认 720p、可最小化）。

### 29.1 `LsmAgentGame-City-*` Agent 清单（**每类一个独立 SubAgent**）

> **硬约束（2026-11-03 §批次81）**：每一类型 `LsmAgentGame-City-*` Agent 的
> **设计与代码实现必须各自由一个独立 SubAgent 独立完成**（§13.1 职责线 8），
> 并各自产出独立设计文档；主 Agent 只收口合批与门禁，不代改 Agent 代码。

| Agent 类型 | 类名 | 包 / 工作面 | 设计文档 | 职责 |
| --- | --- | --- | --- | --- |
| 主 Agent | `LsmAgentGame-City-System` | `ServerGo/agent/citysystem/` | `81-.../CitySystem-Agent设计.md` | ReAct 循环 + 11 工具 + 会话上下文（`ConversationStore`，按 `roomID/userID`） |
| 渲染显示 Agent | `LsmAgentGame-City-WebGL-Show` | `ServerGo/agent/citywebgl/` | `81-.../CityWebGLShow-Agent设计.md` | 显示原语 → 结构化 `DisplayCommand` |
| 数据采集 Agent | `LsmAgentGame-City-Chrome` | `ServerGo/citychrome/` | `81-.../CityChrome-Agent设计.md` | 操作 Chrome（CDP）ReAct 采集真实行情/新闻（批次65/77/78/79 已实现） |
| 城市居民 Agent | `LsmAgentGame-City-Human` | `ServerGo/agent/vcplayer/` | `81-.../CityHuman-Agent设计.md` | 居民月度决策 + 感知行动（批次17 已实现） |
| —（总线，非 Agent） | — | `ServerGo/agent/citybus/` | `81-.../01-方案设计.md` §3.1 | Agent 消息总线：`Register` / `RegisterRoom` / `Send` / `Post` / `Agents` |

- **AgentClassName 常量**在 `ServerGo/agent/class_names.go`（§24）。
- **Agent 间通讯**：`hub.Send(ctx, roomID, from, to, type, payload)`，房间级 handler 优先、回退类级。
  已注册：`City-WebGL-Show`（房间级）、`City-Chrome`（类级，触发采集）、`City-Human`（房间级，广播公告）。
  **新增 Agent 只需 `hub.Register(...)`，System 侧 `list_agents` 自动可见**（§130 反例）。

### 29.2 System Agent 工具（11 个）

`inject_scenario` / `list_scenarios` / `query_city_state` / `query_world_events` /
`query_prediction_report` / `control_simulation` / `launch_survey` / `focus_camera` /
`collect_realworld_data` / `send_agent_message` / `list_agents`

- **引擎零改动**：工具只调既有房间 API + 新增**只读** `room.SystemSummaryText(scope)` / `room.WorldEventLogView()`。
- **零 RNG / 零回归**：注入复用批次 68 `applyScenarioToWorld`（确定性，不消耗 `w.Rand`）。

### 29.3 WS / HTTP 契约

| 方向 | 帧                                 | 载荷                    |
| ---- | ---------------------------------- | ----------------------- |
| C→S  | `game.virtual_city_system_message` | `{room_id, text}`       |
| C→S  | `game.virtual_city_system_reset`   | `{room_id}`             |
| S→C  | `game.virtual_city_system_event`   | `{room_id, kind, data}` |
| S→C  | `game.virtual_city_system_history` | `{room_id, messages[]}` |
| S→C  | `game.virtual_city_show_command`   | `{room_id, command}`    |

`kind`：`agent_start` / `text`（增量）/ `thinking` / `tool_start` / `tool_result` / `show_command` / `report` / `agent_end` / `error` / `cleared`。

HTTP：`GET /api/games/virtual_city/rooms/:id/system_agent/history`、`POST .../system_agent/reset`。

### 29.4 场景目录（40 个 · 6 组）

`市场与经济`(11) / `政策与治理`(5) / `社会与舆情`(7) / `自然灾害`(9) / `未知灾难`(5) / `科技与新文明`(3)。
新增定义在 `game/virtual_city/scenario_catalog_batch81.go`，经 `scenarioDefs = append(…, scenarioDefsBatch81...)` 挂载。

### 29.5 建房「系统 Agent 线路」

`city_system_line`（顶层 / `virtual_city` 子对象均可，>128 rune 拒 400）：
`""` = 沿用服务端配置（零回归）/ `"random"` = 每轮从线路池按 `concurrency_lines` 加权随机（**默认**）/ 其余 = 固定 model key。
与 `city_chrome_line`（批次 79）**完全同构、互不覆盖**。

### 29.6 新增错误码

`35059 ErrVirtualCitySystemAgentDisabled` / `35060 ErrVirtualCitySystemAgentBusy` / `35061 ErrVirtualCityAgentNotFound`。
（35058 已被批次 68 `ErrVirtualCityPredictionRunNotFound` 占用，批次 81 取值顺延；`errcode.go` 为准。）

### 29.7 配置

`LsmAgentGame.conf` 增 `[city_system]`：`enabled` / `model_key` / `max_steps`(8) / `max_messages`(40) / `request_timeout_ms`(180000) / `tool_result_limit`(4000)。
`enabled=false` ⇒ WS 返回 35059（本段只在进程启动时读取，改动后须重启）。

### 29.8 前端

| 文件                                             | 职责                                          |
| ------------------------------------------------ | --------------------------------------------- |
| `components/virtualCity/SystemDialogPanel.tsx`   | 对话框本体（消息流 / 工具卡 / 输入 / 最小化） |
| `components/virtualCity/SystemDialogMessage.tsx` | 单条消息（用户 / Agent / 工具卡 / 报告卡）    |
| `components/virtualCity/cityShowAdapter.ts`      | 显示指令 → `focusRef` / `freeViewApi` / 标注  |
| `components/virtualCity/cityShowStore.ts`        | 高亮区 / 标注文本                             |
| `hooks/useVirtualCitySystemAgent.ts`             | 订阅 WS 帧 → store；`sendMessage` / `reset`   |
| `store/virtualCitySystem.store.ts`               | 会话消息 / 连接态 / 最小化态                  |
| `types/virtualCitySystem.ts`                     | WS 帧与消息类型                               |

- `ScenarioPanel.tsx` **已删除**（功能由对话框取代；`PredictionReportView.tsx` 保留复用）。
- 顶栏按钮 `🎛 场景推演台` → `💬 系统对话`。
- ⚠️ `t(映射表[id])` 必须恒返回字符串（缺 key 即白屏）；新 i18n key 三语同步。

### 29.9 批次 83 · 窗口优化与场景目录 101（2026-11-04）

> 规范见 [`lag_docs/虚拟城市/已实现/83-系统对话窗口优化与场景目录扩充/01-方案设计.md`](lag_docs/虚拟城市/已实现/83-系统对话窗口优化与场景目录扩充/01-方案设计.md)。

- **浮窗可拖拽**：header 指针把手，位置 localStorage `vc-sysdialog-pos-v1` 持久化 + 视口钳制，双击 header 复位；窄屏（≤900px）禁用。
- **顶栏按钮常显**：点击 = 打开 + 退出最小化（**取代**批次 81「停用隐藏入口」策略；停用态照常开窗显示内联红条）。
- **场景目录 40 → 101**：新增定义在 `game/virtual_city/scenario_catalog_batch83.go`，`scenario.go` 只改 append 链；纯数据零派发改动、零 RNG。
- **prompt 目录内联改紧凑索引**（`citysystem.ScenarioCatalogIndex()`，无描述）+ **`list_scenarios(group)`** 按组返回详情（`ScenarioCatalogDetail(group)`），防 101 场景全量回灌被 `tool_result_limit` 截断。
- WS / HTTP 契约零变更。
