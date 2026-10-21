# LsmAgentGame — AI 代理项目规则

> 规范规则文件，唯一事实来源。`AGENTS.md` 是指向本文件的符号链接，请在此更新，勿改符号链接。

## 0. 开源工程说明

开源项目（**MIT License**），GitHub / Gitee / GitCode 三平台同步托管。`CONTRIBUTING.md`（+EN/JA）贡献流程；`CODE_OF_CONDUCT.md` 行为准则（含 AI Agent 约定）；`SECURITY.md` 漏洞上报；`.github/` 模板与 Dependabot。

**给贡献者的提示**：
- 所有 §编号（`§1` / `§20260812-04` 等）均为内部 **lesson 标记**，引用时写 `CLAUDE.md §<编号>` 即可；外部贡献者无需理解全部 §编号（参见 `CONTRIBUTING.md`）。
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

> 2026-08-07 §20260807-03 重构确立，方案见 [`lag_docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md`](lag_docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md)。

```
ClientWeb/src/
├── engine3d/            通用 WebGL/3D 渲染引擎（批次 22 起，游戏无关）
├── shared/utils/        跨游戏工具（balance / ui-storage / format / time）
├── components/
│   ├── chat/            ChatPanel(大厅) + GameChatPanel(房间，多游戏共用)
│   ├── common/ ui/ layout/ auth/ lobby/ rules/ wallet/ ...   共享界面
│   └── <game>/          各游戏私有组件（xiangqi/chess/junqi/doudizhu/
│                        texasholdem/werewolf/virtualCity），werewolf/emotion.ts 亦在此
├── hooks/ store/ types/ api/ pages/ scenes/ services/ i18n/ rules/
├── styles/              globals.css 为唯一入口，@import 顺序即级联优先级
└── assets/images/<game>/index.ts
```

**五条硬约束**：
1. **禁止跨游戏 import** —— `components/<gameA>/` 不得引用 `components/<gameB>/`；多游戏共用组件必须上移 `components/chat|common|ui/` 等共享目录。
2. **游戏私有代码必须落在 `<game>/` 内** —— 判据「引用方是否 100% 属于该游戏」；共享目录出现单一游戏的 i18n 键或类型即违规。
3. **共享工具只有一处**：`shared/utils/`，不得再新建 `util/` / `utils/`。
4. **`styles/globals.css` 的 `@import` 顺序不可调整** —— `werewolf.css → werewolf-v2.css → werewolf-emotion.css → werewolf-speech.css` 存在同优先级选择器覆盖链（`.werewolf-seat` 等），改序即样式回归。CSS 超 §4 行数上限时**只能整段搬移 + 原位置插入 `@import`**，以「构建产物 CSS 字节一致」验证零回归。
5. **`engine3d/` 禁止 import 游戏私有模块** —— 引擎只依赖 three / @react-three/fiber / @react-three/drei / three-stdlib；游戏私有资产路径拼接留在游戏侧薄适配层（如 `components/virtualCity/cityPbr.ts`）；游戏组件一律经 `@/engine3d` 单点引入。

### 2.5 lag_docs/ 知识库索引

完整索引见 [`lag_docs/README.md`](lag_docs/README.md)；新增/迁移文档前先读 §3。速查：`LLM与Agent/`（Provider/协议/拟人化）、`注入攻击演示/`（道具系统事实来源）、`架构与协议/`（API/WS/鉴权/观战者）、`狼人杀-Agent与系统/`（Agent 设计与推理博弈）、`狼人杀-重构方案/`、`狼人杀-角色设计/`（卡池/死亡语义）、`狼人杀-前端UI/` + `狼人杀-设计/`、`狼人杀-道具与经济/`、`虚拟城市/`（§27）、`通用功能/`（i18n/测试/子代理）、`其他Agent代码分析/`。

## 3. 文件命名规范

- `ServerGo/models/` 的 **GORM 模型文件**用前缀 `t_lsm_game_*.go`（如 `t_lsm_game_user.go`）——**唯一允许**此前缀的目录。
- `ServerGo/` **其它所有 Go 文件**一律 `snake_case.go`（如 `user_login.go`）；非模型文件切勿 `TLsmGame_xxx.go`。
- **Markdown 规则文件**（CLAUDE.md、AGENTS.md）≤ 800 行，过长按主题拆分到 `lag_docs/`。

## 4. 代码约束

- **单文件硬上限 ≤ 1800 行（所有语言）**，超限必须按功能/业务模块化拆分（仅纯代码搬移，不改逻辑/签名/导出命名）：
  - **Go**：拆到**同 package** 多个 `snake_case.go`（如 `room.go` → `room_lifecycle.go` / `room_watchdog.go` / `room_broadcast.go`）。
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

