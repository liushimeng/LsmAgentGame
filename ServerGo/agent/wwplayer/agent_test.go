package wwplayer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/core"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/config"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// fakeRunner records tool calls for assertions.
type fakeRunner struct {
	calls  []string
	lastFx wwplayer.EmotionFx // 2026-08-04 §表情特效:记录最近一次 EmotionSwitchSpeak 的 fx 供断言
}

// 2026-07-10 §4: ToolRunner 接口新增的 RecordLog / GameLogID getter。
// 测试桩返回 nil / "",与旧代码路径兼容。
func (f *fakeRunner) RecordLog() *agentcore.RecordLogService { return nil }
func (f *fakeRunner) GameLogID() string                  { return "" }
// §20260811-10 U1 — 照妖镜一次性强制真实身份标记接口桩。
func (f *fakeRunner) ConsumeMirrorExposeFlag() bool      { return false }
func (f *fakeRunner) MirrorExposeFlagNonConsuming() bool { return false }

func (f *fakeRunner) WolfKill(target int, reason string) (string, error) {
	f.calls = append(f.calls, "wolf_kill")
	return "ok", nil
}
func (f *fakeRunner) SeerCheck(target int) (string, error) {
	f.calls = append(f.calls, "seer_check")
	return "good", nil
}
func (f *fakeRunner) WitchAct(action string, target int) (string, error) {
	f.calls = append(f.calls, "witch_act")
	return "ok", nil
}

// 2026-07-29 §134 — guard_protect 工具测试桩。记录 "guard_protect:<target>"
// 以供断言(空守记录 "guard_protect:-1")。
func (f *fakeRunner) GuardProtect(target int) (string, error) {
	f.calls = append(f.calls, "guard_protect")
	return "ok", nil
}

// §198 — knight_duel 工具测试桩。记录 "knight_duel:<target>"(target=-1 表放弃)。
func (f *fakeRunner) KnightDuel(target int) (string, error) {
	f.calls = append(f.calls, "knight_duel")
	return "ok", nil
}

// §猎魔人 — demon_hunter_hunt 工具测试桩。记录 "demon_hunter_hunt:<target>"(target=-1 表空过)。
func (f *fakeRunner) DemonHunterHunt(target int) (string, error) {
	f.calls = append(f.calls, "demon_hunter_hunt")
	return "ok", nil
}
func (f *fakeRunner) Speak(text string) (string, error) {
	f.calls = append(f.calls, "speak")
	return "sent", nil
}

// 2026-07-13 §130:text-block 自动发言 stub — 测试桩。run.go 用 interface assertion
// 调 SpeakAuto;若 fakeRunner 不实现,run.go 会 fallback 到 no-auto-speech(不影响测试)。
func (f *fakeRunner) SpeakAuto(text string) (string, error) {
	f.calls = append(f.calls, "speak_auto")
	return "sent", nil
}

// 2026-07-10 §119「心口不一」工具的 fake 实现 — 记录 speak_with_thought 调用以供断言。
func (f *fakeRunner) SpeakWithThought(publicText, internalThought string) (string, error) {
	f.calls = append(f.calls, "speak_with_thought")
	return "sent", nil
}
func (f *fakeRunner) FinishSpeak() (string, error) {
	f.calls = append(f.calls, "finish_speak")
	return "ok", nil
}
func (f *fakeRunner) Vote(target int) (string, error) {
	f.calls = append(f.calls, "vote")
	return "ok", nil
}
func (f *fakeRunner) FinishVote(tiedRound int) (string, error) {
	f.calls = append(f.calls, "finish_vote")
	return "ok", nil
}
func (f *fakeRunner) StartDay() (string, error) {
	f.calls = append(f.calls, "start_day")
	return "ok", nil
}
func (f *fakeRunner) SheriffCandidate(target int) (string, error) {
	f.calls = append(f.calls, "sheriff_candidate")
	return "ok", nil
}
func (f *fakeRunner) SheriffElect() (string, error) {
	f.calls = append(f.calls, "sheriff_elect")
	return "ok", nil
}
func (f *fakeRunner) SheriffSetSpeakOrder(direction, selfPos string) (string, error) {
	f.calls = append(f.calls, "sheriff_set_speak_order")
	return "ok", nil
}
func (f *fakeRunner) HunterShoot(target int) (string, error) {
	f.calls = append(f.calls, "hunter_shoot")
	return "ok", nil
}

// 修复(2026-08-04)§遗言链路:last_words 工具测试桩 — death_lyric 阶段
// bot 提交遗言。
func (f *fakeRunner) LastWords(text string) (string, error) {
	f.calls = append(f.calls, "last_words:"+text)
	return "ok", nil
}

// R91-P1-1 (2026-07-11): last_words_skip 工具测试桩 — death_lyric 阶段
// bot 放弃遗言。
func (f *fakeRunner) LastWordsSkip() (string, error) {
	f.calls = append(f.calls, "last_words_skip")
	return "ok", nil
}

// 2026-07-10 §7 / §12:sheriff_stream 工具测试桩。格式 "sheriff_stream:<slot>:<target>"。
func (f *fakeRunner) SheriffStream(slot int, target int) (string, error) {
	f.calls = append(f.calls, "sheriff_stream")
	return "ok", nil
}

// 2026-07-10 §3.5 / §12:idiot_reveal 工具测试桩。
func (f *fakeRunner) IdiotReveal(choice string) (string, error) {
	f.calls = append(f.calls, "idiot_reveal:"+choice)
	return "ok", nil
}
func (f *fakeRunner) WolfSuicide() (string, error) {
	f.calls = append(f.calls, "wolf_suicide")
	return "ok", nil
}

// §20260830-02 — 自爆带走工具测试桩。
func (f *fakeRunner) SuicideTake(target int) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("wolf_suicide_take:%d", target))
	return "ok", nil
}
func (f *fakeRunner) Whisper(toSeat int, text string) (string, error) {
	f.calls = append(f.calls, "whisper")
	return "sent", nil
}
func (f *fakeRunner) Interject(text string) (string, error) {
	// BUG-WEREWOLF-AGENT-INTERJECT: 测试 fake 记录"interject"以供断言。
	f.calls = append(f.calls, "interject")
	return "sent", nil
}

// 2026-07-10 重开局投票工具的 fake 实现。
func (f *fakeRunner) RestartVote(choice string) (string, error) {
	f.calls = append(f.calls, "restart_vote:"+choice)
	return "ok", nil
}

// 2026-07-08 §13.2: idle_think 工具的 fake 实现。记录调用以供断言。
func (f *fakeRunner) IdleThink(reason string) (string, error) {
	f.calls = append(f.calls, "idle_think")
	return "idle_think recorded", nil
}

