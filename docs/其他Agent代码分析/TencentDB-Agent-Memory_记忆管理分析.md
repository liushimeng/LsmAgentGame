# TencentDB-Agent-Memory — Agent 记忆管理 / Memory 文件管理分析

> 分析对象：`/usr/local/LsmGitOpenSource/TencentDB-Agent-Memory`（Tencent 开源 Agent Memory Hub，
> TypeScript，约 166K LOC，4 模块：MemoryCore / MemoryKnowledge / MemoryPanel / MemoryProxy）
> 分析日期：2026-08-12 | 分析范围：记忆分层、写入、召回、存储、Skill、遗忘、ACL
> 姊妹文档：[Context 管理分析](TencentDB-Agent-Memory_Context管理分析.md) ·
> [意图识别与任务分解分析](TencentDB-Agent-Memory_意图识别与任务分解分析.md)

---

## 0. 前置澄清：README 的四层命名，代码里只有三层半

README 反复讲 `L0 Conversation → L1 Atom → L2 Scenario → L3 Persona`。
实际 grep 代码：**`Atom` 和 `Scenario` 这两个词在代码里一次都没出现**。

| README 名 | 代码里的真名 | 载体 |
|---|---|---|
| L0 Conversation | `conversation/`（原始对话） | SQLite 表 + JSONL |
| L1 **Atom** | **`MemoryRecord`** | JSONL 文件 + 向量库双写 |
| L2 **Scenario** | **`SceneBlock`** | `scene_blocks/*.md` Markdown 文件 |
| L3 Persona | `persona.md` | 单个 Markdown 文件 |

这不是纯命名问题——它反映了一个重要事实：**L2 和 L3 根本不是数据库记录，是 LLM 直接读写的
Markdown 文件**。这决定了它们的整个管理方式（见 §5）。

---

## 1. 记忆分层模型

### 1.1 L1：`MemoryRecord`（唯一结构化的一层）

`MemoryCore/src/core/record/l1-writer.ts:55-97` 完整字段：

```ts
export interface MemoryRecord {
  id: string;                    // 去重更新用唯一 ID
  content: string;
  type: MemoryType;              // 7 类，见下
  priority: number;              // 0-100，-1 = 严格全局死命令
  scene_name: string;            // 所属场景
  source_message_ids: string[];  // 溯源到原始消息
  metadata: EpisodicMetadata | Record<string, never>;
  timestamps: string[];          // 时间戳轨迹（合并历史追踪）
  createdAt: string;
  updatedAt: string;
  version?: number;              // 单调版本；新建=1，update/merge +1
  sessionKey: string;
  sessionId: string;
  taskId?: string;
  teamId?: string;               // 三维租户隔离
  userId?: string;
  agentId?: string;
}
```

**类型标签集（7 类，两种模式）**（`l1-writer.ts:30-38`）：

```ts
export type MemoryType =
  | "persona" | "episodic" | "instruction"                       // chat 模式
  | "work_fact" | "work_task" | "work_method" | "work_artifact";  // code/work 模式
```

三个设计细节值得单独说：

1. **`priority: -1` 是哨兵值**，表示「极其严格的全局死命令」，与 0-100 正常区间共存。
   注释（`:52`）标注这是 v3 相对 v2 的改动：原来是
   `importance: "high"|"medium"|"low"` 三档枚举，改成数值后才能做细粒度排序和阈值过滤。
2. **`timestamps: string[]` 是数组不是单值** —— 因为合并（merge）会把多条记忆的时间戳
   全部保留，形成「这条记忆由哪几个时刻的对话共同支撑」的证据链。
3. **`source_message_ids`** 让每条记忆可以回溯到 L0 原始消息，这是「记忆可审计」的基础。

### 1.2 L2：`SceneBlock` —— LLM 直接读写的 Markdown

不是数据库记录。`MemoryCore/src/core/scene/` 下 5 个文件全部围绕「管理一个
`scene_blocks/` 目录」：

