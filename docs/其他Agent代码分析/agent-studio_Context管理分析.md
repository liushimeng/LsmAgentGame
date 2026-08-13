# Agent Studio Context 管理与 LLM 调用架构分析

> 分析日期: 2026-08-13
> 源码路径: `/usr/local/LsmGitOpenSource/agent-studio/backend/openjiuwen_studio`
> 分析目的: 提炼 LLM Client 抽象、Context 传递、Context 池、缓存等设计模式,为狼人杀 Agent 的 LLM 调用提供参考

## 1. 架构总览

Agent Studio 的 Context 管理采用 **三层架构 + 双协议适配**:

```
┌──────────────────────────────────────────────────────────────────┐
│                        Router / API Layer                         │
│  /routers/execution.py: SSE 流式 handler() 异步生成器            │
│  /routers/agents.py: agent_mgr 包装                              │
└────────────────────────────┬─────────────────────────────────────┘
                             ↓
┌──────────────────────────────────────────────────────────────────┐
│                       Executor Layer                              │
│  AgentRunner(run) -> Agent(compile) -> InvokableAgent            │
│  WorkflowRunner  ->  Workflow(pregel graph)                      │
│  ComponentRunner ->  单一组件                                     │
└────────────────────────────┬─────────────────────────────────────┘
                             ↓
┌──────────────────────────────────────────────────────────────────┐
│                  LLM Foundation Layer (openjiuwen.core)           │
│  Model (统一接口) + ModelClientConfig + ModelRequestConfig        │
│  Provider 适配: OpenAI / Anthropic / SiliconFlow                 │
│  Client 缓存: @lru_cache(maxsize=32)                             │
└────────────────────────────┬─────────────────────────────────────┘
                             ↓
                  HTTP / HTTPS 上游 LLM Provider
```

## 2. 核心模块

### 2.1 LLM Manager —— 唯一入口

**位置**: `openjiuwen_studio/ops/modules/llm/llm_manager.py`

```python
_config_service: LLMConfigService | None = None  # 全局单例

def init_llm_manager(config_service: LLMConfigService) -> None:
    """在应用启动时调用一次,把配置服务注入进来"""
    global _config_service
    _config_service = config_service

@lru_cache(maxsize=32)
def _create_client(base_url: str, api_key: str) -> OpenAI:
    return OpenAI(api_key=api_key, base_url=base_url)

@lru_cache(maxsize=32)
def _create_async_client(base_url: str, api_key: str) -> AsyncOpenAI:
    return AsyncOpenAI(api_key=api_key, base_url=base_url)
```

**两种获取 client 的入口**:

| 入口 | 用途 | 返回 |
|------|------|------|
| `get_openai_client(model_id, source)` | 业务侧只关心 OpenAI 协议 | `OpenAI` (sync) |
| `get_async_openai_client(model_id, source)` | 流式/异步调用 | `AsyncOpenAI` |
| `get_llm_client(model_id, source)` | 业务侧使用 openjiuwen 抽象 `Model` | `Model` (统一协议) |
| `get_llm_client_by_protocol(protocol)` | 无 model_id 场景(动态协议) | `Model` |

**`source` 参数**: `"db"` 走数据库,`"config"` 走配置文件,业务侧声明即可。

**Client 缓存**:
- `lru_cache(maxsize=32)` 缓存 (base_url, api_key) → `OpenAI` 实例
- 最多 32 个不同组合,命中已构造的 client 避免重复建连

### 2.2 Model 抽象(协议无关)

`openjiuwen.core.foundation.llm.Model` 是统一抽象,业务代码不直接 import OpenAI SDK:

```python
from openjiuwen.core.foundation.llm import Model, ModelClientConfig, ModelRequestConfig

model_client_config = ModelClientConfig(
    client_provider=compatible_provider(protocol.get("provider")),
    api_key=protocol.get("api_key", ""),
    api_base=protocol.get("base_url", ""),
    timeout=protocol.get("timeout", 60),
    verify_ssl=os.getenv("LLM_SSL_VERIFY", "true") == "false",
)
model = Model(
    model_client_config=model_client_config,
    model_config=ModelRequestConfig(model=protocol.get("model", ""))
)
```

**`client_provider` 取值** —— 通过 `compatible_provider` 兼容老字段:

