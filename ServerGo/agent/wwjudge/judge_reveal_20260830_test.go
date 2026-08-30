// Package wwjudge — judge_reveal_20260830_test.go: §20260830-01「死亡亮身份」
// 法官侧 Agent 单测(设计文档 §10.2 J-02 + §5.2~§5.4)。
//
// 覆盖:
//
//	J-02  BuildJudgeSystemPrompt 双模式:开启含 ❹b + 「身份是〔角色名〕」格式指引
//	      且黎明/死亡宣告指令引用 revealed_dead_roles;关闭含「严禁出现任何角色名」
//	      且黎明/死亡宣告指令与旧版逐字一致(零回归)。
//	J-03  BuildJudgeUserPrompt:开启渲染 revealed_dead_roles 权威清单(1-indexed);
//	      关闭不渲染。
//	J-04  JudgeFallbackTextWithSnapshot 双模式:开启时 dawn/death fallback 由服务端
//	      拼装「已公开身份」段(公平性不因 LLM 故障塌);关闭与旧版输出一致。
//	J-05  declare_cause 工具描述含双模式说明。
package wwjudge

import (
	"strings"
	"testing"
)

// revealTestSnapshot 构造带已公开死者身份事实的法官快照。
func revealTestSnapshot(reveal bool) GameSnapshot {
	snap := GameSnapshot{
		Day:               2,
		AliveSeats:        []int{0, 1, 2, 3, 4, 5, 6, 7, 8},
		DeadSeats:         []int{9, 10, 11, 12},
		RevealRoleOnDeath: reveal,
	}
	if reveal {
		snap.RevealedDeadRoles = []DeadRoleFact{
			{Seat: 9, Role: "hunter", Cause: "wolf", Verdict: "death"},
			{Seat: 11, Role: "werewolf", Cause: "vote", Verdict: "execution"},
		}
	}
	return snap
}

// TestJudge_J02_SystemPromptDualMode 系统 prompt 双模式断言(§5.2)。
func TestJudge_J02_SystemPromptDualMode(t *testing.T) {
	// ── 开启 ──
	open := BuildJudgeSystemPrompt(JudgePendingDawnAnnounce, revealTestSnapshot(true))
	openText := open[0].Text
	for _, want := range []string{
		"❹b",                                  // ❹b 死亡亮身份块注入
		"身份是〔角色名〕",                      // 统一宣告格式
		"revealed_dead_roles",                  // 权威清单引用
		"禁止编造未在清单中的身份",               // 严禁凭空猜测
		"昨夜 X 号死亡(死因),身份是〔角色名〕", // 黎明宣告指令带身份
	} {
		if !strings.Contains(openText, want) {
			t.Errorf("开启模式 system prompt 缺少 %q:\n%s", want, openText)
		}
	}
	// 死亡宣告指令双模式:开启要求 declare_cause text 含身份。
	openDeath := BuildJudgeSystemPrompt(JudgePendingDeathAnnounce, revealTestSnapshot(true))
	if !strings.Contains(openDeath[0].Text, "并当场公布死者身份") {
		t.Errorf("开启模式死亡宣告指令缺身份要求:\n%s", openDeath[0].Text)
	}

	// ── 关闭(零回归) ──
	closed := BuildJudgeSystemPrompt(JudgePendingDawnAnnounce, revealTestSnapshot(false))
	closedText := closed[0].Text
	if !strings.Contains(closedText, "❹c") {
		t.Errorf("关闭模式 system prompt 缺少 ❹c 禁令块:\n%s", closedText)
	}
	if !strings.Contains(closedText, "严禁在宣告中出现任何角色名") {
		t.Errorf("关闭模式 system prompt 缺少「严禁出现角色名」:\n%s", closedText)
	}
	// 关闭模式黎明/死亡宣告指令保持现行措辞(不得出现身份指引)。
	if !strings.Contains(closedText, "昨夜 X 号 / Y 号死亡") {
		t.Errorf("关闭模式黎明宣告指令措辞改变:\n%s", closedText)
	}
	if strings.Contains(closedText, "身份是〔角色名〕") {
		t.Errorf("关闭模式黎明宣告指令不得出现身份格式:\n%s", closedText)
	}
	closedDeath := BuildJudgeSystemPrompt(JudgePendingDeathAnnounce, revealTestSnapshot(false))
	if got := closedDeath[0].Text; !strings.Contains(got, `用"处决"或"死亡"区分语义(主动投票=处决;夜间=死亡)。`) ||
		strings.Contains(got, "当场公布死者身份") {
		t.Errorf("关闭模式死亡宣告指令应与旧版一致且无身份要求:\n%s", got)
	}
}