- `scene-extractor.ts` —— LLM 用 `read`/`write`/`edit` 工具在沙箱内整合场景
- `filename-normalizer.ts` —— 工程侧兜底 LLM 起的文件名
- `scene-index.ts` / `scene-navigation.ts` —— 建索引 / 生成导航段

### 1.3 L3：`persona.md` —— 单文件，无 TS 类型

`persona-generator.ts` 让 LLM 直接写文件，**schema 只存在于 prompt 模板**
（`prompts/persona-generation.ts:94-132`）。

### 1.4 层间「提炼/晋升」的触发方式

**关键结论：三种触发机制完全不同，且都不是定时任务。**

| 晋升 | 触发 | 判据 | 位置 |
|---|---|---|---|
| L0 → L1 | 事件驱动（对话归档） | 正则质量闸门 `shouldExtractL1()` | `utils/sanitize.ts:135` |
| L1 → L2 | L1 完成后级联 | `cascadeSchedule` | `services/pipeline-worker.ts:530` |
| L2 → L3 | **带外信号 + 5 条计数规则** | 见下 | `persona/persona-trigger.ts:35-96` |

L2→L3 的 5 条优先级规则（短路返回）：

| 优先级 | 条件 |
|---|---|
| P1 | `cp.request_persona_update` —— **L2 在 text output 里吐 `[PERSONA_UPDATE_REQUEST]` 标记** |
| P2 | 冷启动：`scenes_processed>0 && !hasGeneratedPersona && hasSceneFiles` |
| P2.5 | **恢复**：`hasGeneratedPersona && hasSceneFiles && !hasPersonaBody` |
| P3 | `scenes_processed === 1 && memories_since_last_persona > 0` |
| P4 | `memories_since_last_persona >= interval` |

**P1 的带外信号机制是本项目最干净的解耦设计**：L2 想触发 L3 时不改状态、不调 API，
只在自己的文本输出里吐一个标记，由工程侧 `parsePersonaUpdateSignal()` 解析
（`scene-extractor.ts:79-93`）。L2 失败不会污染 L3 状态。

**P2.5「恢复」分支**是自愈设计：`hasPersonaBody()` 会先 `stripSceneNavigation`
再判空（`:119-135`），检测到 `persona.md` 正文丢失（只剩导航段）就重建。

---

## 2. 记忆写入路径

完整链路（4 次 LLM 调用中的前 2 次）：

```
对话归档 → [正则闸门] → L1 抽取(LLM#1) → 候选召回 → L1 去重(LLM#2) → 应用决策 → 双写
```

### 2.1 抽取（LLM#1）：情境切分 + 记忆提取一次调完

`l1-extractor.ts:387` `callLlmExtraction()`。输出契约（`:38-49`）：

```ts
interface SceneSegment {
  scene_name: string;
  message_ids: string[];
  memories: Array<{ content; type; priority; source_message_ids; metadata }>;
}
```

**一次调用同时完成两件事**（切分 + 抽取），而不是拆两次。理由在 prompt 里很清楚：
场景边界的判断本身就依赖记忆内容，拆开会丢信息。

### 2.2 候选召回：三层降级

`l1-dedup.ts:206/266` —— **向量 → FTS5 → skip**。
即向量库不可用降级全文检索，全文也不可用就跳过去重直接存。

### 2.3 去重（LLM#2）：4 动作分类器，支持多对多合并

`l1-writer.ts:114-137`：

```ts
export type DedupAction = "store" | "update" | "merge" | "skip";

export interface DedupDecision {
  record_id: string;
  action: DedupAction;
  target_ids: string[];          // 多对多合并
  merged_content?: string;
  merged_type?: MemoryType;
  merged_priority?: number;
  merged_timestamps?: string[];
}
```

**冲突消解就是 `update` / `merge` 两个动作**：同一事实前后矛盾时，LLM 判定为 `update`
（新覆盖旧）或 `merge`（合成一条并保留全部 timestamps）。
这是**语义级冲突消解**，不是「后写覆盖先写」。