```python
# memory_base.py: _get_llm_config_from_db
if model_config.provider.lower() == 'openai':
    model_provider = 'OpenAI'
elif model_config.provider == 'siliconflow':
    model_provider = 'SiliconFlow'
```

**ModelRequestConfig 字段**:
- `model`: 模型名
- `temperature`: 0.95 默认
- `top_p`: 0.1 默认
- `max_tokens`: None 默认(由上游决定)

### 2.3 SSE 流式执行 —— Context 全程

**位置**: `routers/execution.py: handler()`

```python
async def handler(
    request_body: Union[ExecuteParas, UserInputParas],
    request: Request,
    mgr: Union[AgentRunner, WorkflowRunner],
    current_user: Dict[str, Any]
) -> AsyncGenerator[str, None]:
    try:
        # 1. 解析 inputs (普通 dict or UserInput)
        if isinstance(request_body.inputs, UserInput):
            inputs = InteractiveInput()
            inputs.update(request_body.inputs.node_id, request_body.inputs.input_value)
        else:
            inputs = request_body.inputs

        # 2. ★ 构造 session_id 并 set_session_id (跨调用追踪)
        session_id = " ".join([
            id_val.strip() for id_val in
            [request_body.space_id, request_body.conversation_id, get_session_id()]
            if id_val and id_val.strip()
        ])
        if session_id:
            set_session_id(session_id)

        # 3. 流式执行
        async for chunk in mgr.run(request_body.id, request_body.version, inputs,
                                   request_body.conversation_id, request_body.space_id, current_user):
            if await request.is_disconnected():
                raise HTTPException(status_code=404, detail="Disconnected")
            logger.debug(f"Received chunk: {chunk}")
            code, message = get_error_info_in_wf_trace(mgr, chunk)
            yield ResponseModel(
                code=code,
                message=message,
                data=chunk
            ).model_dump_json()
    except JiuWenExecuteException as e:
        # 业务异常 → 自定义错误包
        yield WorkflowFailedResponse(...).model_dump_json()
    except JiuWenGraphException as e:
        yield ResponseModel(code=e.code, message=e.message, data=None).model_dump_json()
    ...
```

**SSE 关键点**:
1. **`async for chunk` 异步生成器** —— 边生成边推送,前端 `EventSource` 接收
2. **`request.is_disconnected()` 检查** —— 客户端断连立刻抛 404,避免浪费
3. **`set_session_id` 全链路追踪** —— 拼接 space_id + conversation_id + 当前 session,所有日志带同一 ID
4. **异常转 ResponseModel** —— 5+ 异常类全收口成 SSE 帧,前端统一处理
5. **`get_error_info_in_wf_trace`** —— 从 trace chunk 中提取错误码/消息

### 2.4 Agent 实例缓存

**位置**: `core/executor/agent/agent_runner.py: AgentRunner.get_agent_instance()`

```python
class AgentRunner:
    def __init__(self, flow_mgr, plugin_mgr):
        self.flow_mgr = flow_mgr
        self.plugin_mgr = plugin_mgr
        # Agent实例缓存：{user_id: {agent_key: (config_json, instance)}}
        self._agent_instances: Dict[str, Dict[str, Any]] = {}

    async def get_agent_instance(self, user_id, agent_id, agent_version,
                                  agent_config, space_id, current_user):
        # 1. 初始化用户的缓存空间
        if user_id not in self._agent_instances:
            self._agent_instances[user_id] = {}

        agent_key = generate_agent_key(agent_id, agent_version)
        if agent_key not in self._agent_instances[user_id]:
            self._agent_instances[user_id][agent_key] = ("", None)

        # 2. 用 JSON 序列化比较(避免 Pydantic 对象比较陷阱)
        cache_config_json, catch_instance = self._agent_instances[user_id][agent_key]
        try:
            current_config_json = agent_config.model_dump_json() if hasattr(agent_config, 'model_dump_json') else ""
        except Exception as e:
            logger.warning(f"Failed to serialize agent_config for cache comparison: {e}")
            current_config_json = ""

        # 3. 配置未变 → 直接返回缓存
        if cache_config_json == current_config_json and catch_instance is not None:
            return catch_instance

        # 4. 配置变更 → 清理旧 workflow + 重新编译 + 保留 context_pool
        if catch_instance and hasattr(catch_instance, 'agent_config'):
            old_workflows = [
                (w.id, w.version)
                for w in catch_instance.agent_config.workflows
            ]
            if old_workflows:
                catch_instance.remove_workflows(old_workflows)

        invokable_agent = await self.create_new_agent(agent_config, space_id, current_user)
        # ★ 关键: 保留 context_engine 的 context_pool
        if catch_instance:
            invokable_agent._context_engine._context_pool = catch_instance._context_engine._context_pool

        self._agent_instances[user_id][agent_key] = (current_config_json, invokable_agent)
        return invokable_agent
```

