# LsmAgentGame — AI 代理项目规则

> 规范规则文件。`AGENTS.md` 是指向此文件的符号链接。
> 唯一事实来源。请在此处更新，切勿在符号链接中修改。

## 0. 开源工程说明

本仓库为开源项目 —— 在 GitHub / Gitee / GitCode 三平台同步托管，按 **MIT License** 发布。

| 文档 | 用途 |
|------|------|
| [`LICENSE`](LICENSE) | MIT 协议全文 |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) · [EN](CONTRIBUTING.en.md) · [JA](CONTRIBUTING.ja.md) | 贡献流程与 PR 规范 |
| [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) | 行为准则（含 AI Agent 特别约定） |
| [`SECURITY.md`](SECURITY.md) | 安全漏洞私下上报通道 |
| [`.github/`](.github/) | Issue / PR 模板、Dependabot、FUNDING |

**给贡献者的提示**：

- 本规则文件由 AI Agent 持续维护，所有 §编号（`§1` / `§20260812-04` 等）均为内部 **lesson 标记**，
  引用它时用 `CLAUDE.md §<编号>` 即可。**这些是项目独有的历史决策档案，按要求保留**——
  外部贡献者无需理解全部 §编号即可参与贡献（参见 `CONTRIBUTING.md`）。
- 任何文档/注释改动必须遵循 §3 文件命名规约、§4 单文件 ≤ 1800 行硬上限。
- 跨端联调（前后端同时改）请走集成测试 SubAgent（CLAUDE.md §13.1 职责线 5）。

## 1. 技术栈

- **后端**：Go（模块名 `LsmAgentGame`）、Gin、GORM + MySQL/MariaDB、gorilla/websocket、JWT (HS256)、bcrypt、zap 日志。
- **前端**：React 18 + TypeScript、Vite（生产构建内部使用 Rollup）、`@react-three/fiber` + `@react-three/drei`、zustand、react-router-dom v6。
- **通信协议**：HTTP API 使用 JSON over HTTPS；实时游戏流量使用 Protobuf (proto3) over WSS。
- **数据库**：MariaDB，地址 `127.0.0.1:3306`，schema `lsmDB`，账号 `superuser`。密码从 `LsmAgentGame.conf` 加载——**切勿硬编码**。

## 2. 目录结构

```
ServerGo/                   Go 后端（HTTPS 39001, WSS 39002）
ClientWeb/                  React + Vite 前端
proto/                      .proto 源文件（唯一事实来源）
docs/                       架构设计、鉴权流程、API 参考
python-generate-image-tool/ 子模块 —— AI 图像生成
go-web-debug-tool/          子模块 —— Chrome CDP 自动化调试服务
```

### 2.1 前端目录约定（`ClientWeb/src`）

> 2026-08-07 §20260807-03 重构确立。方案见
> [`docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md`](docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md)。

```
ClientWeb/src/
├── shared/utils/        跨游戏工具（balance / ui-storage / format / time）
├── components/
│   ├── chat/            ChatPanel(大厅) + GameChatPanel(房间，5 款游戏共用)
│   ├── common/ ui/ layout/ auth/ lobby/ rules/ wallet/ ...   共享界面
│   └── <game>/          各游戏私有组件（xiangqi/chess/junqi/doudizhu/
│                        texasholdem/werewolf），werewolf/emotion.ts 亦在此
├── hooks/ store/ types/ api/ pages/ scenes/ services/ i18n/ rules/
├── styles/              globals.css 为唯一入口，@import 顺序即级联优先级
└── assets/images/<game>/index.ts
```

**四条硬约束**：

1. **禁止跨游戏 import** —— `components/<gameA>/` 不得引用 `components/<gameB>/`。
   多款游戏共用的组件必须上移到 `components/chat|common|ui/` 等共享目录。
2. **游戏私有代码必须落在 `<game>/` 内** —— 判据是「引用方是否 100% 属于该游戏」。
   共享目录（`shared/` `components/ui|common/`）中出现单一游戏的 i18n 键或类型即为违规。
3. **共享工具只有一处**：`shared/utils/`。不得再新建 `util/` 或 `utils/`。
4. **`styles/globals.css` 的 `@import` 顺序不可调整** —— `werewolf.css` →
   `werewolf-v2.css` → `werewolf-emotion.css` → `werewolf-speech.css` 之间存在
   同优先级选择器覆盖链（`.werewolf-seat` 等），改序即样式回归。
   CSS 文件超 §4 行数上限时，**只能整段搬移 + 在原位置插入 `@import`**，
   并以「构建产物 CSS 字节一致」验证零回归。

## 3. 文件命名规范

- `ServerGo/models/` 下的 **GORM 模型文件** 使用前缀 `t_lsm_game_*.go`（例如 `t_lsm_game_user.go`）。这是**唯一允许**使用此前缀的目录。
- `ServerGo/` 下的**其他所有 Go 文件** 使用 `snake_case.go`（例如 `user_login.go`、`game_logic.go`）。非模型文件切勿使用 `TLsmGame_xxx.go`。
- **Markdown** 规则文件（CLAUDE.md、AGENTS.md）不超过 800 行。如文件过长，请按主题拆分到 `docs/` 目录。

## 4. 代码约束

- **代码单文件硬上限：≤ 1800 行（所有语言）** —— 适用于 `ServerGo/**/*.go`、`ClientWeb/src/**/*.{ts,tsx,css}` 及任何其他代码文件。任何代码文件超过 1800 行，**必须**按功能/业务模块化拆分：
  - **Go**：拆到**同 package** 下的多个 `snake_case.go` 文件（如 `room.go` → `room_lifecycle.go` / `room_watchdog.go` / `room_broadcast.go`），仅做纯代码搬移，不改逻辑、不改签名、不改导出命名。
  - **TS/TSX**：按职责拆为独立模块/组件文件，通过 `import` 聚合。
  - **CSS**：按主题拆为多个 `.css` 文件，入口文件仅保留 `@import` 列表且**顺序必须与原文件级联顺序完全一致**。
  - 拆分后必须通过对应编译与测试（Go: `go build` + `go test`；前端: `tsc --noEmit` + `npm run build`）。
- 在 `ServerGo/` 目录下执行 `go build -o LsmAgentGame main.go` 进行编译。
- 任何涉及 `ServerGo/` 的提交前，`go test ./...` 必须通过。
- 任何涉及 `ClientWeb/` 的提交前，前端类型检查（`tsc --noEmit`）和 `npm run build` 必须成功。

## 5. 安全

- **禁止硬编码密钥。** 数据库密码、JWT 密钥等存放在 `LsmAgentGame.conf`（已加入 gitignore）。发布时仅提供 `LsmAgentGame.conf.example`，其中为占位符值。
- **强制 HTTPS。** 所有 HTTP 端点运行在 HTTPS 监听器上。不回退到 HTTP。
- **CORS** 白名单配置在 `LsmAgentGame.conf` → `cors.allowed_origins`。新增来源请在此处配置，切勿在中间件中硬编码。
- **密码**使用 bcrypt 存储（`util/password.go`）。
- **JWT** 使用 HS256 签名；密钥从配置读取；默认有效期 7200 秒；签发者 `LsmAgentGame`。

## 6. 网络

