// Package werewolf — prop_effect.go: 道具命中效果注册表（v2 重设计）。
//
// 命中道具后，服务端不替 LLM 决策（对齐设计 §9.3），而是在目标座位的
// GameContext 中注入"干扰信号"。BuildUserPrompt 读取这些字段追加一段
// 文案到 user prompt，让 LLM 看到"直觉引导 / 情绪 / 注意力上限"等信息，
// 由 LLM 自主决定如何响应。
//
// EffectRegistry 是效果落地函数的注册表（key = effect_type）。
// 新效果接入：RegisterEffect(key, fn) 即可。
//
// 2026-07-21 道具系统 v2 重设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §3）。
package werewolf

import (
	"LsmWebGame/agent/wwtypes"
)

// EffectApplyContext 是效果落地时传入的上下文（引擎侧持有，便于按规则计算引导座位）。
type EffectApplyContext struct {
	Room     *WerewolfRoom
	Entry    PropInjectEntry
	FromSeat int
}

// EffectApplier 是效果落地函数类型（把干扰信号写入目标 GameContext）。
// gc 为目标座位的 GameContext；Seat 为目标座位。
type EffectApplier func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext)

// EffectRegistry 是效果落地函数的注册表（key = effect_type）。
var EffectRegistry = map[string]EffectApplier{}

// RegisterEffect 注册一个效果落地函数。重复注册覆盖（便于测试替换）。
func RegisterEffect(key string, fn EffectApplier) {
	EffectRegistry[key] = fn
}

// ApplyEffects 对目标 GameContext 应用 entry 的所有效果（遍历 entry.EffectTypes，逗号分隔）。
// 在 buildAgentContextLocked 消费注入队列时调用。
func ApplyEffects(gc *wwtypes.GameContext, seat int, entry PropInjectEntry, ctx EffectApplyContext) {
	for _, et := range entry.ParseEffectTypes() {
		if fn, ok := EffectRegistry[et]; ok {
			fn(gc, seat, ctx)
		}
	}
}

// init 注册 5 种默认效果落地函数。
func init() {
	// expose_identity：在 internal_thought 暴露信号。
	RegisterEffect("expose_identity", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		gc.EffectExpose = true
	})

	// attention_scatter：降低下轮 tool_use 上限到 2（强制简化决策）。
	RegisterEffect("attention_scatter", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		gc.EffectAttentionScatter = true
		gc.ToolUseMaxOverride = 2
	})

	// target_twist：为目标选择类工具注入"直觉引导"。
	// 引导座位由 ctx.Entry.TwistSeat 决定（PropEngine 在 enqueue 前按 TwistSeatSrc 计算）。
	RegisterEffect("target_twist", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		if ctx.Entry.TwistSeat >= 0 {
			gc.EffectTargetTwistSeat = ctx.Entry.TwistSeat
		}
	})

	// confuse_seer：专用于预言家的查验直觉引导（复用 target_twist 语义）。
	RegisterEffect("confuse_seer", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		if ctx.Entry.TwistSeat >= 0 {
			gc.EffectTargetTwistSeat = ctx.Entry.TwistSeat
		}
	})

	// emotion_disturb：强制切换为情绪不稳（下轮 prompt 含"你感到困惑/心虚"）。
	RegisterEffect("emotion_disturb", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		// guilty/confused 交替，由 from_seat 决定以保持确定性。
		if ctx.FromSeat%2 == 0 {
			gc.EffectForceEmotion = "confused"
		} else {
			gc.EffectForceEmotion = "guilty"
		}
	})

	// 2026-07-21 §G1 v3 新增 — emotion_disturb_light：轻量情绪扰动。
	// 与 emotion_disturb 不同,本效果不强制 confused/guilty,而是引导"角色代入"式
	// 情绪(engaged),与 v3 任务马甲注入"剧本创作"的语义匹配,降低 LLM 安全防御
	// 姿态。效果落地由 PropEffectSignalBlock 渲染为"🎭 你感到与角色产生深度共鸣"
	// 等温和提示,既传达中招事实又保持 Agent 自主权(对齐 §9.3)。
	RegisterEffect("emotion_disturb_light", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		gc.EffectForceEmotion = "engaged"
	})

	// 2026-08-07 §20260807-04 P0-3 — 人类反制道具效果(Agent → 真人玩家)。
	// 这三个效果不落地到目标 GameContext(目标是人类,无 LLM),而是写入
	// **使用者 Agent** 的 gc.HumanDebuff,再由 buildAgentContextLocked 同步到
	// 目标人类座位的 Player.HumanDebuff,供客户端视图渲染。
	RegisterEffect("human_announce_prefix", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		gc.HumanDebuff = &wwtypes.HumanDebuffSpec{
			Type:       "human_announce_prefix",
			Duration:   1,
			PropNameZh: PropKeyToName(ctx.Entry.PropKey),
		}
	})
	RegisterEffect("human_vote_suggest", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		gc.HumanDebuff = &wwtypes.HumanDebuffSpec{
			Type:        "human_vote_suggest",
			SuggestSeat: ctx.Entry.TwistSeat,
			Duration:    1,
			PropNameZh:  PropKeyToName(ctx.Entry.PropKey),
		}
	})
	RegisterEffect("human_char_garble", func(gc *wwtypes.GameContext, seat int, ctx EffectApplyContext) {
		gc.HumanDebuff = &wwtypes.HumanDebuffSpec{
			Type:       "human_char_garble",
			Duration:   1,
			PropNameZh: PropKeyToName(ctx.Entry.PropKey),
		}
	})
}
