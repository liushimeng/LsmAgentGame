// Package werewolf — agent_runner_whisper.go: agentRunner 的私聊通道工具
// 实现组,从 agent_runner.go 纯搬移拆出(CLAUDE.md §4 单文件 ≤ 1800 行,
// §20260821-03;白名单只允许变短)。
//
// 包含 4 个「私语/插话」通道方法:
//   - WolfWhisper    : 狼队密语(WolfPack 通道,§119 协议层隔离)
//   - WolfpackAssign : 狼队分工重排 + 暗号模式(§20260810-10 / §20260811-04)
//   - Whisper        : bot → 座位 私聊(跨阵营拦截,BUG-R194-001)
//   - Interject      : 发言阶段插话(非当前发言人)
//
// 本文件所有方法都是 agentRunner 的方法;类型定义与其余工具实现仍在
// agent_runner.go。同 package 跨文件 = 纯代码搬移,零逻辑改动。
package werewolf

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// WolfWhisper 把留言推入狼小队交流通道（v4 §13.1）。
// 实现 wwplayer.WolfWhisperRunner 接口(供 DispatchTool 派发)。
//
// 协议层硬约束（与 §119 HeartThought 一致）：
//   - 留言**不**进入 chat_message 表 / chat_history 队列 / BotTranscript.HeartThought；
//   - 不广播给人类玩家、不广播给观众；
//   - 仅狼 bot 在下一轮 user prompt 中通过 WolfPackSnapshot 看到。
//
// 错误返回（被 tool_result 反馈给 LLM）：
//   - "wolf_whisper rejected: room not found" — 房间不存在。
//   - "wolf_whisper rejected: wolfpack not initialized" — 房间未初始化 WolfPackRoom。
//   - "wolf_whisper rejected: not a wolf member" — 发送者非狼人成员（双重防御）。
//   - "wolf_whisper rejected: message too long" — text 超过 80 字。
func (r *agentRunner) WolfWhisper(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "wolf_whisper rejected: text required", nil
	}
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return "wolf_whisper rejected: room not found", nil
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return "wolf_whisper rejected: lock contention", nil
	}
	if mgrRoom.wolfPack == nil {
		mgrRoom.mu.Unlock()
		return "wolf_whisper rejected: wolfpack not initialized", nil
	}
	// 双重防御：只有 seats[r.seat] 是狼人时才能写。
	if r.seat < 0 || int(r.seat) >= len(mgrRoom.State.Roles) ||
		mgrRoom.State.Roles[r.seat] != RoleWerewolf {
		mgrRoom.mu.Unlock()
		return "wolf_whisper rejected: not a wolf member", nil
	}
	if len([]rune(text)) > WolfPackMsgLenMax {
		mgrRoom.mu.Unlock()
		return fmt.Sprintf("wolf_whisper rejected: message too long (max %d chars)", WolfPackMsgLenMax), nil
	}
	err := mgrRoom.wolfPack.Append(int(r.seat), r.botUserID, text)
	if err == nil {
		// 2026-08-10 §20260810-05 — 信息账本:狼队密语仅存活狼座位知情。
		// 在 mgrRoom.mu 持锁态登记(§92a);text 经 redactLedgerFact 剔除身份明文。
		mgrRoom.ledgerAppendLocked(InfoSourceWolfPack,
			fmt.Sprintf("wolf_pack from=%d text=%s", int(r.seat), text),
			aliveWolfKnowerSetLocked(mgrRoom), time.Now().UnixMilli())
	}
	mgrRoom.mu.Unlock()
	if err != nil {
		if errors.Is(err, ErrWolfPackNotMember) {
			return "wolf_whisper rejected: not a wolf member", nil
		}
		if errors.Is(err, ErrWolfPackMsgTooLong) {
			return fmt.Sprintf("wolf_whisper rejected: message too long (max %d chars)", WolfPackMsgLenMax), nil
		}
		return fmt.Sprintf("wolf_whisper rejected: %s", err.Error()), nil
	}
	return fmt.Sprintf("wolf_whisper ✓ 留言已加入狼小队(%d字)", len([]rune(text))), nil
}

