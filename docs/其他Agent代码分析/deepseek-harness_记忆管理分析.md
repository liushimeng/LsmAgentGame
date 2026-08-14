# DeepSeek Harness — 记忆管理子系统分析

> **分析对象**：DeepSeek Harness（`@deepseek-ai/dsh-*`，开源 agent harness）
> **版本**：`@deepseek-ai/dsh-root` 0.1.0-rc.5（`/usr/local/LsmGitOpenSource/deepseek-harness/package.json`）
> **首行 commit**：`47f943859b Merge pull request #2519 from deepseek-harness/feat/npm-public`
> **分析日期**：2026-08-14
> **源码路径**：`/usr/local/LsmGitOpenSource/deepseek-harness`
> **聚焦范围**：所有「让 LLM 记住/忘掉/找回/重新对齐」上下文的子系统
>
> 分析定位：本文档不是 DeepSeek Harness 的用户手册，而是面向
> **LsmAgentGame（狼人杀 Agent 平台）工程组**的源码考古——
> 所有结论都给出 `文件路径:行号` 与原始代码片段，便于移植或借鉴。
> 涉及子包 7 个，共 ~50 个源文件（含测试 ~80 个）。

---

## 1. 概览与包结构图

### 1.1 一句话定位

DeepSeek Harness 把 agent 的"记忆"拆成 **5 条独立但咬合的管道**：
**指令上下文（基线注入）→ 会话日志（事件溯源）→ 投影/Checkpoint（read-model）→ 压缩（drop 高负载）→ 外置（spill 大负载）→ 检索（corpus-level search）**。
所有持久化/压缩/外置最终落到一份 **append-only session event log**，每个 session 一个 surface fold（一棵有序的"模型可见节点"树），所有 read-model 都是从这份 log 派生出来的纯函数 fold。

### 1.2 包结构图

```
packages/
├── context/                         ← "记得该记得的指令"
│   ├── agent-instructions/          ← AGENTS.md 多层加载、cache、reconcile、注入 inbox
│   ├── session-reference/           ← 跨 session 引用 resolver（带 budget）
│   ├── time-context/  tmux-context/
│
├── session/                         ← "记得发生过什么"
│   ├── session-persistence/         ← Service Definition + coordinator（write-behind 调度）
│   ├── session-persistence-jsonl/   ← JSONL 实现（zstd 压缩 + frame-aware crash-repair）
│   ├── session-persistence-sqlite/  ← SQLite 实现（WAL + 行级 + format-version gate）
│   ├── session-projection/          ← registry：每 unit `init/apply/view` 纯函数
│   ├── session-projection-cache/    ← unit checkpoint（ver+seq+val, fail-soft）
│   ├── session-checkpoint-policy/   ← 持久化时机：model stream / tool exec / step pre
│   ├── session-stats/  session-title/  session-telemetry/  session-telemetry-otel/
│
├── compaction/                      ← "记得但要忘掉点"
│   ├── compaction/                  ← Service Definition（trigger/region/compactNow）
│   ├── compaction-basic/            ← 默认实现：pressure + 上下文溢出恢复
│   ├── compaction-tool-result-pruner/  ← 无 LLM 的 head/middle/tail 剪枝
│   ├── command-compact/             ← `/compact` 命令 Consumer
│
├── spill/                           ← "把太大的东西丢到磁盘上"
│   ├── spill/                       ← Service Definition：saveText → SpillRef
│   ├── spill-local/                 ← session-scoped 0700 文件 + 路径安全编码
│   ├── spill-policy/                ← tools/post-execute 边界 = 何时 spill + preview
│
├── session-query/                   ← "从其他 session 找回"
│   ├── session-query/               ← Service Definition（live-preferred corpus）
│   ├── session-query-sqlite/        ← FTS5 + reconciliation generation
│   ├── session-log-export/          ← Web 导出 UI
│   ├── tool-session-query/          ← 5 个 model-facing 工具（search/read/trace/...）
│
├── goal/                            ← "长期目标作为独立 projection"
│   ├── goal/                        ← event-sourced 全快照 fold
│   ├── goal-round-driver/           ← agent 内的 round 调度（racing-fence）
│   ├── command-goal/  tool-goal/
│
├── todo/                            ← "当前任务作为独立 projection"
│   └── tool-todo/                   ← whole-list 替换 + projection `todos`
│
└── util/output-retention/           ← 字节级 head/head-tail/tail（spill 预览的底层）
```

### 1.3 关键设计原则（设计文档直引）

- **事件溯源 + 投影模型**：`session-projection/src/index.ts:1-18` 注释明示
  "**whole-value event rule**：`a state-carrying log event MUST carry the complete post-change state, never a bare delta`" —— 这是它和经典 event-sourcing 的关键分歧点。
- **Capability seam**：每个能力包都有一个 `<X>` Service Definition（抽象类）+ 多个 provider 子包。所有 plumbing 都通过 cordis 的 `inject` 装饰器声明依赖。
- **Plain-JSON contract**：投影 unit 状态必须可 plain JSON 序列化（`session-projection/src/index.ts:39-41`），这是持久化 checkpoint 的硬约束。

---

## 2. 上下文指令加载（AGENTS.md 体系）

### 2.1 模块清单

```
packages/context/agent-instructions/src/
├── index.ts        入口（plugin apply + inbox sync）
├── files.ts        文件探测、祖先链、bounded read、dirmtime 去重
├── render.ts       byte-budgeted 渲染（binary search + 截断）
├── state.ts        durable 状态 + reconcile 算法 + versioned cache
├── config.ts       default：maxBytes + maxSourceBytes + 文件名候选列表
├── digest.ts       SHA-1 content / trimmedDigest
```

### 2.2 三层发现（`files.ts:267-308`）

```
discoverInstructionFiles(options):
  userGlobal      = <DSH_HOME>/AGENTS.md               ← 单一 user-global scope
  projectRoot     = walk-up(cwd, projectRootMarkers)   ← 默认 .git
  candidateScopes = ancestorChain(projectRoot, cwd)    ← root → cwd 所有目录
                  × [instructionFileCandidates, localInstructionFileCandidates]
                  默认 [AGENTS.md, CLAUDE.md] 与 [AGENTS.local.md, CLAUDE.local.md]
```

每一层做：
- `statFile` 三态探针：`present` / `absent` / `unavailable`
  （`files.ts:74-78` 的 `ScopeInstructionProbe`）——"unavailable"是单次 provider 失败，
  "absent"是真正不存在；二者语义完全不同。
- per-directory **trimmed-digest 去重**：`dedupInstructionFilesByDirectory`
  （`files.ts:368-384`）—— 同目录多个候选文件若 trim 后内容相同，折叠成最早那个；
  这让 `AGENTS.md` 软链到 `CLAUDE.md` 不会双渲染。

### 2.3 byte-budgeted 渲染（`render.ts:249-272`）

