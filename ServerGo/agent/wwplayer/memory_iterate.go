// Package agent — agent_memory.go: 狼人杀 Agent 持久化记忆(MEMORY.md)纯函数集。
//
// 2026-07-20 §131 新增。本文件是 leaf 逻辑(纯函数 + 常量),
// 禁止 import models / service / db — agent 包保持不依赖 DB 层。
// 迭代编排(goroutine + LLM 调用 + DB 读写)在 game/werewolf/agent_memory_bridge.go。
// 详见 docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md。
package wwplayer

import (
	"fmt"
	"strings"
)

// 持久化记忆大小控制常量(2026-07-20 §131)。
const (
	// MemoryMaxBytes 是 memory_md 的存储硬上限(100KB)。
	// 超过此值服务端硬截断(HardTruncateMemory),并记 logger.Warn。
	MemoryMaxBytes = 102400
	// MemoryCompressThresholdBytes 是"LLM 迭代时主动瘦身"的触发阈值(80KB)。
	// 旧记忆超过此值时,迭代 prompt 追加压缩指令(删 3 局前细节,
	// 每模型特点保留 ≤3 条),由 LLM 输出精简版 —— 压缩不是纯字符串截断。
	MemoryCompressThresholdBytes = 81920
	// MemoryInjectMaxRunes 是注入 user prompt 时的 rune 上限(约 2K token)。
	// 全文 100KB 是存储上限;注入只带"最相关头部"
	// (段落顺序即重要性:战绩→失误→模型→策略)。
	MemoryInjectMaxRunes = 4000
)

// 4 个固定段落标题(顺序不可调换),与 ValidateMemorySections 对齐。
// 2026-07-20 §131 — 与法官 5 段总结的解析方式一致,便于校验与截断。
var memorySectionTitles = []string{
	"## 战绩与趋势",
	"## 我的失误与教训",
	"## 其他模型特点分析",
	"## 决策策略迭代",
}

// BuildIterationPrompt 构造自我迭代 prompt(2026-07-20 §131)。
// 要求模型在旧记忆基础上融合本局事实(seatFacts)+ 法官整局总结(judgeSummary),
// 输出完整的新 MEMORY.md,段落标题固定 4 个、顺序不可调换、空段写"暂无"。
// compress=true(旧记忆 > 80K)时追加压缩指令:删除 3 局以前的细节,
// 每模型特点保留 ≤3 条。
func BuildIterationPrompt(oldMD, seatFacts, judgeSummary string, compress bool) string {
	var sb strings.Builder
	sb.WriteString("你是狼人杀 AI 玩家。下面是你的【长期记忆】(跨局积累的经验)," +
		"以及【本局事实】和【法官整局总结】。请基于这些信息迭代你的长期记忆," +
		"输出一份全新的、完整的 MEMORY.md(不是 diff,是全文)。\n\n")
	sb.WriteString("硬性格式要求(必须严格遵守):\n")
	sb.WriteString("1. 第一行是标题:# Agent 长期记忆(可附模型名 / 已总结局数 / 更新时间)\n")
	sb.WriteString("2. 正文必须恰好包含以下 4 个二级标题,顺序不可调换:\n")
	for _, t := range memorySectionTitles {
		sb.WriteString("   " + t + "\n")
	}
	sb.WriteString("3. 单段不超过 2000 字;某段没有内容时写\"暂无\",不要省略标题。\n")
	sb.WriteString("4. 不要输出 4 段以外的解释、前言或结语。\n")
	// 2026-08-12 §20260812-04 U4 — 要求「失误与教训」按角色分子段。
	//
	// 记忆按 model_key 存储、不按角色隔离,注入时若不分子段,同一模型坐预言家
	// 学到的教训在它坐狼人时也会被全量注入(既浪费 token 又干扰决策)。
	// 分子段后,SelectMemoryForRole 可在注入侧只保留「通用 + 当前角色」。
	sb.WriteString("5. 在「## 我的失误与教训」段内,**必须**按角色分组," +
		"每组用三级标题 `### 作为<角色名>`(角色名取:狼人 / 预言家 / 女巫 / 猎人 / " +
		"白痴 / 守卫 / 骑士 / 猎魔人 / 平民)。跨角色通用的教训写在该段开头、" +
		"任何 `###` 子标题之前。只写你**确实扮演过**的角色,没扮演过的角色不要凭空编造。\n\n")
	if compress {
		sb.WriteString("【压缩指令】你的旧记忆全文已超过 80K 字节,本次迭代必须压缩历史:\n" +
			"删除 3 局以前的细节,每个模型的特点分析最多保留 3 条," +
			"优先保留最近战绩、最新失误教训与仍有效的策略。\n\n")
	}
	sb.WriteString("【你的旧长期记忆】\n")
	if strings.TrimSpace(oldMD) == "" {
		sb.WriteString("(空 — 这是你的第一局,直接基于本局事实创建记忆)\n\n")
	} else {
		sb.WriteString(oldMD + "\n\n")
	}
	sb.WriteString("【本局事实(你所在座位视角)】\n")
	if strings.TrimSpace(seatFacts) == "" {
		sb.WriteString("(无)\n\n")
	} else {
		sb.WriteString(seatFacts + "\n\n")
	}
	sb.WriteString("【法官整局总结(公开信息)】\n")
	if strings.TrimSpace(judgeSummary) == "" {
		sb.WriteString("(无)\n")
	} else {
		sb.WriteString(judgeSummary + "\n")
	}
	return sb.String()
}

