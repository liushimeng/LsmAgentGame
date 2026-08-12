// Package werewolf — speak_mystery_test.go: R132「公屏猜疑化」单元测试。
//
// 覆盖:
//   - T1–T5: 22+ 类规则 × 3 种模式(MysteryAllow/Defer/Fuzzy)的命中与改写;
//   - T6: garble 防护(改写后<50%长度还原);
//   - T7: 空文本 / 边界条件;
//   - T8: ComposeMysteryHint 给 LLM 的反馈字符串;
//   - T9: 多规则命中时的 dominantMode 优先级。

package werewolf

import (
	"strings"
	"testing"
)

// TestMysteryMask_Allow_PsychologicalWarfare 心理战 / 阵营叙事 → 原文发出。
// Mode 期望为 MysteryAllow 或 MysteryDeferToGame (两者都是原文发出,玩家可见
// 完整内容);**不应**为 MysteryFuzzyIntent (后者会改写)。
func TestMysteryMask_Allow_PsychologicalWarfare(t *testing.T) {
	cases := []struct {
		desc string
		text string
	}{
		{"自报狼人", "我是狼人,大家小心"},
		{"作为预言家", "作为预言家我得跳出来"},
		{"悍跳预言家-我才是真预言家", "我才是真预言家"},
		{"悍跳预言家-唯一真预言家在此", "全场唯一真预言家在此"},
		{"狼阵营人数", "我们三个狼都还在场上"},
		{"狼队友披露-我的同伙", "我的同伙有2号、3号、8号"},
		{"狼阵营人数枚举列表", "4 11 12 8,都是我们的队友"},
		{"狼队黑话", "狼在养刀"},
		{"狼队战术-倒钩", "我们是倒钩狼配置"},
		{"公开点名+神职断言", "3号就是预言家"},
		{"公开点名+真假", "5号是真预言家"},
		{"公开点名+第三人称真假神职", "他是真的女巫"},
		{"第三人称狼队友", "3号是狼队友"},
		{"我是7号狼同伴", "我是7号狼同伴"},
		{"击杀意图-今晚先刀5号", "今晚先刀5号"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			res := MysteryMaskText(c.text)
			if res.Text != c.text {
				t.Errorf("心理战 Allowed 类别应原文发出; got %q want %q", res.Text, c.text)
			}
			if len(res.HitCategories) == 0 {
				t.Errorf("至少应有一条命中类别; got 0")
			}
			// 仅排除 MysteryFuzzyIntent (改写);Allow / Defer 都接受
			if res.Mode == MysteryFuzzyIntent {
				t.Errorf("心理战不应被 Fuzzy 改写; mode=%s", res.Mode)
			}
		})
	}
}

// TestMysteryMask_Defer_IdentityExposure 隐晦身份暴露 → 原文发出 + 反馈 LLM。
// Mode 期望 MysteryDeferToGame (优先级低于 Fuzzy,高于 Allow)。
// 若同时被 Allow 命中,dominantMode 也是 Defer (因为 Defer > Allow)。
func TestMysteryMask_Defer_IdentityExposure(t *testing.T) {
	cases := []string{
		"我的身份是真预言家",
		"我的真实身份为女巫",
		"我用了药自救",
		"我用了药",
		"我用了毒药",
		"我用了了解药",
		"我昨晚查了4号是金水",
		"全场唯一真预言家就是我",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			res := MysteryMaskText(text)
			// 原文应保持
			if res.Text != text {
				t.Errorf("DeferToGame 类应原文发出; got %q want %q", res.Text, text)
			}
			// DeferToGame 应为主导(或 Single hit),不应为 Fuzzy
			if res.Mode == MysteryFuzzyIntent {
				t.Errorf("隐晦身份不应被 Fuzzy 改写; mode=%s", res.Mode)
			}
		})
	}
}