| 服务                     | 默认端口 | URL                          |
| ------------------------ | -------- | ---------------------------- |
| HTTPS（REST + 静态文件） | 39001    | `https://127.0.0.1:39001`    |
| WSS（游戏流量）          | 39002    | `wss://127.0.0.1:39002/ws`   |
| MySQL                    | 3306     | `127.0.0.1:3306`（仅限本地） |

TLS 证书路径：`./server.crt`、`./server.key`（相对于项目根目录）。生产环境请通过环境变量或外部挂载覆盖；**切勿**将生产证书提交到代码仓库。

## 7. 错误处理与日志

- 所有返回给客户端的错误使用 `errcode/errcode.go` 中的全局错误码表，格式为 `Result { code, message }`。
- 记录关键操作（登录、注册、WebSocket 连接/断开）及**所有**错误，使用 `logger`（zap）——尽可能包含 `request_id` 和 `user_id`。

### 7.1 前端错误展示规范（Web 页面/API 操作）

> **硬约束**：所有 Web 页面操作对应的服务端 API 失败，**必须**在当前页面以**最高层级**显示给用户，绝不允许失败吞进 `console` 或 3 秒后自动消失的背景 toast。

**两层允许的表面（按优先级）**：

| 优先级 | 表面 | 适用场景 | 实现 |
|--------|------|---------|------|
| 1（首选）| **弹窗/表单内联错误** | 用户正在提交某个弹窗或表单（如编辑模型、修改昵称、登录弹层）| 在弹窗内部渲染一条红色错误条（`formError` state），操作失败时**不关闭弹窗**，让用户就地看到原因并重试 |
| 2（兜底）| **全局顶层 toast** (`GlobalToast`)| 无弹窗的页面级操作（如列表加载、创建房间、刷新），或跨页面信号（auth 过期、WS 断线）| 通过 `services/globalError.ts` 的 `reportGlobalError({ message, severity })` 上报；组件挂于 `AppLayout`，`z-index:1000`，高于所有 modal(modal=200)|

**接入方式**：
- 全局 channel：`import { reportGlobalError, errorMessage } from '@/services/globalError'`。`reportGlobalError` 接受 `GlobalErrorEvent | Error | unknown`，catch 块可直接 `reportGlobalError(e)`。
- 全局组件：`ClientWeb/src/components/common/GlobalToast.tsx`（已挂于 `AppLayout`，无需各页面手动挂）。
- 后端 WS 驱动的错误（如 `AdminUsersPage` 的 `user.error` 帧）同样需 `reportGlobalError`。

**每个 `catch` 块的自检清单**：
1. `isSessionExpiredError(e)` 这类会重走登录弹层的错误 → 不再重复展示友好文案（`http.ts::handleSessionError` 已处理）。
2. 其它错误 → 要么 `setErr(e.message)` 在当前页最高可见位置渲染、要么 `reportGlobalError(...)` 上报到全局 toast（**两者至少其一，推荐两者都做**——本地保留上下文 + 全局兜底）。
3. 纯 best-effort 的静默失败（如复制到剪贴板、`leaveSpectate`、`.catch(() => [])`）可免。

## 8. 前端打包工具

Vite 是打包工具。规范中写的是 "Webpack/Rollup"——Vite 在**生产构建时内部使用 Rollup**，因此规范已满足。不得引入 Webpack 配置文件。未经书面决策不得迁移到 Next.js。

## 9. 子模块初始化

克隆后**必须**执行：

```bash
git submodule update --init --recursive
```

| 子模块                        | 用途                                  |
| ----------------------------- | ------------------------------------- |
| `python-generate-image-tool/` | AI 图像生成（角色、道具、场景、贴图） |
| `go-web-debug-tool/`          | Chrome CDP 自动化调试服务             |

跳过此步骤将导致游戏美术素材生成失败、自动化测试与浏览器调试不可用。

## 10. 工作流程

1. 从计划中选取一个阶段，将其任务标记为 `in_progress`。
2. 编译 → 测试 → 小步提交。尽可能每个逻辑阶段对应一次提交。
3. 进行任何非琐碎更改前，重新阅读相关的 `docs/` 文件。如有结构性变更，请同步更新文档。
4. 切勿在提交中重写 `LsmAgentGame.conf`、`server.crt` 或 `server.key`。

### 10.1 所有改动保持在 `main` 分支

> **2026-07-10 §122 起的硬约束**。本仓库的所有新功能添加、优化、修改、debug
> **必须** 直接在 `main` 分支上进行；不再使用特性分支(除非经用户显式要求)。
> `main` 是**唯一长期维护的代码线**与 CI 默认基线。

**操作规约**:
- **当前开发分支检查**:任何会话开始时第一条 shell 命令必须是 `git branch --show-current`,
  若不在 `main` 立即 `git checkout main`(除非用户在本次任务显式要求使用其它分支)。
- **不创建新分支**:`git checkout -b <name>` **禁止**;`git switch -c` 同样禁止。
- **运行基准**:服务(`./rebuild_restart_app.sh`)和单测(`go test ./...`)始终跑 `main` HEAD。

### 10.2 AI Agent Tools 规则文件同步

> **本规则文件 (`CLAUDE.md`) 同时被多个 AI Agent 工具加载**,作为项目的"系统词"
> 单一事实来源。任何规则更新必须**同步**到所有下游入口,避免工具间出现行为漂移。

| Agent 工具 | 入口文件 | 加载方式 |
|------|------|------|
| **Claude Code** (`Claude`) | `CLAUDE.md` (项目根) | 自动读取根目录 `CLAUDE.md` |
| **Kilo Code / OpenCode / pi / OpenClaw / Hermes** 等其它 Agent | `AGENTS.md` | 项目根 `AGENTS.md` **必须**是 `CLAUDE.md` 的符号链接 |

**同步规约**:
- `AGENTS.md` **必须**是 `ln -s CLAUDE.md AGENTS.md` 创建的符号链接 — 修改一次,所有工具同步生效。
- **若发现 `AGENTS.md` 是普通文件而非符号链接**:立刻 `rm AGENTS.md && ln -s CLAUDE.md AGENTS.md` 修正。
- **CI 自检(可选)**:可在 GitHub Actions 加一步 `test -L AGENTS.md` 防止误把符号链接替换为普通文件。

## 11. 常见陷阱

- **子模块路径使用本地 file://** —— 在当前网络下没有问题。如果在局域网外克隆，请将 `.gitmodules` 更新为真实的远程地址。
- **生成的 proto 文件已加入 gitignore** —— 编辑任何 `.proto` 文件后需重新运行 `proto/gen.sh`。
- **Vite 开发服务器代理** `/api` 和 `/ws` 到 `127.0.0.1` 的 HTTPS，设置 `secure: false` 以接受自签名证书。移除代理时请同时更新配置和开发工作流。
- **MariaDB 是默认数据库** —— GORM 驱动为 `mysql`（同时兼容 MySQL 和 MariaDB）。未经书面决策不得切换到仅支持 Postgres 的功能。

## 12. 国际化与命名补充

- **多国语言(i18n)**、`t_lsm_game_user.language` 字段、`/api/user/*` 偏好接口，以及**服务器测试文件 `test_*_test.go`、临时/数据补全文件 `temp_*.go`** 的命名约定，统一记录在 [`docs/通用功能/国际化与命名规范.md`](docs/通用功能/国际化与命名规范.md)。
- 涉及上述任一主题前请先阅读该文档；新增/删除语言时前后端 `SUPPORTED`/`SupportedLanguages` 必须同步。