**核心要点**:
| 维度 | 实现 | 价值 |
|------|------|------|
| 缓存键 | `{user_id: {agent_id_version: (config_json, instance)}}` | 多租户隔离 + 多版本共存 |
| 失效判定 | `config.model_dump_json()` 字符串比较 | 避免 Pydantic 对象 `__eq__` 不可靠 |
| 编译优化 | 配置未变直接返回缓存 | 同一 Agent 配置的多次调用零成本 |
| 重编译保护 | `remove_workflows` 清理旧组件 | 避免资源泄漏 |
| Context 保留 | 重编译后 `_context_pool = old._context_pool` | 不丢失用户的对话上下文 |

### 2.5 Context 深度限制

**位置**: `core/executor/workflow/context.py`

```python
DEPTH_LIMIT = 5

class Context:
    def __init__(self, parent=None):
        if parent is None:
            self.depth = 0
        else:
            self.depth = parent.get_depth() + 1
        if self.depth > DEPTH_LIMIT:
            raise JiuWenExecuteException(
                StatusCode.WORKFLOW_NESTING_DEPTH_ERROR.code,
                StatusCode.WORKFLOW_NESTING_DEPTH_ERROR.errmsg.format(msg=str(DEPTH_LIMIT))
            )
        self.parent = parent
```

**`Context` 模式**:
- 每个子流程持有 `parent` 引用,深度 = parent.depth + 1
- 超过 5 层即抛 `WORKFLOW_NESTING_DEPTH_ERROR`,**避免无限递归**
- 这是一种"链式上下文隔离"模式,类似 Go 的 `context.Context`

### 2.6 LLM 调用参数构造

**位置**: `ops/modules/llm/llm_manager.py: build_call_kwargs()`

```python
def build_call_kwargs(params: ModelCallParams, cfg: Dict[str, Any]) -> Dict[str, Any]:
    def _default(param: str, cast: type, default_cast_value: Any):
        for schema in cfg["openModel"]["param_config"]["param_schemas"]:
            if schema["name"] == param:
                return cast(schema["default_val"])
        return default_cast_value

    # 1. 清理 messages: 只保留 role/content + tool_calls/tool_call_id
    cleaned_messages = []
    for msg in params.messages:
        cleaned_msg = {"role": msg.get("role"), "content": msg.get("content")}
        if "tool_calls" in msg:
            cleaned_msg["tool_calls"] = msg["tool_calls"]
        if "tool_call_id" in msg:
            cleaned_msg["tool_call_id"] = msg["tool_call_id"]
        cleaned_messages.append(cleaned_msg)

    # 2. 优先级: 前端显式值 > 配置文件默认值
    call_kwargs = {
        "messages": cleaned_messages,
        "temperature": params.temperature if params.temperature is not None
                       else _default("temperature", float, 1.0),
        "top_p": params.top_p if params.top_p is not None
                 else _default("top_p", float, 1.0),
        "max_tokens": params.max_tokens if params.max_tokens is not None
                      else _default("max_tokens", int, 2048),
    }

    if params.tools:
        call_kwargs["tools"] = params.tools
        if params.tool_choice:
            call_kwargs["tool_choice"] = params.tool_choice

    return call_kwargs
```

**关键设计**:
- **消息白名单** —— 只透传 `role/content/tool_calls/tool_call_id`,防止上游不识别字段污染
- **参数优先级** —— 业务侧 > 配置默认 > 硬编码默认
- **配置驱动** —— `param_schemas` 来自 DB 而非硬编码,运营可改

### 2.7 模型配置 DB-first 加载

**位置**: `core/manager/memory_base.py: _get_llm_config_from_db`