// WolfpackAssign 是 §20260810-10 U1 新增的「狼队分工重排」工具实现。
// 仅当前轮值狼王可调用;把自己的分工改为 newRole(若被占用则互换),
// 并以系统留言(FromSeat=WolfRoleSystemSeat)写入 WolfPackRoom 让全狼可见。
//
// §20260811-04 U1 — 追加 cipherMode 可选参数(starter/advanced/"")
//   - "starter"  → 装 2 模板(target_position + fake_seer_posture)
//   - "advanced" → 装 4 模板(全部)
//   - ""         → 关闭暗号,纯 wolf_whisper(默认,零回归)
//
// 暗号模板仅狼 bot GameContext 可见(§119 协议层隔离)。
//
// 协议层隔离(§119):系统留言与 wolf_whisper 同级 — 不进 chat 表/队列/HeartThought。
// 错误返回(rejected 文案,不计入 consecutiveFailures):
//   - "wolfpack_assign rejected: room not found / lock contention / wolfpack not initialized"
//   - "wolfpack_assign rejected: not a wolf member" — 非狼(双重防御)
//   - "wolfpack_assign rejected: only the wolf king ..." — 非轮值狼王
//   - "wolfpack_assign rejected: invalid role ..." — 非法分工枚举
//   - "wolfpack_assign rejected: invalid cipher mode ..." — 非法暗号模式
func (r *agentRunner) WolfpackAssign(newRole string, cipherMode string) (string, error) {
	newRole = strings.TrimSpace(strings.ToLower(newRole))
	cipherMode = strings.TrimSpace(strings.ToLower(cipherMode))
	// cipherMode 校验(只有三个合法值,其他一律按 "" 关闭暗号)。
	if cipherMode != "" && cipherMode != CipherModeStarter && cipherMode != CipherModeAdvanced {
		return fmt.Sprintf("wolfpack_assign rejected: invalid cipher mode %q (want starter/advanced/empty)", cipherMode), nil
	}
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return "wolfpack_assign rejected: room not found", nil
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return "wolfpack_assign rejected: lock contention", nil
	}
	defer mgrRoom.mu.Unlock()
	if mgrRoom.wolfPack == nil {
		return "wolfpack_assign rejected: wolfpack not initialized", nil
	}
	// 双重防御:座位必须是狼人(与 WolfWhisper 同校验)。
	if r.seat < 0 || int(r.seat) >= len(mgrRoom.State.Roles) ||
		mgrRoom.State.Roles[r.seat] != RoleWerewolf {
		return "wolfpack_assign rejected: not a wolf member", nil
	}
	oldRole, err := mgrRoom.wolfPack.AssignRole(int(r.seat), newRole)
	if err != nil {
		return fmt.Sprintf("wolfpack_assign rejected: %s", err.Error()), nil
	}
	cipherSummary := ""
	if cipherMode != "" {
		// §20260811-04 U1 — 装入暗号模板(协议层隔离,只入狼 bot GameContext)。
		templates := CipherTemplatesForMode(cipherMode)
		day := mgrRoom.State.DayNumber
		if day < 1 {
			day = 1
		}
		bundle := CipherBundle{
			Seat:      int(r.seat),
			Day:       day,
			Templates: templates,
		}
		mgrRoom.cipherLocked().Set(int(r.seat), bundle)
		cipherSummary = fmt.Sprintf(" + 🔐 暗号模式 %s(%d 模板)", cipherMode, len(templates))
	}
	if oldRole == newRole && cipherSummary == "" {
		return fmt.Sprintf("wolfpack_assign ✓ 分工保持【%s】(未变更)", WolfRoleLabel(newRole)), nil
	}
	// 系统留言让全狼在下一轮 user prompt 的 WolfPackSnapshot 看到分工变更。
	// FromSeat=WolfRoleSystemSeat 标识系统消息(非任何真实玩家)。
	sysText := fmt.Sprintf("狼王 %d号 把自己的分工从【%s】改为【%s】%s",
		int(r.seat)+1, WolfRoleLabel(oldRole), WolfRoleLabel(newRole), cipherSummary)
	_ = mgrRoom.wolfPack.Append(WolfRoleSystemSeat, "system", sysText)
	// 信息账本:分工变更仅存活狼知情(与 wolf_whisper 同级)。
	mgrRoom.ledgerAppendLocked(InfoSourceWolfPack,
		fmt.Sprintf("wolf_role_assign king=%d old=%s new=%s cipher=%s", int(r.seat), oldRole, newRole, cipherMode),
		aliveWolfKnowerSetLocked(mgrRoom), time.Now().UnixMilli())
	return fmt.Sprintf("wolfpack_assign ✓ 你的分工:【%s】→【%s】%s", WolfRoleLabel(oldRole), WolfRoleLabel(newRole), cipherSummary), nil
}

