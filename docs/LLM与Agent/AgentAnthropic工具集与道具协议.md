# Agent Anthropic 工具集与道具协议契约

> 版本：v5  |
> 适用：LsmWebGame 平台所有 Agent 通过 Anthropic 协议调用 LLM 的工具暴露契约  |
> 配套：`docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md` §16（v5 增量）
>
> **核心目标**：补全 Agent 工具在 Anthropic wire 协议层的字段、命名、顺序约束，
> 避免"漏字段/乱序"导致上游 400 拒绝（对齐 CLAUDE.md §14.1）。

---

## 1. Anthropic wire 协议字段强约束（CLAUDE.md §14.1）

出站请求中 `tools` 字段必须是数组，每条工具严格按下列三段：

```jsonc
{
  "name": "<tool_name>",          // 字符串，唯一标识符
  "description": "<tool_desc>",   // 字符串，自然语言说明（LLM 用来理解何时调用）
  "input_schema": {               // JSON Schema 草案
    "type": "object",             // 必须是 object
    "properties": { ... },        // 各参数 schema
    "required": ["..."],          // 必填字段列表
    "additionalProperties": false // 严格模式
  }
}
```

**字段顺序必须固定为 `name → description → input_schema`**，某些严格的
Anthropic 上游代理（如 DouBao）会把乱序的工具条目视为 schema 不合法而拒收。
序列化稳定性由 `tools_converter_test.go::TestBuildAnthropicTools_FieldOrder` 锁住。

**严禁**：
- 工具条目有未知字段（"additionalProperties": false 等价检查）
- `input_schema.type` 不是 `object`
- 缺 `description` 字段（即便值为空字符串也不行；用 `"(无描述)"` 占位）
- `properties` 含 `required` 未列出的字段（LLM 调出来会被拒）

---

## 2. ToolRegistry 接口

### 2.1 数据结构

```go
// ServerGo/agent/tools_registry.go
type ToolPhase string

const (
    ToolPhaseAny   ToolPhase = "any"
    ToolPhaseSpeak ToolPhase = "speak"
    ToolPhaseNight ToolPhase = "night"
    ToolPhaseVote  ToolPhase = "vote"
)

type ToolSpec struct {
    Name        string
    Phase       ToolPhase
    Category    string                                // "prop" / "wolf" / "core" / "judge"
    Builder     func(gc *GameContext) map[string]any  // → Anthropic input_schema
    MountIf     func(gc *GameContext) bool            // 可选：额外的 GameContext 谓词
    Dispatcher  func(args map[string]any, gc *GameContext, runner ToolRunner) (string, error)
}

var (
    toolRegistryMu sync.RWMutex
    toolRegistry   []*ToolSpec
)
```

### 2.2 注册 API

```go
// 注册；重复注册覆盖（便于测试注入）。
func RegisterTool(t *ToolSpec)

// 按 phase 拉工具列表；MountIf 谓词在构建时过滤。
func MountTools(phase ToolPhase, gc *GameContext) []*ToolSpec

// 按名字查派发器；未注册返回 (nil, false)。
func DispatchToolByName(name string) (DispatcherFunc, bool)
```

### 2.3 注册时机（推荐）

每个工具分类文件包级 `init()` 调用 `RegisterTool`，例如：

```go
// ServerGo/agent/prop_tools.go
func init() {
    RegisterTool(&ToolSpec{
        Name:     "use_prop",
        Phase:    ToolPhaseSpeak,
        Category: "prop",
        Builder:  buildUsePropSchema,
        Dispatcher: dispatchUseProp,
        MountIf:  func(gc *GameContext) bool { return gc != nil && gc.IsAlive },
    })
}
```

这样新增工具**不需要**改 `tools.go`，只新增文件即可。

---

## 3. 狼人杀 13 局工具清单

### 3.1 核心工具（Phase=Speak，白天发言阶段）

| 工具名 | 分类 | wire schema 摘要 | 派发器 |
|--------|------|------------------|--------|
| `speak` | core | `{text: string(<=100), thought?: string(<=200)}` | `dispatchSpeak` |
| `speak_with_thought` | core | `{text: string(<=100), internal_thought: string(<=200)}` | `dispatchSpeakWithThought` |
| `idle_silent` | core | `{}` | `dispatchIdleSilent` |