## 12.5 "我方在底部" 布局设计规范

> 5 款多人游戏的**界面布局**统一规则："我"的座位 / 棋子 / 手牌永远在屏幕底部。
> Agent 新增 / 修改游戏布局前**必须**先阅读 [`docs/通用功能/底部玩家布局设计.md`](docs/通用功能/底部玩家布局设计.md)。
>
> **三种实现模式**（按游戏类型选用）：
>
> | 模式 | 适用 | 示例文件 |
> |------|------|---------|
> | Board 180° 翻转 | 2 人棋类 | `XiangqiBoard.tsx:37-62` / `ChessBoard.tsx:45-72` / `JunqiBoard.tsx:46-69` |
> | 虚拟座位旋转 | 3+ 人卡牌类 | `DoudizhuTable.tsx:37-39` / `TexasHoldemTable.tsx:32` |
> | yBase 坐标偏移 | 棋类非对局拖拽面板 | `LayoutPanel.tsx:65-72` |
>
> 关键约束：本地旋转 ≠ 服务端旋转 / 观战者固定视角 / 服务端权威下发 my_color / my_seat /
> 视图字段脱敏（不走 BroadcastRoom）/ 旋转只影响视觉不影响逻辑。

## 13. SubAgent 分工协作规则

> **核心原则：按职责拆分，每条职责线 = 一个独立 SubAgent。** 无法精确覆盖的工作面必须
> 新建独立 SubAgent 并写入 [`docs/通用功能/子代理角色.md`](docs/通用功能/子代理角色.md)。

### 13.1 8 条职责线

| # | 职责线 | SubAgent 代号 | 工作面 | 触发关键词 |
|---|--------|--------------|--------|-----------|
| 1 | `ClientWeb/` 前端游戏设计 | `frontend-dev` | 仅修改 `ClientWeb/` | React、UI、组件、路由、样式、3D |
| 2 | `ServerGo/` 后端游戏设计 | `backend-dev` | 仅修改 `ServerGo/` | Go、API、数据库、WebSocket、GORM |
| 3 | 游戏规则与产品设计 | `game-designer` | 仅产出 markdown 文档与契约 | 规则、玩法、需求、产品规划 |
| 4 | 界面设计与图像生成 | `art-designer` | `python-generate-image-tool` + `ClientWeb/src/assets/` | 美术素材、图片生成、配色 |
| 5 | 跨端联调 / 复杂规则 | `integration-tester` | 同时涉及 `ClientWeb/` 与 `ServerGo/` | 跨端联调、全栈、Game QA |
| 6 | 游戏策划视觉设计 | `game-visual-designer` | 仅产出 `docs/design/**` 设计文档 | 视觉稿、布局、design tokens |
| 7 | **LLM Provider 模块** | `llm-provider` | `ServerGo/llm/`, `api/llm_api.go`, `config.LLMConfig` | LLM、Anthropic、OpenAI、Provider |
| 8 | **狼人杀 Agent 驱动** | `werewolf-agent` | `ServerGo/agent/`, `ServerGo/game/werewolf/` 的 Agent 接入 | Agent、bot、Memory、工具派发 |

### 13.2 派遣硬约束

- ✅ **单一职责**：每个任务只派遣一个最匹配的 SubAgent。
- ✅ **最小权限**：SubAgent 只能修改其工作面内的文件。
- ✅ **新建而非复用**：覆盖不到的职责线必须新建独立 SubAgent。
- ❌ **不得越俎代庖**：主 Agent 不直接修改 `ClientWeb/`、`ServerGo/`、`python-generate-image-tool/` 或 `go-web-debug-tool/`。
- 🔁 **显式交接**：跨职责线任务按 `game-designer → game-visual-designer → art-designer → frontend-dev / backend-dev → integration-tester` 顺序串联。

## 14. LLM Provider 模块（Anthropic 协议 + OpenAI 预留）

> 通过统一 `LLMProvider` 接口调用大模型。详见 [`docs/LLM与Agent/LLM供应商设计.md`](docs/LLM与Agent/LLM供应商设计.md)。

- **`ServerGo/llm/types/`** —— leaf 包：Anthropic wire 类型 + `LLMProvider` 接口 + `PlaceholderKey` + `ModelInfo`。
- **`ServerGo/llm/anthropic/`** —— 真实 provider 实现：`Authorization: Bearer <key>` + `anthropic-version: 2023-06-01`，5xx/429 重试。
- **`ServerGo/llm/registry.go`** —— `NewRegistry` / `Get` / `List`（key-free）/ `SetUserAgent` / `SetBillingHeader`。
- **`config.LLMConfig`** —— 顶级 `llm{}` 段：`endpoint/timeout_ms/max_retries/providers[]`。**真实 key 仅入 `LsmAgentGame.conf`；`LsmAgentGame.conf.example` 用 `API-KEY-PLACEHOLDER` 占位**。
- **预留 OpenAI 协议** —— `provider_type` 字段已留 `"openai"`。
- **API** —— `GET /api/llm/models`（需登录），返回 `[{agent_name, model, provider_type}]`，**不含 api_key**。
- **8 个默认模型**（运行时 `LsmAgentGame.conf`，seed 源迁到代码常量 `defaultProvidersForSeed()` 见 §20260812-LLM）：

  | AgentName | Model | ProviderType |
  |---|---|---|
  | 美团 LongCat-2.0 | `MeiTuan-model` | anthropic |
  | 豆包 2.0 | `DouBao-model` | anthropic |
  | DeepSeek V4-Pro | `DeepSeek-model` | anthropic |
  | 智谱 GLM-5.2 | `GLM-model` | anthropic |
  | Kimi 2.7 | `Kimi-model` | anthropic |
  | MiniMax M3 | `MinMax-model` | anthropic |
  | Qwen 3.7-Plus-and-Max | `Qwen-model` | anthropic |
  | Xiaomi mimo-v2.5-pro | `Xiaomi-model` | anthropic |

### 14.1 Anthropic 协议对齐（参考 ClaudeCode）

> 出站请求体严格遵循 ClaudeCode 的 Anthropic Messages API 字段顺序与命名。
> **权威数据用例**（项目根目录，Claude Code 实际下发的 wire 格式）：
>
> | 角色 | 文件 |
> |------|------|
> | Request-Body 用例 1/2/3 | `CluadeCode的Anthropic协议-RequestBody-数据用例01.json` / `02.json` / `03.json` |
> | Response-Body 用例 1/2/3 | `CluadeCode的Anthropic协议-ResposeBody-数据用例01.json` / `02.json` / `03.json` |
>
> **关键协议约束（违反即触发上游 400 拒绝与零 token 空响应）**：
>
> - **ContentBlock wire 形状必须按 Type 收敛**——这是本仓库曾反复出 Bug 的根因：
>   - `text` 块**只允许** `{"type","text"}` —— **禁止**携带 `id`/`name`/`input`。
>   - `tool_use` 块**只允许** `{"type","id","name","input"}` —— 严格代理（DouBao）拒绝缺失这三个键的
>     tool_use，即便值为空对象/字符串，必须始终产出。
>   - `tool_result` 块**只允许** `{"type","tool_use_id","content","is_error"}` —— **禁止**携带 `id`/`name`/`input`。
>   - 修复：`llm/types/types.go` 的 `ContentBlock.MarshalJSON()` 按 `Type` 分支产出。
> - **messages 数组 user/assistant 严格交替**——禁止连续 2 条及以上 `role=user` 的消息。修复：`SanitizeMessagesForAnthropic` 末尾合并相邻 user 消息为一条（拼接 content blocks）。
> - `content`（user/assistant 的每条 message）必须是 **content-block 数组**，不允许纯 string。
> - `system` 必须是 **SystemBlock 数组**（`[{"type","text"}]`），不允许纯 string。

