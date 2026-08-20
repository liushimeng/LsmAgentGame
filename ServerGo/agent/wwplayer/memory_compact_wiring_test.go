// Package wwplayer — memory_compact_wiring_test.go: §20260813-02 U1 + §20260820-01 接线测试。
//
// 缺陷背景(§130 第六次复现):CompactWithLLM 早已完整实现,但
// Agent.compactConfig 无任何 setter,Enabled 恒 false,run.go 触发判断永不
// 生效。本文件用 5+3 条断言锁住接线:
//
//	U1-01 SetCompactConfig 接线:setter 注入后配置可见(旧代码无 setter,编译期失败)
//	U1-02 触发路径:Enabled + 消息数达阈值 → 压缩真实执行,消息数下降 + 摘要落库
//	U1-03 失败显式回退:provider 报错 → 规则式压缩兜底 + BotTranscript 可观测标记
//	U1-04 增量摘要 prompt:有上次摘要 → PRESERVE+ADD 模式;无 → 全量模式
//	U1-05 配对完整性:recentMsgs 头部悬空 tool_result 被 dropLeadingOrphans 剔除
//
// 2026-08-20 §20260820-01 新增:
//	V1-01 8 段结构化摘要:fake provider 返回 8 段 → Success=true 且 SectionCount >= 6
//	V1-02 视角隔离:预言家 fake provider 返回含"女巫用药" → Success=false(校验失败)
//	V1-03 质量校验:fake provider 返回 < 100 字 → Success=false
//	V1-04 身份标注:serializeMessagesForCompact 标注 mySeat 为"玩家编号#X"
package wwplayer

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// compactFakeProvider 按脚本返回固定摘要或错误。
type compactFakeProvider struct {
	calls   atomic.Int32
	summary string
	err     error
	// lastPrompt 记录最近一次请求的 user 文本(供增量 prompt 断言)。
	lastPrompt atomic.Value
	// lastSystem 记录最近一次请求的 system 文本(供视角隔离断言)。
	lastSystem atomic.Value
}

func (p *compactFakeProvider) Chat(_ context.Context, _ string, req llm.LLMRequest) (llm.LLMResponse, error) {
	p.calls.Add(1)
	for _, s := range req.System {
		p.lastSystem.Store(s.Text)
	}
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if b.Type == "text" {
				p.lastPrompt.Store(b.Text)
			}
		}
	}
	if p.err != nil {
		return llm.LLMResponse{}, p.err
	}
	return llm.LLMResponse{
		Content: []llmtypes.ContentBlock{{Type: "text", Text: p.summary}},
	}, nil
}

func (p *compactFakeProvider) ChatStream(_ context.Context, _ string, _ llm.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (p *compactFakeProvider) ProviderType() string { return "fake" }

// seedCompactableMemory 构造含 identity + n 条普通 user 消息的 Memory。
func seedCompactableMemory(n int) *Memory {
	m := NewMemory("villager", "good", "放逐全部狼人", 3)
	for i := 0; i < n; i++ {
		m.Push(llm.Message{
			Role:    "user",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "第" + strings.Repeat("x", 10) + "轮游戏上下文"}},
		})
		m.Push(llm.Message{
			Role:    "assistant",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "好的"}},
		})
	}
	return m
}

// validEightSectionSummary 是一个能通过所有校验的 8 段摘要。
// 用于测试 fake provider 返回合格摘要时 Success=true。
const validEightSectionSummary = `## S1. 我的私有情报
该身份无独立私有情报。

## S2. 已确认事实
夜 1: 3 号被狼刀死亡。

## S3. 我的关键决策与理由
夜 1: 我 vote → 5 号(理由:发言可疑)。

## S4. 玩家公开行为
玩家编号#1: 发言 "我是预言家", 投票 5
玩家编号#5: 发言 "我是平民", 投票 1

## S5. 我对各玩家的阵营判断
玩家编号#1: 判定为狼(高置信度)
玩家编号#5: 判定为好人(中置信度)

## S6. 待验证信息
玩家编号#3 身份未明。

## S7. 当前局势提示
存活 11 人,神职 4,平民 5,狼 2。

## S8. 上次压缩以来的新增
(全量模式,无)`

