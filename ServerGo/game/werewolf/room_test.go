// Package werewolf — regression tests for BUG-WEREWOLF-AGENT-PANEL-NULL.
//
// populateBotContexts fills game.state.bot_contexts[] (consumed by the React
// AgentThoughtPanel). Two code paths inside it were serializing slice fields
// as JSON `null` instead of `[]`, which crashed the panel with
// `Cannot read properties of null (reading 'length')` and unmounted the
// whole game page (React 18 error boundary). These tests pin the wire
// shape so the bug cannot regress:
//
//  1. placeholder path   — agent wired but never completed a decision
//     (BotTranscript() == nil), so populateBotContexts emits a stub.
//  2. sanitized path     — sanitizeBotTranscript is exercised as a pure
//     function: mixed-mode human players must never see null slice fields
//     on the wire even when the source transcript had nil slices.
//
// The third code path (spectator pass-through) is implicitly covered by
// `recordTranscript` in the agent package: it always builds RecentMessages
// and ToolCalls via `make([]string, 0, n)` so the field is never nil there.
// Re-asserting it here would require test-only access to agent.lastTranscript
// (same-package field); instead we trust the agent-side invariant and focus
// on the two paths that have historically produced nil.
package werewolf

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// rawBotContext is a minimal projection of ClientGameState.bot_contexts[],
// matching the wire tags emitted by wwplayer.BotTranscript. We re-declare it
// here (rather than importing ClientGameState) because the assertion is
// about JSON output, not internal Go shape — keeping it local means a future
// refactor of BotContext's transport struct can't silently relax the
// invariant the React panel depends on.
type rawBotContext struct {
	Seat      int      `json:"seat"`
	Model     string   `json:"model"`
	LastTool  string   `json:"last_tool"`
	ToolCalls []string `json:"tool_calls"`
	UpdatedAt int64    `json:"updated_at"`
}

// stubAgentNoTranscript builds a bare *wwplayer.Agent whose lastTranscript is
// the zero-value nil. This is exactly the state of a freshly-wired bot that
// has not yet completed a decision — populateBotContexts' "placeholder"
// branch is what runs for it.
func stubAgentNoTranscript(seat int, modelKey string) *wwplayer.Agent {
	return &wwplayer.Agent{
		Seat:     seat,
		ModelKey: modelKey,
	}
}

