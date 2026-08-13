# Agent Studio 意图识别与任务分解架构分析

> 分析日期: 2026-08-13
> 源码路径: `/usr/local/LsmGitOpenSource/agent-studio/backend/openjiuwen_studio`
> 分析目的: 提炼 Agent 意图识别、任务分解、调度执行的架构模式,为狼人杀 Agent 的多阶段/多角色/多工具派发提供参考

## 1. 架构总览

Agent Studio 采用 **Agent(ReAct/Workflow) → 组件(Component) → 工具(Plugin) → 模型(LLM)** 的四层嵌套架构,核心思想是:

```
用户输入
   ↓
┌────────────────────────────────────────────────────────────┐
│ Router(API)                                                │
│   /agents/run → agent_mgr.run()                            │
│   /workflows/run → flow_mgr.run()                          │
└──────────────────────────┬─────────────────────────────────┘
                           ↓
┌────────────────────────────────────────────────────────────┐
│ AgentRunner / WorkflowRunner                                │
│   - get_agent_instance() (缓存)                            │
│   - run() → mgr.run(id, version, inputs, conv, space, user)│
│   - 异步流式 chunk 推送                                     │
└──────────────────────────┬─────────────────────────────────┘
                           ↓
┌────────────────────────────────────────────────────────────┐
│ Agent 实例 (ReActAgent / WorkflowAgent)                    │
│   - 持有 workflow 列表 + plugin 工具列表                    │
│   - context_engine 管理会话上下文                            │
│   - 通过 _context_pool 复用 context                         │
└──────────────────────────┬─────────────────────────────────┘
                           ↓
┌────────────────────────────────────────────────────────────┐
│ Workflow / Component / Plugin                              │
│   - Workflow = DAG, 多个 Component 通过 edges 串联          │
│   - Component = 单一处理单元(LLM 节点/工具节点/...)         │
│   - Plugin = 外部工具(plugin_id + tool_id)                 │
└────────────────────────────────────────────────────────────┘
```

## 2. 核心设计

### 2.1 Agent 双模: ReActAgent + WorkflowAgent

**位置**: `core/executor/agent/agent.py`

```python
from openjiuwen.core.application.llm_agent import ReActAgentConfig, LLMAgent
from openjiuwen.core.application.workflow_agent import WorkflowAgent
from openjiuwen.core.single_agent.legacy import WorkflowAgentConfig, WorkflowFactory

class Agent:
    def __init__(self, workflow_mgr, agent_config, plugin_mgr):
        self.agent_config = agent_config
        self.workflow_mgr = workflow_mgr
        self.plugin_mgr = plugin_mgr
        self.plugins = []
        self.workflows = []

    async def _fetch_from_mgr(self, space_id, current_user):
        # 1. 获取所有 Workflow 组件(去重)
        seen_workflow_keys = set()
        for workflow_schema in self.agent_config.workflows:
            workflow_key = f"{workflow_schema.id}:{workflow_schema.version}"
            if workflow_key in seen_workflow_keys:
                continue
            seen_workflow_keys.add(workflow_key)
            workflow_instance = await self.workflow_mgr.get_flow(
                workflow_schema.id, workflow_schema.version, space_id, current_user
            )
            self.workflows.append(workflow_instance)

        # 2. 获取所有 Plugin 工具 (仅 ReActAgent 需要,去重)
        if isinstance(self.agent_config, ReActAgentConfig):
            for plugin_schema in self.agent_config.plugins:
                plugin_key = f"{plugin_schema.plugin_id}:{plugin_schema.id}:{plugin_schema.version}"
                if plugin_key in seen_plugin_keys:
                    continue
                seen_plugin_keys.add(plugin_key)
                plugin_tool = await self.plugin_mgr.get_tool(...)
```

**两种 Agent 类型**:

| 类型 | 特点 | 适用场景 |
|------|------|---------|
| **ReActAgent** | LLM 驱动循环:思考 → 行动 → 观察,自主决定调用哪个 tool | 开放式任务、探索式问题 |
| **WorkflowAgent** | 预定义 DAG,按边顺序执行组件 | 流程固定、需要可观测/可调试 |

**共同点**: 都持有 `workflows[]` + `plugins[]`(ReAct 才有 plugins)。

### 2.2 组件去重机制