// TestCompactWiring_U1_01_SetterWiresConfig 断言 setter 接线存在且生效。
// 旧代码路径(无 SetCompactConfig)下本测试无法编译 —— 这本身就是 lint 级防护;
// 运行时断言防止未来有人把 setter 改成 no-op。
func TestCompactWiring_U1_01_SetterWiresConfig(t *testing.T) {
	a := &Agent{}
	if got := a.CompactConfigSnapshot().Enabled; got {
		t.Fatal("zero Agent compactConfig.Enabled must be false (default off)")
	}
	cfg := DefaultCompactConfig()
	cfg.MaxTokens = 2048
	cfg.EightSectionsEnabled = true
	a.SetCompactConfig(cfg)
	got := a.CompactConfigSnapshot()
	if !got.Enabled {
		t.Fatal("SetCompactConfig did not wire Enabled=true (§130 wiring regression)")
	}
	if got.MaxTokens != 2048 {
		t.Fatalf("MaxTokens = %d, want 2048", got.MaxTokens)
	}
	if got.TimeoutSec <= 0 {
		t.Fatalf("TimeoutSec = %d, want > 0", got.TimeoutSec)
	}
	if !got.EightSectionsEnabled {
		t.Fatal("EightSectionsEnabled = false, want true (default)")
	}
}

// TestCompactWiring_U1_02_TriggerPath 断言 maybeCompactMemory 真正触发压缩。
// 双向验证:把 SetCompactConfig 的 Enabled 改回 false(模拟「未接线」旧行为),
// 本测试在「消息数不变 + 摘要为空」处失败。
func TestCompactWiring_U1_02_TriggerPath(t *testing.T) {
	a := &Agent{Seat: 3, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25) // 1 + 50 条 > MinMessages(10)
	prov := &compactFakeProvider{summary: validEightSectionSummary}
	a.Provider = prov
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	a.SetCompactConfig(cfg)

	before := a.Memory.Len()
	rp := func() (string, string, int, []int, int, int, bool) {
		return "speak", "villager", 3, []int{1, 2, 3}, -1, -1, false
	}
	a.maybeCompactMemory(context.Background(), rp, &wwtypes.GameContext{MySeat: 3, Round: 2, Role: "villager", Faction: "good"})

	// 压缩在 goroutine 内执行,等待完成。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.Memory.LastCompactSummary() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.Memory.LastCompactSummary() == "" {
		t.Fatal("compact did not execute: lastCompactSummary empty (wiring regression — trigger path dead)")
	}
	if got := a.Memory.Len(); got >= before {
		t.Fatalf("compact did not shrink memory: before=%d after=%d", before, got)
	}
	if prov.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want exactly 1", prov.calls.Load())
	}
	a.Lock()
	fallback := a.compactFallback
	note := a.compactNote
	a.Unlock()
	if fallback {
		t.Fatalf("successful compact must not set fallback marker, note=%q", note)
	}
	if !strings.Contains(note, "压缩成功") {
		t.Fatalf("compact note missing success marker: %q", note)
	}
	// 2026-08-20 §20260820-01 — 成功路径 note 含 8 段 schema 标识。
	if !strings.Contains(note, "8段") {
		t.Fatalf("compact note missing 8-section marker: %q", note)
	}
}