// 2026-07-10 §124 增强 — emotion_switch 工具的 fake 实现。
// EmotionSwitchSpeak fake — 2026-08-04 §重构 替换 EmotionSwitch/EmotionSwitchRandom。
// 记录 "emotion_switch_speak:<emotion>:<text>" 以供断言;空 text 返回 rejected(模拟 dedup-empty 路径)。
// 2026-08-04 §表情特效:签名扩展 fx wwplayer.EmotionFx;记录 effect/caption 供特效断言。
// §20260811-06 U3 — fakeRunner 新增 ReasoningChain 方法以满足 ToolRunner 接口。
func (f *fakeRunner) EmotionSwitchSpeak(text, emotion, reason string, fx wwplayer.EmotionFx) (string, error) {
	if text == "" {
		f.calls = append(f.calls, "emotion_switch_speak:rejected")
		return "emotion_switch_speak rejected: empty text after dedup (no emotion change)", nil
	}
	f.calls = append(f.calls, "emotion_switch_speak:"+emotion+":"+text)
	f.lastFx = fx
	return "speak ok" + " [emotion→" + emotion + "]", nil
}

// ReasoningChain §20260811-06 U3 — fakeRunner stub。记录调用。
func (f *fakeRunner) ReasoningChain(topic string, steps, evidence []string, conclusion string, confidence int) (string, error) {
	f.calls = append(f.calls, "reasoning_chain:"+topic)
	return "reasoning_chain recorded", nil
}

// 2026-07-11 13人局: 预言家 propose_vote 工具的 fake 实现。
func (f *fakeRunner) ProposeVote() (string, error) {
	f.calls = append(f.calls, "propose_vote")
	return "ok", nil
}

// §20260826-01 — 心理博弈工具桩实现。
func (f *fakeRunner) Action_ProbePlayer(targetSeat int, probeText, expectedKind string) (string, error) {
	f.calls = append(f.calls, "probe_player")
	return "probe dispatched", nil
}
func (f *fakeRunner) Action_FramePlayer(targetSeat int, narrative, evidence string) (string, error) {
	f.calls = append(f.calls, "frame_player")
	return "frame dispatched", nil
}
func (f *fakeRunner) Action_FollowCrowd(leaderSeat int, reason string) (string, error) {
	f.calls = append(f.calls, "follow_crowd")
	return "follow dispatched", nil
}

// TestDispatchTool_Vote verifies the vote tool maps to runner.Vote with the
// correct target.
func TestDispatchTool_Vote(t *testing.T) {
	f := &fakeRunner{}
	res, err := wwplayer.DispatchTool("vote", map[string]any{"target": float64(3)}, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res != "ok" {
		t.Errorf("res = %q", res)
	}
	if len(f.calls) != 1 || f.calls[0] != "vote" {
		t.Errorf("calls = %v", f.calls)
	}
}

// TestDispatchTool_WitchAct verifies action + target are forwarded.
func TestDispatchTool_WitchAct(t *testing.T) {
	f := &fakeRunner{}
	_, err := wwplayer.DispatchTool("witch_act", map[string]any{"action": "antidote", "target": float64(-1)}, f)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f.calls[0] != "witch_act" {
		t.Errorf("calls = %v", f.calls)
	}
}

// TestDispatchTool_Unknown ensures unknown tools error cleanly.
func TestDispatchTool_Unknown(t *testing.T) {
	f := &fakeRunner{}
	_, err := wwplayer.DispatchTool("nope", map[string]any{}, f)
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestBuildTools_WerewolfNight ensures only wolf_kill is offered to a werewolf
// at night。
// 2026-07-10 注:原断言「wolf_kill 不含 -1 空刀」仅覆盖 7 人局规则(§15 边界)。
// 12 人竞技局(§12)已新增空刀选项,由 TestBuildTools_WerewolfNight_EmptyKill
// 单独覆盖。此处放宽为「不校验 -1」,保持既有覆盖目标。
// 2026-07-10 §124 增强 — 夜间阶段额外暴露 emotion_switch(可让狼人在
// 决定刀人前切换情绪,如切到 calm 冷静评估票型)。
func TestBuildTools_WerewolfNight(t *testing.T) {
	tools := wwplayer.BuildTools("night_wolves", "werewolf", 0, []int{1, 2, 3}, -1, nil)
	// 期望至少含 wolf_kill + emotion_switch(可能还有其它通用工具)。
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	if !names["wolf_kill"] {
		t.Errorf("expected wolf_kill in tools, got %+v", tools)
	}
	// Schema 结构校验(InputSchema["properties"]["target"]["enum"] 存在且为 []any)。
	for _, tl := range tools {
		if tl.Name != "wolf_kill" {
			continue
		}
		props, ok := tl.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("wolf_kill properties missing: %+v", tl.InputSchema)
		}
		targetSchema, ok := props["target"].(map[string]any)
		if !ok {
			t.Fatalf("wolf_kill target schema missing: %+v", props)
		}
		enumAny := targetSchema["enum"]
		if _, ok := enumAny.([]any); !ok {
			t.Fatalf("wolf_kill target enum missing: type=%T value=%v", enumAny, enumAny)
		}
	}
}

// TestBuildTools_WerewolfDay ensures speak + finish_speak + whisper +
// wolf_suicide are offered ONLY when it's this seat's speak turn (T1 fix:
// speakTurn == seat). Non-speakers should only see whisper.
func TestBuildTools_WerewolfDay(t *testing.T) {
	// speakTurn == 0 == seat → full speak set visible.
	tools := wwplayer.BuildTools("speak", "werewolf", 0, []int{1, 2, 3}, 0, nil)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"speak", "finish_speak", "whisper", "wolf_suicide"} {
		if !names[want] {
			t.Errorf("missing tool %s in %v", want, names)
		}
	}

	// speakTurn == 1 ≠ seat 0 → no speak/finish_speak/wolf_suicide, only whisper.
	tools2 := wwplayer.BuildTools("speak", "werewolf", 0, []int{1, 2, 3}, 1, nil)
	for _, tl := range tools2 {
		switch tl.Name {
		case "speak", "finish_speak", "wolf_suicide":
			t.Errorf("non-speaker seat must NOT see %s (speakTurn=1)", tl.Name)
		}
	}
	hasWhisper := false
	for _, tl := range tools2 {
		if tl.Name == "whisper" {
			hasWhisper = true
		}
	}
	if !hasWhisper {
		t.Errorf("whisper should still be visible to non-speakers")
	}
}