| 服务 | 默认端口 | URL |
|---|---|---|
| HTTPS（REST + 静态文件） | 39001 | `https://127.0.0.1:39001` |
| WSS（游戏流量） | 39002 | `wss://127.0.0.1:39002/ws` |
| MySQL | 3306 | `127.0.0.1:3306`（仅限本地） |

TLS 证书 `./server.crt`、`./server.key`（相对项目根）；生产用环境变量/外部挂载覆盖，**切勿**提交生产证书。

## 7. 错误处理与日志

- 返回客户端的错误统一用 `errcode/errcode.go` 全局错误码表，格式 `Result { code, message }`。
- 关键操作（登录、注册、WS 连接/断开）及**所有**错误用 `logger`（zap）记录，尽可能带 `request_id` / `user_id`。

### 7.1 前端错误展示规范（Web 页面/API 操作）

> **硬约束**：Web 页面操作对应的服务端 API 失败，**必须**在当前页面以**最高层级**展示，绝不允许吞进 `console` 或 3 秒自动消失的背景 toast。

**两层表面（按优先级）**：
1. **弹窗/表单内联错误**（首选，提交弹窗/表单场景）：弹窗内部渲染红色错误条（`formError` state），失败时**不关闭弹窗**，就地看到原因并重试。
2. **全局顶层 toast**（兜底，页面级操作或跨页面信号如 auth 过期/WS 断线）：`services/globalError.ts` 的 `reportGlobalError({ message, severity })` 上报（catch 块可直接传 `e`）；组件 `components/common/GlobalToast.tsx` 已挂于 `AppLayout`（`z-index:1000` > modal=200）。

**每个 `catch` 块自检**：(1) `isSessionExpiredError(e)` 类会重走登录弹层的不再重复展示友好文案；(2) 其它错误至少「`setErr` 当前页最高可见位置渲染」或「`reportGlobalError` 上报全局 toast」其一（推荐都做）；(3) 纯 best-effort 静默失败可免。

## 8. 前端打包工具

Vite（生产构建内部使用 Rollup）。不得引入 Webpack 配置文件；未经书面决策不得迁移 Next.js。

## 9. 子模块初始化

克隆后**必须** `git submodule update --init --recursive`（否则美术素材生成、自动化测试与浏览器调试不可用）：`python-generate-image-tool/` + `go-web-debug-tool/`。

## 10. 工作流程

1. 从计划选取一个阶段标记 `in_progress`；编译 → 测试 → 小步提交，尽可能一个逻辑阶段一次提交。
2. 非琐碎更改前重读相关 `lag_docs/` 文件；结构性变更同步更新文档。
3. 切勿在提交中重写 `LsmAgentGame.conf`、`server.crt`、`server.key`。
4. commit message 只写业务描述，**禁止任何 AI 署名 trailer**（§10.3）。

### 10.1 所有改动保持在 `main` 分支

> **2026-07-10 §122 起硬约束**：所有新功能/优化/修改/debug **必须**直接在 `main` 进行（除非用户显式要求）；`main` 是唯一长期维护代码线与 CI 默认基线。

- 会话第一条命令必须是 `git branch --show-current`，不在 `main` 立即 `git checkout main`。
- **禁止**创建新分支（`git checkout -b` / `git switch -c`）；服务（`./rebuild_restart_app.sh`）与单测（`go test ./...`）始终跑 `main` HEAD。

### 10.2 AI Agent Tools 规则文件同步

`CLAUDE.md` 是多 Agent 工具的「系统词」单一事实来源：Claude Code 读 `CLAUDE.md`；Kilo Code / OpenCode / pi / OpenClaw / Hermes 等读 `AGENTS.md`。

- `AGENTS.md` **必须是** `ln -s CLAUDE.md AGENTS.md` 符号链接——修改一次全工具同步生效。
- 若发现是普通文件：立刻 `rm AGENTS.md && ln -s CLAUDE.md AGENTS.md` 修正；CI 可加 `test -L AGENTS.md` 防误替换。

### 10.3 Git 提交消息规范（禁止 AI 署名）

> **2026-10-10 起硬约束**，适用于主仓库 `main` 与所有子模块。commit message 只承载业务描述：

- 禁止任何 `Co-Authored-By:` 行（含 `Co-Authored-By: Claude Code <noreply@anthropic.com>` 等 AI 名+邮箱变体）。
- 禁止 `"Generated with Claude Code"` / `"🤖 Generated with..."` 等 AI 生成署名（中英文、emoji 变体）。
- 禁止任何 AI attribution 尾部注释（`Co-authored-by` / `Generated with` / `Signed-off-by: AI` / 模型名等）。

**正确形态**：

```
feat(虚拟城市/批次54): <业务变更摘要>

- <改动要点 1>
- <改动要点 2>
```

正文出现 `Claude Code`、`§<编号>`、`AGENTS.md` 等**词条本身不算署名**（是业务上下文），被禁的是署名行/trailer 形态；不要为合规改写业务描述。用户显式要求加署名才例外，默认一律不加。