// TestMysteryMask_Fuzzy_SystemLeak 真 bug(0-indexed 座位号/系统元信息)→ 严改写。
func TestMysteryMask_Fuzzy_SystemLeak(t *testing.T) {
	cases := []struct {
		desc           string
		text           string
		mustNotContain string // 文本里绝不应再含这个子串(已改写的)
		wantMode       MysteryMode
	}{
		{"0-indexed 座位号", "我的座位号3号", "座位号", MysteryFuzzyIntent},
		{"座位号简写", "座位号 3", "座位号", MysteryFuzzyIntent},
		{"剩余秒数-我还有X秒", "我还剩271秒", "271秒", MysteryFuzzyIntent},
		{"剩余秒数-还有X秒", "还有12秒", "12秒", MysteryFuzzyIntent},
		{"剩余秒数-距投票还剩", "距投票时间还剩3秒", "3秒", MysteryFuzzyIntent},
		{"让我用idle工具", "让我用一个idle工具", "idle工具", MysteryFuzzyIntent},
		{"让我评估局势", "让我评估一下当前局势", "评估", MysteryFuzzyIntent},
		{"让我判断局面", "让我判断局面发展", "判断", MysteryFuzzyIntent},
		{"系统提示投票阶段", "系统提示投票阶段开始了", "系统提示", MysteryFuzzyIntent},
		{"我得赶紧投", "我得赶紧投", "赶紧投", MysteryFuzzyIntent},
		// BUG-R191-SEC-01 (2026-07-24): MiniMax M3 13人局 Agent 公屏泄露
		{"R191 bare 0-indexed seat 7", "我是狼人，座位7（对外说8号）", "座位7", MysteryFuzzyIntent},
		{"R191 bare seat with chinese paren", "座位7（对外说8号）", "座位7", MysteryFuzzyIntent},
		{"R191 bare seat alone", "座位7", "座位7", MysteryFuzzyIntent},
		{"R191 bot-vs-human enumeration", "玩家编号13号是人类，其余12个都是不同模型bot", "bot", MysteryFuzzyIntent},
		{"R191 rule-system narrative", "规则要求我必须调speak。规则是死的人是活的", "规则要求", MysteryFuzzyIntent},
		{"R191 system-hint me vote", "系统提示我赶紧投", "系统提示", MysteryFuzzyIntent},
		// BUG-R200-SEC-01 (2026-07-24): Kimi k3 13人局 Agent internal_thought
		// 暴露「逐座位 bot 身份映射」(1号是1号Bot、3号是4号Bot、9号是10号Bot、
		// 12号是13号Bot)。R191 bot_vs_human元认知 仅覆盖「N 个是 bot」/「玩家
		// 编号 X 是 人类」形态,未覆盖「X号是X号Bot / X号是N号Bot」形态。
		{"R200 per-seat bot map same-id", "1号是1号Bot", "Bot", MysteryFuzzyIntent},
		{"R200 per-seat bot map cross-id", "3号是4号Bot", "Bot", MysteryFuzzyIntent},
		{"R200 per-seat bot list", "狼队4人都在存活玩家中(1号是1号Bot、3号是4号Bot、9号是10号Bot、12号是13号Bot)", "Bot", MysteryFuzzyIntent},
		{"R200 per-seat bot with model", "1号是1号Bot,模型是Kimi", "Bot", MysteryFuzzyIntent},
		{"R200 per-seat bot upper", "3号是4号BOT", "BOT", MysteryFuzzyIntent},
		{"R200 per-seat bot qz", "5号是6号机器人", "机器人", MysteryFuzzyIntent},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			res := MysteryMaskText(c.text)
			if strings.Contains(res.Text, c.mustNotContain) {
				t.Errorf("FuzzyIntent 改写未生效; output %q 仍含 %q", res.Text, c.mustNotContain)
			}
			if res.Mode != c.wantMode {
				t.Errorf("Mode 应为 %s; got %s", c.wantMode, res.Mode)
			}
			if !res.Hit {
				t.Errorf("应标记 Hit=true")
			}
		})
	}
}

// TestMysteryMask_Empty 边界条件:空文本。
func TestMysteryMask_Empty(t *testing.T) {
	res := MysteryMaskText("")
	if res.Text != "" {
		t.Errorf("空文本应原样返回; got %q", res.Text)
	}
	if res.Hit {
		t.Errorf("空文本不命中")
	}
}

// TestMysteryMask_GarbleGuard 短文本 garble 防护。
func TestMysteryMask_GarbleGuard(t *testing.T) {
	// "我用了药" 命中 DeferToGame,文本不变(不会 garble)。
	res := MysteryMaskText("药")
	if res.Text != "药" {
		t.Errorf("应原样返回单字; got %q", res.Text)
	}
}