- **顶层字段**：`model` / `system[]` / `messages[]` / `tools[]` / `metadata` / `output_config` / `max_tokens`
  + 可选 `tool_choice` / `stream` / `temperature` / `thinking`。
- **`metadata.user_id`**：stringified JSON，由 `buildMetadataUserID` 构造。
- **`stream`**：true 时 Provider 返回 SSE 字节流，由 `ParseSSE` 解析；Agent 已切到 `ChatStreamAccumulate`。
- **`thinking`**：Extended Thinking，按 `cfg.LLM.Providers[].thinking_required` 与自动愈合 fallback 决定何时注入。
- **出站请求头**：`Authorization` / `anthropic-version: 2023-06-01` / `User-Agent` / `x-anthropic-billing-header` / `Content-Type`。
- **预飞归一化**：
  1. `tool_use.input == nil` → `{}`（§71a）
  2. `req.Thinking != nil` 时注入 `{type:"thinking"}` 块（§74a）

### 14.2 狼人杀 AI 玩家随机分配模型

> 当 `POST /api/games/werewolf/rooms` 携带 `len(agent_seats) > 1` 时，服务端自动把重复
> `model_key` 改写为其他可用模型（Fisher-Yates 洗牌），确保 7 bot 尽量使用不同模型。

- 触发条件：`len(agent_seats) > 1` 且 `len(cfg.LLM.Providers) > 1`
- 保留用户挑选的不同 model；仅改写重复项；占位 key 过滤；候选池不足时降级为随机轮询。

## 15. 狼人杀 13 人局 Agent（in-process 驱动）

