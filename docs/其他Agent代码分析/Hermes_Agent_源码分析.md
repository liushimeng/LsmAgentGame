# Hermes Agent 源码分析

> **分析对象**：`/usr/local/LsmGitOpenSource/hermes-agent`（Nous Research 开源自主 AI 智能体）
> **分析日期**：2026-08-13
> **代码规模**：`hermes_state.py` 11,165 行 / `run_agent.py` 8,383 行 /
> `agent/conversation_loop.py` 7,763 行 / `agent/context_compressor.py` 7,386 行 /
> `agent/conversation_compression.py` 4,133 行 / `tools/delegate_tool.py` 4,356 行
> **定位**：持久记忆 + 自我进化成长的自主智能体

---

## 0. 覆盖度声明

本文档基于**完整读取**以下文件：`tools/memory_tool.py`(1248 行)、`agent/context_engine.py`(489)、
`agent/learning_mutations.py`(206)、`agent/learning_graph.py`(328)、`tools/todo_tool.py`(335)，
以及**定向精读** `agent/context_compressor.py` 的阈值计算与 token 估算段、
`agent/conversation_loop.py` 的函数清单与关键分支。

**未充分覆盖**：`hermes_state.py`(11K 行 SQLite 状态层)、`agent/curator.py`(2019)、
`agent/background_review.py`(1144)、`plugins/memory/` 下 9 个记忆后端插件的具体实现。
相关维度已在文中标注「未深入」。

---

## 1. 记忆管理系统

### 1.1 双文件持久记忆：MEMORY.md + USER.md

Hermes 把持久记忆拆成**两个正交的文件**（`tools/memory_tool.py:5-9`）：

| 文件 | 语义 | 默认字符上限 |
|---|---|---|
| `MEMORY.md` | Agent 自己的笔记与观察（环境事实、项目约定、工具怪癖、学到的东西） | 2200 |
| `USER.md` | Agent 对**用户**的认知（偏好、沟通风格、期望、工作习惯） | 1375 |

存储位置：`get_hermes_home() / "memories"`。

> **设计要点 1 — 动态解析 home 目录**（`memory_tool.py:49-55`）
> ```python
> def get_memory_dir() -> Path:
>     return get_hermes_home() / "memories"
> ```
> 注释明确记录了为什么**不能**用模块级常量：「旧的模块级常量在 import 时被缓存，
> 如果 profile 切换发生在首次 import 之后就会变成陈旧值」。
> 这是「配置读取必须晚绑定」的典范。

> **设计要点 2 — 用字符数而非 token 数做上限**（`memory_tool.py:17`）
> 原文注释：*"Character limits (not tokens) because char counts are model-independent."*
> 记忆文件要被任意模型消费，token 计数是模型相关的，字符数才是稳定契约。

### 1.2 条目分隔符：`§`

```python
ENTRY_DELIMITER = "\n§\n"
```

单个条目**可以多行**，用 section sign 分隔。相比 JSON/YAML，这个选择让
`MEMORY.md` 保持**人类可直接编辑**，同时解析成本极低。

### 1.3 ★ 冻结快照模式（Frozen Snapshot）—— 最重要的设计

这是 Hermes 记忆系统的**核心创新**（`memory_tool.py:11-14, 148-157`）：

```
MemoryStore 维护两份并行状态：
  ① _system_prompt_snapshot  ← 在 load_from_disk() 时冻结一次，
                                整个 session 内永不变更
  ② memory_entries/user_entries ← 活状态，被工具调用修改并落盘
```

**为什么这样设计**（原文注释）：

> *"Mid-session writes update files on disk immediately (durable) but do NOT
> change the system prompt — this preserves the prefix cache for the entire
> session. The snapshot refreshes on the next session start."*

即：**会话中途写记忆立即落盘（持久性），但不动 system prompt（保住 prefix cache）**。
工具返回值反映活状态，让模型知道写成功了；system prompt 保持稳定，
让 Anthropic/OpenAI 的 prompt cache 整个 session 命中。

这解决了一个真实矛盾：记忆要能随时更新（否则学不到东西），
但 system prompt 一变 prefix cache 全失效（成本暴涨）。
冻结快照把「持久性」与「缓存友好」解耦。

### 1.4 记忆写入的三个操作 + 短子串匹配