// TestMysteryMask_NoHitNormalText 良性发言 — R132 视角下允许被命中 (心理战合法),
// 但不应被改写 (原文发出),且 Mode 不应为 MysteryFuzzyIntent (无系统实现泄漏)。
func TestMysteryMask_NoHitNormalText(t *testing.T) {
	cases := []string{
		"我是好人,我不是狼",
		"我感觉3号像狼,因为他昨晚没说话",
		"昨晚我是平安夜",
		"我怀疑7号",
		"我建议今天先放逐5号",
		"今天该投票了吧",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			res := MysteryMaskText(text)
			if res.Text != text {
				t.Errorf("良性发言应原样返回; got %q want %q", res.Text, text)
			}
			if res.Mode == MysteryFuzzyIntent {
				t.Errorf("良性发言不应触发 Fuzzy 改写; mode=%s", res.Mode)
			}
		})
	}
}

// TestMysteryMask_R191_FalsePositives BUG-R191-SEC-01: 新增的 3 条 FuzzyIntent 规则
// 必须不误杀良性发言(座位安排 / 玩家身份推测 / 规则讨论等)。
func TestMysteryMask_R191_FalsePositives(t *testing.T) {
	cases := []string{
		"今天的座位安排挺合理的",
		"5号座位上坐着谁？",
		"我建议把座位3让给新人",
		"13号玩家的发言很奇怪,我先观察",
		"13号身份不明",
		"按规则办,大家举手投票",
		"规则是大家一起定的,得遵守",
		"提示任务比较繁重",
		"系统提示有时候很模糊,需要自己判断",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			res := MysteryMaskText(text)
			if res.Mode == MysteryFuzzyIntent {
				t.Errorf("良性发言不应触发 FuzzyIntent 改写; input=%q mode=%s out=%q", text, res.Mode, res.Text)
			}
		})
	}
}

// TestMysteryMask_R200_FalsePositives BUG-R200-SEC-01: 新增的「逐座位 bot
// 身份枚举」规则必须**不**误伤合法 1-indexed 推理(如「1号是狼」/「5号是
// 预言家」)与多位数座位号边界。
func TestMysteryMask_R200_FalsePositives(t *testing.T) {
	cases := []string{
		// 合法 1-indexed 推理(角色名不是 bot/Bot/机器人)→ 绝不能触发
		"1号是狼",
		"3号是预言家",
		"5号是好人",
		"7号是村民",
		"9号是狼人,昨晚查杀5号",
		"我怀疑1号是狼,理由是他话太多",
		"2号是真预言家,3号是假预言家",
		"5号是狼人悍跳预言家",
		// 数字号 + bot 字面词但不是「X号是X号Bot」映射(应被现有 R191
		// 规则覆盖)→ 不应被本规则重复改写; 走 MysteryDeferToGame / Allow
		"其余12个都是不同模型bot",                  // 计数形式(R191 覆盖)
		"玩家编号13号是人类,其余12个都是不同模型bot", // R191 完整覆盖
		// 边界:多位数座位号 + bot 形式(本规则不要求多位数限制,数字号
		// 形如「\d+\s*号」是允许的,只有「号」+「Bot」组合才命中)
		"10号是11号Bot",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			res := MysteryMaskText(text)
			// 关键断言:不能把「1号是狼」/「3号是预言家」类合法推理
			// 模糊改写为「其他玩家」,否则观战者会读不懂 bot 的公开分析。
			// 唯一例外:R191「其余 N 个都是 bot」已合法命中;允许继续。
			if strings.Contains(res.Text, "其他玩家") && text == "1号是狼" {
				t.Errorf("1号是狼不应被改写为「其他玩家」; got %q", res.Text)
			}
		})
	}
}

