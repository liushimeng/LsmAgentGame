// Package werewolf — speak_filter_test.go: BUG-R74-1/2 regression coverage.
//
// Locks the two engine-side guards added in 2026-07-09:
//  1. ScrubIdentityLeak replaces role self-disclosure with "[已过滤]".
//  2. The SpeakLimiter.Allow() gate is wired into runner.Speak — verified
//     indirectly via the contract: when Allow()=false, runner.Speak returns
//     "rate-limited" without calling chatSvc.
//
// Note: runner.Speak integration is exercised by the room-level tests
// (room_round*.go); here we lock the leaf scrubber.
package werewolf

import (
	"strings"
	"testing"
)

func TestScrubIdentityLeak_SelfReportAre(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantHit   bool
		wantSubst string // substring that must appear in the output when wantHit=true
	}{
		{
			name:      "self-report wolf",
			input:     "作为狼人我得低调一点",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "self-report seer",
			input:     "我是预言家,昨晚查了1号",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "self-report witch",
			input:     "我是女巫,救了他",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "self-report hunter",
			input:     "我是猎人,死前能开枪",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "self-report villager",
			input:     "我是村民,只能投票",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "self-report-as number form",
			input:     "我是3号狼人,跟票",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:    "benign analysis passes through",
			input:   "我怀疑1号是狼人,理由是他话太多",
			wantHit: false,
		},
		{
			name:    "benign talk passes through",
			input:   "今天天气不错,投票吧",
			wantHit: false,
		},
		{
			name:    "empty input",
			input:   "",
			wantHit: false,
		},
		// R86-P1-1 新增 — "真/假神职" 变种 (R86 报告 4.3 场景 B)
		{
			name:      "true-seer without leading 我",
			input:     "我是真预言家。3号是狼人,大家跟我投票",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "true-witch label",
			input:     "我才是真女巫,1号是假冒的",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "fake-seer label",
			input:     "5号是假预言家,别投他",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// R86-P1-1 新增 — "X号神职" 公开点名
		{
			name:      "public number-label",
			input:     "3号预言家查杀2号,大家跟我投2号",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "public number-with-是",
			input:     "5号就是狼人,昨晚跳了预言家",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// R86-P1-1 新增 — 公开查验结果
		{
			name:      "public check result",
			input:     "昨晚查验1号是狼人,请大家相信我",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "verify number with chinese colon",
			input:     "查杀：3号是好人,大家不要投他",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// R86-P1-1 新增 — 装身份衍生态
		{
			name:      "villager impersonates seer",
			input:     "我作为村民跳预言家,这是策略",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// 反向 — 良性分析不应误杀 (R86-P1-1 关键保护)
		{
			name:    "benign true-seer-in-suspicion stays",
			input:   "我怀疑1号是真预言家,因为他首夜跳了",
			wantHit: false,
		},
		{
			name:    "benign seer-suspicion stays",
			input:   "1号是预言家吗?我先观察",
			wantHit: false,
		},
		// R91-P0-1 新增 — "我的同伙/队友/我们狼" 公开狼队成员身份
		{
			name:      "leak pack mates via 同伙",
			input:     "我查到我的同伙有2号、3号、8号、10号。我建议先刀一个站边比较积极、发言有点分量的人",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "leak via 队友 enumeration",
			input:     "我们的队友分别坐1号、5号、9号座位",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "leak via 我们这边是狼",
			input:     "我们这边是狼阵营,先刀5号",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// R91-P0-1 新增 — 公开击杀意图
		{
			name:      "leak kill intent via 我们今晚刀X",
			input:     "我们今晚刀2号,大家准备投2号",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "leak kill intent via 今晚先杀X",
			input:     "今晚先杀3号,5号留着查预言家",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// BUG-R10-NEW3-001 (2026-08-16): 第一人称+软建议词+刀/杀动词+X号
		// R10 自动化测试 23:27 抓取 Bot #1 / Bot #2 白天警长竞选阶段发言
		// 中使用"建议刀8号"/"我觉得刀1号比较合适"等句式,泄露狼队击杀意图。
		// R91-P0-1 regex 仅锚定"我们/今晚"主语,未覆盖此类句式。
		{
			name:      "R10 Bot1 我建议刀X号 leak",
			input:     "我是1号。建议刀8号,他发言积极,可能是神职。",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R10 Bot2 我觉得刀X号 leak",
			input:     "我觉得刀1号比较合适,你们觉得呢?",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R10 我打算刀X号 leak",
			input:     "我打算刀6号,理由是发言太积极",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R10 咱先杀X号 leak",
			input:     "咱们先杀2号吧,大家同意吗",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// 反向 — 良性发言不应误杀
		{
			name:    "benign 我建议投X号 stays",
			input:   "我建议投3号,大家参考",
			wantHit: false,
		},
		{
			name:    "benign 我觉得X号不像狼 stays",
			input:   "我觉得5号不像狼,理由如下",
			wantHit: false,
		},
		{
			name:    "benign 这把刀很锋利 stays",
			input:   "这把刀很锋利",
			wantHit: false,
		},
		{
			name:    "benign 大家要记得跟票 stays",
			input:   "大家要记得跟票",
			wantHit: false,
		},
		// R91-P0-1 新增 — 自我编号+括号式 0-indexed 透出
		{
			name:      "leak seat number via parenthetical 0-indexed",
			input:     "我是12号(13号玩家),先确认队友",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// 反向 — 良性发言不应误杀
		{
			name:    "benign self-intro stays",
			input:   "我是12号玩家,先听听大家发言",
			wantHit: false,
		},
		{
			name:    "benign kill-vote via 单人决策 stays",
			input:   "今晚我准备投2号,理由是他话太多",
			wantHit: false,
		},
		// BUG-R119-SEC-01 (2026-07-14): "X号是狼队友" 公开点名 + 狼队成员关系
		{
			name:      "leak wolf teammate via seat label",
			input:     "3号是狼队友,我要保他,不能投8号",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "leak wolf friend via seat label with 我们 wolf ally",
			input:     "我们这边狼友是5号,先稳住",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// BUG-R119-SEC-01 (2026-07-14): 代词 + 真/假神职
		{
			name:      "leak true-seer via pronoun",
			input:     "不能投8号（他是真预言家）,我先投个没站边的",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "leak false-witch via pronoun",
			input:     "她是假女巫,3号才是真女巫",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// BUG-R119-SEC-01 (2026-07-14): 内部决策元信息泄露 (CoT)
		{
			name:      "leak internal time budget and phase hint",
			input:     "我还剩271秒,发言顺序已过。系统提示投票阶段,我得赶紧投",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "leak internal next-step planning",
			input:     "我的下一步是观察8号的站边,然后投个没站边的",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// BUG-R125-SEC-001 (2026-07-15): 狼人阵营夜间协商泄露公聊 — 队友编号 / 击杀目标。
		{
			name:      "R125 teammate-ID self-report with seat number",
			input:     "2号，我是7号狼同伴。商讨今晚刀型：建议先刀3号或5号平民",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R125 teammate-ID third-person reference",
			input:     "我认为7号狼同伴可以跳预言家,撑身份",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R125 teammate-ID verb-prefix third-person",
			input:     "为7号同伙先发言,我来垫飞",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R125 wolf-count disclosure",
			input:     "6号被刀了！我们三个狼都还在，4号是不是也活着",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R125 wolf-count disclosure arabic",
			input:     "我们四个狼人还活着,稳了",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R125 teammate enumeration with 就是我们",
			input:     "4 11 12 8，就是我们。9号刚说看验人逻辑再判断真假",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R125 teammate enumeration punctuated",
			input:     "1号、2号、5号、9号都是队友,先互保",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// R125 反向 — 良性分析 / 好人发言不应被误杀
		{
			name:    "R125 benign self-intro with seat stays",
			input:   "我是12号玩家,先听听大家发言",
			wantHit: false,
		},
		{
			name:    "R125 benign good-player vote talk stays",
			input:   "我们三个好人,都投5号",
			wantHit: false,
		},
		{
			name:    "R125 benign civic-multi-seat analysis stays",
			input:   "1号、2号、3号，都是我查验的金水",
			wantHit: false,
		},
		// BUG-R191-SEC-01 (2026-07-24): MiniMax M3 13人局 Agent 公屏泄露
		// (a) 0-indexed 座位号 bare 「座位7」
		// (b) bot vs human 元认知
		// (c) 系统提示/规则约束内部叙事
		{
			name:      "R191 bare 0-indexed seat 7",
			input:     "我是狼人，座位7（对外说8号）",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R191 bare 0-indexed seat with comma",
			input:     "我是狼人,座位7,先稳住",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R191 bot-vs-human enumeration",
			input:     "玩家编号13号是人类，其余12个都是不同模型bot",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R191 rule-system internal narrative",
			input:     "规则要求我必须调speak。规则是死的人是活的",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// BUG-R200-SEC-01 (2026-07-24): Kimi k3 13人局 Agent internal_thought
		// 暴露「逐座位 bot 身份映射」(1号是1号Bot、3号是4号Bot、9号是10号Bot、
		// 12号是13号Bot)。这是 streaming-delta / publicText 路径上的同形态
		// 泄漏,R191 bot_vs_human元认知 仅覆盖「N 个是 bot」,本条新规则
		// 把覆盖扩到「X号是X号Bot / X号是N号Bot」逐座位映射形态。
		{
			name:      "R200 per-seat bot map same-id",
			input:     "1号是1号Bot",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R200 per-seat bot map cross-id",
			input:     "狼队4人都在存活玩家中(1号是1号Bot、3号是4号Bot、9号是10号Bot、12号是13号Bot)",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R200 per-seat bot with model",
			input:     "1号是1号Bot,模型是Kimi",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R200 per-seat bot upper",
			input:     "3号是4号BOT",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		// R200 反向 — 合法 1-indexed 角色推理必须**不**被误杀。
		// 关键边界:本规则要求末尾是「bot/Bot/BOT/机器人」,因此
		//「1号是狼」「3号是预言家」「5号是好人」必须原样保留。
		{
			name:    "R200 benign 1号是狼 stays",
			input:   "1号是狼",
			wantHit: false,
		},
		{
			name:    "R200 benign 3号是预言家 stays",
			input:   "3号是预言家",
			wantHit: false,
		},
		// R191 反向 — 良性发言不应被误杀
		{
			name:    "R191 benign seat arrangement stays",
			input:   "今天的座位安排挺合理的",
			wantHit: false,
		},
		{
			name:    "R191 benign X-seat-occupant stays",
			input:   "5号座位上坐着谁？",
			wantHit: false,
		},
		{
			name:    "R191 benign rule talk stays",
			input:   "规则是大家一起定的,得遵守",
			wantHit: false,
		},
		{
			name:    "R191 benign good-faction analysis stays",
			input:   "13号玩家的发言很奇怪,我先观察",
			wantHit: false,
		},
		// BUG-R245-P0-01 (2026-08-06): Bot 9号 在警长竞选阶段公开说
		// "作为守卫,竞选警长是有价值的——警徽流可以传递我的守护信息"。
		// 之前 regex 缺守卫/白痴/神职。新规则:
		{
			name:      "R245 self-report guard bare 作为",
			input:     "作为守卫,竞选警长是有价值的——警徽流可以传递我的守护信息",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R245 self-report guard 我作为",
			input:     "我作为守卫,守了3号一晚",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R245 self-report guard 身为",
			input:     "身为守卫,我压力很大",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:      "R245 self-report guard 我乃是",
			input:     "我乃是白痴,出局直接翻牌",
			wantHit:   true,
			wantSubst: "[已过滤]",
		},
		{
			name:    "R245 benign 我怀疑 4号是守卫 stays",
			input:   "我怀疑4号可能是守卫,他的发言太被动",
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, hit := ScrubIdentityLeak(tc.input)
			if hit != tc.wantHit {
				t.Fatalf("hit mismatch: input=%q got hit=%v want=%v", tc.input, hit, tc.wantHit)
			}
			if tc.wantHit && !strings.Contains(out, tc.wantSubst) {
				t.Fatalf("output missing %q: input=%q out=%q", tc.wantSubst, tc.input, out)
			}
		})
	}
}

func TestScrubIdentityLeak_AggressiveRewriteRefused(t *testing.T) {
	// If scrubbing would remove >50% of the message, return the original
	// unchanged (LLM clearly meant something else — don't garble it).
	// Pick a case where substitution genuinely collapses the message:
	// "我是狼人" (4 runes) → "[已过滤]" (5 runes) — slightly LONGER, so
	// by rune count alone it doesn't shrink. Use a broader pattern that
	// collapses, e.g. "是狼人" (3 runes) — but our patterns don't match
	// bare "是狼人" (we require "我是/作为/身为"). Therefore the natural
	// escape hatch is "I am wolf, no other content" — short, but the
	// substitution expands it. We instead verify that when the entire
	// message is JUST a self-report, we DO scrub (the conservative guard
	// only triggers when scrubbing would *shrink* the message below half).
	in := "我是狼人" // pure self-report
	out, hit := ScrubIdentityLeak(in)
	if !hit {
		t.Fatalf("expected scrub on pure self-report, got out=%q hit=false", out)
	}
	if !strings.Contains(out, "[已过滤]") {
		t.Fatalf("expected [已过滤] in output, got %q", out)
	}
}

// BUG-R122-SEC-01 (2026-07-15): R122 流式直播面板新增泄露样本 — 硬身份自报、
// 查验结果、警徽流计划在 R121 模式之外,需单独锁定。同时校验不误杀平民正常发言。
func TestScrubIdentityLeak_R122StreamingLeak(t *testing.T) {
	leaks := []struct {
		name  string
		input string
	}{
		{"self-report-seer-full", "我是3号,全场唯一预言家,昨晚查的4号是金水。警徽流先摸8号再压5号再压..."},
		{"self-report-death-narrative", "我是3号狼人,昨夜被刀了很遗憾。先不跳身份,免得好人混乱..."},
		{"self-report-villager-hard-role", "我是12号,我就是个女巫,没什么特殊信息。现在已经有6号和7号参选警长..."},
		{"self-report-plain", "我是3号预言家。首夜信息少,先看大家发言..."},
		{"check-result-gold", "我昨晚查了4号是金水"},
		{"check-result-verify", "我昨夜查验4号,是个金水"},
		{"badge-flow", "警徽流先摸8号再压5号"},
		{"badge-flow-short", "警徽流先8后5"},
		{"true-seer-emphasis", "我才是全场唯一真预言家"},
		{"true-witch-emphasis", "我才是全场唯一真女巫"},
	}
	for _, c := range leaks {
		out, hit := ScrubIdentityLeak(c.input)
		if !hit {
			t.Errorf("[%s] expected HIT on leak %q, got pass-through out=%q", c.name, c.input, out)
			continue
		}
		if !strings.Contains(out, "[已过滤]") {
			t.Errorf("[%s] expected [已过滤] in output, got %q", c.name, out)
		}
	}

	// False-positive guard: 平民可伪装身份/正常分析不应被误杀。
	legit := []string{
		"我怀疑1号是狼人",
		"我分析了局势,觉得3号是好人",
		"5号的发言像预言家",
		"3号跳预言家,我暂时站边他",
		"我觉得3号是金水",
		"5号可能是查杀",
		"我是个好人",
		"我只是一个普通的村民",
	}
	for _, in := range legit {
		if _, hit := ScrubIdentityLeak(in); hit {
			t.Errorf("[false-positive] unexpectedly scrubbed legit speech %q", in)
		}
	}
}

// BUG-R121-SEC-01 (2026-07-15): 思考链流式泄露。§127 流式 chunk 路径绕过
// ScrubIdentityLeak,在公屏以 "🤖 Bot #N" 格式实时输出 LLM 内部叙事。
// 这些用例直接来自 R121 报告 r121_thinking_leaks.txt 的真实泄露样本;新增
// 5 条 identityLeakPatterns 就是为覆盖它们。同时校验不对正常的对他人的
// 策略分析误杀 (false-positive guard)。
func TestScrubIdentityLeak_R121StreamingLeak(t *testing.T) {
	leaks := []struct {
		name  string
		input string
	}{
		{"identity-mine-女巫", "我的身份是女巫，第一天晚上有狼人来杀我（座位号3），我用了药自救"},
		{"identity-mine-真实身份", "我的真实身份为预言家，警徽流我会传"},
		{"seat-number-括号", "杀我（座位号3）"},
		{"seat-number-我的", "我的座位号3"},
		{"antidote-use-药自救", "我用了药自救"},
		{"antidote-use-解药", "我用了解药救了5号"},
		{"idle-tool-reveal", "让我用一个idle工具来保持沉默等待"},
		{"vote-tool-reveal", "让我使用投票工具"},
		{"deliberation-评估局势", "让我评估局势继续推进。投票已投给5号"},
		{"deliberation-查看局势", "让我查看当前局势继续推进"},
		{"deliberation-判断局面", "让我判断局面，跟票"},
	}
	for _, c := range leaks {
		out, hit := ScrubIdentityLeak(c.input)
		if !hit {
			t.Errorf("[%s] expected HIT on leak %q, got pass-through out=%q", c.name, c.input, out)
			continue
		}
		if !strings.Contains(out, "[已过滤]") {
			t.Errorf("[%s] expected [已过滤] in output, got %q", c.name, out)
		}
	}

	// False-positive guard: 正常的公开发言不应被误杀。
	legit := []string{
		"我怀疑1号是狼人",
		"5号赶紧报查验！查的是几号，金水还是查杀？",
		"我查到了人，愿意参选警长",
		"7号说得有理,预言家不急着报",
		"我分析了局势，觉得5号可疑", // 对他人分析,无 "让我" 前缀
		"让我看看局势",        // 缺认知动词+局势/局面,不匹配
		"让我想想",
	}
	for _, in := range legit {
		if _, hit := ScrubIdentityLeak(in); hit {
			t.Errorf("[false-positive] unexpectedly scrubbed legit speech %q", in)
		}
	}
}

// F1 (2026-07-24): 「0号」独立 token 改写 — 覆盖 streaming-delta 路径
// (room.go:2280-2301 OnLLMStreamDelta 直接调 ScrubIdentityLeak)。改写为「1号」
// (1-indexed 合法首座),不替换为 "[已过滤]" 以保持观感自然。
//
// 关键边界:必须**仅**匹配独立「0号」,**不**误伤「10号」「20号」「100号」
// 等合法多位数座位号(它们的「0号」前面是数字,被非数字边界检查排除)。
func TestScrubIdentityLeak_F1_BareZeroSeat(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantHit  bool
		wantHas  string // 改写后必须包含的子串
		wantMiss string // 改写后绝对不能包含的子串(保护多位数)
	}{
		// === 独立「0号」必须被改写为「1号」 ===
		{
			name:     "F1 standalone bare 0号",
			input:    "0号走了",
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},
		{
			name:     "F1 standalone bare 0号 at start",
			input:    "0号 在场",
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},
		{
			name:     "F1 standalone bare 0号 mid-sentence",
			input:    "我认为0号是好人",
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},
		{
			name:     "F1 standalone bare 0号 end-of-sentence",
			input:    "我投0号",
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},
		{
			name:     "F1 standalone bare 0号 with chinese comma",
			input:    "0号，先观察",
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},
		{
			name:     "F1 standalone bare 0号 with english comma",
			input:    "0号,先观察",
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},
		{
			name:     "F1 0号 between parens",
			input:    "(0号) 投票",
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},

		// === 多位数「10号」「20号」「100号」必须保留 ===
		{
			name:    "F1 benign 10号 stays",
			input:   "我投10号",
			wantHit: false,
		},
		{
			name:    "F1 benign 20号 stays",
			input:   "20号是预言家",
			wantHit: false,
		},
		{
			name:    "F1 benign 100号 stays (max even though game is 13-seat)",
			input:   "100号玩家",
			wantHit: false,
		},
		{
			name:    "F1 benign 0号 in 100号 not corrupted",
			input:   "100号",
			wantHit: false,
		},
		{
			name:    "F1 benign 10号 mid-sentence stays",
			input:   "我怀疑10号是好人,因为 10 号昨晚没说话",
			wantHit: false,
		},
		{
			name:    "F1 benign 200号 stays",
			input:   "200号玩家",
			wantHit: false,
		},

		// === 流式 chunk 边界(切到「0」+「号」之间)— 单独 chunk 命中独立「0号」 ===
		// 模拟 streaming delta:整条 LLM 输出「0号」被切成多个 chunk。
		// 每个 chunk 经过 ScrubIdentityLeak 时只看到部分 token,但完整
		// 「0号」在某一 chunk 内出现时(例如「我投 0」+「号」)会被识别。
		{
			name:    "F1 streaming delta chunk digit-only 0 stays",
			input:   "0", // chunk 1:仅「0」,不构成「0号」,不命中
			wantHit: false,
		},
		{
			name:     "F1 streaming delta chunk 0号 standalone rewritten",
			input:    "0号", // chunk 2:完整的「0号」独立 token
			wantHit:  true,
			wantHas:  "1号",
			wantMiss: "0号",
		},
		{
			name:    "F1 streaming delta chunk 10号 stays",
			input:   "10号",
			wantHit: false,
		},
		{
			name:    "F1 streaming delta chunk 100号 stays",
			input:   "100号",
			wantHit: false,
		},

		// === 边界:数字紧邻「0号」(视为多位数一部分,不命中) ===
		{
			name:    "F1 0号 preceded by digit stays",
			input:   "我投100号",
			wantHit: false,
		},
		{
			name:    "F1 0号 followed by digit stays",
			input:   "0号1",
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, hit := ScrubIdentityLeak(tc.input)
			if hit != tc.wantHit {
				t.Fatalf("hit mismatch: input=%q got hit=%v want=%v out=%q", tc.input, hit, tc.wantHit, out)
			}
			if tc.wantHit && tc.wantHas != "" && !strings.Contains(out, tc.wantHas) {
				t.Fatalf("output missing %q: input=%q out=%q", tc.wantHas, tc.input, out)
			}
			if tc.wantMiss != "" && strings.Contains(out, tc.wantMiss) {
				t.Fatalf("output should not contain %q: input=%q out=%q", tc.wantMiss, tc.input, out)
			}
		})
	}
}

// F1 (2026-07-24): 「0号」独立 token 改写边界 — 单字符「0」不构成「0号」,
// 不应触发 F1 改写。验证以防 regression 误吃纯数字 token。
func TestScrubIdentityLeak_F1_NoFalsePositiveOnDigit(t *testing.T) {
	cases := []string{
		"0",    // 纯数字
		"00",   // 纯数字
		" 0 ",  // 数字前后空白(无「号」)
		"投票给0", // 「0」单独出现但没有「号」
		"100",  // 纯数字(无「号」)
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			out, hit := ScrubIdentityLeak(in)
			if hit {
				t.Errorf("F1 应仅匹配「0号」,不应误伤纯数字 %q; out=%q", in, out)
			}
		})
	}
}

// R91-P0-2: HTML/XML 标签泄露防护。LLM 在 text 末尾输出 `</text></invoke>`
// 等内部 tool_call XML 闭合片段,直接发给公屏 = 把内部实现暴露给所有玩家。
func TestStripLLMInternalTags_R91LeakCases(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantHit  bool
		wantHas  string // substring that MUST appear in cleaned output
		wantMiss string // substring that MUST NOT appear in cleaned output
	}{
		{
			name:     "anthropic streaming trailing",
			input:    "我保持纯观望,目前没有明确立场。</text></invoke>",
			wantHit:  true,
			wantHas:  "我保持纯观望",
			wantMiss: "</text>",
		},
		{
			name:     "antml:invoke closure",
			input:    "好的我同意。</antml:invoke>",
			wantHit:  true,
			wantHas:  "好的我同意",
			wantMiss: "antml:invoke",
		},
		{
			name:     "tool_use self-closing",
			input:    `先看警长竞选。<tool_use id="abc"/>`,
			wantHit:  true,
			wantHas:  "先看警长竞选",
			wantMiss: "tool_use",
		},
		{
			name:     "function_calls fragment",
			input:    "我的分析。</function_calls>",
			wantHit:  true,
			wantHas:  "我的分析",
			wantMiss: "function_calls",
		},
		{
			name:     "benign math expression stays",
			input:    "我认为5<10是合理的判断",
			wantHit:  false,
			wantHas:  "5<10",
			wantMiss: "",
		},
		{
			name:    "empty input",
			input:   "",
			wantHit: false,
		},
		{
			name:    "benign chat stays",
			input:   "我觉得3号发言太快,可能是有备而来",
			wantHit: false,
		},
		// R91 边界:整段全是标签 → 还原原文(避免空消息落库)
		{
			name:    "all-tag-only returns original",
			input:   "</text></invoke>",
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, hit := StripLLMInternalTags(tc.input)
			if hit != tc.wantHit {
				t.Fatalf("hit mismatch: input=%q got hit=%v want=%v out=%q", tc.input, hit, tc.wantHit, out)
			}
			if tc.wantHas != "" && !strings.Contains(out, tc.wantHas) {
				t.Fatalf("output missing %q: input=%q out=%q", tc.wantHas, tc.input, out)
			}
			if tc.wantMiss != "" && strings.Contains(out, tc.wantMiss) {
				t.Fatalf("output should not contain %q: input=%q out=%q", tc.wantMiss, tc.input, out)
			}
		})
	}
}

// BUG-WITCH-P2-01 (2026-08-07, R246 女巫局): 成对 <tool_call> 块连内容整体剥离。
//
// 报告现象:公聊消息中出现原始 `<tool_call>` 标记。根因:旧 pattern 只覆盖
// `tool_calls`(复数,仅空白)与闭标签,`<tool_call>` 开标签(带属性或不带)
// 会命中兜底 `<[a-zA-Z][\w:\-]*>` 被剥掉,但成对块中间的 JSON 入参
// (`{"name":"speak","input":{...}}`)残留在公屏。修复:新增成对块 pattern
// (先于单标签执行,连内容一起删),并给 tool_calls 组补 tool_call 单数。
func TestStripLLMInternalTags_WitchP2ToolCallPair(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantHit  bool
		wantHas  string
		wantMiss string
	}{
		{
			name:     "paired tool_call with JSON payload stripped entirely",
			input:    `我认为3号发言有问题 <tool_call>{"name":"speak","input":{"text":"..."}}</tool_call> 先观望`,
			wantHit:  true,
			wantHas:  "我认为3号发言有问题",
			wantMiss: "tool_call",
		},
		{
			name:     "paired tool_call JSON body must not survive",
			input:    `先看警长 <tool_call>{"name":"vote","input":{"target":3}}</tool_call>`,
			wantHit:  true,
			wantHas:  "先看警长",
			wantMiss: `"name"`,
		},
		{
			name:     "paired tool_call with attributes",
			input:    `分析完毕。<tool_call name="speak" index="1">content</tool_call>`,
			wantHit:  true,
			wantHas:  "分析完毕",
			wantMiss: "tool_call",
		},
		{
			name:     "multiline tool_call block stripped",
			input:    "结论是投5号 <tool_call>\n{\"name\":\"speak\",\n\"input\":{}}\n</tool_call>",
			wantHit:  true,
			wantHas:  "结论是投5号",
			wantMiss: "speak",
		},
		{
			name:     "paired tool_calls plural with payload",
			input:    `好的 <tool_calls><invoke name="speak"/></tool_calls> 结束`,
			wantHit:  true,
			wantHas:  "好的",
			wantMiss: "tool_calls",
		},
		{
			name:     "lone closing tool_call tag stripped",
			input:    `发言结束。</tool_call>`,
			wantHit:  true,
			wantHas:  "发言结束",
			wantMiss: "tool_call",
		},
		{
			name:     "lone opening tool_call with attrs stripped",
			input:    `继续观察 <tool_call name="x">`,
			wantHit:  true,
			wantHas:  "继续观察",
			wantMiss: "tool_call",
		},
		{
			name:     "paired tool_use block stripped with content",
			input:    `投票给7号 <tool_use id="abc">{"input":{}}</tool_use>`,
			wantHit:  true,
			wantHas:  "投票给7号",
			wantMiss: "tool_use",
		},
		{
			// 注意:兜底 `<[a-zA-Z][\w:\-]*>` 会剥任何裸开标签(R91-P0-2 的既定
			// 策略,见 llmInternalTagPatterns 注释),此处仅验证良性文本主体保留。
			name:     "benign text body preserved",
			input:    "我觉得5<10的概率合理,继续观察",
			wantHit:  false,
			wantHas:  "5<10",
			wantMiss: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, hit := StripLLMInternalTags(tc.input)
			if hit != tc.wantHit {
				t.Fatalf("hit mismatch: input=%q got hit=%v want=%v out=%q", tc.input, hit, tc.wantHit, out)
			}
			if tc.wantHas != "" && !strings.Contains(out, tc.wantHas) {
				t.Fatalf("output missing %q: input=%q out=%q", tc.wantHas, tc.input, out)
			}
			if tc.wantMiss != "" && strings.Contains(out, tc.wantMiss) {
				t.Fatalf("output should not contain %q: input=%q out=%q", tc.wantMiss, tc.input, out)
			}
		})
	}
}

// BUG-R232-P1-01 (2026-08-02): verdict claim guard.
//
// Test ScrubVerdictClaim against the exact phrasing observed in the R232 report
// (Bot 8 智谱 GLM-5.2 hallucinating "游戏结束了" mid-game). Plus verify it does
// NOT scrub legitimate analysis (e.g. "8号 的逻辑跳大神 — 听起来像狼悍跳")
// and does NOT scrub when status == "over" (verdict claims are legitimate at
// game-over).
func TestScrubVerdictClaim_R232Hallucination(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		status        string
		wantHit       bool
		wantHas       string
		wantMiss      string
		wantUnchanged bool
	}{
		// R232 actual observation: Bot 8 GLM-5.2 at 16:01.
		{
			name:     "r232-exact-phrasing",
			input:    "游戏结束了 —— 狼人阵营触发了屠边胜利条件。神职已全部出局,好人败北。",
			status:   "playing",
			wantHit:  true,
			wantMiss: "游戏结束",
		},
		{
			name:     "r232-experience-recap",
			input:    "这局经验:投票时一定要确认目标编号,避免误投。",
			status:   "playing",
			wantHit:  true,
			wantMiss: "这局经验",
		},
		{
			name:     "this-game-ended",
			input:    "这局结束了。",
			status:   "playing",
			wantHit:  true,
			wantMiss: "结束",
		},
		{
			name:     "wolf-faction-wins",
			input:    "狼人阵营胜利,我们赢了。",
			status:   "playing",
			wantHit:  true,
			wantMiss: "阵营胜利",
		},
		{
			name:     "villager-side-lost",
			input:    "好人败北,神职全出局。",
			status:   "playing",
			wantHit:  true,
			wantMiss: "败北",
		},
		{
			name:     "屠边-triggered",
			input:    "狼人屠边胜利条件触发!",
			status:   "playing",
			wantHit:  true,
			wantMiss: "屠边胜利",
		},
		// Legitimate analysis should NOT be scrubbed.
		{
			name:    "legit-analysis-no-verdict",
			input:   "我听了 8 号 的发言,感觉跳大神节奏不太对,像是狼悍跳。",
			status:  "playing",
			wantHit: false,
		},
		{
			name:    "legit-asking-question",
			input:   "现在场上还剩 8 个人,谁是狼?",
			status:  "playing",
			wantHit: false,
		},
		// status == "over" must NOT scrub (verdict claims are legit).
		{
			name:    "over-allows-verdict",
			input:   "游戏结束了 —— 狼人阵营胜利。",
			status:  "over",
			wantHit: false,
		},
		{
			name:    "over-allows-recap",
			input:   "这局经验:投票时一定要确认目标编号。",
			status:  "over",
			wantHit: false,
		},
		// Empty input.
		{
			name:    "empty",
			input:   "",
			status:  "playing",
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, hit := ScrubVerdictClaim(tc.input, tc.status)
			if hit != tc.wantHit {
				t.Fatalf("hit mismatch: input=%q status=%q got hit=%v want=%v out=%q",
					tc.input, tc.status, hit, tc.wantHit, out)
			}
			if tc.wantMiss != "" && strings.Contains(out, tc.wantMiss) {
				t.Fatalf("output should not contain %q: input=%q out=%q", tc.wantMiss, tc.input, out)
			}
			if tc.wantUnchanged && out != tc.input {
				t.Fatalf("output should equal input: in=%q out=%q", tc.input, out)
			}
		})
	}
}
