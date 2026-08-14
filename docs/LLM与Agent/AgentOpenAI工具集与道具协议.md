# Agent OpenAI 工具集与道具协议契约

> 2026-08-14 §20260814-01 新增。姊妹篇:
> [`AgentAnthropic工具集与道具协议.md`](AgentAnthropic工具集与道具协议.md)(Anthropic Messages 协议契约)。
> 本文档定义 **OpenAI Chat Completions 协议**(`openai-completions`)在本工程中的完整接入方案:
> 协议选型、wire 字段映射、SSE 流式解析、Provider 分层架构、数据库 / 管理 API / Web 管理页改造。
>
> **权威数据用例**(opencode Agent 真实抓包,见 [`OpenAI协议样例/`](OpenAI协议样例/) 同级子目录):
>
> | 角色 | 文件 |
> |------|------|
> | Request Headers | [`OpenAI协议样例/RequestHeader.md`](OpenAI协议样例/RequestHeader.md) |
> | Request Body | [`OpenAI协议样例/RequestBody.json`](OpenAI协议样例/RequestBody.json) |
> | Response Headers | [`OpenAI协议样例/ResponseHeader.md`](OpenAI协议样例/ResponseHeader.md) |
> | Response Body | [`OpenAI协议样例/ResponseBody.json`](OpenAI协议样例/ResponseBody.json) |

## 0. 设计总原则

1. **两套协议,两个独立 Provider** —— OpenAI 协议**不是** Anthropic 协议的"翻译版"。
   本工程**不**做"OpenAI 请求 → 转成 Anthropic 报文 → 发给 Anthropic 代理"的协议转换;
   而是新建 `ServerGo/llm/openai/` 包,独立实现 OpenAI Chat Completions 的 wire 类型、
   请求构造、SSE 解析、错误分类与熔断,与 `ServerGo/llm/anthropic/` 平级。
2. **统一抽象不变** —— 上层(狼人杀 Agent / 法官 / 记忆迭代 / 未来其他游戏 Agent)只面向
   `llm/types.LLMProvider` 接口与 `LLMRequest` / `LLMResponse` 规范化类型编程;
   协议差异被封装在各自 Provider 包内,**调用方零改动**。
3. **分层架构**(自上而下):

   ```
   ┌─────────────────────────────────────────────────────────┐
   │ 业务 Agent 层  wwplayer.Agent / wwjudge.AgentJudge /      │
   │ MemoryIter / 未来 Doudizhu-Agent …(不感知协议)           │
   ├─────────────────────────────────────────────────────────┤
   │ 规范化类型层  llm/types: LLMRequest / LLMResponse /       │
   │ ContentBlock / StreamEvent / LLMProvider 接口             │
   ├───────────────────────┬─────────────────────────────────┤
   │ 协议 Provider 层       │                                 │
   │ llm/anthropic (既有)   │ llm/openai (本文档新增)          │
   │ POST /v1/messages      │ POST {base}/chat/completions    │
   ├───────────────────────┴─────────────────────────────────┤
   │ Registry 层  llm/registry.go — 按 DB 行 provider_type     │
   │ 选择协议实现;key 解密注入;endpoint 覆盖;健康检查          │
   ├─────────────────────────────────────────────────────────┤
   │ 持久化层  t_lsm_game_llm_provider.provider_type           │
   │ ('anthropic-messages' | 'openai-completions')             │
   └─────────────────────────────────────────────────────────┘
   ```

4. **未来扩展** —— 新增第三套协议(如 Gemini / Responses API)时,只需:
   新建 `llm/<proto>/` 包实现 `LLMProvider` + 在 registry 的协议分发表登记一个 case,
   业务 Agent 层与 DB schema 均不动。

## 1. 协议标识( provider_type 取值)

| 规范值(canonical) | 含义 | 兼容旧值(读取时归一化) |
|---|---|---|
| `anthropic-messages` | Anthropic Messages API,`POST {endpoint}/v1/messages` | `anthropic`、`""`(空) |
| `openai-completions` | OpenAI Chat Completions API,`POST {endpoint}/chat/completions` | `openai` |