```python
# Workflow 去重
workflow_key = f"{workflow_schema.id}:{workflow_schema.version}"

# Plugin 去重
plugin_key = f"{plugin_schema.plugin_id}:{plugin_schema.id}:{plugin_schema.version}"
```

**核心思想**: 同一 Workflow/Plugin 的不同引用只加载一次,避免重复编译浪费资源。

### 2.3 配置驱动的 DAG

**位置**: `core/manager/agent.py: _create_mapping_table()`

```python
async def _create_mapping_table(self, agent_config, space_id) -> Dict[str, str]:
    mapping = {}
    # 1. LLM 映射
    if hasattr(agent_config, 'model') and hasattr(agent_config.model, 'model_info'):
        mapping["llm"] = agent_config.model.model_info.model_name
    
    # 2. Workflow 及其组件映射
    for workflow in agent_config.workflows:
        workflow_id_obj = WorkflowId(
            workflow_id=workflow.id, space_id=space_id, workflow_version=workflow.version
        )
        workflow_res = workflow_repository.workflow_get(workflow_id_obj)
        if workflow_res.code == 200 and workflow_res.data:
            schema_str = workflow_db.get('schema', '')
            workflow_schema = json.loads(schema_str) if schema_str else {}
            
            def extract_component_names(schema_part, name_map, parent_path="", parent_component_ids=None):
                if isinstance(schema_part, dict):
                    if 'id' in schema_part and 'data' in schema_part and 'title' in schema_part['data']:
                        component_id = schema_part['id']
                        component_title = schema_part['data']['title']
                        name_map[component_id] = component_title
                        if parent_component_ids:
                            full_id = f"{'.'.join(parent_component_ids)}.{component_id}"
                            name_map[full_id] = component_title
                    
                    if 'nodes' in schema_part and isinstance(schema_part['nodes'], list):
                        for node in schema_part['nodes']:
                            # 嵌套子工作流
                            ...
```

**`_create_mapping_table` 关键设计**:
- 把 workflow 的 `schema` JSON 解析成 component name 映射
- 支持**嵌套子工作流** —— 父组件 ID `.` 拼接
- 一次遍历同时建立 `id → title` 映射,加速后续 tracing 展示

### 2.4 ReAct 循环核心

**openjiuwen.core.single_agent.legacy 内部实现**(基于 `LLMAgent`):

```
for iteration in max_iterations:
    response = llm.invoke(messages + tools)  # 思考
    if response.has_tool_use:
        for tool_call in response.tool_calls:
            result = dispatch_tool(tool_call)  # 行动
            messages.append(tool_result)  # 观察
    else:
        return final_response
```

**狼人杀 Agent 的对照**:
- 狼人杀 §119 `speak_with_thought` 是一次性产出"心口不一"发言
- §128「对话即思考」明确反对"显式 thinking 阶段 + 显式 speaking 阶段"的两段式 ReAct
- 狼人杀的"思考"是**多 tool_use 同响应**的隐性 ReAct,LLM 一次产出 1-5 个 tool_use,工具派发并联,prompt 集中思考

### 2.5 工作流图适配器

**位置**: `core/executor/workflow/pregel_graph_adapter.py`

```python
class JiuWenGraphException(Exception):
    pass
```

**Pregel Graph** = Google Pregel 启发的 BSP(Bulk Synchronous Parallel) 图计算模型:
- 节点 = component
- 边 = 数据流依赖
- 一次 super-step: 收集所有 ready 节点 → 并行执行 → 同步 barrier → 进入下一 super-step

**狼人杀借鉴**:
- 13 人局的多 bot 投票/多 Agent 同步决策,本质是 BSP
- 当前是 watchdog 串行驱动,可考虑改为"收集本轮所有 ready 节点 → 批量执行"
- 但狼人杀有顺序约束(投票前必须所有人发言完),不能直接套用

### 2.6 工作流执行管理

**位置**: `core/executor/workflow/workflow_execution_manager.py`

```python
# 全局 single-instance 管理所有工作流执行
workflow_execution_manager
```

**核心职责**:
- 跨请求追踪活跃的 workflow execution
- 提供 cancel/pause/resume 接口
- 统一的 trace_id 分配