## 11. 常见陷阱

- **子模块本地 file:// 路径** —— 当前网络无问题；局域网外克隆需把 `.gitmodules` 更新为真实远程。
- **生成的 proto 文件已 gitignore** —— 编辑任何 `.proto` 后重跑 `proto/gen.sh`。
- **Vite dev 代理** `/api` `/ws` 到 `127.0.0.1` HTTPS，`secure: false` 接受自签证书；移除代理时同步更新配置与开发工作流。
- **MariaDB 默认数据库** —— GORM 驱动 `mysql`（兼容 MySQL/MariaDB）；未经书面决策不得切到 Postgres-only 功能。

## 12. 国际化与命名补充

i18n、`t_lsm_game_user.language` 字段、`/api/user/*` 偏好接口，以及测试文件 `test_*_test.go`、临时/数据补全文件 `temp_*.go` 的命名约定，统一见 [`lag_docs/通用功能/国际化与命名规范.md`](lag_docs/通用功能/国际化与命名规范.md)。涉及前先读该文档；新增/删除语言时前后端 `SUPPORTED`/`SupportedLanguages` 必须同步。

## 13. SubAgent 分工协作规则

> **核心原则：按职责拆分，每条职责线 = 一个独立 SubAgent。** 覆盖不到的工作面必须新建独立 SubAgent 并登记到 [`lag_docs/通用功能/子代理角色.md`](lag_docs/通用功能/子代理角色.md)。

### 13.1 8 条职责线

| # | 职责线 | SubAgent 代号 | 工作面 | 触发关键词 |
|---|--------|--------------|--------|-----------|
| 1 | `ClientWeb/` 前端游戏设计 | `frontend-dev` | 仅修改 `ClientWeb/` | React、UI、组件、路由、样式、3D |
| 2 | `ServerGo/` 后端游戏设计 | `backend-dev` | 仅修改 `ServerGo/` | Go、API、数据库、WebSocket、GORM |
| 3 | 游戏规则与产品设计 | `game-designer` | 仅产出 markdown 文档与契约 | 规则、玩法、需求、产品规划 |
| 4 | 界面设计与图像生成 | `art-designer` | `python-generate-image-tool` + `ClientWeb/src/assets/` + `3d_script/` + models | 美术素材、图片生成、配色、Blender（v19 §27 起） |
| 5 | 跨端联调 / 复杂规则 | `integration-tester` | 同时涉及 `ClientWeb/` 与 `ServerGo/` | 跨端联调、全栈、Game QA |
| 6 | 游戏策划视觉设计 | `game-visual-designer` | 仅产出 `lag_docs/design/**` 设计文档 | 视觉稿、布局、design tokens |
| 7 | **LLM Provider 模块** | `llm-provider` | `ServerGo/llm/`, `api/llm_api.go`, `config.LLMConfig` | LLM、Anthropic、OpenAI、Provider |
| 8 | **狼人杀 Agent 驱动** | `werewolf-agent` | `ServerGo/agent/`, `ServerGo/game/werewolf/` 的 Agent 接入 | Agent、bot、Memory、工具派发 |

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
- **`config.LLMConfig`** 顶级 `llm{}` 段：`endpoint/timeout_ms/max_retries/providers[]`；**真实 key 仅入 `LsmAgentGame.conf`**（example 用占位符）；`provider_type` 预留 `"openai"`。
- **API**：`GET /api/llm/models`（需登录）返回 `[{agent_name, model, provider_type}]`，**不含 api_key**。
- **8 个默认 seed 模型**（`defaultProvidersForSeed()`，§20260812-LLM，表空时注入）：LongCat-2.0 / 豆包 2.0 / DeepSeek V4-Pro / GLM-5.2 / Kimi 2.7 / MiniMax M3 / Qwen 3.7-Plus-and-Max / mimo-v2.5-pro，模型 key `MeiTuan/DouBao/DeepSeek/GLM/Kimi/MinMax/Qwen/Xiaomi-model`，均 `anthropic` 类型。
- **运行时 provider 可超 8 个**（§20260831-07）：运维经 `/api/admin/llm/providers` 接入更多，registry 按需加载；调用方从不感知数量——`registry.Get(model_key)` 按需查找。

### 14.1 Anthropic 协议对齐（参考 ClaudeCode）

> 出站请求体严格遵循 ClaudeCode 的 Messages API 字段顺序与命名。**权威 wire 用例**（项目根目录）：`CluadeCode的Anthropic协议-{Request,Respose}Body-数据用例0{1,2,3}.json`。