### 2.4 版本号 / 乐观锁

`MemoryRecord.version` 是**单调递增计数**（新建=1，update/merge +1），
注释明确写了用途是「merge history tracking」。

注意：这是**审计用的版本轨迹**，不是并发控制的 CAS 乐观锁。
真正的并发保护在任务队列层（单飞 + ACK）。

### 2.5 抗幻觉硬措施（写入前的三道防线）

1. **空 `record_id` 直接判定为幻觉丢弃**（`l1-dedup.ts:358-361`）
2. **缺失决策的记忆补默认 `store`**（`:381-390`）—— 保证决策集完备，
   LLM 漏答不会导致记忆静默丢失
3. **LLM 回填的时间戳一律用原始值覆盖**（`offload-task-executor.ts:185-189`）：

```ts
// Overwrite entry timestamps with original tool pair timestamps (don't trust LLM output)
for (const entry of newEntries) {
  const origTs = tpTimestampMap.get(entry.tool_call_id);
  if (origTs) entry.timestamp = origTs;
}
```

### 2.6 写入失败策略：全 fail-soft（有隐患）

| 阶段 | 解析失败后 |
|---|---|
| L1 抽取 | 返回 `[]`，静默放弃 |
| L1 去重 | 降级「全部 store」 |
| L2 场景 | 从备份恢复 `scene_blocks/` |
| L3 人格 | `return false` |

**最值得警惕**：L1 抽取失败返回 `[]` 后，向上流成 `success: true, extractedCount: 0`
（`l1-extractor.ts:215`）—— **「LLM 吐了乱码」和「这段对话确实没啥可记」
在调用方看来完全一样**，质量损失静默不可观测。

---

## 3. 记忆召回路径

### 3.1 混合检索 + RRF 融合（真实现）

`MemoryCore/src/core/hooks/auto-recall.ts:727-756`：

```ts
// RRF merge: k=60 is a standard constant from the RRF paper
const RRF_K = 60;
for (let rank = 0; rank < keywordResults.length; rank++) {
  const rrfScore = 1 / (RRF_K + rank + 1);
  const existing = mergedMap.get(id);
  if (existing) { existing.rrfScore += rrfScore; }
  else { mergedMap.set(id, { rrfScore, formatable: recordToFormatable(r.record) }); }
}
// embedding 侧同样逻辑，命中同一 id 则分数相加
const sorted = [...mergedMap.entries()].sort((a,b) => b[1].rrfScore - a[1].rrfScore).slice(0, maxResults);
```

即 **score(d) = Σ_lists 1/(60 + rank_i(d))**。

关键点：**RRF 只用 rank 不用原始分数**，所以不需要归一化 BM25 分和余弦相似度这两个
不可比的量纲——这正是 RRF 相对加权求和的优势。

TCVDB 有原生 hybrid 时会短路省一次 embed（`:503-511`）。

**没有 rerank 模型**。RRF 之后直接截断 top-K，无 cross-encoder 二次排序。

### 3.2 策略选择是 fail-fast（与 Skill 侧相反）

`auto-recall.ts:470-478`：

```ts
if ((strategy === "embedding" || strategy === "hybrid") && !embeddingAvailable) {
  throw RecallErrors.configMissingEmbedding(strategy);
}
```

配了 embedding 却不可用就**报错**，不静默退化。

> 对比：Skill 检索侧是「warn + 降级 BM25」（`skill-store.ts:562-574`）。
> 同一个项目里两种相反取舍——Memory 召回质量直接影响回答正确性所以 fail-fast，
> Skill 少装几个只是错过优化所以 fail-soft。**这个区分是对的。**

### 3.3 召回预算：条数生效，字符预算**默认关闭**

`MemoryCore/src/config.ts:574-575`：

```ts
maxCharsPerMemory: num(recallGroup, "maxCharsPerMemory") ?? 0,
maxTotalRecallChars: num(recallGroup, "maxTotalRecallChars") ?? 0,
```

