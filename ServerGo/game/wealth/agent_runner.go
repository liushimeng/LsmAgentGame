// Package wealth — agent_runner.go: ToolRunner 实现 + GameContext 构建
// (2026-09-14 §财商流P0)。
//
// 引擎桥:wealthplayer.Agent 通过此工具桥调用 engine.ApplyAction
// (in-process,不走 WS,与人类同一代码路径,无分叉)。Room 持锁态校验/执行/
// 记账;Agent 侧只接收结果文本。
package wealth

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/wealthplayer"
	"LsmAgentGame/agent/wealthtypes"
	"LsmAgentGame/errcode"
)

// AgentRunner 实现 wealthplayer.ToolRunner(锁内执行;调用方已不在房间锁内)。
type AgentRunner struct {
	room *WealthRoom
	seat int
}

// NewAgentRunner 构造工具桥。
func NewAgentRunner(r *WealthRoom, seat int) *AgentRunner {
	return &AgentRunner{room: r, seat: seat}
}

// BeginDecision 原子获取本月决策槽。月窗推进后,旧上下文无法再获取/执行;
// 同一座位上一轮 LLM 未结束时,新一轮 wake 直接放弃,避免排队旧决策堆叠。
func (a *AgentRunner) BeginDecision(seat, month int) error {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if seat < 0 || seat >= MaxSeats || a.room.World == nil {
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if a.room.closed || a.room.Status != StatusPlaying || a.room.Phase != PhaseActing {
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if a.room.agentDecisionActive[seat] {
		return errcode.CodeMsg(errcode.ErrWealthWrongPhase, "agent decision already active")
	}
	if month != a.room.World.Month {
		return errcode.CodeMsg(errcode.ErrWealthWrongPhase, "stale agent month")
	}
	a.room.agentDecisionActive[seat] = true
	a.room.agentDecisionMonth[seat] = month
	return nil
}

// EndDecision 释放匹配的决策槽;若释放时房间已进入更新月份,则补一次 wake,
// 避免本轮 LLM 占用槽位导致当前月 wake 被丢弃后无人再触发。
func (a *AgentRunner) EndDecision(seat, month int) {
	a.room.mu.Lock()
	matched := a.room.agentDecisionActive[seat] && a.room.agentDecisionMonth[seat] == month
	if matched {
		a.room.agentDecisionActive[seat] = false
	}
	shouldWake := matched && a.room.Status == StatusPlaying &&
		a.room.Phase == PhaseActing && a.room.World != nil &&
		a.room.World.Month > month
	a.room.mu.Unlock()
	if shouldWake {
		a.room.wakeBots()
	}
}

// checkAgentDecisionLocked 在房间锁内原子校验 expected month / active token。
func (a *AgentRunner) checkAgentDecisionLocked(seat int) error {
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if !a.room.agentDecisionActive[seat] || a.room.agentDecisionMonth[seat] != a.room.World.Month {
		return errcode.CodeMsg(errcode.ErrWealthWrongPhase, "stale agent month")
	}
	return nil
}

// ── ToolRunner 实现 ─ ──

func (a *AgentRunner) CheckState(seat int) string {
	return a.room.buildCheckStateText(seat)
}

func (a *AgentRunner) BuyAsset(seat int, asset string, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolBuyAsset, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActBuyAsset, Asset: asset, AmountCNY: amountCNY})
	})
}

func (a *AgentRunner) SellAsset(seat int, asset string, units float64) error {
	return a.apply(seat, wealthplayer.ToolSellAsset, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActSellAsset, Asset: asset, Units: units})
	})
}

func (a *AgentRunner) BuyHouse(seat int, district string, downpayRatio float64, asset string) error {
	return a.apply(seat, wealthplayer.ToolBuyHouse, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActBuyHouse, District: district, DownpayRatio: downpayRatio, Asset: asset})
	})
}

func (a *AgentRunner) TakeLoan(seat int, kind string, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolTakeLoan, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActTakeLoan, Kind: kind, AmountCNY: amountCNY})
	})
}

func (a *AgentRunner) RepayLoan(seat int, loanID string, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolRepayLoan, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActRepayLoan, LoanID: loanID, AmountCNY: amountCNY})
	})
}

// StartSideBusiness 批次20(文档2 §3)增 tier:0=中价(缺省旧行为)/1=低价/2=高价。
func (a *AgentRunner) StartSideBusiness(seat int, kind string, tier int) error {
	return a.apply(seat, wealthplayer.ToolStartSide, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActStartSide, Kind: kind, Tier: tier})
	})
}

func (a *AgentRunner) StopSideBusiness(seat int) error {
	return a.apply(seat, wealthplayer.ToolStopSide, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActStopSide})
	})
}

// SetSidePrice 副业改价(批次20 文档2 §3;走 ApplyAction 与人类同一路径)。
func (a *AgentRunner) SetSidePrice(seat int, tier int) error {
	return a.apply(seat, wealthplayer.ToolSetSidePrice, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActSetSidePrice, Tier: tier})
	})
}

func (a *AgentRunner) Study(seat int) error {
	return a.apply(seat, wealthplayer.ToolStudy, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActStudy})
	})
}

func (a *AgentRunner) Socialize(seat int) error {
	return a.apply(seat, wealthplayer.ToolSocialize, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActSocialize})
	})
}

func (a *AgentRunner) Rest(seat int) error {
	return a.apply(seat, wealthplayer.ToolRest, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActRest})
	})
}

func (a *AgentRunner) WorkOvertime(seat int) error {
	return a.apply(seat, wealthplayer.ToolWorkOvertime, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActWorkOvertime})
	})
}

func (a *AgentRunner) MoveDistrict(seat int, district string) error {
	return a.apply(seat, wealthplayer.ToolMoveDistrict, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActMoveDistrict, District: district})
	})
}

func (a *AgentRunner) Consume(seat int, amountCNY int64, reason string) error {
	return a.apply(seat, wealthplayer.ToolConsume, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActConsume, AmountCNY: amountCNY, Reason: reason})
	})
}

func (a *AgentRunner) Donate(seat int, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolDonate, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActDonate, AmountCNY: amountCNY})
	})
}

// ── P1 新增工具: 央行/银行体系查询 + 存款 ──