// TestCompactWiring_U1_03_FallbackExplicit 断言 LLM 压缩失败显式回退规则式压缩,
// 且可观测标记被写入(禁止假成功,OpenClaw Context §6.2)。
// 双向验证:删除 setCompactOutcome 调用 → fallback/note 断言失败。
func TestCompactWiring_U1_03_FallbackExplicit(t *testing.T) {
	a := &Agent{Seat: 1, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25)
	prov := &compactFakeProvider{err: errors.New("upstream 500")}
	a.Provider = prov
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	a.SetCompactConfig(cfg)

	rp := func() (string, string, int, []int, int, int, bool) {
		return "speak", "villager", 1, []int{1, 2}, -1, -1, false
	}
	a.maybeCompactMemory(context.Background(), rp, &wwtypes.GameContext{MySeat: 1, Round: 1, Role: "villager", Faction: "good"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.Lock()
		done := a.compactAt != 0
		a.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.Lock()
	fallback := a.compactFallback
	note := a.compactNote
	at := a.compactAt
	a.Unlock()
	if at == 0 {
		t.Fatal("compact outcome never recorded (observability regression)")
	}
	if !fallback {
		t.Fatal("fallback marker not set after LLM compact failure (fake-success forbidden)")
	}
	if !strings.Contains(note, "回退") {
		t.Fatalf("fallback note missing 回退 marker: %q", note)
	}
	// 摘要不得写入(失败路径不允许假成功摘要)。
	if s := a.Memory.LastCompactSummary(); s != "" {
		t.Fatalf("failed compact must not store summary, got %q", s)
	}
}

// TestCompactWiring_U1_04_IncrementalPrompt 断言增量更新模式:
// 有上次摘要 → prompt 含 PRESERVE + 旧摘要全文;无 → 全量模式。
// 双向验证:删除 buildCompactUserPrompt 的增量分支 → PRESERVE 断言失败。
func TestCompactWiring_U1_04_IncrementalPrompt(t *testing.T) {
	// 纯函数层:全量 vs 增量。
	full := buildCompactUserPrompt(true, "", 2, 3, "seer", "good", "CONV")
	if strings.Contains(full, "PRESERVE") || strings.Contains(full, "previous_summary") {
		t.Fatal("empty prevSummary must use full-rebuild prompt, got incremental markers")
	}
	inc := buildCompactUserPrompt(true, "旧摘要:3号是金水", 2, 3, "seer", "good", "CONV")
	if !strings.Contains(inc, "PRESERVE") || !strings.Contains(inc, "旧摘要:3号是金水") {
		t.Fatalf("incremental prompt missing PRESERVE / prev summary: %q", inc)
	}

	// 全链路:第一次压缩(无上次摘要)→ Incremental=false;预置摘要后再压 → true。
	a := &Agent{Seat: 2, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25)
	prov := &compactFakeProvider{summary: validEightSectionSummary}
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	gc := &wwtypes.GameContext{MySeat: 2, Round: 2, Role: "villager", Faction: "good"}
	r1 := a.Memory.CompactWithLLM(context.Background(), prov, "k", "fake-model", gc, cfg)
	if !r1.Success {
		t.Fatalf("first compact failed: %v", r1.Error)
	}
	if r1.Incremental {
		t.Fatal("first compact (no prev summary) must be full mode")
	}
	// 再塞够消息触发第二次(直接调用,不经过 compactDone — 测 Memory 层语义)。
	for i := 0; i < 20; i++ {
		a.Memory.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "更多上下文更多上下文"}}})
	}
	r2 := a.Memory.CompactWithLLM(context.Background(), prov, "k", "fake-model", gc, cfg)
	if !r2.Success {
		t.Fatalf("second compact failed: %v", r2.Error)
	}
	if !r2.Incremental {
		t.Fatal("second compact (prev summary exists) must be incremental mode")
	}
	prompt, _ := prov.lastPrompt.Load().(string)
	if !strings.Contains(prompt, "PRESERVE") || !strings.Contains(prompt, "S1. 我的私有情报") {
		t.Fatalf("incremental wire prompt missing PRESERVE / prev summary content")
	}
}

// TestCompactWiring_U1_05_PairIntegrity 断言压缩后无悬空 tool_result
// (§82b:严格代理见到孤儿 tool_result 直接 400)。
// 双向验证:删除 CompactWithLLM 里的 dropLeadingOrphans 调用 → 断言失败。
func TestCompactWiring_U1_05_PairIntegrity(t *testing.T) {
	m := NewMemory("villager", "good", "放逐全部狼人", 0)
	// identity(0) + 2 对普通消息(1..4),使 msgCount=16 时 splitIdx=16/3=5,
	// 下方孤儿 tool_result 恰好落在切分边界 msgs[5](recentMsgs 头部)——
	// 这是生产里唯一会产生孤儿的位置(其配对 tool_use 落在被压缩的旧段)。
	for i := 0; i < 2; i++ {
		m.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "上下文内容上下文内容"}}})
		m.Push(llm.Message{Role: "assistant", Content: []llmtypes.ContentBlock{{Type: "text", Text: "回复"}}})
	}
	// msgs[5] = 孤儿 tool_result(其配对 tool_use 在被压缩的旧段里 → 必被剔除)。
	orphan := llm.Message{
		Role: "user",
		Content: []llmtypes.ContentBlock{{
			Type: "tool_result", ToolUseID: "toolu_orphan_1",
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "OK"}},
		}},
	}
	m.Push(orphan)
	for i := 0; i < 10; i++ {
		m.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "近端消息近端消息近端消息"}}})
	}
	prov := &compactFakeProvider{summary: validEightSectionSummary}
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10
	res := m.CompactWithLLM(context.Background(), prov, "k", "fake-model",
		&wwtypes.GameContext{MySeat: 0, Round: 1, Role: "villager", Faction: "good"}, cfg)
	if !res.Success {
		t.Fatalf("compact failed: %v", res.Error)
	}
	msgs, _ := m.Snapshot()
	for i, msg := range msgs {
		for _, b := range msg.Content {
			if b.Type == "tool_result" && b.ToolUseID == "toolu_orphan_1" {
				t.Fatalf("orphan tool_result survived compact at msgs[%d] (dropLeadingOrphans missing)", i)
			}
		}
	}
	// 头部结构:identity + compact 摘要,且 compact 之后的第一条不是孤儿 tool_result。
	if len(msgs) < 2 {
		t.Fatalf("post-compact messages too short: %d", len(msgs))
	}
}