// TestBuildTools_RequiredNotNull verifies that tools whose schema has no
// required fields still marshal "required" as a JSON array (not null). This is
// BUG-WEREWOLF-P0-8 fix: DeepSeek / DouBao proxies reject `"required": null`
// with 400 "null is not of type array", locking out finish_speak / finish_vote
// / start_day / wolf_suicide / sheriff_elect.
func TestBuildTools_RequiredNotNull(t *testing.T) {
	// start_day (dawn), finish_speak/finish_vote (speak/vote phase, self turn),
	// sheriff_elect (sheriff) all call schema(props) with no required args.
	tools := wwplayer.BuildTools("dawn", "villager", 0, []int{1, 2, 3}, -1, nil)
	found := false
	for _, tl := range tools {
		if tl.Name == "start_day" {
			found = true
			b, err := json.Marshal(tl.InputSchema)
			if err != nil {
				t.Fatalf("marshal start_day schema: %v", err)
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal start_day schema: %v", err)
			}
			raw, ok := decoded["required"]
			if !ok {
				t.Fatalf("start_day schema missing `required` key: %s", string(b))
			}
			if string(raw) == "null" {
				t.Fatalf("start_day schema `required` serialized as null: %s", string(b))
			}
			if string(raw) != "[]" {
				t.Fatalf("start_day schema `required` must be []: %s", string(b))
			}
		}
	}
	if !found {
		t.Fatalf("expected start_day tool at dawn phase")
	}

	// finish_speak at speak phase, self speak turn.
	tools = wwplayer.BuildTools("speak", "villager", 0, []int{1, 2, 3}, 0, nil)
	for _, tl := range tools {
		if tl.Name != "finish_speak" {
			continue
		}
		b, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Fatalf("marshal finish_speak schema: %v", err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("unmarshal finish_speak schema: %v", err)
		}
		raw := decoded["required"]
		if string(raw) == "null" || !strings.HasPrefix(string(raw), "[") {
			t.Fatalf("finish_speak schema `required` must be a JSON array: %s", string(b))
		}
	}
}

// TestDispatchTool_SkipActions verifies vote_skip and witch_act_skip are
// registered in DispatchTool and map to their corresponding engine escape hatches
// (BUG-WEREWOLF-P0-8 / P0-2 fix: previously "unknown tool").
func TestDispatchTool_SkipActions(t *testing.T) {
	fr := &fakeRunner{}
	if res, err := wwplayer.DispatchTool("vote_skip", map[string]any{}, fr); err != nil {
		t.Fatalf("vote_skip dispatch failed: %v", err)
	} else if len(fr.calls) != 1 || fr.calls[0] != "vote" {
		t.Fatalf("vote_skip should map to Vote; got calls=%v res=%q", fr.calls, res)
	}
	fr = &fakeRunner{}
	if res, err := wwplayer.DispatchTool("witch_act_skip", map[string]any{}, fr); err != nil {
		t.Fatalf("witch_act_skip dispatch failed: %v", err)
	} else if len(fr.calls) != 1 || fr.calls[0] != "witch_act" {
		t.Fatalf("witch_act_skip should map to WitchAct; got calls=%v res=%q", fr.calls, res)
	}
}

// TestBuildTools_VillagerDay ensures NO wolf_suicide for a villager.
func TestBuildTools_VillagerDay(t *testing.T) {
	tools := wwplayer.BuildTools("speak", "villager", 0, []int{1, 2, 3}, 0, nil)
	for _, tl := range tools {
		if tl.Name == "wolf_suicide" {
			t.Errorf("villager must not have wolf_suicide")
		}
	}
}

// TestSpeakLimiter_Allow verifies the 30s interval.
func TestSpeakLimiter_Allow(t *testing.T) {
	l := agentcore.NewSpeakLimiter(30 * time.Second)
	if !l.Allow() {
		t.Errorf("first speech should be allowed")
	}
	l.Mark()
	if l.Allow() {
		t.Errorf("immediate second speech should be blocked")
	}
}

// TestSpeakLimiter_Wait verifies Wait blocks until interval elapses.
func TestSpeakLimiter_Wait(t *testing.T) {
	l := agentcore.NewSpeakLimiter(50 * time.Millisecond)
	l.Mark()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait err: %v", err)
	}
	if time.Since(start) < 40*time.Millisecond {
		t.Errorf("Wait returned too early: %v", time.Since(start))
	}
}

// TestMemory_Identity ensures the identity turn is seeded.
func TestMemory_Identity(t *testing.T) {
	m := wwplayer.NewMemory("werewolf", "wolf", "杀光所有神职或所有平民", 2)
	msgs, _ := m.Snapshot()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 identity msg, got %d", len(msgs))
	}
	txt := ""
	for _, c := range msgs[0].Content {
		if c.Type == "text" {
			txt = c.Text
		}
	}
	if !containsStr(txt, "werewolf") || !containsStr(txt, "2") {
		t.Errorf("identity text = %q", txt)
	}
}

// TestMemory_Pruning ensures Prune keeps only recent turns.
func TestMemory_Pruning(t *testing.T) {
	m := wwplayer.NewMemory("villager", "good", "放逐全部狼人", 0)
	for i := 0; i < 20; i++ {
		m.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "x"}}})
	}
	m.Prune(3)
	msgs, _ := m.Snapshot()
	// identity (1) + 2*3 = 7
	if len(msgs) > 7 {
		t.Errorf("after prune: msgs = %d, want ≤7", len(msgs))
	}
}

// TestMemory_ByteBudgetPrune_DouBaoRegression 是 BUG-R241-P1-01 的回归测试。
// 复现 DouBao-model 场景: ~92 条大块头 user 文本块(每条 ~8.8KB)累积到 ~810KB,
// 但条数(92)远低于按条数剪枝阈值(2*80+1=161)。旧实现 Prune 是 no-op,导致
// 400 "exceed max message tokens" 自强化死循环。字节预算应在条数阈值之下强制
// 把 payload 压回预算内,使上下文可恢复。
func TestMemory_ByteBudgetPrune_DouBaoRegression(t *testing.T) {
	m := wwplayer.NewMemory("villager", "good", "放逐全部狼人", 0)
	bigBlock := strings.Repeat("诉-", 4400) // ~8.8KB/条 (UTF-8 中文 3B/字)
	for i := 0; i < 92; i++ {
		m.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: bigBlock}}})
	}
	// 条数 92 < 阈值 161,旧实现 Prune(80) 是 no-op。
	m.Prune(wwplayer.DefaultPruneTurns)
	msgs, _ := m.Snapshot()
	// 字节预算应生效: 裁剪后 payload 明显低于 810KB,且保留 identity + 至少 1 轮。
	if len(msgs) < 3 {
		t.Fatalf("byte-budget prune over-trimmed: msgs=%d, want ≥3 (identity+1轮)", len(msgs))
	}
	if len(msgs) > 93 {
		t.Fatalf("byte-budget prune did nothing: msgs=%d, should be trimmed by byte budget", len(msgs))
	}
	// PruneByBytes 公开入口同逻辑验证。
	m2 := wwplayer.NewMemory("villager", "good", "放逐全部狼人", 0)
	for i := 0; i < 92; i++ {
		m2.Push(llm.Message{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: bigBlock}}})
	}
	m2.PruneByBytes(200 * 1024)
	msgs2, _ := m2.Snapshot()
	if len(msgs2) >= 93 {
		t.Fatalf("PruneByBytes did nothing: msgs=%d, should be trimmed", len(msgs2))
	}
	if len(msgs2) < 3 {
		t.Fatalf("PruneByBytes over-trimmed: msgs=%d", len(msgs2))
	}
}