**关键协议约束（违反即上游 400 拒绝与零 token 空响应）**：
- **ContentBlock wire 形状必须按 Type 收敛**——本仓库反复出 Bug 的根因：`text` 块只许 `{"type","text"}`（禁带 `id`/`name`/`input`）；`tool_use` 只许 `{"type","id","name","input"}`（严格代理 DouBao 拒绝缺键，值空也必须始终产出）；`tool_result` 只许 `{"type","tool_use_id","content","is_error"}`。修复点：`llm/types/types.go` 的 `ContentBlock.MarshalJSON()` 按 `Type` 分支产出。
- **messages 数组 user/assistant 严格交替**——禁止连续 2 条及以上 `role=user`；`SanitizeMessagesForAnthropic` 末尾合并相邻 user 消息（拼接 content blocks）。
- 每条 message 的 `content` 必须是 **content-block 数组**（不允许纯 string）；`system` 必须是 **SystemBlock 数组**（`[{"type","text"}]`），最前三段为标准头（§14.3）。

**其余字段**：
- 顶层：`model` / `system[]` / `messages[]` / `tools[]` / `metadata` / `output_config` / `max_tokens` + 可选 `tool_choice` / `stream` / `temperature` / `thinking`。
- `stream:true` 返回 SSE 字节流（`ParseSSE` 解析），Agent 已切 `ChatStreamAccumulate`；`thinking` 按 `cfg.LLM.Providers[].thinking_required` 与自动愈合 fallback 注入。
- 出站请求头：`Authorization` / `anthropic-version: 2023-06-01` / `User-Agent` / `x-anthropic-billing-header` / `Content-Type`。
- 预飞归一化：(1) `tool_use.input == nil` → `{}`（§71a）；(2) `req.Thinking != nil` 时注入 `{type:"thinking"}` 块（§74a）。

### 14.3 Anthropic 三段式 system 提示词（全部 Agent 统一）

> 每个出站请求 `system[]` 最前固定三段（与 Claude Code 一致）：**① 计费元数据头**（`cc_version / cc_entrypoint / cc_is_subagent`，仅供上游计费追踪）→ **② 基础身份声明** → **③ 核心行为规则**（工具边界/任务准则/输出规范/权限约束）。完整规范见 [`lag_docs/LLM与Agent/AgentAnthropic系统提示词三段式规范.md`](lag_docs/LLM与Agent/AgentAnthropic系统提示词三段式规范.md)。

- **唯一事实来源**：`ServerGo/llm/sysprompt/`（leaf 包）。`texts.go` 持权威文本；`Head(agentClass)` / `EnsureHead(body, agentClass)` 产出与合并。
- **单一注入点（禁止 Agent 侧拼接）**：`llm/anthropic`（`Chat` + `ChatStream`）与 `llm/openai/convert.go` 在序列化前调 `sysprompt.EnsureHead` ⇒ 现有及未来新增 Agent 全部自动带三段，零配置。
- **`cc_is_subagent` 判定**：`req.AgentClassName != ""` ⇒ `true` 并追加 `cc_agent_name=<AgentClassName>`（`ServerGo/agent/class_names.go`）；健康探针等无 ClassName 调用 ⇒ `false`。
- **缓存策略**：②③ 带 `cache_control: {type:"ephemeral"}`，① 不带（`cc_agent_name` 逐 Agent 变化会击穿命中）；Agent 自有块语义不变。
- **字节预算**：三段约 2.5KB，Agent 预算须计入（`sysprompt.HeadBytes()`）；HTTP 头与 ① 段共用 `sysprompt.BillingHeaderText`，防口径漂移。
- **幂等**：`EnsureHead` 检测首位块前缀即跳过，不会重复注入。

**改动纪律**：三段文本逐字节稳定（全体 Agent 共享 prompt cache 前缀）；改动前须读规范文档并同步 `sysprompt` 测试锚点。

## 16. 聊天系统架构

| 作用域 | 说明 | 场景 |
|---|---|---|
| `lobby` | 全局大厅聊天 | 首页、游戏列表、个人中心等非游戏页面 |
| `room` | 房间内聊天 | `/xiangqi/:roomId` 等对局页面 |

- **组件**：`ChatPanel`（大厅右侧栏）与 `GameChatPanel`（房间内嵌，多游戏共用；狼人杀经 `components/werewolf/GameChatPanel.tsx` 薄适配器接入）在 `components/chat/`；`useChat`（`hooks/useChat.ts`）；`ChatService`（`ServerGo/ws/chat_service.go`）。
- **WS 端点**：`wss://HOST:39001/ws?token=<jwt>`。帧格式——C→S：`chat.subscribe / chat.unsubscribe / chat.send / chat.history`；S→C：`chat.message / chat.history / chat.subscribed / chat.unsubscribed / chat.error`。
- 消息持久化 `t_lsm_game_chat_message`，历史查询默认 50 条、最大 200 条。

## 17. 历史教训索引（§编号 → 文档路径）

> §编号是内部 lesson 标记。本节仅保留须**熟记**的 5 条核心教训；完整索引按主题归档在 `lag_docs/`。