// TestJudge_J03_UserPromptRevealedDeadRoles user prompt 权威清单渲染(§5.2)。
func TestJudge_J03_UserPromptRevealedDeadRoles(t *testing.T) {
	open := BuildJudgeUserPrompt(JudgePendingDawnAnnounce, revealTestSnapshot(true))
	if !strings.Contains(open, "revealed_dead_roles(已公开死者身份,宣告可直接引用):") {
		t.Errorf("开启模式 user prompt 缺少 revealed_dead_roles 清单:\n%s", open)
	}
	// 座位 1-indexed + 角色 + verdict/cause。
	if !strings.Contains(open, "10号=hunter(death/wolf)") || !strings.Contains(open, "12号=werewolf(execution/vote)") {
		t.Errorf("revealed_dead_roles 条目格式不符:\n%s", open)
	}

	closed := BuildJudgeUserPrompt(JudgePendingDawnAnnounce, revealTestSnapshot(false))
	if strings.Contains(closed, "revealed_dead_roles") {
		t.Errorf("关闭模式 user prompt 不得渲染已公开身份清单(零改动):\n%s", closed)
	}
	// 开启但清单为空(理论不应发生):不渲染空清单行。
	emptyOpen := BuildJudgeUserPrompt(JudgePendingDawnAnnounce, GameSnapshot{RevealRoleOnDeath: true})
	if strings.Contains(emptyOpen, "revealed_dead_roles") {
		t.Errorf("空清单不应渲染 revealed_dead_roles 行:\n%s", emptyOpen)
	}
}

// TestJudge_J04_FallbackDualMode fallback 双模式(§5.4)。
func TestJudge_J04_FallbackDualMode(t *testing.T) {
	// 开启:dawn/death fallback 带服务端拼装的「已公开身份」段(中文角色名)。
	for _, kind := range []string{JudgePendingDawnAnnounce, JudgePendingDeathAnnounce} {
		got := JudgeFallbackTextWithSnapshot(kind, revealTestSnapshot(true))
		if !strings.Contains(got, "已公开身份:") {
			t.Errorf("开启模式 %s fallback 缺身份段: %q", kind, got)
		}
		if !strings.Contains(got, "10号·猎人") || !strings.Contains(got, "12号·狼人") {
			t.Errorf("开启模式 %s fallback 身份条目不符: %q", kind, got)
		}
	}
	// 开启:非 dawn/death 类 fallback 不追加身份段(阶段宣告与死亡无关)。
	if got := JudgeFallbackTextWithSnapshot(JudgePendingSpeakStart, revealTestSnapshot(true)); strings.Contains(got, "已公开身份") {
		t.Errorf("speak_start fallback 不应带身份段: %q", got)
	}
	// 关闭:即使快照带白名单死者(RevealedDeadRoles 在关闭模式下仅白名单命中者),
	// fallback 也不追加任何角色名 —— 与旧版输出完全一致。
	closedSnap := revealTestSnapshot(false)
	closedSnap.RevealedDeadRoles = []DeadRoleFact{{Seat: 9, Role: "hunter", Cause: "wolf", Verdict: "death"}}
	want := JudgeFallbackTextWithSnapshot(JudgePendingDawnAnnounce, GameSnapshot{
		Day: closedSnap.Day, AliveSeats: closedSnap.AliveSeats, DeadSeats: closedSnap.DeadSeats,
	})
	if got := JudgeFallbackTextWithSnapshot(JudgePendingDawnAnnounce, closedSnap); got != want {
		t.Errorf("关闭模式 fallback 应与旧版一致: got %q, want %q", got, want)
	}
	// 开启:超过 3 条截断为「等N人」,保证 fallback 文案长度可控。
	many := GameSnapshot{
		Day: 3, AliveSeats: []int{0}, DeadSeats: []int{1, 2, 3, 4, 5},
		RevealRoleOnDeath: true,
		RevealedDeadRoles: []DeadRoleFact{
			{Seat: 1, Role: "villager", Cause: "wolf", Verdict: "death"},
			{Seat: 2, Role: "villager", Cause: "wolf", Verdict: "death"},
			{Seat: 3, Role: "villager", Cause: "wolf", Verdict: "death"},
			{Seat: 4, Role: "seer", Cause: "wolf", Verdict: "death"},
		},
	}
	if got := JudgeFallbackTextWithSnapshot(JudgePendingDeathAnnounce, many); !strings.Contains(got, "等1人") {
		t.Errorf("超 3 条应截断为「等N人」: %q", got)
	}
}

// TestJudge_J05_DeclareCauseToolDualModeDescription declare_cause 工具描述双模式说明(§5.3)。
func TestJudge_J05_DeclareCauseToolDualModeDescription(t *testing.T) {
	var desc string
	for _, tool := range BuildJudgeTools() {
		if tool.Name == "declare_cause" {
			desc = tool.Description
		}
	}
	if desc == "" {
		t.Fatal("declare_cause 工具未找到")
	}
	if !strings.Contains(desc, "reveal_role_on_death") {
		t.Errorf("declare_cause 描述缺少双模式开关说明:\n%s", desc)
	}
	if !strings.Contains(desc, "身份是〔角色名〕") {
		t.Errorf("declare_cause 描述缺少开启模式格式:\n%s", desc)
	}
	if !strings.Contains(desc, "严禁在 text 中出现任何角色名") {
		t.Errorf("declare_cause 描述缺少关闭模式禁令:\n%s", desc)
	}
}