// decodeBotContexts JSON-decodes game.state.bot_contexts[] back into a slice
// we can assert on. Goes via the full ClientGameState path so we also catch
// regressions in the view-level omitempty / wrapper logic.
func decodeBotContexts(t *testing.T, bcs *ClientGameState) []rawBotContext {
	t.Helper()
	raw, err := json.Marshal(bcs)
	if err != nil {
		t.Fatalf("marshal ClientGameState: %v", err)
	}
	var wrapper struct {
		BotContexts []rawBotContext `json:"bot_contexts"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal bot_contexts: %v (payload=%s)", err, raw)
	}
	return wrapper.BotContexts
}

// TestPopulateBotContexts_PlaceholderSerializesEmptyArrays is the core
// BUG-WEREWOLF-AGENT-PANEL-NULL regression. Before the fix, the placeholder
// branch emitted an wwplayer.BotTranscript{} literal whose RecentMessages and
// ToolCalls slices were nil — encoding/json renders nil []string as `null`,
// which broke React's AgentThoughtPanel (read .recent_messages.length) and
// crashed the page.
func TestPopulateBotContexts_PlaceholderSerializesEmptyArrays(t *testing.T) {
	r := &WerewolfRoom{
		BotAgents: map[int]*wwplayer.Agent{},
	}
	// Two bots, neither has completed a decision (lastTranscript == nil).
	r.BotAgents[0] = stubAgentNoTranscript(0, "MinMax-model")
	r.BotAgents[3] = stubAgentNoTranscript(3, "Qwen-model")

	cs := &ClientGameState{
		MySeat: -1, // spectator: full bot data is allowed on this path
	}
	r.populateBotContexts(cs)

	if len(cs.BotContexts) != 2 {
		t.Fatalf("expected 2 bot contexts, got %d", len(cs.BotContexts))
	}

	// Direct in-Go assertion — RecentMessages / ToolCalls must be non-nil
	// slices, not nil. BUG FIX 2026-07-09 §13.6: 占位分支现在携带
	// "等待发言中"占位文本 + last_thinking 友好提示,所以 RecentMessages
	// 至少 1 条(而不是 0);RecentMessages / ToolCalls 必须是非 nil。
	//
	// 2026-07-09 §重构 - 适配 "Agent 思考 → Agent 交互":
	// 占位文本从 LastThinking 迁移到 LastDecisionSummary;RecentMessages 仍
	// 是空切片(前端不再渲染,仅 null-safety 防御)。RecentMessages / ToolCalls
	// 仍必须是非 nil(避免前端 React AgentThoughtPanel 抛 null.length)。
	for i, bc := range cs.BotContexts {
		// §128 对话即思考重构:LastThinking / RecentMessages 字段已删除。
		if bc.ToolCalls == nil {
			t.Errorf("bot[%d] seat=%d ToolCalls is nil; expected non-nil slice",
				i, bc.Seat)
		}
		if bc.LastDecisionSummary == "" {
			t.Errorf("bot[%d] seat=%d LastDecisionSummary is empty; expected placeholder text",
				i, bc.Seat)
		}
		if len(bc.ToolCalls) != 0 {
			t.Errorf("bot[%d] seat=%d ToolCalls len=%d, want 0",
				i, bc.Seat, len(bc.ToolCalls))
		}
	}

	// JSON wire assertion — the actual user-facing contract.
	decoded := decodeBotContexts(t, cs)
	if len(decoded) != 2 {
		t.Fatalf("JSON: expected 2 bot contexts, got %d", len(decoded))
	}
	for i, bc := range decoded {
		// §128:RecentMessages 字段已删除,只断言 ToolCalls 仍为非 nil。
		if bc.ToolCalls == nil {
			t.Errorf("JSON bot[%d] seat=%d tool_calls is null; want [] "+
				"(this is what triggered BUG-WEREWOLF-AGENT-PANEL-NULL)",
				i, bc.Seat)
		}
	}

	// String-level sanity check that the JSON literal is `[]`, not `null`.
	// §128:RecentMessages 已删除,只断言 tool_calls 仍为 `[]`。
	raw, _ := json.Marshal(cs)
	payload := string(raw)
	if strings.Contains(payload, `"tool_calls":null`) {
		t.Errorf("payload contains `\"tool_calls\":null`; full payload:\n%s", payload)
	}
	if !strings.Contains(payload, `"tool_calls":[]`) {
		t.Errorf("payload should contain `\"tool_calls\":[]`; full payload:\n%s", payload)
	}
}

// TestSanitizeBotTranscript_NilSlicesBecomeEmptyArrays is the matching
// regression for the sanitize path. §128 对话即思考重构:RecentMessages
// 字段已物理删除,只断言 ToolCalls 仍为非 nil。
func TestSanitizeBotTranscript_NilSlicesBecomeEmptyArrays(t *testing.T) {
	cases := []struct {
		name string
		in   wwplayer.BotTranscript
	}{
		{
			name: "fully_nil_slices",
			in: wwplayer.BotTranscript{
				Seat:  0,
				Model: "MinMax-model",
				// ToolCalls left as nil zero values.
			},
		},
		{
			name: "nil_tool_calls_only",
			in: wwplayer.BotTranscript{
				Seat:  1,
				Model: "Qwen-model",
				// ToolCalls nil.
			},
		},
		{
			name: "sensitive_tool_filtered",
			in: wwplayer.BotTranscript{
				Seat:      2,
				Model:     "GLM-model",
				ToolCalls: []string{"wolf_kill: target=3"}, // sensitive → masked
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizeBotTranscript(tc.in, false)

			if out.ToolCalls == nil {
				t.Errorf("ToolCalls is nil after sanitize; want []string{} (possibly with [已隐藏])")
			}

			// JSON wire shape.
			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			payload := string(raw)
			if strings.Contains(payload, `"ToolCalls":null`) ||
				strings.Contains(payload, `"tool_calls":null`) {
				t.Errorf("payload contains null tool_calls:\n%s", payload)
			}
		})
	}
}