| § | 教训 | 速记 |
|---|---|---|
| **§92a** | `sync.Mutex` 不可重入：`Action_*` 必须建 `*Locked` 锁内变体；凡 `WerewolfRoom` 方法被 `BuildClientState*` 调用必查 | 改 `Action_*` 先 grep `BuildClientState` |
| **§130** | 「声明了却从不接线」：新 helper / 字段 / 角色必须有 grep 验证接线 | 写完新字段立即 `git grep "<新字段名>"` |
| **§134** | 守卫等角色「全链路补全」：进卡池的角色必须完整实现，否则玩家持有无效身份 | 上卡池前先 grep 该角色全部用例 |
| **§135** | 死者身份公开由房间级开关 `reveal_role_on_death` 决定（§20260830-01，默认**开启**=死亡即宣告身份）；服务端权威下发 `my_role` / `my_seat` | 涉身份字段必查 `RolePubliclyRevealed` |
| **§197** | 流式续命——「接收到字节即刷新超时」 | 长上下文 LLM 调用必带流式 + 字节刷新 |

详细文档：§92a/§197 → `狼人杀-Agent与系统/`；§130 → `狼人杀-重构方案/`；§134/§135 → `狼人杀-角色设计/`；推理博弈 → `狼人杀-Agent与系统/Agent升级/`。其余主题见 §2.5 速查。早期高频细节（§43–§112 等 30+ 条）已迁出，按需查归档或 git log 检索 `§<编号>`。

另：WS 自动重连/恢复与 `user.*` 帧规则见 [`lag_docs/架构与协议/WebSocket重连与恢复.md`](lag_docs/架构与协议/WebSocket重连与恢复.md)；用户权限分级见 [`lag_docs/架构与协议/用户类型与权限.md`](lag_docs/架构与协议/用户类型与权限.md)；前端 WS 连接生命周期由 `AppLayout` 唯一持有，页面切换不得 connect/close。

## 19.5 观战者 (Spectator) — 跨 5 款游戏

任何登录用户可进入任意活跃房间以观战者身份实时观看，**不消耗座位、不影响玩家 UI**；底层由 `Hub.rooms` 与 `Hub.spectators` 两组互不相交广播集合隔离（详见 [`lag_docs/架构与协议/观战者架构.md`](lag_docs/架构与协议/观战者架构.md)）。

- 玩家输入帧在观战者身上后端硬性拒绝 → `ErrSpectatorInputForbidden = 30011`。
- 路由 `/<game>/spectate/:roomId`；Hook `useSpectatorMode()`；HTTP `POST /api/rooms/:id/spectate` / `.../leave_spectate`。

## 21. Agent 自动化测试账号

> **2026-08-25 安全加固：CAPTCHA 旁路已全部删除**（后端 `AgentBypassAccounts` 白名单与前端清单均移除），**所有账号登录一律需要 `captcha_id` / `captcha_answer`**，无绕过路径；`DevMode=true` 仅有日志告警，不解锁任何认证旁路。

本地开发自动化可用 [`lag_docs/通用功能/测试账号凭证.md`](lag_docs/通用功能/测试账号凭证.md) 预置账号（`test_01`~`test_04` 等，密码读仓库根 `test_account.json`，已 gitignore）：

- 先 `GET /api/captcha` 获取验证码 → 携带 `captcha_id` / `captcha_answer` 正常登录；账号未落库时先 `GET /api/invites` 取邀请码再 `POST /api/auth/register`。
- 仅限本地/开发环境，**严禁**生产复用。

## 22. 自动化测试与修复处理流程

> 仅保留流程规约，历史报告快照已迁 `lag_docs/`。**§20260915-01**：独立 Debug 流程已合并入各游戏提示词（`AutoTestAndDebug_<Game>`），测试后在同一会话内直接修复 + 提交推送；方案见 [`lag_docs/通用功能/自动化测试与调试流程合并方案-20260915.md`](lag_docs/通用功能/自动化测试与调试流程合并方案-20260915.md)。

- **检索入口**：主工程 `TestReport/<游戏>自动化测试报告_*.md`（glob: `auto_run_common.sh::GAME_GLOBS`）；子工程 `go-web-debug-tool/UseReport/<游戏>测试工具使用报告_*.md`。
- **处理入口**：各游戏脚本 `AutoTestAndDebug_{Werewolf,TexasPoker,Debate,Wealth}.sh` —— **首选 Claude Code CLI**（降级随机；公共库 `agent_cli_common.sh`），加载同名 `.md` 作 prompt；`AutoScreenshot_*.sh` 同机制。
- 修复流程硬约束详见各 `.md` 的「自动修复流程」章节（§12）；**绝对禁止**自动修复流程写入 `CLAUDE.md` / `AGENTS.md`。
- **报告清理**：修复完成后删除已处理的 `TestReport/*.md`（子工程 `UseReport/*.md`）；无问题报告加 `_无问题` 后缀归档。

## 24. AgentClassName 与 User-Agent 拼装约定

