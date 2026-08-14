// Package wwtypes — invariant.go: Agent 级 runtime invariant companion。
//
// 2026-08-13 §20260813-05 U2。借鉴 dsh invariant companion 模式
// (packages/llm/llm/src/invariant.ts:36-83 + packages/compaction/compaction-basic/src/invariant.ts
//  + AGENTS.md:103 "owned relationships")。
//
// 背景：
//
//	§130 "声明了却从不接线" 已反复复发 7 次(§20260811-08 / §20260812-04 /
//	§20260813-04 / §20260814-02 等)。CI 时 AST lint(§20260814-02 U6)能抓
//	"字段无 setter"，但抓不到"运行时字段值违反契约"——本文件补
//	runtime 持续护栏。
//
// 设计要点(对照 DSH invariant.ts):
//
//   - **声明"owned relationships"**(事件+数据)，不是断言存在性
//     (AGENTS.md:103 — DSH 教训)。
//   - **fail-loud + 持续可观测**：违反时 Debug 日志 + 原子计数器，
//     生产不 panic；CI/测试环境 panic(由 `agentcore.InvariantPanicInTests`
//     切换)。
//   - **3 组 12 条不变量**分三 API 返回 `[]InvariantViolation`，便于按组
//     单独部署/单独 disable。
//
// 与 §20260814-02 U6 lint 的分工:
//
//	U6 lint = 静态字段接线检查(CI 时跑一次)
//	本文件 invariant = 运行时数据契约检查(每次发请求前跑)
//	二者不可互替，共同根治 §130 第八次复发。
package wwtypes

import (
	"strconv"
	"sync"
	"sync/atomic"

	"LsmAgentGame/llm"
)

// itoa 局部封装 strconv.Itoa，避免引入 agent/wwplayer.itoa 导致循环依赖。
func itoa(n int) string {
	return strconv.Itoa(n)
}

// InvariantKind 标识不变量来源组，便于诊断 / 计数器分桶。
type InvariantKind string

const (
	InvariantKindContext       InvariantKind = "context"        // I1–I6 GameContext 字段契约
	InvariantKindMessagePairing InvariantKind = "message_pairing" // I7–I9 消息配对保护
	InvariantKindReconstruct   InvariantKind = "reconstruct"     // I10–I12 请求重建一致性
)

// InvariantViolation 是单条违反记录。Kind 区分组；Code 是稳定编号
// (用于 grep 关联测试用例)；Message 含字段路径便于线上排查。
type InvariantViolation struct {
	Kind    InvariantKind
	Code    string // "I1" / "I7" / "I10"
	Message string
}

// violationCounters 按 Code 分桶计数，atomic.Uint64 防并发竞争。
//
// 生产读取方式: `agentcore.InvariantViolationCount("I7")`。
// CI/测试读取方式: 同 + `t.Cleanup(func(){ assert counters["I7"] == 0 })`。
var violationCounters sync.Map // map[string]*uint64

func bumpCounter(code string) {
	v, _ := violationCounters.LoadOrStore(code, new(uint64))
	atomic.AddUint64(v.(*uint64), 1)
}

// InvariantViolationCount 返回某条不变量的累计违反次数。
// 返回 0 表示从未违反；供测试断言 / 监控埋点使用。
func InvariantViolationCount(code string) uint64 {
	v, ok := violationCounters.Load(code)
	if !ok {
		return 0
	}
	return atomic.LoadUint64(v.(*uint64))
}

// ResetInvariantViolationCounters 重置所有计数器。仅用于测试 setup/teardown。
func ResetInvariantViolationCounters() {
	violationCounters.Range(func(k, _ any) bool {
		violationCounters.Delete(k)
		return true
	})
}

// ---------------------------------------------------------------------------
// A 组：GameContext 字段契约（I1–I6）
// ---------------------------------------------------------------------------

