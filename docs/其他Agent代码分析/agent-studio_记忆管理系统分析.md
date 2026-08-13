# Agent Studio 记忆管理系统分析

> 分析日期: 2026-08-13
> 源码路径: `/usr/local/LsmGitOpenSource/agent-studio`
> 项目定位: openJiuwen Studio — 一站式 AI Agent 开发平台,低代码/零代码可视化设计与编排
> 分析目的: 提取记忆管理设计模式,为狼人杀 Agent 持久化记忆系统提供参考

## 1. 架构总览

Agent Studio 的记忆系统采用**四层架构 + 多存储后端**,目标是支撑多租户、可插拔向量库、多模态记忆:

```
┌─────────────────────────────────────────────────────────────────────┐
│                          用户 / Agent                                │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────┐  ┌─────────────┐ │
│  │ 长期记忆     │  │ 变量记忆     │  │ 知识库   │  │ 记忆库CRUD  │ │
│  │ LongTerm     │  │ Variables    │  │ KB       │  │ MemoryBase  │ │
│  └──────┬───────┘  └──────┬───────┘  └────┬─────┘  └──────┬──────┘ │
│         └─────────────────┼───────────────┼────────────────┘        │
│                           ↓               ↓                          │
│         ┌─────────────────────────────────────────────┐              │
│         │       MemoryEngineManager (单例)            │              │
│         │  LongTermMemory.set_scope_config()         │              │
│         └────────────────┬────────────────────────────┘              │
│                          ↓                                           │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │              三存储后端注册 (register_store)                  │   │
│  │  ┌──────────┐    ┌──────────┐    ┌──────────────────┐        │   │
│  │  │ kv_store │    │db_store  │    │ vector_store     │        │   │
│  │  │ SQLAlchemy│   │ Default  │    │ milvus / chroma  │        │   │
│  │  │          │    │ DbStore  │    │ (EmbeddingConfig)│        │   │
│  │  └──────────┘    └──────────┘    └──────────────────┘        │   │
│  └──────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

## 2. 核心模块

### 2.1 `MemoryEngineManager` 单例

**位置**: `openjiuwen_studio/memory_engine_start.py`

```python
class MemoryEngineManager:
    _instance: LongTermMemory | None = None

    @classmethod
    async def init(cls):
        if cls._instance is not None:
            return cls._instance
        # ... 加载 .env、构建三存储、配置加密密钥
        memory_engine = LongTermMemory()
        await memory_engine.register_store(
            kv_store=kv_store,
            db_store=db_store,
            vector_store=vector_store
        )
        memory_engine.set_config(MemoryEngineConfig(
            default_model_cfg=ModelRequestConfig(),
            default_model_client_cfg=ModelClientConfig(
                client_provider="SiliconFlow",
                api_key="default_api_key",
                api_base="default_api_base",
                verify_ssl=False
            ),
            crypto_key=master_aes_key  # AES 加密
        ))
        cls._instance = memory_engine
```

**关键设计点**:

| 维度 | 实现 | 价值 |
|------|------|------|
| 单例模式 | `_instance` 静态字段 + `init` 守卫 | 全应用共享同一 MemoryEngine,避免重复初始化 |
| 多租户隔离 | `scope_id` (memory base id) | 通过 `set_scope_config(scope_id, cfg)` 注入每空间独立配置 |
| 加密密钥 | `master_aes_key` 走 `SERVER_AES_MASTER_KEY_ENV` 或华为云 KMS | 用户敏感数据加密,生产可对接 KMS |
| 异步初始化 | `await init()` | 启动时一次性完成,运行时同步访问 |
| 默认 provider | 兜底用 SiliconFlow | 兜底配置,任何 mdb 创建前已具备可用 client |

### 2.2 三存储后端

```python
# 1. KV 存储 - 配置/状态
kv_store = DbBasedKVStore(create_async_engine(
    async_agent_database_url,
    pool_pre_ping=True,
    echo=False,
))

# 2. DB 存储 - 记忆元数据/变量
db_store = DefaultDbStore(create_async_engine(
    async_agent_database_url,
    pool_size=20,
    max_overflow=20
))

# 3. 向量存储 - 嵌入向量(milvus 或 chroma 二选一)
if vector_db_type == "milvus":
    vector_store = create_vector_store(
        store_type=vector_db_type,
        milvus_uri=f"http://{milvus_host}:{milvus_host}",
        milvus_token=milvus_token,
        alias="memory_milvus_connection"
    )
