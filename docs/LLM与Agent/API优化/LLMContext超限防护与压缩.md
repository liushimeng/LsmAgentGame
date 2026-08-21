# Agent 调用大模型 API 优化方案
> **日期**: 2026-08-10  
> **问题**: DouBao-model 请求 ID 1225 返回 400 Bad Request (input_tokens=651816, output_tokens=51)  
> **目标**: 优化 Context 管理机制，减少重复调用，防止 Context 超限

---

## 1. 问题分析

### 1.1 现象
- 请求 ID: 1225
- 模型: DouBao-model
- Input Tokens: 651816 (约 2.6MB)
- Output Tokens: 51
- 错误: 400 Bad Request

### 1.2 根因分析
1. **Context 大小失控**: 651816 tokens 远超 DouBao 模型的上下文窗口限制
2. **压缩机制失效**: 当前 `DefaultMaxPromptBytes = 200KB`，但实际 payload 达 2.6MB
3. **可能原因**:
   - `approxPayloadBytes` 低估了实际大小
   - System prompt + Tools 定义未计入字节预算
   - 某些路径绕过了压缩机制

### 1.3 当前压缩机制
```go
DefaultPruneTurns = 80      // 保留 80 轮
DefaultMaxPromptBytes = 200 * 1024  // 200KB 预算
DefaultCompressTurns = 20   // 压缩最近 20 轮
```

---

## 2. 优化方案

### 2.1 短期修复 (P0 - 立即实施)

#### 2.1.1 字节预算校准
- **问题**: `approxPayloadBytes` 未计入 system + tools
- **修复**: 扩展字节计算，包含完整 payload 结构

```go
func approxTotalPayloadBytes(msgs []llm.Message, system []SystemBlock, tools []Tool) int {
    bytes := approxPayloadBytes(msgs) // messages
    // system blocks
    for _, s := range system {
        bytes += len(s.Text) + len(s.Type)
    }
    // tools 定义
    for _, t := range tools {
        bytes += len(t.Name) + len(t.Description)
        // schema JSON 序列化大小
        if t.InputSchema != nil {
            bytes += len(json.Marshal(t.InputSchema))
        }
    }
    return bytes
}
```

#### 2.1.2 按模型动态设置字节预算
- **问题**: 所有模型用同一 200KB 预算，不区分上下文窗口
- **修复**: 从模型配置读取 max_context_window，按比例设置

```go
// 建议方案：按模型上下文窗口的 60% 设置预算
func modelContextBudget(modelKey string) int {
    cfg := getModelConfig(modelKey)
    if cfg.MaxContextWindow > 0 {
        return cfg.MaxContextWindow * 60 / 100  // 60% 用于 messages
    }
    return DefaultMaxPromptBytes // fallback
}
```

#### 2.1.3 失败路径即时压缩
- **现状**: 失败后仅调用 `PruneByBytes(0)`，使用默认预算
- **修复**: 失败时使用更激进的压缩比例

```go
// 400 "exceed max message tokens" 等 Context 超限错误
if isContextExceededError(err) {
    // 强制压缩到 50% 预算
    a.Memory.PruneByBytes(a.Memory.MaxPromptBytes() / 2)
}
```

### 2.2 中期优化 (P1 - 本周实施)

#### 2.2.1 智能历史压缩
- **当前**: 简单截断最近 20 轮为摘要
- **优化**: 基于重要性加权压缩

```go
type MessageImportance struct {
    Msg        llm.Message
    Importance int  // 0-100, 越高越重要
}

// 重要性评分规则:
// - 发言/投票决策: 80+
// - 身份相关: 90+
// - 普通对话: 40-60
// - 重复/噪声: < 30

func (m *Memory) CompressHistoryWeighted(maxTokens int) {
    // 1. 计算每条消息的重要性
    // 2. 保留重要性 top N 的消息
    // 3. 其余压缩为摘要
}
```

#### 2.2.2 增量 Context 注入
- **问题**: 每次 LLM 调用都带完整历史
- **优化**: 只注入变化部分 + 关键上下文

```go
type IncrementalContext struct {
    BaseContext   string  // 身份 + 规则 (不变)
    PhaseContext  string  // 当前阶段状态 (每阶段变)
    RecentHistory string  // 最近 N 轮 (每轮变)
    KeyEvents     string  // 关键事件 (渐增)
}
```

#### 2.2.3 System Prompt 分层
- **问题**: 900 行 system prompt 每次都发
- **优化**: 分为核心规则 + 增量规则

```go
// 核心规则: 身份、胜利条件、基本规则 (不变)
// 阶段规则: 当前阶段特有指令 (每阶段变)
// 动态规则: 基于游戏状态生成 (每轮变)
```

### 2.3 长期优化 (P2 - 未来版本)

#### 2.3.1 Context Window 自动检测
- 动态查询模型的实际上下文限制
- 根据剩余空间自适应压缩比例

