# OpenClaw 记忆管理分析(Memory / MEMORY.md)

> 分析日期: 2026-08-13
> 源码路径: `/usr/local/LsmGitOpenSource/openclaw`
> 项目定位: 开源个人 AI 助手 —— 多渠道消息接入(Telegram/Discord/Slack 等 40+ 渠道)、自主执行系统与日常任务,TypeScript / pnpm monorepo
> 分析目的: 提取记忆管理与 Memory.md 文件管理的设计模式,为狼人杀 Agent 持久化记忆系统(§131)提供对标参考

## 0. 执行摘要

OpenClaw 的记忆系统是一个**「纯 Markdown 文件 + SQLite 索引」的双层架构**,核心设计信条是 **"No hidden state"**(模型只能记住写到磁盘文件里的东西)。它在三层维度上做了非常深入的工程化:

1. **分层信任模型**(tier + provenance):`MEMORY.md`/`USER.md`(策展核心层,永远注入)、`memory/YYYY-MM-DD.md`(情景层,只检索不注入)、`DREAMS.md`(人类审阅层)、standing intents(前瞻记忆,SQLite)。每一层有不同的写入者、信任级别和注入行为。
2. **写入路径即安全边界**:记忆投毒(memory poisoning)不靠内容检测防御,而是靠在**写入时打不可伪造的 provenance 标签**(SQLite 列,非文本)+ 提升(promotion)门槛做**结构性隔离**。
3. **Dreaming 后台整合管线**:受 Generative Agents(arXiv:2304.03442)和 sleep-time compute(arXiv:2504.13171)启发的三阶段(light→REM→deep)后台 sweep,把短期回忆信号通过**确定性门槛 + 受限 LLM 整合**逐步「毕业」进 `MEMORY.md`。

## 1. 记忆系统整体架构

### 1.1 模块划分与目录结构

```
openclaw/
├── src/
│   ├── memory/root-memory-files.ts            # MEMORY.md 根文件的定位/修复/遗留名处理
│   ├── memory-host-sdk/                       # 主仓库侧 facade(dreaming/event-store/host 类型)
│   ├── plugin-sdk/
│   │   ├── memory-host-core.ts                # artifact 发现 + 事件导出(materialize/ownership 校验)
│   │   ├── memory-host-markdown.ts            # "managed block"(<!-- start/end --> 生成区块)维护
│   │   └── memory-host-search[.runtime].ts    # memory_search 宿主侧入口
│   ├── agents/
│   │   ├── memory-search.ts                   # ResolvedMemorySearchConfig 解析(全部默认值)
│   │   ├── memory-prompt-prepare.ts           # system prompt 的 Memory Recall 段落
│   │   ├── memory-write-provenance.ts         # 文件写入 provenance 观察器(写前快照 + 回滚)
│   │   ├── project-memory-scope.ts            # 仓库身份(git origin → project key)解析与缓存
│   │   ├── project-memory-bootstrap.ts        # per-turn 项目记忆块注入
│   │   ├── bootstrap-files.ts / bootstrap-cache.ts / bootstrap-budget.ts
│   │   └── system-prompt.ts                   # contextFiles 拼进 system prompt
│   └── auto-reply/reply/
│       ├── memory-flush.ts                    # shouldRunMemoryFlush 门控(token 阈值)
│       └── agent-runner-memory.ts             # flush turn 执行、错误处理、模型 override
├── packages/memory-host-sdk/src/
│   └── host/
│       ├── internal.ts                        # chunkMarkdown(切分)、splitCuratedMarkdownEntries
│       ├── memory-schema-base.ts              # SQLite DDL(meta/sources/chunks/state/embedding_cache)
│       ├── memory-schema-fts.ts               # FTS5 虚表 + triggers
│       ├── memory-schema-recall.ts            # recall metadata(importance/triggers/project_key)
│       ├── memory-schema-provenance.ts        # provenance(origin_class/session_kind/observed_at/supersedes_key)
│       ├── query-expansion.ts                 # 会话式查询关键词抽取(中英 stop words)
│       └── curated-annotations.ts             # <!-- trigger/importance/project --> 注释解析
└── extensions/
    ├── memory-core/                           # ★ 默认内建记忆插件(builtin engine)
    │   └── src/
    │       ├── tools.ts                       # memory_search / memory_get 工具实现(835 行)
    │       ├── flush-plan.ts                  # buildMemoryFlushPlan(预压缩 flush 计划)
    │       ├── memory-budget.ts               # compactMemoryForBudget(MEMORY.md 磁盘预算 10K 字符)
    │       ├── dreaming-phases.ts             # light/REM/deep 三阶段(1589 行)
    │       ├── dreaming-consolidation.ts      # 受限 LLM 整合 + 结构化校验(661 行)
    │       ├── short-term-promotion[-apply].ts# 短期回忆 store + 打分排序 + 写 MEMORY.md(原子 rename)
    │       ├── session-ingestion.ts           # 会话转录摄入情景层
    │       ├── standing-intents[-tool].ts     # intent 工具(前瞻记忆)
    │       └── memory/
    │           ├── manager.ts                 # MemoryIndexManager(701 行)
    │           ├── manager-search.ts          # searchVector / searchKeyword / searchPathKeyword(1070 行)
    │           ├── hybrid.ts                  # 混合合并:0.7*vec + 0.3*bm25 → decay → importance → project → MMR
    │           ├── temporal-decay.ts          # 指数时间衰减(默认 30 天半衰期)
    │           ├── importance.ts              # 重要性乘子 0.75+0.05*n (n∈[1,10])
    │           └── mmr.ts                     # MMR 多样性重排(λ=0.7,Jaccard)
    ├── active-memory/                         # 升级车道:阻塞式回忆 sub-agent
    │   ├── escalation.ts                      # hasRecallIntent(多语言正则意图识别)
    │   └── trigger-recall.ts                  # 触发短语预过滤(阈值 0.65,最多注入 3 条)
    ├── memory-lancedb/                        # 可选 LanceDB 引擎插件(per-agent 行级隔离)
    └── memory-wiki/                           # 知识 wiki 编译层
```

