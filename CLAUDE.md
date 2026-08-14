# LsmAgentGame — AI 代理项目规则

> 规范规则文件。`KILO.md` 和 `AGENT.md` 是指向此文件的符号链接。
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
- **Markdown** 规则文件（CLAUDE.md、KILO.md、AGENT.md）不超过 800 行。如文件过长，请按主题拆分到 `docs/` 目录。

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

**典型写法**：
```ts
} catch (e: any) {
  if (!isSessionExpiredError(e)) {
    setErr(e.message);                                  // 本地展示
    reportGlobalError({ message: e.message, severity: 'error' }); // 全局兜底
  }
}
```
弹窗内联写法（以 `ModelAdminPage` 编辑弹窗为例）：弹窗自己维护 `formError` state；store 的 CRUD 失败写 `lastError`，页面 effect 把 `lastError` 同步到 `formError`；同时 store 仍 `reportGlobalError` 兜底。

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
- **合并(若历史遗留了特性分支)**:`git merge --no-ff <feature-branch> -m "合并: <说明>"`,
  git 自动处理 `docs/` 中文文件名重命名;冲突极少(架构主线一致)。
- **不创建新分支**:`git checkout -b <name>` **禁止**;`git switch -c` 同样禁止。
- **运行基准**:服务(`./rebuild_restart_app.sh`)和单测(`go test ./...`)始终跑 `main` HEAD。
- **HEAD vs 落后**:`git log origin/main..HEAD` 应为空或仅含本地未推送;若有差异,
  优先确认是否需要 `git push` 与团队同步。

**适用场景**:
- ✅ 新功能(如 §122 多线程 LLM 调用):直接在 main 上 add → commit → push。
- ✅ Bug 修复、§13 教训条目:直接在 main 上加 lessons / 改代码。
- ✅ `docs/*.md` 重构或新增文档:直接在 main 上 commit。
- ⚠️ **实验性 / 长期项目**(如大型架构迁移):若必须开分支,完成后立即 `--no-ff` 合并回 main。
- ❌ **永不开分支**:hotfix / 单一 commit 重构 / 规则文件微调 / docs 翻译同步。

### 10.2 AI Agent Tools 规则文件同步

> **本规则文件 (`CLAUDE.md`) 同时被多个 AI Agent 工具加载**,作为项目的"系统词"
> 单一事实来源。任何规则更新必须**同步**到所有下游入口,避免工具间出现行为漂移。

**加载入口清单**:

| Agent 工具 | 入口文件 | 加载方式 |
|------|------|------|
| **Claude Code** (`Claude`) | `CLAUDE.md` (项目根) | 自动读取根目录 `CLAUDE.md` |
| **Kilo Code** (`Kilo`) | `KILO.md` | 项目根 `KILO.md` **必须**是 `CLAUDE.md` 的符号链接 |
| **OpenCode** (`OpenCode`) | `AGENT.md` | 项目根 `AGENT.md` **必须**是 `CLAUDE.md` 的符号链接 |
| **pi / OpenClaw** 等其它 Agent | `CLAUDE.md` / `AGENT.md` | 同上,以符号链接透明兼容 |

**同步规约**:
- **符号链接优于复制**:`KILO.md` 与 `AGENT.md` **必须**是 `ln -s CLAUDE.md KILO.md`
  与 `ln -s CLAUDE.md AGENT.md` 创建的符号链接 — 修改一次,所有工具同步生效。
- **不要**把同一份内容复制到 3 个文件;任何一边不同步就是 bug。
- **若发现 `KILO.md` / `AGENT.md` 是普通文件而非符号链接**:立刻
  `rm KILO.md AGENT.md && ln -s CLAUDE.md KILO.md && ln -s CLAUDE.md AGENT.md` 修正。
- **新增章节时**:只需编辑 `CLAUDE.md`;工具自动加载更新版本,无需通知。

**CI 自检(可选)**:可在 GitHub Actions 加一步 `test -L KILO.md && test -L AGENT.md`
防止误把符号链接替换为普通文件。

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

