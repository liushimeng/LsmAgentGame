// Package agent — 狼人杀 Agent 包文档门面（2026-08-06 §Agent 重构 Step 5）。
//
// 本目录历史上是单 package `agent` 的实现根（~21300 行,所有玩家/法官/工具
// /prompt/memory 混在同一目录),2026-08-06 §Agent 重构按"通用基础 / 狼人杀
// 契约 / 玩家 Bot / 法官"四轴拆分:
//
//   - agent/core/      — 通用基础设施(chat_history/record_log/ratelimit/
//                        speak_dedup/llm_helpers),任何游戏的 Agent 都可复用。
//   - agent/wwtypes/   — 狼人杀专属契约类型(GameContext/SpeechEvent/
//                        WhisperEvent/PlayerBrief/PropSnapshot/WolfPackMsg/
//                        WolfVoteTally/SeatEmotionBrief 等),被 wwplayer /
//                        wwjudge / game/werewolf 三方共享。
//   - agent/wwplayer/  — 狼人杀玩家 Bot Agent(19 源 + 25 测试),内含
//                        Agent/Run/Memory/Prompt/ToolRegistry/Emotion 等。
//   - agent/wwjudge/   — 狼人杀法官 Agent(5 源 + 4 测试),内含
//                        AgentJudge/JudgeTranscript/JudgeSummary 等。
//
// 设计原则(详见 docs/Agent-优化和重构设计解决方案-20260806-02.md):
//   1. 依赖方向严格 acyclic:
//      agentcore ← wwtypes ← wwplayer + wwjudge ← game/werewolf
//   2. 禁止反向依赖 — agentcore 不得 import wwtypes/wwplayer/wwjudge;
//      wwtypes 不得 import wwplayer/wwjudge/game/*;wwplayer/wwjudge 平行。
//   3. 游戏专属语义全部归 wwplayer/wwjudge,通用能力归 agentcore,
//      契约类型归 wwtypes。
//   4. 跨包共享的 LLM 调用约定(metadata.user_id / 限流 / 协议归一化)
//      在 agentcore 提供,游戏包不重复实现。
//
// 本目录**不**承载任何可执行代码;下游 import 必须用具体子包路径:
//
//	import (
//	    agentcore "LsmWebGame/agent/core"
//	    wwtypes   "LsmWebGame/agent/wwtypes"
//	    wwplayer  "LsmWebGame/agent/wwplayer"
//	    wwjudge   "LsmWebGame/agent/wwjudge"
//	)
//
// 未来接入新游戏 Agent 时,在同一目录下加 agent/<game>/{types,player,...}
// 子目录即可复用 agentcore。
package agent