单一 `memory` 工具带 `action` 参数（`memory_tool.py:20-22`）：

| action | 语义 | 定位方式 |
|---|---|---|
| `add` | 追加条目 | — |
| `replace` | 替换条目 | **短唯一子串**匹配（非全文、非 ID） |
| `remove` | 删除条目 | **短唯一子串**匹配 |

> **设计要点 3 — 不用 ID 而用短唯一子串**
> ID 需要模型记住/回读，全文需要模型精确复现（易错）。
> 短唯一子串是「模型最容易正确产出」的定位方式 ——
> 这与本项目 §134 教训 (4)「enum 剔除优于事后报错」同源：
> **降低模型出错的可能性，优于在出错后报错让它重试**。

### 1.5 四层数据安全防御

Hermes 在记忆写入路径上堆了**四层**防御，每层都对应真实 issue：

#### (a) 注入/外泄扫描（`memory_tool.py:70-88`）

```python
def _scan_memory_content(content: str) -> Optional[str]:
    return _first_threat_message(content, scope="strict")
```

记忆用 **strict 作用域**（最广的模式集），理由写在注释里：

> *"memory enters the system prompt as a FROZEN snapshot, so a poisoned
> entry persists for the entire session and across sessions until
> explicitly removed."*

被污染的记忆条目会**跨 session 持续生效**，所以必须用最严的扫描。
命中时**只替换快照中的文本为占位符**，原条目仍留在磁盘上
（`_sanitize_entries_for_snapshot`，:243-250）—— 用户可以修，不是静默丢弃。

#### (b) 外部漂移检测（`_drift_error`，:91-118，issue #26045）

如果磁盘文件包含「无法往返通过记忆工具解析器」的内容
（可能是 patch 工具、shell append、手工编辑、或并发 session 写的），
**拒绝写入**，先存 `.bak.<ts>` 快照，并给出明确修复指引。

这防止了「工具序列化时静默丢弃别人追加的内容」。

#### (c) 不可读文件 ≠ 空文件（`_read_failed_error`，:128-145）

```python
_READ_FAILED = object()   # 哨兵，区别于「干净重载(None)」与「漂移备份路径(str)」
```

注释：*"A file that exists but cannot be read is NOT an empty store.
Reading it as `[]` and then persisting would rewrite the whole file
from an empty entry list — wiping the user's memory."*

文件存在但读失败（被锁/权限变更/编码损坏）时**拒绝写**，
而不是当成空列表然后把整个文件覆盖掉。

> 这一条与本项目 §20260811-08 的 P0（`redactLedgerFact` 无边界 ReplaceAll
> 打坏结构化前缀导致三个聚合字段恒为空）是**同一类缺陷的反面教材**：
> **「读失败」与「读到空」必须在类型层面可区分**。

#### (d) 跨平台文件锁（`_file_lock`，:280）

Unix 用 `fcntl`，Windows 回退 `msvcrt`，都不可用时降级为无锁
（import 时 try/except 三段式，:36-45）。

### 1.6 每轮失败上限：防止记忆写入卡死整轮

`_MAX_CONSOLIDATION_FAILURES_PER_TURN = 3`（`memory_tool.py:159-200`，issue #42405）

记忆满容量时的 consolidation 失败（overflow / zero-match），
在**同一轮内**超过 3 次后，工具返回**终止性**结果：

```python
{
  "success": False,
  "done": True,       # ← 关键：告诉模型别再重试了
  "error": "Memory consolidation failed N times this turn. Stop retrying
            memory calls — leave memory unchanged for now and continue
            with your reply to the user. The fact can be saved in a later turn."
}
```

注释点明设计原则：*"a failed memory side effect must never block the turn's reply"*
—— **记忆写入是副作用，绝不能阻塞对用户的回复**。

### 1.7 记忆提供者插件体系（未深入）

`plugins/memory/` 下有 9 个后端：`byterover` / `hindsight` / `holographic` /
`honcho` / `mem0` / `openviking` / `retaindb` / `supermemory` + `query_rewrite.py`。

`agent/context_engine.py:31-53` 定义了记忆提供者上下文的**出口边界处理**：