```python
def _get_llm_config_from_db(llm_model_id: int, space_id: str) -> tuple[ModelClientConfig, ModelRequestConfig]:
    with get_db_jw() as db:
        manager = ModelConfigManager(db)
        model_config = manager.get_config_by_id(int(llm_model_id), space_id)

        # 解密 API key
        security_utils = SecurityUtils()
        api_key = None
        if model_config.api_key:
            try:
                api_key = security_utils.decrypt_api_key(model_config.api_key)
            except Exception as e:
                raise ValueError(f"Failed to decrypt api key: {e}")

        model_client_config = ModelClientConfig(
            client_id=str(model_config.id),
            client_provider=model_provider,
            api_key=api_key,
            api_base=model_config.base_url,
            timeout=float(model_config.timeout),
            verify_ssl=os.getenv("LLM_SSL_VERIFY", "true") == "false",
        )
        model_request_config = ModelRequestConfig(
            model=model_config.model_type,
            temperature=model_config.parameters.get("temperature", 0.95),
            top_p=model_config.parameters.get("top_p", 0.1),
            max_tokens=model_config.parameters.get("max_tokens", None),
        )
        return model_client_config, model_request_config
```

**关键点**:
- API key **入库前加密**,使用时解密
- `verify_ssl` 通过环境变量控制(测试环境可关)
- `client_id=str(model_config.id)` 唯一标识,便于追踪

## 3. 触发器与调度(辅助)

**位置**: `core/scheduler/scheduler.py` + `core/manager/trigger.py`

```python
def init_scheduler(database_url: str) -> AsyncIOScheduler:
    _scheduler = AsyncIOScheduler(
        jobstores={
            "default": SQLAlchemyJobStore(url=database_url, engine_options=engine_options)
        },
        job_defaults={
            "coalesce": True,             # 错过的多次触发合并为一次
            "max_instances": 1,           # 同 job 不并发
            "misfire_grace_time": 86400,  # 60s 内的延迟仍然接受
        },
        timezone="UTC",
    )
    return _scheduler
```

**Trigger 类型**:
- `cron`: 定时 cron(用 APScheduler 的 `CronTrigger`)
- `polling`: 间隔轮询(`IntervalTrigger`)
- `webhook`: HTTP 触发(不需 scheduler)

**关键防御**: 数字 day-of-week 0=Sun(POSIX) vs APScheduler 0=Mon 的差异,代码显式做了归一化:

```python
def _normalize_posix_cron(cron_expr: str) -> str:
    """Convert the day-of-week field of a POSIX cron expression from numeric
    values to 3-letter abbreviations so that APScheduler's from_crontab()
    interprets them correctly."""
```

## 4. 与狼人杀 Agent LLM 调用的对比

| 维度 | Agent Studio | 狼人杀 Agent (§14) | 差距 / 借鉴点 |
|------|--------------|---------------------|---------------|
| Provider 抽象 | `Model` 统一接口 | `LLMProvider` 接口 + `Registry` | 思路一致 |
| Client 缓存 | `@lru_cache(maxsize=32)` | 每次 `NewRegistry().Get()` 重新构造 | **可借鉴: 缓存减少 client 创建** |
| 协议 | OpenAI / Anthropic 双适配 | Anthropic 主 + OpenAI 预留 | 对齐 |
| 消息清理 | 显式白名单(role/content/tool_calls) | §14.1 已对齐(MarshalJSON 按 type 收敛) | 对齐 |
| Session ID | `set_session_id` 全链路 | `request_id` + `user_id` 日志字段 | 思路一致 |
| 流式 | SSE + async generator | `ChatStreamAccumulate` (§197) | 对齐 |
| Context 隔离 | `Context` 链式 + DEPTH_LIMIT=5 | `agentRunner` / `Memory.Prune` | 模式相似 |
| 模型缓存 | 按 `user_id+agent_id+version` | `llm.NewRegistry` 单例 | 对齐 |
| 重编译保护 | `_context_pool` 保留 | 不适用(per-game instance) | N/A |
| 异常装饰器 | `@with_exception_handling` | 散落各处 | **可借鉴: 统一收口** |
| 定时任务 | APScheduler + cron/polling/webhook | `phaseWatchdogTick` 5s goroutine | 思路相似 |
| 多租户 | `space_id` | 单实例(本地) | 未来扩展可借鉴 |
| API key 加密 | `SecurityUtils.decrypt_api_key` | AES-256-GCM (§118) | 对齐 |
| 客户端构造失败 | 抛 RuntimeError | 兜底 placeholder key | 风格不同,见 §14 |