预算用二进制搜索收敛 —— 不是简单切片：

```ts
function truncateToFit(file, includedFiles, maxBytes, omitted, style) {
  let low = 0, high = originalBytes, best = withTruncatedContent(file, 0)
  while (low <= high) {
    const mid = floor((low + high) / 2)
    const candidate = withTruncatedContent(file, mid)
    const text = buildInstructionText([...includedFiles, candidate], maxBytes, omitted, truncated, style)
    if (byteLength(text) <= maxBytes) { best = candidate; low = mid + 1 }
    else high = mid - 1
  }
  return best
}
```

外加 `truncateUtf8`（`render.ts:69-79`）做 **UTF-8 码点安全截断**：跳过 `10xxxxxx` 续字节，避免半字符。这是写"自动注入文本"时极易踩的坑。

### 2.4 reconcile 算法（`state.ts:246-433`）

agent 启动加载 baseline（`loadBaselineInstructionSet`），之后
**文件 touch 事件**触发动态增量 reconcile。算法核心：

```
reconcileInstructionContext(agent, resolved, versionCache, fileSystem, options):
  // 1. 收集需检查的 scope
  scopes = (baseline scopes, 当前 pending, 当前 visible, descendant of touchedPaths)
  // 2. per-directory probe（unavailable 整组放弃而非单点放弃）
  for [directory, scopes] in scopesByDirectory:
    priorVersions = snapshot
    for scope in scopes:
      probe = provider.probe(scope)
      switch probe:
        case 'unavailable':
          // 整组作废 —— 同一目录候选属于同一权威组，任一观察不到则保留 last-good
          restore priorVersions, clear cache, break
        case 'absent':
          if previous !== 'remove': pushRemoval(scope, previous.path)
          continue
        case 'present':
          if versionCache matches (path+version+digest): continue  // 完全无变化
          content = readBounded(...)
          trimmedDigest = sha1(trim(content))
          if registerKeptTrimmed(directory, trimmedDigest):         // 同目录更早出现的重复
            pushRemoval(...)
            continue
          items.push({ change: 'set'|'replace', file })
          versionUpdates.push({ change, state: nextVersion })
  if items.length === 0: return undefined                            // 真的没变
  rendered = renderInstructionChanges(items, maxBytes)               // 在预算内二次筛选
  if rendered.text.length === 0 || rendered.changes.length === 0:
    return undefined                                                  // notice-only 不产出
```

不变量：
1. **同一目录内多个候选视为一个权威组**：`probeUnavailable` 整组保留 last-good，
   避免一个候选暂时不可读导致整个目录被错误"remove"。
2. **`unavailable !== absent`**：absent 走 `pushRemoval`，unavailable 走"保持上一态"。
3. **版本 cache 用 trimmed-digest**（`InstructionVersionState.trimmedDigest`，
   `state.ts:62`）：trim 后内容相同则判为同一文件，避免末尾换行差异导致 cache miss。
4. **`<system-reminder>` 帧包裹**（`render.ts:10-11` + 242 行）：provider 主动 bake 帧
   到 content 里，session surface 不再二次包裹——"framing is caller-owned"。

### 2.5 注入到 prompt（`index.ts:80-367`）

通过 cordis 钩 `agent/pre-step` + `tools/result`：

- `tools/result` 拦截 `read/write/edit` 三类工具对文件的触碰，把 `{ agent, path }`
  推到 `executionTouches` map；嵌套工具调用把 touches 沿 `exec.parent` 链向根冒泡。
- `step/start`/`step/end` 决定 **批投影时机**：步骤进行中累积 touches，结束时
  `queueProjection` 串行提交（`projectionTails` WeakMap 防止重叠）。
- `agent/pre-step` **最终时刻同步 inbox**：先 `waitForProjections` 等所有尾部投影落地，
  再 `compose` 一次（baseline + reconciliation），然后：
  - **若 desired 为 `undefined`**：从 inbox 移除旧的 workspace context 消息。
  - **若 desired 已存在 surface 或 inbox 中**：dedup（`sameContextPayload` 用 `isDeepStrictEqual`）。
  - 否则 `prepend` 到 `next-step`（`index.ts:243-247`）。

### 2.6 数据结构要点

```ts
// 单一事实来源：scope-keyed 状态变化
AgentInstructionChange = { action: 'set' | 'replace' | 'remove'; scope: string; path: string; digest?: string }
AgentInstructionSource = { kind: 'agent-instructions'; form: 'instructions';
                          baseline?: true; baselineIdentity?: string; changes: Change[] }

// 一次 system-reminder 块被 fold 为一个 UserMessage，content 中 text 块带 system-reminder 帧
workspaceContextMessage(text) = createUserMessage({ content: [{ type: 'text', text }], source: { kind: 'plugin', plugin: name } })

// version cache 用 WeakMap<Session>，session 终止时整体 GC
InstructionVersionCache = WeakMap<Session, Map<scope, InstructionVersionState>>
```

**易借鉴点**：
- 不直接读文件就渲染——probe + cache + reconcile 是三段式。
- per-directory **权威组**（unavailable 整组作废）是一个非常有用的语义。
- `<system-reminder>` 帧由 producer 自己 bake（frame 责任唯一）。
- **trimmed-digest** 比 raw digest 更适合做"内容相同就 cache hit"的判据。

---

## 3. 会话日志与持久化

### 3.1 Service Definition（`session-persistence/src/index.ts:84-241`）

```ts
abstract class SessionPersistence extends Service {
  abstract locate(meta): SessionLocation | undefined                // 后端解析本地路径
  abstract readonly supportsRawArtifacts: boolean                   // 是否能 readRaw 一把原文吐回
  abstract create(meta): Promise<void>                              // 注册 metadata；lazy 物理写
  abstract append(id, events): Promise<void>                        // 严格 contiguous-seq
  async prepare(id): SessionPreparation                             // resume 用：复制 events 给 sessions
  abstract load(id): { meta, events }                               // 冷启动 + 尾巴修复
  abstract inspect(id): { meta, events }                           // 只读，不入 in-memory store
  abstract readFrom(id, fromSeq): { meta, events }                  // 投影 cache 续 fold 用
  abstract list(): SessionHeader[]                                  // 不解析事件流
  abstract listSnapshots(): { header, revision }[]                  // 轻量变更 token
}
```

设计关键：
- `load` vs `inspect` 的区别：**load 做崩溃修复**（自动补齐 `turn/end`，
  `coordinator.ts` 调用 `interruptedTurnClosers`），**inspect 不动**。
- `append` 强制 **contiguous-seq**：第一条事件的 seq 必须等于已存 next-seq，否则拒绝
  （`index.ts:140-143`）。这是事件溯源一致性的基石。
- `revision` 是 backend 自身的不透明 token（`SessionPersistenceRevision = Branded<string>`），
  解决 backend-local counter 跨 store 不可比的问题。