> **综合现状文档**：[`狼人杀13人局-游戏相关信息和Agent现状文档.md`](狼人杀13人局-游戏相关信息和Agent现状文档.md)
> 详见 [`docs/狼人杀-Agent与系统/狼人杀Agent设计.md`](docs/狼人杀-Agent与系统/狼人杀Agent设计.md)。
> **角色实现状态**：`godRolePool` 含 6 个全链路可玩神职：女巫/猎人/白痴/**守卫**/**骑士**/**猎魔人**；
> 魔术师/奇迹商人/射梦人/乌鸦/稻草人/定序王子/纯白之女 已退役（仅保留 wire 兼容）。
> 守卫规则与实现见 [`docs/狼人杀-角色设计/狼人杀守卫角色设计.md`](docs/狼人杀-角色设计/狼人杀守卫角色设计.md)。
> **硬约束**：进卡池的角色要么完整实现，要么移出卡池 —— 「半实现」= 玩家持有无效身份。

- **核心结构** —— `ServerGo/agent/`：`agent.go` / `memory.go` / `tools.go` / `prompt.go` / `ratelimit.go`
  + `BuildTools(phase, role, seat, alive)` 工具定义 + `DispatchTool` 派发
  + `GameContext` 含 `MySeat` / `SpeakTurn` / `TurnActingSeat` 等事件上下文
- **引擎接入** —— Agent 通过 `ToolRunner` 接口（13 个方法）调用 `WerewolfManager.Action_*`，**不走 WS**（in-process）。发言通过 `ChatService.SendFromBot/WhisperFromBot` 复用现有广播路径。
- **可见性** —— `WerewolfRoom.BotTranscripts[seat]` 挂在 `game.state.bot_contexts[]`，前端 `AgentThoughtPanel` 渲染。
- **限流** —— driver 用 30s 令牌桶；文本 100 字截断；单轮最多 5 次 tool_use；超时/5xx 走 Provider 重试。
- **混合房间** —— 全人类 / 全 Agent / 混合；`CreateRoomWithAgents` 入口；`POST /api/rooms` 接受可选 `agent_seats`。
- **公平性** —— 所有 Agent 代码完全相同，仅模型不同；Memory 可见可追溯。

## 16. 聊天系统架构

聊天系统支持两种作用域：

| 作用域  | 说明         | 使用场景                             |
| ------- | ------------ | ------------------------------------ |
| `lobby` | 全局大厅聊天 | 首页、游戏列表、个人中心等非游戏页面 |
| `room`  | 房间内聊天   | `/xiangqi/:roomId` 等游戏对局页面    |

### 关键组件

- **`ChatPanel`** (`components/chat/ChatPanel.tsx`) — 右侧栏全局聊天面板
- **`GameChatPanel`** (`components/chat/GameChatPanel.tsx`) — 游戏页面内嵌的房间聊天面板。
  **6 款游戏共用同一实现**（2026-08-07 §20260807-03：原位于 `components/xiangqi/` 属跨游戏引用，已上移；
  `components/chess/GameChatPanel.tsx` 陈旧副本已删除）。狼人杀通过 `components/werewolf/GameChatPanel.tsx`
  （87 行薄适配器，映射 `gameState.players → roomPlayers`）接入 —— 这是「共享基座 + 游戏私有 props」的推荐范式。
- **`useChat` hook** (`hooks/useChat.ts`) — 管理 WS 订阅、消息收发
- **`ChatService`** (`ServerGo/ws/chat_service.go`) — 后端聊天服务

### WebSocket 端点

前端 `wsClient` 使用 `wss://HOST:39001/ws?token=<jwt>` 连接。

### 聊天帧格式

```
客户端 → 服务端：chat.subscribe / chat.unsubscribe / chat.send / chat.history
服务端 → 客户端：chat.message / chat.history / chat.subscribed / chat.unsubscribed / chat.error
```

消息持久化到 `t_lsm_game_chat_message` 表，支持历史记录查询（默认 50 条，最大 200 条）。

## 17. 历史教训索引（§编号 → 文档路径）

> §编号是项目内部 **lesson 标记**，引用时用 `CLAUDE.md §<编号>` 即可。
> 完整的「现象 / 修复 / 教训」**全部已迁出本文件**，按主题归档到 `docs/` 子目录；
> 本节仅保留**教训标题 + 文档路径**作为高频查阅索引。

#### 通用 Agent 接线 / 死锁类

| §  | 教训 | 文档路径 |
|---|---|---|
| 43 | 聊天消息在游戏页重复显示 | 本节速览 |
| 44 | 德州扑克进入房间崩溃 `null.length` | 本节速览 |
| 47a/47b | 国际象棋坐标 + WS 重连状态覆盖 | 本节速览 |
| 48 | 玩家 reload 后 game.started 硬编码 move_count:0 | 本节速览 |
| 49 | 狼人杀等待阶段无聊天/退出 | 本节速览 |
| 50 | 象棋/国际象棋 reload 后 game.state 缺失 | 本节速览 |
| 51 | 狼人杀黎明阶段无 UI 触发 start_day | 本节速览 |
| 80–92 | 狼人杀 Agent 核心教训速览（MyTurn / lock / Prune / restart_vote 等） | [`docs/狼人杀-Agent与系统/`](docs/狼人杀-Agent与系统/) |
| 92a | **§92a 锁内自死锁**：`Action_*` 必须建 `*Locked` 锁内变体 | 狼人杀-Agent与系统 |
| 93 | 每个活跃狼人杀房间必须有 Phase Watchdog 后台 goroutine | 狼人杀-Agent与系统 |
| 94–97 | R39–45 测试教训（boot-restart / CDP / 网络 / 阶段死锁） | 狼人杀-Agent与系统 |
| 108 | R48 修复（quarantine-skip / game-over-db-sync / retry-cooldown） | 狼人杀-Agent与系统 |
| 111 | 500K 聊天历史队列（房间共享 + ReadPointer） | 狼人杀-Agent与系统 |
| 112 | 发言下限 + 阶段时钟 + 观众唤醒 | 狼人杀-Agent与系统 |
| 117 | 重开局投票流程 | [`docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md`](docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md) |
| 129 | 游戏结束后冷却期 | 狼人杀-Agent与系统 |
| 130 | 主持人 Agent 重构（Provider 注入 + 启动条件 + 阶段事件） | [`docs/狼人杀-重构方案/主持人Agent重构设计.md`](docs/狼人杀-重构方案/主持人Agent重构设计.md) |
| 131 | Agent 持久化记忆跨局迭代学习 | [`docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md`](docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md) |
| 134 | 守卫(Guard)角色全链路补全 | [`docs/狼人杀-角色设计/狼人杀守卫角色设计.md`](docs/狼人杀-角色设计/狼人杀守卫角色设计.md) |
| 135 | 身份公开公平性 —— 死者身份牌不翻开 | 狼人杀-角色设计 |
| 197 | 流式续命 — "接收到字节即刷新超时" | 狼人杀-Agent与系统 |
| 198 | 法官模式三选项→两选项重构 | 狼人杀-重构方案 |
| 212 | §92a 自死锁致「创建房间弹窗卡死 + 永久正在同步…」(R212) | 狼人杀-Agent与系统 |

#### LLM / Provider / Wiring 类

| §  | 教训 | 文档路径 |
|---|---|---|
| 118 | 模型管理 + 模型玩家持久化 + 模型金币 | [`docs/狼人杀-道具与经济/模型管理与持久化玩家设计.md`](docs/狼人杀-道具与经济/模型管理与持久化玩家设计.md) |
| 119 | Agent「心口不一」+「说谎/讲故事」能力（协议层隔离） | 狼人杀-重构方案 |
| 120 | Agent API 调用时间差异公平性 | 狼人杀-Agent与系统 |
| 121 | 模型管理页面渲染崩溃（后端 data 形状与前端类型不匹配） | 狼人杀-重构方案 |
| 122 | Agent 单 bot 内多线程 LLM 调用（默认关闭） | 狼人杀-Agent与系统 |
| 123 | Agent 法官 + 死亡语义区分（execution / death） | [`docs/狼人杀-重构方案/主持人Agent重构设计.md`](docs/狼人杀-重构方案/主持人Agent重构设计.md) + [`docs/狼人杀-角色设计/狼人杀死亡语义设计.md`](docs/狼人杀-角色设计/狼人杀死亡语义设计.md) |
| 213 | emotion_switch → emotion_switch_speak 合并重构 | [`docs/LLM与Agent/Agent工具定义-解决和设计方案-20260804-01.md`](docs/LLM与Agent/Agent工具定义-解决和设计方案-20260804-01.md) |
| 20260807-04 | Agent 道具对齐 6 类 LLM 注入攻击 | [`docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md`](docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md) |
| 20260811-08 | §130「声明了却从不接线」集中清算 + `redactLedgerFact` P0 | 狼人杀-重构方案 |
| 20260812-03 | 狼人杀 Agent 4 项升级（胜率热力图 / 暗线信件 / 阵营赌注 / 3 条核心理由） | 狼人杀-重构方案 |
| 20260812-04 | §130 第五次复发 + P0-1 神职私有信息从未进 prompt + wiring lint | 狼人杀-重构方案 |
| 20260812-DP | 聊天分页 5 条教训 | 狼人杀-重构方案 |
| 20260812-LLM | LLM Provider DB 单源化 3 条决策 | 狼人杀-重构方案 |
| 20260813-04 | §130 第七次复发 + Hermes Agent 对标优化 | [`docs/其他Agent代码分析/Hermes_Agent_源码分析.md`](docs/其他Agent代码分析/Hermes_Agent_源码分析.md) |
| 20260813-05 | DeepSeek Harness 借鉴 U2/U3/U5 落地 | [`docs/狼人杀-重构方案/狼人杀Agent_根据_deepseek-harness_优化和解决方案_20260813.md`](docs/狼人杀-重构方案/狼人杀Agent_根据_deepseek-harness_优化和解决方案_20260813.md) |
| 20260814-02 | OpenCode 启发的 6 项升级（CachePolicy / WolfWhisper / LLMSlot / BotTranscript 分桶 / AgentRunTrace / AgentWiringLint） | [`docs/狼人杀-重构方案/狼人杀Agent_根据_OpenCode_优化和解决方案_20260814.md`](docs/狼人杀-重构方案/狼人杀Agent_根据_OpenCode_优化和解决方案_20260814.md) |

#### 道具 / 经济 / 游戏机制类

| §  | 教训 | 文档路径 |
|---|---|---|
| 124 | Day/Night 视觉特效（CSS-only） | 狼人杀-前端UI |
| 125 | 法官「整局总结」+ 模型记忆持久化 | 狼人杀-重构方案 |
| 126 | Agent「思考中…」多态指示器（R128 已重构删除） | 狼人杀-Agent与系统 |
| 128 | 「对话即思考」重构（R128） | [`docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md`](docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md) |
| 132 | 道具系统 v1.1 补缺 + §130 死代码回归 | [`docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md`](docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md) |
| 133 | 道具系统 v4 重构（狼小队交流 + 经济档位 + 效果链） | [`docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md`](docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md) |

#### 前端结构 / UI 类

| §  | 教训 | 文档路径 |
|---|---|---|
| 136 | 前端目录结构重构 —— 消除跨游戏耦合 | [`docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md`](docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md) |
| 137 | Agent 道具系统对齐 6 类 LLM 注入攻击 | [`docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md`](docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md) |

#### 高频速览（不另归档，Agent 须熟记）

- **§43–§51**：通用 UI 教训速览（4.5 节前文已删）—— 详见 git log 检索 `§43` / `§47a` / `§48` / `§50`。
- **§80–§92**：狼人杀 Agent 核心教训速览 —— 关键词表见本节第一张表。
- **§92a**：**最关键** —— `sync.Mutex` 不可重入，凡 `WerewolfRoom` 方法被 `BuildClientState*` 调用必须用 `*Locked` 变体。
- **§130 / §134 / §135**：**「声明了却从不接线」三次复现模式** —— 凡新增 helper / 角色 / 字段必须有 grep 验证接线。

- 自动重连、Loading 遮罩、刷新/断线后恢复（会话+房间+对局），以及用户列表 `user.*` 帧的完整规则，
  记录在 [`docs/架构与协议/WebSocket重连与恢复.md`](docs/架构与协议/WebSocket重连与恢复.md)。
- 用户列表权限分级见 [`docs/架构与协议/用户类型与权限.md`](docs/架构与协议/用户类型与权限.md)。
- 前端 WS 连接生命周期由 `AppLayout` 唯一持有，页面切换不得 connect/close。

## 19. 斗地主 (Doudizhu) 架构

斗地主是平台首个**卡牌类 / 3 人**游戏（1 地主 + 2 农民）。

### 与棋类的关键差异

| 特征     | 棋类 (xiangqi/chess/junqi)  | 斗地主 (doudizhu)                                |
| -------- | --------------------------- | ------------------------------------------------ |
| 玩家数   | 2                           | 3                                                |
| 房间容量 | 2                           | 3                                                |
| 阶段     | layout→playing              | bidding→playing→over                             |
| 隐藏信息 | junqi 暗棋模式              | 手牌始终隐藏                                     |
| WS 帧    | game.join/move/resign/state | game.join/**bid**/**play**/**pass**/resign/state |
| 视图分发 | BroadcastRoom 统一棋盘      | **BroadcastTo 按座位单独推送**                   |

