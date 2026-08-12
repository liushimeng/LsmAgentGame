package wwplayer

import (
	"strings"
	"testing"

	"LsmWebGame/agent/wwtypes"
)

// §20260812-04 U2 回归测试 —— user prompt 块预算与优先级降级。

// U2-01: 预算充足时全部块都保留，且顺序 = 传入顺序（认知顺序不可被打乱）。
func TestPromptBudget_U2_01_NoDropWhenBudgetAmple(t *testing.T) {
	blocks := []PromptBlock{
		{Name: "A", Priority: PriorityLow, Text: "\nAAA"},
		{Name: "B", Priority: PriorityCritical, Text: "\nBBB"},
		{Name: "C", Priority: PriorityMedium, Text: "\nCCC"},
	}
	got, dropped := AssembleWithBudget(blocks, 1000)
	if len(dropped) != 0 {
		t.Fatalf("预算充足不应丢块,dropped=%v", dropped)
	}
	if got != "\nAAA\nBBB\nCCC" {
		t.Fatalf("输出顺序必须等于传入顺序(认知顺序),got %q", got)
	}
}

// U2-02: 超预算时从**低优先级**开始丢，且高优先级必须存活。
func TestPromptBudget_U2_02_DropsLowestPriorityFirst(t *testing.T) {
	blocks := []PromptBlock{
		{Name: "低", Priority: PriorityLow, Text: "\n" + strings.Repeat("低", 100)},
		{Name: "高", Priority: PriorityHigh, Text: "\n" + strings.Repeat("高", 100)},
		{Name: "中", Priority: PriorityMedium, Text: "\n" + strings.Repeat("中", 100)},
	}
	// 只够放两个块（每块 101 runes）。
	got, dropped := AssembleWithBudget(blocks, 210)

	if len(dropped) != 1 || dropped[0] != "低" {
		t.Fatalf("应且仅应丢掉最低优先级的「低」,实际 dropped=%v", dropped)
	}
	if !strings.Contains(got, "高") || !strings.Contains(got, "中") {
		t.Fatalf("高/中优先级块必须存活\n%s", got)
	}
	if strings.Contains(got, strings.Repeat("低", 100)) {
		t.Fatalf("低优先级块应已被丢弃")
	}
}

// U2-03 ★: 丢弃必须留下可观测标记 —— 这是本项设计最重要的约束。
//
// 对照 TencentDB-Agent-Memory 的反面教材：L1 抽取失败返回 []，与「确实没啥可抽」
// 完全同形，静默劣化潜伏整整一个版本。降级绝不能静默。
func TestPromptBudget_U2_03_DropLeavesObservableMarker(t *testing.T) {
	blocks := []PromptBlock{
		{Name: "关键块", Priority: PriorityCritical, Text: "\n" + strings.Repeat("关", 50)},
		{Name: "流言", Priority: PriorityLow, Text: "\n" + strings.Repeat("流", 200)},
		{Name: "画像", Priority: PriorityLow, Text: "\n" + strings.Repeat("画", 200)},
	}
	got, dropped := AssembleWithBudget(blocks, 100)

	if len(dropped) == 0 {
		t.Fatal("该预算下必然有丢弃")
	}
	if !strings.Contains(got, "因上下文预算省略") {
		t.Fatalf("丢弃后必须留下可观测标记(否则与「本来就没有」不可区分)\n%s", got)
	}
	// 标记里要能看出丢了什么，便于线上排查。
	for _, name := range dropped {
		if !strings.Contains(got, name) {
			t.Fatalf("省略标记应列出被丢块名 %q\n%s", name, got)
		}
	}
}