### 3.2 道具工具（Phase=Speak，§119 心口不一 + 道具系统）

| 工具名 | 分类 | wire schema 摘要 | 派发器 |
|--------|------|------------------|--------|
| `use_prop` | prop | `{prop_key: string(enum), target: int(0-12), payload?: string(<=100)}` | `dispatchUseProp` |
| `prop_inspect` | prop | `{scope: 'mine'\|'all'}` | `dispatchPropInspect` |
| `prop_status` | prop | `{}` | `dispatchPropStatus` |
| `prop_history` | prop | `{limit?: int(<20)}` | `dispatchPropHistory` |

### 3.3 狼人工具（Phase=Night，§132 v4）

| 工具名 | 分类 | 挂载条件 | wire schema 摘要 |
|--------|------|----------|------------------|
| `wolf_whisper` | wolf | `faction=="wolf" && WolfTeammateSeat>=0` | `{text: string(<=80)}` |
| `wolf_kill` | wolf | `faction=="wolf" && 狼夜行动阶段` | `{target: int(0-12, alive)}` |

### 3.4 神职工具（Phase=Night，§119 / §123）

| 工具名 | 分类 | 挂载条件 | wire schema 摘要 |
|--------|------|----------|------------------|
| `seer_check` | core | `role=="seer" && 预言家夜阶段` | `{target: int(0-12)}` |
| `witch_act` | core | `role=="witch" && 女巫夜阶段` | `{action: 'save'\|'poison', target?: int}` |
| `hunter_shoot` | core | `role=="hunter" && 死亡触发` | `{target: int(0-12)}` |

### 3.5 投票工具（Phase=Vote）

| 工具名 | 分类 | wire schema 摘要 |
|--------|------|------------------|
| `vote` | core | `{target: int(0-12) \| -1}` |

### 3.6 法官工具（§130 / §125，全新 Agent 主持人）

| 工具名 | 分类 | 挂载条件 | wire schema 摘要 |
|--------|------|----------|------------------|
| `announce` | judge | 法官 Agent | `{text: string(<=120)}` |
| `prompt_actor` | judge | 法官 Agent | `{seat: int(0-12), hint: string(<=80)}` |
| `summary` | judge | 法官 Agent | `{}` |
| `declare_cause` | judge | 法官 Agent | `{cause: 'wolf'\|'witch_poison'\|...}` |
| `idle_silent` | judge | 法官 Agent | `{}` |

---

## 4. 新工具接入流程（4 步）

每加一个新工具按以下 4 步走，**绝不**直接编辑 `tools.go`：

### Step 1：写 Builder

```go
// ServerGo/agent/<category>_tools.go
func build<NewTool>Schema(gc *GameContext) map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "<arg>": map[string]any{...},
        },
        "required": []string{"<arg>"},
        "additionalProperties": false,
    }
}
```

### Step 2：写 Dispatcher

```go
func dispatch<NewTool>(
    args map[string]any,
    gc *GameContext,
    runner ToolRunner,
) (string, error) {
    // 1. 校验 args 必填字段
    // 2. 调 runner.Action_X(...)
    // 3. 返字符串反馈给 LLM
    return "<result_text>", nil
}
```

### Step 3：注册

```go
func init() {
    RegisterTool(&ToolSpec{
        Name:       "<new_tool>",
        Phase:      ToolPhaseSpeak,   // 或 ToolPhaseNight / ToolPhaseVote / ToolPhaseAny
        Category:   "prop",           // 或 "wolf" / "core" / "judge"
        Builder:    build<NewTool>Schema,
        Dispatcher: dispatch<NewTool>,
        MountIf:    func(gc *GameContext) bool { return gc != nil && <gating> },
    })
}
```

### Step 4：写测试

