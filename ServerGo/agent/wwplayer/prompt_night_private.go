// Package wwplayer — 夜间私有信息 prompt 块。
//
// 2026-08-12 §20260812-04 U1 (P0-1) 新增。
//
// # 这个文件修的是什么
//
// `GameContext.MySeerCheck` / `WolfTarget` 由引擎正确填充（room_agent.go:766/770），
// 但在本次修复之前，**`agent/` 目录下没有任何一处读取它们，`prompt.go` 也从不渲染**。
// 后果：
//
//   - AI 预言家每晚查人，但查验结果从未进入它的上下文 —— 它只能靠猜；
//   - AI 女巫从不知道今晚谁被狼刀，`witch_act` 的 tool 描述也只说
//     「救活今晚被狼杀的玩家」而不告诉它是谁。
//
// 人类玩家走 `view.go` 的 `BuildSeerInform` / `SeerLastCheck` 一直能正常看到，
// 所以这是**只影响 AI 的信息不对称**，直接违反 §15「公平性」与 §120。
//
// # 设计要点
//
//   - 只渲染阵营（金水/查杀），不渲染具体身份 —— §15：预言家只知阵营不知神职。
//     `wwtypes.SeerCheckRecord` 从类型上就没有 Role 字段。
//   - 守卫**不渲染狼刀目标** —— §134 盲守语义（`GuardLastProtect` 可以给，
//     因为 G1「不可连守同一人」需要它）。
//   - 空内容返回 ""，由调用方决定是否拼接（与其余 Block 函数约定一致）。
package wwplayer

import (
	"strings"

	"LsmWebGame/agent/wwtypes"
)

// NightPrivateInfoBlock 渲染本 Agent 的夜间私有信息（查验历史 / 狼刀目标 /
// 守护约束 / 药剂余量）。非神职或无私有信息时返回 ""。
//
// 输出示例（预言家，第 3 天）：
//
//	【🔒 你的私有信息 — 仅你可见】
//	你是预言家。以下是你本局全部查验结果（这是你独有的硬信息，发言时可据此建立可信度）：
//	  · 第 1 轮 查验 4号 → 🐺 查杀（狼人）
//	  · 第 2 轮 查验 7号 → ✅ 金水（好人）
//	⚠️ 查验只给阵营，不给具体身份；4号 是狼人但不知道是普通狼还是狼王。
func NightPrivateInfoBlock(ctx *wwtypes.GameContext) string {
	if ctx == nil {
		return ""
	}
	var b strings.Builder
	switch ctx.Role {
	case "seer":
		writeSeerPrivate(&b, ctx)
	case "witch":
		writeWitchPrivate(&b, ctx)
	case "guard":
		writeGuardPrivate(&b, ctx)
	default:
		return ""
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\n【🔒 你的私有信息 — 仅你可见】\n" + b.String()
}

// writeSeerPrivate 渲染预言家的查验历史。
//
// 优先渲染完整历史（MySeerCheckHistory）；历史为空但有 MySeerCheck 时
// 退化渲染单条（兼容 history 未填充的调用路径，如旧测试）。
func writeSeerPrivate(b *strings.Builder, ctx *wwtypes.GameContext) {
	if len(ctx.MySeerCheckHistory) == 0 && ctx.MySeerCheck < 0 {
		b.WriteString("你是预言家。你还没有查验过任何人。今晚请选择一名存活玩家查验。\n")
		return
	}
	b.WriteString("你是预言家。以下是你本局全部查验结果（这是你独有的硬信息，发言时可据此建立可信度）：\n")
	if len(ctx.MySeerCheckHistory) > 0 {
		for _, rec := range ctx.MySeerCheckHistory {
			b.WriteString("  · 第 " + itoa(rec.Round) + " 轮 查验 " + itoa(rec.Seat+1) + "号 → " + factionLabel(rec.Faction) + "\n")
		}
	} else {
		// 退化路径:只有上一晚的单条结果。
		b.WriteString("  · 上一晚 查验 " + itoa(ctx.MySeerCheck+1) + "号 → " + factionLabel(ctx.MySeerCheckFaction) + "\n")
	}
	b.WriteString("⚠️ 查验只给阵营，不给具体身份 —— 查杀的人是狼人，但不知道是普通狼还是狼王；金水的人是好人，但不知道是神职还是平民。\n")
}

// writeWitchPrivate 渲染女巫的狼刀目标与药剂余量。
func writeWitchPrivate(b *strings.Builder, ctx *wwtypes.GameContext) {
	b.WriteString("你是女巫。\n")
	if ctx.WolfTarget >= 0 {
		b.WriteString("  · 🔪 今晚狼人刀的是 " + itoa(ctx.WolfTarget+1) + "号。\n")
	} else {
		b.WriteString("  · 🔪 今晚狼人空刀（或你已用过解药，看不到刀口）。\n")
	}
	b.WriteString("  · 💊 解药：" + usedLabel(ctx.WitchAntidoteUsed) + "　🧪 毒药：" + usedLabel(ctx.WitchPoisonUsed) + "\n")
	if !ctx.WitchAntidoteUsed || !ctx.WitchPoisonUsed {
		b.WriteString("⚠️ 同一晚不能既用解药又用毒药；两瓶药整局各一次，用完不再有。\n")
	}
}

// writeGuardPrivate 渲染守卫的连守约束。
//
// 刻意**不渲染狼刀目标** —— §134 盲守：守卫看不到狼刀，
// `GameContext.WolfTarget` 对守卫恒为 -1，这里也绝不去读它。
func writeGuardPrivate(b *strings.Builder, ctx *wwtypes.GameContext) {
	b.WriteString("你是守卫（盲守 —— 你看不到今晚狼人刀谁）。\n")
	if ctx.GuardLastProtect >= 0 {
		b.WriteString("  · 🛡 你上一晚守护的是 " + itoa(ctx.GuardLastProtect+1) + "号，今晚**不可**再守他（不可连守同一人）。\n")
	} else {
		b.WriteString("  · 🛡 你上一晚没有守人，今晚可守任意存活玩家（自己除外）。\n")
	}
	b.WriteString("⚠️ 同守同救会导致目标死亡：若你守的人恰好也被女巫解药救，该玩家反而出局。\n")
}

// factionLabel 把 "wolf"/"good" 渲染成玩家口语标签。
func factionLabel(faction string) string {
	switch faction {
	case "wolf":
		return "🐺 查杀（狼人）"
	case "good":
		return "✅ 金水（好人）"
	default:
		return "（结果未知）"
	}
}

func usedLabel(used bool) string {
	if used {
		return "已用完"
	}
	return "仍可使用"
}
