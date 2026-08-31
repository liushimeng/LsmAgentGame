// Package debate — 房间级 Agent 统计聚合(2026-08-31 §20260831-09)。
//
// 设计动机:对齐狼人杀 WerewolfRoom.AggregateAgentStats() 的范式(§92a),
// 把"每个 Bot / 每个裁判的 Token + API 统计"汇总成房间级聚合,
// 通过 ClientState.AgentStats 下发到前端 DebateAgentStatsPanel 实时渲染。
//
// 数据来源:
//   - DebateRoom.agentRegistry(*debaterun.Registry)→ BotStats() / JudgeStats()
//   - 注册到 DebateRoom.agentRegistry interface 的两个新方法(参考 CLAUDE.md §92a)。
//
// 锁规约(对齐 §92a):
//   - 公开变体 AggregateAgentStats() 自己取 r.mu;
//   - 锁内变体 aggregateAgentStatsLocked() 仅读 r.agentRegistry,
//     registry 内部取 Bot.mu / JudgeAgent.mu(不同层级,无锁序倒置)。
//
// 派生指标:
//   - ElapsedSec:startedAt 到 now 的秒数(≥ 0,startedAt=0 时为 0);
//   - TokensPerHour:total_api_tokens / (elapsed_sec / 3600),守卫 ≥60s;
//   - ShowTokenRate:守卫(elapsed_sec ≥60 AND total_api_tokens > 0)才显示。
package debate

import (
	"time"
)