`applyRecallBudget`（`auto-recall.ts:835-843`）在两者都为 0 时直接原样返回。
**开箱即用唯一生效的限制是 `maxResults: 5` + 5s 超时。**

README_CN.md:234 称召回「经过条数、**字符预算**和超时限制」——字符预算这条
**文档声明但默认未启用**。

预算实现本身质量不错，有 surrogate pair 安全截断（`:906-916`）：

```ts
// Count and slice by code point, not UTF-16 code unit, so a cut never lands
// between the halves of a surrogate pair
const cps = Array.from(line);
```

### 3.4 三路并发拉取，任一失败不影响其他

`openclaw-plugin/src/hooks/recall.ts:36-40`：

```ts
const [searchResult, persona, scenarios] = await Promise.allSettled([
  client.searchAtomic({ query: opts.query, limit: opts.maxResults }),
  opts.includePersona ? client.readCore() : Promise.resolve(null),
  opts.includeSceneNav ? client.listScenarios({}) : Promise.resolve(null),
]);
```

### 3.5 召回结果的注入位置：按变化频率二分（见 Context 文档 §1.5）

- **stable**（persona / scene navigation / tools guide）→ system prompt 尾部，吃 cache
- **dynamic**（L1 记忆）→ user prompt 前缀，不污染 system cache

且 `<relevant-memories>` 带 prompt 级护栏：
**"以下是当前对话召回的相关记忆，不代表当前任务进程，仅作为参考"**
—— 防止 LLM 把历史记忆误当成当前任务状态。

---

## 4. 存储后端

`MemoryCore/src/core/store/` 是可插拔多后端：

| 文件 | 后端 |
|---|---|
| `sqlite.ts` | SQLite（含 FTS5 全文） |
| `tcvdb.ts` / `tcvdb-client.ts` | 腾讯云向量数据库 |
| `bm25-local.ts` / `bm25-client.ts` | BM25 全文（本地 / 远程） |
| `embedding.ts` | 向量化 |
| `store-pool.ts` | 连接池 |
| `isolation.ts` | 三维租户隔离 |
| `factory.ts` | 后端选择 |

`MemoryCore/src/core/storage/` 是**文件存储**抽象（`local-backend.ts` / `adapter.ts`），
服务于 L2/L3 的 Markdown 文件与 COS 对象存储。

**L1 是双写**：JSONL 文件 + 向量库（`l1-extractor.ts:543` → `l1-writer.ts:163`）。
JSONL 是真值来源，向量库是可重建的索引。

SQLite 配置：WAL + `busy_timeout=5000`，读连接 LRU 池上限 300，写连接绕过池且事务包裹。

---

## 5. Skill：文件型记忆的管理

### 5.1 数据结构 —— **必填字段只有 2 个**

`MemoryCore/src/core/skill/skill-format.ts:99-130` 解析 frontmatter：

```ts
return {
  frontmatter: {
    name: fm.name,              // 必填，NAME_REGEX 校验
    description: fm.description, // 必填
    category:   typeof fm.category   === "string" ? fm.category   : undefined,
    created_at: typeof fm.created_at === "string" ? fm.created_at : undefined,
    updated_at: typeof fm.updated_at === "string" ? fm.updated_at : undefined,
    source:     fm.source === "auto" || fm.source === "manual" ? fm.source : undefined,
    resources:  parseResources(fm.resources),
  },
  body,
  raw,
};
```

`name` 或 `description` 为空直接 throw（`:96`、`:107`）。

### 5.2 ⚠️ 「触发边界」文档声明但未落地

README_CN.md:87 说 Skill「有版本、资源文件、**触发边界**、执行步骤和验证规则」。

grep 全仓库 `触发边界` → **4 处命中，全部是 markdown**
（`ROADMAP_CN.md:65`、`CHANGELOG.md:23,120`、`README_CN.md:87`）。
代码里**没有任何 `when_to_use` / `whenToUse` / `trigger_boundary` 字段**。