- **归一化单点**:`llm/types.NormalizeProviderType(s string) string`。
  DB 读取(registry `populateLocked`)、cfg 读取、admin API 写入,三个入口全部经它归一化;
  存量 DB 行(`anthropic`)无需迁移即可工作,admin 下次保存时自动写成规范值。
- `types.ProviderTypeAnthropicMessages` / `types.ProviderTypeOpenAICompletions` 常量收口,
  禁止散落字符串字面量。
- `LLMProvider.ProviderType()` 返回规范值(anthropic Provider 维持返回 `anthropic` 的
  历史行为不变 —— 该返回值仅用于诊断日志;所有**判定**必须走 NormalizeProviderType)。

## 2. OpenAI wire 协议字段契约(对照权威用例)

### 2.1 请求头(参考 RequestHeader 用例)

```
Content-Type: application/json
Authorization: Bearer <api_key>
Accept: text/event-stream          # 仅 stream=true 时
User-Agent: <AgentClassName>/<AppVersion> <buildDateTime>   # §24 约定,与 anthropic 一致
```

**不发送** `anthropic-version` / `x-anthropic-billing-header` —— 那是 Anthropic 协议私有头。

### 2.2 请求体顶层字段(参考 RequestBody 用例)

| 字段 | 来源(LLMRequest → OpenAI) | 说明 |
|---|---|---|
| `model` | `req.Model` | 直传 |
| `messages[]` | system + messages 转换(§2.3) | OpenAI 把 system 放在 messages[0] |
| `max_tokens` | `req.MaxTokens` | 直传 |
| `temperature` | `req.Temperature`(nil 省略) | 直传 |
| `tools[]` | `req.Tools` 转换(§2.4) | function 包装 |
| `tool_choice` | `req.ToolChoice` 转换(§2.5) | auto/required/具名函数 |
| `stream` | `req.Stream` | 直传 |
| `stream_options` | stream=true 时固定 `{"include_usage":true}` | 让末 chunk 携带 usage |

**明确不下发**的 Anthropic 私有字段:`system`(顶层)/ `metadata` / `output_config` /
`thinking` / `cache_control`。OpenAI 协议无对应物,硬塞会被严格网关 400。
(opencode 用例里的 `reasoning_effort` 属可选扩展,首版不下发;后续如需
`thinking_enabled=true` 的 OpenAI 行映射 `reasoning_effort:"medium"`,在
`llm/openai/convert.go` 单点增加即可。)

### 2.3 messages 转换规则(核心)

| Anthropic 形态 | OpenAI 形态 |
|---|---|
| `system: [{type:"text",text:"…"}]`(数组) | `{"role":"system","content":"…"}`(多条 text 用 `\n\n` 拼接,置于 messages 首部) |
| user 消息,content 全为 text 块 | `{"role":"user","content":"<拼接文本>"}` |
| user 消息,content 含 tool_result 块 | **每个 tool_result 拆成一条** `{"role":"tool","tool_call_id":"<ToolUseID>","content":"<text 拼接>"}`;同消息内剩余 text 块再合成一条 user 消息 |
| assistant 消息,content 全为 text 块 | `{"role":"assistant","content":"<拼接文本>"}` |
| assistant 消息,content 含 tool_use 块 | `{"role":"assistant","content":"<text 拼接,可空>","tool_calls":[{"id","type":"function","function":{"name","arguments":<input 序列化为 JSON 字符串>}}]}` |
| thinking 块 | **剔除**(同 §128 / AccumulateStream 的 thinking 丢弃规则) |

关键约束:

- `tool_calls[].function.arguments` 是 **JSON 字符串**而不是对象 —— 序列化时必须
  `json.Marshal(input)` 成 string(参考 RequestBody 用例:
  `"arguments": "{\"command\": \"go run . -help …\"}"`)。
- `role:"tool"` 消息必须紧跟在携带对应 `tool_calls` 的 assistant 消息之后(OpenAI 硬校验);
  转换器按消息原顺序展开即可天然满足。
