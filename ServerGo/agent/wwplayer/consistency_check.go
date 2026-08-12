// Package agent — consistency_check.go: Agent 行为一致性校验 (§20260811-06 U4)。
//
// 设计动机:
//   - LLM Agent 可能在不同轮次对同一玩家身份声明自相矛盾(第 1 轮认平民 →
//     第 2 轮跳预言家),影响游戏公平性。
//   - speak_factcheck.go 只校验已死亡玩家引用(§79 教训),不校验身份声明一致性。
//   - 本模块新增 3 类规则,纯规则(不走 LLM),实时检测 + 写 LastConsistencyCheck。
//
// §120 公平性:校验不计入 consecutiveFailures,不触发 quarantine(误判零成本)。
// §128 对话即思考:校验结果写 LastConsistencyCheck(新增字段),不新建独立决策字段。
// §130 接线验证:consistency_check.go::RunCheckLocked 在 speak 工具 dispatch 之后
// 调用,BotTranscript.LastConsistencyCheck 是后端唯一写入点。
package wwplayer

import (
	"strings"
	"sync"
	"time"
)

// consistencyCheckMaxEntries BotTranscript 保留最近多少条 RoleClaims 用于
// 跨 round 比对。30 条 ≈ 30 轮,够覆盖一局典型 13 人局。
const consistencyCheckMaxEntries = 30