### 1.2 五层 tier 模型(出自 `docs/concepts/memory-architecture.md`)

| Tier | 表面 | 写入者 | 注入 |
|---|---|---|---|
| Instructions | `AGENTS.md` 等工作区指令文件 | 仅人类 | 永远,会话开始 |
| Curated core | `MEMORY.md`、`USER.md` | Dreaming 整合;用户直接请求 | 永远,会话开始,有预算 |
| Episodic | `memory/YYYY-MM-DD.md` 日记、会话转录 | Agent 工作中、memory flush、转录摄入 | **从不**;按需可检索 |
| Prospective | Standing intents(SQLite)+ cron | `intent` 工具;定时任务 | 仅触发命中时 |
| Review | `DREAMS.md`、dreaming 报告 | Dreaming 各阶段 | 从不;供人类阅读 |

## 2. MEMORY.md 类文件的管理方式

### 2.1 四类记忆文件

- **`USER.md`**(可选)—— 用户模型层:稳定偏好、沟通风格、活跃项目,**祈使句指令格式**("Always"/"Never"/"Prefer"),每条带 observed-date + active/superseded 元数据;偏好变化时**原地 supersede 而非追加矛盾指令**(依据 PrefEval, ICLR 2025:追加式偏好历史会让模型按旧值回答)。注入预算独立且更小:**4000 字符**。
- **`MEMORY.md`** —— 长期记忆:持久的非画像事实与决定。**唯一主写入者是 dreaming 整合 pass**;人类可直接编辑(operator 信任边界内)。
- **`memory/YYYY-MM-DD.md`** —— 日记工作层:详细观察、会话摘要。可检索但**永不自动注入**。文件名正则:`memory/(\d{4})-(\d{2})-(\d{2})\.md`。
- **`DREAMS.md`** —— Dream Diary,人类审阅面:每次整合的摘要、preimage 记录、grounded backfill 条目。

### 2.2 创建/更新路径

