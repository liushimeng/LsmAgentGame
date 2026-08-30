// emotion_switch_speak_tools_test.go — 2026-08-04 §重构 — 合并工具 emotion_switch_speak
// 的 BuildTools + DispatchTool 不变式测试。
//
// 背景(抓包 + 227MB 运行日志证据):
//   - 一次 LLM 响应里出现 10 个连续 emotion_switch tool_use,reason 字段写着
//     「调 speak 补发言下限」「够了,调 speak」—— 但该请求体里**只有**
//     emotion_switch 一个工具,根本没有 speak 可调。
//   - 日志:588 次 "emotion_switch called alone" 拒绝 / 462 次 "alone count
//     exceeded 3"。
//
// 2026-08-04 重构:emotion_switch 独立工具删除,合并到 emotion_switch_speak(text 必填 +
// emotion 可省略 + reason 可省略)。本测试验证:
//   T1: emotion_switch_speak 的 schema 字段(text 必填 / emotion 可省略 / 无 random)
//   T2: emotion_switch 旧名在任何 (phase, role, speakTurn) 组合下都**不**暴露
//   T3: emotion_switch_speak 在所有活跃 phase 都暴露(前提是该组合有至少一个行动工具)
//   T4: emotion_switch_speak 与 speak / speak_with_thought 不能同响应存在
//       (在 dispatcher 层由 run.go 聚合,不在 BuildTools 层处理;BuildTools 仍都暴露)
//   T5: emotion_switch_speak schema 的 emotion enum 严格为 10 个 key(无 random)
//
// 老的 emotion_switch 三次重试 / 单独调用检测逻辑已删除(见 run.go),不再有
// emotionSwitchAloneCount 字段。本文件不保留旧测试断言。
package wwplayer_test

import (
	"testing"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/config"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
)