### 3.2 写入路径（`coordinator.ts` + `write-behind.ts`）

**写批次合并**用 `SessionWriteBehind`（`write-behind.ts`）：

```
enqueue(event) → push 到 pending → 第一次入队时启动 timer(maxDelayMs)
deadline 到 / flush() 调用 → 把 pending splice(0) 出来调 write(events)
write 失败 → 把 batch 拼回 pending 前 + pause 自动 timer + 报告 background failure
flush() 串行 drain barrier 直到 pending 空，再 resolve（"close admission in the same job"）
```

要点：
- **deadline 内合并，deadline 外落盘**：默认 `DEFAULT_WRITE_BATCH_MAX_DELAY_MS = 200`。
- **失败 batch 回滚到 pending 前**：保留了"在原地重试"的语义，不会丢事件。
- `flush()` 返回的 barrier promise 是一次性的，"A later enqueue therefore starts its own
  automatic window instead of being stranded behind a settled barrier"
  （`write-behind.ts:148-150`）——非常容易写错的细节。

### 3.3 物理层：JSONL + Zstd

`session-persistence-jsonl/src/format.ts` + `zstd.ts`：
- 物理编码：`.jsonl.zstd`（zstd 帧）or `.jsonl`（明文）。
- **zstd 是 concatenated-frame container**，每个 frame 头部有 magic number（`0xFD2FB528`），
  扫描可定位完整 frame 与不完整 frame 起点（`zstd.ts:54-87`）。**崩溃时只丢最后一帧**，
  这就是 `tornMarker` 机制——`scanZstdFrames` 返回 `tornStart`，`commitRepair` 用它截断。
- 头部是 `type: 'session'` 标记的第一行 JSON 对象，与事件行严格区分。
- `encodeSegment` 把不可信 session id / suggestedName 安全编码为单段路径
  （`spill-local/store.ts:48-63` 与 JSONL 镜像实现）——把所有非 `[A-Za-z0-9._-]` 字符
  转义成 `~XXXX`，并对 `.`、`..`、`""` 特殊编码，杜绝路径穿越。

### 3.4 物理层：SQLite

`session-persistence-sqlite/src/schema.ts`：
- `SCHEMA_VERSION = 15`，变更即拒绝旧版本（`schema.ts:22`，不可兼容迁移——注释明确
  "no compatibility promise"）。
- `application_id = 0x44534850` 防与无关 DB 误用。
- 每个事件一行：`{ seq, type, time, data(JSON), source_event_seqs(JSON), surface_op(JSON), ignorable }`。
- `incarnation`（行存在即"已 materialize"的信号）+ `revision`（单调递增变更 token）。
- WAL 默认，rollback 模式（`delete/truncate/persist`）给不支持 mmap 的网络盘。
- `BEGIN IMMEDIATE` + `PRAGMA user_version` 在持锁态校验 schema 版本与 application_id。

### 3.5 chunk 打包（`core/session/src/chunk-rows.ts`）

把连续的 `assistant/chunk` 事件打包成单一存储行：

```ts
// 注释明示 "56x size 缩小 on a real DeepSeek session"
type ChunkRow = 
  | { type: 'text-chunks';        seq0, time0, data: { turn, step, index, dt, texts } }
  | { type: 'reasoning-chunks';   seq0, time0, data: { turn, step, index, dt, texts } }
  | { type: 'tool-call-chunks';   seq0, time0, data: { turn, step, index, dt, id, name?, args } }
```

- **dt 数组**存相邻事件的时间 gap（可负，墙钟回拨）；member `k` 重构为
  `seq = seq0 + k` 与 `time = time0 + sum(dt[0..k-1])`。
- **text 不做合并** —— token 边界就是数据，合并会破坏可恢复性。
- 编码器白名单 + 解码器白名单 → "未知字段未来变体压缩比下降但不会丢数据"。

### 3.6 Surface fold（`core/session/src/surface.ts`）

事件 log 是 append-only，**当前 surface**（模型可见节点）是它的纯 fold：

```
SurfaceOp = 'append' | { op: 'replace'; start: number; end: number }
foldSurface(events):
  for event in events:
    switch surfaceOp:
      case 'append':    nodes.push(event.seq)
      case 'replace':   nodes.splice(startIdx, endIdx - startIdx + 1)
                       replaceGeneration++
                       shadowedSeqs = removed subseq
```

不变量：
- **`sourceEventSeqs` 必须包含每一个被 shadow 的 seq**（`surface.ts:181-199` 的
  `assertProvenance`）—— 让 audit log 可以完整复盘"哪条被替换了"。
- `tool/result` 的 surface replace **只允许改 `content`**（`assertToolResultRewrite`，
  `surface.ts:264-290`）—— 其它字段变化会被拒绝。
- **append-only 的 log + 折叠的 surface** 是经典的"不可变 + 视图"模式，让
  compaction / tool-prune / spill 都能落地为对 log 的 surfaceOp:replace。

### 3.7 Session Checkpoint Policy（`session-checkpoint-policy/src/index.ts`）

**3 个 checkpoint 边界**（注释里说"语义级 durable 检查点"）：
1. `llm/stream`：模型请求前缀进模型流前必须 durability（`afterCheckpoint`）。
2. `tools/execute`：顶级 tool 调用前必须 flush（嵌套复用外层 durable call）。
3. `agent/pre-step`：每个 step 之前 flush 上一 step 的全 batch。

效果：**"对话里看到的就是已经落盘的"，崩溃不会留下"已对模型说但还没存"的事件**。

---

## 4. Projection（read-model）

### 4.1 抽象：`ProjectionDefinition<K, S>`

```ts
interface ProjectionDefinition<K, S> {
  key: K                                // 投影名（必出现在 SessionProjectionMap）
  schema: ZodType<SessionProjectionMap[K]>   // view 输出走 schema 校验
  init(): S
  apply(state: S, event: SessionEvent): S    // 纯函数；未关心的事件返回同一引用（Object.is gate）
  view(state: S): SessionProjectionMap[K]    // state → wire payload
  stateVersion: number                        // 序列化字段或 fold 语义变 → bump
}
```

### 4.2 Registry 驱动（`session-projection/src/index.ts:171-426`）

```
registry.register(definition):
  registrations.set(key, { def, cells: WeakMap<Session, UnitCell>, refs: 1 })
  ... (refs 计数让多个 preset 共用同一 unit 而最后 unload 才移除 key)

session/event (line 181-183):
  drive(session, event):
    for registration in registrations:
      cell = cells.get(session) ?? buildCell(def, session.events.slice(0, event.seq))
      next = def.apply(cell.state, event)
      changed = !Object.is(next, cell.state)      // 引用相等就是没变，零下游工作
      cell.state = next; cell.observedSeq = event.seq
      if changed: listener(session, key, schema.parse(def.view(next)), event.seq)
```