### 后端分层

- **`ServerGo/game/doudizhu/`** — 纯引擎包：`cards.go` / `combo.go` / `engine.go` / `view.go` / `room.go` / `engine_test.go`
- **`ws/game_service.go`** — 注册分发：`game.bid/play/pass` → `BroadcastTo` 按座推 `game.state`
- **`service/room_service.go`** — `CreateRoom` 根据 `game_kind` 设置容量（doudizhu=3）

### 前端分层

- **类型**: `types/doudizhu.ts` / **资产**: `assets/images/doudizhu/index.ts`（PNG + CSS 回退）/ **Store**: `store/doudizhu.store.ts` / **Hook**: `hooks/useDoudizhu.ts`
- **页面**: `DoudizhuLobbyPage` + `DoudizhuGamePage`（路由 `/doudizhu`、`/doudizhu/:roomId`）
- **组件**: `CardView` / `HandPanel` / `BidPanel` / `PlayControls` / `DoudizhuTable` / `GameInfoPanel`

### 两种风格（运行时实时切换）

| 风格       | StyleKey               |
| ---------- | ---------------------- |
| 传统地主   | `traditional_landlord` |
| 都市打工仔 | `urban_worker`         |

美术资源由 `python-generate-image-tool/generate_doudizhu_assets.py` 生成 PNG（火山引擎 Ark API）。

### WS 帧协议

```
客户端→服务端: game.join / game.bid / game.play / game.pass / game.resign / game.leave / game.state
服务端→客户端: game.joined / game.started / game.bidded / game.redealt / game.played / game.passed / game.state / game.over / game.error
```

完整规则见 [`docs/斗地主/斗地主规则与协议.md`](docs/斗地主/斗地主规则与协议.md)。

## 19.5 观战者 (Spectator) — 跨 5 款游戏

任何登录用户都可以进入任意活跃者房间以观察者身份实时观看，**不消耗座位，不影响玩家 UI**。
底层隔离由 `Hub.rooms` 与 `Hub.spectators` 两组互不相交的广播集合实现。详见
[`docs/架构与协议/观战者架构.md`](docs/架构与协议/观战者架构.md)。

要点：
- 玩家输入帧在观察者身上后端硬性拒绝 → `ErrSpectatorInputForbidden = 30011`。
- 路由：`/<game>/spectate/:roomId`；Hook：`useSpectatorMode()`；HTTP：`POST /api/rooms/:id/spectate` / `.../leave_spectate`。

## 20. 德州扑克 (Texas Hold'em) 架构

德州扑克是平台第五款游戏（**2-6 人**、No-Limit），差异：押注轮 + 共享公共牌 + 牌型评估 + `game.action` 统一动作。

### 与斗地主的关键差异

| 特征     | 斗地主             | 德州扑克                         |
| -------- | ------------------ | -------------------------------- |
| 玩家数   | 3                  | 2-6                              |
| 房间容量 | 3                  | 6                                |
| 牌组     | 54 张（含王）      | 52 张（无王）                    |
| 阶段     | bidding→playing    | preflop→flop→turn→river→showdown |
| WS 帧    | game.bid/play/pass | **game.action**（type+amount）   |

### 后端分层

- **`ServerGo/game/texasholdem/`** — 纯引擎包：`cards.go` / `hand.go` / `engine.go` / `view.go` / `room.go` / `engine_test.go`
- **`ws/game_service.go`** — `game.action`→`handleTexasHoldemAction`；`BroadcastTo` 按座推 `game.state`
- **`service/room_service.go`** — `CreateRoom` 容量（texasholdem=6）

### 前端分层

- **类型**: `types/texasholdem.ts` / **资产**: `assets/images/texasholdem/index.ts` / **Store**: `store/texasholdem.store.ts` / **Hook**: `hooks/useTexasHoldem.ts`
- **页面**: `TexasHoldemLobbyPage` + `TexasHoldemGamePage`
- **组件**: `CardView` / `CommunityCards` / `PlayerSeat` / `ActionControls` / `TexasHoldemTable` / `GameInfoPanel`

### 两种风格

| 风格     | StyleKey            |
| -------- | ------------------- |
| 西部牛仔 | `western_cowboy`    |
| 荒野逃生 | `wilderness_escape` |

美术资源由 `python-generate-image-tool/generate_texasholdem_assets.py` 生成 PNG。

### WS 帧协议

```
客户端→服务端: game.join / game.action {type,amount} / game.state / game.resign / game.leave
服务端→客户端: game.joined / game.peer_joined / game.started / game.action_accepted / game.state / game.over / game.error
```

完整规则见 [`docs/德州扑克/德州扑克规则与协议.md`](docs/德州扑克/德州扑克规则与协议.md)。

## 21. Agent 自动化测试账号

AI Agent 在本地开发环境跑自动化登录、回归或 e2e 时,**必须**使用
[`docs/通用功能/测试账号凭证.md`](docs/通用功能/测试账号凭证.md) 中预置的账号:

| account | 验证码旁路 | 备注 |
| --- | --- | --- |
| `test19082jauishf8` | ✅ 可省略 `captcha_id` / `captcha_answer` | 旧版单账号种子(§6) |
| `test_01` | ✅ 可省略 `captcha_id` / `captcha_answer` | 批量测试套件(§7) |
| `test_02` | ✅ 可省略 `captcha_id` / `captcha_answer` | 批量测试套件(§7) |
| `test_03` | ✅ 可省略 `captcha_id` / `captcha_answer` | 批量测试套件(§7) |
| `test_04` | ✅ 可省略 `captcha_id` / `captcha_answer` | 批量测试套件(§7,密码含 `;`) |

> **密码不在任何 git 跟踪的文件中**。请从仓库根目录的 `test_account.json`
> (machine-readable,已在 `.gitignore` 排除)读取。
>
> 旁路白名单定义于 `ServerGo/service/auth_service.go` 的 `AgentBypassAccounts`,
> **仅在 `cfg.Server.DevMode=true` 时生效**。生产部署必须显式设置
> `DevMode=false`,否则 CAPTCHA 旁路会被禁用(防御深度)。
>
> 仅限本地/开发环境使用,**严禁**在生产环境复用。