// ValidateMemorySections 校验 LLM 输出是否包含全部 4 个固定段落标题。
// 缺任意一个返回 false,调用方走 FallbackMerge 规则兜底而非丢弃旧记忆。
func ValidateMemorySections(md string) bool {
	for _, t := range memorySectionTitles {
		if !strings.Contains(md, t) {
			return false
		}
	}
	return true
}

// 保留率下限(2026-08-13 §20260813-02 U4,对齐 OpenClaw maxPriorEntryLossFraction):
//   - 常规场景:新记忆 rune 数不得少于旧记忆的 50%;
//   - 压缩场景(旧记忆 > 80K,迭代 prompt 已带压缩指令):放宽到 30%。
//
// 低于下限视为 LLM 截断事故(输出被 max_tokens 切断 / 模型擅自大段删除),
// 调用方必须回退 FallbackMerge(显式失败,禁止假成功)。
const (
	memoryRetentionMinRatio         = 0.5
	memoryRetentionMinRatioCompress = 0.3
)

// ValidateMemoryRetention 校验新记忆相比旧记忆的内容保留率(2026-08-13
// §20260813-02 U4)。ValidateMemorySections 只保证 4 段标题存在,无法识别
// 「标题都在但正文被 LLM 截掉大半」的截断事故 —— 本函数补这一层。
//
// 返回 nil = 通过;非 nil error 描述保留率违规详情(供日志与 FallbackMerge note)。
// 旧记忆为空(首局)时恒通过(无内容可丢)。
func ValidateMemoryRetention(oldMD, newMD string) error {
	oldRunes := len([]rune(oldMD))
	if oldRunes == 0 {
		return nil
	}
	newRunes := len([]rune(newMD))
	minRatio := memoryRetentionMinRatio
	if len(oldMD) > MemoryCompressThresholdBytes {
		// 旧记忆超 80K 时迭代 prompt 带压缩指令,主动瘦身是预期行为,放宽下限。
		minRatio = memoryRetentionMinRatioCompress
	}
	if float64(newRunes) < float64(oldRunes)*minRatio {
		return fmt.Errorf("memory retention violation: new %d runes < old %d runes × %.2f (min ratio, compress=%t)",
			newRunes, oldRunes, minRatio, len(oldMD) > MemoryCompressThresholdBytes)
	}
	return nil
}

// HardTruncateMemory 把 md 硬截断到 maxBytes 字节以内,rune 边界安全
// (绝不切断一个 UTF-8 字符),保留头部 —— 段落顺序即重要性
// (标题 + 战绩在最前)。已在字节上限内时原样返回。
// 这是最后兜底;主路径是"LLM 迭代时主动瘦身"(compress 指令)。
func HardTruncateMemory(md string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(md) <= maxBytes {
		return md
	}
	// rune 安全截断:先按 rune 迭代找到 ≤ maxBytes 的最大合法前缀。
	b := []byte(md)
	end := maxBytes
	// UTF-8 续字节形如 10xxxxxx;向前退到非续字节(字符起始)。
	for end > 0 && end < len(b) && (b[end]&0xC0) == 0x80 {
		end--
	}
	out := string(b[:end])
	return out + "\n\n…(全文超长已截断)\n"
}