// ─── 2026-08-20 §20260820-01 — 8 段 schema + 视角隔离 + 质量校验 测试 ───

// TestCompact_V1_01_EightSectionSchema 断言 fake provider 返回 8 段时:
//   - Success=true
//   - SectionCount >= 6
//   - SummaryLen >= 100
// 双向验证:把 IsValidCompactSummary 的 SectionCount 阈值改回 0 → 测试仍 PASS
// (无法形成 lint 级防护);故额外断言 SectionCount 实际值。
func TestCompact_V1_01_EightSectionSchema(t *testing.T) {
	a := &Agent{Seat: 3, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25)
	prov := &compactFakeProvider{summary: validEightSectionSummary}
	a.Provider = prov
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10

	gc := &wwtypes.GameContext{MySeat: 3, Round: 2, Role: "villager", Faction: "good"}
	res := a.Memory.CompactWithLLM(context.Background(), prov, "k", "fake-model", gc, cfg)
	if !res.Success {
		t.Fatalf("valid 8-section summary must compact successfully, got error: %v", res.Error)
	}
	if res.SectionCount < 6 {
		t.Fatalf("SectionCount = %d, want >= 6 (8-section schema)", res.SectionCount)
	}
	if res.SummaryLen < 100 {
		t.Fatalf("SummaryLen = %d, want >= 100 (G6 quality threshold)", res.SummaryLen)
	}
}

// TestCompact_V1_02_PerspectiveIsolation 断言视角隔离校验:
// 预言家 bot 调压缩,LLM 返回摘要混入「女巫用药」→ 校验失败 → fallback。
// 双向验证:把 privateInfoBlacklist 中的 WitchActHistory 移除 → 校验放行 → 测试失败。
func TestCompact_V1_02_PerspectiveIsolation(t *testing.T) {
	a := &Agent{Seat: 3, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25)
	// 预言家视角,摘要混入 WitchActHistory 关键词 → 视角隔离违例。
	violatingSummary := validEightSectionSummary + "\n## 附注\nWitchActHistory: 女巫已用解药救 5 号。"
	prov := &compactFakeProvider{summary: violatingSummary}
	a.Provider = prov
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10

	gc := &wwtypes.GameContext{MySeat: 3, Round: 2, Role: "seer", Faction: "good"}
	res := a.Memory.CompactWithLLM(context.Background(), prov, "k", "fake-model", gc, cfg)
	if res.Success {
		t.Fatal("seer bot summary mentioning WitchActHistory must fail perspective-isolation check (G3)")
	}
	if res.Error == nil || !strings.Contains(res.Error.Error(), "perspective isolation violated") {
		t.Fatalf("error must mention perspective isolation, got: %v", res.Error)
	}
	// 失败路径不得写入 lastCompactSummary(OpenClaw §6.2 禁止假成功)。
	if s := a.Memory.LastCompactSummary(); s != "" {
		t.Fatalf("failed perspective-isolation check must not store summary, got %q", s)
	}
}