**whole-value event rule** 的好处在 `apply` 上：每次 commit 都传完整 state，
所有 transition 都极简 `old.thing = new.thing`。下游 listener 只在引用真的变时被唤醒。

### 4.3 检查点持久化（`session-projection-cache/src/index.ts`）

```ts
class SessionProjectionCache extends Service {
  // 写路径 = write-behind（count + interval throttle）+ 2 个 mandatory 点
  ctx.on('session/event', (session, event) => {
    if (event.type === 'turn/end') void flushSoft(session, 'turn/end')
    else { state.pending++; if (>= writeEveryEvents) flushSoft('count threshold');
           else state.timer ??= setTimeout(flushSoft('interval'), writeIntervalMs) }
  })
  ctx.on('session/disposed', (session) => { flushSoft('detach'); markClean; dirty.delete })
}
```

冷读梯子（`coldSnapshot`，`index.ts:166-197`）：
```
1. cachedSnapshot(identityOf(header))    ← 零 I/O，cached rows（version-match only）
2. coldSnapshot:
   floor = registry.restoreFloor(checkpoint)         ← one-below anchor
   tail  = persistence.readFrom(id, floor)
   record 校验 identityOf(tail.meta) 与 stored record 一致
   if 不一致 / 不可用 → throw → catch → full read from seq 0
   restored = registry.restore(cached, tail.events, floor)
   putSoft(checkpoint, 'cold-read write-back')         ← 失败仅 warn，下次冷读更长 tail
   return restored.snapshot
```

**关键设计**：
- **缓存是"fold 加速"而非权威**（注释反复强调"never wrong, only stale"）：
  - `ver` 不匹配 → 整行丢弃，重新 fold。
  - cache 行 `seq > endSeq`（log 被截断 / crash repair）→ 整行丢弃。
- **缓存写入是 fail-soft**：`putSoft` 失败仅 warn，不阻塞调用方。
- **Checkpoint 写入是有序写后于持久化**（`write()` 第 150 行 `await sessions.flush`
  在 `put` 之前）—— 缓存可以"落后于" log，但绝不会"超前"，杜绝"fold 出了
  log 里没有的事件"的鬼影。

### 4.4 identity-of-record

```ts
// spec.ts
const checkpointIdentity = z.object({ createdAt: z.number().int().nonnegative(), cwd: z.string().optional() })
```

`session_id` 是 slot 而不是 lifecycle：`identityMatches(record.identity, identityOf(meta))`
必须 `createdAt + cwd` 同时匹配，否则丢弃。这防住了"删除后重建同 id"或
"持久化根换了但缓存还在"两类陷阱。

### 4.5 几个自带的 unit

- `token-meter` 的 `tokenUsageProjectionDefinition` / `contextPressureProjectionDefinition` /
  `contextBreakdownProjectionDefinition` —— 全程 fold 出来的"对话用了多少 token"折线。
- `goal` —— 长期目标的 snapshot fold（详见 §6）。
- `todos` —— todo/write 事件 + turn/start 清空（详见 §7）。
- `session-stats` —— 计数与 wall time。

---

## 5. 压缩（Compaction）

### 5.1 能力面（`compaction/src/index.ts`）

```ts
abstract class CompactionEngine extends Service {
  abstract compactIfNeeded(agent, trigger: 'pressure' | 'context-overflow', signal)
  abstract compactNow(agent, signal, sourceCommandId?)       // 强制 idle 压缩
  abstract compactRegion(start, end, agent, signal?)        // 强制 surface 区间
}
```

**Compaction result**（`types.ts`）：
```ts
{ compactionId, startSeq, summarySeq, endSeq,
  summary, shadowedRange, shadowedSeqs, shadowedTokenCount }
```

### 5.2 触发路径（`compaction-basic/src/index.ts`）

```
ctx.on('agent/pre-step'):
  result = await compactIfNeeded(agent, 'pressure', signal)         // 步边界常规压力
ctx.on('agent/request-error', { failure.code === CONTEXT_WINDOW_EXCEEDED_CODE }):
  result = await compactIfNeeded(agent, 'context-overflow', signal) // 上游 400 强制压缩
  return { kind: 'retry' }                                          // 重试
ctx.on('agent/status', { status === 'idle' }):
  overflowRetries.delete(agent)                                      // 成功响应清零
ctx.on('session/event', { event.type === 'assistant/message' }):
  overflowRetries.delete(agent)                                      // 成功响应清零
```

**压力预算**（`config.ts`）：
- `thresholdRatio = 0.8` —— `floor(contextWindow * 0.8)` 是触发阈值。
- `retainRatio = 0.16` / `retainTokens` —— 强制保留的尾部 token 数（retainTokens vs retainRatio 二选一）。
- `compactionRetries` —— 单次压力触发最多压缩几次；超限则抛错而非死循环。
- `maxOverflowRetries` —— 上下文溢出后最多 retry 几次。
- `modelPolicies: [{ provider, model, ... }]` —— 按 provider/model 覆盖。

`resolveCompactSpec(policy, contextWindow)`：把 ratio 转成实际 token 数 + 校验
`retainTokens < thresholdTokens`（必须在编译期失败，否则永远不会触发压缩）。

### 5.3 区间选择（`compaction-basic/src/region.ts:98-134`）

```
selectCompactableRange(session, measurement, retainTokens):
  pricedNodes = measurement.nodes                  // tokenMeter 已经按 surface 排序
  // 1. 收集尾部 retainTokens 之后的"保留起点"
  keepFromIdx = pricedNodes.length
  for i from end → 0:
    accumulated += pricedNodes[i].tokens
    keepFromIdx = i
    if accumulated >= retainTokens: break
  // 2. 向左扩展直到 balancedBefore(start) —— 防止把 tool-call/result 对切开
  while keepFromIdx > 0 && !toolPairingBalancedBefore(session, surfaceNodes[keepFromIdx]):
    keepFromIdx--
  // 3. head 直接取 surface 头
  return { start: surfaceNodes[0], end: surfaceNodes[keepFromIdx - 1] }
```

**`toolPairingBalancedBefore` / `toolPairingBalancedAfter`**（`compaction/src/tool-pairing.ts`）：
维护一张"每条 surface seq 在当前 surface 上的位置 + 当前位置 cut 是否平衡"的缓存：
- 在 assistant/message 有 `tool-call` 块 → `delta = +count`
- 在 tool/result → `delta = -1`
- 任意位置 cut `i`：从 0 到 i 累加 delta，若 == 0 则 balanced。

`Surface.replaceGeneration` 触发了缓存重建——**surface 改变后 balance cache 必须重算**，
否则会用旧版本给出过时的"可以切这里"的判定。

### 5.4 事务结构（`region.ts:152-254`）