真相：「触发边界」只是 SKILL.md **正文里一个约定俗成的 `## When to use` 章节标题**
（由 prompt 模板 `skill-review-prompt.ts:161-163` 要求 LLM 写），**引擎从不解析它**。

**「执行步骤」「验证规则」同理** —— 都是正文里的自由文本章节，不是结构化字段。

真正结构化的只有：`name` / `description` / `category` / `created_at` / `updated_at` /
`source` / `resources`。

### 5.3 装载判定：BM25 + prompt 硬指令，无 embedding 无 LLM

`skill-store.ts:562-574` —— `mode` 参数是「宣告了但没实现」：

```ts
// mode 透传：当前 store 仅实现 BM25 路径。
const requestedMode = opts.mode ?? "bm25";
const wantsVec = requestedMode === "embedding" || requestedMode === "hybrid";
if (wantsVec && (!this.vecAvailable || !opts.queryEmbedding)) {
  this.logger?.warn(`[skill-store] search mode='${requestedMode}' downgraded to 'bm25' ...`);
}
// pure embedding 路径暂未实现 → 仍回 BM25；hybrid 同样回 BM25（后续 RRF 融合）。
```

最终「装不装载」交给主模型判断，靠 prompt 硬指令
（`prompts/skill-listing-prompt.ts:11-26`）：

```
Before replying, scan the skills below. If a skill matches or is even partially relevant
to your task, you MUST load it with skill_view(name) and follow its instructions.
Err on the side of loading — it is always better to have context you don't need
than to miss critical steps, pitfalls, or established workflows.
```

注入的 listing 只有 `- ${name}: ${description}` 一行。
**所以 description 写得好不好直接决定触发准确率 —— 这是 Skill 系统的真正命门。**

### 5.4 入库闸门：五分类 + 100 分制（最值得学的部分）

`prompts/skill-review-prompt.ts:95-149`：先五分类
（**Skill / Memory / Wiki / Code-Graph / Temporary Context**），
只有 Skill 才可入库；再四维打分（30/25/20/25），
要求**总分 ≥72 且单项 ≥12**。

五分类恰好是「四类资产 + 垃圾桶」，与系统资产模型严格对齐——
「不属于我这层」的内容有明确去处，而不是硬塞或丢弃。

详见[意图识别文档 §7](TencentDB-Agent-Memory_意图识别与任务分解分析.md)。

---

## 6. 遗忘 / 压缩 / 淘汰

**结论：L1 没有 TTL、没有 LRU、没有自动淘汰。** 控制记忆总量靠三个别的机制：

| 机制 | 层 | 做法 |
|---|---|---|
| **写入端节流** | L1 | 抽取 prompt 强制「宁缺毋滥」+ 正则质量闸门 + `priority<50` 建议丢弃 |
| **去重合并** | L1 | 4 动作分类器把重复/矛盾记忆 merge 掉，而不是各存一份 |
| **场景容量上限** | L2 | 分级预警：≥maxScenes 必须先 MERGE，=maxScenes-1 只能 UPDATE |
| **注入端截断** | L3/L2 | L3 全文截断 6000 字符，L2 只注 summary 200 字符 |

L2 的容量预警是**在 prompt 里对 LLM 施压**（`prompts/scene-extraction.ts:116-127`），
红/橙/黄三档，并配 few-shot 强调「不执行删除 = 文件总数不减少 = 合并无效」。

**真正的 LLM 压缩在 Offload 子系统**（不是记忆层）：4 级阶梯
（0.5/0.85/0.95）+ score 驱动的 tool_result 摘要替换。详见 Context 文档 §3。

**没有「重要性衰减」** —— `priority` 是抽取时一次性打的，不随时间衰减。

---

## 7. 多 Agent / 多 Team 共享与 ACL

### 7.1 三维租户隔离

`MemoryRecord` 的 `teamId` / `userId` / `agentId` 三个字段
（`l1-writer.ts:85-97`），注释说明了灰度策略：