## 5. 可学习的设计思想

### 5.1 `@lru_cache` Client 缓存

OpenAI/AsyncOpenAI 客户端是无状态的,但每次新建都要 DNS 解析 + TCP 握手 + TLS 协商。`@lru_cache(maxsize=32)` 缓存 `(base_url, api_key)` 元组,避免高频场景下的连接浪费。

**狼人杀借鉴**: 当前每个 bot LLM 调用都走 `registry.Get()`,内部可能每次都新建 `*Provider`。可考虑缓存。

### 5.2 消息白名单清理

```python
cleaned_msg = {"role": msg.get("role"), "content": msg.get("content")}
if "tool_calls" in msg: cleaned_msg["tool_calls"] = msg["tool_calls"]
if "tool_call_id" in msg: cleaned_msg["tool_call_id"] = msg["tool_call_id"]
```

防止上游不识别的字段(比如 Anthropic 的 `id`/`name`/`input` 误入 text 块)污染请求。**狼人杀 §14.1 的 `MarshalJSON()` 是更严格的实现**——按 type 字段严格收敛,这是更彻底的方案。

### 5.3 Session ID 全链路追踪

`set_session_id` 把多源 ID 拼成单一字符串,后续所有日志自动带这个字段,排查问题时一行 grep 即可还原整次调用的全链路。**狼人杀可强化**: 在 `llm.Provider.Chat()` 入口打 `request_id`,所有内部日志都带,排查 §197 那种"误杀 quarantine"问题时能精准定位。

### 5.4 Config 优先级覆盖

```python
temperature = params.temperature if params.temperature is not None
              else _default("temperature", float, 1.0)
```

业务 > 配置 > 硬编码,**单一真相源(配置)** 仍是默认,但业务侧可单点覆盖。狼人杀 `cfgLLMConfig` 也是类似模式,但缺乏"业务单次覆盖"能力。

### 5.5 Client 实例池 (LRU)

高频场景下 client 复用收益巨大,`maxsize=32` 适合 8 个默认 model × 4 协议变体 的常规规模。狼人杀当前模型数 8 个,可考虑引入。

### 5.6 Context 深度限制(防止递归死锁)

`DEPTH_LIMIT = 5` 是工程防御——业务可能误配置 `workflow.include(self)`,不限制会栈溢出。**狼人杀 §134 狼人互知也需类似防御**:`WolfTeammateSeat` 不能形成环,需在赋值时校验。

### 5.7 触发表(Trigger)的归一化兜底

`@lru_cache` 兜底装饰器 + `try/except` + 默认值返回,是工程上对**外部输入不信任**的标准范式。狼人杀所有从 WS 帧解析玩家输入的代码都应沿用此模式。

## 6. 落地建议(用于狼人杀 Agent)

### 6.1 短期(直接采纳)

1. **LLM Client LRU 缓存** —— 在 `llm.NewRegistry` 内部对 `(base_url, api_key)` 缓存 client,32 个 LRU 槽位足够覆盖 8 默认 model × 4 协议变体。
2. **`setSessionID` 全链路追踪** —— 给每个 LLM 调用分配 `request_id`(UUID),日志、监控、quarantine 判定全部带这个 ID,出问题时可一键 grep 还原。
3. **`buildCallKwargs` 显式字段白名单** —— 类似 `cleaned_messages`,防止 LLM 内部新增字段意外透传到上游(§14.1 的 MarshalJSON 已对齐此点)。

### 6.2 中期(架构优化)

1. **多协议 Client 工厂** —— `get_llm_client_by_protocol(protocol)` 模式值得借鉴:无需查 DB,直接拿 protocol dict 构造 client,适用于运行时热加载新模型。
2. **Context Pool + Compiled Agent 缓存** —— 狼人杀 agent 不需要这种缓存(每个 bot goroutine 独立),但"清理 + 重建"模式可参考(§134 角色重选时复用 Memory)。

### 6.3 长期(能力扩展)

1. **可视化工作流调度** —— Agent Studio 的"Workflow"概念是 DAG 编排,狼人杀目前是"phase + watchdog"硬编码,如未来要做"自定义规则",可借鉴 DAG 模式。
2. **多租户 / 多 workspace** —— `space_id` 模式,未来若要支持"俱乐部/赛事"等多空间隔离,可参考。