- 空 text 防御:`ensureAssistantMessageHasText` 的 DouBao 空格补丁对 OpenAI 无害,
  转换时空 content 输出 `""`(OpenAI 允许 assistant content 为空串 + tool_calls)。

### 2.4 tools 转换

```
Anthropic: {"name","description","input_schema"}
OpenAI:    {"type":"function","function":{"name","description","parameters": <input_schema 原样>}}
```

`input_schema` 本身是标准 JSON Schema,两协议通用,原样透传。

### 2.5 tool_choice 转换

| Anthropic | OpenAI |
|---|---|
| `nil` / `{"type":"auto"}` | `"auto"` |
| `{"type":"any"}` | `"required"` |
| `{"type":"tool","name":"foo"}` | `{"type":"function","function":{"name":"foo"}}` |

### 2.6 响应体(非流式,参考 ResponseBody 用例)

```json
{
  "id": "chatcmpl-…", "object": "chat.completion", "model": "qwen3.8-max",
  "choices": [{"index":0, "message":{"role":"assistant","content":"",
      "reasoning_content":"…", "tool_calls":[{"id":"call_…","type":"function",
      "function":{"name":"bash","arguments":"{…}"}}]},
    "finish_reason":"tool_calls"}],
  "usage": {"prompt_tokens":14024,"completion_tokens":284,"total_tokens":14308}
}
```

归一化到 `LLMResponse` 的映射:

| OpenAI | LLMResponse |
|---|---|
| `id` / `model` | `ID` / `Model` |
| `choices[0].message.content` | 一个 `ContentBlock{Type:"text"}`(空串则省略) |
| `choices[0].message.tool_calls[]` | 每个 → `ContentBlock{Type:"tool_use", ID, Name, Input=arguments 反序列化}`;arguments 非法 JSON 时落 `{"_partial": raw}`(与 anthropic stream.go 的 ErrStreamToolUsePartial 语义一致) |
| `choices[0].message.reasoning_content` | **丢弃,不进 Content**(同 thinking 块剔除规则:瞬时推理不属于可重放对话历史;若回放进 wire 会被严格网关 400) |
| `finish_reason` → `StopReason` | `stop`→`end_turn`;`tool_calls`→`tool_use`;`length`→`max_tokens`;`content_filter`→`content_filter`;其他原样 |
| `usage.prompt_tokens` / `completion_tokens` | `Usage.InputTokens` / `Usage.OutputTokens` |

### 2.7 SSE 流式解析(Response Headers: `Content-Type: text/event-stream`)

OpenAI 流式响应是**无 `event:` 行**的纯 `data:` 序列,每个 data 是一个
`chat.completion.chunk` JSON,以 `data: [DONE]` 结束:

```
data: {"id":"chatcmpl-…","object":"chat.completion.chunk","choices":[{"index":0,
        "delta":{"role":"assistant","content":""}}]}
data: {"id":"…","choices":[{"index":0,"delta":{"content":"你好"}}]}
data: {"id":"…","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_…",
        "type":"function","function":{"name":"speak","arguments":""}}]}}]}
data: {"id":"…","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,
        "function":{"arguments":"{\"text\":"}}]}}]}
data: {"id":"…","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],
        "usage":{"prompt_tokens":N,"completion_tokens":M}}
data: [DONE]
```

聚合状态机(`llm/openai/stream.go`):

- 文本增量:`choices[].delta.content` 逐 chunk 拼接为一个 text 块。
- 工具增量:`delta.tool_calls[]` 按 **`index`** 分桶(OpenAI 的工具增量不带
  content_block_start/stop 边界,id/name 只在首个 chunk 出现,arguments 是逐段
  到达的 JSON 字符串碎片)——聚合器按 index 累积 `id` / `name` / `arguments`,
  流末对每个 index finalize 成一个 tool_use 块(arguments 反序列化,失败落 `_partial`)。