```python
MEMORY_CONTEXT_MAX_CHARS = 6_000
_MEMORY_CONTEXT_HEAD_CHARS = 4_000
_MEMORY_CONTEXT_TAIL_CHARS = 1_500
_MEMORY_CONTEXT_TRUNCATION_MARKER = "\n...[memory provider context truncated]...\n"

def sanitize_memory_context(memory_context: str) -> str:
    sanitized = redact_sensitive_text(..., force=True, redact_url_credentials=True)
    if len(sanitized) <= MEMORY_CONTEXT_MAX_CHARS:
        return sanitized
    return sanitized[:4000] + MARKER + sanitized[-1500:]
```

**头尾保留 + 中间截断 + 显式标记** —— 而非简单尾截断。
外部记忆后端返回的内容先脱敏（强制 + URL 凭据脱敏）再入 LLM。

---

## 2. Context 管理与 LLM 调用架构

### 2.1 ★ 可插拔 Context 引擎（ContextEngine ABC）

Hermes 把上下文管理抽象成**可替换的引擎**（`agent/context_engine.py`）：

```
配置驱动：config.yaml → context.engine（默认 "compressor"）
插件目录：plugins/context_engine/<name>/
同时只有一个引擎生效
```

引擎职责（文件头 doc）：
1. 决定何时该压缩
2. 执行压缩（摘要 / DAG 构建 / 等）
3. 可选地暴露 Agent 能调的工具（如 `lcm_grep`）
4. 追踪来自 API 响应的 token 使用量

### 2.2 六阶段生命周期

```
1. 引擎实例化并注册（plugin register() 或默认）
2. on_session_start()      ← 会话开始
3. update_from_response()  ← 每次 API 响应后，带 usage 数据
4. should_compress()       ← 每轮后检查
5. compress()              ← should_compress() 为真时
6. on_session_end()        ← 真实会话边界（CLI 退出 / /reset / gateway 过期）
                             —— 注释明确：NOT per-turn
```

### 2.3 ★★ `compress()` 与 `select_context()` 的正交分离

这是本次分析中**最有借鉴价值**的架构洞察（`context_engine.py:213-240`）：

```
compress()       : 上下文太长了      -> 让它变短
select_context() : 这一轮属于别的上下文 -> 换成那个
```

原文说明了不做这个分离的后果：

> *"Without this hook, engines that need per-turn access to the message
> list have to force `should_compress()` to return `True` so that
> `compress()` is invoked every turn purely as a callback — which
> conflates selection with compression and degrades behaviour when the
> engine's backend is unavailable."*

即：如果只有 `compress()`，需要每轮介入的引擎就只能**谎报「需要压缩」**，
把「选择」伪装成「压缩」，后端不可用时行为退化。

`select_context()` 在**每轮请求组装后、派发给 provider 前**调用，
与 `should_compress()` 完全独立，用于检索 / 主题路由 / 角色分支切换。

### 2.4 廉价工具结果剪枝（独立于压缩）

`prune_tool_results_only()`（`context_engine.py:189-212`）：

> *"Deterministically trim old tool-result payloads without an LLM call.
> Runs on a low, cost-oriented trigger independent of `should_compress`
> so large-window engines can reclaim re-sent tool output long before
> full compaction would fire."*

**不调 LLM 的确定性剪枝**，用独立的低成本触发器，
让大窗口模型能在「完整压缩」触发之前很久就回收重发的工具输出。

默认实现是**安全 no-op**（返回原列表 + 0），理由写在注释里：

> *"Engines that don't implement a cheap prune — and any engine that
> predates this hook — inherit this default, so the agent loop's
> post-tool-call prune path never raises `AttributeError` on them."*

**新增可选 hook 必须有安全默认实现**，让旧引擎不会崩。

### 2.5 阈值计算：四个真实 issue 堆出来的边界处理

`_compute_threshold_tokens()`（`context_compressor.py:2472-2512`）
是本次分析中**代码质量最高**的一段，每个分支都有 issue 编号：

```python
effective_window = context_length - (max_tokens or 0)   # ← issue #43547
if effective_window <= 0:
    effective_window = context_length
pct_value = int(effective_window * threshold_percent)
floored = max(pct_value, MINIMUM_CONTEXT_LENGTH)
if effective_window > 0 and floored >= effective_window:  # ← issue #14690
    return max(1, min(int(effective_window * _MIN_CTX_TRIGGER_RATIO),  # 85%
                      effective_window - 1))
return floored
```