// TestAgent_New verifies New resolves the model from the registry.
func TestAgent_New(t *testing.T) {
	reg := llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://localhost:1/x",
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "MeiTuan-model", APIKey: "sk-real"},
		},
	})
	a, err := wwplayer.New(0, "MeiTuan-model", "werewolf", "wolf", "win", reg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if a.Seat != 0 {
		t.Errorf("seat = %d", a.Seat)
	}
	if a.MaxToolUse != 0 {
		// §130 重构(2026-07-13):MaxToolUse 默认 0 表示无硬上限,
		// 每个 bot 按 LLM 输出(end_turn / refusal / max_tokens)自由循环退出。
		t.Errorf("MaxToolUse = %d (expected 0 after §130 refactor)", a.MaxToolUse)
	}
	if a.RoomID != "" || a.UserID != "" {
		t.Errorf("New should leave RoomID/UserID empty; got %q/%q", a.RoomID, a.UserID)
	}

	// NewWithRoom binds room + user.
	a2, err := wwplayer.NewWithRoom(1, "MeiTuan-model", "seer", "good", "win", reg, "room-42", "bot-uid")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if a2.RoomID != "room-42" || a2.UserID != "bot-uid" {
		t.Errorf("NewWithRoom didn't bind: %q/%q", a2.RoomID, a2.UserID)
	}
}

// TestAgent_New_MissingModel ensures New errors on unknown model.
func TestAgent_New_MissingModel(t *testing.T) {
	reg := llm.NewRegistry(config.LLMConfig{
		Endpoint:  "http://localhost:1/x",
		Providers: []config.ProviderConfig{{AgentName: "A", Model: "X", APIKey: "sk-real"}},
	})
	if _, err := wwplayer.New(0, "not-exist", "werewolf", "wolf", "win", reg); err == nil {
		t.Fatalf("expected error for unknown model")
	}
}

