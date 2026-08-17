// Package werewolf — speak_wolfguard_test (BUG-R11-001 回归, 2026-08-17)
//
// R11 自动化测试报告 P0:Bot 9 号 (美团 LongCat-2.0) 在 D1 遗言阶段公屏发言
// 「狼人队友，今晚先刀7号。他拿了警徽今晚要验8号，刀了他好人节奏就乱了,
// 10号金水也站不住。」— 狼队内沟通(称谓+击杀意图+目标)泄漏公屏,违反
// §133 WolfPackRoom 协议层隔离。
//
// 根因:R132 把「狼队-击杀意图」定为 MysteryAllow(主路径原文放行);
// §R10-NEW3 的 regex 只接在 ScrubIdentityLeak(主发言路径死代码)。
//
// 测试不变式:
//  1. CheckWolfCoordinationLeak 命中 R11/R10/R91 全部实测泄漏句式;
//  2. 不误杀:白天投票讨论(投/投死)、悍跳猎人(我死后带走X号)、刀口抽象
//     表达、对他人身份的怀疑分析;
//  3. 四条公屏广播路径(Speak/SpeakAuto/SpeakWithThought/Interject)全部接线
//     hard-reject — 狼阵营座位命中后不广播(§130 防「声明了却从不接线」);
//  4. 阵营门控:同一文本在好人阵营座位上**正常广播**(只拦狼);
//  5. ScrubIdentityLeak(流式 delta / internal_thought 旁路)同样覆盖 R11 句式。
package werewolf

import (
	"strings"
	"testing"
)

// TestWolfGuard_R11_Positive 实测泄漏句式必须全部命中。
func TestWolfGuard_R11_Positive(t *testing.T) {
	cases := []string{
		// R11 原句 (2026-08-17 Bot 9 美团 LongCat-2.0)
		"狼人队友，今晚先刀7号。他拿了警徽今晚要验8号，刀了他好人节奏就乱了，10号金水也站不住。",
		"狼人队友,今晚先刀7号",
		"我们的人今晚干掉3号",
		"自己人，准备刀5号",
		"队友们，先带走8号",
		"狼友,今晚咬9号",
		// R91 家族
		"我们今晚先刀7号",
		"咱们夜里杀3号",
		"今晚先刀7号",
		"今夜目标杀3号",
		// R10-NEW3 家族 (2026-08-16 Bot 1/2 实测)
		"我是1号。建议刀8号,他发言积极,可能是神职。",
		"我觉得刀1号比较合适,你们觉得呢?",
		"我打算杀6号",
	}
	for _, s := range cases {
		if cat, hit := CheckWolfCoordinationLeak(s); !hit {
			t.Errorf("应命中但未命中: %q", s)
		} else {
			t.Logf("命中 %q → pattern=%s", s, cat)
		}
	}
}

// TestWolfGuard_R11_Negative 合法发言不得误杀。
func TestWolfGuard_R11_Negative(t *testing.T) {
	cases := []string{
		"我建议投8号",        // 白天投票是全员合法行为
		"我建议大家投死2号",     // 「投死」不在守卫动词表
		"我死后带走7号",       // 悍跳猎人是 R132 合法心理战(第一人称条目不含「带走」)
		"刀口可能在7号",       // 刀口抽象表达是留给狼的合法讨论空间
		"我怀疑7号是狼人",      // 对他人身份的分析
		"狼人杀这个游戏太狠了",    // 元讨论
		"3号发言像狼人,跟票投2号", // 跟票表态
		"今晚要验8号",        // 查验不是击杀
		"我们今晚集中投7号",     // 夜间时间词+投票,非击杀
		"预言家今晚会查验谁?",    // 无击杀动词+数字号直连
	}
	for _, s := range cases {
		if cat, hit := CheckWolfCoordinationLeak(s); hit {
			t.Errorf("误杀合法发言: %q (pattern=%s)", s, cat)
		}
	}
}

