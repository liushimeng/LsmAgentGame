// Package werewolf — 夜间私有信息聚合（Agent 侧）。
//
// 2026-08-12 §20260812-04 U1 (P0-1) 新增。
//
// # 缺陷背景
//
// `MySeerCheck`（room_agent.go:766）与 `WolfTarget`（:770）自落地起就由引擎正确填充，
// 但全仓 grep 显示 `agent/` 目录下 **零读取点**、`prompt.go` **零渲染**：
//
//   - AI 预言家查完人永远不知道查验结果（`SeerCheck` runner 只 `return "ok"`）；
//   - AI 女巫永远不知道今晚谁被刀（`witch_act` schema 只说「救活今晚被狼杀的玩家」，
//     不告知是谁）；
//   - `EmitSeerCheck` 文案不含阵营，且 `silentForBots=true`（其注释还写反了，
//     声称 `false`，这多半是本缺陷长期未被发现的直接原因）。
//
// 而人类玩家走 `view.go:1285` 的 `BuildSeerInform` 可以正常拿到 `LastResultFaction` ——
// 两个核心神职的技能对 Agent 完全失效，且**人类玩家不受影响**，直接违反
// §15「公平性」与 §120。
//
// # 事实来源选择
//
// 查验历史用 `Player.SeerCheckHistory []Seat`（engine.go:121，§20260810-04 U3 落地，
// 由 `engine_night.go:202` 每次查验成功追加、`engine.go:705` 每局重置）。
//
// 刻意**不用** `InformationLedger`：后者存的是 `"seer_check seat=S target=T"` 字符串，
// 需要 `Sscanf` 反解，而 §20260811-08 的 P0 正是「脱敏函数打坏了这个前缀导致解析
// 全部失败」。直接读结构化字段没有这个脆弱面。
//
// # 为什么单独一个文件
//
// `room_agent.go` 已 1903 行、超过 CLAUDE.md §4 的 1800 行硬上限，不再往里加。
package werewolf

import "LsmAgentGame/agent/wwtypes"

// buildSeerCheckHistoryLocked 聚合某预言家座位本局的全部查验历史。
//
// 调用方必须持有 r.mu（本函数只读 r.State，不自己加锁）——
// 唯一调用点 `buildAgentContextLocked` 已持锁（§92a）。
//
// 返回顺序即查验发生顺序（`SeerCheckHistory` 是 append-only 切片）。
// Round 字段填 index+1：首夜查验记为第 1 轮，与玩家口语一致。
//
// 阵营只给 "wolf"（查杀）/ "good"（金水），不给具体身份 ——
// §15 规则：预言家只知阵营不知具体神职。`wwtypes.SeerCheckRecord` 从类型上
// 就没有 Role 字段，杜绝越权渲染。
func (r *WerewolfRoom) buildSeerCheckHistoryLocked(seerSeat int) []wwtypes.SeerCheckRecord {
	if r.State == nil || seerSeat < 0 || seerSeat >= MaxPlayers {
		return nil
	}
	hist := r.State.Players[seerSeat].SeerCheckHistory
	if len(hist) == 0 {
		return nil
	}
	out := make([]wwtypes.SeerCheckRecord, 0, len(hist))
	for i, target := range hist {
		if target < 0 || int(target) >= MaxPlayers {
			continue
		}
		faction := "good"
		if FactionOf(r.State.Roles[target]) == FactionWolf {
			faction = "wolf"
		}
		out = append(out, wwtypes.SeerCheckRecord{
			Round:   i + 1,
			Seat:    int(target),
			Faction: faction,
		})
	}
	return out
}