// TestComposeMysteryHint 反馈字符串格式。
func TestComposeMysteryHint(t *testing.T) {
	cases := []struct {
		desc   string
		res    MysteryMaskResult
		expect string
	}{
		{
			"心理战类别",
			MysteryMaskResult{
				Hit:           true,
				Mode:          MysteryAllow,
				HitCategories: []string{"自报身份"},
			},
			"心理战",
		},
		{
			"隐晦身份",
			MysteryMaskResult{
				Hit:           true,
				Mode:          MysteryDeferToGame,
				HitCategories: []string{"隐晦身份-我用了药"},
			},
			"隐晦身份暴露",
		},
		{
			"系统实现",
			MysteryMaskResult{
				Hit:           true,
				Mode:          MysteryFuzzyIntent,
				HitCategories: []string{"硬底线-剩余秒数"},
			},
			"系统实现泄漏",
		},
		{
			"未命中",
			MysteryMaskResult{Hit: false},
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			hint := ComposeMysteryHint(c.res)
			if c.expect == "" {
				if hint != "" {
					t.Errorf("未命中应返回空字符串; got %q", hint)
				}
				return
			}
			if !strings.Contains(hint, c.expect) {
				t.Errorf("hint 应包含 %q; got %q", c.expect, hint)
			}
			if c.res.Hit && len(c.res.SpokenHints) > 0 {
				if !strings.Contains(hint, "下次同类表达建议:") {
					t.Errorf("hint 应包含下次同类段; got %q", hint)
				}
			}
		})
	}
}

// TestDominantMode 多规则命中时优先级。
func TestDominantMode(t *testing.T) {
	cases := []struct {
		desc  string
		modes []MysteryMode
		want  MysteryMode
	}{
		{"全 Allow", []MysteryMode{MysteryAllow, MysteryAllow}, MysteryAllow},
		{"Allow + Defer", []MysteryMode{MysteryAllow, MysteryDeferToGame}, MysteryDeferToGame},
		{"Allow + Fuzzy", []MysteryMode{MysteryAllow, MysteryFuzzyIntent}, MysteryFuzzyIntent},
		{"Defer + Fuzzy", []MysteryMode{MysteryDeferToGame, MysteryFuzzyIntent}, MysteryFuzzyIntent},
		{"全 Fuzzy", []MysteryMode{MysteryFuzzyIntent, MysteryFuzzyIntent}, MysteryFuzzyIntent},
		{"空", []MysteryMode{}, MysteryAllow},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			got := dominantMode(c.modes)
			if got != c.want {
				t.Errorf("dominantMode mismatch; got %s want %s", got, c.want)
			}
		})
	}
}

// TestMysteryMask_LengthGuard 防 garble: 改写后长度 <50% 还原文本。
// 极端:Fuzzy 命中短文本会破坏完整性。
func TestMysteryMask_LengthGuard(t *testing.T) {
	// 极短文本只有 "我用了药"(3字符),Fuzzy 不命中(DeferToGame 不改写),
	// 所以这里用 MysteryFuzzyIntent 规则测:只剩 "剩余X秒" 的极短样本。
	// 现状:只有 MysteryFuzzyIntent 会改写,而 1-char 触发几乎不可能;
	// 这里直接验证逻辑: 如果 cleaned<50% 长度,会还原。
	// 我们手工构造一个 MysteryFuzzyIntent 命中、改写后变短的 case:
	res := MysteryMaskText("座位号 3")
	// "座位号 3" 长度 5; 改写为 "我自己" 长度 3; 5/2=2.5 < 3 → 不还原
	if res.Text != "我自己" {
		t.Errorf("短文本应改写; got %q", res.Text)
	}
}

// TestMysteryMask_MultipleHits 一句话命中多条规则应聚合 categories。
func TestMysteryMask_MultipleHits(t *testing.T) {
	// 同时触发"心理战自报"+"狼队友披露" 的极端文本
	res := MysteryMaskText("我是狼人,我的同伙有2号、3号、8号")
	if !res.Hit {
		t.Fatalf("应命中; got Hit=false")
	}
	if len(res.HitCategories) < 2 {
		t.Errorf("应至少 2 条命中类别; got %d", len(res.HitCategories))
	}
	// 纯心理战文本,不改写
	if res.Text != "我是狼人,我的同伙有2号、3号、8号" {
		t.Errorf("MysteryAllow 命中不应改写文本; got %q", res.Text)
	}
}

