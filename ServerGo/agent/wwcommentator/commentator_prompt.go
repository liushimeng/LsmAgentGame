package wwcommentator

import "strings"

// §20260811-09 U1.4 — 解说 system / user prompt。
// system 约束风格 + 硬限制;user 渲染快照事实(去重 + 长度保护)。

const (
	// hardLimits 是全局硬约束,所有风格共用。
	hardLimits = "【硬约束】\n" +
		"• 解说长度 ≤ 120 字。\n" +
		"• 不要编造任何未在「快照事实」中出现的事件。\n" +
		"• 不要输出工具调用语法 / JSON / 内部标识符。\n" +
		"• 输出中文(若用户快照用其他语言,则用对应语言)。\n" +
		"• 你是观战者的解说,**绝不**会与玩家对话 — 不要写成 1v1 互动。\n"

	stylePro = "【风格:专业严谨】\n" +
		"你是狼人杀赛事专业解说员,熟悉 13 人局标准竞技局规则。\n" +
		"语气:客观、数据驱动、概率推理、战术拆解;适度使用赛事术语(如「悍跳」「查杀」「警徽流」「倒钩」)。\n" +
		"结构:1~2 句事实陈述 + 1 句战术点评或趋势预测。\n"

	styleFun = "【风格:娱乐吐槽】\n" +
		"你是狼人杀赛事娱乐型解说员,语言犀利、玩梗适度。\n" +
		"语气:调侃、戏剧化、观众友好;偶尔用 emoji 但不要刷屏。\n" +
		"结构:1 句反差梗 + 1 句战术洞察 + 1 句对观众情绪的呼应。\n"
)

func buildSystemPrompt(style string) string {
	dir := stylePro
	if style == "fun" {
		dir = styleFun
	}
	return dir + "\n" + hardLimits
}

func buildUserPrompt(snap *CommentarySnapshot) string {
	var b strings.Builder
	b.WriteString("【事件触发】")
	b.WriteString(snap.EventKind)
	b.WriteString("\n")
	b.WriteString("【快照事实】\n")
	b.WriteString("- 房间: ")
	b.WriteString(snap.RoomID)
	b.WriteString("\n- 阶段: ")
	b.WriteString(snap.Phase)
	b.WriteString(" · 轮 ")
	b.WriteString(itoa(snap.Round))
	b.WriteString(" · 第 ")
	b.WriteString(itoa(snap.Day))
	b.WriteString(" 天\n- 存活: ")
	b.WriteString(joinInts(snap.Alive))
	b.WriteString("\n")

	// 上帝视角信息 —— **只用于本快照内 prompt 构造**,绝不进任何玩家可见字段。
	if len(snap.Roles) > 0 {
		b.WriteString("- 上帝视角身份(仅你可看):\n")
		for seat, role := range snap.Roles {
			b.WriteString("  · ")
			b.WriteString(itoa(seat))
			b.WriteString(" 号 → ")
			b.WriteString(role)
			b.WriteString("\n")
		}
	}
	if len(snap.RecentPub) > 0 {
		b.WriteString("- 最近公开发言:\n")
		for _, line := range snap.RecentPub {
			b.WriteString("  · ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if len(snap.WolfVote) > 0 {
		b.WriteString("- 昨夜狼队协商摘要(已 §119 隔离过的副本):\n")
		for _, line := range snap.WolfVote {
			b.WriteString("  · ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if len(snap.Extra) > 0 {
		b.WriteString("- 事件数据: ")
		b.WriteString(joinMap(snap.Extra))
		b.WriteString("\n")
	}
	if len(snap.History) > 0 {
		b.WriteString("- 你最近 ")
		b.WriteString(itoa(len(snap.History)))
		b.WriteString(" 条解说(请勿重复):\n")
		for _, h := range snap.History {
			b.WriteString("  · ")
			b.WriteString(truncateForPrompt(h, 80))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n请基于上述事实生成 1 条解说(≤120 字)。")
	return b.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func joinInts(xs []int) string {
	if len(xs) == 0 {
		return "(无)"
	}
	var b strings.Builder
	for i, x := range xs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(itoa(x))
		b.WriteString(" 号")
	}
	return b.String()
}

func joinMap(m map[string]any) string {
	var b strings.Builder
	first := true
	for k, v := range m {
		if !first {
			b.WriteString("; ")
		}
		first = false
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(toStringShort(v))
	}
	if first {
		return "(无)"
	}
	return b.String()
}

func toStringShort(v any) string {
	switch x := v.(type) {
	case string:
		return truncateForPrompt(x, 60)
	case int:
		return itoa(x)
	case int64:
		return itoa(int(x))
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return "?"
	}
}

func truncateForPrompt(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}