// TestSanitizeBotTranscript_KeepsNonSensitiveTools confirms the filter
// behavior didn't regress: non-sensitive tool_calls pass through, sensitive
// ones become `name: [已隐藏]`.
func TestSanitizeBotTranscript_KeepsNonSensitiveTools(t *testing.T) {
	in := wwplayer.BotTranscript{
		Seat: 4,
		ToolCalls: []string{
			"speak: voted seat 1",
			"wolf_kill: target=5", // sensitive
			"vote: seat=2",
			"seer_check: result=wolf", // sensitive
		},
	}
	out := sanitizeBotTranscript(in, false)

	if len(out.ToolCalls) != 4 {
		t.Fatalf("ToolCalls len=%d, want 4 (all entries preserved, sensitive masked)",
			len(out.ToolCalls))
	}
	for _, tc := range out.ToolCalls {
		if strings.Contains(tc, "target=5") {
			t.Errorf("wolf_kill target leaked through sanitize: %q", tc)
		}
		if strings.Contains(tc, "result=wolf") {
			t.Errorf("seer_check result leaked through sanitize: %q", tc)
		}
	}
	if !strings.Contains(out.ToolCalls[0], "speak") {
		t.Errorf("non-sensitive 'speak' was dropped: %q", out.ToolCalls[0])
	}
	if !strings.Contains(out.ToolCalls[2], "vote") {
		t.Errorf("non-sensitive 'vote' was dropped: %q", out.ToolCalls[2])
	}
}