```go
// ServerGo/agent/<category>_tools_test.go
func Test<NewTool>_Mount(t *testing.T) {
    specs := MountTools(ToolPhaseSpeak, &GameContext{IsAlive: true})
    found := false
    for _, s := range specs {
        if s.Name == "<new_tool>" { found = true }
    }
    if !found { t.Fatal("...") }
}

func Test<NewTool>_Dispatch(t *testing.T) {
    // 准备 mock runner
    // 调 dispatcher
    // 验证返回 + runner 调用
}
```

---

## 5. Anthropic Wire 序列化（BuildAnthropicTools）

`ServerGo/llm/anthropic/tools_converter.go` 的 `BuildAnthropicTools` 转换
`[]*agent.ToolSpec` → `[]AnthropicToolWire`，要点：

| 序 | 字段 | 值 |
|----|------|------|
| 1 | `name` | `spec.Name` |
| 2 | `description` | 包装 `Category + Name + 说明`，避免空字符串 |
| 3 | `input_schema` | `spec.Builder(gc)` 输出 |
| 4 (v5+) | `_meta` (可选) | `{"version": "agent:tool-spec/v5"}` 协议版本号 |

调用方在 `ServerGo/llm/anthropic/chat.go`：

```go
func buildChatRequest(req *ChatRequest, gc *agent.GameContext) *AnthropicWire {
    // ...
    tools := anthropic.BuildAnthropicTools(agent.MountTools(mapPhase(req.Phase), gc))
    // ...
}
```

`BuildAnthropicTools` 内会调用 `json.Marshal`，稳定序列化。测试用 `json.Marshal`
+ byte-equal 锁住字段顺序。

---

## 6. 反模式 / 故障样例

### ❌ 缺 description

```json
{"name": "use_prop", "input_schema": {...}}
```
**错误**：上游某些代理（如 DouBao）拒绝。

### ❌ 乱序字段

```json
{"description": "...", "input_schema": {...}, "name": "use_prop"}
```
**错误**：同上。

### ❌ 缺 required

```json
{
  "input_schema": {
    "type": "object",
    "properties": {"target": {...}, "prop_key": {...}}
    // 缺 "required"
  }
}
```
**错误**：LLM 可能传空 target 触发派发器 panic。

### ✅ 正确样板（use_prop）

```json
{
  "name": "use_prop",
  "description": "使用道具对目标进行心理战攻击。消耗金币，50% 回本局彩池，30% 系统吸收，20% 补偿被击中者。",
  "input_schema": {
    "type": "object",
    "properties": {
      "prop_key": {
        "type": "string",
        "enum": ["markdown_bomb","nested_maze","char_confuse","long_swear","task_disguise","task_disguise_v3","emotion_plea"],
        "description": "道具类型"
      },
      "target": {
        "type": "integer",
        "minimum": 0,
        "maximum": 12,
        "description": "目标座位号 (0-indexed)"
      },
      "payload": {
        "type": "string",
        "maxLength": 100,
        "description": "道具附带自定义文本（可选）"
      }
    },
    "required": ["prop_key", "target"],
    "additionalProperties": false
  }
}
```

---

## 7. 协议版本与向前兼容

`AnthropicToolWire` 加可选 `_meta` 字段（v5 引入）：

```go
type AnthropicToolWire struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    InputSchema map[string]any `json:"input_schema"`
    Meta        *ToolMeta      `json:"_meta,omitempty"`
}

type ToolMeta struct {
    Version string `json:"version"`  // 例: "agent:tool-spec/v5"
}
```

Anthropic provider 把 `_meta` 透明透传给上游（Anthropic / DouBao / Qwen 均支持）。
版本号便于未来回退：`agent:tool-spec/v5` → `agent:tool-spec/v4` 时 API 端解析老
字段。

---

## 8. 关联文档

- `docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md` §16 v5 增量
- `docs/狼人杀-Agent与系统/狼人杀Agent设计.md` Agent 基础架构
- `docs/LLM与Agent/LLM供应商设计.md` LLM Provider 总设计
- `docs/Anthropic协议对齐.md`（CLAUDE.md §14.1 详细 wire 字段）
- `ServerGo/llm/anthropic/` Anthropic provider 实现
- `ServerGo/agent/` Agent 工具定义层