// wolfGuardRoom 构造一个房间并把指定座位设置为指定角色,返回 runner。
// 与 newBlankSpeakRunner 同型,但 roomID/seat 可指定且注入角色。
func wolfGuardRunner(t *testing.T, role Role) (*agentRunner, *fakeChatSender) {
	t.Helper()
	mgr := blankMgr(t)
	const roomID = "test-room-wolfguard-r11"
	const botID = "bot-9-wolfguard"
	if _, _, em := mgr.JoinGame(roomID, botID); em != nil {
		t.Fatalf("JoinGame failed: %v", em)
	}
	chat := &fakeChatSender{}
	r := newAgentRunner(mgr, roomID, 9, botID, "Bot9", "MeiTuan-model", chat)
	r.filterCfg.EnableRateLimit = false
	r.filterCfg.EnableIdentityFilter = false
	// 注入权威角色:isWolfSeat 走 lockRoomBriefly 读 State.Roles[seat]。
	mgrRoom, ok := mgr.rooms[roomID]
	if !ok {
		t.Fatalf("room not created")
	}
	mgrRoom.mu.Lock()
	if mgrRoom.State == nil {
		mgrRoom.State = &GameState{}
	}
	mgrRoom.State.Roles[9] = role
	mgrRoom.mu.Unlock()
	return r, chat
}

const r11LeakText = "狼人队友，今晚先刀7号。他拿了警徽今晚要验8号，刀了他好人节奏就乱了。"

// TestWolfGuard_R11_WolfSeat_Rejected 狼阵营座位在 4 条广播路径全部被 hard-reject。
func TestWolfGuard_R11_WolfSeat_Rejected(t *testing.T) {
	t.Run("Speak", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleWerewolf)
		res, err := r.Speak(r11LeakText)
		if err != nil {
			t.Fatalf("Speak 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") {
			t.Fatalf("狼座位泄漏文本应 rejected, 实际: %q", res)
		}
		if chat.sendFromBotN != 0 {
			t.Fatalf("狼座位泄漏文本不应广播, sendFromBotN=%d", chat.sendFromBotN)
		}
	})
	t.Run("SpeakAuto", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleWerewolf)
		res, err := r.SpeakAuto(r11LeakText)
		if err != nil {
			t.Fatalf("SpeakAuto 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") || chat.sendFromBotN != 0 {
			t.Fatalf("SpeakAuto 应 reject 且不广播: res=%q sendN=%d", res, chat.sendFromBotN)
		}
	})
	t.Run("SpeakWithThought", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleWerewolf)
		res, err := r.SpeakWithThought(r11LeakText, "<internal>")
		if err != nil {
			t.Fatalf("SpeakWithThought 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") || chat.sendFromBotN != 0 {
			t.Fatalf("SpeakWithThought 应 reject 且不广播: res=%q sendN=%d", res, chat.sendFromBotN)
		}
	})
	t.Run("Interject", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleWerewolf)
		res, err := r.Interject(r11LeakText)
		if err != nil {
			t.Fatalf("Interject 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") || chat.sendInterjectN != 0 {
			t.Fatalf("Interject 应 reject 且不广播: res=%q sendN=%d", res, chat.sendInterjectN)
		}
	})
}

// TestWolfGuard_R11_GoodFactionAllowed 阵营门控:好人座位说同样的话不拦截
// (不构成真实刀口泄漏,R132 阵营叙事合法化保持有效)。
func TestWolfGuard_R11_GoodFactionAllowed(t *testing.T) {
	r, chat := wolfGuardRunner(t, RoleVillager)
	res, err := r.Speak(r11LeakText)
	if err != nil {
		t.Fatalf("Speak 不应返回 error: %v", err)
	}
	if strings.Contains(res, "rejected") {
		t.Fatalf("好人座位不应被狼队守卫拦截, 实际: %q", res)
	}
	if chat.sendFromBotN != 1 {
		t.Fatalf("好人座位文本应正常广播, sendFromBotN=%d", chat.sendFromBotN)
	}
}