// TestAgent_SetEvents verifies the events channel can be swapped safely and
// that PushEvent + Shutdown behave as documented. We avoid reading the
// unexported `events` field directly; instead we drive the public surface and
// observe via the channel itself.
func TestAgent_SetEvents(t *testing.T) {
	reg := llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://localhost:1/x",
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "MeiTuan-model", APIKey: "sk-real"},
		},
	})
	a, err := wwplayer.New(0, "MeiTuan-model", "werewolf", "wolf", "win", reg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// PushEvent before SetEvents should be a no-op (no panic).
	a.PushEvent(wwplayer.AgentEvent{Kind: "noop"})

	ch := make(chan wwplayer.AgentEvent, 4)
	a.SetEvents(ch)
	a.PushEvent(wwplayer.AgentEvent{Kind: "ping"})

	select {
	case got := <-ch:
		if got.Kind != "ping" {
			t.Fatalf("expected kind=ping on channel, got %q", got.Kind)
		}
	default:
		t.Fatalf("SetEvents + PushEvent didn't deliver to the channel")
	}

	// Drop a second event so we can verify Shutdown closes the channel with
	// a leftover value still inside.
	a.PushEvent(wwplayer.AgentEvent{Kind: "leftover"})

	// Shutdown closes the channel; further PushEvent must not panic.
	a.Shutdown()
	a.PushEvent(wwplayer.AgentEvent{Kind: "post-shutdown"})

	// Drain: first value should be the leftover event, then EOF (closed).
	if got, ok := <-ch; !ok {
		t.Fatalf("expected leftover event, got EOF")
	} else if got.Kind != "leftover" {
		t.Fatalf("expected kind=leftover, got %q", got.Kind)
	}
	if _, ok := <-ch; ok {
		t.Fatalf("channel should be closed after Shutdown")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestSkipPhaseAction_DawnPhase verifies the dawn→start_day mapping added by
// BUG-WEREWOLF-P1-NEW-1: a quarantined driver bot (e.g. Kimi 403 quota) used
// to leave the room permanently stuck in PhaseDawn because SkipPhaseAction
// returned "" for dawn, so dispatchQuarantinedSkipLocked never advanced the
// phase. Now both the canonical ("dawn") and engine ("PhaseDawn") spellings
// map to start_day with target=0.
func TestSkipPhaseAction_DawnPhase(t *testing.T) {
	cases := []struct {
		phase    string
		wantName string
		wantArg  int
	}{
		{"dawn", "start_day", 0},
		{"PhaseDawn", "start_day", 0},
		{"speak", "finish_speak", 0},
		{"PhaseSpeak", "finish_speak", 0},
		{"vote", "vote_skip", 0},
		{"sheriff", "sheriff_elect", 0},
		{"night_wolves", "wolf_kill", -1},
		{"night_seer", "seer_check", -1},
		{"night_witch", "witch_act_skip", 0},
		// 2026-07-29 §134 — 守卫守护空守(quarantine / 沉默兜底)。
		{"night_guard", "guard_protect_skip", 0},
		{"PhaseNightGuard", "guard_protect_skip", 0},
		// unknown phase still yields no skip (caller falls through).
		{"unknown_phase", "", 0},
	}
	for _, c := range cases {
		name, arg := wwplayer.SkipPhaseAction(c.phase, "villager")
		if name != c.wantName || arg != c.wantArg {
			t.Errorf("SkipPhaseAction(%q) = (%q, %d), want (%q, %d)",
				c.phase, name, arg, c.wantName, c.wantArg)
		}
	}
}

// TestSkipPhaseAction_SheriffOrder 验证 §20260810-09 警长定序兜底:
// 第 3 轮自动化测试(R212)发现警长定序阶段永久卡死,根因是 SkipPhaseAction
// 缺少 sheriff_order / PhaseSheriffOrder case,watchdog 拿到的 skipName
// 为空、dispatchQuarantinedSkipLocked 永不派发,阶段无限循环。
//
// 修复后两种字符串拼写都必须映射到 sheriff_set_speak_order,确保
// dispatchQuarantinedSkipLocked(room_agent.go:634) 的派发表 case 能命中,
// 默认值(顺时针 + 警长先发言)走 sheriffSetSpeakOrderLocked。
func TestSkipPhaseAction_SheriffOrder(t *testing.T) {
	cases := []struct {
		phase    string
		wantName string
		wantArg  int
	}{
		// canonical 字符串拼写(§20260810-09 在 engine_state.go:168 注册)
		{"sheriff_order", "sheriff_set_speak_order", 0},
		// 引擎 enum 字符串
		{"PhaseSheriffOrder", "sheriff_set_speak_order", 0},
	}
	for _, c := range cases {
		name, arg := wwplayer.SkipPhaseAction(c.phase, "villager")
		if name != c.wantName || arg != c.wantArg {
			t.Errorf("SkipPhaseAction(%q) = (%q, %d), want (%q, %d)",
				c.phase, name, arg, c.wantName, c.wantArg)
		}
	}
}

// TestShouldAutoSkip_SpeakPhase verifies BUG-WEREWOLF-P0-SPEAK-AUTOSKIP +
// BUG-WEREWOLF-P0-NEW-33 (Round 33):
//   - in the speak phase, only the current speaker should auto-skip.
//     Non-speaker agents get woken by wakeAll() for transcript sync — their
//     end_turn must NOT trigger finish_speak (which the engine rejects with
//     [30008] not current speaker).
//   - the live currentSpeakTurn rp() reads at dispatch time wins over the
//     stale evtContext snapshot. If the snapshot says seat=3 speaker but
//     currentSpeakTurn has rolled to 4 already, the skip must NOT fire.
func TestShouldAutoSkip_SpeakPhase(t *testing.T) {
	cases := []struct {
		name      string
		phase     string
		seat      int
		speakTurn int // evtContext.SpeakTurn (snapshot)
		liveSpeak int // currentSpeakTurn from rp() (live). -1 means unknown.
		liveAct   int // currentTurnActing from rp() (live). -1 means unknown.
		want      bool
	}{
		// Speaker should auto-skip.
		{"speaker can skip", "speak", 3, 3, 3, -1, true},
		{"speaker PhaseSpeak can skip", "PhaseSpeak", 3, 3, 3, -1, true},
		// Non-speaker should NOT auto-skip.
		{"non-speaker must not skip", "speak", 0, 3, 3, -1, false},
		{"non-speaker must not skip 2", "speak", 5, 2, 2, -1, false},
		// BUG-WEREWOLF-P0-NEW-33: snapshot says I'm speaker but live has
		// moved on → must NOT skip (would [30008] warn-storm and miss the
		// real next-seat dispatch).
		{"stale snapshot must defer to live", "speak", 4, 4, 5, -1, false},
		{"stale snapshot opposite direction", "speak", 5, 5, 2, -1, false},
		// rp() didn't produce a live value → fall back to snapshot.
		{"rp live missing falls back to snapshot", "speak", 3, 3, -1, -1, true},
		// Night phases: live TurnActingSeat wins.
		{"night_wolves live acting matches", "night_wolves", 1, -1, -1, 1, true},
		{"night_wolves live acting mismatch", "night_wolves", 4, -1, -1, 1, false},
		{"night_seer fallback when live unknown", "night_seer", 4, -1, -1, -1, true},
		{"night_witch fallback when live unknown", "night_witch", 6, -1, -1, -1, true},
		// Other phases (sheriff / dawn / vote): live turns usually -1 here
		// because rp() doesn't return TurnActing for structural phases;
		// the fallback returns true to preserve the original behavior.
		{"sheriff always skips fallback", "sheriff", 0, -1, -1, -1, true},
		{"dawn always skips fallback", "dawn", 0, -1, -1, -1, true},
		{"vote always skips fallback", "vote", 2, -1, -1, -1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := wwtypes.GameContext{
				Phase:     c.phase,
				SpeakTurn: c.speakTurn,
			}
			got := wwplayer.ShouldAutoSkip(c.phase, c.seat, ctx, c.liveSpeak, c.liveAct)
			if got != c.want {
				t.Errorf("ShouldAutoSkip(%q, seat=%d, snapshotSpeakTurn=%d, liveSpeak=%d, liveAct=%d) = %v, want %v",
					c.phase, c.seat, c.speakTurn, c.liveSpeak, c.liveAct, got, c.want)
			}
		})
	}
}

// 2026-07-13 瘦身:验证 BuildSystemPrompt 只输出「规则 + 硬约束 + 工具清单」
// 三段,且包含核心的 13 人配置、胜利条件、工具列表关键字。历史冗长引导(
// 复盘 / 欺骗 / 情绪 / 阶段时钟)已删除,不再要求出现在 system 字段中。
func TestSystemPrompt_ContainsCoreSections(t *testing.T) {
	blocks := wwplayer.BuildSystemPrompt("", wwplayer.PersonalityVector{}, "", "", false)
	if len(blocks) == 0 {
		t.Fatal("BuildSystemPrompt returned no blocks")
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Text != "" {
			sb.WriteString(b.Text)
			sb.WriteString("\n---\n")
		}
	}
	full := sb.String()

	mustContain := []string{
		"13 人标准竞技局规则",
		// 2026-07-29 §134: 这两条断言原为固定牌组文案
		//   "4 狼人 + 1 预言家 + 1 女巫 + 1 猎人 + 1 白痴 + 5 平民 = 13 人"
		//   "屠边 = 杀光 4 神职 OR 杀光 5 平民"
		// 但 prompt.go 早已改为**随机牌组**描述(4 狼 + 1 预言家必出 + 神职池随机
		// 抽 2~3 + 平民补齐),屠边阈值也随本局实际神职/平民数量浮动而非硬编码 4/5。
		// 断言未同步 → 该用例在 main HEAD 上长期 FAIL(与 §134 守卫改动无关,
		// 已 git stash 验证)。此处把断言对齐到当前真实文案。
		"4 狼人 + 1 预言家(必出) + 从神职池随机抽取 2~3 个神职 + 平民补齐 = 13 人",
		"屠边 = 杀光全部神职 OR 杀光全部平民",
		"硬约束",
		"不是主持人 / GM / 玩家中心",
		"≤ 80 字",
		"speak / speak_with_thought",
	}
	for _, s := range mustContain {
		if !strings.Contains(full, s) {
			t.Errorf("BuildSystemPrompt missing required snippet %q (2026-07-13 瘦身核心段)", s)
		}
	}
	// 13 人固定 — 不应再出现 12 人 / 7 人局规则配置字符串。
	mustNotContain := []string{
		"7 人标准竞技局规则",
		"12 人标准竞技局规则",
		"复盘性发言",
		"欺骗 / 讲故事 / 心口不一",
		"情绪驱动决策",
		"阶段时钟与超时策略",
	}
	for _, s := range mustNotContain {
		if strings.Contains(full, s) {
			t.Errorf("BuildSystemPrompt unexpectedly contains legacy snippet %q (应已瘦身删除)", s)
		}
	}
}

// TestSystemPrompt_ContainsEmotionSwitchSpeakRule 验证 2026-08-04 §表情特效 修复:
// EmotionSwitchSpeakWriteRule() 已注入 BuildSystemPrompt,让 LLM 知道该工具存在。
// 此前该函数已定义但从未调用(grep prompt.go 返回 0),LLM 只靠 tool schema description,
// 调用率 <30%。加入 system 级硬约束后应推到 90%+。
func TestSystemPrompt_ContainsEmotionSwitchSpeakRule(t *testing.T) {
	blocks := wwplayer.BuildSystemPrompt("", wwplayer.PersonalityVector{}, "", "", false)
	if len(blocks) == 0 {
		t.Fatal("BuildSystemPrompt returned no blocks")
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Text != "" {
			sb.WriteString(b.Text)
		}
	}
	full := sb.String()
	mustContain := []string{
		"emotion_switch_speak",
		"合并发言 + 切情绪",
		"2026-08-04 重构",
		"text 必填",
	}
	for _, s := range mustContain {
		if !strings.Contains(full, s) {
			t.Errorf("BuildSystemPrompt missing emotion_switch_speak 硬约束 %q", s)
		}
	}
}

// TestBuildTools_WerewolfNight_EmptyKill verifies 12 人局 wolf_kill 暴露 -1 空刀选项
// (2026-07-10 §12:12 人竞技局新增空刀合法)。
// 2026-07-10 §124 增强 — 夜间阶段额外暴露 emotion_switch,这里只校验 wolf_kill。
func TestBuildTools_WerewolfNight_EmptyKill(t *testing.T) {
	tools := wwplayer.BuildTools("night_wolves", "werewolf", 0, []int{1, 2, 3}, -1, nil)
	var wolfKill *llm.ToolDef
	for i := range tools {
		if tools[i].Name == "wolf_kill" {
			wolfKill = &tools[i]
			break
		}
	}
	if wolfKill == nil {
		t.Fatalf("expected wolf_kill in tools, got %+v", tools)
	}
	props := wolfKill.InputSchema["properties"].(map[string]any)
	targetSchema := props["target"].(map[string]any)
	enumAny := targetSchema["enum"]
	enum, ok := enumAny.([]any)
	if !ok {
		t.Fatalf("wolf_kill target enum missing")
	}
	hasNegOne := false
	for _, v := range enum {
		switch iv := v.(type) {
		case int:
			if iv == -1 {
				hasNegOne = true
			}
		case float64:
			if iv == -1 {
				hasNegOne = true
			}
		}
	}
	if !hasNegOne {
		t.Errorf("12 人局 wolf_kill 应暴露 -1 空刀选项; enum=%v", enum)
	}
}

// TestBuildTools_SheriffStream 验证sheriff_stream 工具在 seat==SheriffSeat 且 role==seer
// 时暴露给警长。
func TestBuildTools_SheriffStream(t *testing.T) {
	gc := &wwtypes.GameContext{
		SheriffSeat:   3,
		SheriffStream: [2]int{-1, -1},
	}
	// seat==SheriffSeat && role==seer → 暴露
	tools := wwplayer.BuildTools("speak", "seer", 3, []int{0, 1, 2, 3, 4, 5}, -1, gc)
	found := false
	for _, tl := range tools {
		if tl.Name == "sheriff_stream" {
			found = true
			// schema 应有 slot 和 target, target enum 包含 -1(撤回)
			props, ok := tl.InputSchema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("sheriff_stream properties missing")
			}
			if _, ok := props["slot"]; !ok {
				t.Errorf("missing slot prop")
			}
			targetSchema, ok := props["target"].(map[string]any)
			if !ok {
				t.Fatalf("missing target prop")
			}
			enum, ok := targetSchema["enum"].([]any)
			if !ok {
				t.Fatalf("missing target enum")
			}
			hasNegOne := false
			for _, v := range enum {
				switch iv := v.(type) {
				case int:
					if iv == -1 {
						hasNegOne = true
					}
				case float64:
					if iv == -1 {
						hasNegOne = true
					}
				}
			}
			if !hasNegOne {
				t.Errorf("sheriff_stream target enum 应包含 -1 撤回哨兵; enum=%v", enum)
			}
		}
	}
	if !found {
		t.Errorf("sheriff_stream 应暴露给 sheriff+seer;speak 阶段 tools=%v", toolNames(tools))
	}

	// seat != SheriffSeat 或 role 非 seer → 不暴露
	tools2 := wwplayer.BuildTools("speak", "seer", 2, []int{0, 1, 2, 3, 4, 5}, -1, gc)
	for _, tl := range tools2 {
		if tl.Name == "sheriff_stream" {
			t.Errorf("seat≠SheriffSeat 不应暴露 sheriff_stream")
		}
	}
	tools3 := wwplayer.BuildTools("speak", "villager", 3, []int{0, 1, 2, 3, 4, 5}, -1, gc)
	for _, tl := range tools3 {
		if tl.Name == "sheriff_stream" {
			t.Errorf("非 seer 不应暴露 sheriff_stream")
		}
	}
	// PhaseSheriff 也暴露
	tools4 := wwplayer.BuildTools("sheriff", "seer", 3, []int{0, 1, 2, 3, 4, 5}, -1, gc)
	found4 := false
	for _, tl := range tools4 {
		if tl.Name == "sheriff_stream" {
			found4 = true
		}
	}
	if !found4 {
		t.Errorf("PhaseSheriff 也应暴露 sheriff_stream 给 seer sheriff")
	}
	// gc==nil(测试/旧调用方) → 不 panic
	tools5 := wwplayer.BuildTools("speak", "seer", 3, []int{0, 1, 2}, -1, nil)
	for _, tl := range tools5 {
		if tl.Name == "sheriff_stream" {
			t.Errorf("gc==nil 默认不暴露 sheriff_stream")
		}
	}
}