```
compactSurfaceRegion(deps, session, start, end, agent, options, signal):
  // 1. 同步预检
  selection = validateSurfaceRegion(session, start, end)     // 必 balanced 两端 + 顺序
  inspectCompactionEntryState()                               // 是否有未关闭 compaction/start？
  assertCompactionInactive(...)                               // 否才允许
  // 2. 同步提交 compaction/start（写日志）—— durable lock
  startEvent = session.append('compaction/start', { compactionId, ...lifecycle })
  // 3. 异步 summarizer
  prepared = prepareCompaction(deps, session, selection)     // token 价 + replay input
  summarized = await summarizeCompaction(deps, prepared, agent, compactionId, ..., signal)
  // 4. 同步提交 summary + 替换 user message + compaction/end
  if options.stability === 'whole-surface':
    assertWholeSurfaceUnchanged(deps, session, prepared)      // surface 全不变才能替换
  else ('selected-span'):
    assertSelectedSpanStable(...)                             // 仅区间不变即可（外面可以 append 新事件）
  summaryEvent = session.append('compaction/summary', { ... })
  session.append('user/message', checkpointMessage, { surfaceOp: replace start..end, sourceEventSeqs: [...] })
  endEvent = session.append('compaction/end', { compactionId })
  // 5. flush() (manual 路径) —— durable 检查点
  // 6. throw ManualCompactionError 类化失败
```

**两条硬约束**：
1. `compaction/start` 是 **durable compaction lock**：一旦 append，下一次
   `compactIfNeeded` 必须等到匹配 `compaction/end` 才能继续。
2. **替换必须 framed**（`frameSummary`，`summarizer.ts:189-194`）：
   ```
   <system-reminder>...CHECKPOINT_PREAMBLE...
   <compacted-summary>...summary blocks...</compacted-summary>
   ```
   frame 由 producer 拥有（与 AGENTS.md 的 `<system-reminder>` 同一规约）。

### 5.5 摘要复用 KV cache（`summarizer.ts:121-164`）

摘要调用 = 一次 `ctx.llm.stream()`：
- **system 块 + tools + messages** 全部来自最近一次路由请求（`buildSummarizationInput`）。
- 最后追一条 user message `COMPACTION_INSTRUCTION`（要求输出固定 8 节 Markdown）。
- 这让摘要请求是 **真前缀**：system/tools/prompt 都与原对话一致 → 命中 provider 的 KV cache。

**8 节模板**（`summarizer.ts:36-58`）：
```
## Primary Request and Intent
## Key Technical Concepts
## Files and Code
## Errors and Fixes
## Pending Jobs
## Current Work
## Next Step
## Critical Context
```

`summaryText()` 把图像 block 视为不支持 → 拒收（`contentHasImage` 抛 `UNSUPPORTED_CONTENT`）。

### 5.6 Tool-Result 剪枝（`compaction-tool-result-pruner/src/index.ts`）

**与 LLM 摘要并行**的纯机械降压：

```ts
pruneContent(blocks):
  totalChars = measureContent(blocks)             // Unicode code points（不是字节）
  if totalChars <= thresholdChars (默认 8192): return null   // 不动
  removedStart = headChars (4096)                  // 保留头部
  removedEnd = totalChars - tailChars (1024)       // 保留尾部
  for block in blocks:
    if not text: 保留原样
    text = head_end + PRUNE_MARKER + tail_start    // Unicode code point slice
  marker 插入在第一次与 removed span 相交的 text block 里
  marker = '\n\n[... tool result middle pruned ...]\n\n'
```

**`pruneSession`** 遍历当前 surface 所有 `tool/result`，对每个：
1. `compaction/prune` 同步事件（shadow-price，记 shadowedTokenCount）
2. 紧接着 `tool/result`（`surfaceOp: { replace, start, end }`，`sourceEventSeqs: [seq]`）

**shadow-price protocol**：纯 consumer（projection unit 等）能通过 `compaction/prune`
事件的 `shadowedTokenCount` 字段 **在 fold 时减去**这部分价，无需存 per-node state。

### 5.7 `/compact` 命令（`command-compact/src/index.ts`）

人类命令 Consumer：6 类失败映射到人类可读错误（`busy/cancelled/changed/summary/commit/persistence`）。
`busy` 是 `agent not idle` 或 `durable compaction lock` 占用。

---

## 6. 外置存储（Spill）

### 6.1 Service Definition（`spill/src/index.ts`）

```ts
abstract class SpillStore extends Service {
  abstract saveText(input: SaveTextSpill): Promise<SpillRef>
}
// input: { owner: { sessionId }, source: { toolName, callId, label }, suggestedName, content }
// output: { locator: SpillLocator, bytes, retrievalHint }
```

**最小抽象**：只 saveText，绝不提供 list / search / delete。注释明示 "owns NO retention policy,
NO tool-result replacement, NO retrieval"。

### 6.2 本地实现（`spill-local/src/store.ts`）

```
saveTextFile({ root, sessionId, suggestedName, content }):
  dir  = sessionDir(root, sessionId)              // = "<root>/session-<sha256(sessionId)[:12]>"
  mkdir(dir, { recursive, mode: 0o700 })
  safeName = encodeSegment(suggestedName)         // 单段路径安全编码
  path = <dir>/<randomBytes(6).hex>-<safeName>    // 随机前缀防 symlink 预植
  handle = open(path, 'wx', 0o600)                // 排他 owner-only 写
  handle.writeFile(content)
  return { path, bytes }
```

**多道防御**：
- `mkdtempSync(tmpdir + 'dsh-spill-')`（`store.ts:27-30`）—— 默认根目录是
  **私有 0700 进程级临时目录**，"predictable world-readable paths would let other
  local users read spilled tool output"。
- `encodeSegment` —— 单段字符映射，不允许 `..` `~`、路径分隔符。
- 随机文件名 + `wx` 标志 —— 防止 symlink 攻击。
- `0o600` 模式 —— 只有 owner 可读写。

### 6.3 Spill Policy（`spill-policy/src/index.ts`）

只挂 `tools/post-execute` 与 `tools/code-dispatch-log` 两个 waterfall：

```
post-execute (prepend: true):
  decision = await next()                       // 让下游先 settle
  if decision.kind !== 'accept' || has 'value' || exec.parent !== undefined || name === 'read':
    return decision                              // 嵌套 / 块 / 值替换 / read 跳过
  text = flattenPlainText(content)
  if byteLength(text) <= maxInlineBytes: return decision
  replacedText = await spillReplacement(text, totalBytes, ...)
  if replacedText === undefined: return decision  // 任何保存失败 → 保留 inline
  return { kind: 'accept', content: [{ type: 'text', text: replacedText }] }
```

`spillReplacement` 内部：
1. 拿不到 `sessionId` / 拿不到 `ctx.spillStore` / `saveText` 抛错 → warn + 保留 inline
   （注释："A spill failure must NEVER turn a successful tool call into an isError or hide the inline result"）。
2. `reserve = byteLength(notice) + 2` 先于 preview 计算（`index.ts:171-172`），避免
   "naive preview that spent the whole budget then appended the notice" 比原值还大。
