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
  引用它时用 `CLAUDE.md §<编号>` 即可。外部贡献者无需理解全部 §编号即可参与贡献（参见 `CONTRIBUTING.md`）。
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
lag_docs/                       架构设计、鉴权流程、API 参考
python-generate-image-tool/ 子模块 —— AI 图像生成
go-web-debug-tool/          子模块 —— Chrome CDP 自动化调试服务
```

### 2.1 前端目录约定（`ClientWeb/src`）

> 2026-08-07 §20260807-03 重构确立。方案见
> [`lag_docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md`](lag_docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md)。

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
1. **禁止跨游戏 import** —— `components/<gameA>/` 不得引用 `components/<gameB>/`。多款游戏共用的组件必须上移到 `components/chat|common|ui/` 等共享目录。
2. **游戏私有代码必须落在 `<game>/` 内** —— 判据是「引用方是否 100% 属于该游戏」。共享目录（`shared/` `components/ui|common/`）中出现单一游戏的 i18n 键或类型即为违规。
3. **共享工具只有一处**：`shared/utils/`。不得再新建 `util/` 或 `utils/`。
4. **`styles/globals.css` 的 `@import` 顺序不可调整** —— `werewolf.css` → `werewolf-v2.css` → `werewolf-emotion.css` → `werewolf-speech.css` 之间存在同优先级选择器覆盖链（`.werewolf-seat` 等），改序即样式回归。CSS 文件超 §4 行数上限时，**只能整段搬移 + 在原位置插入 `@import`**，并以「构建产物 CSS 字节一致」验证零回归。

## 2.5 lag_docs/ 知识库索引

按主题分组的完整索引见 [`lag_docs/README.md`](lag_docs/README.md)。新增/迁移文档前请先阅读 §3 命名规约。

| 主题 | 路径 |
|---|---|
| LLM Provider 协议 / API 优化 / 工具集 / 拟人化 | `lag_docs/LLM与Agent/` |
| 注入攻击演示（道具系统事实来源） | `lag_docs/注入攻击演示/` |
| 架构、API、WS 协议、鉴权、观战者 | `lag_docs/架构与协议/` |
| 狼人杀 Agent 设计 / 升级批次 / 上下文压缩 | `lag_docs/狼人杀-Agent与系统/` |
| 狼人杀重构方案 / 借鉴第三方 Agent 平台 | `lag_docs/狼人杀-重构方案/` |
| 狼人杀角色卡池完整性 / 死亡语义 | `lag_docs/狼人杀-角色设计/` |
| 狼人杀 UI 与设计 | `lag_docs/狼人杀-前端UI/` / `lag_docs/狼人杀-设计/` |
| 狼人杀道具系统 / 金币 / 模型玩家 | `lag_docs/狼人杀-道具与经济/` |
| 通用功能（i18n / 测试 / 布局 / 子代理） | `lag_docs/通用功能/` |
| 第三方 Agent 平台分析（DeepSeek / Hermes / OpenCode / PI） | `lag_docs/其他Agent代码分析/` |

## 3. 文件命名规范

- `ServerGo/models/` 下的 **GORM 模型文件** 使用前缀 `t_lsm_game_*.go`（例如 `t_lsm_game_user.go`）。这是**唯一允许**使用此前缀的目录。
- `ServerGo/` 下的**其他所有 Go 文件** 使用 `snake_case.go`（例如 `user_login.go`、`game_logic.go`）。非模型文件切勿使用 `TLsmGame_xxx.go`。
- **Markdown** 规则文件（CLAUDE.md、AGENTS.md）不超过 800 行。如文件过长，请按主题拆分到 `lag_docs/` 目录。

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

**每个 `catch` 块的自检清单**：
1. `isSessionExpiredError(e)` 这类会重走登录弹层的错误 → 不再重复展示友好文案。
2. 其它错误 → 要么 `setErr(e.message)` 在当前页最高可见位置渲染、要么 `reportGlobalError(...)` 上报到全局 toast（**两者至少其一，推荐两者都做**）。
3. 纯 best-effort 的静默失败可免。

## 8. 前端打包工具

Vite 是打包工具。规范中写的是 "Webpack/Rollup"——Vite 在**生产构建时内部使用 Rollup**，因此规范已满足。不得引入 Webpack 配置文件。未经书面决策不得迁移到 Next.js。

## 9. 子模块初始化

克隆后**必须**执行 `git submodule update --init --recursive`（否则美术素材生成失败、自动化测试与浏览器调试不可用）：`python-generate-image-tool/`（AI 图像生成）+ `go-web-debug-tool/`（Chrome CDP 自动化调试服务）。

## 10. 工作流程

1. 从计划中选取一个阶段，将其任务标记为 `in_progress`。
2. 编译 → 测试 → 小步提交。尽可能每个逻辑阶段对应一次提交。
3. 进行任何非琐碎更改前，重新阅读相关的 `lag_docs/` 文件。如有结构性变更，请同步更新文档。
4. 切勿在提交中重写 `LsmAgentGame.conf`、`server.crt` 或 `server.key`。

### 10.1 所有改动保持在 `main` 分支

> **2026-07-10 §122 起的硬约束**。本仓库的所有新功能添加、优化、修改、debug
> **必须** 直接在 `main` 分支上进行；不再使用特性分支(除非经用户显式要求)。
> `main` 是**唯一长期维护的代码线**与 CI 默认基线。

**操作规约**:
- 会话开始时第一条命令必须是 `git branch --show-current`,若不在 `main` 立即 `git checkout main`。
- **禁止**创建新分支 (`git checkout -b` / `git switch -c`)。
- 服务 (`./rebuild_restart_app.sh`) 和单测 (`go test ./...`) 始终跑 `main` HEAD。

### 10.2 AI Agent Tools 规则文件同步

> `CLAUDE.md` 同时被多个 AI Agent 工具加载，作为项目"系统词"单一事实来源。任何规则更新必须**同步**到所有下游入口。

| Agent 工具 | 入口文件 | 加载方式 |
|------|------|------|
| **Claude Code** (`Claude`) | `CLAUDE.md` (项目根) | 自动读取根目录 `CLAUDE.md` |
| **Kilo Code / OpenCode / pi / OpenClaw / Hermes** 等其它 Agent | `AGENTS.md` | 项目根 `AGENTS.md` **必须**是 `CLAUDE.md` 的符号链接 |

**同步规约**:
- `AGENTS.md` **必须**是 `ln -s CLAUDE.md AGENTS.md` 创建的符号链接 — 修改一次,所有工具同步生效。
- **若发现 `AGENTS.md` 是普通文件而非符号链接**:立刻 `rm AGENTS.md && ln -s CLAUDE.md AGENTS.md` 修正。
- CI 可加一步 `test -L AGENTS.md` 防止误把符号链接替换为普通文件。

## 11. 常见陷阱

- **子模块路径使用本地 file://** —— 在当前网络下没有问题。如果在局域网外克隆，请将 `.gitmodules` 更新为真实的远程地址。
- **生成的 proto 文件已加入 gitignore** —— 编辑任何 `.proto` 文件后需重新运行 `proto/gen.sh`。
- **Vite 开发服务器代理** `/api` 和 `/ws` 到 `127.0.0.1` 的 HTTPS，设置 `secure: false` 以接受自签名证书。移除代理时请同时更新配置和开发工作流。
- **MariaDB 是默认数据库** —— GORM 驱动为 `mysql`（同时兼容 MySQL 和 MariaDB）。未经书面决策不得切换到仅支持 Postgres 的功能。

## 12. 国际化与命名补充

- **多国语言(i18n)**、`t_lsm_game_user.language` 字段、`/api/user/*` 偏好接口，以及**服务器测试文件 `test_*_test.go`、临时/数据补全文件 `temp_*.go`** 的命名约定，统一记录在 [`lag_docs/通用功能/国际化与命名规范.md`](lag_docs/通用功能/国际化与命名规范.md)。
- 涉及上述任一主题前请先阅读该文档；新增/删除语言时前后端 `SUPPORTED`/`SupportedLanguages` 必须同步。

## 13. SubAgent 分工协作规则

> **核心原则：按职责拆分，每条职责线 = 一个独立 SubAgent。** 无法精确覆盖的工作面必须
> 新建独立 SubAgent 并写入 [`lag_docs/通用功能/子代理角色.md`](lag_docs/通用功能/子代理角色.md)。

### 13.1 8 条职责线

| # | 职责线 | SubAgent 代号 | 工作面 | 触发关键词 |
|---|--------|--------------|--------|-----------|
| 1 | `ClientWeb/` 前端游戏设计 | `frontend-dev` | 仅修改 `ClientWeb/` | React、UI、组件、路由、样式、3D |
| 2 | `ServerGo/` 后端游戏设计 | `backend-dev` | 仅修改 `ServerGo/` | Go、API、数据库、WebSocket、GORM |
| 3 | 游戏规则与产品设计 | `game-designer` | 仅产出 markdown 文档与契约 | 规则、玩法、需求、产品规划 |
| 4 | 界面设计与图像生成 | `art-designer` | `python-generate-image-tool` + `ClientWeb/src/assets/` + `3d_script/` + `ClientWeb/src/assets/models/` | 美术素材、图片生成、配色、Blender 3D 模型（v19 §27 起）|
| 5 | 跨端联调 / 复杂规则 | `integration-tester` | 同时涉及 `ClientWeb/` 与 `ServerGo/` | 跨端联调、全栈、Game QA |
| 6 | 游戏策划视觉设计 | `game-visual-designer` | 仅产出 `lag_docs/design/**` 设计文档 | 视觉稿、布局、design tokens |
| 7 | **LLM Provider 模块** | `llm-provider` | `ServerGo/llm/`, `api/llm_api.go`, `config.LLMConfig` | LLM、Anthropic、OpenAI、Provider |
| 8 | **狼人杀 Agent 驱动** | `werewolf-agent` | `ServerGo/agent/`, `ServerGo/game/werewolf/` 的 Agent 接入 | Agent、bot、Memory、工具派发 |

### 13.2 派遣硬约束

- ✅ **单一职责**：每个任务只派遣一个最匹配的 SubAgent。
- ✅ **最小权限**：SubAgent 只能修改其工作面内的文件。
- ✅ **新建而非复用**：覆盖不到的职责线必须新建独立 SubAgent。
- ❌ **不得越俎代庖**：主 Agent 不直接修改 `ClientWeb/`、`ServerGo/`、`python-generate-image-tool/` 或 `go-web-debug-tool/`。
- 🔁 **显式交接**：跨职责线任务按 `game-designer → game-visual-designer → art-designer → frontend-dev / backend-dev → integration-tester` 顺序串联。

## 14. LLM Provider 模块（Anthropic 协议 + OpenAI 预留）

> 通过统一 `LLMProvider` 接口调用大模型。详见 [`lag_docs/LLM与Agent/LLM供应商设计.md`](lag_docs/LLM与Agent/LLM供应商设计.md)。

- **`ServerGo/llm/types/`** —— leaf 包：Anthropic wire 类型 + `LLMProvider` 接口 + `PlaceholderKey` + `ModelInfo`。
- **`ServerGo/llm/sysprompt/`** —— leaf 包：Anthropic 三段式 system 提示词头（计费头 / 身份 / 核心规则）权威文本 + `EnsureHead`（§14.3）。
- **`ServerGo/llm/anthropic/`** —— 真实 provider 实现：`Authorization: Bearer <key>` + `anthropic-version: 2023-06-01`，5xx/429 重试。
- **`ServerGo/llm/registry.go`** —— `NewRegistry` / `Get` / `List`（key-free）/ `SetUserAgent` / `SetBillingHeader`。
- **`config.LLMConfig`** —— 顶级 `llm{}` 段：`endpoint/timeout_ms/max_retries/providers[]`。**真实 key 仅入 `LsmAgentGame.conf`；`LsmAgentGame.conf.example` 用 `API-KEY-PLACEHOLDER` 占位**。
- **预留 OpenAI 协议** —— `provider_type` 字段已留 `"openai"`。
- **API** —— `GET /api/llm/models`（需登录），返回 `[{agent_name, model, provider_type}]`，**不含 api_key**。
- **8 个默认模型**（运行时 `LsmAgentGame.conf`，seed 源迁到代码常量 `defaultProvidersForSeed()` 见 §20260812-LLM）：美团 LongCat-2.0 / 豆包 2.0 / DeepSeek V4-Pro / 智谱 GLM-5.2 / Kimi 2.7 / MiniMax M3 / Qwen 3.7-Plus-and-Max / Xiaomi mimo-v2.5-pro，对应模型 key `MeiTuan/DouBao/DeepSeek/GLM/Kimi/MinMax/Qwen/Xiaomi-model`，均为 `anthropic` provider_type。
- **运行时 provider 数量可超过 8 个**（§20260831-07 增补）：上述 8 个仅是 `t_lsm_game_llm_provider` 表为空时自动 seed 的默认值。运维可在后台通过 `/api/admin/llm/providers` 接入更多 provider（如 `Claude-model` / `Gemini-model` / `Tencent-model` / `ChatGPT-model` / `Kwai-model`），registry 按需加载；测试报告 R6 实测运行时 13 个 provider 全部 usable 即此场景。LLM 调用方（狼人杀 Agent / 辩论 Agent / 裁判）从不感知具体 provider 数量——`registry.Get(model_key)` 按需查找。

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
>   - `tool_use` 块**只允许** `{"type","id","name","input"}` —— 严格代理（DouBao）拒绝缺失这三个键的 tool_use，即便值为空对象/字符串，必须始终产出。
>   - `tool_result` 块**只允许** `{"type","tool_use_id","content","is_error"}` —— **禁止**携带 `id`/`name`/`input`。
>   - 修复：`llm/types/types.go` 的 `ContentBlock.MarshalJSON()` 按 `Type` 分支产出。
> - **messages 数组 user/assistant 严格交替**——禁止连续 2 条及以上 `role=user` 的消息。修复：`SanitizeMessagesForAnthropic` 末尾合并相邻 user 消息为一条（拼接 content blocks）。
> - `content`（user/assistant 的每条 message）必须是 **content-block 数组**，不允许纯 string。
> - `system` 必须是 **SystemBlock 数组**（`[{"type","text"}]`），不允许纯 string；数组最前面三段为标准头，见 §14.3。

- **顶层字段**：`model` / `system[]` / `messages[]` / `tools[]` / `metadata` / `output_config` / `max_tokens` + 可选 `tool_choice` / `stream` / `temperature` / `thinking`。
- **`metadata.user_id`**：stringified JSON，由 `buildMetadataUserID` 构造。
- **`stream`**：true 时 Provider 返回 SSE 字节流，由 `ParseSSE` 解析；Agent 已切到 `ChatStreamAccumulate`。
- **`thinking`**：Extended Thinking，按 `cfg.LLM.Providers[].thinking_required` 与自动愈合 fallback 决定何时注入。
- **出站请求头**：`Authorization` / `anthropic-version: 2023-06-01` / `User-Agent` / `x-anthropic-billing-header` / `Content-Type`。
- **预飞归一化**：(1) `tool_use.input == nil` → `{}`（§71a）；(2) `req.Thinking != nil` 时注入 `{type:"thinking"}` 块（§74a）。

### 14.3 Anthropic 三段式 system 提示词（全部 Agent 统一升级）

> 每个出站请求的 `system[]` 最前面固定三段，与 Claude Code 一致：
> **① 计费元数据头**（`x-anthropic-billing-header: cc_version / cc_entrypoint / cc_is_subagent`，
> 承载 SDK 版本 + 调用入口 + 子代理标识，仅供上游计费与追踪，不影响模型行为）→
> **② 基础身份声明**（`You are a Claude agent, built on Anthropic's Claude Agent SDK.`）→
> **③ 核心行为规则**（Claude Code CLI Agent 完整指令集：工具使用边界 / 任务执行准则 / 输出规范 / 权限约束）。
> 完整规范与权威文本见 [`lag_docs/LLM与Agent/AgentAnthropic系统提示词三段式规范.md`](lag_docs/LLM与Agent/AgentAnthropic系统提示词三段式规范.md)。

- **唯一事实来源**：`ServerGo/llm/sysprompt/`（leaf 包，只依赖 `llm/types`）。`texts.go` 持有三段权威文本；`Head(agentClass)` / `EnsureHead(body, agentClass)` 产出与合并。
- **单一注入点（禁止 Agent 侧拼接）**：`llm/anthropic`（`Chat` + `ChatStream`）与 `llm/openai/convert.go` 在**序列化前**调用 `sysprompt.EnsureHead` ⇒ 狼人杀玩家/法官/解说、辩论玩家/裁判/解说、德扑玩家、财商居民、记忆迭代器、压缩器**以及未来新增 Agent** 全部自动带上三段，零配置。
- **`cc_is_subagent` 判定**：`req.AgentClassName != ""` ⇒ `true` 并追加 `cc_agent_name=<AgentClassName>`（`ServerGo/agent/class_names.go`）；健康探针等无 AgentClassName 的调用 ⇒ `false`。
- **缓存策略**：②③ 段带 `cache_control: {type:"ephemeral"}`，① 段不带（其 `cc_agent_name` 逐 Agent 变化，作前缀会击穿命中）。Agent 自有块的 `cache_control` 语义不变。
- **字节预算**：三段约 2.5KB，属 Provider 侧注入的"隐形开销"，Agent 字节预算须计入（`sysprompt.HeadBytes()`，见 `agent/wwplayer/memory.go` 的 `approxSystemToolsBytes`）。
- **HTTP 头同源**：`x-anthropic-billing-header` 请求头与 ① 段共用 `sysprompt.BillingHeaderText`，避免两处口径漂移。
- **幂等**：`EnsureHead` 检测首位块前缀即跳过（调用方若已自带头不会重复注入）。

**改动纪律**：三段文本逐字节稳定（全体 Agent 的共享 prompt cache 前缀）；改动前须读上述规范文档并同步更新 `sysprompt` 测试锚点。

## 16. 聊天系统架构

聊天系统支持两种作用域：

| 作用域  | 说明         | 使用场景                             |
| ------- | ------------ | ------------------------------------ |
| `lobby` | 全局大厅聊天 | 首页、游戏列表、个人中心等非游戏页面 |
| `room`  | 房间内聊天   | `/xiangqi/:roomId` 等游戏对局页面    |

**关键组件**：
- **`ChatPanel`** (`components/chat/ChatPanel.tsx`) — 右侧栏全局聊天面板
- **`GameChatPanel`** (`components/chat/GameChatPanel.tsx`) — 房间内嵌聊天面板，6 款游戏共用（2026-08-07 §20260807-03 上移，狼人杀通过 `components/werewolf/GameChatPanel.tsx` 87 行薄适配器接入）
- **`useChat` hook** (`hooks/useChat.ts`) — WS 订阅 + 消息收发
- **`ChatService`** (`ServerGo/ws/chat_service.go`) — 后端聊天服务

**WebSocket 端点**：前端 `wsClient` 使用 `wss://HOST:39001/ws?token=<jwt>` 连接。

**聊天帧格式**：
```
客户端 → 服务端：chat.subscribe / chat.unsubscribe / chat.send / chat.history
服务端 → 客户端：chat.message / chat.history / chat.subscribed / chat.unsubscribed / chat.error
```

消息持久化到 `t_lsm_game_chat_message` 表，支持历史记录查询（默认 50 条，最大 200 条）。

## 17. 历史教训索引（§编号 → 文档路径）

> §编号是项目内部 **lesson 标记**，引用时用 `CLAUDE.md §<编号>` 即可。
> 本节仅保留 Agent 须**熟记**的核心教训；完整索引按主题归档到 `lag_docs/` 子目录（`lag_docs/狼人杀-Agent与系统/`、`lag_docs/狼人杀-重构方案/`、`lag_docs/狼人杀-角色设计/` 等）。

#### 必须熟记的 5 条核心教训

| §  | 教训 | 速记 | 详细文档 |
|---|---|---|---|
| **§92a** | **`sync.Mutex` 不可重入**：`Action_*` 必须建 `*Locked` 锁内变体；凡 `WerewolfRoom` 方法被 `BuildClientState*` 调用必查 | 凡改 `Action_*` 先 grep `BuildClientState` | [`lag_docs/狼人杀-Agent与系统/`](lag_docs/狼人杀-Agent与系统/) |
| **§130** | 「声明了却从不接线」：**新 helper / 字段 / 角色**必须有 grep 验证接线 | 写完新字段立即 `git grep "<新字段名>"` | [`lag_docs/狼人杀-重构方案/`](lag_docs/狼人杀-重构方案/) |
| **§134** | 守卫(Guard)等角色「全链路补全」：进卡池的角色必须完整实现，否则玩家持有无效身份 | 上卡池前先 grep 该角色全部用例 | [`lag_docs/狼人杀-角色设计/狼人杀守卫角色设计.md`](lag_docs/狼人杀-角色设计/狼人杀守卫角色设计.md) |
| **§135** | 身份公开公平性 —— 死者身份公开与否由房间级开关 `reveal_role_on_death` 决定（§20260830-01：默认**开启**=死亡即法官宣告身份；关闭=竞技规则死者牌不翻开）；服务端权威下发 `my_role` / `my_seat` | 涉及身份字段必查 `RolePubliclyRevealed`（第⑦分支门控死亡亮身份） | [`lag_docs/狼人杀-角色设计/狼人杀死亡语义设计.md`](lag_docs/狼人杀-角色设计/狼人杀死亡语义设计.md) + [`狼人杀死亡身份公开设计-20260830-01.md`](lag_docs/狼人杀-角色设计/狼人杀死亡身份公开设计-20260830-01.md) |
| **§197** | 流式续命 ——「接收到字节即刷新超时」 | 长上下文 LLM 调用必带流式 + 字节刷新 | [`lag_docs/狼人杀-Agent与系统/`](lag_docs/狼人杀-Agent与系统/) |

#### 完整索引按主题归档

| 主题 | 归档目录 |
|---|---|
| 狼人杀 Agent（死锁 / 接线 / 上下文 / 工具派发 / 重启投票 / 阶段 Watchdog 等） | [`lag_docs/狼人杀-Agent与系统/`](lag_docs/狼人杀-Agent与系统/) |
| 狼人杀 Agent 推理与战术博弈（猜疑链现状/多假说推演/暗号系统/§20260826-01 心理博弈增强） | [`lag_docs/狼人杀-Agent与系统/Agent升级/Agent推理与战术博弈/`](lag_docs/狼人杀-Agent与系统/Agent升级/Agent推理与战术博弈/) |
| 法官 Agent 重构 / Provider 注入 / 死亡语义 / §130 复发集中清算 | [`lag_docs/狼人杀-重构方案/`](lag_docs/狼人杀-重构方案/) |
| 角色卡池完整性（守卫 / 骑士 / 猎魔人 等） | [`lag_docs/狼人杀-角色设计/`](lag_docs/狼人杀-角色设计/) |
| 道具系统 + 模型玩家金币 + 注入攻击对齐 | [`lag_docs/狼人杀-道具与经济/`](lag_docs/狼人杀-道具与经济/) |
| 前端目录结构 + UI 对比度 + 游戏状态重构 | [`lag_docs/狼人杀-前端UI/`](lag_docs/狼人杀-前端UI/) + [`lag_docs/狼人杀-设计/`](lag_docs/狼人杀-设计/) |
| LLM Provider / Anthropic 协议 / 工具集 / 拟人化 | [`lag_docs/LLM与Agent/`](lag_docs/LLM与Agent/) |
| 第三方 Agent 平台借鉴（DeepSeek / Hermes / OpenCode / PI / jiuwenswarm） | [`lag_docs/其他Agent代码分析/`](lag_docs/其他Agent代码分析/) |
| WS 重连 / 鉴权 / 观战者架构 / 用户权限 | [`lag_docs/架构与协议/`](lag_docs/架构与协议/) |

> **早期高频速览（§43–§51、§80–§92、§94–§108、§111–§112 等 30+ 条细节）已迁出本文件**，按需查阅归档目录或在 git log 检索 `§<编号>`。

- 自动重连、Loading 遮罩、刷新/断线后恢复（会话+房间+对局），以及用户列表 `user.*` 帧的完整规则，
  记录在 [`lag_docs/架构与协议/WebSocket重连与恢复.md`](lag_docs/架构与协议/WebSocket重连与恢复.md)。
- 用户列表权限分级见 [`lag_docs/架构与协议/用户类型与权限.md`](lag_docs/架构与协议/用户类型与权限.md)。
- 前端 WS 连接生命周期由 `AppLayout` 唯一持有，页面切换不得 connect/close。

## 19.5 观战者 (Spectator) — 跨 5 款游戏

任何登录用户都可以进入任意活跃者房间以观察者身份实时观看，**不消耗座位，不影响玩家 UI**。
底层隔离由 `Hub.rooms` 与 `Hub.spectators` 两组互不相交的广播集合实现。详见
[`lag_docs/架构与协议/观战者架构.md`](lag_docs/架构与协议/观战者架构.md)。

**要点**：
- 玩家输入帧在观察者身上后端硬性拒绝 → `ErrSpectatorInputForbidden = 30011`。
- 路由：`/<game>/spectate/:roomId`；Hook：`useSpectatorMode()`；HTTP：`POST /api/rooms/:id/spectate` / `.../leave_spectate`。

## 21. Agent 自动化测试账号

> **2026-08-25 安全加固：CAPTCHA 旁路已全部删除。** 后端
> `AgentBypassAccounts` 白名单与前端 `AGENT_BYPASS_ACCOUNTS` 清单均已移除,
> **所有账号登录一律需要 `captcha_id` / `captcha_answer`**,无任何绕过路径。

AI Agent 在本地开发环境跑自动化登录、回归或 e2e 时,可使用
[`lag_docs/通用功能/测试账号凭证.md`](lag_docs/通用功能/测试账号凭证.md) 中预置的账号
(`test_01` ~ `test_04` 等;密码从仓库根目录 `test_account.json` 读取,
该文件已被 `.gitignore` 排除):

- 自动化脚本需先 `GET /api/captcha` 获取验证码 → 正常携带
  `captcha_id` / `captcha_answer` 登录(与真实用户路径完全一致)。
- 账号可能尚未落库:先 `GET /api/invites` 取公开邀请码,再 `POST /api/auth/register`。
- 仅限本地/开发环境使用,**严禁**在生产环境复用。
- `cfg.Server.DevMode=true` 仅有开发模式日志告警,**不再解锁任何认证旁路**。

## 22. 自动化测试与修复处理流程

> R46–R51 等多轮「报告→修复→验证」历史快照已迁出本文件，按需查阅 `lag_docs/` 下归档或 git log。
> 本节仅保留**流程规约**，不再记录每轮具体报告内容。
> **§20260915-01 重构**：原独立 Debug 流程 (`AutoDebugTestReport.md`) 已合并入各游戏测试提示词，
> 文件命名统一为 `AutoTestAndDebug_<Game>`；Agent 测试完成后在同一会话内直接执行修复 + 提交推送，
> 无需外部脚本接力。方案详见 [`lag_docs/通用功能/自动化测试与调试流程合并方案-20260915.md`](lag_docs/通用功能/自动化测试与调试流程合并方案-20260915.md)。

- **检索入口**：主工程 `TestReport/<游戏>自动化测试报告_*.md`（glob 来源: `auto_run_common.sh::GAME_GLOBS`）；子工程 `go-web-debug-tool/UseReport/<游戏>测试工具使用报告_*.md`。
- **处理入口**：各游戏独立入口脚本 `AutoTestAndDebug_{Werewolf,TexasPoker,Debate,Wealth}.sh` —— **首选 Claude Code CLI 执行**（claude 不可用降级随机选；公共库 `agent_cli_common.sh`，`AGENT_CLI=<name>` 可强制指定），加载同名的 `AutoTestAndDebug_*.md` 作为 prompt；`AutoScreenshot_{Werewolf,TexasPoker}.sh` 等同机制（全部可用 Agent 随机选择）。
- **流程规范与硬约束**详见各 `AutoTestAndDebug_*.md` 的「自动修复流程」章节(§12)；其中**绝对禁止**自动修复流程写入 `CLAUDE.md` / `AGENTS.md` 这两个规则文件。
- **报告清理**：修复完成后必须删除已处理的 `TestReport/*.md`（子工程 `UseReport/*.md`），报告不应在仓库中长期堆积；无问题的报告追加 `_无问题` 后缀归档。
- **提示词内嵌修复**：各游戏提示词已包含完整「测试 → 自动修复 → 提交推送」流程，Agent 无需依赖外部 debug 脚本接力。

## 24. AgentClassName 与 User-Agent 拼装约定

> 2026-08-06 §Agent 重构增强。**所有 Agent 都必须设置** `AgentClassName`，统一登记在 [`ServerGo/agent/class_names.go`](ServerGo/agent/class_names.go)。

**当前已注册**：

| AgentClassName | 调用 LLM 的场景 |
|---|---|
| `LsmAgentGame-Werewolf-Player` | 玩家 Bot 主对话 + `speak_floor_tick` 自救路径 |
| `LsmAgentGame-Werewolf-Judge` | 法官宣告 / `prompt_actor` / summary / `declare_cause` |
| `LsmAgentGame-Werewolf-MemoryIter` | 整局总结后异步自我迭代 `MEMORY.md` |

**命名规则**：`<Game>-<Role>` —— 玩家 `Player` / 法官 `Judge` / 工具型 `MemoryIter`。

**User-Agent 出站**：`User-Agent: <AgentClassName>/<AppVersion> <buildDateTime>`。`AppVersion` + `buildDateTime` 来自 `main.go` ldflags 注入（`rebuild_restart_app.sh` 强制覆盖），拼装的"程序名前缀"由 `LLMRequest.AgentClassName` 在出站前覆盖（`llm/anthropic/anthropic.go::userAgentFor`）。

**接入新 Agent 的 2 步**：(1) `class_names.go` 追加 `AgentClass<Game><Role>` 常量 + 接入 `AllAgentClassNames()`；(2) Agent 实现的 `llm.LLMRequest{...}` 构造点填 `AgentClassName`，并加单测断言 `!= ""`（防「声明了却从不接线」）。

**设计动机**：同一 LLM Provider 被多类 Agent 复用，上游/网关需通过 UA 区分调用方做计费/限流/审计；`AgentClassName`（业务身份） 与 `ModelKey`（推理引擎）正交。

## 26. 前端 UI 颜色对比度与可读性规范

> **2026-08-08 §20260808-02 用户反馈响应**。通用规约全文见 [`lag_docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md`](lag_docs/狼人杀-前端UI/前端UI颜色对比度与可读性规范.md)。

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

### 26.3 经济档位 / 风险档位三件套规约

任何后端下发枚举值拼接为 className 的样式,**必须**同提交完成:JSX 拼接 + CSS 类规则 + `@keyframes + prefers-reduced-motion` 兜底。写完 JSX 后**立即** `grep -rn "<新class前缀>" ClientWeb/src/styles/`,零命中即 P1 缺陷(2026-08-08 §20260808-02 已踩坑:`econ-tier-${econTier}` 三档零 CSS 规则)。

> 8 行状态徽章色相库 + 5 项验收 checklist + 实战 diff，详见规范文档 §2.4/§6 + [审计报告 20260808-02](lag_docs/狼人杀-前端UI/狼人杀13人局-前端UI颜色对比度审计报告-20260808-02.md)。

## 27. Blender 真实 3D 模型工作流（v19 引入）

> 2026-09-23 §20260923-01：把虚拟城市 12 个高视觉冲击组件（5 建筑 + 4 车 + 1 树 + 1 行人 + 1 路面）
> 从"程序化几何 + PNG sprite"升级为 Blender headless 导出的真实 `.glb` 模型。
> 完整规约见 [`lag_docs/虚拟城市/已实现/19-Blender3D模型集成/02-架构设计.md`](lag_docs/虚拟城市/已实现/19-Blender3D模型集成/02-架构设计.md)。

### 27.1 三层架构（数据流）

```
.blend (不入库,本地美术)
    ↓ 导出脚本（headless）
3d_script/build_<X>.py (入 git,Python)
    blender --background --python build_<X>.py -- ClientWeb/src/assets/models/<cat>/<name>.glb
    ↓ 输出
ClientWeb/src/assets/models/{civic|vehicles|nature|characters|road}/<name>.glb (入 git,二进制)
    ↓ import.meta.glob ?url, eager:true
ClientWeb/src/assets/models/index.ts → modelUrl(category, name)
ClientWeb/src/components/wealth/modelCache.ts → useSharedGLTF(url)
ClientWeb/src/components/wealth/Model.tsx → <primitive object={scene.clone(true)}>
    ↓ vite build (contenthash)
ClientWeb/dist/assets/<name>-<hash8>.glb
    ↓ rsync (rebuild_restart_app.sh)
ServerGo/static/assets/<name>-<hash8>.glb (Go embed.FS)
    ↓ GET /static/...
浏览器: GLTFLoader.parse → cache.scene → per-instance .clone(true) → <primitive/>
```

### 27.2 12 个模型清单（v19 首期交付）

| 类别 | 模型名 | 导出脚本 | 单文件体积 |
|------|--------|---------|-----------|
| civic | city_hall / comm_tower / water_tower / fire_station / police_station | `3d_script/build_<name>.py` | 15-64 KB |
| vehicles | sedan / truck / bus / taxi | `3d_script/build_vehicle_<name>.py` | 27-48 KB |
| nature | oak_tree | `3d_script/build_oak_tree.py` | 32 KB |
| characters | pedestrian_walk (含 walk 动画 clip) | `3d_script/build_pedestrian.py` | 38 KB |
| road | road_props (StreetLight_A/B/C 合并) | `3d_script/build_road_props.py` | 42 KB |

### 27.3 三条硬约束

1. **`.blend` 源文件不入库** —— `.gitignore` 已加 `*.blend` / `*.blend1`；本地美术保留 `.blend`，git 只入 `.py` + `.glb`
2. **单 .glb ≤ 500 KB** —— 验收硬约束（CI 跑 `find ClientWeb/src/assets/models -name "*.glb" -size +500k`）
3. **保留原程序化几何为 fallback** —— 每个接入组件必须包一层 `<Model url={...}>{原程序化几何}</Model>`，
   `.glb` 缺失/加载失败自动降级，零代码分支

### 27.4 命令模板（art-designer 工作流）

```bash
# 在 3d_script/ 目录下执行
cd 3d_script
blender --background --python build_city_hall.py -- \
  /usr/local/LsmAgentGame/LsmAgentGame/ClientWeb/src/assets/models/civic/city_hall.glb

# 或者用 find 批量重新导出（前提：依赖本地 .blend 美术资产）
for s in build_*.py; do
  name="${s%.py}"
  blender --background --python "$s" -- "/tmp/${name}.glb"
done
```

### 27.5 缓存与降级契约

- **缓存 key = url**（与 `textureCache.ts` 不同：GLB 不像贴图有 wrap/repeat 参数）
- **每实例 `.clone(true)`** —— 56 个行人 / N 车 / N 建筑 共享同 URL 但 transform 独立
- **降级链**：`url===''` / `onError` / `加载中` 三态都直接渲染 children fallback
- **feature flag**：`localStorage.getItem('disable-blender-models') === '1'` 强制全 fallback（测试/回滚用）

### 27.6 Blender 版本与依赖

- 本仓库用 **Blender 5.2.1 LTS**（已安装 `/usr/local/bin/blender`，version 5.2.1）
- **DRACO / Meshopt 不启用**（v19 简化，单 .glb 控制在 15-64 KB 已远低于 500 KB 上限；
  生产 nginx gzip 压缩比 3-8x 等效 4-20 KB/文件）
- v19.5 候选：DRACO 压缩（目标 .glb 减 60-80%）

### 27.7 §13.1 职责线扩展

`art-designer`（职责线 4）新增工作面：`3d_script/build_*.py` + `ClientWeb/src/assets/models/**/*.glb`。
原 `python-generate-image-tool/`（Pillow 2D 贴图）继续保留，与 Blender 3D 流水线并列。

| 关键词 | 触发子代理 | 工作面 |
|--------|-----------|--------|
| Blender / .glb / 3D 模型 / 几何 | art-designer | 3d_script/build_*.py + ClientWeb/src/assets/models/ |
| GLTFLoader / useSharedGLTF / modelUrl | frontend-dev | ClientWeb/src/components/wealth/{modelCache,Model}.tsx + 12 个接入组件 |