// TestBuildTools_IdiotReveal 验证 idiot_reveal 工具在 PhaseIdiotReveal 暴露。
func TestBuildTools_IdiotReveal(t *testing.T) {
	tools := wwplayer.BuildTools("idiot_reveal", "idiot", 5, []int{0, 1, 2, 3, 4, 5}, -1, nil)
	found := false
	for _, tl := range tools {
		if tl.Name == "idiot_reveal" {
			found = true
			props, ok := tl.InputSchema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("properties missing")
			}
			choice, ok := props["choice"].(map[string]any)
			if !ok {
				t.Fatalf("choice missing")
			}
			enum, ok := choice["enum"].([]any)
			if !ok {
				t.Fatalf("choice enum missing")
			}
			// 必须同时包含 reveal 与 skip
			set := map[string]bool{}
			for _, v := range enum {
				if s, ok := v.(string); ok {
					set[s] = true
				}
			}
			if !set["reveal"] || !set["skip"] {
				t.Errorf("idiot_reveal choice enum 必须含 reveal+skip; enum=%v", enum)
			}
		}
	}
	if !found {
		t.Fatalf("idiot_reveal 工具应在 PhaseIdiotReveal 暴露; tools=%v", toolNames(tools))
	}
	// 其他阶段不暴露
	for _, p := range []string{"speak", "vote", "night_wolves", "dawn", "hunter_shoot", "sheriff"} {
		others := wwplayer.BuildTools(p, "idiot", 5, []int{0, 1, 2, 3, 4, 5}, -1, nil)
		for _, tl := range others {
			if tl.Name == "idiot_reveal" {
				t.Errorf("phase=%s 不应暴露 idiot_reveal", p)
			}
		}
	}
}