// esToolNames2 把 BuildTools 结果压成名字切片,方便断言。
func esToolNames2(tools []llm.ToolDef) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func esContains2(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// ─── T1 — BuildTools 不暴露 emotion_switch 旧名 ───────────────────────────

// TestEmotionSwitchSpeak_T1_LegacyNotExposed 验证 §重构:emotion_switch 旧名
// 在 BuildTools 输出中**完全消失**,所有 (phase, role) 组合都不再下发。
func TestEmotionSwitchSpeak_T1_LegacyNotExposed(t *testing.T) {
	phases := []string{
		"pre_wolves", "PhasePreWolves",
		"night_wolves", "PhaseNightWolves",
		"night_guard", "PhaseNightGuard",
		"night_seer", "PhaseNightSeer",
		"night_witch", "PhaseNightWitch",
		"night_demon_hunter", "PhaseNightDemonHunter",
		"dawn", "PhaseDawn",
		"speak", "PhaseSpeak",
		"vote", "PhaseVote",
		"sheriff", "PhaseSheriff",
		"hunter_shoot", "PhaseHunterShoot",
		"idiot_reveal", "PhaseIdiotReveal",
		"death_lyric", "PhaseDeathLyric",
		"restart_vote", "gameover", "filling",
	}
	roles := []string{
		"villager", "werewolf", "seer", "witch", "guard",
		"hunter", "knight", "demon_hunter", "idiot",
	}
	alive := []int{0, 1, 2, 3}
	gcs := []*wwtypes.GameContext{
		nil,
		{DeathLyricCurrent: -1, GuardLastProtect: -1, SheriffSeat: -1, WolfTeammateSeats: nil, Round: 3},
		{DeathLyricCurrent: 0, GuardLastProtect: -1, SheriffSeat: 0, WolfTeammateSeats: nil, Round: 3},
	}
	for _, phase := range phases {
		for _, role := range roles {
			for _, speakTurn := range []int{-1, 0, 1} {
				for i, gc := range gcs {
					names := esToolNames2(wwplayer.BuildTools(phase, role, 0, alive, speakTurn, gc))
					if esContains2(names, "emotion_switch") {
						t.Errorf("phase=%s role=%s speakTurn=%d gc#%d: emotion_switch 旧名仍暴露; tools=%v",
							phase, role, speakTurn, i, names)
					}
				}
			}
		}
	}
}

// ─── T2 — emotion_switch_speak 在所有活跃 phase 都暴露 ──────────────────

// TestEmotionSwitchSpeak_T2_ExposedInActivePhases 验证 §重构:emotion_switch_speak
// 在所有 phase 白名单(pre_wolves / night_* / dawn / speak / vote / sheriff /
// hunter_shoot / idiot_reveal / death_lyric)中至少有一个 (role, gc, speakTurn)
// 组合暴露出来。
func TestEmotionSwitchSpeak_T2_ExposedInActivePhases(t *testing.T) {
	phases := []string{
		"pre_wolves", "PhasePreWolves",
		"night_wolves", "PhaseNightWolves",
		"night_guard", "PhaseNightGuard",
		"night_seer", "PhaseNightSeer",
		"night_witch", "PhaseNightWitch",
		"night_demon_hunter", "PhaseNightDemonHunter",
		"dawn", "PhaseDawn",
		"speak", "PhaseSpeak",
		"vote", "PhaseVote",
		"sheriff", "PhaseSheriff",
		"hunter_shoot", "PhaseHunterShoot",
		"idiot_reveal", "PhaseIdiotReveal",
		"death_lyric", "PhaseDeathLyric",
	}
	roles := []string{
		"villager", "werewolf", "seer", "witch", "guard",
		"hunter", "knight", "demon_hunter", "idiot",
	}
	alive := []int{0, 1, 2, 3}
	gcs := []*wwtypes.GameContext{
		nil,
		{DeathLyricCurrent: 0, GuardLastProtect: -1, SheriffSeat: 0, WolfTeammateSeats: nil, Round: 3},
	}
	for _, phase := range phases {
		found := false
		for _, role := range roles {
			for _, speakTurn := range []int{-1, 0, 1} {
				for _, gc := range gcs {
					names := esToolNames2(wwplayer.BuildTools(phase, role, 0, alive, speakTurn, gc))
					if esContains2(names, "emotion_switch_speak") {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("phase=%s: emotion_switch_speak 在所有 (role, gc, speakTurn) 组合下都未暴露", phase)
		}
	}
}

// ─── T3 — emotion_switch_speak schema 不允许 emotion=random ───────────────

// TestEmotionSwitchSpeak_T3_NoRandomInSchema 验证 §重构:emotion enum 已剔除 random。
// LLM 不允许再让系统随机选情绪。
func TestEmotionSwitchSpeak_T3_NoRandomInSchema(t *testing.T) {
	alive := []int{0, 1, 2}
	tools := wwplayer.BuildTools("speak", "villager", 0, alive, 0, nil)
	for _, td := range tools {
		if td.Name != "emotion_switch_speak" {
			continue
		}
		// td.InputSchema 是 map[string]any
		props, ok := td.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("emotion_switch_speak schema properties 缺失")
		}
		emotion, ok := props["emotion"].(map[string]any)
		if !ok {
			t.Fatalf("emotion_switch_speak schema 缺 emotion 字段")
		}
		enum, ok := emotion["enum"].([]string)
		if !ok {
			t.Fatalf("emotion enum 类型不是 []string")
		}
		for _, e := range enum {
			if e == "random" {
				t.Errorf("emotion_switch_speak emotion enum 不应包含 'random'; enum=%v", enum)
			}
		}
		// 期望恰好 10 个 key
		if len(enum) != 10 {
			t.Errorf("emotion_switch_speak emotion enum 期望 10 个 key,实际 %d 个; enum=%v", len(enum), enum)
		}
		// text 必填
		required, _ := td.InputSchema["required"].([]string)
		foundText := false
		for _, r := range required {
			if r == "text" {
				foundText = true
			}
		}
		if !foundText {
			t.Errorf("emotion_switch_speak schema required 必须包含 'text'; required=%v", required)
		}
		// emotion / reason 必须不在 required(可省略)
		for _, r := range required {
			if r == "emotion" || r == "reason" {
				t.Errorf("emotion_switch_speak schema %q 不应在 required(应可省略); required=%v", r, required)
			}
		}
		return
	}
	t.Errorf("speak phase villager 角色未找到 emotion_switch_speak 工具")
}

// ─── T4 — DispatchTool 派发到 EmotionSwitchSpeak ────────────────────────────

// TestEmotionSwitchSpeak_T4_DispatcherRoutes 验证 dispatcher 把 emotion_switch_speak
// 路由到 runner.EmotionSwitchSpeak(text, emotion, reason),而 emotion_switch 旧名
// 返回 "unknown tool"。
//
// 复用 agent_test.go 的 fakeRunner(已在该文件实现 EmotionSwitchSpeak)。
func TestEmotionSwitchSpeak_T4_DispatcherRoutes(t *testing.T) {
	fr := &fakeRunner{}
	input := map[string]any{
		"text":    "我是8号",
		"emotion": "confident",
		"reason":  "票型基本成形",
	}
	out, err := wwplayer.DispatchTool("emotion_switch_speak", input, fr)
	if err != nil {
		t.Fatalf("dispatch emotion_switch_speak err: %v", err)
	}
	if out == "" {
		t.Errorf("dispatch emotion_switch_speak 应有反馈")
	}
	// 旧名应被 unknown tool 拒绝
	out2, err := wwplayer.DispatchTool("emotion_switch", map[string]any{
		"emotion": "confident",
		"reason":  "test",
	}, fr)
	if err == nil {
		t.Errorf("emotion_switch 旧名应返回 error(unknown tool); out=%v", out2)
	}
}

// ─── T5 — §表情特效 schema 含 4 个新可选参数 ────────────────────────────────

// TestEmotionSwitchSpeak_T5_FxSchemaParams 验证 2026-08-04 §表情特效(§5.1):
// emotion_switch_speak schema 新增 intensity/duration_sec/effect/caption
// 4 个**可选**参数(均不在 required),enum 约束正确。
func TestEmotionSwitchSpeak_T5_FxSchemaParams(t *testing.T) {
	alive := []int{0, 1, 2}
	tools := wwplayer.BuildTools("speak", "villager", 0, alive, 0, nil)
	for _, td := range tools {
		if td.Name != "emotion_switch_speak" {
			continue
		}
		props, ok := td.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("emotion_switch_speak schema properties 缺失")
		}
		// 4 个新参数存在
		for _, k := range []string{"intensity", "duration_sec", "effect", "caption"} {
			if _, ok := props[k]; !ok {
				t.Errorf("emotion_switch_speak schema 缺 §表情特效参数 %q", k)
			}
		}
		// effect enum = 8 种
		eff, _ := props["effect"].(map[string]any)
		effEnum, _ := eff["enum"].([]string)
		wantEffects := map[string]bool{
			"pulse": true, "shake": true, "sweat": true, "rage": true,
			"tears": true, "spin_question": true, "glow": true, "drowsy": true,
		}
		if len(effEnum) != 8 {
			t.Errorf("effect enum 期望 8 种,实际 %d; enum=%v", len(effEnum), effEnum)
		}
		for _, e := range effEnum {
			if !wantEffects[e] {
				t.Errorf("effect enum 含非法值 %q", e)
			}
		}
		// intensity enum = low/mid/high
		ins, _ := props["intensity"].(map[string]any)
		insEnum, _ := ins["enum"].([]string)
		if len(insEnum) != 3 {
			t.Errorf("intensity enum 期望 3 档,实际 %v", insEnum)
		}
		// 新参数必须不在 required(可省略)
		required, _ := td.InputSchema["required"].([]string)
		for _, r := range required {
			if r == "intensity" || r == "duration_sec" || r == "effect" || r == "caption" {
				t.Errorf("§表情特效参数 %q 不应在 required(应可省略); required=%v", r, required)
			}
		}
		return
	}
	t.Errorf("speak phase villager 角色未找到 emotion_switch_speak 工具")
}

// ─── T6 — §表情特效 dispatch 归一化 ────────────────────────────────────────

// TestEmotionSwitchSpeak_T6_DispatchFxNormalize 验证 dispatcher 把新参数
// 解析为 wwplayer.EmotionFx 并经 NormalizeEmotionFx 服务端归一化:
// caption 20 rune 截断 / duration clamp [8,30] / 非法 effect 归一 pulse。
func TestEmotionSwitchSpeak_T6_DispatchFxNormalize(t *testing.T) {
	// (a) 全参数正常透传 + caption 超长截断(21 rune → 20)
	fr := &fakeRunner{}
	longCaption := "一二三四五六七八九十abcdefghijk" // 21 rune
	_, err := wwplayer.DispatchTool("emotion_switch_speak", map[string]any{
		"text":         "我是8号",
		"emotion":      "panic",
		"effect":       "sweat",
		"intensity":    "high",
		"duration_sec": float64(20),
		"caption":      longCaption,
	}, fr)
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if fr.lastFx.Effect != "sweat" || fr.lastFx.Intensity != "high" || fr.lastFx.DurationSec != 20 {
		t.Errorf("fx 透传错误: %+v", fr.lastFx)
	}
	if got := len([]rune(fr.lastFx.Caption)); got != 20 {
		t.Errorf("caption 应截断到 20 rune,实际 %d; caption=%q", got, fr.lastFx.Caption)
	}

	// (b) duration clamp 下界 8 / 上界 30
	for _, tc := range []struct {
		in   float64
		want int
	}{{3, 8}, {45, 30}, {8, 8}, {30, 30}} {
		fr2 := &fakeRunner{}
		_, err := wwplayer.DispatchTool("emotion_switch_speak", map[string]any{
			"text":         "测试",
			"duration_sec": tc.in,
		}, fr2)
		if err != nil {
			t.Fatalf("dispatch err: %v", err)
		}
		if fr2.lastFx.DurationSec != tc.want {
			t.Errorf("duration_sec=%v 应 clamp 到 %d,实际 %d", tc.in, tc.want, fr2.lastFx.DurationSec)
		}
	}

	// (c) 非法 effect 归一 pulse;非法 intensity 归一 mid;缺省 duration → 12
	fr3 := &fakeRunner{}
	_, err = wwplayer.DispatchTool("emotion_switch_speak", map[string]any{
		"text":      "测试",
		"effect":    "explode",
		"intensity": "ultra",
	}, fr3)
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if fr3.lastFx.Effect != "pulse" {
		t.Errorf("非法 effect 应归一 pulse,实际 %q", fr3.lastFx.Effect)
	}
	if fr3.lastFx.Intensity != "mid" {
		t.Errorf("非法 intensity 应归一 mid,实际 %q", fr3.lastFx.Intensity)
	}
	if fr3.lastFx.DurationSec != 12 {
		t.Errorf("缺省 duration_sec 应默认 12,实际 %d", fr3.lastFx.DurationSec)
	}

	// (d) 全部省略(向后兼容):pulse/mid/12,空 caption
	fr4 := &fakeRunner{}
	_, err = wwplayer.DispatchTool("emotion_switch_speak", map[string]any{"text": "测试"}, fr4)
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if fr4.lastFx.Effect != "pulse" || fr4.lastFx.Intensity != "mid" ||
		fr4.lastFx.DurationSec != 12 || fr4.lastFx.Caption != "" {
		t.Errorf("省略新参数时应为 pulse/mid/12/空caption,实际 %+v", fr4.lastFx)
	}
}

// ─── T7 — §表情特效 SwitchEmotionFx + speak 失败回滚 ────────────────────────

// TestEmotionSwitchSpeak_T7_SwitchEmotionFxState 验证 Agent.SwitchEmotionFx
// 与 CurrentEmotionFx 的状态读写;以及 speak 失败(不调用 SwitchEmotionFx)
// 时 fx 不生效的回滚语义(状态保持上一次)。
func TestEmotionSwitchSpeak_T7_SwitchEmotionFxState(t *testing.T) {
	reg := llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://localhost:1/x",
		Providers: []config.ProviderConfig{
			{AgentName: "Kimi", Model: "Kimi-model", APIKey: "sk-real"},
		},
	})
	a, err := wwplayer.New(3, "Kimi-model", "seer", "good", "win", reg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 初始无 fx
	_, started, dur := a.CurrentEmotionFx()
	if started != 0 || dur != 0 {
		t.Errorf("初始 fx 应为零值,实际 started=%d dur=%d", started, dur)
	}
	// 第一次切换带 fx
	a.SwitchEmotionFx("panic", "被质疑", wwplayer.EmotionFx{
		Effect: "sweat", Intensity: "high", Caption: "不是我!", DurationSec: 15,
	})
	fx, started, dur := a.CurrentEmotionFx()
	if fx.Effect != "sweat" || fx.Intensity != "high" || fx.Caption != "不是我!" || fx.DurationSec != 15 {
		t.Errorf("fx 状态错误: %+v", fx)
	}
	if started <= 0 || dur != 15000 {
		t.Errorf("fx 时间戳错误: started=%d dur=%d(want 15000ms)", started, dur)
	}
	if a.CurrentEmotion() != "panic" {
		t.Errorf("emotion 应为 panic,实际 %q", a.CurrentEmotion())
	}
	// 模拟 speak 失败路径:不调用 SwitchEmotionFx → 状态保持上一次(回滚语义)
	a.SwitchEmotionFx("calm", "", wwplayer.EmotionFx{})
	fx2, started2, dur2 := a.CurrentEmotionFx()
	if a.CurrentEmotion() != "calm" {
		t.Errorf("emotion 应为 calm,实际 %q", a.CurrentEmotion())
	}
	// 零值 fx 切换后 fx 字段清空,时间戳归 0
	if fx2.Effect != "" || fx2.Caption != "" || started2 != 0 || dur2 != 0 {
		t.Errorf("零值 fx 切换后应清空 fx 状态,实际 %+v started=%d dur=%d", fx2, started2, dur2)
	}
	// 无效 emotion no-op
	a.SwitchEmotionFx("not_an_emotion", "x", wwplayer.EmotionFx{Effect: "rage", DurationSec: 10})
	if a.CurrentEmotion() != "calm" {
		t.Errorf("无效 emotion 应 no-op,实际 %q", a.CurrentEmotion())
	}
}