> **所有 Agent 都必须设置** `AgentClassName`，统一登记在 [`ServerGo/agent/class_names.go`](ServerGo/agent/class_names.go)。命名 `<Game>-<Role>`（`Player` / `Judge` / 工具型如 `MemoryIter`）。

**已注册**：`LsmAgentGame-Werewolf-Player`（Bot 主对话 + `speak_floor_tick` 自救）、`LsmAgentGame-Werewolf-Judge`（宣告 / `prompt_actor` / summary / `declare_cause`）、`LsmAgentGame-Werewolf-MemoryIter`（整局总结后迭代 `MEMORY.md`）。

- **User-Agent 出站**：`<AgentClassName>/<AppVersion> <buildDateTime>`；版本来自 `main.go` ldflags（`rebuild_restart_app.sh` 注入），由 `LLMRequest.AgentClassName` 在 `llm/anthropic/anthropic.go::userAgentFor` 出站前拼装。
- **接入新 Agent 两步**：(1) `class_names.go` 追加 `AgentClass<Game><Role>` 常量 + `AllAgentClassNames()`；(2) `llm.LLMRequest{...}` 构造点填 `AgentClassName`，并加单测断言 `!= ""`（防「声明了却从不接线」）。
- **动机**：同一 Provider 被多类 Agent 复用，上游靠 UA 区分调用方做计费/限流/审计；`AgentClassName`（业务身份）与 `ModelKey`（推理引擎）正交。

## 26. 前端 UI 颜色对比度与可读性规范

> 全文见 [`lag_docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md`](lag_docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md)（§20260808-02）。

- **26.1 暗色主题硬阈值**：正文/列表/表单 ≥ **4.5:1**；大字体(≥18pt / 14pt 加粗) ≥ **4.0:1**；状态徽章 ≥ **5.0:1**；selected/active 焦点态 ≥ **6.0:1**。
- **26.2 五大反模式（一票否决）**：① 同色相叠加（红底红字等）——文字改白 + 背景 ≥ 45% 不透明；② 浅色文字 + 25% 透明背景——`rgba(..., 0.45)` 起跳；③ `color: inherit` + 半透明背景——子元素显式声明 `color`；④ 选中/未选差异 < 1.5 倍明度——加 `box-shadow` 光晕（不被 night brightness 衰减）；⑤ `opacity: 0.4` 表 disabled——改单独灰底 + `cursor: not-allowed`。
- **26.3 三件套规约**：后端枚举拼 className 的样式必须**同提交**完成 JSX + CSS 规则 + `@keyframes + prefers-reduced-motion` 兜底；写完 JSX 立即 `grep -rn "<新class前缀>" ClientWeb/src/styles/`，零命中即 P1（§20260808-02 踩坑 `econ-tier-*`）。

## 27. Blender 真实 3D 模型工作流（v19 引入）

> 2026-09-23 §20260923-01：虚拟城市高视觉冲击组件（5 建筑 + 4 车 + 1 树 + 1 行人 + 1 路面）从程序化几何升级为 Blender headless 导出 `.glb`。完整规约见 [`lag_docs/虚拟城市/已实现/19-Blender3D模型集成/02-架构设计.md`](lag_docs/虚拟城市/已实现/19-Blender3D模型集成/02-架构设计.md)；§27.0 展开见 [`03-建模生产线规范.md`](lag_docs/虚拟城市/已实现/19-Blender3D模型集成/03-建模生产线规范.md)。

### 27.0 建模生产线纪律（2026-09-28 §20260928-01 确立，最高优先级）

> 本机已装 `/usr/local/bin/blender`（5.2.1 LTS），建模直接调 Blender，无需人工外部资产。以下 6 条为硬性要求，优先于 §27.1-§27.7，冲突时以本节为准。

1. **开工前先检索真实参考** —— 任何物件先检索真实结构比例与常见材质，结论写进脚本注释再动手，不许凭想象开建。
2. **禁止一次性完成整个复杂模型** —— 按资产逐个创建，一次只交付一个 `build_<name>.py` + `.glb`。
3. **统一色调 + 经年磨损** —— 颜色一律取 `__common__.py::CITY_PALETTE`（禁裸写 hex），表面用 `weathered_pbr()`，目标是可直接用于镜头的写实品质。⚠️ glTF 无 Noise Texture 对应物，程序化磨损**不经导出**（只作用 Blender 内预览/烘焙源；游戏内可见须另烘贴图，尚未实现）。
4. **可拉 SubAgent 分工** —— 结构调研/建模/材质/灯光渲染四工序可并行派 SubAgent（§27.8），主 Agent 只收口合批与门禁。
5. **质量优先于时间与 credit** —— 不以赶进度/省 token 为由降质，返工到达标为止。
6. **每批资产渲染检查** —— `render_preview.py` 出图 → 人/Agent **真的看一眼** → 不达标（穿模/比例错/糊/脏/色不统一）即返工，禁止只导出不验收。