elif vector_db_type == "chroma":
    vector_store = create_vector_store(vector_db_type, persist_directory=data_dir)
```

**关注**:
- 三存储通过**统一接口**注册到 `LongTermMemory`,业务侧只与 `LongTermMemory` API 交互
- 通过 `INDEX_MANAGER_TYPE` 环境变量切换向量库,业务代码零修改
- `pool_pre_ping` + `pool_size=20` 保证高并发下连接健康

### 2.3 `MemoryBase` (记忆库作用域)

**位置**: `openjiuwen_studio/core/manager/memory_base.py`

记忆库 = 同一业务空间下的一组**长期记忆**集合,有独立的 embedding model + LLM model 配置:

```python
async def memory_base_create(req, current_user):
    # 1. 验证用户空间权限
    check_user_space(req.space_id, current_user)

    # 2. 验证 embedding_model_config_id 存在且启用
    # 3. 生成 mdb_id = uuid.uuid4().hex
    # 4. 保存到 DB
    # 5. ★ 核心: 解析并注入 LongTermMemory 作用域配置
    memory_scope_config = _parse_to_memory_scope_config(mdb_id=mdb_id)
    await LongTermMemory().set_scope_config(
        scope_id=mdb_id,
        memory_scope_config=memory_scope_config
    )
```

**`_parse_to_memory_scope_config`** 把 DB 中的模型配置解析为:

```python
MemoryScopeConfig(
    embedding_cfg=EmbeddingConfig(...),  # 向量化配置
    model_client_cfg=ModelClientConfig(...),  # LLM 客户端(provider/api_key/base_url)
    model_cfg=ModelRequestConfig(...)  # 请求参数(temperature/top_p/max_tokens)
)
```

**CRUD 全闭环**:

| 操作 | 关键钩子 |
|------|---------|
| Create | `set_scope_config` 注册到 LongTermMemory |
| Update | `update` DB + 重新 `set_scope_config`(保证运行时一致) |
| Delete | `delete_scope_config` 清理 + `delete_mem_by_scope` 级联删除记忆 |
| Get/List/Search | 仅 DB 查询,不动运行时 |

### 2.4 长期记忆 API

**位置**: `openjiuwen_studio/core/manager/memory.py`

```python
@with_exception_handling
async def get_longterm_mem(req: SearchLongtermMem):
    memory_engine = get_memory_engine()
    memory_data = await memory_engine.get_user_mem_by_page(
        user_id=req.user_id,
        scope_id=req.group_id,   # = mdb_id
        page_size=req.num,
        page_idx=req.page,
        memory_type=safe_get_memory_type(req.memory_type)
    )
```

| API | 用途 | 内部方法 |
|------|------|---------|
| get_longterm_mem | 分页查询某 mdb 下用户的长期记忆 | `get_user_mem_by_page` |
| delete_longterm_mem | 按 mem_id 删除 | `delete_mem_by_id` |
| update_longterm_mem | 按 mem_id 更新 | `update_mem_by_id` |
| delete_longterm_mem_by_scope_id | 整 mdb 删除 | `delete_mem_by_scope` |
| get_user_variable | 读用户变量(短记忆) | `get_variables` |
| update_user_variable | 写用户变量 | `update_variables` |
| delete_user_variable | 删用户变量 | `delete_variables` |

**`safe_get_memory_type` 兜底** —— 任何 `value_str` 解析失败返回 `MemoryType.UNKNOWN`,不抛错:

```python
def safe_get_memory_type(value_str: str) -> MemoryType:
    try:
        return MemoryType(value_str.lower())
    except ValueError:
        logger.error(f"'{value_str}' 不是有效的 MemoryType 值")
        return MemoryType.UNKNOWN
```

## 3. 统一异常处理装饰器

```python
def with_exception_handling(func: Callable[..., Awaitable[Any]]) -> Callable[..., Awaitable[Any]]:
    @wraps(func)
    async def wrapper(*args, **kwargs) -> Any:
        try:
            return await func(*args, **kwargs)
        except ValidationError as e:
            log_exception(e)
            return ResponseModel(code=400, message=type(e).__name__)
        except Exception as e:
            log_exception(e)
            return ResponseModel(code=500, message=type(e).__name__)
    return wrapper
```

**`memory_base.py` 升级版** —— 同时支持同步/异步函数:

```python
def with_exception_handling(func):
    if asyncio.iscoroutinefunction(func):
        # async wrapper
    # sync wrapper