1. **直接写入**:Agent 工作中通过普通文件写工具追加到日记文件。
2. **Memory flush(预压缩冲刷)**:在自动 compaction 摘要对话**之前**,系统跑一个静默 turn(`buildMemoryFlushPlan`),提示词强制约束:只写 `memory/YYYY-MM-DD.md`、已存在则**仅追加**、`MEMORY.md`/`DREAMS.md`/`AGENTS.md` 该 turn **只读**、无事可记回复 `SILENT_REPLY_TOKEN`、可为 flush turn 单独指定模型(如本地小模型)。
3. **Dreaming deep phase 提升**(见 §2.4)。

### 2.3 索引(`MemoryIndexManager`)

- 索引来源:`MEMORY.md` + `USER.md` + `memory/*.md`(可选 + extraPaths;可选 + session transcripts)。
- **切分** `chunkMarkdown`:默认 **400 token/块、80 token 重叠**(字符估算;CJK 加权两段切分);`perEntry` 模式按 curated entry(列表项/标题)对齐边界,同一 entry 的所有碎片继承完整注释区间(防止项目作用域泄漏)。
- **触发重建**:文件 watcher 1.5s debounce;embedding provider/model/chunking/sources 任一变化 → index identity 不匹配 → **暂停向量检索并提示 `memory index --force`**(而不是自动重 embed 全部 / 静默混用两个 embedding 空间)。
- **影子库重建**:全量重建在影子 DB 中进行,完成后事务内核对 `memory_index_state.revision`(乐观并发)再整表替换 + 重建 FTS 虚表;超 24h 孤儿影子库自动清理。

### 2.4 Flush / Compaction / Consolidation 机制(全系统最精华,分四层)

**(a) Pre-compaction memory flush** —— 触发门控 `shouldRunMemoryFlush`:`totalTokens >= contextWindow - reserveTokensFloor(默认 20K) - softThresholdTokens(默认 4K)`;每个 compaction 周期只 flush 一次(按 `compactionCount` 判重);另有转录大小(默认 2MB)强制触发路径。

**(b) Dreaming 三阶段 sweep**(默认开,cron 自动对账):

- **Light**:读近期短期回忆 store + 日记 + 脱敏会话转录;去重、暂存候选;写 `## Light Sleep` managed block;**不写 MEMORY.md**。
- **REM**:主题/反思摘要,写 `## REM Sleep` block;记录 REM 强化信号供 deep 排序;**不写 MEMORY.md**。
- **Deep**:打分 + 门槛 + 提升。候选排序信号:回忆频次(`log1p(count)/log1p(10)`)、平均检索相关分、查询多样性(unique query hash)、recency(半衰期衰减)、多日重现、概念丰富度、light/REM 相位 boost。**确定性门槛**:`minScore(0.75)`、`minRecallCount(3)`、`minUniqueQueries(3)` 必须全部通过;`originClass ∈ {untrusted, system}` 的候选**结构性排除**(不是扣分,是前提 —— "任何召回频次都不能把 untrusted 内容推进策展核心")。

**(c) 受限 LLM 整合(consolidation)** `dreaming-consolidation.ts::consolidateMemory`:

- 按 project key 分组,每组一个 subagent run(60s 超时,失败即回退);
- System prompt 硬约束:候选的 `resultEntry` 由代码预生成,LLM **不得自创文本**,只决定每个候选是 `added`/`merged`/`superseded` 并给出 `priorEntries` 证据;
- `validateConsolidatedMemory`(212 行)**逐条结构化校验**:操作数 == 候选数、resultEntry 与代码预生成值**逐字节相等**、Source 引用存在、merged 的先验条目必须与候选语义同一事实(剥离注释后比对)、superseded 必须有 `supersedesKey` 血统、`lostFraction <= maxPriorEntryLossFraction(默认 0.25)`、输出 ≤ `memoryFileMaxChars`、最终条目多重集合与操作**精确对账**;
- 任一校验失败 → 该组**回退 append-only**。

**(d) 写入安全(乐观并发 + 原子替换)** `applyShortTermPromotions`:

- 文件锁 + 短期 store 锁双层;锁内重读 store 与源文件指纹,已变化/已提升的候选剔除;
- `consolidationBaseMemoryHash` 在整合输入构建时捕获,rename 前**复核 hash 未变**,否则放弃 rewrite 走 append 回退;
- 接受的 rewrite 先存 **preimage** 到 SQLite plugin state,再原子 rename;
- 磁盘预算 `DEFAULT_MEMORY_FILE_MAX_CHARS = 10_000`,`compactMemoryForBudget` 只丢弃**最早日期且结构完全匹配 promotion 标记**的自动生成段;**人类写的内容、任何混合/歧义结构无条件保留**;宁可全部丢弃 promotion 段也要写入新段("拒绝新写入会静默吞掉最新材料")。

## 3. 记忆存储层

### 3.1 双层存储:Markdown 是事实源,SQLite 是派生索引

- **事实源(canonical)**:workspace 内纯 Markdown 文件,文本编辑器可查看/编辑;迁移"按构造无损"——只重建派生状态。
- **索引(derived)**:per-agent SQLite(`~/.openclaw/agents/<agentId>/agent/openclaw-agent.sqlite`),WAL 模式。

### 3.2 SQLite Schema

```sql
memory_index_meta(key TEXT PRIMARY KEY, value TEXT)            -- providerKey、vectorDims 等索引身份
memory_index_sources(id, path, source, hash, mtime, size, UNIQUE(path, source)) STRICT
memory_index_chunks(id TEXT PK, path, source, start_line, end_line,
                    hash, model, text, embedding, updated_at) STRICT  -- embedding 以 JSON 文本存储
memory_index_chunk_recall_metadata(chunk_id, importance,        -- 可空;NULL = 中性,老索引兼容
                                   triggers, project_key)
memory_index_chunk_provenance(chunk_id, origin_class,           -- 信任元数据,模型无法通过文本改写
                              session_kind, observed_at, supersedes_key)
memory_index_state(id CHECK(id=1), revision)                    -- 乐观并发版本号
memory_embedding_cache(provider, model, provider_key, hash,     -- 跨文件 embedding 缓存
                       embedding, dims, updated_at, PK(...))
-- 虚表:
memory_index_chunks_fts  USING fts5(...)   -- BM25
memory_index_paths_fts   USING fts5(...)   -- 路径检索
memory_index_chunks_vec  (sqlite-vec vec0, KNN 索引,可选扩展,失败回退进程内 cosine)
```

- **provenance 四元组**:`originClass ∈ {owner, agent, untrusted, system}`(闭集)、`sessionKind ∈ {interactive, cron, heartbeat, subagent, unknown}`、`observedAt`、`supersedesKey`。分类保守:无法确定来源的外部内容一律 `untrusted`,**永不默认 owner**。
- **召回元数据**:`importance`(1-10,写入时由有模型在环的写入者打分)、`triggers`(触发短语)、`projectKey`(git origin 归一化)。全部可空,NULL=中性,老索引向后兼容。

### 3.3 向量存储

