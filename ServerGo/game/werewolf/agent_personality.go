// Package werewolf — agent_personality.go: 人设化 Agent 性格倾向参数装配层（§20260811-04 U2）。
//
// 设计动机(Agent-Surpport-01.md §9.4 / §4.4):
//   - 同一模型所有 Agent 打法同质化 → 持久化"性格"维度,让 13 人局内
//     7 个 bot 展现差异化策略倾向(逻辑流 vs 戏精型 vs 激进冲锋)。
//   - 与 §20260810-10 U2 ModelSelfPortrait 同源思路(都是 system 末尾注入),
//     但维度互补:SelfPortrait 是「事后经验」(胜率/局数),Personality
//     是「事前倾向」(5 维行为向量)。
//
// 数据流:
//   - RoomConfig.PersonalityMode 在 RoomCreateModal 设置(uniform/random/custom)。
//   - 创建房间时由房间装配层(StartAgentsLocked)按模式决定每个 seat 的 preset,
//     写入 t_lsm_game_agent_personality。
//   - 每局 StartAgentsLocked 末尾调 resolvePersonalityForSeatLocked(r, seat)
//     从 DB 读 5 维向量,灌入 ag.Personality / ag.PersonalityPresetKey。
//
// §92a 锁约束:resolvePersonalityForSeatLocked 在持锁路径被调
//   (StartAgentsLocked 末尾),所以*不*自行加锁(调用方持 r.mu)。
// §119 协议层隔离:仅注入 system 末尾,无 chat 写入。
// §128 对话即思考:5 维向量是「你是谁」,不是「策略建议」。
package werewolf

import (
	"context"

	"LsmAgentGame/agent/wwplayer"
)

// PersonalityAssignmentMode 是房间级人设分配模式(对应 RoomConfig.PersonalityMode)。
const (
	PersonalityModeUniform = "uniform" // 7 个 Agent 统一人设(默认 logical)
	PersonalityModeRandom  = "random"  // 每个 Agent 独立随机人设
	PersonalityModeCustom  = "custom"  // 用户自定义 5 维向量
)

// resolvePersonalityForSeatLocked 在 StartAgentsLocked 末尾(持锁路径)
// 装配某座位的人设向量。
//
// 简化策略(§13 §118 异步持久化 — DB 失败仅 log,降级为 logical 预设):
//   - 读 r.personalityMode / r.personalityPresetKey / r.personalityCustomVec
//     (装配层已在房间初始化时写入);
//   - uniform → 全部用 personalityPresetKey;
//   - random  → 按 seat % len(presets) 选(确定性,与 AutoAssignRoles 同款);
//   - custom  → 用 personalityCustomVec;
//
// DB 回读链路为可选增强:本期先用 in-memory 字段装配,后续可补 DB-first。
func resolvePersonalityForSeatLocked(r *WerewolfRoom, seat int) (wwplayer.PersonalityVector, string) {
	mode := r.personalityMode
	if mode == "" {
		mode = PersonalityModeUniform
	}
	presetKey := r.personalityPresetKey
	if presetKey == "" {
		presetKey = "logical"
	}

	switch mode {
	case PersonalityModeRandom:
		// 5 个预设 key 列表(确定性按 seat 选,无随机 → 同一房间所有 goroutine
		// 看到一致结果;与 wolfRoleTemplates 同款确定性约定)。
		presetKeys := []string{"logical", "emotional", "aggressive", "cautious", "showman"}
		pick := presetKeys[seat%len(presetKeys)]
		return wwplayer.LookupPersonalityPreset(pick), pick
	case PersonalityModeCustom:
		if vec := r.personalityCustomVec; vec != nil {
			return *vec, presetKey
		}
		return wwplayer.LookupPersonalityPreset("logical"), "logical"
	default: // uniform
		return wwplayer.LookupPersonalityPreset(presetKey), presetKey
	}
}

// cfgWerewolfAgentPersonalityEnabled 是开关(默认 true)。与 §20260810-10 U2
// cfgWerewolfModelSelfPortraitEnabled 同款模式:运维级 kill switch。
//
// 实现:配置来源 LsmAgentGame.conf 的 werewolf.agent_personality_enabled。
// 此处用占位常量,真正读取走 config 包;若 config 未配置则默认 true。
var cfgWerewolfAgentPersonalityEnabledCache = true

func cfgWerewolfAgentPersonalityEnabled() bool {
	return cfgWerewolfAgentPersonalityEnabledCache
}

// enableAgentPersonalityForTest 仅供测试设置开关(单元测试临时切换)。
func enableAgentPersonalityForTest(v bool) {
	cfgWerewolfAgentPersonalityEnabledCache = v
}

// _ = context.Background  // 保留 import 占位,后续 DB-first 链路会用到。
var _ = context.Background