> 完整角色定义见 [`docs/通用功能/子代理角色.md`](docs/通用功能/子代理角色.md)。

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
- **8 个默认模型**（运行时 `LsmAgentGame.conf`）：

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
> **权威数据用例**（项目根目录，Claude Code 实际下发的 wire 格式，AI Agent Tools 必须
> 严格对照这些文件的形状与字段集）：
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
>   - 修复：`llm/types/types.go` 的 `ContentBlock.MarshalJSON()` 按 `Type` 分支产出，text/tool_result 不再泄漏
>     `id`/`name/input` 字段。量化证据：修复前 Doubao 请求体的 18 个块 **100%** 携带非法字段污染，修复后为 0。
> - **messages 数组 user/assistant 严格交替**——禁止连续 2 条及以上 `role=user` 的消息（典型违规场景：
>   `recordToolResult` 推入 `tool_result`(user) 后下一轮 `handleEvent` 又推入 `game_state`(user)，或压缩摘要落地后
>   ［identity(user), summary(user)］头两条连续）。修复：`SanitizeMessagesForAnthropic` 末尾合并相邻 user 消息为一条
>   （拼接 content blocks）。
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
> 包含完整游戏规则、角色定义、Agent 实现现状、道具系统、前后端架构、关键设计决策、Bug 教训等一站式参考。
> 每个 bot 座位一个 goroutine + LLM + 工具定义。详见 [`docs/狼人杀-Agent与系统/狼人杀Agent设计.md`](docs/狼人杀-Agent与系统/狼人杀Agent设计.md)。
> **角色实现状态**(§134/§198/§猎魔人):`godRolePool` 含 6 个全链路可玩神职：女巫/猎人/白痴/**守卫**/**骑士**/**猎魔人**;
> 魔术师/奇迹商人/射梦人/乌鸦/稻草人/定序王子/纯白之女 已退役(仅保留 wire 兼容)。
> 守卫规则与实现见 [`docs/狼人杀-角色设计/狼人杀守卫角色设计.md`](docs/狼人杀-角色设计/狼人杀守卫角色设计.md)。
> **硬约束**:进卡池的角色要么完整实现,要么移出卡池 —— 「半实现」= 玩家持有无效身份。

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
  **6 款游戏共用同一实现**（2026-08-07 §20260807-03：原位于 `components/xiangqi/` 属跨游戏
  引用，已上移；`components/chess/GameChatPanel.tsx` 陈旧副本已删除）。
  狼人杀通过 `components/werewolf/GameChatPanel.tsx`（87 行薄适配器，映射
  `gameState.players → roomPlayers`）接入 —— 这是「共享基座 + 游戏私有 props」的推荐范式
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

## 17. 历史 Bug / 测试报告索引

> §19–§60 / §80–§91 段的 Bug 修复记录、Round 审计报告已迁出到 `docs/` 目录以维持 800 行上限。
> 查阅「现象 / 修复 / 教训」完整记录请先阅读对应归档文档。

| 归档内容 | 文档路径 |
|---------|---------|
| §94–§107 Round 39–45 狼人杀 Agent + go-web-debug-tool（BootCleanup 同步 / Agent 死亡守卫 / Quarantine UI / 身份硬约束 / input_text 默认 / stream-drop / ws-queue / hunter_shoot-deadlock / 回归测试补全） | 本文件 lessons 表 §94–§107（本节） |
| **§20260807-04 Agent 道具对齐 6 类 LLM 注入攻击**（`isExposeProp` 漏判 / AOE EffectTypes 丢失 / 人类反制道具 / 角色差异化 / 过期递减 bug / PropHitLastRound 反馈闭环） | [`docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md`](docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md) |

下面仍然保留常用且需要被 Agent 高频查阅的 7 条教训摘要：

### 43. 聊天消息在游戏页重复显示
- 每条 `chat.message` 被全局 `<ChatPanel>` 与 `<GameChatPanel>` 各自处理一次。
- 教训：一份 `scope+roomId` 对应一份 hook 实例。

### 44. 德州扑克进入房间崩溃 `null.length`
- Go JSON 序列化 nil slice 输出 `null`；`BuildClientState` 未赋值的 slice 走 `null`。
- 教训：任意 slice 字段前端都可能拿到 null，必须双保险。

### 47. 国际象棋坐标 + WS 重连状态覆盖
- **47a**：棋盘翻转只应影响 y 轴（rank），x 轴（file）永远不变。
- **47b**：`game.join` 是幂等的，重连不应广播 `game.started`，否则覆盖对手实时状态。

### 48. 玩家 reload 后 game.started 硬编码 move_count:0
- 前端 `game.started` 处理必须是「服务端字段全量透传 + 服务端未下发才取默认值」。

### 49. 狼人杀等待阶段无聊天/退出
- 多游戏等待阶段应统一渲染 sidebar，与 `gameState.ready` 解耦。

### 50. 象棋/国际象棋 reload 后 game.state 缺失
- 所有玩家页都应有周期 `requestState`（8s），弥补 React 渲染时序吞掉首帧或 tab 节流漏接 push。

### 51. 狼人杀黎明阶段无 UI 触发 start_day
- 任何「前端暂时跳过 → 服务端推进」的阶段，都必须前端给出 UI/计时器主动推一下。

### 80–92. 狼人杀 Agent 核心教训速览

| # | 核心教训 | 关键词 |
|---|---------|--------|
| 80 | REST 房间视图必须从 in-memory 引擎拉权威状态，不能只镜像 DB 行 | phase/round_number |
| 81a | 「延迟唤醒」goroutine 必须持有可取消 context | quarantine |
| 81b | sleep/retry 路径分发前必须重新 fetch live phase | auto-skip |
| 81c | Tee 日志 + stdout 重定向到同一文件 → 双写 | logger |
| 82a | 暴露给 LLM 的"对外编号"必须与 UI 完全一致 | 0-indexed/1-indexed |
| 82b | Anthropic 协议是 LLM-as-judge 最大长尾；Prune() 不能切断消息配对 | tool_use/tool_result |
| 82c | 被引擎长持的锁，REST 入口必须用 bounded-latency snapshot + cache fallback | lockRoomBriefly |
| 83a | 「成功但无动作」必须被视为与「错误」同等需要恢复 | auto-skip |
| 84a | 「只有行动者才应触发」的逻辑必须检查行动者标识 | ShouldAutoSkip |
| 84b | 结构化请求必须 `DisallowUnknownFields` | API 严格校验 |
| 85 | 任何推进 phase 的 Action_* 都应当自带「唤醒下一行动者」 | FinishSpeak |
| 86 | acting bot 判定必须用权威性字段 gc.MyTurn；四条 wake 路径必须共享同一套判定逻辑 | quarantine-skip |
| 87 | 处理报告前必须 git log/grep 验证缺陷是否仍存在 | 版本基线 |
| 88 | 计数器必须分离；quarantine 需要主动通知 manager | semaphore |
| 89 | 同一语义的 skip 动作在 manager/agent 路径必须有完全一致的引擎调用 | vote_skip |
| 90a | 底层网络库接管连接后，gin wrapper 不要再用于子写入 | WSS 39002 |
| 90b | broadcast 路径推送前必须检查房间是否持有真正的 in-memory 游戏 | SpectatorView |
| 90c | 幂等 cleanup 路径中的"已不存在"应 Debug | 日志降噪 |
| 91 | CDP `add_script` ≠ "全局/永久 hook"；SPA 内 hook 必须每次导航后重装 | add_script |
| 92a | **锁内路径调用的 Action_* 必须全部建 `*Locked` 锁内变体**（sync.Mutex 不可重入，漏一个即自死锁）；锁内函数的测试必须持锁 + 超时守卫 | r.mu self-deadlock |
| 92b | 新增 `*_skip` 动作必须 manager/agent 双路径对照引擎参数（动作名/target/弃权哨兵） | witch_act_skip |
| 93 | **每个活跃狼人杀房间必须有 Phase Watchdog 后台 goroutine**（每 5s 轮询，90s 同 phase+actingSeat 则强制 skip，60s 心跳日志）；`stopAgentsLocked` 首行必须 `r.watchdogCancel()` | phase stall safety-net |
| 94 | **R39-45 测试教训(一)**: 进程重启时 BootCleanupStaleWerewolfRooms 必须在 HTTPS/WSS listener 之前**同步**执行; Agent 死亡检查必须基于 live `rp()` 的 `alive[]` 而非 event snapshot; quarantine 后 `BotTranscript` 必须主动刷新,否则前端面板空白; LLM system prompt 必须显式约束"你不是主持人"+"死亡后停止发言" | boot-restart-sync / dead-state-rp / quarantine-ui / agent-identity-guard |
| 95 | **R39-45 测试教训(二) — CDP 自动化**: `input_text` 默认 `use_js=true`(直接走 React native setter); `eval_js` 长 payload 用 `expression_b64/script_b64` 绕开 JSON 转义; `input_text` 加 `findDeep` 递归穿透 ShadowRoot + `wait_ms` 轮询等待 selector;LLM 输出整段复读由 `dedupSpeakText` 清理 | input-text-default / eval-js-b64 / input-text-deep-wait / speak-dedup |
| 96 | **R39-45 测试教训(三) — 网络与 UI**: 上游 Anthropic 代理主动断连(`context canceled` / `use of closed network connection`)必须 classifier 标记 `Retryable=true` 进重试,否则 4/7 Agent 被无谓 quarantine; `wsClient.send()` 非 OPEN 时入 `pendingSend` 队列 + onopen flush,避免 spectator UI 永久 spinner | stream-drop-classify / ws-send-queue |
| 97 | **R39-45 测试教训(四) — 阶段死锁与 watchdog**: single-actor 阶段(仅特定角色才可行动)的 MyTurn / watchdog 必须正确识别该角色,即使已死亡(死后能力触发); 每个新阶段接入必须同步更新 `SkipPhaseAction` / `dispatchQuarantinedSkipLocked` / `watchdogActingSeat` 三处; **per-bot 自救 与 manager/watchdog 救援 必须走不同派发路径**; `watchdogActingSeat` 返回 -1 的所有 phase 必须显式列出 fallback; 处理报告前必须先 `git log/grep` 验证缺陷是否仍存在(§87/§107) | hunter-shoot-deadlock / vote-watchdog-rescue |
| 108 | **R48 修复**: (a) quarantine-skip 递归深度溢出 → `skippingSeats map[int]bool` + `lastSkipPhase` 同 seat 同 phase 仅派发一次(**重入保护优于 depth limit 兜底**); (b) `status=playing + phase=over` 矛盾 → `GameState` 内存状态与 DB 持久状态必须双写,每个 `SetOnGameStarted` 都应有对应 `SetOnGameOver`; (c) retryable 瞬断密集重试 → quarantine → `consecutiveFailures` 加 30s 冷却窗口(永久错误 403/401 绕过冷却快速 quarantine) | quarantine-skip-reentry / game-over-db-sync / retry-cooldown-window |
| 111 | **§13 增强 — 500K 聊天历史队列(房间共享 + ReadPointer)**: 单源 `ChatHistoryQueue` + 全局递增 `Seq` + per-consumer `ReadPointers map[int]uint64`(`WindowFor(seat)` / `Advance(seat, seq)` / `SnapshotLastSeq`);覆盖公开 chat + whisper + 房间活动事件;4 级压缩(相邻合并 → 单条 > 1KB 截断 → 超 capBytes 淘汰 → fallback 摘要)。**教训**: per-seat fan-out append 在 7 bot 房间导致 ~Nx 内存,改为单源+序号+read pointer 是 7-AI 公平性根本保证;ReadPointer 必须 `snapshot.lastSeq` 否则 WindowFor 丢消息 | chat-history-queue / shared-queue-readpointer |
| 112 | **§13 增强 — 发言下限 + 阶段时钟 + 观众唤醒**: (a) **speak_floor watchdog** 每 bot 60s 窗口 < 2 次触发 `speak_floor_tick`(跳过 LLMCallLimiter/SpeakLimiter,失败**不**计入 consecutiveFailures); (b) **阶段时钟** `PhaseDeadlineAt` + `SetPhaseDeadline(phase, secs)`,deadline 到期 watchdog **立即派发 skip**(优先于 90s 兜底); (c) **观众全频唤醒** 移除 15s 节流(默认 `cfgWerewolfSpectatorFullWake=true`),每条观众消息触发全部 bot wake,成本由 `roomLLMConcurrency=4` 信号量兜底。**教训**: per-bot 自救 与 watchdog 救援 必须走不同事件路径;speak_floor 失败若计入 consecutiveFailures 会导致误 quarantine | speak-floor-watchdog / phase-clock-deadline / spectator-full-wake |
| 117 | **重开局投票流程**: 新增 `PhaseRestartVote` 阶段(5 分钟窗口,≥ 2/3 同意即原地复用 7 座位 + 保留 chatQueue/ReadPointers 重开一局);`restartGameLocked` 调 `NewGame+StartGame` 但保留 `r.Seats + r.chatQueue`;watchdog 在 `Status=="over"` 后 5s tick 自动切入。**教训**: 对局结束 ≠ 房间结束;原地复用 Seats + 保留 chatQueue 是"连续游戏"的关键;**5 分钟窗口 + quorum 评估应在 manager 单一路径**(类似 §92a 锁内变体约束)。详见 [`docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md`](docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md) | werewolf-restart-vote |
| 118 | **模型管理 + 模型玩家持久化 + 模型金币**: LLM 模型升级为「数据库持久玩家」 — 5 张新表 + `t_lsm_game_user` 加 `IsBot`/`BotProviderID`/`LinkedProviderAccount`;**AES-256-GCM** 加密 API Key(主密钥 base64 存 `t_lsm_game_kv`);**DB-first 加载**(`llm.NewRegistryWithDB`,空 DB 时 seed from cfg + 自动注册 bot user/wallet);`RecordLogService` 异步 worker(失败仅 log 不阻塞游戏流);`/api/admin/llm/*` 11 个端点;狼人杀结算 ±100 金币。详见 [`docs/狼人杀-道具与经济/模型管理与持久化玩家设计.md`](docs/狼人杀-道具与经济/模型管理与持久化玩家设计.md) + [`docs/狼人杀-道具与经济/模型玩家金币设计.md`](docs/狼人杀-道具与经济/模型玩家金币设计.md)。**教训**: DB-first 加载必须「DB 行 → seed from cfg」二选一;加密 API Key 必须显式主密钥;异步持久化不阻塞游戏流是硬约束;软删除必须保留 bot_user/wallet(历史审计链路);HTTP API 与前端必须共用 TS 类型契约 | model-admin-system |
| 119 | **Agent「心口不一」+「说谎/讲故事」能力**: `speak_with_thought` 工具在同一调用里填 public text(广播给所有玩家) + internal_thought(**仅**写入 `BotTranscript.HeartThought`,**绝不**进 chat_message 表 / chat_history 队列 — 协议层物理隔离);`ToolRunner.SpeakWithThought` 共享 speakLimiter + recentSpeakDedup + ScrubIdentityLeak;`BuildSystemPrompt` 注入「聊天对话 = 思考本身」方法论 + 8 类欺骗剧本(狼人悍跳/平民装神职/预言家避嫌/女巫用药)。**教训**: 狼人杀核心技能是「讲故事」;internal_thought 必须协议层隔离(非仅 UI 层隐藏);「心口不一」对好人阵营同样适用;speak_with_thought 限流必须与 speak 共享同一令牌桶 | heart-thought-deception |
| 213 | **emotion_switch → emotion_switch_speak 合并重构(2026-08-04)**: 原 emotion_switch 独立工具删除(LLM 抓包显示单响应可产生 10 次 emotion_switch tool_use,reason 全为空话),合并为 `emotion_switch_speak(text=必填, emotion=可省略, reason=可省略)`:text 强制绑定发言(等价强制走 Speak 限流/去重/广播链),emotion 仅在 Speak 真正成功后才切(speak 失败回滚 emotion);单响应最多 1 次 emotion_switch_speak;与 speak/speak_with_thought 不能同响应。**后端 Bug 顺手补**:`BotTranscript` 加 `EmotionReason` 字段填补契约 gap;`recordTranscript` 保留前次 HeartThought(避免同轮 `speak_with_thought` 内心独白被覆盖)。**前端**:emotion metadata 抽到 `ClientWeb/src/utils/werewolfEmotion.ts` 共享;WerewolfTable badge 加 `emotion_reason` tooltip;FactionDrawer 用 emoji+label+reason 渲染;HistoryDrawer 加 emotion history `<details>`(spectator only);i18n 三语种补 10 个 `werewolf.emotion.*` 键。详见 [`docs/LLM与Agent/Agent工具定义-解决和设计方案-20260804-01.md`](docs/LLM与Agent/Agent工具定义-解决和设计方案-20260804-01.md) | emotion-switch-speak |
| 120 | **Agent API 调用时间差异公平性**: `Agent` 累计耗时统计(`avgLLMLatencyMs` 指数加权 α=0.3 / `lastLLMLatencyMs` / `totalLLMCalls`);`GameContext` 注入 `ModelName` / `MyAvgLLMLatencyMs` / `RoomFastestModel` / `RoomSlowestModel` / `IsHumanInRoom`;`BuildUserPrompt` 按 avg 三档(≥4s 慢/≤2s 快/2-4s 中)给出工具策略 + 按 `IsHumanInRoom` 切"与人类交互"或"全 AI 房间"文案。**教训**: 公平性核心是根据响应速率调整工具策略(慢模型单 tool,快模型多 tool 合并);真人 vs 全 AI 房间实时性策略必须分开;耗时统计用指数加权避免冷启动偏差 | api-latency-fairness |
| 121 | **模型管理页面渲染崩溃(后端 data 形状与前端类型不匹配)**: 后端 `ListProviders` 返回 `{providers: [...], total, source}`(包装对象),但前端 `http<LlmProvider[]>` 直接解成数组 → `providers.map` 报 `TypeError`。同样问题在 `listProviderGames`/`getGameLog`/`deleteProvider`/`reloadProviders`。修复: 拆出 `ListProvidersResponse`/`ProviderGamesResponse` wrapper 类型并提取目标字段。**教训**: `http<T>` 把后端 `data` 直接展开给前端类型,若 `data` 是 wrapper 对象则前端必须显式声明 wrapper 类型;admin CRUD 上线时前端必须用真实后端响应(curl)核对,不能照文档"按惯例"写 | model-admin-response-shape |
| 122 | **Agent 单 bot 内多线程 LLM 调用(只读并行 worker)**: `ParallelThink(ctx, queries, maxWait)` 启动 N 个 worker(默认 ≤ 2),走房间级 `llmSema` + 独立 `LLMRequest`(不携带 Memory.messages);`ParallelThoughts()` 取出 + `appendToLastUserMessage` 追加到最后一条 user message 末尾;**默认 `EnableParallelThink=false`**(§128 后整套机制保留但默认关闭)。**不变式**: `agentRunner` / `Memory.Prune/Push` / tool dispatch 串行路径不动;总并发 = Σ bots × perBotParallel ≤ `roomLLMConcurrency`(默认 8);worker 失败**不**计入 consecutiveFailures;测试环境需 `defer recover` 兜住 config.Load() panic | parallel-think-122 |
| 123 | **Agent 法官 + 死亡语义区分**: (a) **Agent 法官**(LLM 驱动的非玩家主持人,5 个工具:`announce`/`prompt_actor`/`summary`/`declare_cause`/`idle_silent`,`JudgeLimiter` 30s,`MaxToolUse=2`,默认 `cfgWerewolf.JudgeMode="ai"` 启动);(b) **死亡语义二分** `verdict`(**execution = 处决 / death = 死亡**),`killPlayer` 内部 `verdictFor(cause)` 查表自动填(`wolf/hunter/witch_poison` → death,`vote/suicide` → execution,**狼自爆 = 处决**),术语全栈统一(`normalizeDeathTerms` 清洗 + Anthropic 协议注入 `death_semantic_version="2026-07-10-v1"`)。详见 [`docs/狼人杀-重构方案/主持人Agent重构设计.md`](docs/狼人杀-重构方案/主持人Agent重构设计.md) + [`docs/狼人杀-角色设计/狼人杀死亡语义设计.md`](docs/狼人杀-角色设计/狼人杀死亡语义设计.md)。**教训**: 法官 ≠ 身份牌;法官不能影响 phase 状态(驱动分层:watchdog→host driver→judge);`cause → verdict` 必须查表派散 | judge-execution-death |

- 自动重连、Loading 遮罩、刷新/断线后恢复（会话+房间+对局），以及用户列表 `user.*` 帧的完整规则，
  记录在 [`docs/架构与协议/WebSocket重连与恢复.md`](docs/架构与协议/WebSocket重连与恢复.md)。
- 用户列表权限分级见 [`docs/架构与协议/用户类型与权限.md`](docs/架构与协议/用户类型与权限.md)。
- 前端 WS 连接生命周期由 `AppLayout` 唯一持有，页面切换不得 connect/close。

## 19. 斗地主 (Doudizhu) 架构

斗地主是平台首个**卡牌类 / 3 人**游戏（1 地主 + 2 农民）。

### 与棋类的关键差异

| 124 | **Day/Night 视觉特效(CSS-only)**: 3 类覆盖层(`DayNightOverlay.tsx` + `pointer-events:none` + `setTimeout` 自动消失) — 🌙 天黑请闭眼(2.5s 蓝黑径向渐变)/ 🌅 天亮了(1.8s 暖橙)/ ☀️ 白天开始(1.2s 浅蓝)。**教训**: CSS 特效必须 `pointer-events:none` + 自动消失,否则拦截点击;持续时间按信息密度分级(night > dawn > day) | day-night-overlay |
| 125 | **法官「整局总结」+ 模型记忆持久化**: LLM 输出 5 段格式(`【阵营胜负】`/`【关键翻盘点】`/`【角色操作时间线】`/`【MVP 玩家】`/`【狼人悍跳记录】`)由 `ParseSummary` 切分;持久化双路径 — `chatQueue.Append(IsActivity:true)` + `modelMemories[modelKey]`(per-ModelKey 隔离,`summaryLimiter` 60s 与 `announceLimiter` 30s 解耦)。详见 [`docs/狼人杀-重构方案/主持人Agent重构设计.md`](docs/狼人杀-重构方案/主持人Agent重构设计.md)。**教训**: 总结失败必须 FallbackSummary 兜底;触发时机在 `Status="over"` 末尾(非 `gameOverNotified=true` 后) | judge-game-summary |
| 126 | **Agent「思考中…」多态指示器(R128 已重构删除)**: §126 原重构通过 `BotTranscript.LLMCallPhase` 5 态(`Idle/Calling/Streaming/Retrying/Quarantined`) + `BotPhaseIndicator` 替代二态"思考中"。**R128 后已删除**(§128 统一文案为"响应中"),核心教训保留: LLM 调用指示器必须多态而非二态;WS 推送全帧而非独立帧;**设计文档不等于实际渲染** — 组件存在但未被 import 等于不存在 | thinking-indicator |
| 128 | **「对话即思考」重构(R128)**: 删除 5 类冗余 — `BotTranscript.LastThinking/FullThinking/RecentMessages` 兼容字段/`parallel_think.go`(保留但默认关闭)/`idle_think` 工具/SeatCell 内嵌 `<details> heart_thought`(违反 §119 协议层隔离)/死代码 CSS;**额外删除 Anthropic extended thinking 块**(`ThinkingConfig`/`ThinkingRequired/ThinkingBudget`/`ThinkingFor()` API)。**保留** 7 类核心: `HeartThought` + `Emotion*` + `LLMCallPhase`(文案"思考中"→"响应中") + 5 字段决策可观测 + 4 字段死亡事件 + `LastSummary*` + `LastLLMLatencyMs/Avg/TotalCalls`。详见 [`docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md`](docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md)。**教训**: §128 后统一 — 对话(text+tool_use)= 推理+决策+行动的总和;LLM 不在思考,它在响应;兼容字段置空是技术债务 | dialog-is-thinking |
| 129 | **游戏结束后冷却期(§129)**: 一局结束 (`checkWriter` 写 `Status="over"` + `Phase=PhaseGameOver`) 后,**不立刻**走 `onGameOver` callback / `forceCloseRoomLocked` / `tryEnterRestartVoteFromGameOverLocked`,而是先进入「冷却期」(默认 `CoolingSec=1800` = 30 分钟,配置项 `werewolf.cooling_sec`)。冷却期由 `room_cooling.go` 的 `coolingWatchdog` goroutine 推进,每 `coolingTickInterval=30s` 调用 `m.coolingHumanPresence`(默认 `!hub.IsRoomEmpty`,即 hub 玩家集合 + 观众集合任一非空 = 有人类在线)探测一次:有人类 → 清零 `coolingEmptySince` 延长窗口;无人类 → 记录 `coolingEmptySince`(首次) 或检查是否已超 `CoolingSec`;**超时** → `finishCoolingLocked` 走 `tryEnterRestartVoteFromGameOverLocked` 或 `forceCloseRoomLocked`(走原有关门流程)。watgame 主入口在 `phaseWatchdogTick` 的 `Status="over"` 分支,先 `tryEnterCoolingFromGameOverLocked`,进入后本 tick 直接返回。`restartGameLocked` 重开新一局时调 `resetCoolingStateLocked` 清零状态。`stopAgentsLocked` 首行(在 phase watchdog 之后、agentCancels 之前)必须 `r.coolingCancel()` 清掉 cooling goroutine。**教训**: 冷却期让人类有足够时间复盘 BotTranscript / 整局流程;`coolingHumanPresence = nil` 时 watchdog 视作"始终有人类"永不关门(兜底保护);冷却期在 `gameOverNotified` 之前触发, 房间 status 保持 playing, RestartVote 仅在冷却期结束后触发 | werewolf-room-cooling |
| 130 | **主持人 Agent 重构(R130)**: 审计发现法官三个阻塞缺陷并修复 — (a) **Provider 注入缺失**:`AgentJudge.Provider`/`apiKey` 字段已声明但 `startJudgeGoroutine` 从未赋值,入口守卫 `if j.Provider==nil||j.apiKey==""` 永远成立 → 逐阶段旁白永远走硬编码 `JudgeFallbackText`、从不调 LLM(`judgeChatOrFallback` 失效),仅整局总结路径(绕过 j.Provider 直连 registry)正常。修复:startJudgeGoroutine 经 `registry.Get` 注入,goroutine 内只读(§92a);(b) **法官启动不依赖 Agent 数量**:加 `≥1 Agent` 守卫 + 房间级 `JudgeDesired`/`JudgeModelKey`(`JudgeConfig` 经 `CreateRoomWithAgents` 末尾可选 `*JudgeConfig` 指针透传);(c) **各阶段事件未接通**:`phaseWatchdogTick` 用 §92a **两段式**(锁内记 `judgeWakeKind` 字符串、`defer` 在 `r.mu.Unlock()` 之后调 `wakeJudgeLocked`)接通 11 类 phase 事件,秘密阶段(NightWolves/Seer/Witch)静默。产品:存在性规则「有 Agent 就有法官」+ `RoomCreateModal` 法官配置卡 + `JudgePanel`「一举一动」活动流(`Activities`/`LastLLMMs`) + `JudgeActionBar` pulse + `GameChatPanel` ⚖️ 渲染 + 三语种 i18n。详见 [`docs/狼人杀-重构方案/主持人Agent重构设计.md`](主持人Agent重构设计.md)。**教训**:(1) 声明了可选字段(`Provider`)却永远不注入 = 整条代码路径静默失效,比 panic 更难发现 —— 凡是「可选」字段必须追查所有生产注入点;(2) **设计文档 ≠ 实现**:初版设计(`docs/狼人杀-重构方案/主持人Agent重构设计.md`)列了 `judgeWake`/`SendFromJudge`/`game.judge_announce` WS 帧等愿望清单,但审计时均未实现,重构以「审计发现的真实代码形状」为准;(3) 被 `phaseWatchdogTick` 这类持 r.mu 的长函数调用的唤醒,必须用「锁内记、锁外发」两段式规避 §92a 自死锁 | judge-agent-r130 |
| 131 | **Agent 持久化记忆(MEMORY.md)跨局迭代学习(§131)**: 新增 `t_lsm_game_agent_memory` 表(一模型一行,`model_key` UNIQUE + `version` 乐观锁 + `memory_md` mediumtext ≤100KB);每局结束法官总结落地后(`PersistSummary` 成功路径),由 `IterateAgentMemoriesAsync` 对每个 bot 模型异步发起一次自我迭代(读旧记忆 → `BuildIterationPrompt`(旧记忆 + 本局座位事实/角色/胜负 + 法官总结;>80K 加压缩指令) → 该模型自己的 provider.Chat → `ValidateMemorySections` 校验 4 段标题,不全则 `FallbackMerge` 规则兜底 → >100K 则 `HardTruncateMemory` rune 安全硬截断 → `SaveIterated` version 乐观锁写回,冲突重试 1 次);下一局 `StartAgentsLocked` 时按 modelKey 从 DB 读 `memory_md` 赋给 `a.MemoryMD`,每次 LLM 调用前 `InjectBlock` 注入 user prompt 末尾(截断到 `MemoryInjectMaxRunes=4000` 字 ≈ 2K token)。**4 段固定标题**(战绩与趋势 / 我的失误与教训 / 其他模型特点分析 / 决策策略迭代),空段写"暂无"。**角色差异化学习**:迭代 prompt 注入"本局角色 X",LLM 在"失误与教训"中按角色分类记录(如"作为女巫首夜救人浪费解药"),实现同模型不同角色的经验分化。**开关**:`werewolf.agent_memory_enabled`(默认 true),`agent_memory_max_tokens`(默认 2048)。管理接口:`GET/DELETE /api/admin/llm/providers/:id/memory`(查看/清空)。详见 [`docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md`](docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md)。**教训**:(1) 记忆按 `model_key` 不按座位 —— 同一模型坐几号位共享一份记忆,正是"迭代学习"语义;(2) 压缩主路径是"LLM 迭代时主动瘦身"(compress 指令),硬截断(`HardTruncateMemory`)只是最后兜底,避免关键信息被硬切;(3) 异步迭代不阻塞游戏流(对齐 §118),失败仅 `logger.Warn`;(4) 注入走 user prompt 末尾而非 system,避免 system 膨胀且天然享受"最新消息优先";(5) 乐观锁 + per-model `sync.Mutex` 单飞双保险,防重开局相邻触发并发覆盖 | agent-persistent-memory |
| 132 | **道具系统(LLM 注入攻击游戏化)v1.1 补缺 + §130 死代码回归(§132)**: 道具系统把 6 类 LLM 注入攻击(Markdown 注入/**提示词套娃 `nested_maze`=首个身份暴露道具**/字符欺骗/长上下文失焦/任务马甲/情绪操控)封装为可购买的心理战道具。经济:`prop_engine.go` 扣款后 **50% 回彩池(`r.propPotBonus`,结算 `PropDistributePotBonus` 分给胜方)/ 30% 系统销毁 / 20% 中招补偿被击中者**;`use_prop` Agent 工具 + 人类 WS 帧 `game.werewolf_use_prop`(严格 JSON,字段 `target` **非** `target_seat`)共用 `WerewolfManager.Action_UseProp` 单一真相源;中招服务端权威骰点(不替 LLM 决策,只在目标 GameContext 注入"干扰信号");注入文本入 `propInjectQueue` → 目标下一轮 user prompt(`PropInjectPromptBlock`);公开广播走 `broadcastPropUseLocked`→`emitActivity(kind=prop_used)` 复用活动流(**不新增 game.state 字段/独立帧** — 与 WolfKill/VoteResult 一致,前端 GameChatPanel 自动可见);REST `GET /api/games/werewolf/props`(+admin CRUD)。狼人开局 30% 互知:`StartAgentsLocked` 调 `PickWolfTeammateHint`(rate=`werewolf.wolf_teammate_hint_rate` 默认 30,≥2 狼才触发)→`SetWolfTeammateSeat`→`Memory.ReplaceIdentity` 注入身份 prompt。**教训**:(1) **§130 回归**:`WolfTeammateHint` 曾"定义了却从未被调用"= 功能静默失效,凡新增 helper 必须 grep 确认真实接线(本次修复即在 `StartAgentsLocked` 补接线);(2) **前后端契约字段名**:后端严格 JSON(DisallowUnknownFields)下前端字段名拼错(`target_seat` vs `target`)= 静默全量拒收,新增 WS 帧必须 curl/核对后端 struct tag;(3) **子 Agent worktree 陷阱**:子 Agent 在独立 worktree 跑 `gofmt -w` 会污染 120+ 无关文件,合并回 main 时必须**按 gofmt-归一化 diff 甄别真实语义改动**(9 改+4 新),只 `git apply` 意图文件,禁止整树合并(§10.1 main-only 也要求最小 diff);(4) 公开广播复用既有活动流优于造独立帧,前端零改动即可见 | prop-system-v1.1 | 
| 133 | **道具系统 v4 重构(狼小队交流 + 经济档位 + 效果链)(§133)**: v3 道具系统(v3 commit a8014d4)已实现 7 种道具 + 可扩展注册表 + 任务马甲 v3 示范 + 30% 狼人互知。v4 补齐 3 项缺口:(a) **`WolfPackRoom` + `wolf_whisper` 工具**(`wolfpack_room.go`)—— 狼人小队内部广播通道,FIFO ≤50 条 ≤80 字/条;`addWolfWhisperTool` 仅在 `faction=="wolf" 且 WolfTeammateSeat>=0` 时挂载,留言**不**进入 `chat_message`/`chat_history`/`HeartThought`(协议层隔离,§119);`EmitPlayerDied` 在持锁态调 `PurgeByDeath` 清理死亡狼的留言(防止死人继续影响队友);`buildAgentContextLocked` 把快照拼入狼 bot `WolfPackPromptBlock`。(b) **经济档位 `EconTier`**(`econ_tier.go`)—— 3 档(Health≥50K 销毁30%/Caution≥10K 销毁40%/Danger<10K 销毁50%)按房间总金币存量动态切档,反通胀 + 防无限刷道具;`ComputeEconTier` 调 `r.roomTotalCoin()`(`propEngine.walletSvc.GetBalance` 累加存活玩家),`PropEngine.UseProp` 按档位计算 `potReturn/systemAbsorb/targetCompens`;Agent `EconTierFeedbackBlock` 把档位+销毁率拼入 user prompt 末尾。(c) **可选**:效果链 `EffectStep[]`(后续追加,不影响 v3 字符串格式)。详见 [`docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md`](狼人杀13人局道具系统设计.md) §13。**教训**:(1) **协议层隔离 vs UI 隐藏**:与 §119 HeartThought 一致,wolfpack 留言必须**不入** chat 表/队列/BotTranscript,纯靠 GameContext 在狼 bot user prompt 渲染——若写入聊天表则破坏「狼队内沟通不可见」的核心博弈价值;(2) **经济模型档位化**:硬切 50/30/20 在通胀房间无法抑制道具刷屏;档位化让"销毁比例"成为可调的反通胀工具,且 Agent prompt 同步显示档位让其感知经济压力;(3) **死亡清理路径**:`EmitPlayerDied` 已在 `phaseWatchdogTick` 持锁态被调用,直接接 `PurgeByDeath` 不需要新加锁;(4) **双重防御**:wolf_whisper 在 tool 层校验 `faction=="wolf"` + 服务端 `WolfWhisper` 再次校验 `State.Roles[seat]==RoleWerewolf`,防止身份未确认狼 Agent 误用;(5) **GameContext vs Agent 字段对称**:为避免 agent→werewolf 循环导入,agent 包定义本地 `WolfPackMsg` 镜像结构,`buildAgentContextLocked` 负责 werewolf→agent 的转换 | prop-system-v4 |
| 197 | **流式续命 — "接收到字节即刷新超时"(§197)**: 13 人局慢模型(Kimi/GLM/DeepSeek 典型首字节 1-3min + 长 thinking + tool_use 总耗时 5-15min)经常逼近 `cfgLLMCallTimeoutSec` 上限(300s 基础 / 480s cap)被外层 ctx cancel → `consecutiveFailures++` → 误 quarantine。修复:新增常量 `defaultStreamExtendedTimeoutSec = 900` (15 min) + `cfgStreamExtendedTimeoutSec()` 读取 config `Werewolf.LLMStreamExtendedTimeoutSec`(默认 900,代码内常量兜底);`run.go` 调用层把 ctx 从单层 `WithTimeout(parent, callTimeout)` 改为 `parentCtx = WithTimeout(parent, callTimeout + extendedTimeout)` + `streamProgress._first_token` 触发时打 Debug 日志记录"extended timeout active";`idleTimeoutReader` 已配置 `streamIdleTimeout = 0`(首字节后无 idle 计时器),实际"接收到字节即刷新"由外层总预算实现。`callProvider` / 重试循环的 `select<-ctx.Done()` 全部切到 `parentCtx` 保留长预算语义。详见 §流式续命 测试 `ServerGo/agent/run_stream_extend_test.go` 4 项不变式。**教训**:(1) **分阶段预算优于硬上限**:旧"单层 WithTimeout(callTimeout)"在慢模型场景下频繁误杀;拆为"首字节前 callTimeout + 首字节后 (callTimeout+extendedTimeout)"后,慢模型响应不再被外层 deadline 误杀,但极端卡死场景仍有 parentCtx 总预算兜底(默认 1200s);(2) **不要 ctx.Cancel 后再复用 ctx**:我曾尝试"双层 ctx + streamCancel() 释放短超时",但 `CancelFunc` 一旦调用 ctx 即 Done,无法再"延长";最终选择"单层 parentCtx + idleTimeoutReader 已有 first-byte 熔断"是最简方案;(3) **测试环境 config 可能 panic**:`config.Load()` 在 `ServerGo/` 子目录测试时找不到 `./LsmAgentGame.conf.example` 会 panic,`defer recover()` 兜底返回 0 — 测试不应假设 config 加载成功,而应直接断言常量值(见 `TestStreamExtend_001_DefaultConstant`);(4) **每个活跃 Agent 调用必须走同一套 parentCtx**:含初始 `callProvider` + retry 路径的 `callProvider` + retry backoff 的 `select<-ctx.Done()`,任何一处仍用旧 `ctx` 都会让长预算失效,这是必须 3 处同步修改的关键 | stream-extended-timeout |
| 198 | **法官模式三选项→两选项重构(§198)**: UI 上「主持人 Agent (法官)」「AI 法官」「真人法官」「关闭」四个标签其实只是两件事 —— 「主持人 Agent (法官)」是卡片标题的全称,「AI 法官」是它的简称(同一概念),真人法官后端无实现路径(等同 AI 法官,§130 跟踪项),「关闭」是运维级 kill switch。重构合并为两选项:**Agent 法官** + **真人法官**。改动面 15 个文件:前端 `JUDGE_MODES` 常量、`CreateRoomOptions.judge.mode` 类型联合(`'ai'\|'human'\|'off'` → `'agent'\|'human'`)、三语 i18n(`werewolf.judge.mode.{ai,off}` 删除,新增 `werewolf.judge.mode.agent`,标题文案 `⚖️ 主持人 Agent (法官)` → `⚖️ 法官`);后端 `JudgeConfig.Mode` 注释+`room_service_crud.go:113` 的 `judgeDesired` 判定改为 `judge.Mode == "" \|\| == "agent" \|\| == "human"`、`WerewolfRoom` 加 `JudgeMode` 字段、`SetJudgeConfig` 接口签名加 `mode` 参数、`cfgWerewolfJudgeMode()` 归一化旧 `"ai"` → `"agent"`、测试 fake seater 同步。**教训**:(1) **UI 冗余选项 = 设计未完成** —— 同一概念两个 radio(`AI 法官` vs `主持人 Agent (法官)`)是设计师最初没意识到"全称 vs 简称"造成的迷惑;**新建产品选项前必须先 grep 看是否已有同义选项**;(2) **后端「声明了却从不接线」(§130 三次复现)+ 前端 UI「同义三选项」= 双重失真** —— 真人法官在 `room_service_crud.go:113` 的 `judgeDesired := judge == nil \|\| judge.Mode != "off"` 实际会让 `mode:"human"` 走 AI 法官 goroutine,UI 看着是"请真人来管",后端实际跑的还是 LLM,**UI 占位必须配明确 hint** 或后端必须真实现,二者不可兼得;(3) **跨端字符串契约修改必须同步 7 处**:`JudgeConfig.Mode`(后端注释)/ `cfgWerewolfJudgeMode()` 默认值 / `CreateRoomOptions.judge.mode`(TS 类型)/ `JUDGE_MODES` 常量 / 三语 i18n 键 / 三语 i18n 文案 / 测试 fake seater 签名 —— 漏一处即 silent failure(R118 / R121 同源问题);(4) **旧值归一化是契约兼容最低成本**:`cfgWerewolfJudgeMode()` 把旧 `"ai"` 归一化为 `"agent"` 后,部署中残留的旧 `LsmAgentGame.conf` 不会报错;同理 `room_service_crud.go:113` 接受 `"ai"`/`"off"`/`"human"`/`"agent"`/`""` 五种值,确保 §198 上线后存量房间 + 老客户端不会立刻 400;(5) **「关闭」从房间级 mode 提升为运维级 cfg**:旧 `"off"` 既在 UI 也在 cfg,概念混淆;重构后 UI 不再暴露,但 `cfg.Werewolf.JudgeMode="off"` 仍是全局 kill switch,语义清晰 | judge-mode-three-to-two | 