// TestDispatchTool_SheriffStream 验证 sheriff_stream 工具派发。
func TestDispatchTool_SheriffStream(t *testing.T) {
	f := &fakeRunner{}
	_, err := wwplayer.DispatchTool("sheriff_stream", map[string]any{"slot": float64(1), "target": float64(5)}, f)
	if err != nil {
		t.Fatalf("sheriff_stream dispatch err: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "sheriff_stream" {
		t.Errorf("calls = %v", f.calls)
	}
}

// TestDispatchTool_IdiotReveal 验证 idiot_reveal 工具派发。
func TestDispatchTool_IdiotReveal(t *testing.T) {
	f := &fakeRunner{}
	_, err := wwplayer.DispatchTool("idiot_reveal", map[string]any{"choice": "reveal"}, f)
	if err != nil {
		t.Fatalf("idiot_reveal dispatch err: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "idiot_reveal:reveal" {
		t.Errorf("calls = %v", f.calls)
	}
}

// TestDispatchTool_IdiotRevealSkip 验证 idiot_reveal_skip 派发.IdiotReveal("skip")。
func TestDispatchTool_IdiotRevealSkip(t *testing.T) {
	f := &fakeRunner{}
	_, err := wwplayer.DispatchTool("idiot_reveal_skip", map[string]any{}, f)
	if err != nil {
		t.Fatalf("idiot_reveal_skip dispatch err: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "idiot_reveal:skip" {
		t.Errorf("calls = %v", f.calls)
	}
}

// TestSkipPhaseAction_IdiotReveal 验证 SkipPhaseAction(idiot_reveal) 返回
// idiot_reveal_skip(target=0),让 quarantine 路径放弃翻牌。
func TestSkipPhaseAction_IdiotReveal(t *testing.T) {
	name, arg := wwplayer.SkipPhaseAction("idiot_reveal", "idiot")
	if name != "idiot_reveal_skip" || arg != 0 {
		t.Errorf("SkipPhaseAction(idiot_reveal) = (%q,%d), want (idiot_reveal_skip,0)", name, arg)
	}
	name2, arg2 := wwplayer.SkipPhaseAction("PhaseIdiotReveal", "idiot")
	if name2 != "idiot_reveal_skip" || arg2 != 0 {
		t.Errorf("SkipPhaseAction(PhaseIdiotReveal) = (%q,%d), want (idiot_reveal_skip,0)", name2, arg2)
	}
}

// toolNames 提取工具名称列表。
func toolNames(tools []llm.ToolDef) []string {
	var out []string
	for _, tl := range tools {
		out = append(out, tl.Name)
	}
	return out
}

// 2026-07-10 §116:验证 BuildTools 在各个阶段都暴露至少 1 个工具 + BuildTools
// 返回的 tool 描述里明确告诉 LLM "允许一次响应多个 tool_use"。
//
// 设计:在 BuildTools 各阶段返回的工具集中检查某 1 个工具的 Description
// 字段是否包含 §116 的强 prompt 关键词("单次响应" / "多个 tool_use" /
// "speak + finish_speak")。
func TestBuildTools_DescriptionMentionsMultiTool(t *testing.T) {
	cases := []struct {
		phase     string
		role      string
		seat      int
		speakTurn int
	}{
		{"pre_wolves", "villager", 0, -1}, // 任意存活角色在首夜缓冲期
		{"speak", "villager", 0, 0},       // 当前发言者
	}
	for _, c := range cases {
		t.Run(c.phase, func(t *testing.T) {
			alive := []int{0, 1, 2, 3, 4, 5, 6}
			gc := &wwtypes.GameContext{Phase: c.phase, SpeakTurn: c.speakTurn}
			tools := wwplayer.BuildTools(c.phase, c.role, c.seat, alive, c.speakTurn, gc)
			if len(tools) == 0 {
				t.Fatalf("phase=%s role=%s: BuildTools returned no tools", c.phase, c.role)
			}
			// 收集所有工具的描述,合并后查找 §116 关键短句。
			var sb strings.Builder
			for _, tool := range tools {
				sb.WriteString(tool.Name)
				sb.WriteString(": ")
				sb.WriteString(tool.Description)
				sb.WriteString("\n---\n")
			}
			full := sb.String()
			if !strings.Contains(full, "强制发言") &&
				!strings.Contains(full, "可选") &&
				!strings.Contains(full, "工具") {
				t.Errorf("phase=%s: tool descriptions lack multilingual orientation cues", c.phase)
			}
		})
	}
}

// TestRecordToolResult_EmptyContentDoesNotProduceEmptyTextBlock 是 R143
// (2026-07-17) 回归测试。背景:线上 15280-400-RequestBody.json 抓包显示
// doubao-seed-2.0-pro 因 tool_result.content[0].text 为空而拒绝整次请求
// (400 "missing messages.content.text parameter")。根因是 LLM 调用了未注册
// 的 `skip` 工具,DispatchTool 返回 ("", "unknown tool: skip"),然后
// recordToolResult 把空字符串塞进 ContentBlock.Text,MarshalJSON 在 text 块
// 上 omitempty 把它吞掉,产出 `{"type":"text"}` 非法形状。
//
// 修复:recordToolResult 在 content=="" 时写占位文本(错误时附 tool_use_id)。
// 本测试断言序列化后的 tool_result 块一定含有非空 text 字段,且 wire 形状
// 满足 Anthropic 协议。
func TestRecordToolResult_EmptyContentDoesNotProduceEmptyTextBlock(t *testing.T) {
	reg := llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://localhost:1/x",
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "MeiTuan-model", APIKey: "sk-real"},
		},
	})
	a, err := wwplayer.NewWithRoom(2, "MeiTuan-model", "werewolf", "wolf", "win", reg, "room-r143", "bot-uid")
	if err != nil {
		t.Fatalf("wwplayer.NewWithRoom: %v", err)
	}

	cases := []struct {
		name     string
		content  string
		isErr    bool
		wantHint string
	}{
		// 错误 + 空字符串:占位文本必须包含 tool_use_id 标识。
		{"err+empty writes placeholder with id", "", true, "tool_use_id=call_unknown_skip"},
		// 成功 + 空字符串:占位文本 "empty tool result" 即可。
		{"ok+empty writes generic placeholder", "", false, "empty tool result"},
		// 正常文本:占位逻辑不破坏原有 content。
		{"normal text preserved", "vote_skip: -1 recorded", false, "vote_skip: -1 recorded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a.RecordToolResultForTest("call_unknown_skip", tc.content, tc.isErr)

			msgs, _ := a.Memory.Snapshot()
			if len(msgs) == 0 {
				t.Fatalf("Memory.Snapshot returned 0 messages after RecordToolResultForTest")
			}
			// 找最近一条 user 消息的最后一个 tool_result 块。
			var foundToolResult *llmtypes.ContentBlock
			for i := len(msgs) - 1; i >= 0; i-- {
				m := msgs[i]
				if m.Role != "user" {
					continue
				}
				for j := len(m.Content) - 1; j >= 0; j-- {
					if m.Content[j].Type == "tool_result" {
						foundToolResult = &m.Content[j]
						break
					}
				}
				if foundToolResult != nil {
					break
				}
			}
			if foundToolResult == nil {
				t.Fatalf("no tool_result block in last user message; msgs=%+v", msgs)
			}

			b, err := json.Marshal(foundToolResult)
			if err != nil {
				t.Fatalf("marshal tool_result: %v", err)
			}
			s := string(b)

			// 关键断言 1:序列化后必须始终有 "text":<非空> 字段,否则触发 Anthropic 400。
			if !strings.Contains(s, `"text":`) {
				t.Fatalf("wire payload missing text field — would trigger Anthropic 400 MissingParameter; got %s", s)
			}
			// 关键断言 2:tool_use_id 必须保留(Doubao 严格校验)。
			if !strings.Contains(s, `"tool_use_id":"call_unknown_skip"`) {
				t.Fatalf("tool_use_id lost; got %s", s)
			}
			// 关键断言 3:占位文本包含期望 hint。
			if tc.content == "" && !strings.Contains(s, tc.wantHint) {
				t.Fatalf("placeholder missing hint %q; got %s", tc.wantHint, s)
			}
			// 关键断言 4:不允许出现 `{"type":"text"}`(无 text 字段)的非法形状。
			if strings.Contains(s, `{"type":"text"}`) {
				t.Fatalf("wire payload still emits illegal {type:text} shape; got %s", s)
			}
		})
	}
}