- 账号可能尚未落库:先 `GET /api/invites` 取公开邀请码,再 `POST /api/auth/register`。

## 22. 自动化测试报告处理流程

> R46–R51 等多轮「报告→修复→验证」历史快照已迁出本文件，按需查阅 `docs/` 下归档或 git log。
> 本节仅保留**流程规约**，不再记录每轮具体报告内容。

- **检索入口**：主工程 `TestReport/自动化测试报告_*.md`；子工程 `go-web-debug-tool/UseReport/测试工具使用报告_*.md`。
- **处理入口**：根目录 `AutoDebugTestReport.sh` —— **每次运行随机选一个可用编程 Agent CLI**（Claude Code / OpenCode / Hermes / OpenClaw；公共库 `agent_cli_common.sh`，`AGENT_CLI=<name>` 可强制指定），加载 `AutoDebugTestReport.md` 作为 prompt；`AutoTestAndSaveReport.sh` / `AutoScreenshotWerewolf.sh` 同机制。
- **流程规范与硬约束**详见 `AutoDebugTestReport.md`；其中**绝对禁止**自动修复流程写入 `CLAUDE.md` / `AGENTS.md` 这两个规则文件。
- **报告清理**：修复完成后必须删除已处理的 `TestReport/*.md`（子工程 `UseReport/*.md`），报告不应在仓库中长期堆积。

## 23. 狼人杀 Web 运行时 UI（房间总运行时间 + 历史抽屉）

> 2026-07-18 用户反馈响应。详见 [`docs/design/狼人杀13人局UI运行时优化设计.md`](docs/design/狼人杀13人局UI运行时优化设计.md)。

- **`game_started_at` 下发**：服务端 `WerewolfRoom.gameStartedAt`(已存 6 处写入) 之前只入 `agent.GameContext.GameStartedAt` 给 LLM；从未下发到 `ClientGameState`。**修复**：`ServerGo/game/werewolf/view.go` 给 `ClientGameState` 加 `GameStartedAt int64 \`json:"game_started_at,omitempty"\``(`omitempty` 保证 0 不污染历史回放)。
- **前端新组件 `RoomRunningClock.tsx`**：1s `setInterval`（可选注入 `nowMs`）计算 `nowMs - gameStartedAt*1000`,格式化 `{HH:}MM:SS`,未开局显 `⏱ —`,结束态显 `整局`。
- **新抽屉 `HistoryDrawer.tsx`**：与 `FactionDrawer` 同宽同位（380px / 30vw），4 sub-tab：⏱ 时间轴 / 🤖 独白 / ⚰ 死亡 / 🏆 总结。ESC 关闭,焦点循环在抽屉内。
- **3 入口冗余**：Header 顶层 "📜 历史"（主入口,sticky）+ 房间信息面板 header 并列按钮（次入口,`📚 500K` 旁）+ `GameInfoPanel` 第 5 块改三按钮(规则/历史/退出)。
- **`max_seat ?? 12` 兜底错位**：玩家页 fallback 与 i18n/`RoomCreateModal`/`WerewolfTable` 不一致(后者已迁 13),会导致等待阶段显示 `等待 12 位玩家入座…`。修复为 `?? 13`,保持一致。
- **触控 44 × 44 token**：`.werewolf-game { --ww-touch-target: 44px }`,作用域隔离（仅狼人杀）。
- **中栏紧凑间距**：`<1599px` `board-container` 内 gap/pad 由 10px 缩到 6px；`<1280px` 再压到 4px。
- **i18n 三语同步**:zh-CN/en/ja 全部补 `werewolf.history.*` 21 键。**教训**:`http<T>` 拆 wrapper 类型外,新 UI 字段必须**同时**进 `i18n/types.ts` + 三语文件。
- **数据来源 100% 复用现有字段**：时间轴 / 死亡 / 总结 / 独白全部用 `game.state` 中已有字段,**不新增 HTTP API**。

## 24. AgentClassName 与 User-Agent 拼装约定

> 2026-08-06 §Agent 重构增强。每种 Agent 实现都有一个独立的 `AgentClassName`，统一登记在 [`ServerGo/agent/class_names.go`](ServerGo/agent/class_names.go)。

### 24.1 当前已注册

| AgentClassName | 实现 | 调用 LLM 的场景 |
|---|---|---|
| `LsmAgentGame-Werewolf-Player` | `ServerGo/agent/wwplayer.Agent` | 玩家 Bot 主对话 + speak_floor_tick 自救路径 |
| `LsmAgentGame-Werewolf-Judge` | `ServerGo/agent/wwjudge.AgentJudge` | 法官宣告 / prompt_actor / summary / declare_cause |
| `LsmAgentGame-Werewolf-MemoryIter` | `game/werewolf/agent_memory_bridge.go::IterateAgentMemoriesAsync` | 整局总结后异步自我迭代 MEMORY.md |

### 24.2 命名规则

- 狼人杀玩家 Bot → `LsmAgentGame-Werewolf-Player`
- 狼人杀法官 Bot → `LsmAgentGame-Werewolf-Judge`
- 其他游戏玩家 Bot → `LsmAgentGame-<Game>-Player`（例：未来 `LsmAgentGame-Doudizhu-Player`）
- 其他游戏法官/裁判 Bot → `LsmAgentGame-<Game>-Judge`
- 工具型 Agent（记忆迭代） → `LsmAgentGame-<Game>-MemoryIter`

### 24.3 User-Agent 出站拼装

**格式**：`User-Agent: <AgentClassName>/<AppVersion> <buildDateTime>`

- `AppVersion` 与 `buildDateTime` 来自 `ServerGo/main.go`（ldflags `-X main.AppVersion=...` / `-X main.buildDateTime=...` 注入；rebuild_restart_app.sh 强制覆盖）。
- 版本+时间注入仍走 `llm.Registry.SetUserAgent(...)`（main.go 一次性注入），**拼装的"程序名前缀"由 `LLMRequest.AgentClassName` 在出站前覆盖**（`llm/anthropic/anthropic.go::userAgentFor`）。
- 例：`p.userAgent = "LsmAgentGame/v1.0.0-7740495 Aug  6 2026 11:37:43"` + `req.AgentClassName = "LsmAgentGame-Werewolf-Player"` → 出站 `User-Agent: LsmAgentGame-Werewolf-Player/v1.0.0-7740495 Aug  6 2026 11:37:43`。

### 24.4 接入新 Agent 的 4 步

1. 在 `ServerGo/agent/class_names.go` 追加 `AgentClass<Game><Role>` 常量 + `AllAgentClassNames()` 切片。
2. 在 Agent 实现的 `llm.LLMRequest{...}` 构造点填 `AgentClassName: string(agentroot.AgentClass<Game><Role>)`。
3. `import agentroot "LsmAgentGame/agent"`（用别名避免与本包符号冲突）。
4. 单元测试断言 `LLMRequest.AgentClassName != ""`，让"忘记设置"立即失败。

### 24.5 设计动机

- **同一 LLM Provider 被多类 Agent 复用**（如法官与玩家共用同一 model_key），上游/网关需要通过 UA 区分调用方做计费/限流/审计。
- **`AgentClassName` 与 `ModelKey` 正交**：前者决定**是哪一类调用方**（业务身份），后者决定**用哪个 LLM 模型**（推理引擎）。