| 212 | **§92a 自死锁致「创建房间弹窗卡死 + 永久正在同步游戏状态…」(R212)**: 两个 P0。**(A) `AggregateAgentStats` 二次加锁** —— `ec4e71d`「§统计增强」新增的 `func (r *WerewolfRoom) AggregateAgentStats()` 内部 `r.mu.Lock()`,却被 `BuildClientStateWithRoom`(`view.go:997`)直接调用,而后者**全部 4 个调用点**(`GetState` / `StateForSeat` / `SpectatorState` / `SpectatorView`)都已持有 `r.mu`。Go `sync.Mutex` 不可重入 → 第二次 `Lock()` 永久阻塞**且不释放**。表现三连:`CreateRoomWithAgents → SyncSeat → broadcastWerewolfState → StateForSeat` 死锁使 `POST /api/games/werewolf/rooms` **永不返回**(前端 `await` 不 resolve → 弹窗卡死、不导航,但房间列表已能刷出新房间,因为 DB commit 与 `room.state` 广播都发生在死锁**之前**);刷新后 `requestState → GetState` 撞同一死锁 → `game.state` 永不下发 → 永久 `⏳ 正在同步游戏状态…`;该房间所有 REST 快照退化为 `lockRoomBriefly` 200ms 超时兜底。修复:拆 `aggregateAgentStatsLocked()` 锁内变体,公开变体包一层加锁后委托。**(B) `completeHumanWaitAndStart` 双重解锁** —— R211(`03f7c38`)把 `ForceStartIfReady`(**纯显式解锁、无 defer**)的「锁内快照 → 解锁 → onGameStarted + 种 publicStateCache」两段式范式搬过来时,漏删了本函数开头的 `defer r.mu.Unlock()`,末尾显式 `Unlock()` 后 defer 再解一次 → `fatal error: sync: unlock of unlocked mutex`(**不可 recover,直接杀进程**);被 (A) 掩盖故从未跑到。修复:删 defer,两条提前 return 分支补显式解锁。前端韧性:`http()` 加可选 `timeoutMs`(`AbortController`,未设置则完全保持原行为)、`roomService.create` 用 30s、`WerewolfGamePage` 同步超 20s 从无限 spinner 升级为「重试 / 返回大厅」可操作错误态 + `reportGlobalError`(§7.1)。回归测试 `room_r212_deadlock_test.go` R212-A01..A05/B01..B02,**全部经过「还原缺陷代码 → 测试失败 → 恢复修复 → 测试通过」双向验证**。**教训**:(1) **§92a 第 N 次复现,且这次是「新增只读 getter」引入的** —— 凡新增 `WerewolfRoom` 方法,必须先 grep 调用链上游是否已持 `r.mu`;被 `BuildClientState*` 家族调用的方法**默认必须是 `*Locked` 锁内变体**,需对外暴露时再包公开变体;(2) **「纯内存态、不进 DB」的注释会掩盖锁风险** —— `ec4e71d` 反复强调统计不进 DB、随房间 GC 回收,唯独没说它加了一把上游已持有的锁;**评审新代码时锁的获取比 I/O 更值得追问**;(3) **搬运锁相关范式时,第一件事是核对目标函数的解锁风格**(defer vs 显式),只搬后半段必出双重解锁;(4) **定位挂起的三步法**:`gin` 无 HTTP 完成日志 = handler 未返回 → 所有 REST 恒等于 `lockRoomBriefly` 超时值 = 锁竞争 → `kill -QUIT <pid>` 抓 goroutine dump 定位到精确行号;(5) **回归测试必须持锁调用 + 超时守卫**,否则在未持锁的宽松环境下会假通过;并且**新写的回归测试必须先在缺陷代码上验证它确实失败** | werewolf-r212-self-deadlock |
| 134 | **守卫(Guard)角色全链路补全(§134)**: `RoleGuard` 自 2026-07-11 起就在 `godRolePool` 中**真实发牌**,但引擎/工具/UI 三层完全缺失 —— 被发到守卫的玩家整局持有一个**无效身份**,13 人局好人阵营等价于少一个神职,而狼人阵营毫不知情地获得优势。这是 §130「声明了却从不接线」的完整复现,且**比 §130 更隐蔽**:`prompt.go`/`memory.go` 主动向 LLM 声明「⚠️ 暂无独立工具」,把缺陷伪装成既定设计;`activity_emitter.go:739` 早有 `case "guard": return "守卫守护"` 标签是链路做了一半的物证。**实现**: 新增 `PhaseNightGuard`(置于 `pre_wolves` 之后、`night_wolves` **之前**以实现「盲守」)+ 6 个 GameState 字段(`GuardSeat`/`GuardProtectTarget`/`GuardLastProtect`/`GuardSavedTarget`/`SameGuardSameSave`/`WitchSavedTarget`)+ `NightGuardProtect`/`endGuardPhase` + `Action_GuardProtect`/`guardProtectLocked` 双变体(§92a)+ Agent `guard_protect` 工具 + `NightActionPanel` 第四形态 + 三语 i18n。规则:每晚守护一人免疫狼刀 / 不可连守同一人 / 不可守自己 / 可空守 / 护盾只挡狼刀不挡毒 / **同守同救 = 死亡**。详见 [`docs/狼人杀-角色设计/狼人杀守卫角色设计.md`](docs/狼人杀-角色设计/狼人杀守卫角色设计.md)。**教训**:(1) **角色卡池是契约** —— 任何进入 `godRolePool` 的角色必须有完整引擎+工具实现,否则就该移出卡池,二者必居其一,「半实现」是最坏状态(剩余 7 个半实现角色:骑士/魔术师/奇迹商人/射梦人/乌鸦/猎魔人/纯白之女);(2) **护盾与解药必须独立记录** —— 女巫解药用**破坏性赋值** `WolfKillTarget=NoSeat` 表达「已救」,若护盾直接读该字段则同守同救永远无法触发,必须新增 `WitchSavedTarget` 独立记录;(3) **同守同救与护盾两分支必须互斥(switch/else-if)** —— 写成两条独立 if 时,同守同救先把 `WolfKillTarget` 复位为目标,紧接着护盾分支又因 `GuardProtectTarget==WolfKillTarget` 把它清成 `NoSeat`,净效果变回「存活」,规则被完全抵消;(4) **enum 剔除优于事后报错** —— `guard_protect` 的 target enum 在服务端就剔除自己与上晚目标,LLM 无法选出违规值,比返回 error 让模型重试省一整轮 tool_use(慢模型尤其重要,见 §197);(5) **盲守的信息隔离是特性不是限制** —— 守卫 `GameContext.WolfTarget` 恒为 -1,与女巫可见狼刀形成策略反差;(6) 新增夜间阶段必须同步 `SkipPhaseAction`/`watchdogActingSeat`/`dispatchQuarantinedSkipLocked`/`isActingPhase`/`defaultPhaseDeadlineSec`/**前端 `DayNightOverlay.NIGHT_PHASES|DAY_PHASES`** 共 **六处**(§97 的三处 + 两处易漏 + §20260811-08 U4 新增的前端第六处 —— 前端那份 Set 是引擎 `Phase.IsNight()` 的手工副本,§134 守卫/§猎魔人/§20260810-09 警长定序**三次新增 phase 三次都漏改**,导致夜晚遮罩晚一个阶段才出现);(7) `GuardLastProtect` **绝不能**在 `startNight()` 重置 —— 它是跨夜连守校验的唯一依据;(8) 插入 mid-enum Phase 常量前必须验证无序数比较(`grep "Phase >=/<="` 零命中)且 DB 中 phase 均以 varchar 存储 | guard-role-full-chain |

| 20260811-08 | **§130「声明了却从不接线」集中清算 + `redactLedgerFact` P0(§20260811-08)**: 一次清掉 4 项接线缺陷,并由回归测试意外暴露一个更严重的 P0。**(P0) `redactLedgerFact` 破坏结构化 fact 前缀** —— 该函数对整条 fact 做**无边界** `strings.ReplaceAll` 剔除角色词,把机器可读前缀一并打成占位符:`"seer_check seat=1 target=4"` → `"▪_check seat=1 target=4"`、`"guard_protect"` → `"▪_protect"`、`"hit_wolf=true"` → `"hit_▪=true"`;下游 `parseSeatTargetPair`/`parseWitchTriple` 用 `Sscanf` 按前缀匹配,**全部解析失败** → §20260810-09 上帝视角的 `SeerChecks`/`WitchDecisions`/`GuardProtects` 三个聚合字段**自落地起 100% 恒为空**,观战面板的夜间行动历史从未显示过任何数据。修复:结构化前缀白名单(15 个 prefix 逐字节保留,只脱敏参数区)+ ASCII 词边界规则(`replaceWordBoundary` 跳过左右紧邻 `[A-Za-z0-9_]` 的命中,保护 `hit_wolf`/`wolf_pack` 键名;中文角色词非 ASCII 词字符故不受影响,自由文本照常脱敏)。**(U1) `PerSeatPOV` 7 字段硬编码为空** —— §20260810-11 V1 声称落地「全视角读心观战」,但填充代码只赋 3/10 字段,注释自述「实际数据由前端通过单独的 spectator-only endpoint 拉取」而**该 endpoint 从未存在**;且 `GodModeView.tsx` **从未被任何文件 import、零 CSS 规则**(§126 复现)。修复:8 个字段真实填充(HeartThought/LastDecision 走 BotTranscript、NightActions 走信息账本聚合、PublicCommitments 走 CommitmentLedger)+ 组件接线为 HistoryDrawer 第 10 sub-tab + 补 119 行 CSS。**(U1-b) §135 单点判定被绕过** —— `view_godmode.go` 手写 `Status=="over" \|\| HunterFired \|\| IdiotRevealed`,**漏了狼自爆/骑士决斗/猎魔人狩猎三个分支**,这正是 §135 教训 (1)「建立单点判定让第 5 处无法诞生」中所说的第 5 处,它在 §135 落地**之后**又诞生了;收口为 `gs.RolePubliclyRevealed(seat)`。**(U2) 结算奖励只接了 1/4 条终局路径** —— `grantSettlementRewardsLocked` 仅在 `forceCloseRoomLocked` 被调用,`room_watchdog.go:184/205` 与 `finishCoolingLocked`(§129 冷却期,**最常见路径**)三条全漏,即绝大多数对局的奖励从未发放;且 `if !p.Alive { continue }` 让死亡的胜方玩家拿不到奖励(与同路径 `computeCoinDelta` 不看 alive 自相矛盾)。修复:收口进 `EmitGameOver`(4 路径自动覆盖,已核对四处均持 `r.mu`)+ `settlementRewarded` 幂等 + `restartGameLocked` 跨局重置 + 去 alive 过滤 + 失败补 `logger.Warn`。**(U3)** GodMode 补 `PublicActions[]`(猎人开枪/骑士决斗/猎魔人狩猎/白痴翻牌,`HitWolf` 用 `*bool` 区分「没打中(false)」与「不适用(nil)」)。**(U4)** `DayNightOverlay` 补 `night_guard`/`night_demon_hunter`/`sheriff_order` 三个遗漏阶段。**(U5)** 模型风格标识符(前端派发表 + `🤖` 兜底)。**教训**:(1) **「后续 V2 补」是最有效的缺陷伪装** —— 凡遇「本字段先占位,后续 X 会补」必须立刻 grep X 是否存在(同 §134 prompt.go「暂无独立工具」);(2) **作者写下的 §130 自检条款不等于执行了自检** —— `settlement_reward.go:10` 明写「必须在终局路径实际调用」然后只接 1/4,**注释里的自检条款必须转化为测试断言**;(3) **§135 单点判定不是一次性工程**,新增任何身份可见性判断前必须 grep `RolePubliclyRevealed`,建议 CI lint 检测 `view*.go` 中 `HunterFired \|\|`/`IdiotRevealed \|\|` 的裸组合;(4) **前端是「五处同步」清单里被系统性遗忘的第六处**,凡引擎侧有 `IsNight()` 这类分类函数,前端手工副本必须在注释里互相指认;(5) **只测转换函数、不测转换结果,等于没测** —— 原 `TestLedger_L07_Redact` 只断言「身份词被剔除」,从未断言「聚合结果非空」,P0 因此潜伏整整一个版本;**凡「写入→解析」成对的管线,必须有一条端到端断言解析产物非空的用例**;(6) **无边界 `ReplaceAll` 用于脱敏是高危模式** —— 脱敏函数作用于「自由文本 + 结构化载荷」混合的字符串时,必须先切分载荷边界再脱敏 | upgrade-20260811-08-wiring-audit |
| 135 | **身份公开公平性 —— 死者身份牌不翻开(§135)**: 审计发现两条违反标准竞技局规则的缺陷。(a) **死亡即全场翻牌**:`view.go` 的 `if !p.Alive { RoleRevealed = true }` 使**任何**玩家一死(狼刀/毒杀/投票/猎人枪)就把 role+faction 下发给全房 —— 女巫毒药沦为「免费验人」、狼刀预言家后的悍跳博弈价值归零、误票平民后好人立刻确认票错。同源违规共 4 处:`view.go:387`(players[].role)、`buildAllDeadListLocked`/`buildDeadListLocked`/`buildDeadListForSeatsLocked`(**无条件**填 `DeadPlayerJSON.Role`,是绕过 players[] 脱敏的第二条通道)、`room_state.go`(REST 详情,一个请求拿全部死者身份)。(b) **猎人夜间被刀从不能开枪**:`HunterPendingShoot` 仅在白天投票放逐两条路径置位,`endWitchPhase` 结算狼刀后从不检查死者是否猎人 —— `HunterPendingFrom=="wolf"` 分支、`view.go` 的 `!="poison"` 守卫、`HunterShoot` 的 poison 拒绝分支全是为一条**永不执行**的路径写的死代码,猎人损失约一半技能触发面。**实现**:新增 `Player.HunterFired` + `GameState.RolePubliclyRevealed(seat)` **单一事实来源**(终局/白痴翻牌/狼自爆/猎人实际开枪 4 类白名单)+ `publicRoleName()` 收口三个死亡列表构造器 + `endWitchPhase` 接线猎人夜间开枪 + `resumeAfterHunterShoot(from)` 按触发来源分流恢复(wolf→回 dawn 续白天,vote→advanceDay 进下一夜)+ Agent prompt 注入身份公开规则 + 前端 4 处纵深防御 + `werewolf.md` §7.0 章节。测试 `engine_reveal_fairness_test.go` R-01..R-14。**教训**:(1) **「死亡即公开」是最容易被当成常识的错误假设** —— 它在 4 个文件里被独立复制了 4 遍,每处都写着「死亡的人:全场合法公开」的注释,说明作者确信无疑;修复的关键不是改 4 处 if,而是建立 `RolePubliclyRevealed` 单点判定让第 5 处无法诞生;(2) **脱敏必须堵住所有下发通道** —— 只修 `players[].role` 而漏掉 `all_dead_list_verbose` 等于没修,前端 HistoryDrawer 照样渲染出全部死者身份,**通道数量要靠 grep 结构体字段而非靠读主流程**;(3) **§130/§134「声明了却从不接线」第三次复现** —— 这次伪装得更深:规则文档 §8.2 明确写了「狼刀 ✅ 可开枪 HunterPendingFrom=wolf」,校验分支、view 字段、枚举值全部齐备,唯独没有生产置位点;**凡是 enum/常量的某个取值 grep 不到写入点,该分支就是死代码**;(4) **恢复路径必须按触发来源分流** —— 猎人夜死的开枪发生在黎明遗言之后,若沿用白天的 `advanceDay()` 会直接跳进下一夜、整个白天被吞掉;(5) **「不开枪」也是一种身份保护** —— 开枪才亮身份,给猎人「藏身份 vs 带人」的真实权衡,与「被毒不能开枪且身份保密」语义自洽;(6) 反转既有测试断言时必须**改测试名**(`TestBuildClientState_RevealsDeadPlayers` → `..._HidesOrdinaryDeadPlayerRole`),否则名字与断言相反会误导后来者 | identity-reveal-fairness |

| 136 | **前端目录结构重构 —— 消除跨游戏耦合(§20260807-03)**: 审计 `ClientWeb/src`(193 文件 / 42,857 行)发现 6 项结构缺陷并全部修复。**(a) 跨游戏 import(P0)**:`components/xiangqi/GameChatPanel.tsx` 实为 5 款游戏共用的房间聊天面板(其文件头自述 "Shared by xiangqi, junqi, doudizhu, texasholdem, and werewolf"),却放在象棋目录下 —— 4 个 GamePage + werewolf 适配器共 5 处跨目录引用,是 `components/` 树中唯一的游戏→游戏边;已上移至 `components/chat/`。**(b) 陈旧副本 + 功能漂移(P0)**:`components/chess/GameChatPanel.tsx`(303 行)是共享面板的 fork,`grep -c 'useStreamingMessages|isSpectator|isLocalPlayerDead|currentDay'` → 共享版 18 / chess 版 **0**,即 SSE 流式气泡 / 观战者提示 / 死亡清稿 / Day N 四项特性全缺,另残留 2 个已在共享版修掉的 bug(`selectMention` 未剥离 `@name` 前缀、whisper 按钮双写 draft);已删除并改用共享面板,chess **零改动获得 4 项特性 + 2 个 bug 修复**。**(c) `util/` vs `utils/` 双目录**:语义相同、内容不相交(10 vs 6 个引用),已合并为 `shared/utils/`。**(d) 游戏私有代码混入共享目录**:`utils/werewolfEmotion.ts` 的 3 个引用 100% 在 `components/werewolf/` 内,已归位为 `components/werewolf/emotion.ts`。**(e) §4 行数超限**:`styles/werewolf-v2.css` 1940 行 > 1800,拆出 `werewolf-emotion.css`(EmotionAvatar 整段,自带 keyframes、边界干净)后降至 1631 行。**(f) 资产命名不一致**:`assets/images/werewolf.ts`(文件)与同名目录并存,归一为 `werewolf/index.ts`。**教训**:(1) **「共享组件放在某个游戏目录下」是最隐蔽的耦合** —— 它不报错、不影响运行,却让 3 个 page 各引用 2 个游戏目录,且新人会自然地在 chess 下再 fork 一份(本仓库已真实发生);判据应是**「引用方是否 100% 属于该游戏」而非「谁先写的」**;(2) **fork 一旦产生就只会单向腐化** —— chess 副本停留在初版,共享版迭代出的 4 项特性它一项没有,且共享版修过的 2 个 bug 在它身上仍然活着;**删除 fork 比同步 fork 便宜一个数量级**;(3) **薄适配器是正确范式**:`werewolf/GameChatPanel.tsx` 用 87 行做 props 映射,既保留游戏语义又零重复,应推广;(4) **不为合并而合并** —— 6 份 `GameInfoPanel` 分棋类/卡牌/狼人杀三簇,狼人杀版(263 行,含 FactionDrawer/暂停控制)与棋类版语义差异过大,强行统一会产出谁都不好用的联合 API,本次刻意只做「消除跨游戏耦合」主线,合并 CardView / 抽 GamePanelActions 等留待独立提交;(5) **CSS 拆分必须以构建产物验证** —— `globals.css` 的 @import 顺序即级联优先级(`werewolf-v2.css:493-508` 有翻车记录),本次拆分后 `dist/assets/index-*.css` **字节完全一致**(154,297 B),这是零视觉回归的唯一硬证据,`tsc` 与 `npm run build` 通过都证明不了这一点 | frontend-structure-20260807-03 |
| 137 | **Agent 道具系统对齐 6 类 LLM 注入攻击(§20260807-04)**: 基于仓库根目录 6 份注入攻击文档(`第一种~第六种`),审计 `ServerGo/game/werewolf/prop_*` + `agent/wwplayer/prop_blocks.go`,发现并修复 7 项差距。**(P0-1)** `isExposeProp` 漏 `PropTaskDisguiseV3` → 狼人对队友可绕过保护用任务马甲(v3 强化版)暴露身份,补枚举。**(P0-2)** AOE 道具 `long_swear` EffectTypes 在 manager/agent 双路径因 `if result.Hit && target >= 0` 条件全部丢失(AOE target=-1) → 范围道具命中后 `attention_scatter/target_twist` 干扰信号从未落地任何 Agent GameContext;修复改为遍历 `r.State.Players` 对所有存活 bot 逐个 `enqueuePropHitLocked`,每个目标独立 `computeTwistSeatLocked`。**(P0-3)** 现有 7 个道具全部是 Agent→Agent,**人类被道具击中时 PropInjectPromptBlock 注入文本毫无意义**(`internal_thought`/安全对齐机制对人类不存在);新增 3 个 `TargetCamp:"human"` 道具 — 📰`md_bomb_human`(公告轰炸,130/30%,目标人类下一轮发言强制「系统公告」前缀 + UI 高亮)/ 🎭`nested_maze_human`(剧本迷宫·人,160/25%,UI 显示「系统推荐投票目标」视觉干扰)/ 🔣`char_confuse_human`(乱码干扰,90/22%,其他玩家发言被插入 emoji/乱码);走 `gc.HumanDebuff`(`wwtypes.HumanDebuffSpec` 新 struct)→ `r.State.Players[seat].HumanDebuff` → `view.go` 透传前端。**(P1-1)** `markdown_bomb`/`nested_maze`/`task_disguise[_v3]`/`emotion_plea` 注入文本一律「暴露身份」对狼人无效(狼人巴不得被当好人);扩展 `InjectRequest.ToRole` 分支:werewolf→诱导 internal_thought 写刀人目标 / seer→写查验座位 / witch→写是否用药 / 其他→保持暴露身份。**(P1-2)** `drainPropInjectQueueLocked` 过期 `ExpiresAfter--` 写在 `for _, e := range entries` 值拷贝上(v3 v4 链路效果若设 `ExpiresAfter>1` 会永不递减),改索引遍历。**(P2-1)** `PropSystemPrompt()` 硬编码 7 个道具名 → admin DB 新增道具 system prompt 不提及,改为「清单见每轮【道具状态】」。**(P2-2)** Agent 被击中后 `PropEffectSignalBlock` 仅命中轮渲染,下一轮即被防御性重置清空;新增 `GameContext.PropHitLastRound` + `r.lastPropHitEffect map[int]string` 让 Agent 在下一轮 prompt 看到「📌 上一轮你被 X 击中」。回归测试 `prop_aoe_test.go` 8 项(AOE 入队/HumanDebuff 落地/ExpiresAfter 递减/角色差异化/3 Effect + 3 Inject 注册)全 PASS。**教训**:(1) **「条件判断漏枚举」是 P0 bug 的高发区** —— `if result.Hit && target >= 0` 这种看似自然的写法在 AOE 路径下永远为假,`long_swear` 唯一 AOE 道具干扰信号**从未生效**,审计前没人察觉,因为「公开广播+中招文案」仍正常触发;(2) **「目标导向攻击」必须双向实现** —— 原 7 道具只覆盖 Agent→Agent,人类面对 Agent 无任何反制手段;新增 `TargetCamp:"human"` 后攻击/反制对称(§132 道具系统 v1.1 已声明可扩展但从未补人类侧,§130 第 N 次复现);(3) **§92a 教训的镜像** —— `enqueuePropHitLocked` 在 manager/agent 双路径必须同步改,漏一处即 AOE 半失效,同 §132 教训 (1);(4) **值拷贝 vs 索引遍历** —— Go `for _, e := range entries` 中 `e` 是副本,对 `e` 的修改不写回原切片,过期递减/字段累加类操作必须用 `for i := range entries`;(5) **Agent 反馈必须跨轮延续** —— 一次性「你被击中」信号无法驱动 Agent 调整策略(它下一轮已忘记),`PropHitLastRound` 把「持续 1 轮的记忆」推到 prompt 边缘,Agent 才能形成「我被 X 道具击中过 → 这轮要保守」的策略闭环 | agent-prop-20260807-04 |

| 20260812-03 | **狼人杀 Agent 4 项升级(§20260812-03)**: 4 项独立升级按"易/价值高/无重复"原则精选:**U1 阵营胜率热力图**(P6-D 余项)/ **U2 暗线信件+私密结盟合并**/ **U3 阵营赌注系统**(P7-B)/ **U4 3 条核心理由 prompt 增强**(DouBao §五.4 余项)。**(U1)** 新增 `ServerGo/game/werewolf/win_predict.go`(启发式算法,纯客观信号无 LLM 决策污染)+ `ClientGameState.WinRateProbability []float64`(仅 `viewer<0` 分支填充,§132 隐私隔离)+ 前端 `WinRateHeatmapPanel`(13 座位 6 阶色阶网格)+ HistoryDrawer 第 11 sub-tab;**教训**:(1) **公式不调 LLM 是 §120 公平性硬约束** —— 胜率若调 LLM,LLM 概率推理可能与狼人 Agent 决策形成「LLM 提示词套娃」漏洞(同 §20260807-04 第二种攻击);(2) **概率钳制到 [0.02,0.98] 是 §135 隐私护栏** —— 0/1 极端值会暴露"已验人"的确定身份,必须留 2% 模糊带;(3) **`gs.RolePubliclyRevealed(seat)` 是单一事实来源**(同 §135 教训 1)—— 不要重写 "if dead and revealed" 散落判定,所有"该座位身份是否公开"判断都走这个方法。**(U2)** 新增 `secret_letter_room.go`(内存态,§119 三重隔离,每日 5 条/≤200 字/仅白天 speak 阶段)+ GORM 模型 + manager 入口 + 前端 `SecretLetterPanel`;**教训**:(1) **私下通道设计要"合并同类项"** —— §3.1 暗线信件与 §4.3 私密结盟本质同构,合并实现可省一半实现/测试工作量;(2) **§97 不发新 phase 即可"借道"** —— 借白天 speak→vote 窗口,无需触发六处同步表(R212 §97 教训的镜像);(3) **服务端校验不可被前端选项绕过** —— `<select>` 看似只显示存活座位,但恶意客户端可发任意 seat,服务端必须独立校验 `r.State.Players[targetSeat].Alive`。**(U3)** 新增 `faction_bet.go` + GORM 模型 + manager `PlaceFactionBet`/`SettleFactionBetsLocked`(纯函数 `settleBetsCore`)+ 前端 `FactionBetPanel`;**教训**:(1) **§133 EconTier 独立常量原则** —— `FactionBetDestroyRate = 50` 与 §133 道具销毁**不**共用常量,避免互相耦合(同 §133 教训);(2) **结算逻辑保持纯函数** —— `settleBetsCore(bets, actualFaction)` 不持锁、不读 r,方便单测与未来替换为更复杂公式;(3) **DB schema 与字段名前后端必须一致** —— `predicted_faction`/`target_seat`/`amount` 严格 JSON,前端用 camelCase 转换避免 silent failure(§121 教训)。**(U4)** `prompt.go` 末尾新增 `ThreeReasonsBlock`(仅在关键行动阶段输出)+ `decision_summary.go` 新增 `ExtractThreeReasons` 函数 + `BuildDecisionSummary` 对 `speak_with_thought` 走 80 字放宽分支;**教训**:(1) **「决策时思考」必须走 `LastDecisionSummary` 既有通道**(§128)—— 不新增字段是硬约束;(2) **工具内 `internal_thought` 字段已在 §119 协议层隔离**,本 U4 完全复用,无需新加 chat/HeartThought 通道;(3) **多字节前缀匹配必须用 `[]rune`** —— 「1.」「1、」「1)」 三个分隔符的字符串处理,`trimmed[1] == '、'` 在 Go 中会触发 "overflows byte" 编译错误(实测在 decision_summary.go:223),必须先 `[]rune(trimmed)` 再索引。**验收**:4 项后端 1300+ 行 + 前端 1000+ 行,`go build ./...`/`go test ./game/werewolf/... ./agent/... -count=1`/`tsc --noEmit` 全部 PASS;新增 16 项单测(7 SecretLetter + 4 FactionBet + 5 WinPredict)。**整体教训**:(1) **「多升级一锅端」必须分清共享 vs 独立** —— 4 项共享一个 `Werewolf20260812API` 入口避免 4 个零散 router 路由,但每个 endpoint 独立校验 + 独立 isRoomMember 判定,不能为了共享而牺牲隔离;(2) **CSS 文件命名要带日期** —— 4 项新组件共用一个 `werewolf-20260812-03.css` 而不是 `werewolf.css`/`werewolf-v2.css`,避免 1800 行超限(CLAUDE.md §4) | upgrades-20260812-03 |
| 20260812-04 | **§130「声明了却从不接线」第五次复发 + P0-1 神职私有信息从未进 prompt(§20260812-04)**: 基于 TencentDB-Agent-Memory 架构对照审计,发现 7 项 P0 + 6 项 P1,本次修复 6 项(U1-U6)。**(P0-1 ★ 最严重)** `GameContext.MySeerCheck`/`WolfTarget` 由引擎正确填充(room_agent.go:766/770),但 `agent/` 目录**零读取点**、`prompt.go` **零渲染** —— **AI 预言家查完人永远不知道结果、AI 女巫永远不知道谁被刀**,两个核心神职技能对 Agent 完全失效;而人类玩家走 `view.go:1285 BuildSeerInform` 一直正常,**只影响 AI 的信息不对称**直接违反 §15/§120。更隐蔽的是 `MySeerCheck` **只存座位号不存阵营**(`FactionOf` 结果只在 BuildSeerInform 里算过),即便渲染也说不出「4号是狼人」。修复:新增 `MySeerCheckFaction`/`MySeerCheckHistory`/`Witch*Used` 字段 + `NightPrivateInfoBlock` (紧跟身份块的高注意力位) + `buildSeerCheckHistoryLocked`(复用 `Player.SeerCheckHistory` 结构化字段,**不用 InformationLedger 字符串反解** —— §20260811-08 的 P0 正是脱敏打坏了那个前缀)。**(U2)** user prompt 尾部 13 块改走 `AssembleWithBudget` 优先级降级(Critical 永不丢 + **丢弃必留可观测标记**);**(U3)** `SystemBlock.CacheControl` 字段存在却**从未赋值**,~14KB system prompt 每次全额计费 —— 打上 ephemeral breakpoint(只打 1 个,多打反而计费);**(U4)** 记忆按角色分子段选取注入 + 接线死配置 `difficulty.MemoryInjectRunes`(4 赋值 0 读取);**(U5)** 三项一行修复:endpoint breaker 未列 transient(唯一会累计 cf 的熔断,一次上游抖动全房 13 bot 批量 quarantine)/ `defer ReleaseLLMSlot()` **写在 for 循环体内**(Go defer 是函数级,5 轮内层循环吃满 cap=4 房间信号量,与注释宣称的「不阻塞快模型」完全相反)/ 发言下限 2次/60s 与同座位冷却 60s **数学互斥**(每分钟最多 1 次 ⇒ 下限永不满足 ⇒ 13 人局 39 次注定失败的 wake/分钟);**(U6 ★ 机制化)** 新增 `wiring_lint_test.go` 三条断言(块函数有生产调用点 / SkipPhaseAction 动作有派发 case / 夜间私有字段有读取点),并顺手补齐 `demon_hunter_hunt_skip` 缺失的派发 case。**教训**:(1) **注释里的自检条款不会被执行,必须转化为测试断言** —— §130/§134/§135/§20260811-08 已四次记录该教训并写入自检条款,本次仍新发现 12 处;§20260811-08 教训(2)自己也没转化,U6 是第一次真正转化;(2) **写反的注释是缺陷的最佳伪装** —— `EmitSeerCheck` 注释写 `silentForBots=false` 而实参是 `true`,读代码的人会以为 bot 能看到,这多半是 P0-1 潜伏整个版本的直接原因;**注释与实参不符应当像编译错误一样对待**;(3) **lint 写对了会咬自己** —— U4 把 `InjectBlock` 变成零调用点包装函数,U6 lint 立刻判它死代码(判得对),遂删除并改 `InjectBlockWithBudget` 为唯一实现点;**能咬到作者本人的 lint 才是有效的 lint**;(4) **降级必须留可观测标记** —— 对照 TencentDB 的反面教材(L1 抽取失败返回 `[]` 与「确实没啥可抽」同形,静默劣化潜伏一个版本),U2 从设计之初就带 `[因上下文预算省略 N 块: ...]`;(5) **护栏预算宁松勿紧** —— userPromptTailBudgetRunes 取 12000(实测常规对局 6500),正常路径完全不触发;**宁可护栏偶尔不生效,也不要它频繁误杀**;(6) **fail-fast 与 fail-soft 按「失败后果」选而非统一风格** —— breaker 应 transient(后果是误杀 bot),400 超限应 fail-fast(后果是无限重试);(7) **Go `defer` 是函数级不是块级** —— 循环体内 `defer` 释放资源必然累积到函数返回,凡在 `for` 内 acquire 的资源必须显式轮末释放 + 函数级 defer 兜底。**验收**:`go build ./...` + `go test ./agent/... ./game/werewolf/... ./llm/...` 全 PASS;新增 30 项单测(6 NightPrivate + 7 PromptBudget + 7 MemoryRole + 3 SlotLeak + 7 Clamp)+ 3 条 wiring lint;U1/U2/U6 三组均经「还原缺陷 → 测试失败 → 恢复修复 → 测试通过」双向验证 | wiring-lint-20260812-04 |
| 20260813-04 | **§130「声明了却从不接线」第七次复发 + Hermes Agent 对标优化(§20260813-04)**: 以 Nous `hermes-agent` 的 10 项设计模式为标尺审计,修 3 项 P0 接线 + 3 项 P1 能力。**(U1)** `steeringQueue` —— `steering_queue.go` 是 **149 行完整实现**、`run.go:682` 甚至写了「灵感来源: PI Agent 的 steeringQueue.drain()」,但**零 setter** → 恒 nil → 整个实现从未执行;补 `SetSteeringQueue`/`SteeringQueue`/`CloseSteeringQueue`(**先置 nil 再 close**,否则并发调用方拿到 closed channel 发送即 panic)+ `StartAgentsLocked` 注入 + `enqueuePropHitLocked` 单点入队(manager/agent 双路径已汇于此)。**(U2)** `toolHooks` 字段是**孤岛**(零读取零 setter),`tools.go:846` 写死 `nil`;补 setter 并让**生产调用点**(run.go 主路径 + speak_floor 路径,两处必须一致)改走 `DispatchToolWithHooks(..., a.ToolHooks())` —— **不改包级 `DispatchTool` 签名**(会让所有 ToolRunner 测试桩编译失败,§130 防御)。**(U3)** `difficulty.MaxToolUse` 4 赋值 0 生效(agent.go 硬设 0 + 注释「不再使用」),难度档位对工具轮次完全无效;**不复活旧全局上限**(§130 废弃它是对的),改名 `difficultyRoundCap` 并**调制** `maxInnerRoundsForPhase`,**只收紧不放宽**(easy=3 把发言从 5 轮收到 3;hard=6/hell=8 比夜间基线 3 宽故保持基线 —— 放宽会破坏 §197 慢模型预算假设)。**(U4)** pre-flight 预检(借鉴 Hermes `_compute_threshold_tokens`):**从模型窗口减去 max_tokens 输出预留**(Hermes #43547,本项目此前完全没有这个概念,`getModelContextBudget` 返回的是整个窗口)+ 预留吃掉窗口时退化为 85%(#14690)+ `llmMaxTokensPerCall` 常量收口 run.go 两处硬编码 2048(**两处漂移则预留量算错,预检前提失效**);此前唯一自适应路径是等上游 400 后 `PruneByBytesAggressive` —— 对慢模型 = 白等数分钟且占着 llmSema 槽位。**(U6)** `PruneToolResultsOnly` 无 LLM 确定性剪枝(借鉴 Hermes `prune_tool_results_only`),形成三层梯度 60%/100%/400;**只截断 tool_result 内层 text,不删块不动 tool_use_id**(§82b 配对保护)+ rune 边界回退(中文按字节硬切必产无效 UTF-8)+ 标记后缀幂等守卫。**(U6 顺带修既有 P0)** `approxPayloadBytes` **完全不统计 `c.Content`**(tool_result 嵌套 `[]ContentBlock`,正是工具返回文本的存放处)—— 实测 30 组 4.8KB 的 tool_result 只被算作 1.6KB,**低估 90×**,于是字节预算认为「还没超」而不剪枝,直到 400 才被动压缩:**这是「等 400 才压缩」的根因之一**。**(U5 ★ 机制化)** 新增 `wiring_lint_field_test.go`:AST 提取 `Agent` struct 全部**引用类型私有字段**,断言每个都有生产 setter / 构造赋值 / 白名单(**理由非空**);§20260812-04 U6 的 6 条断言是「针对当时具体缺陷的**专项**断言」,本次 3 处缺陷**一条都没咬到**。**教训**:(1) **完整实现 + 详尽注释 + 零接线 = 最强伪装** —— U1/U2 都是可用的完整模块,文件头详述设计动机,**注释的详尽程度与接线的完整性无关;写得越像已完成,越容易被当作已完成**;(2) **lint 必须断言「模式」而非「实例」** —— 专项断言追不上复发速度,U5 先写、先验证它咬到 U1/U2、再修复转绿(**能咬到作者本人的 lint 才有效**,§20260812-04 教训 3 的第二次应用);(3) **同一模式的两个实例,修一个漏一个** —— `MemoryInjectRunes`(§20260812-04 U4 已修)与 `MaxToolUse`(本次)都是「difficulty.go 4 赋值 + agent 侧 0 读取」,**修完一个缺陷必须 grep 同 struct 其他字段是否同病**;(4) **Hermes 的 `@abstractmethod` 是 Go 缺失的能力** —— Python ABC 让「声明了不实现」无法实例化,Go 的 interface 不约束字段、编译器未使用检查不覆盖 struct 字段,**语言不保证的必须靠 CI 保证**;(5) **pre-flight 与 post-error 是独立机制不可互替** —— 前者处理可预测超限、后者处理估算不准,Hermes 同时保留两者;(6) **近似统计漏字段比没有统计更危险** —— `approxPayloadBytes` 漏 `c.Content` 让预算「看起来在工作」,实际对最大的负载视而不见,**凡嵌套结构的字节/token 估算必须逐字段核对 wire 形状**;(7) **幂等性要显式守卫** —— 截断后追加标记使总长仍 > 阈值,无「已含标记」守卫会导致每轮蚕食一小段直到只剩标记。**验收**:`go build ./...` + `go test ./...` 全 PASS;新增 38 项单测(4 SteeringQueue + 4 ToolHooks + 4 DifficultyCap + 11 Preflight + 7 ToolResultPrune + 1 字段 lint + 7 dispatch);U5 lint 经「先失败(咬到 U1/U2) → 修复 → 转绿」双向验证;U6 的两项缺陷(payload 低估 / 非幂等)均由测试**先抓到**再修。文档:[Hermes 源码分析](docs/其他Agent代码分析/Hermes_Agent_源码分析.md) + [优化方案](docs/狼人杀-重构方案/狼人杀Agent_根据_Hermes_优化和解决方案_20260813.md) | hermes-wiring-20260813-04 |

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
- 完整 curl 样例、密码管理与验证码旁路规则见 `docs/通用功能/测试账号凭证.md`。

## 22. 自动化测试报告处理流程

> R46–R51 等多轮「报告→修复→验证」历史快照已迁出本文件，按需查阅 `docs/` 下归档或 git log。
> 本节仅保留**流程规约**，不再记录每轮具体报告内容。

- **检索入口**：主工程 `TestReport/自动化测试报告_*.md`；子工程 `go-web-debug-tool/UseReport/测试工具使用报告_*.md`。
- **处理入口**：根目录 `AutoDebugTestReport.sh` —— 后台启动 Claude Code，加载 `AutoDebugTestReport.md` 作为 prompt。
- **流程规范与硬约束**详见 `AutoDebugTestReport.md`；其中**绝对禁止**自动修复流程写入 `CLAUDE.md` / `KILO.md` / `AGENT.md` 这三个规则文件。
- **报告清理**：修复完成后必须删除已处理的 `TestReport/*.md`（子工程 `UseReport/*.md`），报告不应在仓库中长期堆积。

## 23. 狼人杀 Web 运行时 UI（房间总运行时间 + 历史抽屉）

> 2026-07-18 用户反馈响应：① 房间运行多长时间没有显示；② "显示历史"按钮/功能没有入口、没有空间。详见 [`docs/design/狼人杀13人局UI运行时优化设计.md`](docs/design/狼人杀13人局UI运行时优化设计.md)。

- **`game_started_at` 下发**：服务端 `WerewolfRoom.gameStartedAt`(已存 6 处写入) 之前只入 `agent.GameContext.GameStartedAt` 给 LLM；从未下发到 `ClientGameState`。**修复**：`ServerGo/game/werewolf/view.go` 给 `ClientGameState` 加 `GameStartedAt int64 \`json:"game_started_at,omitempty"\``(`omitempty` 保证 0 不污染历史回放),`BuildClientStateWithRoom` 头部塞 `cs.GameStartedAt = r.gameStartedAt`。
- **前端新组件 `RoomRunningClock.tsx`**：1s `setInterval`（可选注入 `nowMs`）计算 `nowMs - gameStartedAt*1000`,格式化 `{HH:}MM:SS`,未开局显 `⏱ —`,结束态显 `整局`。挂在 `werewolf-main-header` 右上 + `HistoryDrawer` 头部。
- **新抽屉 `HistoryDrawer.tsx`**：与 `FactionDrawer` 同宽同位（380px / 30vw），4 sub-tab：⏱ 时间轴（拼 `votes / sheriff_streams / idiot_revealed_seats / all_dead_list_verbose`）/ 🤖 独白（仅 spectator 显 `heart_thought`,全员显 `last_decision_summary`）/ ⚰ 死亡（遍历 `all_dead_list_verbose`,按 `verdict` 上色）/ 🏆 总结（`judge_summary` + `judge_model_memories` 按模型折叠）。ESC 关闭,焦点循环在抽屉内。
- **3 入口冗余**：Header 顶层 "📜 历史"（主入口,sticky）+ 房间信息面板 header 并列按钮（次入口,`📚 500K` 旁）+ `GameInfoPanel` 第 5 块改三按钮(规则/历史/退出)。
- **`max_seat ?? 12` 兜底错位**：玩家页 fallback 与 i18n/`RoomCreateModal`/`WerewolfTable` 不一致(后者已迁 13),会导致等待阶段显示 `等待 12 位玩家入座…`。修复为 `?? 13`,保持一致。
- **触控 44 × 44 token**：`.werewolf-game { --ww-touch-target: 44px }`,作用域隔离（仅狼人杀）。所有 `.btn / .seat-chip / .ww-header-btn / .ww-history-drawer__tab` `min-height: 44px`。棋类不受影响。
- **中栏紧凑间距**：`<1599px` `board-container` 内 gap/pad 由 10px 缩到 6px；`<1280px` 再压到 4px,缓解 13 人 4 行堆叠挤压。
- **i18n 三语同步**:zh-CN/en/ja 全部补 `werewolf.history.*` 21 键。**教训**:`http<T>` 拆 wrapper 类型外,新 UI 字段必须**同时**进 `i18n/types.ts` + 三语文件,否则类型检查不报错但运行期 t() 返回 key 字符串。
- **数据来源 100% 复用现有字段**：时间轴 / 死亡 / 总结 / 独白全部用 `game.state` 中已有的 `phase_extra/votes/sheriff_streams/idiot_revealed_seats/all_dead_list_verbose/bot_contexts/judge_summary/judge_model_memories`,**不新增 HTTP API**。

## 24. AgentClassName 与 User-Agent 拼装约定

> 2026-08-06 §Agent 重构增强。每种 Agent 实现（玩家 Bot / 法官 / 记忆迭代 / 未来其他游戏）都有一个独立的 `AgentClassName`，统一登记在 [`ServerGo/agent/class_names.go`](ServerGo/agent/class_names.go) 单文件。

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
- 不修改 `SetUserAgent` 全局调用避免破坏向后兼容（默认 `LsmAgentGame/<ver> <time>`）；per-request 覆盖通过 `LLMRequest.AgentClassName` 透传，空值回退默认。

## 25. Agent 道具与 LLM 注入攻击对齐（§20260807-04）

> 仓库 6 份注入攻击演示文件(`docs/注入攻击演示/01-06-*.md`)是 Agent 道具系统的事实来源。Agent
> 道具的设计边界与攻防含义详见 [`docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md`](docs/狼人杀-道具与经济/狼人杀13人局-Agent道具-20260807-04.md)。

### 25.1 三类攻击分类（事实来源 vs 落地方向）

| 攻击文档 | 攻击类型 | 落地方向 | 已实现道具 |
|---|---|---|---|
| `第一种：Markdown 格式注入` | Agent → 人类 | 注入 → `gc.PropInjectText` + UI 公告前缀 debuff | `markdown_bomb` + `md_bomb_human` |
| `第二种：提示词套娃（多层嵌套）` | Agent → 人类 | 注入 → 投票推荐 debuff | `nested_maze` + `nested_maze_human` |
| `第三种：字符级欺骗（混淆式）` | Agent → 人类 | 注入 → 发言乱码 debuff | `char_confuse` + `char_confuse_human` |
| `第四种：长上下文注意力失焦` | Agent → Agent | `long_swear`(AOE) → 所有存活 bot `propInjectQueue` + EffectTypes | `long_swear`(v20260807-04 修复 AOE 入队) |
| `第五种：任务马甲` | Agent → Agent | `expose_identity` + `emotion_disturb_light` 干扰信号 | `task_disguise` + `task_disguise_v3` |
| `第六种：情绪操控` | Agent → Agent | `emotion_disturb`(下轮 confused/guilty) | `emotion_plea` |

### 25.2 §20260807-04 关键修复清单（与 §13 lesson 137 对照）

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
- `wwtypes.GameContext` 新增 `HumanDebuff *HumanDebuffSpec` + `PropHitLastRound string`
- `WerewolfRoom.Player.HumanDebuff` 透传到 `view.go::PlayerJSON.HumanDebuff`

## 26. 前端 UI 颜色对比度与可读性规范

> **2026-08-08 §20260808-02 用户反馈响应**。狼人杀 13 人局 Web 端被报告
> 存在多处「白底白字 / 绿底绿字 / 选中态不可见」等可读性缺陷。
> 通用规约全文见
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
- **生效版本**:RoomCreateModal.tsx (`werewolf-modal.css`) + `chat.css` + `werewolf.css` + `werewolf-v2.css` + `werewolf-panels.css` + `werewolf-agent.css`