3. **最终 byte 仍 > cap** → 丢弃 spill 文件、保留 inline。
4. `preview()` 用 `TextRetainer({ kind: 'headTail', headBytes: ceil(budget/2), tailBytes: floor(budget/2) })`
   —— 头尾双保留。

**与 tool-result-pruner 的关键区别**：
| 维度 | spill-policy | compaction-tool-result-pruner |
|---|---|---|
| 触发时机 | post-execute 当下 | step / 溢出 compaction 之前 |
| 写入 | 文件（session 外） | session log（surface replace） |
| 触发阈值 | 单次工具结果的 UTF-8 字节数 | 全 session 全部 tool/result 的 Unicode code points |
| 输出可读性 | head+tail + 完整文件 locator | head+marker+tail（没有"原文件"可访问） |
| 与下轮交互 | 文件可被 `read` 工具显式读回 | 永久被替换 |

### 6.4 输出保留底座（`util/output-retention/src/index.ts`）

`TextRetainer` 是 spill-policy 复用的字节级保留器：
- `head` / `tail` / `headTail` 三策略。
- **UTF-8 边界保留**：`trimTrailingPartialUtf8` 与 `trimLeadingContinuationUtf8`
  保证 cut 不产生 U+FFFD。
- **预算内存**：head 限制为 cap 字节，suffix 用滑动窗口 + 单 chunk 内 subarray，
  内存占用恒为 `headCap + tailCap + 一 chunk`。
- `omittedBytes` 字段：精确的 byte 计数（"Omitted N bytes"。

---

## 7. 会话检索（Session-Query）

### 7.1 抽象（`session-query/src/index.ts`）

```ts
abstract class SessionQueryEngine extends Service {
  abstract searchSessions(req, exec?): Promise<SessionSearchPage<SessionSearchHit>>
  abstract searchEvents(req, exec?): Promise<SessionEventSearchPage>
  abstract searchAll(req, exec?): Promise<AllSessionsSearchPage>  // 同时跨 session+event
  abstract trace(req, exec?): Promise<SessionEventTracePage>      // lineage + 关系
  abstract readWindow(req, exec?): Promise<SessionEventWindow>    // 精确读 + bounded window
  abstract list(filter?, exec?): Promise<SessionRecord[]>         // 列出全部
  abstract exportSnapshot(req, exec?): Promise<SessionLogSnapshot> // 完整事件流 + surface
}
```

### 7.2 Corpus（`session-query/src/corpus.ts`）

`SessionCorpus` 解决 **live vs persisted** 的双源一致：
```
listSessions:
  persisted = persistence.list()  (or [])
  live     = ctx.sessions.list()
  records.set(persisted.id, { header: clone, live: false, persisted: true })
  records.set(live.id, { header: clone, live: true, persisted: persisted_id !== undefined })
  return [...records.values()].sort(compareSessions)
load(id):
  live = ctx.sessions.get(id)
  if live: snapshotLive(live)             // 优先 in-memory
  else: persisted via list+inspect
       if attached mid-flight: snapshotLive(attached)         // race: persistence 期间 live attach 了
       else: cloned persisted snapshot
```

`projectMany(ids, projector)`：并发 `persistedInspectConcurrency`（默认 5）的 worker
跑 inspect；同步投影由 caller 在 worker 内完成，**log 不被 batch retained**。

**`compareSessions`**：`b.createdAt - a.createdAt || a.id.localeCompare(b.id)` —— 新→旧，
同时间戳按 id 字典序，二级稳定。

### 7.3 文档构建（`session-query/src/documents.ts` + `extraction.ts`）

`buildSessionEventSearchDocuments`：每个事件过 `extractSessionEventText`：

```
user/message      → 全部 content 块拼接
assistant/message → 全部 content 块拼接
tool/call         → name + arguments 拼接
tool/result       → content + error.name + error.code
todo/write        → 每 todo "status content" 拼成多行
turn/end          → "error: msg" | "aborted" | reason.kind
其余结构性事件    → 空字符串（不索引）
```

`classifySurface`：调 `foldSurface(events)` 把每个 seq 标 `current` 或 `shadowed`，
SQLite 的 `surface UNINDEXED` 字段（`'current' | 'shadowed' | 'log-only'`）就能支持
"只搜当前可见节点"的过滤。

### 7.4 SQLite 实现（`session-query-sqlite/src/index.ts` + `schema.ts`）

**双 schema 关键设计**：

```
搜索状态表 (persistent):
  search_state (singleton PK, global_generation)        ← 跨 session 单调递增
  persisted_sessions (id, version, created_at, ..., revision, generation)  ← 持久化 sessions 元数据
  persisted_docs (FTS5: text, session_id, seq, type, time, surface, codepoint_length)

会话搜索专用 TEMP 表 (temp):
  live_sessions (id, ..., fingerprint, persisted, generation)              ← fingerprint = sha256(header-json)
  live_docs (FTS5: 同上)
```

**reconcile 算法**（`_reconcile`，`index.ts:395-456`）：
1. 列 `persisted_sessions` → 列 `live_sessions`（temp）
2. `live_sessions[i].fingerprint !== persisted_sessions[i].fingerprint` →
   **dirty**（持久化元数据变了）
3. dirty 的 entry 重新 load、extract、重写对应 FTS5 行 + `revision`、`generation++`
4. 全表完成后 `search_state.global_generation = nextMainGeneration`
5. **generation cursor**：调用方可基于 generation 跳过没变的内容。

**FTS5 用 `unicode61` 分词** —— unicode 友好，对中文/Japanese 也能分。

`SESSION_QUERY_SQLITE_SCHEMA_VERSION = 8` + `SESSION_QUERY_SQLITE_APPLICATION_ID = 0x44534851`
同 SQLite 持久化：陌生 DB 直接拒绝。

### 7.5 检索工具（`tool-session-query/src/operations.ts`）

5 个 model-facing 工具：

| 工具 | 用途 |
|---|---|
| `session_search` | 按 query 跨 session 找最强匹配事件 |
| `session_event_search` | 在 1 个 session 内按 query 找事件 |
| `session_trace` | 沿 parentSession 追溯 lineage |
| `session_event_trace` | 看 1 个事件的 `sourceEventSeqs`（被替换了哪些、谁替换了谁） |
| `session_event_read` | bounded window 精确读（`SESSION_QUERY_READ_WINDOW_MAX`） |

`sourceEventSeqs` 是个特别重要的字段 —— 任何 surface replace 都强制携带它，所以
"我这条日志是从哪几条被压缩/被 prune 来的"是可审计的。这与 §3.6 的不变量互相绑定。

---

## 8. 目标与 Todo：自带 fold 的"亚记忆"

### 8.1 Goal（`goal/src/index.ts` + `fold.ts`）

