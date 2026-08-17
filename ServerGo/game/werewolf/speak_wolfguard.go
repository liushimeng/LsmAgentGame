// Package werewolf — speak_wolfguard.go: BUG-R11-001 (2026-08-17) 狼队内沟通
// 公屏泄漏 hard-reject 守卫。
//
// 背景:R11 自动化测试(2026-08-17 20:10,D1 遗言阶段)实测 Bot 9 号
// (美团 LongCat-2.0) 在公屏发言:
//
//	「狼人队友，今晚先刀7号。他拿了警徽今晚要验8号，刀了他好人节奏就乱了,
//	  10号金水也站不住。」
//
// 该发言同时暴露狼人阵营身份(「狼人队友」)+ 当晚击杀意图(「今晚先刀7号」)
// + 战略动机,直接违反 §133 WolfPackRoom 协议层隔离设计(狼队内沟通只走
// wolf_whisper,绝不进 chat_message / 公屏)。
//
// 根因(两层):
//
//  1. R132 (2026-07-16)「公屏猜疑化」重构把「狼队-击杀意图」类规则在
//     MysteryMaskText 中定为 MysteryAllow(原文放行,仅给 LLM 风险提示),
//     主发言路径(Speak/SpeakAuto/SpeakWithThought/Interject)自 R132 起
//     不再调用 ScrubIdentityLeak;
//  2. §R10-NEW3 (2026-08-16) 把「我/咱/咱们 + 建议/觉得 + 刀/杀 + X号」
//     regex 只加进了 ScrubIdentityLeak.identityLeakPatterns —— 该列表在主
//     发言路径上已是死代码(§130「声明了却从不接线」第 N 次复现),仅影响
//     流式 delta 与 internal_thought 两条旁路。R11 报告「R10-NEW3 修复保持
//     有效」是无法验证的假阳性(本轮无 bot 产出该句式)。
//
// 修复设计:
//
//   - 与 R93-P1 (death-fact) / R151-FAIRNESS (whisper-attribution) 同范式的
//     hard-reject:命中的发言**整条不广播**,真人观战者看不到任何 marker;
//     LLM 收到 reject hint,引导其改用 wolf_whisper 工具;
//   - **仅对狼阵营 bot 生效**(isWolfSeat 门控):好人 bot 说「我建议刀X号」
//     不构成本质泄漏(无真实刀口信息),且 R132 已把阵营叙事合法化为心理战,
//     门控避免误杀好人修辞;
//   - 动词表刻意**不含**「投死」(白天投票是公开合法行为)与第一人称条目中的
//     「带走」(狼悍跳猎人「我死后带走X号」是 R132 合法心理战);
//   - 接线位置:四条公屏广播路径 MysteryMaskText 之后、SendFromBot 之前,
//     与 R93-P1 同位。
//
// 与 mysteryRules「狼队-击杀意图」(MysteryAllow) 的关系:该规则保留 —
// 非狼 bot 命中时仍按 R132 心理战哲学放行;狼 bot 在广播前已被本守卫
// hard-reject,永远走不到广播。
package werewolf

import (
	"regexp"
	"time"
)

// wolfCoordinationLeakPattern 单条狼队协调泄漏模式。
type wolfCoordinationLeakPattern struct {
	Name string
	Re   *regexp.Regexp
}

