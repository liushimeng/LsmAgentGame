// Package debate implements the AI-vs-AI "辩论比赛" (Debate Competition) engine.
//
// # 概要
//
// 辩论比赛是纯 Agent 驱动的自动化辩论平台:
//   - 辩方 Agent(数量由 Mode 决定,2-10 个):由 LLM 驱动,按辩位 (一/二/三/四辩) 职责发言。
//   - 裁判 Agent(固定 3 个):独立评审 + 打分,单数投票。
//   - 人类玩家:仅作为房主 / 观众,不可参赛。
//
// # 阶段机
//
// 阶段机按文档定义:
//
//	PhaseFilling → PhasePreparation → PhaseOpeningArgument
//	→ PhaseRebuttal → PhaseCrossExamination → PhaseCrossExamSummary
//	→ PhaseFreeDebate → PhaseClosingArgument → PhaseJudging
//	→ PhaseResult → PhaseGameOver
//
// 每阶段由 watchdog 控制最大时长,超时自动推进下一阶段。
//
// # 数据流
//
//	HTTP POST /api/games/debate/rooms 创建房间
//	  ↓
//	DebateManager.CreateRoom → DebateRoom
//	  ↓
//	StartGame(房主触发)→ 引擎启动 → 每阶段推进
//	  ↓ WS 帧(实时)
//	前端 useDebate 订阅 → store 更新 → 渲染
//	  ↓ DB(异步,§20260831-08)
//	onSpeech/onJudgeScore/onResult/onGameOver → persistence.go
//	  → t_lsm_game_debate_{speech,score,room,model_stats}
//	  → GET /api/games/debate/history(复盘)+ /stats(跨重启胜率)
//
// # 公平性
//
// 模型分配走 FairModelAssignment:蛇形 + 洗牌,确保正反方能力均衡,
// 裁判模型与辩方不重复(详见 fair_assignment.go)。
//
// # 持久化(§20260831-08)
//
// AttachPersistence(m, gormDB) 链式包装广播钩子(gormDB == nil 时 no-op):
// 发言/评审逐条落库,比赛结束 upsert 房间记录 + model_stats 原子累加,
// 启动时回读统计(重启不清零)。详见 persistence.go。
//
// # 详细设计
//
// 完整设计见 docs/辩论比赛/*.md(2026-08-31 §00-§06 体系)。
//
// 本包首期实现 §20260831-01,持久化 §20260831-08。
package debate