// TestWolfGuard_R11_WolfNormalSpeak 狼座位的正常发言不受影响。
func TestWolfGuard_R11_WolfNormalSpeak(t *testing.T) {
	r, chat := wolfGuardRunner(t, RoleWerewolf)
	res, err := r.Speak("我怀疑7号是狼人,建议投他")
	if err != nil {
		t.Fatalf("Speak 不应返回 error: %v", err)
	}
	if strings.Contains(res, "rejected") || chat.sendFromBotN != 1 {
		t.Fatalf("狼座位正常发言应广播: res=%q sendN=%d", res, chat.sendFromBotN)
	}
}

// TestWolfGuard_R11_ScrubStreamPath 流式 delta / internal_thought 旁路
// (ScrubIdentityLeak)同样覆盖 R11 称谓句式 — 纵深防御。
func TestWolfGuard_R11_ScrubStreamPath(t *testing.T) {
	cases := []string{
		"狼人队友，今晚先刀7号",
		"队友们，今晚刀9号",
		"自己人，准备干掉3号",
	}
	for _, s := range cases {
		out, hit := ScrubIdentityLeak(s)
		if !hit {
			t.Errorf("ScrubIdentityLeak 应命中 R11 句式: %q", s)
		}
		if hit && !strings.Contains(out, "[已过滤]") {
			t.Errorf("ScrubIdentityLeak 命中后应含 [已过滤]: %q → %q", s, out)
		}
	}
}

// TestWolfGuard_R11_UnknownRoleDegraded 未发牌(RoleUnknown)降级不拦截。
func TestWolfGuard_R11_UnknownRoleDegraded(t *testing.T) {
	r, chat := wolfGuardRunner(t, RoleUnknown)
	res, err := r.Speak(r11LeakText)
	if err != nil {
		t.Fatalf("Speak 不应返回 error: %v", err)
	}
	if strings.Contains(res, "rejected") || chat.sendFromBotN != 1 {
		t.Fatalf("未发牌阶段应降级放行: res=%q sendN=%d", res, chat.sendFromBotN)
	}
}

// ────────────────────────────────────────────────────────────────────
// BUG-R13-NEW-001 (2026-08-17) 神职未来时计划公屏泄漏 hard-reject 回归
// ────────────────────────────────────────────────────────────────────

// R13 原句(Bot 4 号 MiniMax M3 警长竞选阶段实测):
// 「今晚查 11 号。理由:发言模板化强、像悍跳预言家。如果查到狼,明天对跳抢
// 警徽;如果查到金水,11 号是真预言家或高玩好人,我跟随站边。」

// TestFutureTenseSkillPlan_R13_Positive 实测泄漏句式必须全部命中。
func TestFutureTenseSkillPlan_R13_Positive(t *testing.T) {
	cases := []string{
		// R13 原句(Bot 4 号 完整原话)
		"今晚查 11 号。理由:发言模板化强、像悍跳预言家。",
		"今晚查 11 号",
		// 其它神职动词家族
		"今晚守 7 号",
		"明晚验 3 号",
		"今夜解 2 号",
		"明晚毒 4 号",
		"今晚守护 5 号",
		// 第一人称+动词
		"我今晚查 11 号",
		"我准备验 4 号",
		"我打算守 7 号",
		"我会查 3 号",
		"我准备解 2 号",
		"我决定毒 5 号",
	}
	for _, s := range cases {
		if cat, hit := CheckFutureTenseSkillPlan(s); !hit {
			t.Errorf("应命中但未命中: %q", s)
		} else {
			t.Logf("命中 %q → pattern=%s", s, cat)
		}
	}
}