**狼人杀对照**: `WerewolfRoom` 本身就是单房间单 execution 的状态机,不需要全局 manager。但跨房间的"房间生命周期管理"(`room_watchdog`)思路类似。

### 2.7 触发器调度

**位置**: `core/scheduler/scheduler.py` + `core/manager/trigger.py`

```python
def init_scheduler(database_url: str) -> AsyncIOScheduler:
    _scheduler = AsyncIOScheduler(
        jobstores={
            "default": SQLAlchemyJobStore(url=database_url, engine_options=engine_options)
        },
        job_defaults={
            "coalesce": True,             # 多次错过的触发合并为一次
            "max_instances": 1,           # 同 job 不并发
            "misfire_grace_time": 86400,  # 60s 延迟内接受
        },
        timezone="UTC",
    )
    return _scheduler
```

**Trigger 类型**:

| Type | 用途 | 内部 |
|------|------|------|
| `cron` | 定时任务(每 5 分钟) | `CronTrigger.from_crontab()` |
| `polling` | 间隔轮询(每 5 秒) | `IntervalTrigger(seconds=...)` |
| `webhook` | HTTP 触发(无 scheduler) | 直接路由到 `execute_trigger_job` |

**狼人杀对照**: `phaseWatchdogTick` 是 5s 间隔的轮询,模式与 `polling` 类似。但狼人杀不需要持久化(房间销毁后任务即停),不需要 SQLAlchemyJobStore。

### 2.8 Cron POSIX 兼容性归一化

```python
_POSIX_DOW_NAMES = {
    "0": "sun", "7": "sun",
    "1": "mon", "2": "tue", "3": "wed",
    "4": "thu", "5": "fri", "6": "sat",
}

def _normalize_posix_cron(cron_expr: str) -> str:
    """Convert the day-of-week field of a POSIX cron expression from numeric
    values to 3-letter abbreviations so that APScheduler's from_crontab()
    interprets them correctly (APScheduler numeric 0=Mon ≠ POSIX numeric 0=Sun)."""
    parts = cron_expr.strip().split()
    if len(parts) != 5:
        return cron_expr
    minute, hour, dom, month, dow = parts
    if dow == "*":
        return cron_expr
    normalized_dow = ",".join(_normalize_dow_token(t) for t in dow.split(","))
    return f"{minute} {hour} {dom} {month} {normalized_dow}"
```

**设计思想**: 工程上对"外部格式不统一"的标准应对 —— 显式归一化 + 单测覆盖边界值。

### 2.9 触发器执行入口

**位置**: `core/scheduler/runner.py: execute_trigger_job()`

**模式**:
1. Trigger 命中 → `execute_trigger_job(trigger_id, fired_by)`
2. 查询 `TriggerDB` 获取 target(agent/workflow)信息
3. 异步执行目标(类似 `agent_mgr.run()`)
4. 写 `TriggerExecutionLogDB` 记录执行结果
5. webhook 类型直接路由,不进 scheduler

## 3. 与狼人杀 Agent 多阶段任务分解的对比

| 维度 | Agent Studio | 狼人杀 Agent (§15) | 差距 / 借鉴点 |
|------|--------------|---------------------|---------------|
| 任务类型 | ReAct / Workflow 二选一 | 单一"工具派发"模型 | 狼人杀适合 ReAct,无需 DAG |
| 工具发现 | Agent 持有 plugin 列表,LLM 自选 | `BuildTools(phase, role, seat, alive)` 阶段化工具 | **可借鉴: 阶段工具白名单** |
| 工具派发 | `dispatch_tool` 通用入口 | `DispatchTool` 单一入口 | 对齐 |
| 思考循环 | 显式 iteration, 多次 LLM 调用 | 一次响应多 tool_use, 工具并行 | **风格不同,见 §128** |
| 上下文隔离 | `Context` 链式 + depth=5 | `agentRunner` 独立 + `Memory.Prune` | 模式相似 |
| 组件缓存 | workflow/plugin 去重 | `BuildTools` 每次重建 | 狼人杀可缓存 |
| 触发器 | cron/polling/webhook | watchdog 5s tick | 思路一致 |
| Trace | `agent_trace_utils` span 收集 | `BotTranscript` + `LLMCallPhase` | 对齐 |
| DAG 编排 | 支持嵌套子工作流 | phase + 状态机 | 狼人杀硬编码 |
| 任务图 | Pregel BSP | 顺序 phase | 狼人杀有序约束 |

