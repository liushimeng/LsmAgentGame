// Package wwplayer — 长期记忆的按角色选取（注入侧）。
//
// 2026-08-12 §20260812-04 U4 新增。
//
// # 对照 TencentDB-Agent-Memory 的差距
//
// 参考项目对四层记忆采用**差异化注入**：L3 全文直注、L2 只注 path+summary、
// L1/L0 完全不注入而是暴露检索工具。核心判断是「可访问 ≠ 必须注入」。
//
// 本仓库的跨局记忆（§131 MEMORY.md）此前是**一律全量注入**：
// `run.go:664` 无条件 `InjectBlock(a.MemoryMD)`，4000 runes（≈3200 token）
// 每次 LLM 调用全量重发。13 人局单 bot 一局 50+ 次调用 = 约 16 万 token 纯重复。
//
// 更关键的是**记忆按 model_key 存储、不按角色隔离**：同一模型坐预言家时学到的
// 教训，坐狼人时照样被全量注入。「角色差异化学习」此前只靠迭代 prompt 里
// 一句「本局角色 X」软约束，注入层零隔离。
//
// # 本文件做什么
//
// 不引入 embedding（13 人局记忆总量 <100KB，向量检索过重），改为
// **结构化分段 + 按角色选取**：识别 `### 作为<角色>` 子段，注入时只保留
// 「通用内容 + 当前角色子段」，其它角色的子段整段跳过。
//
// 旧格式（无子段）完全不受影响 —— 原样走既有的 TruncateMemoryBySections 路径。
package wwplayer

import (
	"strings"

	"LsmWebGame/agent/wwtypes"
)

// roleSubsectionPrefix 是角色子段的标题前缀。
// 迭代 prompt 会引导 LLM 在「## 我的失误与教训」下按此格式分组。
const roleSubsectionPrefix = "### 作为"

// roleDisplayName 把引擎 role 字符串映射为记忆里使用的中文角色名。
//
// 只覆盖 godRolePool 实际可发牌的角色 + 狼人 + 平民（§134：进卡池的角色
// 要么完整实现要么移出卡池）。未知 role 返回 ""，此时不做角色过滤（保守：
// 宁可多注入也不要误删）。
func roleDisplayName(role string) string {
	switch role {
	case "werewolf":
		return "狼人"
	case "seer":
		return "预言家"
	case "witch":
		return "女巫"
	case "hunter":
		return "猎人"
	case "idiot":
		return "白痴"
	case "guard":
		return "守卫"
	case "knight":
		return "骑士"
	case "demon_hunter":
		return "猎魔人"
	case "villager":
		return "平民"
	default:
		return ""
	}
}

// SelectMemoryForRole 从完整 MEMORY.md 中挑出与当前角色相关的部分。
//
//   - role 为空或未知 → 原样返回（不过滤）
//   - 记忆中不含任何 `### 作为X` 子段（旧格式）→ 原样返回
//   - 否则：保留所有非角色子段的内容 + 仅当前角色的子段
//
// 返回值仍是 Markdown，交由 InjectBlock 做后续的按段配额截断。
// 本函数**不做长度截断** —— 职责单一，长度归 InjectBlock 管。
func SelectMemoryForRole(md, role string) string {
	md = strings.TrimSpace(md)
	if md == "" {
		return ""
	}
	want := roleDisplayName(role)
	if want == "" || !strings.Contains(md, roleSubsectionPrefix) {
		return md
	}

	var out []string
	// skipping=true 表示当前正处于「别的角色」的子段内，逐行丢弃。
	skipping := false
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, roleSubsectionPrefix) {
			// 命中角色子段标题：判断是不是我要的那个。
			skipping = !strings.HasPrefix(trimmed, roleSubsectionPrefix+want)
			if skipping {
				continue
			}
			out = append(out, line)
			continue
		}
		// 二级标题（## ）结束任何角色子段的作用域。
		if strings.HasPrefix(trimmed, "## ") {
			skipping = false
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// InjectBlockForRole 是 InjectBlock 的角色感知版本。
//
// 先按角色裁剪，再按 maxRunes 做分段配额截断。maxRunes ≤ 0 时回退到
// MemoryInjectMaxRunes 常量 —— 这样调用方可以接入难度档位配置
//（difficulty.MemoryInjectRunes，此前 4 处赋值 0 处读取的死配置）。
// 角色裁剪后复用 InjectBlockWithBudget 做截断与包装，保证两条路径
// (角色感知 / 旧全量) 的输出格式逐字一致。
func InjectBlockForRole(md, role string, maxRunes int) string {
	return InjectBlockWithBudget(SelectMemoryForRole(md, role), maxRunes)
}

// InjectBlockForContext 是给 run.go 用的便捷入口:从 GameContext 取角色。
func InjectBlockForContext(md string, ctx *wwtypes.GameContext, maxRunes int) string {
	role := ""
	if ctx != nil {
		role = ctx.Role
	}
	return InjectBlockForRole(md, role, maxRunes)
}