// TestFutureTenseSkillPlan_R13_Negative 合法发言不得误杀。
// 「过去时 + 查验/用药/守护」是真预言家/女巫/守卫的标准播报句式,
// 必须放行(否则本守卫会把所有真神职发言都拦掉)。
func TestFutureTenseSkillPlan_R13_Negative(t *testing.T) {
	cases := []string{
		// 过去时 — 真神职标准播报
		"昨夜我查验了 4 号,结果是狼人",
		"昨夜我守了 7 号",
		"昨夜我用了毒药",
		"昨夜我解了 2 号",
		"我昨晚查到 4 号是金水",
		// 白天合法讨论
		"我投 7 号",
		"我建议投死 2 号",
		"我怀疑 4 号是狼人",
		// 悍跳预言家
		"我是预言家,4 号是我查杀",
		"我是女巫,昨夜用了解药救了 3 号",
		// 元讨论
		"狼人杀这个游戏太狠了",
		"今晚的月亮真圆",
		// 非神职动词 + 数字号
		"我查了 3 号的发言记录",
		"7 号这人不错",
	}
	for _, s := range cases {
		if cat, hit := CheckFutureTenseSkillPlan(s); hit {
			t.Errorf("误杀合法发言: %q (pattern=%s)", s, cat)
		}
	}
}

// TestFutureTenseSkillPlan_R13_AllFactionsRejected 4 条广播路径全部
// 接线 hard-reject(不门控阵营,预言家/女巫/守卫/狼悍跳神职都可能误用)。
// 使用好人(平民)角色验证「不门控 isWolfSeat」生效。
func TestFutureTenseSkillPlan_R13_AllFactionsRejected(t *testing.T) {
	const leakText = "今晚查 11 号。理由:发言模板化强。"
	t.Run("Speak_Villager", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleVillager)
		res, err := r.Speak(leakText)
		if err != nil {
			t.Fatalf("Speak 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") {
			t.Fatalf("好人座位未来时泄漏应 rejected, 实际: %q", res)
		}
		if chat.sendFromBotN != 0 {
			t.Fatalf("好人座位未来时泄漏不应广播, sendFromBotN=%d", chat.sendFromBotN)
		}
	})
	t.Run("SpeakAuto_Villager", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleVillager)
		res, err := r.SpeakAuto(leakText)
		if err != nil {
			t.Fatalf("SpeakAuto 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") || chat.sendFromBotN != 0 {
			t.Fatalf("SpeakAuto 应 reject 且不广播: res=%q sendN=%d", res, chat.sendFromBotN)
		}
	})
	t.Run("SpeakWithThought_Villager", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleVillager)
		res, err := r.SpeakWithThought(leakText, "<internal>")
		if err != nil {
			t.Fatalf("SpeakWithThought 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") || chat.sendFromBotN != 0 {
			t.Fatalf("SpeakWithThought 应 reject 且不广播: res=%q sendN=%d", res, chat.sendFromBotN)
		}
	})
	t.Run("Interject_Villager", func(t *testing.T) {
		r, chat := wolfGuardRunner(t, RoleVillager)
		res, err := r.Interject(leakText)
		if err != nil {
			t.Fatalf("Interject 不应返回 error: %v", err)
		}
		if !strings.Contains(res, "rejected") || chat.sendInterjectN != 0 {
			t.Fatalf("Interject 应 reject 且不广播: res=%q sendN=%d", res, chat.sendInterjectN)
		}
	})
}

// TestFutureTenseSkillPlan_R13_SeerAllowed 真正的预言家过去时播报不受影响。
// 真预言家「昨夜我查验了 X 号,结果是 X 色」是 R132 心理战合法化后的标准
// 播报句式,必须放行,否则本守卫会把所有真神职发言都拦掉。
func TestFutureTenseSkillPlan_R13_SeerAllowed(t *testing.T) {
	r, chat := wolfGuardRunner(t, RoleSeer)
	res, err := r.Speak("昨夜我查验了 4 号,结果是狼人。11 号给我金水,我跟他走。")
	if err != nil {
		t.Fatalf("Speak 不应返回 error: %v", err)
	}
	if strings.Contains(res, "rejected") {
		t.Fatalf("真预言家过去时播报应放行, 实际: %q", res)
	}
	if chat.sendFromBotN != 1 {
		t.Fatalf("真预言家过去时播报应广播, sendFromBotN=%d", chat.sendFromBotN)
	}
}