- 首选 **sqlite-vec** 做 in-DB KNN(候选过采样 8×);扩展加载失败自动回退**进程内 cosine 全表扫描**,但做防主线程阻塞:按 256 行分批 + rowid 游标 + 每批 `setImmediate` 让出事件循环(issue #81172)。
- `memory_embedding_cache` 按 `(provider, model, provider_key, hash)` 缓存,换 provider 不重复算。

## 4. 记忆检索机制

### 4.1 三路并行 + 确定性排序

```
Query ─→ Embedding ─→ Vector search ─┐
      ─→ Tokenize  ─→ BM25 search  ─┼→ Weighted merge → Recency×Importance → MMR → Top
      ─→ Path FTS(exact path 优先)─┘
```

- **BM25 关键词**:FTS5 `bm25()` rank → 负 rank 映射 `relevance/(1+relevance)`;短 CJK 词(<3 字符)走 trigram/LIKE 子串补丁;MATCH 失败自动降级 LIKE 兜底并 warn。
- **路径检索**:路径与正文**分开建 FTS**;`ExactPathSpecificity` 4 级(完整路径 > basename > stem > 部分),精确文件名层级**优先于一切内容分**;注册 JS SQL 函数做 Unicode 感知判定。
- **混合合并** `mergeHybridResults`:`score = 0.7×vectorScore + 0.3×keywordScore`(单路时单独跑)→ 时间衰减 → 重要性乘子 → 项目亲和 → MMR。
- **查询扩展**:从会话式查询("那个我们昨天讨论的东西")抽关键词,内置中英 stop words。

### 4.2 排序三乘子(全部零查询期模型调用)

1. **时间衰减**:`score × exp(-λ×ageDays)`,λ=ln2/30(30 天半衰期)。**只对 dated 日记生效**;`MEMORY.md`/`USER.md` 是 evergreen 永不衰减。
2. **重要性乘子**:`0.75 + 0.05×n`,n∈[1,10] **写入时**打分,查询期零模型调用实现 Generative Agents 的 relevance×recency×importance。
3. **项目亲和**:会话保持最多 4 个活跃 repo key(MRU,不持久化);在活跃集 → boost,其他项目 → 温和 demote,未标注 → 中性。

### 4.3 MMR 多样性

- `λ×relevance - (1-λ)×max_sim_to_selected`,λ=0.7,Jaccard over snippet tokens;O(k²) 但有界(每路 24 候选);FTS-only / vector-only 回退路径**不跑** MMR。

### 4.4 工具 API

- **`memory_search`**:参数 `query`/`maxResults`/`minScore`/`corpus ∈ {memory, wiki, all, sessions}`(闭集校验,**模型自撰参数不能扩大召回边界**);60s per-agent 冷却;`sessions` 语料按 visibility 过滤;成功后**异步**记录回忆信号喂 dreaming;返回 citations + staleness。
- **`memory_get`**:按路径 + 行区间安全读取,有界 excerpt + 截断/续读信息。
- **`intent`**:standing intents 创建/列举/取消。

### 4.5 触发式自动召回

- 写入者在条目行尾存 `<!-- trigger: phrase -->`;只取**策展层**或 promoted-trusted 条目。
- 每条入站消息跑快速词面预过滤(单词 0.85 / 多词精确序列 1.0 / 覆盖度公式),最终 `triggerScore×0.8 + 检索相关×0.2`,**阈值 0.65**(注释有完整标定理由:0.60 以下零误报、0.72 会杀掉单词概念 trigger)。
- 最多注入 3 条、≤1800 字符紧凑隐藏上下文块;**日记、转录永不自动注入 —— 这是安全属性不是调参选择**。

## 5. 记忆与 LLM 上下文的结合

### 5.1 Bootstrap 注入(会话开始 + 每轮刷新)

- 文件清单:`AGENTS.md`/`SOUL.md`/`IDENTITY.md`/`USER.md`/`BOOTSTRAP.md`/`MEMORY.md`。
- **预算**:单文件 20K 字符(USER.md 4K),全部合计 60K 字符;超限按 head 75% / tail 25% 截断。
- **超预算不删文件**:磁盘保留完整,只截断注入副本,并生成 `[Bootstrap truncation warning]` 注入 prompt + run metadata(`openclaw doctor` 可查 raw vs injected)。
- **每轮刷新**:per-session 快照缓存 + 每轮 inode/mtime 守卫检查 —— **长会话无需重启即可拾取 dreaming 整合后的新 MEMORY.md**。

### 5.2 两条召回车道

- **Lane 1(永远开,零模型调用)**:bootstrap 注入 + `memory_search` 排序检索 + trigger 注入。
- **Lane 2(escalation)**:阻塞式回忆 sub-agent。**默认只在两个确定性条件同时成立时触发**:(1) 消息显示回忆意图(`hasRecallIntent` 英/西/中/日/韩多语言正则,对未来时态话题二次确认防误触发);(2) Lane 1 无强命中。预算:主 lane 15s,连续 3 次超时熔断冷却 60s;**回忆失败永不阻塞回复**。

### 5.3 预算与截断策略汇总

| 位置 | 预算 |
|---|---|
| 单 bootstrap 文件 | 20K chars(USER.md 4K) |
| bootstrap 总计 | 60K chars |
| MEMORY.md 磁盘 | 10K chars(promotion 段 FIFO 淘汰) |
| trigger 注入 | ≤3 条、≤1800 chars |
| standing intent 注入 | ≤3 条、≤1200 chars |
| 检索 | 默认 maxResults=6、minScore=0.35、候选 4× 过采样 |

## 6. 记忆生命周期管理

### 6.1 写入时机

- **显式**:Agent 工作中追加日记;用户直接说「记住……」。
- **自动 pre-compaction flush**:每次 compaction 前最多一次。
- **会话结束转录摄入**:每 sweep ≤240 消息、最少 12 条才摄入;**只有 interactive 会话合格**(cron/heartbeat/subagent **结构性排除**);敏感内容脱敏;**已注入上下文的回忆内容被结构性标记并剔除**(防回忆回路:"一条被回忆 100 次的事实仍然是一条事实")。
- **dreaming deep promotion**:唯一写 `MEMORY.md` 的自动路径。

### 6.2 过期/修剪/去重

- **排序层衰减**:检索 30 天半衰期;promotion 打分 maxAgeDays 上限。
- **结构层淘汰**:MEMORY.md 磁盘预算 10K,promotion 段 oldest-first FIFO;USER.md 原地 supersede;`supersedesKey` 血统链让新观察**替换**旧观察(校验强制:`leaves stale lineage` 即拒绝)。
- **审计/修复**:`auditDreamingArtifacts`/`repair*`(doctor 用:失效条目、悬空条目、陈旧锁)。

### 6.3 Standing intents(前瞻记忆)

- 设计前提(引用 TriggerBench arXiv:2606.23459):前瞻回忆随上下文长度急剧退化,**把意图编译出模型**:时间型 → cron;事件型 → per-agent SQLite 表(关键词 + 可选 trigger embedding + scope + 过期 + 触发预算 + 冷却)。
- 生命周期显式状态机:`pending/armed/fired/done/cancelled/expired`。
- **反打扰是结构性的**:默认冷却 24h、默认最多触发 3 次、90 天过期、每轮最多注入 3 条 ≤1200 字符;匹配路径**零模型调用**。

## 7. 值得借鉴的设计决策与经验(12 条)

1. **"No hidden state" —— 纯文件事实源 + 派生索引**。所有记忆都是文本编辑器可查可改的 Markdown;SQLite 只是可重建的派生索引。对狼人杀项目启示:bot 记忆的人类可审阅性应优先于存储效率。
2. **写入路径即安全边界,provenance 用数据库列而非文本**。origin class 存在 SQLite 列里,"prose 声称来自 owner 不等于 owner 内容";分类保守(不确定 → untrusted/system,永不默认 owner)。这比任何内容级注入检测都可靠 —— 是防御侧对应 LLM 注入攻击的哲学。
3. **提升门槛是确定性前提而非打分项**。`untrusted`/`system` 候选在构建 prompt **之前**被结构性排除。打分、门槛、资格、生命周期全是确定性代码,LLM 只在确定性边界内做语言判断。
4. **回忆回路阻断**。凡从记忆注入进上下文的内容被结构性标记,永远不再被抽取为新记忆;session-kind gating(cron/heartbeat/subagent 不产持久候选)直接消灭「自动捕获的记忆绝大多数是脚手架复述、心跳噪声、回忆反馈回路」这一类生产事故。
5. **受限 LLM 整合 + 逐条结构化校验 + append-only 回退**。LLM 不许创作文本(resultEntry 代码预生成、逐字节比对),只做 added/merged/superseded 决策;校验覆盖条目多重集合精确对账、merged 语义一致性、superseded 血统、丢失比例上限(0.25)、预算上限;任一失败回退 append-only。这是「LLM 判断 + 确定性护栏」的最完整落地范本 —— 可比狼人杀 §131 的 `ValidateMemorySections` + `FallbackMerge` 三级,但校验粒度(逐操作、逐血统、多重集合对账)更严。
6. **乐观并发 + 原子 rename + preimage 三板斧**。整合输入构建时记 content hash → rename 前复核 → 冲突放弃 rewrite 走 append;preimage 存 SQLite;`DREAMS.md` 记录人类可读 diff。明确声明接受「毫秒级残余竞争窗口」换取「不要求每个编辑器共享一把锁」的工程取舍。
7. **索引身份不匹配时暂停而非自动重建**。换 embedding provider/model/chunking 后向量检索暂停并明确报告,等用户 `memory index --force` —— 避免静默混用两个 embedding 空间的搜索结果。
8. **显式配置 provider 失败时 fail-closed,未配置/auto 时 fail-soft**。显式指定的 provider 挂了返回 unavailable 而不是静默降级到 FTS-only(保持配置错误可见);`provider: "none"` 才表示「我故意只要关键词」。这把「配置意图」编码进了降级策略。
9. **检索排序零查询期模型调用**。relevance×recency×importance 三乘子中,importance 在写入时打一次分存列,查询期纯确定性;recency 只对 dated 日记衰减;MMR 本地有界 O(k²)。整个 Lane 1 召回路径不加任何延迟。
10. **分层信任决定注入资格而非内容审查**。自动注入仅限策展层;日记和转录永远只能显式工具或 escalation 车道到达;untrusted 命中包裹 untrusted framing;带 project key 的条目要求所有 key 都在活跃集(防跨项目泄漏)。"This restriction is a security property, not a tuning choice."
11. **项目作用域记忆用 git remote 归一化身份,且 ephemeral**。`origin` URL → `host/path`(SSH/HTTPS 克隆收敛;fork 故意分开);每会话最多 4 个活跃 key(MRU 淘汰,不持久化)—— 一个代码库里学到的 workaround 不会悄悄影响另一个库。
12. **Flush 提示词的「安全 hint 强制注入」与文件级 provenance 降级**。flush prompt 三条 REQUIRED_HINTS(目标文件/只追加/根文件只读)缺啥补啥,用户自定义 prompt 也无法移除;文件级 provenance **向最低信任坍缩** —— trusted 行在被降级的文件里故意失去提升资格,untrusted 内容不能搭 trusted 文件 hash 的便车。

**附加可参考项**:

- **多语言召回意图正则**(英/西/中/日/韩)+ 未来时态话题二次确认 —— 直接可移植到任何多语言 bot。
- **事件溯源**:`memory.recall.recorded`/`memory.promotion.applied`/`memory.dream.completed` 等事件导出 jsonl,让「什么进了长期记忆、从哪来」永远可事后审阅。
- **诊断可见性**:`memory status --deep` 把 `Vector store: unavailable`(sqlite-vec 加载)与 `Embeddings: unavailable`(provider/auth)分开报告 —— 排障时两类根因不混淆。
- OpenClaw 明确**不做**的事也值得注意:学习式 cross-encoder reranking、HyDE 查询生成被刻意排除在 builtin 引擎外("MMR 减重复但不是学习式相关性 reranker")。

## 8. 与狼人杀项目(§131)的对照要点

- OpenClaw 的 dreaming 管线 ≈ 狼人杀 `IterateAgentMemoriesAsync`,但多了**确定性门槛(recall 频次/查询多样性驱动)** —— 记忆「因为持续有用而毕业,而不是因为写得自信」,这是纯 LLM 迭代路径可补的维度。
- OpenClaw 的 `maxPriorEntryLossFraction` + 多重集合对账校验 ≈ 狼人杀 `ValidateMemorySections` + `FallbackMerge`,但校验严格度高一个量级。
- OpenClaw 的 `memory/.dreams/` 短期 store + `DREAMS.md` 人类审阅面 + preimage 三层留痕 ≈ 狼人杀目前只有 `BotTranscript` 审阅面,可借鉴「机器面/人类面/持久面」三面分离。