#### 2.3.2 跨轮次知识库
- 将历史决策抽取为结构化知识
- LLM 可按需查询而非全量注入

#### 2.3.3 多模型差异化策略
- 小窗口模型 (DouBao ~128K): 激进压缩
- 中窗口模型 (Kimi ~256K): 适中压缩
- 大窗口模型 (Claude ~200K): 保守压缩

---

## 3. 实施计划

### 3.1 Phase 1: 紧急修复 (今天) ✅ 已完成
- [x] 校准字节计算，包含 system + tools (`approxSystemToolsBytes` + `estimateMapSize`)
- [x] 按模型配置设置不同预算 (`getModelContextBudget` + `SetMaxPromptBytes`)
- [x] 失败路径即时压缩 (`isContextExceededError` + `PruneByBytesAggressive`)
- [x] 验证: `go test ./...` 通过(25 个包全部 PASS)
- [x] 新增单元测试: `TestApproxSystemToolsBytes` / `TestMemory_SetSystemTools` / `TestIsContextExceededError` / `TestPruneByBytesAggressive`

**改动文件**:
- `ServerGo/agent/wwplayer/memory.go` - 新增 `approxSystemToolsBytes` / `estimateMapSize` / `SetSystemTools` / `PruneByBytesAggressive`;修改 `enforceByteBudgetLocked` / `PruneByBytes` 使用完整 payload 大小
- `ServerGo/agent/wwplayer/run.go` - 新增 `SetSystemTools` 调用点;新增 `isContextExceededError` 检测 + `PruneByBytesAggressive` 调用
- `ServerGo/agent/wwplayer/run_helpers.go` - 新增 `isContextExceededError` 函数
- `ServerGo/agent/wwplayer/agent.go` - 新增 `getModelContextBudget` 函数;在 `NewWithRoom` 中按模型设置字节预算
- `ServerGo/agent/wwplayer/context_budget_test.go` - 新增 5 个单元测试

**模型预算配置**:
| 模型 | 预算 | 说明 |
|------|------|------|
| DouBao-model | 400KB | 实测 ~810KB 触发 400,保守设 400KB |
| Kimi-model | 400KB | 类似 DouBao |
| DeepSeek-model | 300KB | 实测上下文窗口较小 |
| GLM-model | 300KB | 类似 DeepSeek |
| MeiTuan-model | 600KB | 较大上下文窗口 |
| MinMax-model | 500KB | 中等上下文窗口 |
| Qwen-model | 600KB | 较大上下文窗口 |
| Xiaomi-model | 500KB | 中等上下文窗口 |

### 3.2 Phase 2: 增强压缩 (本周)
- [ ] 实现智能历史压缩
- [ ] 增量 Context 注入
- [ ] System Prompt 分层

### 3.3 Phase 3: 监控与调优 (下周)
- [ ] 添加 Context 大小监控指标
- [ ] A/B 测试压缩策略
- [ ] 根据实际数据调优参数

---

## 4. 测试计划

### 4.1 单元测试
- `TestApproxTotalPayloadBytes`: 验证字节计算准确性
- `TestCompressHistoryWeighted`: 验证智能压缩
- `TestModelContextBudget`: 验证按模型设置预算

### 4.2 集成测试
- 模拟 DouBao 13 人局，验证 Context 不超限
- 验证压缩后 Agent 行为不退化
- 验证失败路径正确压缩

### 4.3 回归测试
- 现有 `go test ./...` 全部通过
- 前端 `tsc --noEmit` + `npm run build` 通过

---

## 5. 风险评估

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 压缩丢失关键信息 | Agent 行为退化 | 重要性加权 + 保留关键事件 |
| 字节计算不准 | 预算失效 | 多层校验 + 实际测试 |
| 模型配置缺失 | fallback 失败 | 保守默认值 + 日志警告 |

---

## 6. 相关文件

- `ServerGo/agent/wwplayer/memory.go` - 记忆管理核心
- `ServerGo/agent/wwplayer/run.go` - Agent 运行循环
- `ServerGo/agent/wwplayer/prompt.go` - Prompt 构建
- `ServerGo/config/config.go` - 模型配置

---

## 7. 教训记录

### 20260810-14-L1: 字节预算必须包含完整 payload
- **问题**: 仅计算 messages 大小，忽略 system + tools
- **后果**: 实际 payload 超限但预算未触发
- **修复**: 计算完整 HTTP 请求体大小

### 20260810-14-L2: 模型上下文窗口必须动态获取
- **问题**: 所有模型用同一预算
- **后果**: 小窗口模型容易溢出
- **修复**: 从模型配置读取，按比例设置

### 20260810-14-L3: 失败路径必须即时压缩
- **问题**: 失败后仅用默认预算压缩
- **后果**: Context 超限时无法自恢复
- **修复**: 400 错误时强制压缩到 50%