// QueryCentralBank 查询央行状态(不耗动作预算)。
func (a *AgentRunner) QueryCentralBank(seat int) (string, error) {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if a.room.closed || a.room.Status != StatusPlaying {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	cb := a.room.World.CB
	if cb == nil {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	snap := cb.Snapshot()
	return fmt.Sprintf("央行:M0=%.0f M1=%.0f M2=%.0f MB=%.0f 乘数=%.3f 政策利率=%.2f%% LPR=%.2f%% CPI=%.2f%% 信贷约束=%.2f 额度系数=%.2f",
		snap.M0, snap.M1, snap.M2, snap.MB, snap.MoneyMultiplier,
		snap.PolicyRate*100, snap.LPR*100, snap.CPI*100, snap.CreditTightness, snap.LoanQuotaFactor), nil
}

// QueryBankingSystem 查询银行体系(不耗动作预算)。
func (a *AgentRunner) QueryBankingSystem(seat int) (string, error) {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if a.room.closed || a.room.Status != StatusPlaying {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	cb := a.room.World.CB
	if cb == nil {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	bs := cb.BankingSystem()
	return fmt.Sprintf("银行体系:活期=%.0f 定期=%.0f 总存款=%.0f 准备金=%.0f 超额准备金=%.0f 贷款余额=%.0f",
		bs.DemandDeposits, bs.TimeDeposits, bs.TotalDeposits, bs.Reserves, bs.ExcessReserves, bs.LoansOutstanding), nil
}

// ApplyLoanWithCredit 带信贷约束的贷款申请(不耗动作预算,仅查询额度/利率)。
func (a *AgentRunner) ApplyLoanWithCredit(seat int, kind string, amountCNY int64) (string, error) {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if a.room.closed || a.room.Status != StatusPlaying {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	cb := a.room.World.CB
	if cb == nil {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	quotaFactor := cb.LoanQuotaFactor
	creditSpread := cb.CreditSpread
	creditThreshold := 500
	if cb.CreditTightness > 0 {
		creditThreshold += CreditScoreBonus
	}
	p := a.room.World.Players[seat]
	if p == nil {
		return "", errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	cap := int64(200000 * quotaFactor)
	if kind == "consumer" {
		cap = int64(float64(p.monthlyIncomeEstimate()*12) * quotaFactor)
		if cap > 200000 {
			cap = 200000
		}
	}
	rate := cb.PolicyRate + creditSpread
	if kind == "business" {
		rate = cb.BusinessRate()
	} else if kind == "consumer" {
		rate = ConsumerRate + creditSpread
	}
	approved := amountCNY <= cap && p.CreditScore >= creditThreshold
	return fmt.Sprintf("贷款申请:kind=%s 申请=%.0f 额度上限=%.0f 利率=%.2f%% 信用门槛=%d 信用分=%d 批准=%v",
		kind, float64(amountCNY), float64(cap), rate*100, creditThreshold, p.CreditScore, approved), nil
}

// DepositSavings 活期→定期(不耗动作预算)。
func (a *AgentRunner) DepositSavings(seat int, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolDepositSavings, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActDeposit, AmountCNY: amountCNY})
	})
}

// WithdrawSavings 定期→活期(不耗动作预算)。
func (a *AgentRunner) WithdrawSavings(seat int, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolWithdrawSavings, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActWithdraw, AmountCNY: amountCNY})
	})
}

// QueryMinsky 查询明斯基全局概览(不耗动作预算,v2.60 N11-4/N11-5)。
func (a *AgentRunner) QueryMinsky(seat int) (string, error) {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if a.room.closed || a.room.Status != StatusPlaying {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	w := a.room.World
	if w == nil {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	// 统计各等级玩家数。
	var ponzi, spec, hedge, alive int
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		alive++
		worst := MinskyHedge
		for _, ms := range p.MinskyByLoan {
			if ms == nil {
				continue
			}
			if ms.Tier == MinskyPonzi {
				worst = MinskyPonzi
				break
			}
			if ms.Tier == MinskySpeculative {
				worst = MinskySpeculative
			}
		}
		switch worst {
		case MinskyPonzi:
			ponzi++
		case MinskySpeculative:
			spec++
		default:
			hedge++
		}
	}
	// 当前座位的主导等级。
	myTier := ""
	if p := w.Players[seat]; p != nil {
		worst := MinskyHedge
		for _, ms := range p.MinskyByLoan {
			if ms == nil {
				continue
			}
			if ms.Tier == MinskyPonzi {
				worst = MinskyPonzi
				break
			}
			if ms.Tier == MinskySpeculative {
				worst = MinskySpeculative
			}
		}
		myTier = string(worst)
	}
	ponziRatio := 0.0
	if alive > 0 {
		ponziRatio = float64(ponzi) / float64(alive) * 100
	}
	return fmt.Sprintf("明斯基概览:庞氏 %d 人(%.0f%%)/投机 %d/对冲 %d;您=%s;冷却 %d 月;累计触发 %d 次;庞氏 >30%% 触发明斯基时刻(杠杆资产腰斩)",
		ponzi, ponziRatio, spec, hedge, myTier, w.MinskyMomentCooldown, w.MinskyMomentCount), nil
}

// EarlyRepay 提前还款(仅房贷,v2.60 N12-5)。
func (a *AgentRunner) EarlyRepay(seat int, loanID string, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolEarlyRepay, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActEarlyRepay, LoanID: loanID, AmountCNY: amountCNY})
	})
}

// ── P1(§财商流P1-2 §7.1):set_consumption / answer_survey / query_economy ──

// SetConsumption 调整消费档位(耗 1 次动作预算;走 ApplyAction 与人类同路径)。
func (a *AgentRunner) SetConsumption(seat int, level int) error {
	return a.apply(seat, wealthplayer.ToolSetConsumption, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActSetConsumption, Level: level})
	})
}