// RoleClaim 是 LLM 在某轮声明的身份。本模块按 (round, claim) 二元组记录。
// claim 是 LLM 自由文本中提取的"我/他是 X"声明;空 = 未声明。
type RoleClaim struct {
	Round     int    `json:"round"`
	Claim     string `json:"claim"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// ConsistencyCheckResult 单次校验结果(§20260811-06 U4)。
//
// 字段语义:
//   - Rule: 触发的规则代码("R1" / "R2" / "R3" / "OK");
//   - Severity: high / medium / low / none;
//   - Detail: 校验描述(LLM 友好,prompt 末尾 ⚠️ 块直接展示);
//   - Claims: 本次校验涉及的 RoleClaim 列表(空表示无);
type ConsistencyCheckResult struct {
	Rule     string      `json:"rule"`
	Severity string      `json:"severity"`
	Detail   string      `json:"detail"`
	Claims   []RoleClaim `json:"claims,omitempty"`
}

// BotTranscript 一致性相关字段(在 agent.go 中由 AppendRoleClaim /
// SetLastConsistencyCheck 操作)。本文件仅做引用 + helper 实现。
// 实际字段定义见 agent.go BotTranscript struct。

// 角色声明关键字(LLM 中文发言里"我是 X"/"我认 X"等常见模式)。
// §20260811-06 U4 — 关键词识别。LLM 自由文本中粗略定位"我/他是 X"声明。
// 误判容忍(只用作一致性检测,真正身份由服务端 Roster 字段决定)。
var (
	roleKeywordsZh = []string{
		"我是预言家", "我认预言家", "跳预言家", "报预言家",
		"我是女巫", "我认女巫", "跳女巫", "报女巫",
		"我是猎人", "跳猎人", "报猎人",
		"我是守卫", "我认守卫", "跳守卫", "报守卫",
		"我是骑士", "跳骑士", "报骑士",
		"我是村民", "我是平民", "我是老百姓", "好人阵营",
		"我是狼人", "我认狼人", "悍跳", "自爆",
	}
	roleKeywordsEn = []string{
		"i am the seer", "i'm the seer", "i claim seer", "i am seer",
		"i am the witch", "i claim witch",
		"i am the hunter", "i claim hunter",
		"i am the guard", "i claim guard",
		"i am the knight", "i claim knight",
		"i am the villager", "i am villager", "i am a villager",
		"i am a werewolf", "i am werewolf", "i claim werewolf",
	}
)

// extractRoleClaim 从单次发言文本里抽取"我是 X"声明。
// 简单实现:遍历关键词列表,第一个命中即返回。无命中返回 ""。
// §20260811-06 U4 — 关键词粗匹配,误判由 RunCheckLocked 二次过滤。
func extractRoleClaim(text string) string {
	if text == "" {
		return ""
	}
	low := strings.ToLower(text)
	for _, kw := range roleKeywordsZh {
		if strings.Contains(text, kw) {
			return kw
		}
	}
	for _, kw := range roleKeywordsEn {
		if strings.Contains(low, kw) {
			return kw
		}
	}
	return ""
}

// AppendRoleClaim 追加一条 RoleClaim 到 BotTranscript.RoleClaims。已持 a.mu。
//
// FIFO 上限 30 条;空 claim 直接 return。
func (a *Agent) AppendRoleClaim(round int, claim string) {
	if claim == "" {
		return
	}
	if a.lastTranscript == nil {
		return
	}
	entry := RoleClaim{Round: round, Claim: claim, CreatedAt: time.Now().UnixMilli()}
	claims := append(a.lastTranscript.RoleClaims, entry)
	if len(claims) > consistencyCheckMaxEntries {
		claims = claims[len(claims)-consistencyCheckMaxEntries:]
	}
	a.lastTranscript.RoleClaims = claims
}

// SetLastConsistencyCheck 写入最近一次校验结果(空表示清空)。已持 a.mu。
func (a *Agent) SetLastConsistencyCheck(result ConsistencyCheckResult) {
	if a.lastTranscript == nil {
		return
	}
	if result.Rule == "OK" || result.Rule == "" {
		a.lastTranscript.LastConsistencyCheck = nil
		return
	}
	a.lastTranscript.LastConsistencyCheck = &result
}

// runConsistencyCheckLocked 是内部一致性检测入口(已持 a.mu)。
// 3 类规则:
//   R1 — 同 round 内身份反复跳变(high)
//   R2 — 平民跳神 / 跨 round 身份由弱变强(medium)
//   R3 — 投票自相矛盾(low,仅日志)
//
// 返回第一条命中的高严重度结果;无命中返回 OK。
// 实际 RunCheckLocked 由调用方在 speak 工具 dispatch 之后触发。
func runConsistencyCheckLocked(a *Agent) ConsistencyCheckResult {
	if a == nil || a.lastTranscript == nil {
		return ConsistencyCheckResult{Rule: "OK"}
	}
	claims := a.lastTranscript.RoleClaims
	if len(claims) < 2 {
		return ConsistencyCheckResult{Rule: "OK"}
	}
	// R1: 同 round 出现 ≥2 个不同身份声明 → 反复跳变。
	byRound := make(map[int][]string)
	for _, c := range claims {
		if c.Round <= 0 {
			continue
		}
		byRound[c.Round] = append(byRound[c.Round], c.Claim)
	}
	for round, list := range byRound {
		uniq := uniqueStrings(list)
		if len(uniq) >= 2 {
			return ConsistencyCheckResult{
				Rule:     "R1",
				Severity: "high",
				Detail:   "本局第 " + itoa(round) + " 轮内出现 " + itoa(len(uniq)) + " 个不同身份声明,可能反复跳变",
				Claims:   filterClaimsByRound(claims, round),
			}
		}
	}
	// R2: 跨 round 身份由"村民/平民" → "神职"(悍跳 / 反向悍跳)。
	for i := 1; i < len(claims); i++ {
		prev := claims[i-1]
		curr := claims[i]
		if prev.Round == curr.Round {
			continue
		}
		if isVillagerClaim(prev.Claim) && isMysticClaim(curr.Claim) {
			return ConsistencyCheckResult{
				Rule:     "R2",
				Severity: "medium",
				Detail:   "第 " + itoa(prev.Round) + " 轮声明「" + prev.Claim + "」,第 " + itoa(curr.Round) + " 轮改为「" + curr.Claim + "」,请确认是否合理",
				Claims:   []RoleClaim{prev, curr},
			}
		}
	}
	return ConsistencyCheckResult{Rule: "OK"}
}

func uniqueStrings(xs []string) []string {
	seen := make(map[string]struct{}, len(xs))
	out := make([]string, 0, len(xs))
	for _, s := range xs {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func isVillagerClaim(claim string) bool {
	vs := []string{"我是村民", "我是平民", "我是老百姓", "好人阵营", "i am villager", "i am a villager", "i am the villager"}
	for _, v := range vs {
		if strings.Contains(claim, v) {
			return true
		}
	}
	return false
}

func isMysticClaim(claim string) bool {
	// 排除"我是狼人"自身;只关心"村民 → 神职"跳变。
	ms := []string{"预言家", "女巫", "守卫", "骑士", "猎人", "seer", "witch", "guard", "knight", "hunter"}
	for _, m := range ms {
		if strings.Contains(claim, m) {
			return true
		}
	}
	return false
}

func filterClaimsByRound(claims []RoleClaim, round int) []RoleClaim {
	out := make([]RoleClaim, 0, 4)
	for _, c := range claims {
		if c.Round == round {
			out = append(out, c)
		}
	}
	return out
}

// consistencyCheckOnce 保证 RunCheckLocked 内部并发安全(纯 LLM 串行 dispatch,
// 但保留锁便于 future 扩展)。
var consistencyCheckOnce sync.Mutex