// TestPopulateBotContexts_EmitsPlaceholdersFromSeatModelKeys 是 BUG-WEREWOLF-
// AGENT-PANEL-EMPTY 的回归测试:当 r.seatModelKeys 已注册 N 个 bot 座位
// (典型场景:全 AI 房间创建后,spectator 加入但 StartAgentsLocked 还没跑,或
// 因为某些原因 BotAgents 仍未填充),populateBotContexts 必须基于
// seatModelKeys 输出 N 个占位条目,绝不能因为 BotAgents 为空就早返回,
// 否则前端 `gameState.bot_contexts === undefined` → 显示「尚无思考内容」,
// 即使房间已经配置了 bot 座位。
func TestPopulateBotContexts_EmitsPlaceholdersFromSeatModelKeys(t *testing.T) {
	r := &WerewolfRoom{
		RoomID:        "r-placeholders",
		seatModelKeys: map[int]string{0: "model-a", 2: "model-b", 5: "model-c"},
		// BotAgents intentionally nil → 模拟 StartAgentsLocked 尚未跑
		// (典型全 AI spectator 等待阶段)。
		BotAgents: nil,
	}
	cs := &ClientGameState{MySeat: -1} // spectator
	r.populateBotContexts(cs)

	if len(cs.BotContexts) != 3 {
		t.Fatalf("expected 3 bot placeholders from seatModelKeys, got %d (raw=%+v)",
			len(cs.BotContexts), cs.BotContexts)
	}
	// 按 seat 升序
	if cs.BotContexts[0].Seat != 0 || cs.BotContexts[1].Seat != 2 || cs.BotContexts[2].Seat != 5 {
		t.Fatalf("placeholders not in ascending seat order: %+v",
			[]int{cs.BotContexts[0].Seat, cs.BotContexts[1].Seat, cs.BotContexts[2].Seat})
	}
	// model_key 必须保留
	for i, want := range []string{"model-a", "model-b", "model-c"} {
		if cs.BotContexts[i].Model != want {
			t.Errorf("BotContexts[%d].Model = %q, want %q", i, cs.BotContexts[i].Model, want)
		}
		// 占位必须显式把 slice 初始化为 [] 而非 nil,否则前端 .length 崩溃
		// §128:RecentMessages 字段已删除,只断言 ToolCalls。
		if cs.BotContexts[i].ToolCalls == nil {
			t.Errorf("BotContexts[%d].ToolCalls is nil (would crash React panel)", i)
		}
	}

	// 序列化后必须是 [] 而非 null —— 这才是「前端看到 bot_contexts 有 N 项」
	// 而不是「undefined → 尚无思考内容」分支。
	raw, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"bot_contexts":null`) {
		t.Errorf("bot_contexts serialized as null, would unmount React panel: %s", raw)
	}
	decoded := decodeBotContexts(t, cs)
	if len(decoded) != 3 {
		t.Errorf("decoded bot_contexts = %d, want 3", len(decoded))
	}
}

// TestStopAgentsLocked_WaitsForGoroutineExit 验证 BUG-WEREWOLF-DISBAND-LEAK
// 修复:stopAgentsLocked 调用后,所有 agent goroutine 必须在 timeout 之内
// 真正退出(通过 WaitGroup 同步),而不是仅 cancel+close 就放行。如果未
// 等到退出,后续 ForceDisbandRoom / RemoveGame 的 m.rooms[roomID] delete
// 会与正在跑的 LLM HTTP 调用产生孤儿 goroutine + TCP 连接泄漏。
//
// 用一个能立即响应 ctx.Done() 的 noop Runner 模拟 agent: Run 看到 ctx
// cancel 后立即 return; stopAgentsLocked 调 Wait() 必须看到 Done()。
func TestStopAgentsLocked_WaitsForGoroutineExit(t *testing.T) {
	r := &WerewolfRoom{
		RoomID:       "r-stop-test",
		BotAgents:    make(map[int]*wwplayer.Agent),
		agentCancels: make(map[int]context.CancelFunc),
	}
	// 启 3 个"伪 agent"协程,各自循环 100ms 检查 ctx。
	for seat := 0; seat < 3; seat++ {
		ctx, cancel := context.WithCancel(context.Background())
		r.agentCancels[seat] = cancel
		r.agentWG.Add(1)
		go func(s int) {
			defer r.agentWG.Done()
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		}(seat)
	}
	// 等待协程稳定进入循环
	time.Sleep(50 * time.Millisecond)

	mgr := &WerewolfManager{}
	done := make(chan struct{})
	go func() {
		mgr.stopAgentsLocked(r)
		close(done)
	}()

	select {
	case <-done:
		// 验证 WaitGroup 计数为 0
		// 注意:sync.WaitGroup 没有公开 Count,改为再次 Wait() 立即返回
		// 作为 proxy。
		immediate := make(chan struct{})
		go func() {
			r.agentWG.Wait()
			close(immediate)
		}()
		select {
		case <-immediate:
			// ok,所有 goroutine 已退出
		case <-time.After(200 * time.Millisecond):
			t.Fatal("agentWG still pending after stopAgentsLocked returned — goroutines leaked")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stopAgentsLocked did not return within 2s — Wait() is blocking or timeout path is broken")
	}
}

// TestSanitizeBotTranscript_R55_RedactsThinkingAndQuarantineReason 是
// BUG-WEREWOLF-R55-PRIVACY 的回归测试:R55 报告人类 villager 可通过
// game.state payload 中的 bot_contexts[*].last_thinking / quarantine_reason
// 字段推断角色身份。sanitizeBotTranscript 必须把这两个字段清空,
// 仅保留能在公屏暴露的信息(model / seat / 占位 tool_calls)。
func TestSanitizeBotTranscript_R55_RedactsThinkingAndQuarantineReason(t *testing.T) {
	in := wwplayer.BotTranscript{
		Seat: 3,
		// §128 对话即思考重构:LastThinking / RecentMessages 字段已物理删除。
		// R55 原始测试关注的是这两个字段的清空,§128 后已无字段可清空,仅保留
		// QuarantineReason / LastTool / ToolCalls 三个仍需脱敏的字段。
		QuarantineReason: "anthropic: http=0 retryable=true source=stream: stream transport dropped: context canceled",
		LastTool:         "wolf_kill",
		ToolCalls:        []string{"wolf_kill: target=4", "speak: 先观察"},
	}
	out := sanitizeBotTranscript(in, false)

	if out.QuarantineReason != "" {
		t.Errorf("QuarantineReason leaked through sanitize: %q", out.QuarantineReason)
	}
	// BUG-R226-P1-01 (2026-08-01): wolf_kill 工具名现在同样抽象化为
	// night_act(此前原样透传是身份侧信道),此处断言工具名不泄露 + 目标脱敏。
	if strings.Contains(out.LastTool, "wolf_kill") {
		t.Errorf("LastTool leaked real tool name: %q", out.LastTool)
	}
	if out.LastTool != "night_act" {
		t.Errorf("LastTool = %q, want night_act", out.LastTool)
	}
	// sensitive tool must be redacted, non-sensitive preserved
	joined := strings.Join(out.ToolCalls, "|")
	if strings.Contains(joined, "target=4") {
		t.Errorf("wolf_kill target leaked: %q", joined)
	}
	if !strings.Contains(joined, "speak") {
		t.Errorf("non-sensitive speak was dropped: %q", joined)
	}
}

// TestSanitizeBotTranscript_R87_P0_1_MasksSensitiveDecisionTarget 是 R87 P0-1
// 的回归测试:LastDecisionSummary 含敏感工具的具体目标("wolf_kill → 7号"),
// 观战者可见等于开全图。sanitizeBotTranscript 必须把目标替换为 [已隐藏]。
// 引用:自动化测试报告_20260710_195056.md P0-1。
func TestSanitizeBotTranscript_R87_P0_1_MasksSensitiveDecisionTarget(t *testing.T) {
	cases := []struct {
		name          string
		lastTool      string
		summary       string
		wantSubstring string
		wantAbsent    string
	}{
		{
			name:          "wolf_kill_target_hidden",
			lastTool:      "wolf_kill",
			summary:       "wolf_kill → 7号",
			wantSubstring: "night_act → [已隐藏]",
			wantAbsent:    "7号",
		},
		{
			name:          "seer_check_target_hidden",
			lastTool:      "seer_check",
			summary:       "seer_check → 0号",
			wantSubstring: "night_act → [已隐藏]",
			wantAbsent:    "0号",
		},
		{
			name:          "witch_act_target_hidden",
			lastTool:      "witch_act",
			summary:       "witch_act → 2号",
			wantSubstring: "night_act → [已隐藏]",
			wantAbsent:    "2号",
		},
		{
			name:          "hunter_shoot_target_hidden",
			lastTool:      "hunter_shoot",
			summary:       "hunter_shoot → 5号",
			wantSubstring: "day_act → [已隐藏]",
			wantAbsent:    "5号",
		},
		{
			name:          "vote_not_sensitive_preserved",
			lastTool:      "vote",
			summary:       "vote → 3号",
			wantSubstring: "vote → 3号",
			wantAbsent:    "[已隐藏]",
		},
		{
			name:          "speak_not_sensitive_preserved",
			lastTool:      "speak",
			summary:       "speak(3号):我怀疑4号",
			wantSubstring: "speak(3号):我怀疑4号",
			wantAbsent:    "[已隐藏]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := wwplayer.BotTranscript{
				Seat:               0,
				LastTool:           tc.lastTool,
				LastDecisionSummary: tc.summary,
			}
			// both viewer paths must mask sensitive targets
			for _, isSpec := range []bool{false, true} {
				out := sanitizeBotTranscript(in, isSpec)
				if !strings.Contains(out.LastDecisionSummary, tc.wantSubstring) {
					t.Errorf("isSpectator=%v LastDecisionSummary=%q; want substring %q",
						isSpec, out.LastDecisionSummary, tc.wantSubstring)
				}
				if tc.wantAbsent != "" && strings.Contains(out.LastDecisionSummary, tc.wantAbsent) {
					t.Errorf("isSpectator=%v LastDecisionSummary=%q; must not contain %q",
						isSpec, out.LastDecisionSummary, tc.wantAbsent)
				}
			}
		})
	}
}

// TestSanitizeBotTranscript_R87_P0_3_HeartThoughtBoundary 是 R87 P0-3 的回归测试:
// HeartThought 是 bot 真实内心独白(含身份/策略)。人类玩家(isSpectator=false)
// 读取等同全图挂,必须清空;观战者(isSpectator=true)依 §119 设计契约保留。
// 引用:自动化测试报告_20260710_195056.md P0-3。
func TestSanitizeBotTranscript_R87_P0_3_HeartThoughtBoundary(t *testing.T) {
	in := wwplayer.BotTranscript{
		Seat:               3,
		HeartThought:       "我是猎人,身份暂时不暴露,先观察局面",
		HeartThoughtAt:     1720000000000,
		LastDecisionSummary: "speak(3号):我是平民,观望",
	}
	// Human player in mixed mode: HeartThought must be cleared.
	outPlayer := sanitizeBotTranscript(in, false)
	if outPlayer.HeartThought != "" {
		t.Errorf("HeartThought leaked to human player: %q", outPlayer.HeartThought)
	}
	if outPlayer.HeartThoughtAt != 0 {
		t.Errorf("HeartThoughtAt leaked to human player: %d", outPlayer.HeartThoughtAt)
	}
	// Spectator: HeartThought preserved per §119.
	outSpec := sanitizeBotTranscript(in, true)
	if outSpec.HeartThought != in.HeartThought {
		t.Errorf("HeartThought must be preserved for spectator; got %q want %q",
			outSpec.HeartThought, in.HeartThought)
	}
	if outSpec.HeartThoughtAt != in.HeartThoughtAt {
		t.Errorf("HeartThoughtAt must be preserved for spectator; got %d want %d",
			outSpec.HeartThoughtAt, in.HeartThoughtAt)
	}
}

// TestSanitizeBotTranscript_R238_P0_1_EmotionIdentityLeak 是 BUG-R238-P0-1 的
// 回归测试:emotion_reason / emotion_caption / emotion_history[].reason 均为
// LLM 自由文本,可能包含身份自述(如「继续隐藏预言家身份」)。人类玩家
// (isSpectator=false) 读取等同全图挂,必须清空;观战者(isSpectator=true)保留。
// 封闭枚举字段(emotion / emotion_effect / emotion_intensity)是公开行为,两条路径均保留。
// 引用:自动化测试报告_20260804_185558.md BUG-R238-P0-1。
func TestSanitizeBotTranscript_R238_P0_1_EmotionIdentityLeak(t *testing.T) {
	in := wwplayer.BotTranscript{
		Seat:           5,
		Emotion:        "irritated",
		EmotionEffect:  "shake",
		EmotionIntensity: "mid",
		EmotionReason:  "恼怒急躁 | 回应10号的质疑,表现出被冤枉的不满,同时继续隐藏预言家身份",
		EmotionCaption: "不是我!",
		EmotionHistory: []wwplayer.EmotionRecord{
			{Emotion: "calm", Reason: "开局观望,隐藏女巫身份", AtMs: 1720000000000},
			{Emotion: "irritated", Reason: "被多人质疑急于自证", AtMs: 1720000001000},
		},
	}
	// Human player: all free-text emotion fields must be cleared.
	outPlayer := sanitizeBotTranscript(in, false)
	if outPlayer.EmotionReason != "" {
		t.Errorf("EmotionReason leaked to human player: %q", outPlayer.EmotionReason)
	}
	if outPlayer.EmotionCaption != "" {
		t.Errorf("EmotionCaption leaked to human player: %q", outPlayer.EmotionCaption)
	}
	for i, rec := range outPlayer.EmotionHistory {
		if rec.Reason != "" {
			t.Errorf("EmotionHistory[%d].Reason leaked to human player: %q", i, rec.Reason)
		}
	}
	// Closed-envelope enums must survive for human player (public behavior).
	if outPlayer.Emotion != in.Emotion {
		t.Errorf("Emotion enum must survive for human player; got %q want %q", outPlayer.Emotion, in.Emotion)
	}
	if outPlayer.EmotionEffect != in.EmotionEffect {
		t.Errorf("EmotionEffect enum must survive for human player; got %q want %q", outPlayer.EmotionEffect, in.EmotionEffect)
	}
	if outPlayer.EmotionIntensity != in.EmotionIntensity {
		t.Errorf("EmotionIntensity enum must survive for human player; got %q want %q", outPlayer.EmotionIntensity, in.EmotionIntensity)
	}

	// Spectator: free-text emotion fields preserved.
	outSpec := sanitizeBotTranscript(in, true)
	if outSpec.EmotionReason != in.EmotionReason {
		t.Errorf("EmotionReason must be preserved for spectator; got %q want %q", outSpec.EmotionReason, in.EmotionReason)
	}
	if outSpec.EmotionCaption != in.EmotionCaption {
		t.Errorf("EmotionCaption must be preserved for spectator; got %q want %q", outSpec.EmotionCaption, in.EmotionCaption)
	}
	for i, rec := range outSpec.EmotionHistory {
		if rec.Reason != in.EmotionHistory[i].Reason {
			t.Errorf("EmotionHistory[%d].Reason must be preserved for spectator; got %q want %q",
				i, rec.Reason, in.EmotionHistory[i].Reason)
		}
	}
	// Closed-envelope enums must also survive for spectator.
	if outSpec.Emotion != in.Emotion {
		t.Errorf("Emotion enum must survive for spectator; got %q want %q", outSpec.Emotion, in.Emotion)
	}
}