- `reasoning_content` / `reasoning` delta:计数后丢弃(不物化)。
- `finish_reason`:首次出现即记录为 StopReason(映射同 §2.6)。
- `usage`:依赖请求期 `stream_options.include_usage=true`,末 chunk 携带。
- `[DONE]`:正常终止;EOF 无 `[DONE]` 但有内容 → `ErrStreamTruncated` 语义对齐
  anthropic(返回部分响应 + 错误,由调用方决定 salvage)。
- **progress 事件兼容**:为复用 Agent 现有的 `onProgress func(types.StreamEvent)`
  管线(§127 `_first_token` / §197 流式续命),openai 聚合器合成同形事件:
  首个 delta → `_first_token`;首个 chunk → `message_start`(携带 id/model);
  `[DONE]` → `message_stop`。Agent 层完全无感。

## 3. Provider 实现(`ServerGo/llm/openai/`)

| 文件 | 职责 |
|---|---|
| `openai.go` | `Provider` 结构(双 httpClient:常规 / 流式无整体超时)、`Chat` / `ChatStream` / `ChatStreamAccumulate` / `SetUserAgent` / `SetStreamTimeouts` / `ChatTimeout`、线性退避重试(1s→4s,与 anthropic 对齐)、429/5xx Retryable 分类、端点级熔断(60s 窗 / 3 次 / 60s 冷却)+ failover 端点推进 |
| `convert.go` | §2.3–§2.5 全部请求转换 + §2.6 响应归一化 + `ChatCompletionsURL(endpoint)` |
| `stream.go` | §2.7 SSE 解析与聚合器 |
| `openai_test.go` 等 | 转换 / 聚合 / URL / 错误分类单测 |

**与 anthropic.Provider 的刻意差异**(协议本质差异,非偷懒):

- 无 `anthropic-version` / billing header;无 thinking / output_config / metadata 下发。
- 无 `ensureAssistantMessageHasText` 空补丁之外的任何 DouBao 式字段污染防御 ——
  OpenAI 协议天然允许 assistant 空 content + tool_calls。
- 熔断粒度首版只做端点级(不含 model_400 / model_429 电路)——`run_llm.go` 的短路
  前置是对 `*anthropic.Provider` 的类型断言,openai Provider 自动跳过,行为正确;
  model 级电路留作后续增强(在 doc 中显式登记为已知差异)。

**URL 规则(硬约束)**:`ChatCompletionsURL(base)` ——

1. `strings.TrimRight(base, "/")`;
2. 已以 `/chat/completions` 结尾 → 原样;
3. 否则追加 `/chat/completions`。

管理员在 Web 页面填 `https://api.openai.com/v1` 或 `http://proxy:8080/openai`
均可,最终请求 URL 自动补全;**绝不**出现 `…/chat/completions/chat/completions`。

**User-Agent**:`userAgentFor(agentClassName)` 逻辑与 anthropic 完全一致
(§24 `<AgentClassName>/<AppVersion> <buildDateTime>`),经 `registry.SetUserAgent` 注入。

## 4. Registry / 配置 / 数据库改造

### 4.1 数据库

`t_lsm_game_llm_provider.provider_type varchar(32)` 容量足够(`openai-completions` 18 字符),
**无 DDL 变更**。存量行 `anthropic` 由读取路径归一化;新行由 admin API 写规范值。

### 4.2 Registry(`llm/registry.go`)

- `registeredProvider` 新增 `protocol string`(归一化后的规范值)。
- `newRegistryShared` 增建 `sharedOpenAI *openai.Provider`(同 timeout / 重试 /
  stream 超时配置;全局 endpoint 列表仅作兜底 —— OpenAI 行**应当**配置 per-row endpoint)。
- `populateLocked` / `loadFromConfigLocked`:按 `NormalizeProviderType(provider_type)`
  选择挂 sharedAnthropic 还是 sharedOpenAI;`info.ProviderType` 写规范值。
- `Get` 慢路径 `newEndpointProviderLocked(endpoint, protocol)`:按协议 new 对应 Provider。
- thinking 自愈(BUG-R229-P0-01)仅对 `anthropic-messages` 族生效,OpenAI 行
  `thinking_enabled` 恒按 DB 原值(且 openai 转换器不下发 thinking,双保险)。