**三个非平凡洞察**：

1. **必须减去 `max_tokens`**（#43547）——
   provider 从同一个窗口里预留输出空间，可用**输入**预算是
   `context_length - max_tokens`。`max_tokens=65536` 的自定义 provider 上，
   按完整窗口算阈值会让 session 在压缩触发前就撞上 provider 400。

2. **地板值在小窗口上会退化**（#14690）——
   `MINIMUM_CONTEXT_LENGTH` 地板是为了让大窗口模型不要在 50% 就过早压缩，
   但对 64K 本地模型，`max(0.5*64000, 64000) == 64000`
   使阈值**等于整个窗口**，自动压缩永远无法触发
   （provider 在使用率到 100% 前就拒绝请求了）。
   修复：地板值 ≥ 有效窗口时，改在 **85%** 触发。

3. **`max_tokens=None` 保守假设无预留**（完整窗口）。

### 2.6 Token 估算

`estimate_tokens_rough()` + 显式开销补偿（`context_compressor.py:1045-1059`）：

```python
tokens = estimate_tokens_rough(content) + 10   # +10 补 role/key 开销
...
tokens += estimate_tokens_rough(str(tc))       # tool_calls 单独计
```

注释指出 preflight 估算器与实际计算必须**看到相同的消息形状**（:1024），
以及 base64 信封需要单独处理（:1059，#73298）。

### 2.7 Provider 层容错（`conversation_loop.py` 函数清单可见）

从函数命名可见容错的覆盖面：

| 函数 | 处理的问题 |
|---|---|
| `_is_stale_copilot_credential_error` | Copilot 凭据过期 |
| `_image_error_max_dimension` | 图片超尺寸 |
| `_ollama_context_limit_error` | Ollama 上下文超限 |
| `_nous_entitlement_message` / `_billing_or_entitlement_message` | 计费/权限 |
| `_canonicalize_tool_call_arguments` / `_canonicalize_api_tool_calls` | 工具调用参数归一化 |
| `_invalid_tool_name_error_content` | 模型产出空/非法结构化调用（注释点名 mimo/nemotron 类模型） |
| `_content_policy_blocked_result` | 内容策略拦截 |
| `_compression_deferred_result` | 压缩延后 |
| `_rewrite_system_content_blocks` / `_sync_failover_system_message` | failover 时同步 system |
| `_redecorate_prompt_cache_for_provider` | **切 provider 时重新装饰 prompt cache** |

> `_redecorate_prompt_cache_for_provider` 特别值得注意：
> prompt cache 的 breakpoint 标记是 **provider 特定**的，
> failover 到另一个 provider 时必须重新装饰，否则缓存标记非法。

---

## 3. 意图识别与任务分解

### 3.1 ★ Todo 工具：穿越压缩的任务列表

`tools/todo_tool.py` 的设计核心（文件头 doc）：

> *"Provides an in-memory task list the agent uses to decompose complex tasks,
> track progress, and maintain focus across long conversations. The state
> lives on the AIAgent instance (one per session) and **is re-injected into
> the conversation after context compression events**."*

**关键机制**：任务列表在**上下文压缩后被重新注入**。

```python
TODO_INJECTION_HEADER = (
    "[Your active task list was preserved across context compression]"
)
```

注释说明这个 header 是**稳定契约**：
*"ContextCompressor uses this stable header to distinguish the synthetic
post-compaction row from a real user."*

即压缩器靠这个固定 header 区分「合成的压缩后行」与「真实用户消息」。

### 3.2 Todo 的边界防护

```python
MAX_TODO_CONTENT_CHARS = 4000
MAX_TODO_ITEMS = 256
MAX_TODO_RESULT_CHARS = 512_000   # 历史水合时单个 payload 上限
```

理由（注释）：

> *"The todo list is a planning aid the model re-reads after every
> context-compression event, so unbounded item content or count defeats
> the compression it rides through."*

**任务列表自己不能变成压缩的负担** —— 它每次压缩后都要重新注入，
无界的条目内容/数量会抵消它所搭乘的那次压缩。

`MAX_TODO_RESULT_CHARS` 针对的是另一类威胁：
gateway/API server 会重放调用方提供的对话历史来重建 store，
**超大的伪造结果必须在解析并重新注入之前被丢弃**。