// U2-04 ★: Critical 块即使超预算也绝不丢弃。
// 少了身份/私有信息/狼队留言，LLM 会做出**违规**决策，比信息缺失更糟。
func TestPromptBudget_U2_04_CriticalNeverDropped(t *testing.T) {
	blocks := []PromptBlock{
		{Name: "私有信息", Priority: PriorityCritical, Text: "\n" + strings.Repeat("私", 500)},
		{Name: "狼队留言", Priority: PriorityCritical, Text: "\n" + strings.Repeat("狼", 500)},
		{Name: "流言", Priority: PriorityLow, Text: "\n" + strings.Repeat("流", 10)},
	}
	got, dropped := AssembleWithBudget(blocks, 50) // 远小于 Critical 总量

	if !strings.Contains(got, strings.Repeat("私", 500)) {
		t.Fatal("Critical「私有信息」被丢弃 —— 这会让 AI 神职技能再次失效(P0-1 回归)")
	}
	if !strings.Contains(got, strings.Repeat("狼", 500)) {
		t.Fatal("Critical「狼队留言」被丢弃")
	}
	for _, d := range dropped {
		if d == "私有信息" || d == "狼队留言" {
			t.Fatalf("Critical 块不应出现在 dropped 列表:%v", dropped)
		}
	}
}

// U2-05: 空块被跳过，不占预算也不出现在输出里。
func TestPromptBudget_U2_05_EmptyBlocksSkipped(t *testing.T) {
	blocks := []PromptBlock{
		{Name: "空1", Priority: PriorityHigh, Text: ""},
		{Name: "空2", Priority: PriorityHigh, Text: "   \n  "},
		{Name: "实", Priority: PriorityLow, Text: "\n有内容"},
	}
	got, dropped := AssembleWithBudget(blocks, 1000)
	if len(dropped) != 0 {
		t.Fatalf("空块不应计入 dropped,%v", dropped)
	}
	if got != "\n有内容" {
		t.Fatalf("空块不应出现在输出,got %q", got)
	}
}

// U2-06: maxRunes ≤ 0 表示不限预算（灰度关闭开关），行为与旧代码一致。
func TestPromptBudget_U2_06_ZeroBudgetMeansUnlimited(t *testing.T) {
	blocks := []PromptBlock{
		{Name: "A", Priority: PriorityLow, Text: "\n" + strings.Repeat("A", 9999)},
		{Name: "B", Priority: PriorityLow, Text: "\nB"},
	}
	got, dropped := AssembleWithBudget(blocks, 0)
	if len(dropped) != 0 {
		t.Fatalf("maxRunes=0 应表示不限预算,却丢了 %v", dropped)
	}
	if !strings.Contains(got, "B") {
		t.Fatal("不限预算时所有块都应保留")
	}
}

// U2-07: 正常对局的 user prompt 不应触发裁剪 —— 护栏不能误伤日常路径。
func TestPromptBudget_U2_07_TypicalPromptNotTruncated(t *testing.T) {
	ctx := typicalGameContext()
	got := BuildUserPrompt(ctx)
	if strings.Contains(got, "因上下文预算省略") {
		t.Fatalf("常规 13 人局 prompt(%d runes)不该触发裁剪 —— 预算过紧会误伤",
			len([]rune(got)))
	}
}

// typicalGameContext 构造一个「典型 13 人局 Round 3 speak 阶段」的上下文，
// 用于验证预算护栏不会误伤常规路径。
func typicalGameContext() wwtypes.GameContext {
	ctx := wwtypes.GameContext{
		Round: 3, Phase: "speak", Role: "seer", MySeat: 0,
		MyTurn: true, SpeakTurn: 0, SeatCount: 13,
		MySeerCheck: 6, MySeerCheckFaction: "good",
		MySeerCheckHistory: []wwtypes.SeerCheckRecord{
			{Round: 1, Seat: 3, Faction: "wolf"},
			{Round: 2, Seat: 6, Faction: "good"},
		},
	}
	for i := 0; i < 13; i++ {
		ctx.AliveSeats = append(ctx.AliveSeats, i)
		ctx.AllPlayers = append(ctx.AllPlayers, wwtypes.PlayerBrief{
			Seat: i, Account: "player", IsBot: true, AgentName: "DouBao-model",
		})
	}
	return ctx
}
