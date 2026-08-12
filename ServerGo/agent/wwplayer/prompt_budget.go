// Package wwplayer — user prompt 的块预算与优先级降级。
//
// 2026-08-12 §20260812-04 U2 新增。
//
// # 缺陷背景
//
// `BuildUserPrompt`（prompt.go:200-680）是一条 **41 个块的无条件顺序 `s +=` 链**，
// 其中 16 个是 `XxxBlock()` 函数。审计结论：
//
//   - **没有任何一处门控是「预算不足所以跳过」** —— 全是业务条件（`if ctx.MyTurn`）；
//   - **块内部也无上限** —— `HypothesisTableBlock` 无条数限制，
//     `RumorBlock` 注释说「最近 5 条」但代码里没有这个限制；
//   - `context_budget_test.go`（255 行）**全部测 Memory 层，无一测 prompt 长度**。
//
// 现有的 `getModelContextBudget`（agent.go:1944）只覆盖 messages 历史，
// 且单位是**字节**不是 token（中文 UTF-8 3 bytes/字），还是 8 键硬编码 map ——
// 第 9 个模型静默 fallback 到 200KB。
//
// 唯一的自适应路径是等 provider 返回 400「exceed max message tokens」之后才
// `PruneByBytesAggressive()`（run.go:1073）——**没有任何 pre-flight 检查**。
//
// # 设计（对照 TencentDB-Agent-Memory）
//
// 参考项目的 4 级压缩阶梯有两条经验被直接采用：
//
//  1. **整块丢弃而非截断** —— 半截的假说表比没有假说表更糟（它会让 LLM
//     基于不完整信息推理，比信息缺失更危险）。
//  2. **降级必须留下可观测标记** —— 参考项目的反面教材是 L1 抽取失败返回 `[]`，
//     与「确实没啥可抽」完全同形，静默劣化潜伏了整整一个版本。
//     所以这里丢弃时会在末尾追加 `[已省略 N 个低优先级信息块: ...]`。
package wwplayer

import (
	"sort"
	"strings"
)

// 块优先级。数值越小越不可牺牲；预算不足时从数值最大的开始整块丢弃。
//
// 分档依据是「LLM 少了它会不会做出**违规或明显错误**的决策」：
//   - Critical：少了会违规（不知道自己是谁、不知道私有信息、不知道该干什么）
//   - High：少了会明显错判（不知道谁活着、谁投了谁）
//   - Medium：少了推理质量下降但不会错到离谱
//   - Low：锦上添花的氛围/生态类信息
const (
	// PriorityCritical 永不牺牲：身份、夜间私有信息、当前阶段指令。
	PriorityCritical = 0
	// PriorityHigh 存活列表、投票状态、发言历史。
	PriorityHigh = 100
	// PriorityMedium 假说表、知识摘要、影响力。
	PriorityMedium = 200
	// PriorityLow 流言、对手画像、一致性校验、经济反馈。
	PriorityLow = 300
)

// userPromptTailBudgetRunes 是 BuildUserPrompt 的总 rune 预算（含头部）。
//
// 定值依据（本次审计实测）：
//   - 最小上下文下 BuildUserPrompt ≈ 3,815 bytes ≈ 1,600 runes；
//   - 13 人局 Round 3 speak 阶段实测约 6,500 runes；
//   - 叠加长期记忆后可达 10,500 runes。
//
// 取 12,000 runes（≈ 9.6K token）：**正常对局完全不触发裁剪**，
// 只在假说表/流言/画像同时膨胀的病态场景兜底。
//
// 刻意取保守大值而非「按模型窗口精算」：本次改动的目标是**建立护栏**，
// 不是压缩 token。激进的预算会在没有充分线上数据前误伤正常对局 ——
// 宁可护栏偶尔不生效，也不要它频繁误杀（对照 §20260811-08 的教训：
// 静默劣化比不优化更贵）。后续可按模型窗口做差异化。
const userPromptTailBudgetRunes = 12000

// PromptBlock 是一个带优先级的 prompt 片段。
type PromptBlock struct {
	// Name 是可读名（出现在省略提示里，便于线上排查「这轮丢了什么」）。
	Name string
	// Priority 取 PriorityCritical / High / Medium / Low。
	Priority int
	// Text 是块的完整文本（含前导换行）。空串视为无内容，直接跳过。
	Text string
}

// AssembleWithBudget 按优先级拼装块，并在超出 maxRunes 时从低优先级开始整块丢弃。
//
// 返回 (拼装结果, 被丢弃的块名列表)。
//
// 语义细节：
//   - maxRunes ≤ 0 表示不限预算，全部拼接（与旧行为一致，用于灰度关闭）。
//   - **同优先级内保持传入顺序**（sort.SliceStable），因为 BuildUserPrompt 的块
//     顺序本身编码了「认知顺序」：我知道什么 → 我猜什么 → 我的话有多大分量。
//   - PriorityCritical 的块**即使超预算也不丢弃** —— 宁可超一点，
//     也不能让 LLM 不知道自己的身份或私有信息（那会直接导致违规操作）。
//   - 丢弃发生时，结果末尾追加一行可观测标记。
func AssembleWithBudget(blocks []PromptBlock, maxRunes int) (string, []string) {
	// 过滤空块，保留原始序号用于稳定排序。
	type indexed struct {
		blk PromptBlock
		ord int
	}
	items := make([]indexed, 0, len(blocks))
	for i, b := range blocks {
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		items = append(items, indexed{blk: b, ord: i})
	}
	if len(items) == 0 {
		return "", nil
	}

	// 不限预算：按原始顺序直接拼。
	if maxRunes <= 0 {
		var sb strings.Builder
		for _, it := range items {
			sb.WriteString(it.blk.Text)
		}
		return sb.String(), nil
	}

	// 按优先级升序（Critical 先）；同优先级保持原顺序。
	byPriority := make([]indexed, len(items))
	copy(byPriority, items)
	sort.SliceStable(byPriority, func(i, j int) bool {
		return byPriority[i].blk.Priority < byPriority[j].blk.Priority
	})

	// 逐块「录取」，超预算即淘汰（Critical 除外）。
	keep := make(map[int]bool, len(items))
	var dropped []string
	used := 0
	for _, it := range byPriority {
		n := len([]rune(it.blk.Text))
		if it.blk.Priority <= PriorityCritical {
			// Critical 无条件录取，但仍计入已用量，
			// 这样后面的低优先级块能感知到真实剩余空间。
			keep[it.ord] = true
			used += n
			continue
		}
		if used+n > maxRunes {
			dropped = append(dropped, it.blk.Name)
			continue
		}
		keep[it.ord] = true
		used += n
	}

	// 按**原始顺序**输出（认知顺序不能被优先级排序打乱）。
	var sb strings.Builder
	for _, it := range items {
		if keep[it.ord] {
			sb.WriteString(it.blk.Text)
		}
	}
	if len(dropped) > 0 {
		// 可观测标记:让「被裁剪」与「本来就没有」在 prompt 里可区分。
		sb.WriteString("\n\n[本轮因上下文预算省略了 " + itoa(len(dropped)) +
			" 个低优先级信息块: " + strings.Join(dropped, "、") +
			"。如需相关信息请在发言中主动询问。]")
	}
	return sb.String(), dropped
}