// wolfCoordinationLeakPatterns 狼队内沟通公屏泄漏模式表(仅对狼阵营 bot 生效)。
//
// 误杀防护要点:
//   - 「投死」不在动词表 — 白天投票讨论(「我建议大家投2号」)是全员合法行为;
//   - 第一人称条目不含「带走」 — 狼悍跳猎人「我死后带走X号」是合法心理战;
//   - 「队友」裸词不在称谓表(好人也说「我们队友」),只列狼阵营内部称谓
//     (狼人队友/狼队友/狼同伴/狼友/同伙/自己人/我们的人/队友们/同伴们);
//   - 「刀口」抽象表达不匹配(「刀口可能在7号」无 刀+数字号 直连结构),
//     给狼留合法的夜间局势讨论空间。
var wolfCoordinationLeakPatterns = []wolfCoordinationLeakPattern{
	{
		// BUG-R11-001 (2026-08-17): 狼阵营称谓 + ≤12 字符 + 可选时间/软化词
		// + 击杀动词 + X号。R11 实测原句「狼人队友，今晚先刀7号」— §R10-NEW3
		// 的「我/咱/咱们」第一人称锚定不覆盖第二人称复数称谓句式。
		Name: "wolf_addr_kill",
		Re: regexp.MustCompile(`(?:狼人队友|狼队友|狼同伴|狼友|同伙|同伙们|自己人|我们的人|队友们|同伴们|兄弟们)` +
			`(?:[\s\S]{0,12}?)` +
			`(?:(?:今晚|今夜|明晚|夜里|夜间|明天|等会|马上|先|优先|准备|打算|计划)\s*){0,3}` +
			`(?:刀|杀|干掉|带走|投死|咬|撕)\s*\d+\s*号`),
	},
	{
		// R91-P0-1 (2026-07-11): 第一人称复数 + 夜间时间词 + 击杀动词 + X号。
		// 「我们今晚先刀7号」「咱们夜里杀3号」。时间词**必选**,把
		// 「我们投X号」等白天合法讨论排除在外。
		Name: "wolf_plural_night_kill",
		Re: regexp.MustCompile(`(?:我们|咱们)\s*(?:今晚|今夜|明晚|夜里|夜间)\s*` +
			`(?:先|准备|打算|计划|就|优先)?\s*(?:刀|杀|干掉|带走|咬|撕)\s*\d+\s*号`),
	},
	{
		// R91-P0-1 / BUG-R11-001: 夜间时间词开头 + 软化词 + 击杀动词 + X号。
		// 「今晚先刀7号」「今夜目标杀3号」。狼公屏推测刀口(「我觉得今晚刀7号
		// 概率大」含「今晚刀7号」子串)同样命中 — 狼掌握真实刀口,公屏复述
		// 即软泄漏,按 R10/R11 先例从严。
		Name: "wolf_tonight_kill",
		Re: regexp.MustCompile(`(?:今晚|今夜|明晚)\s*(?:先|目标|计划|打算|就|优先|准备)?\s*` +
			`(?:刀|杀|干掉|带走|咬|撕)\s*\d+\s*号`),
	},
	{
		// §R10-NEW3 (2026-08-16) 主路径补接线: 第一人称 + ≤8 字符 + 软建议词
		// + 击杀动词 + X号。R10 实测「我是1号。建议刀8号」「我觉得刀1号比较合适」。
		// 与 speak_filter.go:99 的 ScrubIdentityLeak 版差异:动词表去掉
		// 「带走」(悍跳猎人 FP) 与「投死」(白天投票合法)。
		Name: "wolf_suggest_kill",
		Re: regexp.MustCompile(`(?:我|咱|咱们)(?:[\s\S]{0,8}?)\s*` +
			`(?:(?:建议|觉得|想|打算|准备|决定|应该|要|先|提前|马上)\s*)*` +
			`(?:刀|杀|干掉|咬|撕)\s*\d+\s*号`),
	},
}

// CheckWolfCoordinationLeak 检测公屏发言是否包含狼队内沟通语义。
// 返回 (命中模式名, 是否命中)。纯文本函数,阵营门控由调用方
// (agentRunner.isWolfSeat) 完成。
func CheckWolfCoordinationLeak(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	for _, p := range wolfCoordinationLeakPatterns {
		if p.Re.MatchString(text) {
			return p.Name, true
		}
	}
	return "", false
}

// isWolfSeat 报告本 runner 座位是否狼阵营。走 lockRoomBriefly(200ms)短时
// 持锁读权威 Roles;锁竞争失败 / 房间不存在 / State 未初始化(未发牌)时
// 降级返回 false(不拦截),与 getAuthoritativeDeathsAndAlive 的防御性降级
// 一致 — 锁竞争不应阻塞玩家发言。
//
// 注意 Roles 零值为 RoleUnknown(FactionOf→FactionUnknown),未发牌阶段
// 不会误判为狼。
func (r *agentRunner) isWolfSeat() bool {
	if r.mgr == nil {
		return false
	}
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok || mgrRoom == nil {
		return false
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return false
	}
	defer mgrRoom.mu.Unlock()
	if mgrRoom.State == nil {
		return false
	}
	return FactionOf(mgrRoom.State.Roles[r.seat]) == FactionWolf
}

// wolfCoordinationRejectHint 是 hard-reject 时返回给 LLM 的引导文案。
// 各广播路径在返回值前拼接各自的动作名前缀(「speak rejected: ...」)。
const wolfCoordinationRejectHint = "你的发言包含狼队击杀协调语义(狼队友称谓/夜间时间+刀杀目标)," +
	"公屏发出会立即暴露你与队友的阵营 — 队内协调请改用 wolf_whisper 工具(仅狼队友可见);" +
	"公屏讨论夜间局势请用「刀口可能在 X 号」这类不确认信息的抽象表达,白天请用「投 X 号」「怀疑 X 号」"