// AnswerSurvey 回答进行中调研(不耗动作预算;调研契约 §4.2)。
// 锁纪律照 QueryMinsky:持锁校验/写入;若全部存活 bot 已答 → 锁内提前关闭,
// 锁外调 hooks.OnSurvey(§92a)。
func (a *AgentRunner) AnswerSurvey(seat int, surveyID string, optionIdx int, reason string) error {
	a.room.mu.Lock()
	if a.room.closed || a.room.Status != StatusPlaying || a.room.World == nil {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	w := a.room.World
	p := w.Players[seat]
	if p == nil || !p.Alive || !a.room.BotSeats[seat] {
		// 仅 alive 且 bot 座位可答(人类在座玩家作答入口为 P2 扩展)。
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if err := a.checkAgentDecisionLocked(seat); err != nil {
		a.room.mu.Unlock()
		return err
	}
	sv := w.findSurveyByID(surveyID)
	if sv == nil || sv.Status != SurveyOpen {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthSurveyNotFound) // 35019
	}
	if _, dup := sv.Answers[seat]; dup {
		a.room.mu.Unlock()
		return errcode.CodeMsg(errcode.ErrWealthSurveyNotFound, "已回答过该调研")
	}
	if optionIdx < 0 || optionIdx >= len(sv.Options) {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthSurveyOptionsInvalid) // 35016
	}
	sv.Answers[seat] = &SurveyAnswer{
		Seat:      seat,
		OptionIdx: optionIdx,
		Reason:    clip(reason, surveyReasonMaxRunes),
		ModelKey:  a.room.SeatModelKeys[seat],
	}
	// 全部存活 bot 已答 → 立即关闭聚合(不等 deadline)。
	allAnswered, anyBot := true, false
	for s, pp := range w.Players {
		if pp == nil || !pp.Alive || !a.room.BotSeats[s] {
			continue
		}
		anyBot = true
		if _, ok := sv.Answers[s]; !ok {
			allAnswered = false
			break
		}
	}
	var closed *Survey
	if anyBot && allAnswered {
		w.CloseAndAggregate(sv)
		closed = sv
	}
	hooks := a.room.hooks
	roomID := a.room.RoomID
	a.room.mu.Unlock()

	if closed != nil && hooks.OnSurvey != nil {
		hooks.OnSurvey(roomID, closed) // 锁外广播 game.survey_result
	}
	return nil
}

// QueryEconomy 查询经济全景(不耗动作预算):CPI 同比/环比、八大类价格环比、
// 失业率、工资增长、企业营收、基尼、圈层分布。返回单行中文摘要。
func (a *AgentRunner) QueryEconomy(seat int) (string, error) {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if a.room.closed || a.room.Status != StatusPlaying || a.room.World == nil {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	w := a.room.World
	if !w.EconomyEnabled {
		return "经济循环引擎未启用(P0 模式:物价/就业由市场周期阶段外生决定)", nil
	}
	var b strings.Builder
	b.WriteString("经济:")
	if w.Goods != nil {
		fmt.Fprintf(&b, "CPI同比%.1f%% 环比%.1f%%|", w.Goods.CPIYoY*100, w.Goods.CPIMom*100)
		for _, id := range goodsOrder {
			if it := w.Goods.Items[id]; it != nil {
				fmt.Fprintf(&b, "%s%+.1f%% ", goodsCN[id], it.MomChange*100)
			}
		}
	}
	if w.Labor != nil {
		fmt.Fprintf(&b, "|失业率%.1f%% 工资增长%.1f%% 企业营收¥%d 裁员潮%d",
			w.Labor.Unemployment*100, w.Labor.WageGrowthYoY*100, w.Labor.RevenueCNY, w.Labor.LayoffWave)
	}
	if w.Society != nil {
		fmt.Fprintf(&b, "|基尼%.2f 圈层:生存%d/积累%d/自由%d",
			w.Society.Gini, w.Society.Circles[0], w.Society.Circles[1], w.Society.Circles[2])
	}
	return b.String(), nil
}

// ── P1-4(§财商流P1-4 §7): 商业保险三工具 ──

// BuyInsurance 投保(耗 1 次动作预算;走 ApplyAction 与人类同一路径)。
func (a *AgentRunner) BuyInsurance(seat int, kind string) error {
	return a.apply(seat, wealthplayer.ToolBuyInsurance, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActBuyInsurance, Kind: kind})
	})
}

// CancelInsurance 退保(耗 1 次动作预算;消费型零现金价值)。
func (a *AgentRunner) CancelInsurance(seat int, kind string) error {
	return a.apply(seat, wealthplayer.ToolCancelInsurance, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActCancelInsurance, Kind: kind})
	})
}