// CheckGameContextInvariant 校验 GameContext 字段值的契约。
// 每次 buildAgentContextLocked 末尾调用。
//
// 6 条契约对照 §130 / §134 / §133 / §20260807-04 / §20260812-04:
//
//	I1: MySeerCheck != -1 ⇒ MySeerCheckFaction != ""
//	I2: WolfTarget != -1 ⇒ WitchAntidoteUsed 与 WolfTarget 状态一致
//	I3: WolfTeammateSeat >= 0 ⇒ Role == "werewolf"
//	I4: WolfPackSnapshot 非空 ⇒ Faction == "wolf"
//	I5: HumanDebuff 非空 ⇒ !IsBot
//	I6: MySeerCheckHistory 长度 ≤ Round + 1
func CheckGameContextInvariant(gc *GameContext) []InvariantViolation {
	if gc == nil {
		return nil
	}
	var out []InvariantViolation

	// I1: 预言家身份时,MySeerCheckFaction 必须有值(查过人或未查都要明示)
//    适配说明:MySeerCheck 座位号可以为 0(查的 0 号玩家),所以不能用
//    `!= -1` 判断;改用 Role 识别。
	if gc.Role == "seer" && gc.MySeerCheckFaction == "" {
		out = append(out, InvariantViolation{
			Kind:    InvariantKindContext,
			Code:    "I1",
			Message: "Role=seer 但 MySeerCheckFaction 为空 (§20260812-04 P0-1 预言家永远要有 faction)",
		})
	}

	// I2: WolfTarget 与 Witch 解药状态一致
	//    WolfTarget != -1 表示狼已刀 → 如果 WitchAntidoteUsed==true，则
	//    WitchSavedTarget 必须 == WolfTarget(否则解药救了别人)。
	//    适配说明:GameContext 无 WitchSavedTarget 字段(权威态在 engine.GameState),
	//    改为校验 gc.WitchAntidoteUsed==true 时 WolfTarget 不能等于 -1
	//    (否则就是「解药救了空气」)。
	if gc.WitchAntidoteUsed && gc.WolfTarget == -1 {
		out = append(out, InvariantViolation{
			Kind:    InvariantKindContext,
			Code:    "I2",
			Message: "WitchAntidoteUsed=true 但 WolfTarget=-1 (解药救了空气,§134 守卫/女巫)",
		})
	}

	// I3: WolfTeammateSeat ⇒ Role == "werewolf"
//    适配说明:WolfTeammateSeat=0 是合法值(队友是 0 号玩家),所以用 != -1 判断;
//    但 -1 是初始化值,生产路径会赋 0+ 真实值。改用 Role 识别。
	if gc.WolfTeammateSeat != -1 && gc.Role != "werewolf" {
		out = append(out, InvariantViolation{
			Kind:    InvariantKindContext,
			Code:    "I3",
			Message: "WolfTeammateSeat=" + itoa(gc.WolfTeammateSeat) + " 但 Role=" + gc.Role + " ≠ werewolf (§133)",
		})
	}

	// I4: WolfPackSnapshot 非空 ⇒ Faction == "wolf"
	if len(gc.WolfPackSnapshot) > 0 && gc.Faction != "wolf" {
		out = append(out, InvariantViolation{
			Kind:    InvariantKindContext,
			Code:    "I4",
			Message: "WolfPackSnapshot 非空但 Faction=" + gc.Faction + " ≠ wolf (§133 协议层隔离)",
		})
	}

	// I5: HumanDebuff 非空 ⇒ 目标座位是真人(非 bot)
	//    适配说明:GameContext 无 IsBot 字段,通过 Static.AllPlayers[MySeat].IsBot 判断。
	if gc.HumanDebuff != nil && gc.Static != nil {
		mySeat := gc.Static.MySeat
		if mySeat >= 0 && mySeat < len(gc.Static.AllPlayers) &&
			gc.Static.AllPlayers[mySeat].IsBot {
			out = append(out, InvariantViolation{
				Kind:    InvariantKindContext,
				Code:    "I5",
				Message: "HumanDebuff 非空但本座位 IsBot=true (§20260807-04 人类反制 debuff 仅人类触发)",
			})
		}
	}

	// I6: MySeerCheckHistory 长度 ≤ Round + 1
	//    守卫/预言家每晚至多查验 1 人；Round=0(尚未开局)允许空。
	if len(gc.MySeerCheckHistory) > gc.Round+1 {
		out = append(out, InvariantViolation{
			Kind:    InvariantKindContext,
			Code:    "I6",
			Message: "MySeerCheckHistory 长度=" + itoa(len(gc.MySeerCheckHistory)) + " > Round+1=" + itoa(gc.Round+1),
		})
	}

	if len(out) > 0 {
		for _, v := range out {
			bumpCounter(v.Code)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// B 组：消息配对保护（I7–I9）
// ---------------------------------------------------------------------------

// CheckMessagePairingInvariant 校验消息序列的配对与连续性。
// 在发请求前 + 处理 LLM 响应后各调用一次。
//
//	I7: tool_use ↔ tool_result 1:1 配对
//	I8: role=user 不可连续 2 条
//	I9: tool_use.input == nil 必须空对象 {} (对照 §71a)
func CheckMessagePairingInvariant(msgs []llm.Message) []InvariantViolation {
	if len(msgs) == 0 {
		return nil
	}
	var out []InvariantViolation

	// I8: role=user 不可连续 2 条
	prevUser := false
	for i, m := range msgs {
		if m.Role == "user" && prevUser {
			out = append(out, InvariantViolation{
				Kind:    InvariantKindMessagePairing,
				Code:    "I8",
				Message: "msg[" + itoa(i) + "]: 连续 2 条 role=user (§14.1)",
			})
		}
		prevUser = m.Role == "user"
	}

	// I7: tool_use ↔ tool_result 配对（仅检查 assistant 块的 tool_use 与后续 user 块的 tool_result）
	// 收集所有未配对的 tool_use id
	pendingUseIDs := make(map[string]struct{})
	for _, m := range msgs {
		for _, cb := range m.Content {
			switch cb.Type {
			case "tool_use":
				if cb.ID != "" {
					pendingUseIDs[cb.ID] = struct{}{}
				}
			case "tool_result":
				if cb.ToolUseID != "" {
					delete(pendingUseIDs, cb.ToolUseID)
				}
			}
		}
	}
	if len(pendingUseIDs) > 0 {
		out = append(out, InvariantViolation{
			Kind:    InvariantKindMessagePairing,
			Code:    "I7",
			Message: "未配对的 tool_use ids: " + mapKeysString(pendingUseIDs) + " (§82b)",
		})
	}

	// I9: tool_use.input == nil 时 MarshalJSON 会输出 "input":null，
	//     §71a 要求统一为空对象 {}。检测到 nil 即报错(便于运行时立刻修复)。
	for i, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for j, cb := range m.Content {
			if cb.Type == "tool_use" && cb.Input == nil {
				out = append(out, InvariantViolation{
					Kind:    InvariantKindMessagePairing,
					Code:    "I9",
					Message: "msg[" + itoa(i) + "].content[" + itoa(j) + "]: tool_use.input == nil (§71a)",
				})
			}
		}
	}

	if len(out) > 0 {
		for _, v := range out {
			bumpCounter(v.Code)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// C 组：请求重建一致性（I10–I12）
// ---------------------------------------------------------------------------

// CheckRequestReconstructabilityInvariant 校验 LLMRequest 与底层状态的一致性。
// 在 anthropic.Provider.Chat 真正发出前调用。
//
//	I10: req.Messages 字节数 ≈ Memory.TotalPayloadBytes() 估算
//	I11: system[].Text 字节与 SystemPromptBytes 一致（U5 引入）
//	I12: req.AgentClassName != ""（§24）
//
// 参数：
//   - req: 待发出的 LLMRequest
//   - memoryBytes: 来自 Memory.TotalPayloadBytes() 的字节估算
//   - systemPromptBytes: 来自 Agent.SystemPromptBytes 的冻结字节(U5 字段，nil = 未启用)
func CheckRequestReconstructabilityInvariant(req llm.LLMRequest, memoryBytes int, systemPromptBytes []byte) []InvariantViolation {
	var out []InvariantViolation

	// I10: req.Messages 字节数 ≈ Memory.TotalPayloadBytes() 估算
	//     容忍 ±50%(Memory.TotalPayloadBytes 含 system+tools+metadata,req.Messages 仅消息;
//     §20260810-14 已加总,二者基线不同;50% 是工程经验值,过严会触发噪声)。
	if memoryBytes > 0 {
		reqBytes := approxRequestMessagesBytes(req.Messages)
		delta := reqBytes - memoryBytes
		if delta < 0 {
			delta = -delta
		}
		threshold := memoryBytes * 50 / 100
		if threshold < 512 {
			threshold = 512 // 极小 payload 兜底
		}
		if delta > threshold {
			out = append(out, InvariantViolation{
				Kind:    InvariantKindReconstruct,
				Code:    "I10",
				Message: "req.Messages 字节=" + itoa(reqBytes) + " 与 Memory 估算=" + itoa(memoryBytes) + " 偏差 > 50%(§20260813-04 U6)",
			})
		}
	}

	// I11: system bytes 一致性
	if len(systemPromptBytes) > 0 {
		var sysBytes int
		for _, sb := range req.System {
			sysBytes += len(sb.Text)
		}
		if sysBytes != len(systemPromptBytes) {
			out = append(out, InvariantViolation{
				Kind:    InvariantKindReconstruct,
				Code:    "I11",
				Message: "req.System bytes=" + itoa(sysBytes) + " ≠ SystemPromptBytes=" + itoa(len(systemPromptBytes)) + " (§20260813-05 U5 字节稳定)",
			})
		}
	}

	// I12: AgentClassName != ""
	if req.AgentClassName == "" {
		out = append(out, InvariantViolation{
			Kind:    InvariantKindReconstruct,
			Code:    "I12",
			Message: "req.AgentClassName 为空(§24)",
		})
	}

	if len(out) > 0 {
		for _, v := range out {
			bumpCounter(v.Code)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 内部工具
// ---------------------------------------------------------------------------

func approxRequestMessagesBytes(msgs []llm.Message) int {
	var total int
	for _, m := range msgs {
		total += len(m.Role) + 16 // block overhead
		for _, cb := range m.Content {
			switch cb.Type {
			case "text":
				total += len(cb.Text)
			case "tool_use":
				total += len(cb.ID) + len(cb.Name) + len(cb.ToolUseID) + 32
			case "tool_result":
				total += len(cb.ToolUseID) + 16
				for _, inner := range cb.Content {
					total += len(inner.Text) + 8
				}
			default:
				total += 16
			}
		}
	}
	return total
}

func mapKeysString(m map[string]struct{}) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ","
		}
		out += k
	}
	return out
}