**Event-sourced 全快照**（"whole-value rule" 的活教材）：
```ts
// 每次写都是整张快照（除了 clear 是 tombstone）
type GoalChangeMeta =
  | GoalSnapshotChangeMeta  // { operation: 'create'|'edit'|'pause'|'resume'|'complete'|'block', goal, roundsStarted, createdAt, updatedAt }
  | GoalClearChangeMeta     // { operation: 'clear', cleared: {id, revision}, clearedAt }
```

`applyGoalEvent(state, event)`:
- `goal/change` → `decodeGoalChange` 校验 → `applyGoalChange`（严格语义校验）
- `user/message` 且 source.kind === 'goal' → 校验 `(goalId, revision, round) == (current.id, current.revision, roundsStarted+1)` → `roundsStarted++`

**严格 fold** 与 **轻投影** 是两个不同函数：
- `foldGoal` / `applyGoalChange` —— fail-loud，跨实例复盘必走它。
- `applyGoalProjection` —— last-wins fold，对**无效变更静默返回原 state**
  （`index.ts:96-113` 注释："this transition is projection-grade: correctness of the
  written change is the write side's job"）。

注册到 `sessionProjections` 作为 `'goal'` unit（`index.ts:204-213`），`stateVersion: 4`。

**Goal rounds** —— `user/message` 的 `source: { kind: 'goal', goalId, revision, round }`
作为"下一轮已 admitted"信号，由 `goal-round-driver` 监听：
- `agent/inbox/inserted` / `claimed` / `discarded` —— 标记 attempt 的 phase。
- `agent/pre-step` —— 校验 reservation 还活着、goal 还 armed、round 序号正确，否则
  reject + disarm。
- `turn/end.kind === 'aborted'` —— 撤回 attempt。
- `turn/end.kind === 'max-tokens'` —— 直接 disarm。

`requestDrive` 在每次 agent idle / goal changed 时尝试 inject 下一 round 的 user message。

### 8.2 Todo（`todo/tool-todo/src/index.ts`）

更简单 —— **whole-list 替换**：
```ts
// 'todo/write' 事件 data = { todos: TodoItem[] }
// projection 'todos' (stateVersion: 2):
apply(state, event):
  if event.type === 'todo/write': return event.data.todos   // 最新 list
  if event.type === 'turn/start':  return null              // 下一轮清空
  return state                                                // 其他保持
```

`allowParallelInProgress: boolean` —— 单活跃 vs 多活跃：描述文案根据这个 flag 切。

`schema.additionalProperties: false` —— 不允许偷偷塞额外字段（`index.ts:159`），"the
logged snapshot must equal what the model believes it wrote"。

---

## 9. 设计不变式清单（综合）

按"违反即触发上游 bug"的级别排序：

1. **`toolPairingBalancedBefore/After` 必须用 surface 当前位置**（不是 seq 数）。
   seq 数对，surface 位置错 → 拆坏 tool-call/result 对 → 上游 API 400。
2. **Surface replace 必须携带 `sourceEventSeqs` 覆盖所有 shadowed seq**。
   缺一个 → 审计不可复盘 + compaction fold 拒绝。`surface.ts:181-199` `assertProvenance`。
3. **Append-only log 是真权威**。所有 read-model（projection/cache/index）都允许 stale，
   但**不允许 ahead-of-log**。`session-projection-cache/src/index.ts:140-152` 的"先
   `sessions.flush` 再 `put`"是这个不变量在 cache 路径的具体落实。
4. **State-carrying event 必须 whole-value**（非 delta）。projection unit 即可插即用，
   listeners 靠 `Object.is` 零开销唤醒。
5. **Ver mismatch = 整行 discard**。projection cache / index / 任何持久化 row 都不能
   silent migrate schema，version 是隔离不同 schema 版本的唯一标签。
6. **`unavailable !== absent`**：provider 暂时不可读 ≠ 文件不存在；统一走"保持 last-good"
   而不是 pushRemoval。三层目录（user-global / project root / cwd descendant）都有这个
   语义。
7. **Identical-region identical-output**：trim-then-digest 让"软链等价"和"末尾换行差异"
   都不会被误判为"文件变了"。
8. **Best-effort 是 fail-soft，绝不变成 isError**。spill 失败 → warn + 保留 inline。
   Prune 失败 → throw（因为是 in-memory mutation 不一样）。
9. **Reconciliation generation = one-below anchor**。`restoreFloor(checkpoint) → row.seq + 1`，
   让 tail read 从这里开始。如果 log 截短到 row.seq 之下 → 触发 full re-read。
10. **`compaction/start` 是 durable lock**。一旦写入必须等到 `compaction/end` 才能开始
    下一次。手动 `/compact` 在 agent idle 才能开。
11. **UTF-8 边界保留是硬约束**。`truncateUtf8` / `trimTrailingPartialUtf8` /
    `trimLeadingContinuationUtf8` —— 任何字节级 cut 都不能切在 codepoint 中间。
12. **`<system-reminder>` 帧由 producer bake**。session surface 不二次包裹。
13. **Capability seam 的 inject 必须是显式 cordis `inject`/`static inject`**。任何隐式
    `ctx.get(...)` 都意味着"我假装依赖它，但可能它还没就位"。
14. **`ver === row.seq`（cache） && `seq === row.endSeq`**（读 tail）才是 "usable row"。
    不满足则全行丢弃（绝不向前兼容 forward-apply）。

---

## 10. 对「狼人杀 13 人局 Agent」的可借鉴点

LsmAgentGame 的狼人杀 Agent 当前有：13 个 bot、5 个法官、内存（§131）、steering queue
（§20260813-04）、heart-thought（§119）、道具系统（§132-§133）、限流、retry。
可借鉴点按"性价比"排序：

### 10.1 **`session-query` 的 reconcile generation + FTS5 cursor** —— 高

狼人杀现在每次 BotTranscript 推送都是简单 fire-and-forget。
**借鉴 7.4 的 schema 设计**：双表（持久化 metadata + TEMP live）+ `fingerprint` +
`global_generation` cursor → 前端可以按 generation 跳过没变的内容，省全量重算；
历史多局索引可以用 FTS5 + 同样的 surface 分类（current/shadowed）。

> 注意：FTS5 在 Go 生态可用 `mattn/go-sqlite3` 或 `modernc.org/sqlite`；或者直接
> 用 `t_lsm_game_session_history` + 触发器维持 `FTS5` virtual table。

### 10.2 **AGENTS.md 的 reconcile 算法** —— 高

狼人杀每个 Bot 现在每轮重读"我的系统提示"是字面常量；难以支持"运行时修改人设"
或"按角色注入不同章节"。
**借鉴 §2.4 的 reconcile 算法**：把 bot prompt 拆成 baseline（不会变）+ scope-keyed
chunks（按角色/按房间/按 night/day 注入），用 cache + versioned reconcile
而不是全量 re-read。

### 10.3 **`compaction/start` + `compaction/end` 范围锁** —— 高