> `userId` / `agentId` are mandatory for new writes once gateway-level isolation
> enforcement is on, but kept optional on the type to avoid breaking pre-isolation
> call sites and tests during rollout. The SQLite upsert defaults them to '' if missing;
> the migration script backfills existing rows with `__legacy__`.

用 `__legacy__` 回填存量行是个务实做法：可以区分「新写入漏填」和「历史数据」。

### 7.2 L1 是 agent 维度跨 session 的

`MemoryCore/src/gateway/v2-router.ts:1152-1161`：

```ts
// L1 召回为 agent 维度（跨 session）：filter 只取 team/user/agent/task，
// **不带 sessionId**，否则会把其它 session 写入的 L1 记忆过滤掉
```

**这是「记忆」区别于「会话历史」的本质** —— 带 sessionId 过滤就退化成了会话历史。

### 7.3 文件层隔离：禁止回退到未隔离根目录

`auto-recall.ts:166-174` persona / scene 按 `team+agent` 分目录，注释强调：

> Recall resolves the same scope and never falls back to the unscoped data root,
> preventing cross-scope profile reads.

### 7.4 跨 Agent 记忆借用：ACL + 来源标注

`resolveFixedAssetCtxs` 返回 `[self, ...借入≤2]`，注入 prompt 时**分段标注来源**
（`tdai-profile-memory-injector.ts:102-104`）：

```ts
const tag = g.ctx.isSelf ? "self" : "imported_from";
lines.push(`<agent name=${JSON.stringify(g.ctx.agentName)} role=${JSON.stringify(tag)} agent_id=${JSON.stringify(g.ctx.agentId)}>`);
```

借入记忆走 ACL 校验（`TdaiL1RecallInjector` 的 `aclClient` 参数）。
控制面不可达时**降级为只查当前 agent**（fail-closed，正确的方向）。

### 7.5 per-user 能力开关

`MemoryProxy/src/tdai/capabilities.ts` 四个布尔开关
（`skill` / `llm_wiki` / `code_graph` / `chat_memory`），
从 `/v3/meta/config/user/get` 拉取，**失败一律 fail-open 返回全开**。

> 注意这里和 §7.4 的 fail-closed 是**相反**的取舍：能力开关拉取失败 → 全开
> （避免拉不到配置就整个不能聊天）；ACL 拉取失败 → 只查自己（避免越权）。
> 两者都对，因为前者的失败后果是「多注入点东西」，后者是「泄漏别人的记忆」。

### 7.6 ⚠️ private / team / restricted 三级可见性

README_CN.md 描述「`private` 严格属于 Owner；`team` 面向全队；
`restricted` 通过 User / Role / Agent ACL 精确授权」。

这套三级模型主要落在 **MemoryPanel 的资产管理面（Wiki / Skill / CodeGraph 的
visibility 字段 + Owner 标记）**，而 **L1 Chat Memory 的隔离走的是上述三维租户
字段 + 借用 ACL**，两套机制并存、不共享代码。阅读时不要把两者混为一谈。

---

## 8. 「真实实现」vs「文档声明但未落地」

### ✅ 真实实现且质量高

- `MemoryRecord` 完整 17 字段含溯源 / 时间戳轨迹 / 版本 / 三维租户
- 4 动作去重分类器（store/update/merge/skip）+ 多对多合并
- RRF 混合检索（k=60，真实现）
- 候选召回三层降级（向量 → FTS5 → skip）
- L2→L3 带外信号触发 + P2.5 自愈恢复
- 抗幻觉三道防线（空 id 丢弃 / 补默认决策 / 时间戳覆盖）
- 可插拔多存储后端（SQLite / TCVDB / BM25 本地远程）
- Skill 五分类闸门 + 100 分制评分
- 三维租户隔离 + 跨 Agent 借用 ACL + 来源标注

### ⚠️ 文档声明但未落地

1. **Skill 的「触发边界」** —— 无字段、无解析，只是正文章节标题。
   「执行步骤」「验证规则」同理。
