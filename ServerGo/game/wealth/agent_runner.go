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

func (a *AgentRunner) StartSideBusiness(seat int, kind string) error {
	return a.apply(seat, wealthplayer.ToolStartSide, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActStartSide, Kind: kind})
	})
}

func (a *AgentRunner) StopSideBusiness(seat int) error {
	return a.apply(seat, wealthplayer.ToolStopSide, "", func() (string, error) {
		return a.room.World.ApplyAction(seat, Action{Type: ActStopSide})
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
	sv := w.findSurveyByID(surveyID)
	if sv == nil || sv.Status != SurveyOpen {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthSurveyNotFound) // 35019
	}
	p := w.Players[seat]
	if p == nil || !p.Alive || !a.room.BotSeats[seat] {
		// 仅 alive 且 bot 座位可答(人类在座玩家作答入口为 P2 扩展)。
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthPlayerInactive)
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

func (a *AgentRunner) Speak(seat int, text, internalThought string) error {
	// speak 走 chat sender,不耗动作预算。
	a.room.mu.Lock()
	if a.room.closed || a.room.Status != StatusPlaying || a.room.Phase != PhaseActing {
		a.room.mu.Unlock()
		return errcode.Code(errcode.ErrWealthWrongPhase)
	}
	p := a.room.World.Players[seat]
	if p == nil || !p.Alive || p.SpokenThisMonth {
		a.room.mu.Unlock()
		return errcode.CodeMsg(errcode.ErrWealthWrongPhase, "speech already used this month or player inactive")
	}
	chat := a.room.chatSender
	roomID := a.room.RoomID
	userID := a.room.Seats[seat]
	modelKey := a.room.SeatModelKeys[seat]
	// 写入 transcript 内心独白(供前端 BotThoughtPanel 渲染)。
	a.room.Transcripts[seat] = BotTranscript{
		HeartThought: clip(internalThought, 200),
		LastDecisionSummary: clip(a.room.Transcripts[seat].LastDecisionSummary, 120),
	}
	p.SpokenThisMonth = true
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
	return a.room.SubmitMonth(seat)
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
		// err!=nil 为 true 但 .Error() panic(财商流 P0 崩溃根因)。
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
	t.LastDecisionSummary = clip(text, 120)
	t.LastToolInput = toolName
	t.LastToolResult = text
	a.room.Transcripts[seat] = t
	// 全员 submitted 检查(触发 settle 提前推进)。
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
	if p == nil {
		return nil, false
	}
	age := r.World.Age()
	month := r.World.Month

	// Market/Cycle 快照。
	cycle := wealthtypes.CycleBrief{
		Phase: string(r.World.Market.CyclePhase),
		LPR:   r.World.Market.Params().LPR,
		CPI:   r.World.Market.Params().CPI,
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
			Detail: convertFlowItems(p.Monthly.Detail),
		},
		Energy: p.Energy, Network: p.Network, Cognition: p.Cognition,
		PensionCNY:  p.PensionCNY,
		CreditScore: p.CreditScore,
		Marital:     p.Family.Marital,
		Children:    p.Family.Children,
		FIIndex:     fiIndexFor(r, p),
		NetWorth:    p.NetWorth(r.World.Market),
		ActionBudget: p.ActionBudget,
		Goals:        append([]string(nil), p.Card.Goals...),
	}
	for i := range p.Assets {
		me.Assets = append(me.Assets, assetBriefFor(r, &p.Assets[i]))
	}
	for i := range p.Loans {
		me.Loans = append(me.Loans, loanBriefFor(&p.Loans[i]))
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
		AgentClass: "LsmAgentGame-Wealth-Player",
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
		Salary: p.Card.Salary, Expense: p.Card.Expense, Savings: p.Card.Savings,
		StartAge: p.Card.StartAge, Energy: p.Card.Energy,
		Network: p.Card.Network, Cognition: p.Card.Cognition,
		CreditScore: p.Card.CreditScore, RiskPreference: p.Card.RiskPreference,
		Personality:    append([]string(nil), p.Card.Personality...),
		BehaviorTraits: append([]string(nil), p.Card.BehaviorTraits...),
		HealthGrade:    p.Card.HealthGrade,
		Marital:        p.Card.Marital, ChildrenCount: p.Card.ChildrenCount,
		EldersDependent: p.Card.EldersDependent,
		Goals:          append([]string(nil), p.Card.Goals...),
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
		RoomID: r.RoomID, GameKind: "wealth",
		MySeat: seat, MyUserID: r.Seats[seat], ModelKey: r.SeatModelKeys[seat],
		Month: month, Age: age, Phase: r.Phase,
		TimeRemainingSec: int(time.Until(r.NextMonthAt).Seconds()),
		Cycle: cycle, Market: market,
		Me: me, Peers: peers,
		RecentEvents: evRecent, RecentLedger: ledgerRecent,
		BotIdentity: botIdent, MyCard: cardBrief,
		CentralBank: cbSnapshot, CreditTightness: creditTightness, LoanQuotaFactor: loanQuotaFactor,
		// P1: 经济环境 + 待答调研。
		CPIYoY:            ecoCPIYoY,
		UnemploymentRate:  ecoUnemployment,
		ConsumptionLevel:  p.ConsumptionLevelSafe(),
		OpenSurveyID:      openSurveyID,
		OpenSurveyQuestion: openSurveyQ,
		OpenSurveyOptions: openSurveyOpts,
		EconomyBrief:      ecoBrief,
	}, true
}

// fiIndexFor 包装 p.FIIndex(锁内)。
func fiIndexFor(r *WealthRoom, p *Player) float64 {
	return p.FIIndex(r.World.Market, r.World.Age())
}

// assetBriefFor 资产→wealthtypes 快照。
func assetBriefFor(r *WealthRoom, a *Asset) wealthtypes.AssetBrief {
	b := wealthtypes.AssetBrief{
		Kind: a.Kind, Units: a.Units,
		Price: assetPrice(r, a),
		ValueCNY: AssetValue(a, r.World.Market),
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