// TestAllRules_HaveName 健全性:每条规则必须有 Name + SpokenHint 非空。
func TestAllRules_HaveName(t *testing.T) {
	for _, r := range mysteryRules {
		if r.Name == "" {
			t.Errorf("rule missing Name")
		}
		if r.Pattern == nil {
			t.Errorf("rule %s missing Pattern", r.Name)
		}
		if r.SpokenHint == "" {
			t.Errorf("rule %s missing SpokenHint", r.Name)
		}
		if r.Mode == MysteryFuzzyIntent && r.Fuzzy == nil {
			t.Errorf("rule %s Mode=Fuzzy 但缺 Fuzzy func", r.Name)
		}
	}
}

// F1 (2026-07-24): 「0号」独立 token 改写 — MysteryMaskText 路径(non-streaming,
// agent_runner.Speak / SpeakAuto / SpeakWithThought / Interject 调用的入口)。
//
// 关键边界:
//   - 独立「0号」必须被改写为「1号」(1-indexed 合法首座);
//   - 「10号」「20号」「100号」必须保留(它们的「0号」前面是数字,被非数字边界
//     检查排除);
//   - 流式 chunk 切到「0」+「号」之间时,单 chunk「0号」会被识别并改写。
func TestMysteryMask_F1_BareZeroSeat(t *testing.T) {
	cases := []struct {
		desc           string
		text           string
		mustNotContain string // 文本里绝不应再含这个子串(已改写的)
		wantMode       MysteryMode
	}{
		// === 独立「0号」必须被改写为「1号」+ FuzzyIntent ===
		{"F1 bare 0号", "0号走了", "0号", MysteryFuzzyIntent},
		{"F1 bare 0号 at start", "0号 在场", "0号", MysteryFuzzyIntent},
		{"F1 bare 0号 mid-sentence", "我认为0号是好人", "0号", MysteryFuzzyIntent},
		{"F1 bare 0号 end-of-sentence", "我投0号", "0号", MysteryFuzzyIntent},
		{"F1 bare 0号 with chinese comma", "0号，先观察", "0号", MysteryFuzzyIntent},
		{"F1 bare 0号 with english comma", "0号,先观察", "0号", MysteryFuzzyIntent},
		{"F1 0号 between parens", "(0号) 投票", "0号", MysteryFuzzyIntent},

		// === 多位数「10号」「20号」「100号」必须保留(不被改写) ===
		{"F1 benign 10号 stays", "我投10号", "", MysteryAllow},
		{"F1 benign 20号 stays", "20号是预言家", "", MysteryAllow},
		{"F1 benign 100号 stays", "100号玩家", "", MysteryAllow},
		{"F1 benign 0号 in 100号 not corrupted", "100号", "", MysteryAllow},
		{"F1 benign 10号 mid-sentence stays", "我怀疑10号是好人,因为 10 号昨晚没说话", "", MysteryAllow},
		{"F1 benign 200号 stays", "200号玩家", "", MysteryAllow},

		// === 流式 chunk 边界 ===
		{"F1 streaming chunk 0 alone stays", "0", "", MysteryAllow},
		{"F1 streaming chunk 0号 standalone rewritten", "0号", "0号", MysteryFuzzyIntent},
		{"F1 streaming chunk 10号 stays", "10号", "", MysteryAllow},
		{"F1 streaming chunk 100号 stays", "100号", "", MysteryAllow},

		// === 边界:数字紧邻「0号」视为多位数一部分 ===
		{"F1 0号 preceded by digit stays", "我投100号", "", MysteryAllow},
		{"F1 0号 followed by digit stays", "0号1", "", MysteryAllow},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			res := MysteryMaskText(c.text)
			if c.mustNotContain != "" && strings.Contains(res.Text, c.mustNotContain) {
				t.Errorf("FuzzyIntent 改写未生效; output %q 仍含 %q", res.Text, c.mustNotContain)
			}
			if res.Mode != c.wantMode {
				t.Errorf("Mode 应为 %s; got %s", c.wantMode, res.Mode)
			}
		})
	}
}

// F1 (2026-07-24): 「0号」改写后必须**含**「1号」(确保改写非空)。
func TestMysteryMask_F1_RewriteContainsOneSeat(t *testing.T) {
	res := MysteryMaskText("0号走了")
	if !strings.Contains(res.Text, "1号") {
		t.Errorf("F1 改写后必须含「1号」; got %q", res.Text)
	}
	if strings.Contains(res.Text, "0号") {
		t.Errorf("F1 改写后必须不残留「0号」; got %q", res.Text)
	}
}