## 4. 可学习的设计思想

### 4.1 双模 Agent(ReAct vs Workflow)

为不同任务特性选择不同执行范式:
- **ReAct** = 灵活但不可预测
- **Workflow** = 固定但可观测

狼人杀的"硬编码 phase + watchdog"是 Workflow 的退化版(没有用户可视化),ReAct 模式则用在了 bot 的多 tool_use 响应中。两种风格混用是合理的。

### 4.2 工具去重 + 缓存

```python
plugin_key = f"{plugin_schema.plugin_id}:{plugin_schema.id}:{plugin_schema.version}"
if plugin_key in seen_plugin_keys:
    continue
```

防止同一工具被多次引用导致重复编译。**狼人杀可借鉴**: 13 bot 房间,每个 bot 的 `BuildTools(phase)` 可能产出重叠工具集,可在 `WerewolfRoom` 级别缓存 `(phase, role) → []Tool` 映射。

### 4.3 嵌套子工作流命名

```python
full_id = f"{'.'.join(parent_component_ids)}.{component_id}"
```

层级化 ID 让 tracing 展示更清晰。**狼人杀可借鉴**: 当前 `BotTranscript` 是扁平的,可加 `phase_substep` 字段标记当前在哪个子阶段。

### 4.4 APScheduler Jobstore 持久化

用 DB 存 trigger 而非内存,服务重启后任务不丢。**狼人杀 watchdog 当前是 in-memory,进程重启丢失**。若未来要支持"7 天长任务",可借鉴 SQLAlchemyJobStore。

### 4.5 触发器归一化(POSIX cron)

显式归一化外部格式 + 注释 + 单元测试覆盖,是对"格式歧义"的工程应对范式。狼人杀的 `cfgWerewolf` 系列配置项都应有类似的"配置项兼容性归一化"(§198 judge mode 的"ai"→"agent"就是这一思想)。

### 4.6 触发器执行日志

每个 trigger 命中后写 `TriggerExecutionLogDB`,提供排查依据。**狼人杀当前 watchdog 只有 logger**,可借鉴加一个 `watchdog_log` 内存 ring buffer(最近 100 条),供前端调试面板拉取。

### 4.7 DAG 嵌套(递归结构)

Agent Studio 的 workflow 嵌套设计是树形结构,任意 component 可调用子 workflow。狼人杀的"13 人局"虽然没用到嵌套,但未来若要支持"赛季/俱乐部"嵌套,这种设计可参考。

## 5. 落地建议(用于狼人杀 Agent)

### 5.1 短期(直接采纳)

1. **工具缓存** —— `WerewolfRoom.buildToolsFor(phase, role)` 内部按 `(phase, role)` 缓存,避免 13 bot 重复构造 `[]Tool`。
2. **watchdog 日志 ring buffer** —— 内存循环队列,最近 100 条 tick 记录,前端调试面板可拉取。
3. **Cron / 调度配置兼容性归一化** —— 类似 §198 的 `cfgWerewolfJudgeMode()` 归一化,所有"接受多种历史值"的配置都应走 `cfgNormalize()` 兜底。

### 5.2 中期(架构优化)

1. **工具白名单 + 阶段上下文** —— `BuildTools(phase, role, seat, alive)` 已经做了阶段 + 角色 + 存活过滤,可再加"对话轮次"维度,例如投票阶段第 2 轮才开放"vote_change"。
2. **嵌套阶段** —— 当前是平铺 phase,可考虑"夜=子阶段(Wolves→Seer→Witch→...)" 嵌套,便于 trace 展示。
3. **trace span 标准化** —— `BotTranscript` 已有,但 stage 划分不清晰,可借鉴 `agent_trace_utils` 的 KB retrieval span 模式。

### 5.3 长期(能力扩展)

1. **DAG 工作流** —— 如果未来要做"自定义游戏模式",可把当前 phase 硬编码抽象为 DAG,让运营可视化编排。
2. **持久化 scheduler** —— 进程重启不丢任务,适合"7 天跨服赛"等长周期任务。
3. **多 agent 协同** —— 当前狼人杀是 per-bot goroutine,未来若做"狼队联合决策",可借鉴 Agent Studio 的 multi-agent 编排。