> 门禁顺序不可跳：**调研 → 单资产建模 → 出图验收 → `verify_glb_aabb.py` → 合批入 git**。导出成功 ≠ 合格；**没看过渲染图的 `.glb` 视为未交付**。

### 27.1 数据流

`.blend`（不入库，本地美术）→ `3d_script/build_<X>.py`（入 git）经 `blender --background --python` 导出 → `ClientWeb/src/assets/models/{civic|vehicles|nature|characters|road}/<name>.glb`（入 git，15-64 KB）→ `import.meta.glob ?url` → `assets/models/index.ts::modelUrl` → `engine3d/modelCache.ts::useSharedGLTF` → `engine3d/Model.tsx`（`scene.clone(true)` per-instance）→ vite build（contenthash）→ rsync → `ServerGo/static/assets/`（embed.FS）→ `GET /static/...` → 浏览器 GLTFLoader。

### 27.2 模型清单（v19 首期，单文件 15-64 KB）

civic：city_hall / comm_tower / water_tower / fire_station / police_station；vehicles：sedan / truck / bus / taxi；nature：oak_tree；characters：pedestrian_walk（含 walk 动画）；road：road_props（StreetLight_A/B/C 合并）。导出脚本均为 `3d_script/build_*.py`（§27.4）。

### 27.3 硬约束

1. **`.blend` 不入库**（gitignore 挡 `*.blend` / `*.blend1`）；git 只入 `.py` + `.glb`。
2. **单 `.glb` ≤ 500 KB**（CI 跑 `find ClientWeb/src/assets/models -name "*.glb" -size +500k`）。
3. **保留程序化几何 fallback** —— 接入组件必须包 `<Model url={...}>{原程序化几何}</Model>`，缺失/失败/加载中自动降级，零代码分支。
4. **单位 = 世界单位（1 单位 = 10 m）** —— 按 `cityScale.METERS_PER_UNIT` 口径，**禁止按真实米导出**（批次 26 的 10× 巨型雪山即此错）。
5. **Blender 按 Z-up 摆放** —— 高度落 Z 轴，导出转 Yup 后 three.js 直立；按 Y-up 写的老脚本导出后**整体侧躺**（批次 19 事故，批次 29 修正）。
6. **节点变换必须 identity，尺寸只能烘焙进顶点**（骨骼/蒙皮豁免；原点 `minY=0` 贴地、X/Z 居中）。⚠️ 尺寸挂节点 `scale` 会被消费端 `RoadsideBins.collectPairs()` 的 `rootInv` **静默抵消**（批次 29「20 m 巨型垃圾桶」），且只量 `Box3.setFromObject` 判不出来。生产源 `__common__.py` 用 `obj.scale` 表达尺寸、`join_objects` 残留 scale，`export_glb()` 内 `bake_transforms()` 统一兜底。⚠️ `export_apply=True` 是 Apply Modifiers，**不烘焙** object transform，脚本需自行 `transform_apply`。
7. **GLB ≡ 程序化 fallback 同尺寸**（可测不变量）；尺寸唯一事实来源 = `components/virtualCity/cityScale.ts::REAL_DIMS_M`（真实米制），改表须「Blender 脚本 + `.glb` + 前端两条路径」**三侧同步**。
8. **`.py` 生产源全部入库**（§20260928-01）——`3d_script/*.py` 是唯一可复现源码，必须可 review 可回滚。

> **两个可执行护栏**（批次 29 起）：`3d_script/verify_glb_aabb.py` —— 全量 GLB 校验（**世界/几何双口径** + 节点 scale identity + 直立/贴地），非零退出即失败，**新增或重导资产后必跑**；`ClientWeb/src/engine3d/glbSizeGuard.ts` —— dev 态载入即校验，刻意在**被测节点局部系**量测（= 消费端 `rootInv` 后所见口径，唯一拦得住「挂节点」错法的量法），只告警绝不改几何。事故复盘见 [`lag_docs/虚拟城市/已实现/29-统一度量衡/01-方案设计.md`](lag_docs/虚拟城市/已实现/29-统一度量衡/01-方案设计.md)。

### 27.4 命令模板（art-designer 工作流）

```bash
cd 3d_script
blender --background --python build_city_hall.py -- \
  /usr/local/LsmAgentGame/LsmAgentGame/ClientWeb/src/assets/models/civic/city_hall.glb

# 渲染检查（§27.0-6 门禁，不可省）：--view front|side|top|iso（默认 iso）
blender --background --python render_preview.py -- \
  ClientWeb/src/assets/models/civic/city_hall.glb /tmp/prev_city_hall.png --view iso
```

出图后**用 Read 工具真的看一眼**再决定是否返工——工具不会替你判断「丑」。

### 27.5 缓存与降级契约