```

**价值**: 业务函数体可专注于正常路径,异常 → 统一 `ResponseModel`,router 直接返回,无需嵌套 try/except。

## 4. 与狼人杀 Agent §131 持久化记忆的对比

| 维度 | Agent Studio | 狼人杀 Agent (现有 §131) | 差距 / 可借鉴点 |
|------|--------------|-------------------------|-----------------|
| 存储后端 | SQLAlchemy + Milvus/Chroma | GORM + MySQL (单库) | 缺向量检索能力,语义回忆是字符串包含/正则 |
| 记忆分类 | MemoryType 枚举 (USER/GROUP/...) | `model_key` 唯一索引 | Agent Studio 多类型更细粒度 |
| 作用域 | `scope_id` (mdb_id) 多空间 | 单 `model_key` 全局 | 当前够用,但未来多租户时需引入 |
| 加密 | `crypto_key` AES + KMS | AES-256-GCM (§118) | 对齐 |
| 异步 | `await` 全链路 | `goroutine` 异步,持久化 worker 异步 | 对齐 |
| 异常装饰器 | `with_exception_handling` | 散落各方法 | **可借鉴: 全局错误码统一封装** |
| 作用域注入 | `set_scope_config` 实时生效 | `StartAgentsLocked` 读 DB 写内存 | 缺运行时切换能力 |
| 异步初始化 | `MemoryEngineManager.init()` 启动时 | bot 启动时按需加载 | 对齐 |
| 记忆 CRUD | 长记忆 + 变量 双通道 | 单 MEMORY.md | **可借鉴: 短记忆与长记忆分离** |

## 5. 可学习的核心设计思想

### 5.1 单一入口(Manager Singleton)

`MemoryEngineManager._instance` 全应用单例,避免每个业务调用都初始化一次 LLM/向量库 client。狼人杀 Agent 已有 `llm.NewRegistryWithDB` 全局 registry,模式一致。

### 5.2 作用域注入(scope_id)

`LongTermMemory.set_scope_config(scope_id, cfg)` 允许运行时新增/切换 mdb 配置,不影响其他空间。狼人杀 §131 当前按 `model_key` 索引,粒度粗,未来若引入"按房间"记忆,可借鉴 scope 模式。

### 5.3 兜底 + 容错(`safe_get_memory_type`)

任何外部输入枚举值都先解析,失败给默认值而非 panic。这是防御式编程典范。狼人杀 §118 模型加载时也有类似 `_tryRegisterBotUser` fallback。

### 5.4 三存储分离(KV / DB / Vector)

- KV: 配置/会话状态(高频读、低频写)
- DB: 结构化记忆(查询/分页/CRUD)
- Vector: 语义检索(相似度召回)

狼人杀 MEMORY.md 当前是单一字符串 + 4 段硬编码标题,可考虑未来引入向量库做"相似场景经验回忆"。

### 5.5 装饰器统一异常处理

`@with_exception_handling` 模式简单但有效,可推广到所有内部 manager 函数,避免每个函数都写 try/except。

## 6. 落地建议(用于狼人杀 Agent)

### 6.1 短期(直接采纳)

1. **`WithErrorResponse` 装饰器** —— 狼人杀 `WerewolfRoom` 方法如 `Action_*` 可统一收口到 `Result{code, message}`,减少 if-else 错误传播。
2. **`MemoryEngineManager` 单例** —— 狼人杀 LLM registry 已对齐,确认无重复 init 即可。
3. **Aes 加密 + 兜底** —— §118 已对齐。

### 6.2 中期(架构优化)

1. **作用域记忆** —— 引入 `room_id` 作为 memory 作用域,使"该房间的教训"与"跨房间的教训"分离。
2. **多类型记忆** —— `UserProfile`(玩家偏好)/ `GameFact`(客观事实)/ `StrategyTip`(主观经验) 三类分离。
3. **短记忆 + 长记忆分离** —— 模仿 `LongTermMemory` + `Variables` 双通道,狼人杀可分 "本局内事件"(短期) 与 "跨局经验"(长期)。

### 6.3 长期(能力扩展)

1. **向量库语义检索** —— 接入 Milvus/Chroma,按 query 相似度召回历史经验,而非"全文 grep"。
2. **Dreaming 离线整理** —— 类似 JiuwenSwarm 模式,后台 goroutine 周期性扫描历史对局摘要,提取值得长期保留的经验。
3. **多模态记忆** —— 不止文本,狼人杀可记录"投票模式""发言节奏""表情切换"等行为特征。