- `SetUserAgent`:遍历所有已实例化 Provider,按 `interface{ SetUserAgent(string) }`
  派发(两协议都实现);`SetBillingHeader` 仍仅 anthropic。
- `SetStreamTimeouts`:同步下发到 openai Provider(同样的"首字节限时 + 首字节后
  不限时"§130 语义)。

### 4.3 配置(`config/config.go`)

- `ProviderConfig.ProviderType` 注释更新为两个规范值;`applyDefaults` 的
  thinking 自愈判定改为 `NormalizeProviderType(pt) == anthropic-messages` 族。

### 4.4 管理 API(`api/model_admin_api.go`)

- `CreateProviderRequest.ProviderType` 的 binding 从 `oneof=anthropic openai` 放宽,
  服务端归一化 + 白名单校验(仅两个规范值,旧值自动归一)。
- `UpdateProviderRequest` 同理。
- `TestProvider` 诊断面板按协议渲染:
  - anthropic-messages → `request_url = {endpoint}/v1/messages`,headers 带 `anthropic-version`;
  - openai-completions → `request_url = {endpoint}/chat/completions`,无 anthropic-version,
    body 预览为 OpenAI 形态(system 合并进 messages)。
  - 判定用 `registry.GetInfo(model).ProviderType` 归一化结果,非 DB 原值。

## 5. Web 管理页(🤖 LLM 模型管理)

1. **协议下拉**(`ModelAdminPage` 表单):两个选项
   `anthropic-messages`(Anthropic Messages)/ `openai-completions`(OpenAI Chat Completions),
   三语 i18n 文案;读取旧值 `anthropic`/`openai` 时表单回显规范值。
2. **Endpoint 提示**:选中 `openai-completions` 时,endpoint 输入框下方显示
   「填写基础地址即可,请求时自动追加 `/chat/completions`」提示(三语);
   且 OpenAI 协议下 endpoint 必填(无全局默认),前端做提交前校验 + 后端 create/update
   对 openai 行空 endpoint 返回 400 带明确文案。
3. **列表 / 详情页**:`provider_type` 列直接展示规范值;`RoomCreateModal` 的模型过滤
   从 `=== 'anthropic'` 改为「双协议均可」(OpenAI 模型同样可开 AI 房间)。
4. `ClientWeb/src/types/model.ts` 增加 `ProviderProtocol` 联合类型与常量表,
   页面不散落字符串字面量。
5. **§4 行数上限**:`ModelAdminPage.tsx` 已 1798 行,本次把「模型测试诊断弹窗」
   整段抽为 `components/admin/ModelTestResultPanel.tsx`,为主页腾出空间。

## 6. 测试与验收

- `llm/openai` 单测:转换(4 种消息形态 / tools / tool_choice)、URL 规则、
  非流式响应归一化、SSE 聚合(文本 + 单工具 + 多工具 index 分桶 + arguments 碎片 +
  usage + [DONE] + 截断)、错误分类(429/5xx Retryable,400/401 否)。
- `llm` registry 测试:DB 行 `openai` / `openai-completions` → Get 返回 openai Provider;
  归一化后 `info.ProviderType` 为规范值;thinking 自愈不影响 openai 行。
- admin API 测试:create/update 接受旧值归一;TestProvider URL 按协议分叉。
- 全量:`go build ./...` + `go test ./...` + `tsc --noEmit` + `npm run build`。
- 运行时:配置一个真实 OpenAI 协议模型 → admin 「测试」按钮真实对话成功 →
  开 7-AI 房间验证 Agent 全链路。

## 7. 关联文档

- [`AgentAnthropic工具集与道具协议.md`](AgentAnthropic工具集与道具协议.md)
- [`LLM供应商设计.md`](LLM供应商设计.md)
- CLAUDE.md §14(LLM Provider 模块)/ §14.1(Anthropic 协议对齐)/ §24(AgentClassName)