// GetInsuranceStatus 查询本人保单状态(不耗动作预算,check_state 同惯例;
// 锁内读引擎,返回人读文本 + 结构化 JSON)。
func (a *AgentRunner) GetInsuranceStatus(seat int) (string, error) {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if a.room.closed || a.room.Status != StatusPlaying || a.room.World == nil {
		return "", errcode.Code(errcode.ErrWealthNotPlaying)
	}
	w := a.room.World
	if !w.InsuranceEnabled {
		return "保险引擎未启用(insurance_enabled=false)", nil
	}
	p := w.Players[seat]
	if p == nil {
		return "", errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	return w.InsuranceStatusText(p), nil
}

func (a *AgentRunner) Speak(seat int, text, internalThought string) error {
	// speak 走 chat sender,不耗动作预算。
	a.room.mu.Lock()
	if a.room.closed || a.room.Status != StatusPlaying || a.room.Phase != PhaseActing {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	if err := a.checkAgentDecisionLocked(seat); err != nil {
		a.room.mu.Unlock()
		return err
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive || p.SpeakCountThisMonth >= speakMonthlyLimit {
		a.room.mu.Unlock()
		return errcode.CodeMsg(errcode.ErrWealthSenseLimit, "speak monthly limit reached (2 per month)")
	}
	chat := a.room.chatSender
	roomID := a.room.RoomID
	userID := a.room.Seats[seat]
	modelKey := a.room.SeatModelKeys[seat]
	// 写入 transcript 内心独白(供前端 BotThoughtPanel 渲染)。
	t := a.room.Transcripts[seat]
	t.Month = a.room.World.Month
	t.LastDecisionMonth = a.room.World.Month
	t.UpdatedAt = time.Now().UnixMilli()
	t.Active = p.Alive
	t.HeartThought = clip(internalThought, 200)
	a.room.Transcripts[seat] = t
	p.SpeakCountThisMonth++
	// hear 感知数据源:记录同区公开发言(2026-09-22 §CityHuman重构)。
	a.appendUtteranceLocked(UtteranceRecord{
		Month: a.room.World.Month, Seat: seat, District: p.District, Text: clip(text, 100),
	})
	a.room.mu.Unlock()

	if chat != nil && text != "" {
		truncated := clip(text, 100)
		if err := chat.SendFromBot(roomID, userID, modelKey, modelKey, truncated); err != nil {
			return err
		}
	}
	return nil
}

func (a *AgentRunner) SubmitMonth(seat int) error {
	a.room.mu.Lock()
	if a.room.closed || a.room.Status != StatusPlaying || a.room.Phase != PhaseActing {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if err := a.checkAgentDecisionLocked(seat); err != nil {
		a.room.mu.Unlock()
		return err
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if p.Submitted {
		a.room.mu.Unlock()
		return nil
	}
	p.Submitted = true
	all := a.room.allSubmittedLocked()
	a.room.mu.Unlock()
	if all {
		select {
		case a.room.settleCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// RecordTranscript 实现 wealthplayer.TranscriptSink。这是 bot_contexts 的
// 权威写入点:Agent 在月初、LLM 返回后、submit 前都会调用;旧月快照会被
// 拒绝,避免 3s 月窗下长 LLM 调用回写覆盖新月状态。
func (a *AgentRunner) RecordTranscript(seat int, transcript wealthplayer.BotTranscript) {
	a.room.mu.Lock()
	if a.room.World == nil || seat < 0 || seat >= MaxSeats {
		a.room.mu.Unlock()
		return
	}
	if transcript.Month < a.room.World.Month {
		a.room.mu.Unlock()
		return
	}
	p := a.room.World.Players[seat]
	if p == nil {
		a.room.mu.Unlock()
		return
	}
	t := a.room.Transcripts[seat]
	t.Month = transcript.Month
	t.LastDecisionMonth = transcript.Month
	t.UpdatedAt = time.Now().UnixMilli()
	t.Active = p.Alive
	if !p.Alive {
		t.LastDecisionSummary = "已出局,停止月度决策"
		t.LastDecisionMonth = a.room.World.Month
		t.LastToolInput = ""
		t.LastToolResult = ""
		t.HeartThought = ""
	} else {
		t.LastDecisionSummary = clip(transcript.LastDecisionSummary, 120)
		t.LastToolInput = clip(transcript.LastToolInput, 200)
		t.LastToolResult = clip(transcript.LastToolResult, 200)
		t.HeartThought = clip(transcript.HeartThought, 200)
	}
	a.room.Transcripts[seat] = t
	hooks := a.room.hooks
	roomID := a.room.RoomID
	a.room.mu.Unlock()

	if hooks.OnState != nil {
		hooks.OnState(roomID)
	}
}

// ── P2(2026-09-16 §财商流P2): 玩家间交易与财富流动系统 12 工具 ──
//
// 以下方法为 Agent 工具桥的 P2 扩展。引擎侧实现(listing.go / auction.go /
// trade_actions.go)为独立交付;当前为占位实现,返回友好中文错误提示,
// 避免 Agent 调用时 panic。引擎接入后替换为真实逻辑。

func (a *AgentRunner) checkActing(seat int) error {
	if a.room.closed || a.room.Status != StatusPlaying {
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if a.room.Phase != PhaseActing {
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if p.Submitted {
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	return nil
}

// ListAsset 挂牌出售资产:从玩家持仓中按 asset_index 取出快照,创建挂单。
func (a *AgentRunner) ListAsset(seat int, assetIndex int, askCNY, minCNY int64) error {
	return a.apply(seat, wealthplayer.ToolListAsset, "", func() (string, error) {
		w := a.room.World
		p := w.Players[seat]
		if p == nil || !p.Alive {
			return "", errcode.Code(errcode.ErrWealthPlayerInactive)
		}
		if assetIndex < 0 || assetIndex >= len(p.Assets) {
			return "", errcode.CodeMsg(errcode.ErrWealthAssetInvalid, "asset_index 越界")
		}
		snap := p.Assets[assetIndex]
		payload := ListingPayload{
			Asset: &AssetPayload{Asset: snap, MinCNY: minCNY},
		}
		l, err := w.CreateListing(seat, ListingAsset, payload, askCNY)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("挂牌成功 %s(要价 ¥%d)", l.ID, askCNY), nil
	})
}

// CancelListing 取消自己的 open 挂单。
func (a *AgentRunner) CancelListing(seat int, listingID string) error {
	return a.apply(seat, wealthplayer.ToolCancelListing, "", func() (string, error) {
		if err := a.room.World.CancelListing(seat, listingID); err != nil {
			return "", err
		}
		return "已取消挂单 " + listingID, nil
	})
}

// ViewListings 查看挂单簿(不耗动作预算)。
func (a *AgentRunner) ViewListings(seat int, typeFilter string) (string, error) {
	a.room.mu.Lock()
	defer a.room.mu.Unlock()
	if err := a.checkActingUnlocked(seat); err != nil {
		return "", err
	}
	w := a.room.World
	lType := ListingType(typeFilter)
	snapshots := w.SnapshotListings(lType)
	if len(snapshots) == 0 {
		return "挂单簿为空(暂无活跃挂单)", nil
	}
	out := fmt.Sprintf("活跃挂单 %d 笔:\n", len(snapshots))
	for _, s := range snapshots {
		out += fmt.Sprintf("- %s [%s] %d 号位 要价 ¥%d", s.ID, s.Type, s.Seat, s.AskCNY)
		if s.AssetKind != "" {
			out += fmt.Sprintf(" 资产:%s(%s)", s.AssetKind, s.AssetUnits)
		}
		if s.InfoTitle != "" {
			out += fmt.Sprintf(" 信息:%s", s.InfoTitle)
		}
		if s.Direction != "" {
			out += fmt.Sprintf(" 借贷:%s ¥%d 月利率 %.2f%%", s.Direction, s.PrincipalCNY, s.MaxRate*100)
		}
		out += "\n"
	}
	return out, nil
}

// StartNegotiate 对 open 挂单发起议价。
func (a *AgentRunner) StartNegotiate(seat int, listingID string, offerCNY int64) error {
	return a.apply(seat, wealthplayer.ToolNegotiateStart, "", func() (string, error) {
		neg, err := a.room.World.NegotiateStart(seat, listingID, offerCNY)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("议价 %s 已发起(首轮报价 ¥%d)", neg.ID, offerCNY), nil
	})
}

// RespondNegotiate 响应议价(还价/接受/拒绝)。
func (a *AgentRunner) RespondNegotiate(seat int, negID string, action string, offerCNY int64, comment string) error {
	return a.apply(seat, wealthplayer.ToolRespondNegotiate, "", func() (string, error) {
		text, err := a.room.World.NegotiateRespond(seat, negID, action, offerCNY, comment)
		if err != nil {
			return "", err
		}
		return text, nil
	})
}

// CreateLoanListing 创建借贷挂单。
func (a *AgentRunner) CreateLoanListing(seat int, direction string, principal int64, rate float64, term int, needGuarantee bool) error {
	return a.apply(seat, wealthplayer.ToolCreateLoanListing, "", func() (string, error) {
		l, err := a.room.World.CreateLoanListing(seat, direction, principal, rate, term, needGuarantee)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("借贷挂单 %s 已创建(%s ¥%d)", l.ID, direction, principal), nil
	})
}

// AcceptLoan 接受借贷要约(匹配成交)。
func (a *AgentRunner) AcceptLoan(seat int, listingID string) error {
	return a.apply(seat, wealthplayer.ToolAcceptLoan, "", func() (string, error) {
		loan, err := a.room.World.AcceptLoan(seat, listingID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已接受借贷要约,合约 %s 成立(月供 ¥%d)", loan.ID, loan.MonthlyPayment), nil
	})
}

// RepayP2PLoan 偿还 P2P 借贷(部分或全额)。
func (a *AgentRunner) RepayP2PLoan(seat int, loanID string, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolRepayLoanP2P, "", func() (string, error) {
		text, err := a.room.World.RepayLoan(seat, loanID, amountCNY)
		if err != nil {
			return "", err
		}
		return text, nil
	})
}

// AddGuarantor 为 P2P 借贷提供担保。
func (a *AgentRunner) AddGuarantor(seat int, loanID string) error {
	return a.apply(seat, wealthplayer.ToolAddGuarantor, "", func() (string, error) {
		if err := a.room.World.AddGuarantor(loanID, seat); err != nil {
			return "", err
		}
		return "担保已生效(" + loanID + ")", nil
	})
}

// BidAuction 参与拍卖出价(区分公开/密封)。
func (a *AgentRunner) BidAuction(seat int, auctionID string, amountCNY int64) error {
	return a.apply(seat, wealthplayer.ToolBidAuction, "", func() (string, error) {
		w := a.room.World
		ta := TradeAction{
			Type:      ActionBidAuction,
			AuctionID: auctionID,
			OfferCNY:  amountCNY,
		}
		text, err := w.ApplyTradeAction(seat, ta)
		if err != nil {
			return "", err
		}
		return text, nil
	})
}

// SellInfo 出售信息(密封暗标)。
func (a *AgentRunner) SellInfo(seat int, category string, title string, detail string, minBid int64) error {
	return a.apply(seat, wealthplayer.ToolSellInfo, "", func() (string, error) {
		l, err := a.room.World.SellInfo(seat, category, title, detail, minBid)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("信息挂单 %s 已创建(暗标最低 ¥%d)", l.ID, minBid), nil
	})
}

// BidInfo 暗标信息。
func (a *AgentRunner) BidInfo(seat int, listingID string, bidCNY int64) error {
	return a.apply(seat, wealthplayer.ToolBidInfo, "", func() (string, error) {
		if err := a.room.World.BidInfo(seat, listingID, bidCNY); err != nil {
			return "", err
		}
		return "暗标已提交(" + listingID + ",出价 ¥" + fmt.Sprintf("%d", bidCNY) + ")", nil
	})
}

// checkActingUnlocked 是 checkActing 的无锁变体(调用方已持 r.mu)。
func (a *AgentRunner) checkActingUnlocked(seat int) error {
	if a.room.closed || a.room.Status != StatusPlaying {
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if a.room.Phase != PhaseActing {
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive {
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if p.Submitted {
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	return nil
}

// apply 通用动作派发:锁内校验 + 执行;成功返回 nil,失败返回 errcode.Error
// (工具层 IsErr)。
// 2026-09-14 §财商流P0-bugfix: 本函数持 r.mu 期间,闭包内必须使用
// a.room.World 直接字段访问,严禁调 a.room.Engine()(内部再次 r.mu.Lock,
// sync.Mutex 不可重入 → 自死锁,曾导致整个房间卡死:bot 首个 buy_asset
// 即锁死房间锁,JoinGame/trySettle/game.state 全部阻塞)。
func (a *AgentRunner) apply(seat int, toolName, toolID string, fn func() (string, error)) error {
	a.room.mu.Lock()
	if a.room.closed {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if a.room.Status != StatusPlaying {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthNotPlaying)
	}
	if a.room.Phase != PhaseActing {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	if err := a.checkAgentDecisionLocked(seat); err != nil {
		a.room.mu.Unlock()
		return err
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthPlayerInactive)
	}
	if p.Submitted {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	monthBefore := a.room.World.Month
	text, err := fn()
	monthAfter := a.room.World.Month
	a.room.mu.Unlock()

	if err != nil {
		// 防御 typed nil:*errcode.Error 成功路径返回 nil 指针,装入 error 接口后
		// err!=nil 为 true 但 .Error() panic(虚拟城市 P0 崩溃根因)。
		if e, ok := err.(*errcode.Error); ok && e == nil {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	// 月结可能由 ApplyAction 同步触发(不应发生,但保险):若 month 变化,忽略 — 房间 runLoop 会处理。
	if monthAfter != monthBefore {
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	// 锁外记入 agent transcript(供 bot_contexts)。
	a.room.mu.Lock()
	t := a.room.Transcripts[seat]
	t.Month = monthAfter
	t.LastDecisionMonth = monthAfter
	t.UpdatedAt = time.Now().UnixMilli()
	t.Active = true
	t.LastDecisionSummary = clip(text, 120)
	t.LastToolInput = toolName
	t.LastToolResult = text
	a.room.Transcripts[seat] = t
	// 全员 submitted 检查(触发 settle 提前推进)。
	all := a.room.allSubmittedLocked()
	hooks := a.room.hooks
	roomID := a.room.RoomID
	a.room.mu.Unlock()
	if hooks.OnEvent != nil {
		eventType := "action"
		if toolName == wealthplayer.ToolMoveDistrict || toolName == wealthplayer.ToolMove {
			eventType = "move"
		}
		hooks.OnEvent(roomID, EventRecord{
			Month: monthAfter, Type: eventType, Seat: seat, Text: text,
		})
	}
	if all {
		select {
		case a.room.settleCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// buildCheckStateText 返回座位当前摘要文本(check_state 工具用)。
func (r *WealthRoom) buildCheckStateText(seat int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil {
		return ""
	}
	p := r.World.Players[seat]
	if p == nil {
		return ""
	}
	nw := p.NetWorth(r.World.Market)
	fi := p.FIIndex(r.World.Market, r.World.Age())
	return fmt.Sprintf("现金 %d 元 | 净资产 %d | FI %.2f | 精力 %d/%d | 信用 %d | 月预算 %d",
		p.Cash, nw, fi, p.Energy, 10, p.CreditScore, p.ActionBudget)
}

// clip 截断 rune。
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// BuildContextForAgent 在房间持锁态构造 GameContext 快照(锁外消费,§92a)。
func BuildContextForAgent(r *WealthRoom, seat int) (*wealthtypes.GameContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil || seat < 0 || seat >= MaxSeats {
		return nil, false
	}
	p := r.World.Players[seat]
	// 已出局座位不再构建决策上下文;否则每继续浪费一次 LLM 调用,
	// 并把死亡前 transcript 回写为“活跃”。
	if p == nil || !p.Alive {
		return nil, false
	}
	age := r.World.Age()
	month := r.World.Month

	// Market/Cycle 快照。
	cycle := wealthtypes.CycleBrief{
		Phase:      string(r.World.Market.CyclePhase),
		LPR:        r.World.Market.Params().LPR,
		CPI:        r.World.Market.Params().CPI,
		MonthsLeft: r.World.Market.CycleMonthsLeft,
	}
	market := wealthtypes.MarketBrief{
		StockIndex: r.World.Market.StockIndex,
		GoldPrice:  r.World.Market.GoldPrice,
		BondRate:   r.World.Market.Params().BondRate,
		HouseIdx:   map[string]float64{},
	}
	for d, idx := range r.World.Market.DistrictIdx {
		market.HouseIdx[d] = idx
	}

	// Me 镜像。
	me := wealthtypes.SelfBrief{
		Cash: p.Cash, Salary: p.SalaryBase,
		SpouseIncome: p.Family.SpouseIncome,
		Monthly: wealthtypes.MonthlyBrief{
			Income: p.Monthly.Income, Expense: p.Monthly.Expense, Net: p.Monthly.Net,
			Tax: p.Monthly.Tax, Social: p.Monthly.Social,
			PassiveIncome: p.Monthly.PassiveIncome, SideIncome: p.Monthly.SideIncome,
			OvertimeBonus: p.Monthly.OvertimeBonus,
			Detail:        convertFlowItems(p.Monthly.Detail),
		},
		Energy: p.Energy, Network: p.Network, Cognition: p.Cognition,
		PensionCNY:   p.PensionCNY,
		CreditScore:  p.CreditScore,
		Marital:      p.Family.Marital,
		Children:     p.Family.Children,
		FIIndex:      fiIndexFor(r, p),
		NetWorth:     p.NetWorth(r.World.Market),
		ActionBudget: p.ActionBudget,
		Goals:        append([]string(nil), p.Card.Goals...),
	}
	for i := range p.Assets {
		me.Assets = append(me.Assets, assetBriefFor(r, &p.Assets[i]))
	}
	for i := range p.Loans {
		me.Loans = append(me.Loans, loanBriefFor(&p.Loans[i]))
	}
	// P1-4(§财商流P1-4 §7.4):保单摘要(prompt 保单行渲染)。
	if r.World.InsuranceEnabled {
		for _, kind := range insuranceKindOrder {
			pol := p.Policies[kind]
			if pol == nil {
				continue
			}
			me.Policies = append(me.Policies, wealthtypes.PolicyBrief{
				Kind:              pol.Kind,
				MonthlyPremiumCNY: monthlyPremium(pol.AnnualPremiumCNY),
				Status:            pol.Status(r.World.Month),
				WaitingLeft:       pol.waitingLeft(r.World.Month),
			})
		}
	}

	// Peers 公开字段。
	peers := []wealthtypes.PeerBrief{}
	for s, pp := range r.World.Players {
		if pp == nil || s == seat {
			continue
		}
		nw := pp.NetWorth(r.World.Market)
		peers = append(peers, wealthtypes.PeerBrief{
			Seat: s, Account: pp.Card.ID, Nickname: r.Nicknames[s],
			IsBot: r.BotSeats[s], ProfessionTitle: pp.Card.Title,
			District: pp.District, NetWorth: nw,
			FIIndex: pp.FIIndex(r.World.Market, age),
			Alive:   pp.Alive,
		})
	}

	// 近期事件(最近 30)。
	evRecent := recentEvents(r, 30)
	// 近期流水(本人最近 20)。
	ledgerRecent := ledgerRecent(r, seat, 20)
	// BotIdentity。
	botIdent := wealthtypes.BotIdentityBrief{
		UserID: p.Card.ID, ModelKey: r.SeatModelKeys[seat], ModelName: ModelDisplayName(r.SeatModelKeys[seat]),
		AgentClass: string(agentroot.AgentClassCityHuman),
	}

	// P1: 央行快照 + 信贷约束参数。
	var cbSnapshot *wealthtypes.CentralBankSnapshot
	creditTightness, loanQuotaFactor := 0.0, 1.0
	if r.World.CB != nil {
		snap := r.World.CB.Snapshot()
		if snap != nil {
			cbSnapshot = &wealthtypes.CentralBankSnapshot{
				M0: snap.M0, M1: snap.M1, M2: snap.M2,
				MB: snap.MB, MoneyMultiplier: snap.MoneyMultiplier,
				PolicyRate: snap.PolicyRate, LPR: snap.LPR, CPI: snap.CPI,
				CreditTightness: snap.CreditTightness, LoanQuotaFactor: snap.LoanQuotaFactor,
			}
		}
		creditTightness = r.World.CB.CreditTightness
		loanQuotaFactor = r.World.CB.LoanQuotaFactor
	} else {
		// CB nil 回退:PhaseTable 基础值。
		pp := r.World.Market.Params()
		cbSnapshot = &wealthtypes.CentralBankSnapshot{
			PolicyRate: pp.LPR, LPR: pp.LPR, CPI: pp.CPI,
			CreditTightness: 0, LoanQuotaFactor: 1,
		}
	}

	// Card 投影。
	cardBrief := wealthtypes.CardBrief{
		ID: p.Card.ID, Title: p.Card.Title, Name: p.Card.Name,
		HomeDistrictCN: DistrictCN(p.HomeDistrict),
		Salary:         p.Card.Salary, Expense: p.Card.Expense, Savings: p.Card.Savings,
		StartAge: p.Card.StartAge, Energy: p.Card.Energy,
		Network: p.Card.Network, Cognition: p.Card.Cognition,
		CreditScore: p.Card.CreditScore, RiskPreference: p.Card.RiskPreference,
		Personality:    append([]string(nil), p.Card.Personality...),
		BehaviorTraits: append([]string(nil), p.Card.BehaviorTraits...),
		HealthGrade:    p.Card.HealthGrade,
		Marital:        p.Card.Marital, ChildrenCount: p.Card.ChildrenCount,
		EldersDependent: p.Card.EldersDependent,
		Goals:           append([]string(nil), p.Card.Goals...),
	}

	// P1(§财商流P1-2 §7.2):真实经济循环 + 社会调研上下文。
	ecoCPIYoY, ecoUnemployment, ecoBrief := 0.0, 0.0, ""
	if r.World.EconomyEnabled && r.World.Goods != nil {
		if r.World.Labor != nil {
			ecoUnemployment = r.World.Labor.Unemployment
		}
		ecoCPIYoY = r.World.Goods.CPIYoY
		ecoBrief = economyBrief(r.World.Goods, ecoUnemployment)
	}
	var openSurveyID, openSurveyQ string
	var openSurveyOpts []string
	if sv := r.World.OpenSurvey(); sv != nil { // 限流保证至多 1 个 open
		openSurveyID = sv.ID
		openSurveyQ = sv.Question
		openSurveyOpts = append([]string(nil), sv.Options...)
	}

	return &wealthtypes.GameContext{
		// 批次20(文档2 §4.3 / 文档3 A5/B4):三块预渲染小节(无信息 → "")。
		SideMarketBrief: sideMarketBriefLocked(r, seat),
		ElectionBrief:   electionBriefLocked(r.World, seat),
		MicroPriceBrief: microPriceBriefLocked(r.World, p),
		RoomID:          r.RoomID, GameKind: "wealth",
		MySeat: seat, MyUserID: r.Seats[seat], ModelKey: r.SeatModelKeys[seat],
		Month: month, Age: age, Phase: r.Phase,
		TimeRemainingSec: int(time.Until(r.NextMonthAt).Seconds()),
		Cycle:            cycle, Market: market,
		Me: me, Peers: peers,
		RecentEvents: evRecent, RecentLedger: ledgerRecent,
		BotIdentity: botIdent, MyCard: cardBrief,
		CentralBank: cbSnapshot, CreditTightness: creditTightness, LoanQuotaFactor: loanQuotaFactor,
		// §CityHuman重构(2026-09-22): 感官上下文(同区邻居 + 城区当月氛围)。
		Surroundings: surroundingsForLocked(r, p),
		Ambiance:     (&AgentRunner{room: r}).ambianceForLocked(p.District),
		// P1: 经济环境 + 待答调研。
		CPIYoY:             ecoCPIYoY,
		UnemploymentRate:   ecoUnemployment,
		ConsumptionLevel:   p.ConsumptionLevelSafe(),
		OpenSurveyID:       openSurveyID,
		OpenSurveyQuestion: openSurveyQ,
		OpenSurveyOptions:  openSurveyOpts,
		EconomyBrief:       ecoBrief,
	}, true
}

// surroundingsForLocked 构造同城区邻居摘要(锁内;座位居民优先 +
// Backdrop 抽样真实档案补齐,≤8 条;2026-09-22 §CityHuman重构)。
func surroundingsForLocked(r *WealthRoom, p *Player) []wealthtypes.NeighborBrief {
	out := []wealthtypes.NeighborBrief{}
	for s, pp := range r.World.Players {
		if len(out) >= 8 {
			break
		}
		if pp == nil || s == p.Seat || pp.District != p.District {
			continue
		}
		name := r.Nicknames[s]
		if name == "" {
			name = pp.Card.Name
		}
		out = append(out, wealthtypes.NeighborBrief{
			Kind: "seat", Seat: s, Name: name, Occupation: pp.Card.Title,
			District: p.District, MoodHint: moodOfPlayer(pp),
		})
	}
	if r.City != nil && len(out) < 8 {
		for _, n := range r.City.SampleDistrictNeighbors(p.District, 8-len(out)) {
			out = append(out, wealthtypes.NeighborBrief{
				Kind: "resident", CardID: n.CardID, Name: n.Name, Occupation: n.Occupation,
				District: n.DistrictName, MoodHint: moodOfNeighbor(n),
			})
		}
	}
	return out
}

// clipBytes UTF-8 字节预算裁剪(按 rune 边界,不切半个汉字;批次20 上下文
// 字节预算纪律,文档2 §4.3 ≤350B / 文档3 B4 ≤120B)。
func clipBytes(s string, budget int) string {
	if len(s) <= budget {
		return s
	}
	r := []rune(s)
	out := strings.Builder{}
	for _, c := range r {
		if out.Len()+len(string(c)) > budget-3 { // 预留「…」
			break
		}
		out.WriteRune(c)
	}
	return out.String() + "…"
}

// sideMarketBriefLocked 副业定价市场小节(文档2 §4.3;锁内纯读;≤350B;
// 无副业 → "" 不注入)。对手仅档位/份额,不含对方 BaseIncome 细节。
func sideMarketBriefLocked(r *WealthRoom, seat int) string {
	if r.World == nil {
		return ""
	}
	p := r.World.Players[seat]
	if p == nil || p.SideBusiness == nil {
		return ""
	}
	shares := sideMarketShares(r.World)
	sb := p.SideBusiness
	share := 1.0
	if ks, ok := shares[sb.Kind]; ok {
		if s, ok2 := ks[seat]; ok2 && s > 0 {
			share = s
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "你的副业:%s %s档,客群份额 %.0f%%,上月实收 ¥%d。",
		sideBizCN(sb.Kind), sideTierCN(sb.PriceTier), share*100, p.Monthly.SideIncome)
	rivals := sideOperatorSeats(shares, sb.Kind)
	if len(rivals) >= 2 {
		b.WriteString("同品类对手:")
		shown := 0
		for _, s := range rivals {
			if s == seat || shown >= 5 {
				continue
			}
			op := r.World.Players[s]
			fmt.Fprintf(&b, " %d号位%s档份额%.0f%%;", s,
				sideTierCN(op.SideBusiness.PriceTier), shares[sb.Kind][s]*100)
			shown++
		}
	} else {
		b.WriteString("该品类暂无竞争对手。")
	}
	b.WriteString("规则:低50%·中30%·高20%客群;高价利润×1.25但份额看对手;集体低价=集体受损。")
	if share < 0.4 {
		b.WriteString("份额偏低,考虑差异化档位或转行。")
	}
	return clipBytes(b.String(), sideMarketBriefBudget)
}

// sideMarketBriefBudget 文档2 §4.3 上下文增量字节预算。
const sideMarketBriefBudget = 350

// electionBriefLocked 市长选举小节(文档3 A5;锁内纯读;未启用 → "")。
func electionBriefLocked(w *World, seat int) string {
	if w == nil || w.Election == nil || !w.Election.Enabled {
		return ""
	}
	ce := w.Election
	var b strings.Builder
	if ce.MayorSeat >= 0 && ce.MayorSeat == seat {
		fmt.Fprintf(&b, "本城已启动市长选举:你是现任市长,下届选举在第 %d 月。", ce.NextElectionMonth())
	} else if ce.MayorSeat >= 0 {
		fmt.Fprintf(&b, "本城已启动市长选举:现任市长为 %d 号位,下届选举在第 %d 月。", ce.MayorSeat, ce.NextElectionMonth())
	} else {
		fmt.Fprintf(&b, "本城已启动市长选举:市长职位空缺,下届选举在第 %d 月。", ce.NextElectionMonth())
	}
	// 本人上届得票排名(LastVotes 已按得分降序)。
	if rank := electionRankOf(ce, seat); rank > 0 {
		fmt.Fprintf(&b, "你上届得票排名第 %d 名。", rank)
	}
	b.WriteString("财富/人脉/满意度决定选情,socialize 也是竞选资源。")
	return b.String()
}

// electionRankOf 座位在最近一次得票明细中的排名(1 起;无记录 → 0)。
func electionRankOf(ce *CivicElection, seat int) int {
	for i, v := range ce.LastVotes {
		if v.Seat == seat {
			return i + 1
		}
	}
	return 0
}

// microPriceBriefLocked 股票微观结构现价小节(文档3 B4;锁内纯读;≤120B)。
// 「有信息才注入」:熔断生效、有 T+1 冻结或持有股票时输出,否则 ""。
func microPriceBriefLocked(w *World, p *Player) string {
	if w == nil || w.Market == nil {
		return ""
	}
	m := w.Market
	breaker := m.StockBreakerActive(w.Month)
	hasStock := p != nil && (p.StockT1Locked > 0 || p.assetOf(AssetStockIndex) != nil)
	if !breaker && !hasStock {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "股票现价 买%.2f/卖%.2f(价差 %dbp)。",
		m.StockBuyUnit(), m.StockSellUnit(), int(m.StockSpread()*10000+0.5))
	if p != nil && p.StockT1Locked > 0 {
		fmt.Fprintf(&b, "T+1 冻结 %d 份。", p.StockT1Locked)
	}
	if breaker {
		fmt.Fprintf(&b, "熔断暂停股票交易至第 %d 月。", m.BreakerUntilMonth)
	}
	return clipBytes(b.String(), microPriceBriefBudget)
}

// microPriceBriefBudget 文档3 B4 字节预算。
const microPriceBriefBudget = 120

// fiIndexFor 包装 p.FIIndex(锁内)。
func fiIndexFor(r *WealthRoom, p *Player) float64 {
	return p.FIIndex(r.World.Market, r.World.Age())
}

// assetBriefFor 资产→wealthtypes 快照。
func assetBriefFor(r *WealthRoom, a *Asset) wealthtypes.AssetBrief {
	b := wealthtypes.AssetBrief{
		Kind: a.Kind, Units: a.Units,
		Price:          assetPrice(r, a),
		ValueCNY:       AssetValue(a, r.World.Market),
		MonthlyFlowCNY: assetMonthlyFlow(r, a),
	}
	b.Name = assetNameCN(a)
	return b
}

// assetPrice 单价:股票→现价、债券→1、黄金→现价、房产/商铺→现房价。
func assetPrice(r *WealthRoom, a *Asset) float64 {
	switch {
	case a.Kind == AssetStockIndex:
		return r.World.Market.StockIndex
	case a.Kind == AssetBond:
		return 1
	case a.Kind == AssetGold:
		return r.World.Market.GoldPrice
	case isHouseKind(a.Kind):
		return float64(r.World.Market.HousePrice(a.AssetDistrict()))
	case isShopKind(a.Kind):
		return float64(r.World.Market.ShopPrice(a.AssetDistrict()))
	}
	return 0
}

// assetMonthlyFlow 单笔月现金流(正=流入)。
func assetMonthlyFlow(r *WealthRoom, a *Asset) int64 {
	switch {
	case isHouseKind(a.Kind) && !a.IsSelfOccupied():
		return r.World.Market.HouseRent(a.AssetDistrict())
	case isShopKind(a.Kind):
		return r.World.Market.ShopRent(a.AssetDistrict())
	case a.Kind == AssetBond:
		return int64(a.Units * a.AssetRate() / 12)
	}
	return 0
}

func assetNameCN(a *Asset) string {
	switch {
	case a.Kind == AssetStockIndex:
		return "指数基金"
	case a.Kind == AssetBond:
		return "债券理财"
	case a.Kind == AssetGold:
		return "黄金"
	case a.Kind == AssetSideBusiness:
		return "副业"
	case a.Kind == AssetPension:
		return "养老金账户"
	case isHouseKind(a.Kind):
		return DistrictCN(a.AssetDistrict()) + "住宅"
	case isShopKind(a.Kind):
		return DistrictCN(a.AssetDistrict()) + "商铺"
	}
	return a.Kind
}

// loanBriefFor 负债→wealthtypes 快照。
func loanBriefFor(l *Loan) wealthtypes.LoanBrief {
	return wealthtypes.LoanBrief{
		ID: l.ID, Kind: l.Kind, Principal: l.Principal, Balance: l.Balance,
		AnnualRate: l.AnnualRate, MonthlyPayment: l.MonthlyPayment,
		MonthsLeft: l.MonthsLeft,
	}
}

func convertFlowItems(in []FlowItem) []wealthtypes.FlowDetailItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]wealthtypes.FlowDetailItem, len(in))
	for i, f := range in {
		out[i] = wealthtypes.FlowDetailItem{Key: f.Key, AmountCNY: f.AmountCNY, Text: f.Text}
	}
	return out
}

func recentEvents(r *WealthRoom, n int) []wealthtypes.EventBrief {
	events := r.World.RecentEvents(n)
	out := make([]wealthtypes.EventBrief, len(events))
	for i, e := range events {
		out[i] = wealthtypes.EventBrief{Month: e.Month, Type: e.Type, Seat: e.Seat, Text: e.Text}
	}
	return out
}

func ledgerRecent(r *WealthRoom, seat, n int) []wealthtypes.LedgerBrief {
	ents := r.World.Ledger.SeatRecent(seat, n)
	out := make([]wealthtypes.LedgerBrief, len(ents))
	for i, e := range ents {
		out[i] = wealthtypes.LedgerBrief{
			Month: e.Month, From: e.From, To: e.To,
			AmountCNY: e.AmountCNY, Category: e.Category, Note: e.Note,
		}
	}
	return out
}

var _ = context.Background