// FallbackMerge 是 LLM 输出不合格(段落不全 / 空响应 / 调用失败)时的规则兜底:
// 保留旧记忆全文,在末尾追加一行本局 note,保证"这一局的经验"不丢。
// 旧记忆为空时返回一个含 4 段骨架 + note 的新文档。
func FallbackMerge(oldMD, gameNote string) string {
	note := strings.TrimSpace(gameNote)
	if note == "" {
		note = "(本局迭代失败,仅记录局数)"
	}
	line := fmt.Sprintf("- %s", note)
	if strings.TrimSpace(oldMD) == "" {
		var sb strings.Builder
		sb.WriteString("# Agent 长期记忆\n\n")
		for _, t := range memorySectionTitles {
			sb.WriteString(t + "\n")
			if t == "## 决策策略迭代" {
				sb.WriteString(line + "\n")
			} else {
				sb.WriteString("暂无\n")
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}
	return strings.TrimRight(oldMD, "\n") + "\n\n" + line + "\n"
}

// TruncateMemoryBySections 按 4 个固定段落标题切段,每段配额 rune 截断,
// 标题永远保留(§20260810-04 U4,修复 LongCat-D4「头部截断导致第 4 段
// 「决策策略迭代」被系统性丢弃」缺陷)。
//
// 行为:
//   - 完整包含全部 4 个标题时,每段配额 = maxRunes/4(向下取整,余数给第 4 段
//     以优先保留「决策策略迭代」);
//   - 缺失任一标题视为旧格式记忆,回退到头部 maxRunes rune 截断;
//   - 空字符串直接返回空(调用方应自行处理)。
func TruncateMemoryBySections(md string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if !ValidateMemorySections(md) {
		// 回退路径:头部截断(与原行为一致)。
		return truncateHeadByRunes(md, maxRunes)
	}
	// 找到 4 个标题的位置(按 memorySectionTitles 顺序)。
	// segStarts[i] = 标题 i 的字节偏移(0-indexed);segStarts[len] = len(md)。
	segStarts := make([]int, len(memorySectionTitles)+1)
	for i, title := range memorySectionTitles {
		idx := strings.Index(md, title)
		if idx < 0 {
			// 校验后再走到这里理论上不会触发;兜底回退头部截断。
			return truncateHeadByRunes(md, maxRunes)
		}
		segStarts[i] = idx
	}
	segStarts[len(memorySectionTitles)] = len(md)

	// 段配额:平均分配(向下取整,余数给第 4 段以优先保留「决策策略迭代」)。
	perSection := maxRunes / len(memorySectionTitles)
	extra := maxRunes % len(memorySectionTitles)
	quotas := make([]int, len(memorySectionTitles))
	for i := range quotas {
		quotas[i] = perSection
	}
	quotas[len(quotas)-1] += extra

	var sb strings.Builder
	for i, title := range memorySectionTitles {
		segStart := segStarts[i]
		segEnd := segStarts[i+1]
		// 段配额需扣掉标题的 rune 长度,避免标题被吞。
		titleRunes := len([]rune(title))
		contentQuota := quotas[i] - titleRunes
		if contentQuota < 0 {
			contentQuota = 0
		}
		// 标题按 byte 边界原样写出(中文标题 = 多 byte,segStart/contentStart
		// 也是 byte 偏移;len(title) 返回 byte 数)。
		sb.WriteString(md[segStart:segStart+len(title)])
		// 截断标题之后的内容(到下一段标题前为止)。
		content := md[segStart+len(title) : segEnd]
		r := []rune(content)
		if len(r) > contentQuota {
			content = string(r[:contentQuota]) + "\n…(本段过长已截断)"
		}
		sb.WriteString(content)
	}
	return sb.String()
}

// truncateHeadByRunes 把 md 按头部 rune 截断到 maxRunes,rune 边界安全。
// (TruncateMemoryBySections 的回退路径;等价于旧 InjectBlock 的截断行为。)
func truncateHeadByRunes(md string, maxRunes int) string {
	r := []rune(md)
	if len(r) <= maxRunes {
		return md
	}
	return string(r[:maxRunes]) + "\n…(记忆过长仅注入前部)"
}

// InjectBlock 把 memory_md 格式化成注入 user prompt 末尾的段落
// (【你的长期记忆(跨局积累)】头 + 尾注),并按 MemoryInjectMaxRunes 截断。
// §20260810-04 U4:从「头部 4000 rune 截断」改为「按段配额」截断
// (TruncateMemoryBySections),保证 4 段均有机会被注入,不再系统性丢失
// 第 4 段「决策策略迭代」;缺段(旧格式)回退头部截断。空输入返回 ""(不注入)。
//
// 2026-08-12 §20260812-04 U4:原 InjectBlock(md) 已删除 —— 生产路径全部改走
// InjectBlockForRole(按角色裁剪 + 难度档位预算),保留一个零调用点的公开
// 包装函数只会被 U6 wiring lint 判为死代码(而它判得对)。
//
// InjectBlockWithBudget 是所有注入路径的唯一实现点(角色感知路径经
// InjectBlockForRole 委托到这里)。
//
// maxRunes ≤ 0 时回退到 MemoryInjectMaxRunes 默认常量。
// 空输入(或裁剪后为空)返回 "" —— 调用方直接拼接即可,无需判空。
func InjectBlockWithBudget(md string, maxRunes int) string {
	md = strings.TrimSpace(md)
	if md == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = MemoryInjectMaxRunes
	}
	md = TruncateMemoryBySections(md, maxRunes)
	if strings.TrimSpace(md) == "" {
		return ""
	}
	return "\n\n【你的长期记忆（跨局积累）】\n" + md +
		"\n(以上是你过去多局的经验;本局信息以上方实时状态为准)"
}