// AgentTokenSnapshot 单 Bot 详细统计(用于前端每个 Bot 卡片)。
//
// 字段命名对齐 ClientWeb/src/types/debate.ts::DebateAgentTokenSnapshot,
// 后端 wire tag 与前端 TS interface 一一对应,新增字段必须双向同步。
type AgentTokenSnapshot struct {
	TeamID         int    `json:"team_id"`
	Seat           int    `json:"seat"`
	Role           Role   `json:"role"`
	RoleName       string `json:"role_name,omitempty"`
	ModelKey       string `json:"model_key,omitempty"`
	LLMCallCount   int    `json:"llm_call_count"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
	APITokens      int    `json:"api_tokens"`
	APISuccessCount int   `json:"api_success_count"`
	APIFailCount    int   `json:"api_fail_count"`
}

// JudgeTokenSnapshot 单裁判详细统计。
type JudgeTokenSnapshot struct {
	JudgeID         int    `json:"judge_id"`
	ModelKey        string `json:"model_key,omitempty"`
	LLMCallCount    int    `json:"llm_call_count"`
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	APITokens       int    `json:"api_tokens"`
	APISuccessCount int    `json:"api_success_count"`
	APIFailCount    int    `json:"api_fail_count"`
}

// DebateRoomAgentStats 房间级 Agent 统计聚合。
//
// 字段命名对齐前端 DebateRoomAgentStats;
// bot_* = 辩方聚合、judge_* = 裁判聚合、total_* = bot+judge 房间总聚合。
type DebateRoomAgentStats struct {
	// 辩方 Bot 聚合
	BotCount           int `json:"bot_count"`
	BotTotalInputTokens  int `json:"bot_total_input_tokens"`
	BotTotalOutputTokens int `json:"bot_total_output_tokens"`
	BotTotalAPITokens    int `json:"bot_total_api_tokens"`
	BotAPICallCount      int `json:"bot_api_call_count"`
	BotAPISuccessCount   int `json:"bot_api_success_count"`
	BotAPIFailCount      int `json:"bot_api_fail_count"`

	// 裁判 Agent 聚合
	JudgeCount           int `json:"judge_count"`
	JudgeTotalInputTokens  int `json:"judge_total_input_tokens"`
	JudgeTotalOutputTokens int `json:"judge_total_output_tokens"`
	JudgeTotalAPITokens    int `json:"judge_total_api_tokens"`
	JudgeAPICallCount      int `json:"judge_api_call_count"`
	JudgeAPISuccessCount   int `json:"judge_api_success_count"`
	JudgeAPIFailCount      int `json:"judge_api_fail_count"`

	// 全房间总聚合(辩方 + 裁判)
	TotalInputTokens  int `json:"total_input_tokens"`
	TotalOutputTokens int `json:"total_output_tokens"`
	TotalAPITokens    int `json:"total_api_tokens"`
	TotalAPICallCount int `json:"total_api_call_count"`

	// 房间运行
	ElapsedSec      int64 `json:"elapsed_sec"`       // 已运行秒数(从 startedAt 计)
	TokensPerHour   int64 `json:"tokens_per_hour"`   // 派生:total_api_tokens / (elapsed_sec / 3600)
	ShowTokenRate   bool  `json:"show_token_rate"`   // 守卫:elapsed_sec < 60 时 false
}

// DebateAgentStatsDetail 单房间聚合 + 每个 Agent 详细。
//
// 与 ClientState.AgentStats(只含 aggregate)平行;通过 WS 帧
// debate.stats_update 下发完整结构,供前端 DebateAgentStatsPanel 渲染。
type DebateAgentStatsDetail struct {
	RoomID    string                 `json:"room_id"`
	Aggregate DebateRoomAgentStats   `json:"aggregate"`
	Bots      []AgentTokenSnapshot   `json:"bots"`
	Judges    []JudgeTokenSnapshot   `json:"judges"`
}

// agentRegistryStatsInterface DebateRoom.agentRegistry 必须实现的统计接口(§20260831-09)。
//
// 实际类型为 *debaterun.Registry;DebateRoom 通过该接口读取统计,
// 不直接 import debaterun 包(避免循环引用)。
type agentRegistryStatsInterface interface {
	BotStats() []AgentTokenSnapshot
	JudgeStats() []JudgeTokenSnapshot
}

// AggregateAgentStats 聚合房间级 Agent + 法官 Token + API 统计。
//
// §92a 范式:本函数自己获取 r.mu,只能由**未持锁**的调用方使用。
// BuildClientState 等已持锁路径必须调 aggregateAgentStatsLocked。
func (r *DebateRoom) AggregateAgentStats() *DebateAgentStatsDetail {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.aggregateAgentStatsLocked()
}

// aggregateAgentStatsLocked 是 AggregateAgentStats 的**锁内变体**(§92a 范式)。
//
// 调用方必须已持有 r.mu。
// 仅读 r.agentRegistry(interface 类型)+ r.startedAt + r.Config.Teams/Judges;
// 内部 registry.BotStats()/JudgeStats() 取 Bot.mu/Judge.mu(不同层级锁,
// 不反向获取 r.mu,无锁序倒置)。
func (r *DebateRoom) aggregateAgentStatsLocked() *DebateAgentStatsDetail {
	out := &DebateAgentStatsDetail{RoomID: r.RoomID}

	// 已运行秒数(startedAt = 0 时为 0,与狼人杀 GameRunningClock 语义对齐)
	elapsed := int64(0)
	if r.startedAt > 0 {
		elapsed = int64(time.Now().Unix() - r.startedAt)
		if elapsed < 0 {
			elapsed = 0
		}
	}
	out.Aggregate.ElapsedSec = elapsed

	// 没有 agentRegistry 时(如房间已关闭或尚未启动)→ 仍返回 elapsed = 0
	// 的 aggregate 快照,前端据此显示「等待开始」占位。
	if reg, ok := r.agentRegistry.(agentRegistryStatsInterface); ok && reg != nil {
		out.Bots = reg.BotStats()
		out.Judges = reg.JudgeStats()
	} else {
		// 旧 registry 实现(没有 stats interface)→ 只发空数组 + aggregate=0
		out.Bots = []AgentTokenSnapshot{}
		out.Judges = []JudgeTokenSnapshot{}
	}

	// 聚合(锁内纯计算,无副作用)
	for _, b := range out.Bots {
		out.Aggregate.BotCount++
		out.Aggregate.BotTotalInputTokens += b.InputTokens
		out.Aggregate.BotTotalOutputTokens += b.OutputTokens
		out.Aggregate.BotTotalAPITokens += b.APITokens
		out.Aggregate.BotAPICallCount += b.LLMCallCount
		out.Aggregate.BotAPISuccessCount += b.APISuccessCount
		out.Aggregate.BotAPIFailCount += b.APIFailCount
	}
	for _, j := range out.Judges {
		out.Aggregate.JudgeCount++
		out.Aggregate.JudgeTotalInputTokens += j.InputTokens
		out.Aggregate.JudgeTotalOutputTokens += j.OutputTokens
		out.Aggregate.JudgeTotalAPITokens += j.APITokens
		out.Aggregate.JudgeAPICallCount += j.LLMCallCount
		out.Aggregate.JudgeAPISuccessCount += j.APISuccessCount
		out.Aggregate.JudgeAPIFailCount += j.APIFailCount
	}

	// 全房间总聚合
	out.Aggregate.TotalInputTokens = out.Aggregate.BotTotalInputTokens + out.Aggregate.JudgeTotalInputTokens
	out.Aggregate.TotalOutputTokens = out.Aggregate.BotTotalOutputTokens + out.Aggregate.JudgeTotalOutputTokens
	out.Aggregate.TotalAPITokens = out.Aggregate.BotTotalAPITokens + out.Aggregate.JudgeTotalAPITokens
	out.Aggregate.TotalAPICallCount = out.Aggregate.BotAPICallCount + out.Aggregate.JudgeAPICallCount

	// 派生:TokensPerHour
	if elapsed >= 60 && out.Aggregate.TotalAPITokens > 0 {
		out.Aggregate.ShowTokenRate = true
		// elapsed 是整数秒;小时 = elapsed / 3600,至少 1
		hours := float64(elapsed) / 3600.0
		out.Aggregate.TokensPerHour = int64(float64(out.Aggregate.TotalAPITokens) / hours)
	}

	// 填充 bot 缺失的元数据(role / model_key)
	if len(out.Bots) > 0 {
		roleByTeamSeat := make(map[string]AgentConfig, len(r.Config.Teams)*4)
		for _, team := range r.Config.Teams {
			for _, a := range team.Agents {
				roleByTeamSeat[SeatKey(team.TeamID, a.SeatID)] = a
			}
		}
		for i := range out.Bots {
			key := SeatKey(out.Bots[i].TeamID, out.Bots[i].Seat)
			if ac, ok := roleByTeamSeat[key]; ok {
				out.Bots[i].Role = ac.Role
				out.Bots[i].RoleName = ac.RoleName
				out.Bots[i].ModelKey = ac.ModelKey
			}
		}
	}

	// 填充 judge 缺失的 model_key
	if len(out.Judges) > 0 {
		for i := range out.Judges {
			if jc, ok := r.JudgeByIndex(out.Judges[i].JudgeID); ok {
				out.Judges[i].ModelKey = jc.ModelKey
			}
		}
	}

	return out
}