### 3.3 单工具读写 + 行为指引在 schema 里

```
- Single `todo` tool: provide `todos` param to write, omit to read
- Every call returns the full current list
- No system prompt mutation, no tool response modification
- Behavioral guidance lives entirely in the tool schema description
```

**行为指引完全放在工具 schema 的 description 里**，不改 system prompt。
这与 §1.3 的冻结快照一致：**能不动 system prompt 就不动**（prefix cache）。

状态机：`pending` / `in_progress` / `completed` / `cancelled`。
条目**有序**，列表位置即优先级。

### 3.4 任务委派（`tools/delegate_tool.py`，4356 行，未深入）

规模远超 todo 工具，配套 `tools/delegation_live_log.py` +
`agent/delegation_context.py`，说明 Hermes 有完整的**子 Agent 委派**体系。

---

## 4. 自我进化与成长机制

### 4.1 ★ 学习图谱（Journey Graph）—— 让学习可见

`agent/learning_graph.py` 文件头：

> *"Assemble the 'learning made visible' graph for desktop."*

图谱**刻意限定**在「用户真正随时间学到的东西」：

| 节点类型 | 来源 | 稳定 ID |
|---|---|---|
| 技能 | 非基础的、学到/profile 的技能（Agent 创建或使用过的） | 技能名（如 `"debugging-hermes-desktop"`）|
| 记忆 | `MEMORY.md` / `USER.md` 的条目，**作为一等公民节点** | `memory:<source>:<index>` |

**边的两个来源**：
- 技能↔技能：声明式 `related_skills` frontmatter
- 记忆↔技能：**词法重叠**推导

设计目的（原文）：让图谱能回答
*"which learned skills are connected to the things I remember?"*
（哪些学到的技能与我记住的事情相关？）

`SkillNode` 数据结构携带成长信号：

```python
@dataclass
class SkillNode:
    name: str
    category: str
    source: str = "profile"
    timestamp: Optional[int] = None
    use_count: int = 0          # ← 使用次数
    state: str = "active"
    created_by: Optional[str] = None
    pinned: bool = False
    related: list[str] = field(default_factory=list)
```

### 4.2 用户可编辑/删除学到的东西

`agent/learning_mutations.py` 把节点 ID 映射回磁盘位置并执行变更，
被 CLI（`hermes journey delete|edit`）、TUI `/journey` overlay、
desktop GUI REST **三个前端共用**。

**删除语义按类型分化**：
- 删技能 = **归档**（可通过 `hermes curator restore` 恢复）
- 删记忆 = 重写文件

> **设计要点 4 — 陈旧 ID 显式报错**（`learning_mutations.py:56-62`）
> ```python
> if cards[global_index].get("source") != source:
>     raise ValueError("memory node id is stale — refresh the graph")
> ```
> 索引型 ID 天然会因为并发编辑而失效，
> Hermes 在越界/来源不匹配时**显式抛「ID 陈旧，请刷新图谱」**，
> 而不是静默改错条目。

### 4.3 记忆解析器复用（避免索引漂移）

```python
chunks = MemoryStore._read_file(path)   # ← 复用记忆工具的同一个解析器
```

注释点明理由：*"Entries come from `MemoryStore._read_file` — the same parser
the memory tool uses — so journey indices stay aligned with what the graph renders."*

**图谱索引与工具解析必须用同一个解析器**，否则索引会错位。

### 4.4 Curator + 背景审查（未深入）

- `agent/curator.py`(2019 行) + `agent/curator_backup.py` —— 技能策展与归档恢复
- `agent/background_review.py`(1144 行) —— 背景审查（推测是异步自我复盘）
- `skills/` + `optional-skills/` —— 20+ 分类的技能库
  （`autonomous-ai-agents` / `software-development` / `research` / …）

---

## 5. 整体架构与模块划分

### 5.1 分层