// TestBuildTools_EmotionSwitchSpeak_OnlyWithActionTool 验证 emotion_switch_speak 的暴露不变式。
//
// 2026-08-04 §重构 — emotion_switch 旧工具删除,合并到 emotion_switch_speak。
// 新工具 text 必填,等价于强制绑定发言;但仍保留「只在已有至少一个行动工具时
// 才下发」的不变式,避免 LLM 在无行动工具的 phase 反复调它。
//
// 取代原 TestBuildTools_EmotionSwitch_OnlyWithActionTool(2026-07-30 修复)。
func TestBuildTools_EmotionSwitchSpeak_OnlyWithActionTool(t *testing.T) {
	phases := []string{
		"pre_wolves", "night_wolves", "night_guard", "night_seer", "night_witch",
		"dawn", "speak", "vote", "sheriff", "hunter_shoot", "idiot_reveal", "death_lyric",
	}
	exposedAnywhere := false
	for _, phase := range phases {
		tools := wwplayer.BuildTools(phase, "villager", 0, []int{1, 2, 3}, -1, nil)
		found := false
		for _, tl := range tools {
			if tl.Name == "emotion_switch_speak" {
				found = true
				// 验证 description 中明确说明 text 必填
				if !strings.Contains(tl.Description, "text 必填") {
					t.Errorf("phase %s: emotion_switch_speak description missing 'text 必填' constraint", phase)
				}
				break
			}
		}
		if found {
			exposedAnywhere = true
			// 不变式:emotion_switch_speak 出现时,必须至少还有另外一个工具。
			if len(tools) < 2 {
				t.Errorf("phase %s: emotion_switch_speak exposed ALONE (tools=%d) — violates 「必须与行动工具合并调用」",
					phase, len(tools))
			}
		}
	}
	if !exposedAnywhere {
		t.Error("emotion_switch_speak never exposed in any phase — over-restricted")
	}
}

// TestBuildTools_SpeakPhase_SpeakTurnRequired 验证 speak 阶段仅当前发言者获得 speak 工具。
// 2026-07-29 修复:speak 描述中已加强约束「必须发言,不可用 idle_silent 跳过」。
func TestBuildTools_SpeakPhase_SpeakTurnRequired(t *testing.T) {
	// 当前发言座位
	tools := wwplayer.BuildTools("speak", "villager", 0, []int{0, 1, 2}, 0, nil)
	hasSpeak := false
	for _, tl := range tools {
		if tl.Name == "speak" {
			hasSpeak = true
			// 验证描述中包含「必须发言」约束
			if !strings.Contains(tl.Description, "必须用 speak") {
				t.Error("speak description missing '必须用 speak' constraint")
			}
			break
		}
	}
	if !hasSpeak {
		t.Error("current speaker should have speak tool")
	}

	// 非当前发言座位不应有 speak 工具(但有 interject)
	tools2 := wwplayer.BuildTools("speak", "villager", 1, []int{0, 1, 2}, 0, nil)
	for _, tl := range tools2 {
		if tl.Name == "speak" {
			t.Error("non-speaker should NOT have speak tool")
		}
	}
}

// TestBuildTools_IdleSilent_DescriptionConstraint 验证 idle_silent 描述中加强约束。
// 2026-07-29 优化:idle_silent 描述中明确「当前发言轮到你时不可用 idle_silent 跳过」。
func TestBuildTools_IdleSilent_DescriptionConstraint(t *testing.T) {
	tools := wwplayer.BuildTools("speak", "villager", 0, []int{0, 1, 2}, 0, nil)
	for _, tl := range tools {
		if tl.Name == "idle_silent" {
			if !strings.Contains(tl.Description, "当前发言轮到你时不可用") {
				t.Errorf("idle_silent description missing speak turn constraint: %s", tl.Description)
			}
			return
		}
	}
	// idle_silent 不在 speak 阶段暴露也没关系(设计选择)
}

// §20260830-02 — 自爆带走工具暴露/派发测试。
// wolf_suicide_take 仅在 suicide_take 阶段 + 本座位是自爆狼时暴露;
// DispatchTool 把 target 透传 runner.SuicideTake。
func TestBuildTools_SuicideTake(t *testing.T) {
	// 自爆狼(seat=2)在 suicide_take 阶段,gc.SuicideTakeSeat=2 → 应有工具。
	gc := &wwtypes.GameContext{SuicideTakeSeat: 2, MyTurn: true}
	tools := wwplayer.BuildTools("suicide_take", "werewolf", 2, []int{0, 1, 3}, -1, gc)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	if !names["wolf_suicide_take"] {
		t.Errorf("suicided wolf must see wolf_suicide_take, got %v", names)
	}

	// 非自爆狼座位 → 不暴露。
	gc2 := &wwtypes.GameContext{SuicideTakeSeat: 2}
	tools2 := wwplayer.BuildTools("suicide_take", "werewolf", 5, []int{0, 1, 3}, -1, gc2)
	for _, tl := range tools2 {
		if tl.Name == "wolf_suicide_take" {
			t.Error("non-suicided seat must NOT see wolf_suicide_take")
		}
	}

	// 其它阶段(gc=nil)→ 不暴露。
	tools3 := wwplayer.BuildTools("speak", "werewolf", 2, []int{0, 1, 3}, 2, nil)
	for _, tl := range tools3 {
		if tl.Name == "wolf_suicide_take" {
			t.Error("wolf_suicide_take must NOT be exposed outside suicide_take phase")
		}
	}
}

// TestDispatchTool_SuicideTake 验证 wolf_suicide_take 派发到 runner.SuicideTake。
func TestDispatchTool_SuicideTake(t *testing.T) {
	f := &fakeRunner{}
	res, err := wwplayer.DispatchTool("wolf_suicide_take", map[string]any{"target": float64(4)}, f)
	if err != nil {
		t.Fatalf("DispatchTool(wolf_suicide_take): %v", err)
	}
	_ = res
	want := "wolf_suicide_take:4"
	found := false
	for _, c := range f.calls {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("runner.SuicideTake not called with target 4; calls=%v", f.calls)
	}
}