// TestCompact_V1_03_QualityGuardTooShort 断言质量校验:
// 摘要 < 100 字 → 校验失败 → fallback。
// 双向验证:把 IsValidCompactSummary 的 SummaryLen 阈值改回 0 → 测试失败。
func TestCompact_V1_03_QualityGuardTooShort(t *testing.T) {
	a := &Agent{Seat: 3, ModelKey: "fake-model"}
	a.Memory = seedCompactableMemory(25)
	shortSummary := "## S1. 我的私有情报\n无\n## S2. 已确认\n无\n## S3. 决策\n无\n## S4. 玩家\n无\n## S5. 判断\n无\n## S6. 待验证\n无\n## S7. 局势\n无"
	prov := &compactFakeProvider{summary: shortSummary}
	a.Provider = prov
	cfg := DefaultCompactConfig()
	cfg.MinMessages = 10

	gc := &wwtypes.GameContext{MySeat: 3, Round: 2, Role: "villager", Faction: "good"}
	res := a.Memory.CompactWithLLM(context.Background(), prov, "k", "fake-model", gc, cfg)
	if res.Success {
		t.Fatal("summary < 100 chars must fail quality guard (G6)")
	}
	if res.Error == nil || !strings.Contains(res.Error.Error(), "too short") {
		t.Fatalf("error must mention too short, got: %v", res.Error)
	}
}

// TestCompact_V1_04_SeatAnnotation 断言 serializeMessagesForCompact 按 mySeat 标注身份:
//   - assistant text → "我自己"(对应 玩家编号#X)
//   - assistant tool_use → "玩家编号#X 决策: ..."
//   - user text 含座位标识 → 提取并标注
func TestCompact_V1_04_SeatAnnotation(t *testing.T) {
	msgs := []llm.Message{
		// 模拟一条 user text 含「玩家编号#3」
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "玩家编号#3: 我是预言家,昨晚查了 5 号是金水。"}}},
		// 模拟一条 assistant text(默认标注 mySeat)
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "text", Text: "好的,5号金水"}}},
		// 模拟一条 assistant tool_use(默认标注 mySeat)
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "tool_use", ID: "tu1", Name: "speak", Input: map[string]any{"text": "我查了5号是金水"}}}},
		// 模拟一条 user tool_result
		{Role: "user", Content: []llm.ContentBlock{{Type: "tool_result", ToolUseID: "tu1", Content: []llm.ContentBlock{{Type: "text", Text: "speak OK"}}}}},
	}
	// mySeat=2 → 玩家编号#3
	out := serializeMessagesForCompact(msgs, 2)
	if !strings.Contains(out, "玩家编号#3") {
		t.Fatalf("user text seat not extracted; output=%q", out)
	}
	if !strings.Contains(out, "玩家编号#3 决策") {
		t.Fatalf("assistant tool_use not annotated with mySeat; output=%q", out)
	}
	if !strings.Contains(out, "玩家编号#?") {
		t.Fatalf("tool_result without seat marker should fall back to #?; output=%q", out)
	}
	// 不可出现「我」「某人」「该玩家」等指代不清的措辞(系统 prompt 强制要求)
	for _, bad := range []string{"某人", "该玩家"} {
		if strings.Contains(out, bad) {
			t.Fatalf("output contains vague pronoun %q: %q", bad, out)
		}
	}
}

// TestCompact_V1_05_SystemPromptRoleRouted 断言 system prompt 按 role 路由:
//   - seer → 含 "MySeerCheckHistory"
//   - werewolf → 含 "WolfTeammateSeat"
//   - villager → 含 "无独立私有情报"
func TestCompact_V1_05_SystemPromptRoleRouted(t *testing.T) {
	seerSys := buildCompactSystemPrompt("seer", "good")
	if !strings.Contains(seerSys, "MySeerCheckHistory") {
		t.Fatalf("seer system prompt missing MySeerCheckHistory: %q", seerSys)
	}
	wolfSys := buildCompactSystemPrompt("werewolf", "wolf")
	if !strings.Contains(wolfSys, "WolfTeammateSeat") {
		t.Fatalf("werewolf system prompt missing WolfTeammateSeat: %q", wolfSys)
	}
	villagerSys := buildCompactSystemPrompt("villager", "good")
	if !strings.Contains(villagerSys, "无独立私有情报") {
		t.Fatalf("villager system prompt missing 无独立私有情报: %q", villagerSys)
	}
	// 不应出现"女巫用药"等违规关键词(防止 system prompt 自身违反视角隔离)
	for _, sys := range []string{seerSys, wolfSys, villagerSys} {
		if strings.Contains(sys, "女巫用药") && !strings.Contains(sys, "禁止") {
			t.Fatalf("system prompt mentions 女巫用药 without 禁止 guard: %q", sys)
		}
	}
}