```
┌─────────────────────────────────────────────────────┐
│ 入口层  cli.py / run_agent.py / batch_runner.py     │
│         mcp_serve.py / gateway/ / acp_adapter/       │
├─────────────────────────────────────────────────────┤
│ 循环层  agent/conversation_loop.py (7763)            │
│         ├─ Provider 适配 (anthropic/bedrock/codex…)  │
│         ├─ failover + 计费/权限 + 错误分类           │
│         └─ prompt cache 装饰（provider 特定）        │
├─────────────────────────────────────────────────────┤
│ 上下文层 agent/context_engine.py (ABC, 可插拔)       │
│         ├─ context_compressor.py (7386, 默认实现)    │
│         ├─ conversation_compression.py (4133)        │
│         └─ plugins/context_engine/<name>/            │
├─────────────────────────────────────────────────────┤
│ 记忆层  tools/memory_tool.py (MEMORY.md + USER.md)   │
│         └─ plugins/memory/ (9 个外部后端)            │
├─────────────────────────────────────────────────────┤
│ 规划层  tools/todo_tool.py + tools/delegate_tool.py  │
├─────────────────────────────────────────────────────┤
│ 成长层  agent/learning_graph.py + learning_mutations │
│         + curator.py + background_review.py          │
├─────────────────────────────────────────────────────┤
│ 状态层  hermes_state.py (11165, SQLite + FTS5 CJK)   │
└─────────────────────────────────────────────────────┘
```

### 5.2 反复出现的设计模式

| # | 模式 | 出现位置 |
|---|---|---|
| 1 | **冻结快照** —— 持久层可变、prompt 层不可变 | `MemoryStore._system_prompt_snapshot` |
| 2 | **ABC + 安全默认实现** —— 新 hook 不破坏旧插件 | `ContextEngine.prune_tool_results_only` |
| 3 | **正交动词分离** —— 不要用一个 hook 冒充另一个 | `compress()` vs `select_context()` |
| 4 | **哨兵对象区分失败模式** —— 「读失败」≠「读到空」 | `_READ_FAILED = object()` |
| 5 | **每轮失败上限 + `done:True`** —— 副作用不阻塞主流程 | `_MAX_CONSOLIDATION_FAILURES_PER_TURN` |
| 6 | **拒绝写入优于静默丢失** —— 漂移/不可读时中止 | `_drift_error` / `_read_failed_error` |
| 7 | **单一解析器** —— 多消费方共用避免索引漂移 | `MemoryStore._read_file` |
| 8 | **行为指引放 schema** —— 不动 system prompt | `todo` / `memory` 工具 |
| 9 | **issue 编号背书每个边界分支** | `_compute_threshold_tokens` 四处 |
| 10 | **头尾保留 + 显式截断标记** | `sanitize_memory_context` |

### 5.3 与其他 Agent 框架的差异

| 维度 | 常见做法 | Hermes |
|---|---|---|
| 记忆存储 | 单一 memory 文件 / 向量库 | **MEMORY.md（自我）+ USER.md（用户）双文件正交** |
| 记忆上限 | token 数 | **字符数**（模型无关） |
| 记忆更新 | 直接改 system prompt | **冻结快照**，落盘即时 + prompt 稳定 |
| 记忆定位 | ID 或全文 | **短唯一子串** |
| 上下文管理 | 硬编码压缩策略 | **可插拔引擎 ABC + 配置驱动** |
| 压缩触发 | 固定百分比 | **减 max_tokens + 小窗口退化保护（85%）** |
| 工具结果 | 随压缩一起处理 | **独立的无 LLM 确定性剪枝** |
| 任务列表 | 存在对话历史里（压缩即丢） | **穿越压缩重新注入 + 稳定 header** |
| 学习成果 | 不可见 | **Journey 图谱 + 用户可编辑/归档** |
| 记忆安全 | 无 | **strict 注入扫描 + 漂移检测 + 不可读保护 + 文件锁** |

---

## 6. 可借鉴的设计亮点（按价值排序）

### ★★★ 1. 冻结快照模式
记忆随时可写（落盘即时生效），但 system prompt 整个 session 稳定。
**解决了「持久性 vs prefix cache」的真实矛盾**，任何用 prompt cache 的
Agent 都应该采用。

### ★★★ 2. `compress()` 与 `select_context()` 正交
「上下文太长→压缩」与「这轮该用别的上下文→切换」是两件事。
合并成一个 hook 会迫使实现方谎报状态。

### ★★★ 3. Todo 穿越压缩重新注入
任务列表是**规划状态**，不是对话内容。它必须独立于压缩存活，
并有稳定 header 让压缩器识别。同时**自己也要有上限**，
否则会抵消所搭乘的那次压缩。

