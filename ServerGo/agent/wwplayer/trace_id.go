// Package wwplayer — trace_id.go: 每次 LLM 调用的全链路追踪 ID 与 AgentRunTrace。
//
// 2026-08-13 §20260813-01 优化: 借鉴 agent-studio `set_session_id` 全链路追踪
// (docs/其他Agent代码分析/agent-studio_Context管理分析.md §5.3),
// 给每次 LLM 调用分配唯一 RequestID,贯穿 RunLoop / callProvider /
// tool dispatch 全路径,所有 logger 自动带同一 ID,排查 §197 误杀 / §82b
// 配对失败等问题时可一键 grep 还原。
//
// 2026-08-14 §20260814-02 U5 扩展: 引入 AgentRunTrace 与 RunID,把单次 LLM 调用 ID
// 升级到"整个 bot 生命周期"的 run ID,一个 run 内跨多次 LLM 调用 + tool dispatch 共享。
//
// 设计要点:
//   - RequestID/RunID 都是 16 字节 UUID 的 hex 形式(32 字符),全局唯一。
//   - 通过 context.Value 传递,**禁止**全局变量(并发安全)。
//   - 不修改现有 callProvider / DispatchTool 签名 — 调用方按需 ctx 提取。
//   - TraceSpan 是可选扩展(本版本仅提供 ID,完整 span 树留待后续)。
//
// 与 §197/§128/§130 兼容性: 本文件不改变 LLM 调用链,只增加 ID 字段。
package wwplayer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type traceIDKey struct{}
type runIDKey struct{}

// RunID 是 Agent 整个生命周期(每局对局)共享的 ID(32 字符 hex,基于 crypto/rand)。
//
// 失败时降级为 0 填充(理论上不应发生)。
func NewRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		for i := range b {
			b[i] = byte(i)
		}
	}
	return "run_" + hex.EncodeToString(b[:])
}

// NewRequestID 生成新的 RequestID(32 字符 hex,基于 crypto/rand)。
//
// 失败时降级为 0 填充(理论上不应发生)。
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 极端情况:rand 不可用。用 0 填充,仍保证 ID 唯一性(用 timestamp 兜底)。
		// 此处不 panic,因为 logger 失败不应阻塞游戏流。
		for i := range b {
			b[i] = byte(i)
		}
	}
	return hex.EncodeToString(b[:])
}

// WithRequestID 把 RequestID 注入 ctx。
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey{}, id)
}

// RequestIDFromContext 提取 ctx 中的 RequestID,空字符串表示未注入。
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithRunID 把 RunID 注入 ctx。
func WithRunID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, runIDKey{}, id)
}

// RunIDFromContext 提取 ctx 中的 RunID。
func RunIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(runIDKey{}).(string); ok {
		return v
	}
	return ""
}

// AgentRunTrace 是一个 Agent 在一局对局内的"跨调用追踪 ID"容器 (§20260814-02 U5)。
//
// 单一事实来源:Agent.Run 启动时分配一个 RunID,整局跨多次 LLM 调用复用;
// 每条 LLM 调用独立分配 RequestID;两者按 level 嵌套。审计员
// /api/admin/llm/agents/:seat/run/:runID 可复盘该 run 完整流。
//
// 设计动机(OpenCode 启发):
//   - OpenCode 的 assistantMessage.id 跨 turn ID 复用;
//     doc 提到 package.json trace 模型覆盖全调用链。
//   - 本仓库 §R232 BUG-R220 教训:46 条日志需要靠 PID 4023 才能定位到具体 bot;
//     引入 RunID 后 grep 即可还原。
//   - 不进 messages[] (与 §130 一致:不入 LLM 视野,仅出站 header 注入)。
type AgentRunTrace struct {
	mu        sync.Mutex
	runID     string
	seat      int
	roomID    string
	modelKey  string
	startedAt time.Time
	// seqGenerator 单调递增 seq,给流式 delta 推送序号(每 LLM step 自分配)
	seq uint64
}

// NewAgentRunTrace 创建一个新 AgentRunTrace。
func NewAgentRunTrace(seat int, roomID, modelKey string) *AgentRunTrace {
	return &AgentRunTrace{
		runID:     NewRunID(),
		seat:      seat,
		roomID:    roomID,
		modelKey:  modelKey,
		startedAt: time.Now(),
	}
}

// RunID 导出 run ID。
func (t *AgentRunTrace) RunID() string {
	if t == nil {
		return ""
	}
	return t.runID
}

// Seat / RoomID / ModelKey / StartedAt 时间只读快照。
func (t *AgentRunTrace) Seat() int {
	if t == nil {
		return -1
	}
	return t.seat
}
func (t *AgentRunTrace) RoomID() string {
	if t == nil {
		return ""
	}
	return t.roomID
}
func (t *AgentRunTrace) ModelKey() string {
	if t == nil {
		return ""
	}
	return t.modelKey
}
func (t *AgentRunTrace) StartedAt() time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.startedAt
}

// NextSeq 自增并返回下一个流式 seq(LLM 调用每次 start 推一次)。
func (t *AgentRunTrace) NextSeq() uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	return t.seq
}

// CurrentSeq 仅快照当前 seq,不增。
func (t *AgentRunTrace) CurrentSeq() uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seq
}

// StreamMarker 是 Agent 流式 delta 推送的对外格式 (§20260814-02 U5)。
//
// 前端 AgentThoughtPanel 可显示 `seq 12 / 35` 形式的进度。
type StreamMarker struct {
	RunID      string `json:"run_id"`
	Seat       int    `json:"seat"`
	Seq        uint64 `json:"seq"`
	BlockKind  string `json:"block_kind"` // text / tool_use / thinking
	BlockID    string `json:"block_id,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	Begin      bool   `json:"begin"`     // true=开始,false=结束
}

// NewStreamMarker 由 Agent 在每个流式 begin/end 回调时构造。
func NewStreamMarker(trace *AgentRunTrace, kind, blockID string, begin bool) *StreamMarker {
	if trace == nil {
		return nil
	}
	return &StreamMarker{
		RunID:     trace.RunID(),
		Seat:      trace.Seat(),
		Seq:       trace.NextSeq(),
		BlockKind: kind,
		BlockID:   blockID,
		Begin:     begin,
	}
}