// Whisper sends a private message from a bot to another seat. The recipient
// is recorded in the per-seat whisper inbox and the sender is excluded from
// the redacted room broadcast (see ChatService.WhisperFromBot).
//
// BUG-R194-001 (2026-07-24): 之前任何 bot 都可向任何 alive 座位 whisper,导致
// 狼人 Agent 经常把狼队协同信息(刀型/悍跳顺序/票型集中)误发给人类或好人阵营,
// 公屏 UI 以「🔒 私聊→ test_01」形式泄露给所有人,严重破坏对局公平性。
// 修复:服务端按阵营拦截跨阵营 whisper——
//   - 狼 → 狼: 允许(本就是工具设计的狼人夜间战术会议场景)
//   - 狼 → 好人(含人类/神职/平民): 拒绝,引导 Agent 改用 wolf_whisper
//   - 好人 → 好人: 允许(预言家↔女巫等同伴沟通的合法场景)
//   - 好人 → 狼: 拒绝(避免好人被狼反串骗取信息)
//
// 注意:跨阵营 whisper 被服务端硬拒后,狼 Agent 的 wolf_whisper 工具
// (v4 §13.1)仍是其唯一合规的狼队内部通道;人类/好人 Agent 完全不受影响。
func (r *agentRunner) Whisper(toSeat int, text string) (string, error) {
	// 2026-07-24 修复:self/invalid 校验必须在 chatSvc==nil 之前,
	// 否则 stub 测试(r.chatSvc=nil)永远拿不到"rejected"响应,只能拿到
	// "chat unavailable",无法验证防御逻辑。
	if toSeat < 0 || toSeat >= MaxPlayers {
		return fmt.Sprintf("whisper rejected: invalid seat %d", toSeat), nil
	}
	if r.seat == Seat(toSeat) {
		return "whisper rejected: cannot whisper to self", nil
	}
	// 阵营一致性校验:BUG-R194-001 防御层 — 狼 bot 只能 whisper 给狼 bot,
	// 好人 bot 只能 whisper 给好人 bot;否则拒绝并提示走 wolf_whisper。
	// 先于 chatSvc 检查,这样即便 chatSvc 未注入也能保证跨阵营硬拒语义。
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok || mgrRoom == nil || mgrRoom.State == nil {
		return "whisper rejected: room or game state unavailable", nil
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return "whisper rejected: lock contention", nil
	}
	// 双重防御:不仅检查自身阵营,也校验目标阵营 — 防止 wolf_whisper
	// 路径外仍然向好人泄密。
	myRole := mgrRoom.State.Roles[r.seat]
	myFaction := FactionOf(myRole)
	targetRole := mgrRoom.State.Roles[toSeat]
	targetFaction := FactionOf(targetRole)
	mgrRoom.mu.Unlock()
	if myFaction == FactionUnknown || targetFaction == FactionUnknown {
		return "whisper rejected: faction not initialized", nil
	}
	if myFaction != targetFaction {
		// 跨阵营 whisper 被服务端硬拒。
		// 引导语区分两类场景:狼→好人提示用 wolf_whisper;好人→狼无合法替代,
		// 直接拒绝。
		if myFaction == FactionWolf {
			return "whisper rejected: cross-faction (wolf→good) not allowed; use wolf_whisper for wolf team coordination", nil
		}
		return "whisper rejected: cross-faction (good→wolf) not allowed", nil
	}
	if r.chatSvc == nil {
		return "chat unavailable", nil
	}
	// Resolve the target seat's userID from the manager's Seats snapshot.
	seats, ok := r.mgr.Seats(r.roomID)
	if !ok {
		return "room not found", nil
	}
	toUserID := seats[toSeat]
	if toUserID == "" {
		return fmt.Sprintf("seat %d empty", toSeat), nil
	}
	// 2026-08-05 §Agent聊天显示优化 (B7, 修复 P1-2):Whisper 此前**一道过滤都没有**,
	// LLM 的 <thinking> 残留标签 / 纯空白会原样进私聊并落库。这里补齐与 Speak /
	// Interject 对齐的两道最小过滤:
	//   1. StripLLMInternalTags — 清除 XML/HTML 内部标签泄露(R91-P0-2 同源);
	//   2. 空文本硬拒 — 过滤后为空则不消耗任何配额直接拒绝(R233-P1-01 同源)。
	// **不加** dedup / rate-limit:私聊是点对点定向沟通,与公屏刷屏语义不同,
	// 加去重会误杀「向不同人重复确认同一件事」这类合法用法。
	if cleaned, hit := StripLLMInternalTags(text); hit {
		logger.L().Info("agentRunner.Whisper internal-tag stripped",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.Int("to_seat", toSeat))
		text = cleaned
	}
	if strings.TrimSpace(text) == "" {
		logger.L().Info("agentRunner.Whisper blank-text rejected",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.Int("to_seat", toSeat))
		return "whisper rejected: 过滤后内容为空(LLM 仅产出空白/标签),请重新组织有效私聊内容或改用 idle_silent", nil
	}
	_, err := r.chatSvc.WhisperFromBot(r.roomID, r.botUserID, r.botAccount, r.modelKey, toUserID, "", text)
	if err != nil {
		return "whisper failed: " + err.Error(), err
	}
	// 2026-08-05 §Agent聊天显示优化 (B4):私聊**只记事件不记原文** —— text 传 ""。
	// 私聊原文只对收发双方可见,而 LastSpeech 是全房可见字段;这里仅让座位卡
	// 能显示「刚刚发了一条私聊」的时间与类型,不泄露一个字。
	r.recordLastSpeech("", "whisper")
	return "sent", nil
}