缓存 key = url；每实例 `.clone(true)`（共享 URL 但 transform 独立）；降级链：`url===''` / `onError` / 加载中三态直渲 children fallback；feature flag：`localStorage 'disable-blender-models' === '1'` 强制全 fallback。

### 27.6 Blender 版本与依赖

**Blender 5.2.1 LTS**（`/usr/local/bin/blender`）；DRACO / Meshopt 不启用（单文件远低于 500 KB）；v19.5 候选：DRACO 压缩（目标减 60-80%）。

### 27.7 §13.1 职责线扩展

Blender / `.glb` / 3D 模型 / 几何 → `art-designer`（`3d_script/build_*.py` + `ClientWeb/src/assets/models/**/*.glb`，与 `python-generate-image-tool/` 2D 贴图并列）；GLTFLoader / `useSharedGLTF` / `modelUrl` → `frontend-dev`（`engine3d/{modelCache,Model}.tsx` + 游戏侧接入组件）。

### 27.8 建模四工序 SubAgent 分工（§27.0-4）

结构调研（真实比例/结构/材质结论 → `build_<name>.py` 头部注释）→ 建模（几何 + 尺寸：`REAL_DIMS_M` 口径、Z-up、烘焙变换）→ 材质（`CITY_PALETTE` + `weathered_pbr()`）→ 灯光渲染（`render_preview.py` 出图 + 验收结论）。工序间**串行依赖**，并行只对**多件资产之间**成立；同一资产不要拆多个 Agent 改同一脚本。

## 28. 虚拟城市 · 实时看板（批次 69 统一重构）

> 右侧栏原 9 个平级面板（财务 / 行情 / 流水 / 经济 / 行为 / 宏观 / 交易 / 保险 / 实时）统一重构为**单一看板**「虚拟城市 - 实时看板」，采用**两级 Tab 架构**：分类选择器（城市运行 / 市场金融 / 外部实时）→ 维度 Tab 行（仅当前分类下的维度）；维度 Tab 条 `position: sticky` 贴在侧栏滚动容器顶部（改造前 Tab 条在独立滚动窗内会把数据挤出可视区，勿回退）。规范见 [`lag_docs/虚拟城市/已实现/69-实时看板统一重构/01-方案设计.md`](lag_docs/虚拟城市/已实现/69-实时看板统一重构/01-方案设计.md)。

### 28.1 维度目录（11 个 · 前后端双源）

- **前端静态目录 = 渲染权威**：`ClientWeb/src/components/virtualCity/RealtimeBoard/dashboardCatalog.ts` ⇒ 首屏不依赖网络，后端不可达 / 通道离线也出全量 Tab。
- **后端目录 = 校验 / 补充**：`GET /api/virtual_city/dashboard` 的 `dimensions[]`，按 id 覆盖 `title_key` / `group`，**未知 id 一律忽略**；后端目录**不得**成为 Tab 可见性的唯一决定因素。
- 后端目录源：`ServerGo/game/virtual_city/dashboard.go`（`DashboardDimensions()` / `DashboardDataSources()`，均返回**副本**，防调用方改坏包级切片）。

### 28.2 看板快照端点

`GET /api/virtual_city/dashboard`（需登录）→ `data.dashboard`：

| 字段 | 说明 |
|---|---|
| `schema_version` | 目录结构版本（当前 1） |
| `generated_at` | 生成时间（unix 秒） |
| `dimensions[]` | 11 条维度目录（id / group / title_key / source / scope / unit / refresh） |
| `data_sources[]` | 22 条公开免登录金融数据源登记册（category / name / purpose / url） |
| `realtime` | 外部行情通道快照，与 `GET /api/virtual_city/realtime_board` 的 `feed` **同构** |

⚠️ **通道离线时 `code` = 35053，但 `data.dashboard` 仍完整**（dimensions + data_sources 齐全，`realtime.channel.status="offline"`）。前端 `http<T>()` 在 `code≠0` 时抛错并**丢弃 body** ⇒ `fetchVirtualCityDashboard()` 对 35053 走**裸 fetch 兜底**取回完整目录。改这一段时前后端必须同步，否则「通道离线 = 看板骨架消失」。

### 28.3 看板硬约束

- 看板外壳与既有面板**不得双标题**：`behavior` / `macro` / `insurance` 的纯标题元素由 CSS 精确隐藏，**保险的「本月保费」必须保留可见**。
- 刷新 / 快照失败一律**看板内联红条**展示（§7.1），不吞 console。
- 通道徽章**只反映外部行情通道**；城市内维度的新鲜度由各面板负责，不重复展示。
- 跨面板跳转（`ActionPanel.onTradeTab` → `market-trade`、`GameOverModal.onViewLedger` → `ledger`）经 store 的 `panelTab` 中转，改 store 类型时勿断链。
- 看板维度**不得新增后端字段**：城市总览的 KPI 全部取自既有 `game.state` 段。