## 25. Agent 道具与 LLM 注入攻击对齐（§20260807-04）

> 仓库 6 份注入攻击演示文件(`docs/注入攻击演示/01-06-*.md`)是 Agent 道具系统的事实来源。详见 [`docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md`](docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md)。

### 25.1 三类攻击分类（事实来源 vs 落地方向）

| 攻击文档 | 攻击类型 | 落地方向 | 已实现道具 |
|---|---|---|---|
| `第一种：Markdown 格式注入` | Agent → 人类 | 注入 → `gc.PropInjectText` + UI 公告前缀 debuff | `markdown_bomb` + `md_bomb_human` |
| `第二种：提示词套娃（多层嵌套）` | Agent → 人类 | 注入 → 投票推荐 debuff | `nested_maze` + `nested_maze_human` |
| `第三种：字符级欺骗（混淆式）` | Agent → 人类 | 注入 → 发言乱码 debuff | `char_confuse` + `char_confuse_human` |
| `第四种：长上下文注意力失焦` | Agent → Agent | `long_swear`(AOE) → 所有存活 bot `propInjectQueue` + EffectTypes | `long_swear`(v20260807-04 修复 AOE 入队) |
| `第五种：任务马甲` | Agent → Agent | `expose_identity` + `emotion_disturb_light` 干扰信号 | `task_disguise` + `task_disguise_v3` |
| `第六种：情绪操控` | Agent → Agent | `emotion_disturb`(下轮 confused/guilty) | `emotion_plea` |

### 25.2 §20260807-04 关键修复清单

- **`isExposeProp` 补 `PropTaskDisguiseV3`** —— `prop_engine.go:307`
- **AOE 道具双路径入队** —— `room_action.go:~749` + `agent_runner.go:~1152`
- **人类反制 debuff** —— `prop_catalog.go` + `prop_effect.go` + `view.go` 透传
- **注入文本按角色差异化** —— `prop_inject.go` `roleSpecificInduction` helper
- **过期递减索引遍历** —— `room_prop.go:413`
- **`PropHitLastRound` 反馈闭环** —— `room_agent.go:1044` + `prop_blocks.go`
- **`PropSystemPrompt` 去硬编码** —— `prop_blocks.go:30`

### 25.3 新增道具速查

| PropKey | 中文名 | 价格 | 中招率 | TargetCamp | EffectType |
|---|---|---|---|---|---|
| `md_bomb_human` | 公告轰炸 | 130 | 30% | human | human_announce_prefix |
| `nested_maze_human` | 剧本迷宫·人 | 160 | 25% | human | human_vote_suggest |
| `char_confuse_human` | 乱码干扰 | 90 | 22% | human | human_char_garble |

> `TargetCamp:"human"` 是 §20260807-04 引入的新枚举值,`prop_engine.go::UseProp` 校验目标必须 `!IsBot`。

### 25.4 验收依据

- `ServerGo/game/werewolf/prop_aoe_test.go` 8 项测试全 PASS(AOE 入队 / HumanDebuff 落地 / ExpiresAfter 递减 / 角色差异化 / EffectRegistry 注册 / InjectRegistry 注册)
- `go build ./...` + `go test ./game/werewolf/... ./agent/... -count=1` 全部通过

## 26. 前端 UI 颜色对比度与可读性规范

> **2026-08-08 §20260808-02 用户反馈响应**。通用规约全文见
> [`docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md`](docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md),
> 本节留硬约束摘要供 Agent 高频查阅。

### 26.1 暗色主题对比度硬阈值

| 元素类型 | WCAG 等级 | 项目硬阈值 |
|---|---|---|
| 正文 / 列表项 / 表单文字 | AA | **≥ 4.5:1** |
| 大字体(≥18pt / 14pt 加粗) | AA | **≥ 4.0:1** |
| 状态徽章 / badge | AA | **≥ 5.0:1** |
| selected / active 焦点态 | AAA | **≥ 6.0:1** |

### 26.2 五大反模式(一票否决)

1. **红底红字 / 绿底绿字 / 蓝底蓝字** — 同色相叠加再高透明度也没救。文字改白 + 背景 ≥ 45% 不透明。
2. **浅色文字 + 25% 透明背景** — 暗色主题常见反模式。改成 `rgba(..., 0.45)` 起跳。
3. **`color: inherit` + 半透明背景** — 子元素必须显式声明 `color`。
4. **选中态与未选态差异 < 1.5 倍明度** — 必须加 `box-shadow` 光晕(不被 night brightness 衰减)。
5. **`opacity: 0.4` 表示 disabled** — 污染选中态判定,改用单独灰底 + `cursor: not-allowed`。

### 26.3 原生 `<select>` 四管齐下

`color-scheme: dark` + `appearance: none` + `option { background + color }` 显式深底浅字 + `:checked/:hover` 紫底白字加粗。详见规范文档 §3。

### 26.4 状态徽章色相规约(色相库,禁新创)

| 语义 | 色相 | 底透明度 | 字色 | 字重 |
|---|---|---|---|---|
| 「我」(self) | 红 | ≥ 0.45 | 白 | 700 |
| 「警长」 | 金 | ≥ 0.45 | 白 | 700 |
| 「白痴翻牌」 | 紫 | ≥ 0.55 | 白 | 700 |
| 「处决」 | 橙 | ≥ 0.7 | 白 | 700 |
| 「死亡」 | 灰 | ≥ 0.45 | 白 | 700 |
| 「Bot AI」 | 青绿 | ≥ 0.55 | 浅青绿/白 | 600 + text-shadow |
| 「法官」 | 金黄 | ≥ 0.35 | 深金/白 | 700 |
| 「quarantined/告警」 | 红 | ≥ 0.55 | 白 | 700 |

### 26.5 经济档位 / 风险档位三件套规约

任何后端下发枚举值拼接为 className 的样式,**必须**同提交完成:JSX 拼接 + CSS 类规则 + `@keyframes + prefers-reduced-motion` 兜底。写完 JSX 后**立即** `grep -rn "<新class前缀>" ClientWeb/src/styles/`,零命中即 P1 缺陷(2026-08-08 §20260808-02 已踩坑:`econ-tier-${econTier}` 三档零 CSS 规则)。

### 26.6 验收 checklist(摘要)

- [ ] 主要文字对比度 ≥ 4.5(devtools Inspect → Accessibility)
- [ ] `<select>` 三浏览器(Chrome/Firefox/Safari)展开均为深底浅字
- [ ] `is-selected` 在 `.is-night brightness(0.4)` 滤镜下肉眼可辨
- [ ] `tsc --noEmit` + `npm run build` 通过
- [ ] 新 className grep 至少一处命中(防「声明了却从不接线」)

### 26.7 教学文档归档

- **本节规约(CLAUDE.md)** — 摘要高频查阅;**详细规约**:[规范文档](docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md)
- **实战案例 + 修复 diff**:[审计报告 20260808-02](docs/狼人杀-前端UI/狼人杀13人局-前端UI颜色对比度审计报告-20260808-02.md)
- **代码改动**:RoomCreateModal `<select>` 四管齐下 + chat bot badge + 13 处徽章/chip 对比度提升,统一打 §20260808-02 标签