// Interject broadcasts a "non-current-speaker" chat message during the speak
// phase. BUG-WEREWOLF-AGENT-INTERJECT: lets any alive bot chime in
// voluntarily (follow-up question, banter, mild challenge) without being the
// formal speaker. Routed through ChatService.SendInterjectFromBot which sets
// is_interject=true on the wire envelope so the UI can render it as 💬插话
// distinct from the formal speak broadcast.
//
// Throttle is enforced upstream by Agent.Limiter (30s interval, shared with
// speak). This method does NOT re-check phase — BuildTools only exposes
// `interject` during PhaseSpeak, so a non-speak dispatch is gated by the
// tool catalog itself.
func (r *agentRunner) Interject(text string) (string, error) {
	if r.chatSvc == nil {
		return "chat unavailable", nil
	}
	// BUG-R74-1 (2026-07-09): interject 与 speak 共用 45s 限流桶。
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil && !r.speakLimiter.Allow() {
		logger.L().Warn("agentRunner.Interject rate-limited",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "interject rate-limited: 距上次公开发言不足 45s", nil
	}
	// R76 P1-3 (2026-07-10): 独立 InterjectLimiter — 60s 间隔 + 5min/4 条
	// 软上限,解决 MiniMax #6 单 bot 一局 7+ 插话刷屏(Mark/Allow 通过
	// Agent.AllowInterject/MarkInterject,直接读结构体,无需额外注入)。
	if r.agent != nil && !r.agent.AllowInterject(time.Now()) {
		logger.L().Warn("agentRunner.Interject rate-limited (per-bot interject quota)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("reason", "5min/4条 或 60s 间隔"))
		return "interject rate-limited: 单 bot 插话频率超限(60s 间隔 / 5min 最多 4 条)", nil
	}
	// R132 (2026-07-16)「公屏猜疑化」:同 Speak。
	if r.filterCfg.EnableIdentityFilter {
		if res := MysteryMaskText(text); res.Hit {
			logger.L().Info("agentRunner.Interject mystery-mask hit",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("original", text), zap.String("masked", res.Text),
				zap.String("mode", res.Mode.String()),
				zap.Strings("categories", res.HitCategories))
			text = res.Text
			if hint := ComposeMysteryHint(res); hint != "" {
				r.lastMysteryHint = hint
			}
		}
	}
	// BUG-R11-001 (2026-08-17): 狼队内沟通公屏泄漏 hard-reject,与 Speak 同源。
	if r.isWolfSeat() {
		if cat, hit := CheckWolfCoordinationLeak(text); hit {
			logger.L().Info("agentRunner.Interject wolf-coordination rejected (R11-001)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("pattern", cat), zap.String("rejected", text))
			return "interject rejected: " + wolfCoordinationRejectHint, nil
		}
	}
	// BUG-R13-NEW-001 (2026-08-17): 神职未来时计划 hard-reject,与 Speak 同源。
	if cat, hit := CheckFutureTenseSkillPlan(text); hit {
		logger.L().Info("agentRunner.Interject future-tense-skill-plan rejected (R13-NEW-001)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("pattern", cat), zap.String("rejected", text))
		return "interject rejected: " + futureTenseSkillPlanRejectHint, nil
	}
	// BUG-R158-FAIRNESS-001 (2026-07-19): 反私聊内容幻觉事实校验补全。
	// R151 (commit 7451782) 仅在 Speak() 通道过 FactCheckWhisperAttribution,
	// 遗漏了 SpeakAuto / SpeakWithThought / Interject 三条同样调 chatSvc
	// 广播的路径。本轮 R158 测试在 spectator 视图直接看到 Bot 通过
	// Interject 绕过 filter 公屏捏造「X号 私聊我指挥刀Y号」并成功广播,
	// 完全复现 R151 修复前的问题。修复:在 SendInterjectFromBot 前过
	// FactCheckWhisperAttribution, 与 Speak 一致。Interject 没有 R93-P1
	// death-fact 校验(短文本插话一般不含死亡声明), 这里直接接 R91-P0-2
	// 之前。
	if inbox := r.getAuthoritativeWhisperInbox(); inbox != nil {
		if _, hit := wwplayer.FactCheckWhisperAttribution(text, inbox); hit {
			logger.L().Info("agentRunner.Interject whisper-attribution rejected (R158-FAIRNESS)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("rejected", text))
			return "interject rejected: 引用了从未收到过的私聊内容,请仅基于 user prompt 中【发给你的私聊】段引用真实私聊;若要指控他人请改用公开行为/公开发言,不要捏造不存在的私聊", nil
		}
	}
	// R91-P0-2 (2026-07-11): HTML/XML 标签泄露防护 — 与 Speak / SpeakWithThought 共享。
	if cleaned, hit := StripLLMInternalTags(text); hit {
		logger.L().Info("agentRunner.Interject internal-tag stripped",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("original", text), zap.String("cleaned", cleaned))
		text = cleaned
	}
	// BUG-R233-P1-01 (2026-08-02): 见 SpeakAuto 同源注释。
	if strings.TrimSpace(text) == "" {
		logger.L().Info("agentRunner.Interject blank-text rejected (R233-P1-01)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.Int("orig_len", len(text)))
		return "interject rejected: 过滤后内容为空(LLM 仅产出空白/标签),请重新组织有效插话或保持沉默", nil
	}
	_, err := r.chatSvc.SendInterjectFromBot(r.roomID, r.botUserID, r.botAccount, r.modelKey, text)
	if err != nil {
		return "interject failed: " + err.Error(), err
	}
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil {
		r.speakLimiter.Mark()
	}
	// R76 P1-3 (2026-07-10): 登记本次插话,后续 AllowInterject 会看到
	// interjectWindowCount++ / InterjectLimiter.Mark(),5min/60s 双门生效。
	if r.agent != nil {
		r.agent.MarkInterject(time.Now())
	}
	// 2026-08-05 §Agent聊天显示优化 (B4):插话同样是已广播的公开内容,
	// kind="interject" 让前端座位卡气泡能与正式发言区分渲染。
	r.recordLastSpeech(text, "interject")
	// R132 (2026-07-16): 拼 lastMysteryHint。
	resultText := "sent"
	if r.lastMysteryHint != "" {
		resultText += "\n" + r.lastMysteryHint
		r.lastMysteryHint = ""
	}
	return resultText, nil
}