2. **Skill 检索的 `mode: "hybrid"|"embedding"`** —— 永远降级 BM25（至少有 warn）。
3. **召回的「字符预算」** —— 代码存在但默认值 0 = 关闭。
4. **`charBudgetPercent`** —— 定义 + 赋默认值，**零消费点**。
5. **`searchMemoriesWithDetails`**（`auto-recall.ts:397-422`）—— 导出但无调用者。
6. **`TdaiL1RecallInjector`** —— 完整实现，零注册（**但这是有意下线**，为保 prompt cache）。
7. **`Atom` / `Scenario`** —— 纯文档词汇，代码中零出现。

### 🐛 真实缺陷

1. **L1 抽取失败与「无可抽取」不可区分** —— 都表现为
   `success:true, extractedCount:0`，质量损失静默。
2. **重试不改 prompt** —— `reEnqueue` 只加 `retryCount` 且无人读取，
   3 次重试发送逐字节相同的 prompt，确定性失败必然全败。

---

## 9. 可迁移到其他 Agent 系统的记忆设计经验

1. **记忆必须可溯源：每条记忆记住它是从哪几条原始消息来的。**
   `source_message_ids` + `timestamps[]` 让「这条记忆凭什么成立」可审计，
   出错时能回到原文核对，而不是只能删掉重来。

2. **「重要性」用数值不用枚举，并留一个哨兵值给硬约束。**
   v2 的 `high/medium/low` 升级成 `priority: 0-100` 才能做阈值过滤和排序；
   `-1` 单独表示「死命令」，让不可协商的规则不参与普通排序竞争。

3. **冲突消解要有独立的语义动作，不能靠「后写覆盖」。**
   `store/update/merge/skip` 四动作 + 多对多 `target_ids`，
   让「同一事实前后矛盾」由 LLM 判定合并方式，而不是时间戳决定谁赢。

4. **写入端节流比读取端淘汰更重要。**
   本项目没有 TTL / LRU，靠抽取 prompt 的「宁缺毋滥」+ 去重合并 + 场景容量上限控制总量——
   不让垃圾进来，比进来后再淘汰便宜得多。

5. **分层记忆要有分层的注入策略：稳定的直注、庞大的注索引、易变的给工具。**
   L3 全文 / L2 只注 summary+path / L1&L0 只给检索工具——
   「可访问」不等于「必须注入」。

6. **召回融合用 RRF 而不是加权求和。**
   RRF 只用 rank，天然回避了 BM25 分数与余弦相似度量纲不可比的问题，且无需调权重。

7. **fail-fast 与 fail-soft 要按「失败后果」而非「统一风格」来选。**
   Memory 召回配了 embedding 不可用就报错（影响答案正确性）；
   Skill 检索降级 BM25（只是少装几个）；ACL 失败只查自己（防泄漏）；
   能力开关失败全开（防不能用）。四处四种，每一处都对。

8. **跨主体共享的记忆必须在 prompt 里标注来源。**
   `<agent name=... role="imported_from">` 让 LLM 知道这条经验不是自己的，
   可以打折采信，而不是当成第一手事实。

9. **让「记忆」和「会话历史」在检索过滤条件上区分开。**
   L1 召回刻意**不带 sessionId** —— 带上就退化成会话历史，
   跨 session 复用这个核心价值就没了。

10. **永不采信 LLM 回填的可校验字段。**
    时间戳用原始值覆盖、文件名做路径穿越校验、空 id 判定幻觉——
    凡是系统自己知道正确答案的，就别问模型。

11. **给「记忆文件损坏」准备自愈路径。**
    P2.5 分支专门检测「persona.md 只剩导航段、正文丢了」并重建——
    文件型记忆一定会因为 LLM 写坏/并发/中断而损坏，要假设它会坏。

12. **降级时必须留下可观测的区分标记。**
    本项目的反面教材：抽取失败返回 `[]` 与「确实没啥可记」完全同形，
    静默劣化整整潜伏一个版本才被发现。