狼人杀现在每局都是 in-memory 状态；**如果未来做跨局"分析房间"功能**（比如教练视角
对 13 局回放做总结），可以参考 §5.4：
- `analysis/start` —— 进入分析模式的 durable lock；期间禁用 bot 发言。
- 摘要写完后 `analysis/end`，normal mode 恢复。
- 整个流程有可审计的事件链。

### 10.4 **`session-projection-cache` 的 whole-value + versioned checkpoint** —— 中

狼人杀的 13 bot 模型运行统计（§131）可以走 projection unit：
- `modelMemory` 的 fold 来自 `MEMORY.md` 写入事件
- 用 `stateVersion` 隔离 schema 变更
- 同样的"可落后于 log，绝不超前于 log"约束

### 10.5 **Tool-Result 剪枝 + Spill 的"两段式"** —— 中

狼人杀现在没有大输出问题（bot 发言限流 100 字），但**未来如果做"bot 看到完整
游戏日志"的 debug 工具**，可以借鉴 §5.6 + §6：
- 当前日志太大 → `compaction-tool-result-pruner` 风格的 head/middle/tail 永久压
- 引用（URL / 工具返回值）太大 → spill 文件 + locator（不入上下文）

### 10.6 **Tool-pairing balanced cache** —— 中

狼人杀当前 tool-call/result 的对应关系是隐式的（按顺序）。**借鉴 §5.3 的
balance cache**：
- 在 `(assistant/text-thought + tool-call)` / `(tool/result + 心口不一)` 等组合时
  可以有 "balanced" 校验，防止某些边界条件把"召唤思考+ tool-call"和" tool-result"
  错误配对。
- 特别是 §20260813-04 U1 的 `SteeringQueue` 引入后，可能出现"多路并发 tool_call"
  的配对问题 —— 这种 cache 显式化就有用了。

### 10.7 **`ver` 隔离 schema 变更** —— 低（但简洁）

狼人杀的多个版本共存经常出现"老 DB schema 解析新 JSON 字段"的问题。
**借鉴 §4 的 `stateVersion`**：每个 projection unit / 每个 fold 加一个 `version` 字段；
schema 变了 bump 一档，老 cache 整行丢弃，下次从 0 fold。
（狼人杀当前 `t_lsm_game_agent_memory` 已有 `version` 字段（§131），但 fold 路径上
还没有系统性的"version mismatch → discard"约束。）

### 10.8 **Capability seam 的 inject / static inject** —— 低

狼人杀是单进程 Go，不需要 cordis 的 DI。但**包设计可以借用"每个能力包一个
`<X>` Service Definition + 多个 provider"** 的范式：
- `WerewolfManager` 是能力 seam，下面有 `Action_Speak` / `Action_Vote` 等接口。
- 多个 driver（agent / judge / replayer / observer）共用 seam。
- 新加 `Action_UseProp` 时只需 provider 知道，不用动 manager。

---

## 11. 关键文件速查（按用途）

| 用途 | 文件 | 行数（不含测试） |
|---|---|---|
| AGENTS.md 发现/缓存/渲染入口 | `packages/context/agent-instructions/src/index.ts` | 367 |
| AGENTS.md 文件探测与读取 | `packages/context/agent-instructions/src/files.ts` | 521 |
| AGENTS.md byte-budgeted 渲染 | `packages/context/agent-instructions/src/render.ts` | 361 |
| AGENTS.md reconcile + versioned cache | `packages/context/agent-instructions/src/state.ts` | 433 |
| Session persistence seam | `packages/session/session-persistence/src/index.ts` | 243 |
| Session persistence coordinator + write-behind | `packages/session/session-persistence/src/coordinator.ts` | 1361 |
| Session write-behind 调度器 | `packages/session/session-persistence/src/write-behind.ts` | 163 |
| JSONL 后端 + zstd frame scan | `packages/session/session-persistence-jsonl/src/{format,zstd}.ts` | 413 + ~150 |
| SQLite 后端 schema + open | `packages/session/session-persistence-sqlite/src/schema.ts` | 270 |
| Chunk row 打包（56× 压缩比） | `packages/core/session/src/chunk-rows.ts` | 346 |
| Surface fold 算法 | `packages/core/session/src/surface.ts` | 460 |
| Projection registry (drive) | `packages/session/session-projection/src/index.ts` | 428 |
| Projection cache 持久化 | `packages/session/session-projection-cache/src/index.ts` | 300 |
| Checkpoint 持久化时机 | `packages/session/session-checkpoint-policy/src/index.ts` | 84 |
| Compaction seam | `packages/compaction/compaction/src/index.ts` | 172 |
| Compaction tool-pairing balance | `packages/compaction/compaction/src/tool-pairing.ts` | ~135 |
| Compaction trigger + 自动调度 | `packages/compaction/compaction-basic/src/index.ts` | 431 |
| Compaction 区间 + 事务 | `packages/compaction/compaction-basic/src/region.ts` | 550 |
| Compaction 摘要（复用 KV cache） | `packages/compaction/compaction-basic/src/summarizer.ts` | 224 |
| Tool-result 剪枝 | `packages/compaction/compaction-tool-result-pruner/src/index.ts` | 187 |
| `/compact` 命令 | `packages/compaction/command-compact/src/index.ts` | 106 |
| Spill seam | `packages/spill/spill/src/index.ts` | 56 |
| Spill 本地存储（含 encodeSegment） | `packages/spill/spill-local/src/store.ts` | ~120 |
| Spill policy | `packages/spill/spill-policy/src/index.ts` | 232 |
| Output retention (head/headTail/tail) | `packages/util/output-retention/src/index.ts` | 443 |
| Session query seam | `packages/session-query/session-query/src/index.ts` | 359 |
| Session corpus (live vs persisted) | `packages/session-query/session-query/src/corpus.ts` | 309 |
| Session event 文本抽取 | `packages/session-query/session-query/src/extraction.ts` | ~80 |
| Session query SQLite + FTS5 + reconcile | `packages/session-query/session-query-sqlite/src/index.ts` | 1103 |
| Session query SQLite schema | `packages/session-query/session-query-sqlite/src/schema.ts` | 173 |
| Goal fold (event-sourced) | `packages/goal/goal/src/fold.ts` | 349 |
| Goal 服务 | `packages/goal/goal/src/index.ts` | 592 |
| Goal-round driver | `packages/goal/goal-round-driver/src/index.ts` | 445 |
| Todo tool + 投影 unit | `packages/todo/tool-todo/src/index.ts` | 226 |

---

## 12. 一句话回顾

DeepSeek Harness 的记忆管理不是单一"记忆系统"，而是 **append-only event log + 多 fold 视图 + 多种压缩/外置策略 + capability-seam DI** 的拼装。每个 fold 都是纯函数，每个 provider 都是可替换的，每个 schema 变更都有 `ver` 标记，**"可落后于权威，但绝不能超前于权威"** 是贯穿全文的硬约束。