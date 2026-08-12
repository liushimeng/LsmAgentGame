// Package agent — class_names.go: AgentClassName 常量集中定义(2026-08-06 §Agent 重构增强)。
//
// 设计动机:
//   1. 每种 Agent 实现(玩家 Bot / 法官 / 未来其他游戏 / 未来其他类型)
//      都有一个独立的 AgentClassName,统一在此文件登记。
//   2. User-Agent 拼装规则(出站 LLM HTTP 请求头):
//         User-Agent: <AgentClassName>/<AppVersion> <buildDateTime>
//      其中 AppVersion 与 buildDateTime 来自 main.go,经 llm.Registry
//      的 SetUserAgent 注入到每个 Provider(共享底层 HTTP 客户端)。
//   3. 命名规则:
//      - 游戏无关通用 Agent     → "LsmAgentGame-<职能>" (例:未来 "LsmAgentGame-Sandbox")
//      - 狼人杀玩家 Bot          → "LsmAgentGame-Werewolf-Player"
//      - 狼人杀法官 Bot          → "LsmAgentGame-Werewolf-Judge"
//      - 其他游戏玩家 Bot        → "LsmAgentGame-<Game>-Player"
//      - 其他游戏法官/裁判 Bot   → "LsmAgentGame-<Game>-Judge"
//      - 工具型 Agent(记忆迭代)  → "LsmAgentGame-<Game>-MemoryIter"
//
// 4. 与 §14 LLM Provider 的关系:
//    - AgentClassName 用于 User-Agent(让上游/网关区分调用方);
//    - ModelKey/Provider 由 llm.Registry 管理,决定**用哪个模型**;
//    - 二者正交:同一 AgentClassName 可配多个 ModelKey(同一类 Agent 用
//      不同 LLM 模型),同一 ModelKey 也可被多个 AgentClassName 复用(不推荐,
//      会让上游无法区分调用方)。
//
// 注册新 Agent 时:
//   1. 在本文件追加常量(注释注明出处 §/docs/);
//   2. 在对应 Agent 实现里以常量引用,**不要**散写字面量;
//   3. 在 llm.Registry.SetUserAgent 调用处用本常量拼装 UA(见 main.go
//      与 agent_run 系列路径)。
package agent

// AgentClassName 是 Agent 实现的唯一类别标识。类型化避免与 ModelKey
// (LLM 模型标识)混淆 — ModelKey 决定**用哪个模型**,AgentClassName 决定
// **是哪一类调用方**。
type AgentClassName string

// 狼人杀(Werewolf)Agent 类别。
const (
	// AgentClassWerewolfPlayer 是狼人杀玩家 Bot 的 AgentClassName。
	// 由 ServerGo/agent/wwplayer/(原 ServerGo/agent/)的 Agent struct
	// 实现;驱动 WWerewolf 引擎参与游戏(发言/投票/技能/私聊)。
	// 详见 docs/狼人杀-Agent与系统/狼人杀Agent设计.md / docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md。
	AgentClassWerewolfPlayer AgentClassName = "LsmAgentGame-Werewolf-Player"

	// AgentClassWerewolfJudge 是狼人杀法官(主持人)Bot 的 AgentClassName。
	// 由 ServerGo/agent/wwjudge/(原 ServerGo/agent/judge*.go)的
	// AgentJudge struct 实现;负责公开宣告/阶段切换口播/死因宣告/
	// 整局总结。**不**参与投票/夜间行动/胜负。
	// 详见 docs/狼人杀-重构方案/主持人Agent重构设计.md / docs/狼人杀-重构方案/主持人Agent重构设计.md。
	AgentClassWerewolfJudge AgentClassName = "LsmAgentGame-Werewolf-Judge"

	// AgentClassWerewolfMemoryIter 是狼人杀 Agent 持久化记忆(MEMORY.md)
	// 自我迭代的 AgentClassName。由 ServerGo/agent/wwplayer/memory_iterate.go
	// 的 IterateAgentMemoriesAsync 调用;读旧记忆 + 本局事实 + 法官总结,
	// 生成新 MEMORY.md 写回 t_lsm_game_agent_memory。
	// 详见 docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md §131。
	AgentClassWerewolfMemoryIter AgentClassName = "LsmAgentGame-Werewolf-MemoryIter"

	// AgentClassWerewolfProfileIter 是狼人杀 Agent「玩家行为画像」迭代的
	// AgentClassName。由 ServerGo/game/werewolf/player_profile_bridge.go 的
	// IteratePlayerProfilesAsync 调用;每局结束后对每个 (bot model_key ×
	// 人类 user_id) 组合异步生成/更新打法画像,写回
	// t_lsm_game_agent_player_profile。
	// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-05.md §U1。
	AgentClassWerewolfProfileIter AgentClassName = "LsmAgentGame-Werewolf-ProfileIter"

	// AgentClassWerewolfRecall 是狼人杀「赛后复盘问答」的 AgentClassName。
	// 由 ServerGo/game/werewolf/recall_chat.go 的 RecallChat 调用;对局结束后
	// 玩家/观战者向指定 bot 座位提问,bot 用冻结的本局 Memory 快照 + 复盘
	// system 指令做单轮问答(不写回 Memory,不进 chat 表)。
	// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-05.md §U2。
	AgentClassWerewolfRecall AgentClassName = "LsmAgentGame-Werewolf-Recall"

	// AgentClassWerewolfCommentator 是狼人杀「AI 实时解说」的 AgentClassName。
	// 由 ServerGo/agent/wwcommentator/ 的 CommentatorAgent struct 实现;
	// 观战模式新增 🎙️ 解说席,事件驱动 + 双风格(pro 严谨 / fun 吐槽),
	// 仅推送给观战者(Hub.BroadcastRoomSpectators),玩家与 Agent 不可见。
	// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-09.md §U1。
	AgentClassWerewolfCommentator AgentClassName = "LsmAgentGame-Werewolf-Commentator"
)

// AllAgentClassNames 返回当前已注册的全部 AgentClassName。
// 供 main.go / 测试 / 管理接口枚举展示用。新增 Agent 类型时**必须**
// 同时把常量追加到本切片。
func AllAgentClassNames() []AgentClassName {
	return []AgentClassName{
		AgentClassWerewolfPlayer,
		AgentClassWerewolfJudge,
		AgentClassWerewolfMemoryIter,
		AgentClassWerewolfProfileIter,
		AgentClassWerewolfRecall,
		AgentClassWerewolfCommentator,
	}
}

// IsValidAgentClassName 报告给定字符串是否是已注册的 AgentClassName。
func IsValidAgentClassName(s string) bool {
	for _, c := range AllAgentClassNames() {
		if string(c) == s {
			return true
		}
	}
	return false
}