### ★★ 4. pre-flight 阈值必须减去 max_tokens
provider 从同一窗口预留输出空间。按完整窗口算阈值，
会在压缩触发前撞上 provider 400。

### ★★ 5. 「读失败」与「读到空」必须类型可分
用哨兵对象（而非 `None`/`[]`）区分，否则一次瞬时读失败
会把整个持久存储覆盖成空。

### ★★ 6. 副作用失败不阻塞主流程 + `done:True` 终止信号
记忆写入失败超过 N 次后，返回明确的「别再试了，继续回复用户」，
防止工具循环吃掉整轮预算。

### ★★ 7. 新增可选 hook 必须有安全默认实现
`prune_tool_results_only` 默认 no-op，让旧引擎不会 `AttributeError`。

### ★ 8. 字符数而非 token 数做持久层上限
持久文件被任意模型消费，字符数才是稳定契约。

### ★ 9. 短唯一子串优于 ID/全文
选择「模型最容易正确产出」的定位方式。

### ★ 10. 学习可视化 + 用户可编辑
把「学到了什么」做成一等公民的图谱，用户能删/改/归档，
删技能是归档（可恢复）而非硬删。

---

## 7. 对本项目（LsmAgentGame 狼人杀 Agent）的直接启示

对照 CLAUDE.md 中已记录的教训，Hermes 有几处**正面印证**与**可补短板**：

### 正面印证（本项目已做对的）

| 本项目机制 | Hermes 对应 | 一致性 |
|---|---|---|
| §134 (4) enum 剔除优于事后报错 | 短唯一子串定位 | ✅ 同源思想 |
| §20260812-04 (4) 降级必留可观测标记 | `[memory provider context truncated]` | ✅ 一致 |
| §20260812-04 U3 prompt cache 只打 1 breakpoint | 冻结快照保 prefix cache | ✅ 同目标 |
| §131 MEMORY.md 4 段固定标题 | MEMORY.md + USER.md | ⚠️ Hermes 分两文件更正交 |

### 可补短板（Hermes 有、本项目缺）

| 缺口 | Hermes 做法 | 对应本项目问题 |
|---|---|---|
| **无 pre-flight token 检查** | `_compute_threshold_tokens` 减 max_tokens + 小窗口保护 | 现状：等 400 后才 `PruneByBytesAggressive` |
| **Token 全靠字节近似** | `estimate_tokens_rough` + 显式 +10 开销 | 现状：中文 UTF-8 误差 3 倍 |
| **记忆中途更新会动 prompt** | 冻结快照 | 现状：`InjectBlockWithBudget` 每轮重算 |
| **无独立的工具结果剪枝** | `prune_tool_results_only`（无 LLM） | 现状：只有整体压缩 |
| **Context 组装无抽象层** | `ContextEngine` ABC | 现状：91 处 `s +=` |
| **规划状态不穿越压缩** | Todo 重新注入 + 稳定 header | 现状：无等价机制 |
| **「字段声明了但无 setter」反复复发** | ABC `@abstractmethod` 强制实现 | 现状：§130 已复发 5 次 |

---

## 8. 参考文件索引

| 主题 | 文件 | 行数 |
|---|---|---|
| 记忆工具（MEMORY.md/USER.md） | `tools/memory_tool.py` | 1248 |
| Context 引擎 ABC | `agent/context_engine.py` | 489 |
| 默认压缩器 | `agent/context_compressor.py` | 7386 |
| 对话压缩 | `agent/conversation_compression.py` | 4133 |
| 主对话循环 | `agent/conversation_loop.py` | 7763 |
| 轨迹压缩 | `trajectory_compressor.py` | 1598 |
| 任务列表 | `tools/todo_tool.py` | 335 |
| 任务委派 | `tools/delegate_tool.py` | 4356 |
| 学习图谱 | `agent/learning_graph.py` | 328 |
| 学习变更 | `agent/learning_mutations.py` | 206 |
| 技能策展 | `agent/curator.py` | 2019 |
| 背景审查 | `agent/background_review.py` | 1144 |
| 状态层（SQLite） | `hermes_state.py` | 11165 |
| Agent 初始化 | `agent/agent_init.py` | 2858 |
