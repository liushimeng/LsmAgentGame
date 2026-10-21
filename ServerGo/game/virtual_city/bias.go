// Package virtual_city — bias.go: 居民行为金融偏差引擎(批次62 §20261020-62)。
//
// 唯一事实来源:lag_docs/虚拟城市/虚拟城市行为金融与投资心理机制设计.md
//   §1   偏差体验机制总表(11 类偏差 → 可执行机制)
//   §3.1 锚定效应系统(买入价锚 / 历史最高价锚 / 锚定指数)
//   §3.2 心理账户系统(六类资金来源标签 / 消费倾向放大 / 合并账户)
//   §3.3 双曲贴现系统(延期支付 ×1.3 / 储蓄纪律奖励 2% / 延迟满足指数)
//   §3.4 羊群效应系统(操作摘要广播 / 跟风概率 / 逆势奖励 / 羊群指数)
//   §4.1 认知检定规则(阈值判定;通过 ⇒ 效果减弱 50% + 显示偏差名)
//   §4.2 认知不足时的偏差加重规则
//   §4.3 主动反思机制(轻度 −1 / 深度 −2 / 复盘预习 −1;每回合 1 次;K ≤ 1 禁用)
//   §4.4 教育闭环(跨局继承 30% / 偏差免疫进度 / 新手保护期)
//
// 数值全部照抄文档,不自造参数。
//
// 设计纪律(与既有引擎一致):
//   - 零全局 rand:画像采样 / 泡沫衰退月数等"需随机但必须可复现"的场景用
//     detRand(自实现 mulberry32),**不动 World.Rand** —— World.Rand 有
//     "固定种子存量对局回归零偏移"契约(settlement.go 头注释),挪用会破坏回归。
//   - 认知检定是阈值判定(非掷骰),与 §4.1 一致。
//   - 未通过检定时**不得**向玩家/Agent 暴露偏差名(教学闭环的前提)。
package virtual_city

import (
	"fmt"
	"math"

	"LsmAgentGame/errcode"
)

// ── 偏差目录 ────────────────────────────────────────────────────────────

// BiasCategory 偏差大类(画像展示用)。
type BiasCategory string

const (
	BiasCatEntry  BiasCategory = "entry"  // 入场类(买入)
	BiasCatExit   BiasCategory = "exit"   // 出场类(卖出)
	BiasCatSpend  BiasCategory = "spend"  // 支出类(消费)
	BiasCatInvest BiasCategory = "invest" // 投入类(教育/副业追加)
)

// BiasEffect 偏差作用目标(决定乘子乘在哪一项上)。
type BiasEffect string

const (
	EffectSellPrice   BiasEffect = "sell_price"    // 卖出成交价乘子(<1 = 折价)
	EffectBuyPrice    BiasEffect = "buy_price"     // 买入成交价乘子(>1 = 追高多付)
	EffectBuyAmount   BiasEffect = "buy_amount"    // 买入金额乘子(>1 = 加仓)
	EffectInvestAmt   BiasEffect = "invest_amount" // 投入金额乘子(>1 = 追加)
	EffectConsumeCap  BiasEffect = "consume_cap"   // 消费上限乘子(>1 = 放宽)
	EffectCostExtra   BiasEffect = "cost_extra"    // 额外成本乘子(>1 = 滑点)
	EffectDeferredPay BiasEffect = "deferred_pay"  // 提供"延期支付"选项(不乘子)
)

// BiasDef 单条偏差的静态定义。
type BiasDef struct {
	Code      string
	NameCN    string
	Category  BiasCategory
	Threshold int        // 认知阈值(认知 ≥ 阈值 ⇒ 检定通过)
	Effect    BiasEffect // 作用目标(决定乘子乘在哪一项上)
	// Mul 检定通过时的乘子(效果减弱 50% 后使用);Exaggerated 未通过时的加重乘子。
	Mul         float64
	Exaggerated float64
	Desc        string // 一句话学术解释(仅在检定通过后向玩家/Agent 展示)
	// Guard 守卫条件(非 nil 时,条件不满足 ⇒ 该偏差本次不生效)。
	// 例:损失厌恶只在「卖出浮亏标的」时触发;赌徒谬误只在「连续亏损 ≥2 次」时触发。
	Guard func(p *Player, ctx *BiasCtx) bool
}

// BiasCtx 一次决策的上下文(供 Guard 判定资产相关条件)。
type BiasCtx struct {
	// AtLoss 标的当前处于浮亏(成本 > 现价)—— 卖出决策用。
	AtLoss bool
	// AtProfit 标的当前处于浮盈(成本 ≤ 现价)—— 卖出决策用。
	AtProfit bool
}

// biasCatalog 偏差目录(9 条;数值照抄任务书 / §4.2 表)。
var biasCatalog = []BiasDef{
	{
		Code: "loss_aversion", NameCN: "损失厌恶", Category: BiasCatExit, Threshold: 5,
		Effect: EffectSellPrice,
		Mul:   0.92, Exaggerated: 0.85,
		Desc:  "损失厌恶:对已实现亏损的心理权重约为同等收益的两倍,于是不愿割肉",
		Guard: func(p *Player, ctx *BiasCtx) bool { return ctx != nil && ctx.AtLoss },
	},
	{
		Code: "disposition_effect", NameCN: "处置效应", Category: BiasCatExit, Threshold: 5,
		Effect: EffectSellPrice,
		Mul:   0.97, Exaggerated: 0.94,
		Desc:  "处置效应:赢跑输扛,倾向提前兑现盈利、拖延亏损",
		Guard: func(p *Player, ctx *BiasCtx) bool { return ctx != nil && ctx.AtProfit },
	},
	{
		Code: "overconfidence", NameCN: "过度自信", Category: BiasCatEntry, Threshold: 4,
		Effect: EffectBuyAmount,
		Mul:    1.15, Exaggerated: 1.30,
		Desc:   "过度自信:把短期运气误判为能力,系统性放大仓位",
	},
	{
		Code: "anchoring", NameCN: "锚定效应", Category: BiasCatEntry, Threshold: 5,
		Effect: EffectBuyPrice,
		Mul:    1.10, Exaggerated: 1.20,
		Desc:   "锚定效应:决策被买入价锚定,追高时愿意多付",
	},
	{
		Code: "herd_follow", NameCN: "羊群跟风", Category: BiasCatEntry, Threshold: 5,
		Effect: EffectCostExtra,
		Mul:    1.10, Exaggerated: 1.20,
		Desc:   "羊群效应:跟随多数方向,付出额外的流动性与滑点成本",
	},
	{
		Code: "mental_account", NameCN: "心理账户", Category: BiasCatSpend, Threshold: 4,
		Effect: EffectConsumeCap,
		Mul:    1.50, Exaggerated: 2.00,
		Desc:   "心理账户:把意外之财当成白捡的钱花,与工资一视同仁",
		// 仅在账户里还有「意外之财」标签余额时生效(§3.2.2)。
		Guard: func(p *Player, ctx *BiasCtx) bool { return p.mentalAccountWindfall() > 0 },
	},
	{
		Code: "sunk_cost", NameCN: "沉没成本谬误", Category: BiasCatInvest, Threshold: 5,
		Effect: EffectInvestAmt,
		Mul:    1.30, Exaggerated: 1.50,
		Desc:   "沉没成本谬误:已经花掉的钱不是继续投入的理由",
	},
	{
		Code: "gamblers_fallacy", NameCN: "赌徒谬误", Category: BiasCatEntry, Threshold: 5,
		Effect: EffectBuyAmount,
		Mul:    1.50, Exaggerated: 1.80,
		Desc:   "赌徒谬误:以为独立事件之间存在记忆,连亏后急于追回",
		// §1 表第 10 项:连续 3 次亏损后加注 —— 引擎口径取「连续亏损 ≥ 2 次」。
		Guard: func(p *Player, ctx *BiasCtx) bool { return p.LastConsecutiveLossCount >= 2 },
	},
	{
		Code: "hyperbolic_discount", NameCN: "双曲贴现", Category: BiasCatSpend, Threshold: 4,
		Effect: EffectDeferredPay,
		Mul:    1.00, Exaggerated: 1.00,
		Desc:   "双曲贴现:偏好即时满足,对未来收益折现过重",
	},
}

// biasByCode code → 定义(线性查表,9 条,无需 map 缓存)。
func biasByCode(code string) (BiasDef, bool) {
	for _, d := range biasCatalog {
		if d.Code == code {
			return d, true
		}
	}
	return BiasDef{}, false
}

// biasImmunityBlock 偏差免疫进度阈值(§4.4.2:≥80% ⇒ 触发概率 −50%)。
// 本实现用**确定性等价**落地:进度达标 ⇒ 该偏差本回合不生效(效果 = 1.0)。
// 取舍理由:引擎内禁止用全局 rand 做概率分支(固定种子回归契约),故以
// "不生效"替代"概率减半" —— 对玩家是更友好的方向(免疫 = 真免疫)。
const biasImmunityBlock = 0.80

// biasImmunityReveal 复盘揭示该类偏差时的免疫进度增量(§4.4.2)。
const biasImmunityReveal = 0.10

// biasImmunityResist 局内成功抵抗(检定通过)时的免疫进度增量(§4.4.2)。
const biasImmunityResist = 0.15

// biasImmunityFall 踩坑且未抵抗时的免疫进度惩罚(§4.4.2)。
const biasImmunityFall = -0.05

// 延期支付参数(§3.3.1):本月免付,3 个月后金额 ×1.3。
const (
	deferredPayMonths   = 3
	deferredPayPenalty  = 0.30
	deferredPayDueMult  = 1.30
	savingsRewardRate   = 0.02  // §3.3.2 储蓄纪律奖励 = 月结余 × 2%
	savingsRewardStreak = 3     // 连续 3 个月正储蓄 ⇒ 第 4 个月起奖励
)

// 跨局认知继承比例(§4.4.1):本局获得认知的 30% 带入下一局(向下取整)。
const cognitionCarryOverRate = 0.30

// ── 确定性伪随机(批次62 自实现 mulberry32) ────────────────────────────

// detRand 确定性伪随机发生器(mulberry32)。**仅**用于"需随机但必须可复现"
// 的新逻辑(偏差画像采样 / 泡沫衰退月数),不替代 World.Rand。
type detRand struct{ s uint32 }

// newDetRand 以种子构造。
func newDetRand(seed uint32) *detRand { return &detRand{s: seed} }

// next 返回下一个 uint32。
func (r *detRand) next() uint32 {
	r.s += 0x6D2B79F5
	t := r.s
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + (t ^ (t >> 7)) * (t | 61)
	return t ^ (t >> 14)
}

// float64 返回 [0,1) 均匀分布。
func (r *detRand) float64() float64 { return float64(r.next()>>8) / float64(1<<24) }

// intn 返回 [0,n) 整数。
func (r *detRand) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint32(n))
}

// ── 画像分配 ────────────────────────────────────────────────────────────

// assignBiasProfiles 为每个座位分配 2~3 条偏差画像(确定性;按 seat 派生)。
// 在 NewWorld 构造玩家后调用一次,保证同 seat 每次对局画像一致。
func (w *World) assignBiasProfiles() {
	for seat, p := range w.Players {
		if p == nil {
			continue
		}
		assignBiasProfile(seat, p)
	}
}

// assignBiasProfile 为单个座位分配偏差画像(中途入座时也必须调用 ——
// 房间层会用 newPlayerFromCard 覆盖 Players[seat],覆盖后画像会丢)。
func assignBiasProfile(seat int, p *Player) {
	if p == nil {
		return
	}
	p.BiasProfile = pickBiasProfile(seat)
	p.BiasImmunity = map[string]float64{}
	p.MentalAccount = map[string]int64{}
}

// pickBiasProfile 确定性采样 2~3 条偏差代码(Fisher-Yates 洗牌后取前 n)。
func pickBiasProfile(seat int) []string {
	r := newDetRand(uint32(1000 + seat*7919))
	pool := make([]BiasDef, len(biasCatalog))
	copy(pool, biasCatalog)
	for i := len(pool) - 1; i > 0; i-- {
		j := r.intn(i + 1)
		pool[i], pool[j] = pool[j], pool[i]
	}
	n := 2 + r.intn(2) // 2~3 条
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, pool[i].Code)
	}
	return out
}

// hasBiasCode 玩家画像是否含该偏差(且未被反思解除)。
func (p *Player) hasBiasCode(code string) bool {
	if p == nil {
		return false
	}
	for _, c := range p.BiasProfile {
		if c == code {
			return !p.biasResolved(c)
		}
	}
	return false
}

// biasResolved 该偏差是否已被"主动反思"解除(§4.3)。
func (p *Player) biasResolved(code string) bool {
	if p == nil || p.ResolvedBiases == nil {
		return false
	}
	return p.ResolvedBiases[code]
}

// ── 认知检定(§4.1) ────────────────────────────────────────────────────

// applyBias 认知检定:返回该偏差在当前认知下的最终乘子与是否已被识破。
//
//	通过(Cognition ≥ Threshold):效果减弱 50%(偏离 1 的部分砍半)⇒ detected=true,
//	  允许向玩家/Agent 显示偏差名与学术解释。
//	未通过:全额生效(用 Exaggerated 加重值)⇒ detected=false,
//	  **不得**向玩家/Agent 显示偏差名(教学闭环的前提,复盘才揭示)。
//	免疫进度 ≥ 0.80(§4.4.2):该偏差本回合不生效(乘子 1.0)+ detected=true。
//	深度反思的本回合免疫(BiasImmuneThisTurn):全部偏差不生效。
//	复盘预习(BiasPreviewNextTurn):无论认知是否达标,一律视为"通过"。
//
// 检定本身不消耗认知点(§4.1 末行)。
func applyBias(p *Player, def BiasDef) (float64, bool) {
	if p == nil {
		return 1, false
	}
	if p.BiasImmuneThisTurn {
		return 1, true
	}
	if p.BiasImmunity != nil && p.BiasImmunity[def.Code] >= biasImmunityBlock {
		return 1, true
	}
	passed := p.Cognition >= def.Threshold || p.BiasPreviewNextTurn
	if passed {
		return 1 + (def.Mul-1)/2, true
	}
	return def.Exaggerated, false
}

// biasEffectMulOf 取指定**效果类别**下该玩家所有生效偏差的乘子(逐条相乘)。
//
// 返回:总乘子、已识破的偏差定义列表(可展示)、未识破的偏差 code 列表
// (**不可**向玩家/Agent 暴露 —— 教学闭环的前提)。
//
// 参与条件:在画像内 ∧ 未被反思解除 ∧ Guard 通过 ∧ 认知检定后乘子 ≠ 1。
// 免疫进度达标(≥0.80)⇒ 乘子 1.0(不生效),但记作"已识破"(UI 显示"已免疫")。
func (p *Player) biasEffectMulOf(effect BiasEffect, ctx *BiasCtx) (float64, []BiasDef, []string) {
	mul := 1.0
	var detected []BiasDef
	var hidden []string
	if p == nil {
		return mul, detected, hidden
	}
	for _, code := range p.BiasProfile {
		if p.biasResolved(code) {
			continue
		}
		def, ok := biasByCode(code)
		if !ok || def.Effect != effect {
			continue
		}
		if def.Guard != nil && !def.Guard(p, ctx) {
			continue
		}
		m, det := applyBias(p, def)
		if m == 1 {
			detected = append(detected, def)
			continue
		}
		mul *= m
		// 批次73 O1:偏差实际生效(m≠1)计数。计数放在本函数(经济应用的
		// 唯一漏斗)而非 applyBias —— applyBias 同时被 behaviorJSONFor 等
		// 展示路径每次 game.state 重建调用,会在那里重复计数(实施记录
		// 偏离 1)。
		p.BiasHits++
		if det {
			detected = append(detected, def)
			// 局内成功抵抗 ⇒ 免疫进度 +0.15(§4.4.2)。
			p.bumpBiasImmunity(def.Code, biasImmunityResist)
		} else {
			hidden = append(hidden, def.Code)
			// 踩坑且未抵抗 ⇒ 免疫进度 −0.05(§4.4.2)。
			p.bumpBiasImmunity(def.Code, biasImmunityFall)
		}
	}
	return mul, detected, hidden
}

// bumpBiasImmunity 调整某偏差的免疫进度(钳制到 [0,1])。
func (p *Player) bumpBiasImmunity(code string, delta float64) {
	if p.BiasImmunity == nil {
		p.BiasImmunity = map[string]float64{}
	}
	v := p.BiasImmunity[code] + delta
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.BiasImmunity[code] = v
}

// ── 主动反思(§4.3) ────────────────────────────────────────────────────

// 反思模式常量。
const (
	ReflectModeLight   = "light"   // 认知 −1:解除 1 张当前生效偏差的剩余效果
	ReflectModeDeep    = "deep"    // 认知 −2:解除全部 + 本回合免疫新偏差
	ReflectModePreview = "preview" // 认知 −1:下一回合偏差触发一律视为"通过"
)

// actReflect 主动反思(§4.3)。限制:每回合最多 1 次;认知 ≤ 1 不可执行。
func (w *World) actReflect(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.Cognition <= 1 {
		return "", errcode.Code(errcode.ErrVirtualCityReflectCognitionLow)
	}
	if p.ReflectCountThisMonth >= 1 {
		return "", errcode.Code(errcode.ErrVirtualCityReflectLimit)
	}
	mode := a.ReflectMode
	switch mode {
	case ReflectModeLight, ReflectModeDeep, ReflectModePreview:
	default:
		return "", errcode.CodeMsg(errcode.ErrVirtualCityReflectModeInvalid,
			"reflect_mode must be light|deep|preview")
	}
	switch mode {
	case ReflectModeLight:
		p.Cognition--
		resolved := p.resolveOneBias()
		if !resolved {
			return "", errcode.Code(errcode.ErrVirtualCityReflectNothingToResolve)
		}
	case ReflectModeDeep:
		p.Cognition -= 2
		p.resolveAllBiases()
		p.BiasImmuneThisTurn = true
	case ReflectModePreview:
		p.Cognition--
		p.BiasPreviewNextTurn = true
		p.BiasPreviewNextTurnMonth = w.Month
	}
	p.ReflectCountThisMonth++
	p.LastReflect = mode
	var text string
	switch mode {
	case ReflectModeLight:
		text = "轻度反思(认知 −1):解除 1 项正在生效的行为偏差"
	case ReflectModeDeep:
		text = "深度反思(认知 −2):解除全部生效偏差,本回合免疫新偏差"
	case ReflectModePreview:
		text = "复盘预习(认知 −1):下一回合偏差效果减弱 50% 且可被识破"
	}
	w.spendBudget(p, "resting", text)
	return text, nil
}

// resolveOneBias 解除 1 项生效偏差(按画像顺序取第一项未解除者)。
func (p *Player) resolveOneBias() bool {
	if p.ResolvedBiases == nil {
		p.ResolvedBiases = map[string]bool{}
	}
	for _, code := range p.BiasProfile {
		if p.ResolvedBiases[code] {
			continue
		}
		p.ResolvedBiases[code] = true
		return true
	}
	return false
}

// resolveAllBiases 解除全部画像偏差。
func (p *Player) resolveAllBiases() {
	if p.ResolvedBiases == nil {
		p.ResolvedBiases = map[string]bool{}
	}
	for _, code := range p.BiasProfile {
		p.ResolvedBiases[code] = true
	}
}

// ── 双曲贴现:延期支付(§3.3.1) ─────────────────────────────────────────

// DeferredPayment 一笔延期支付(本月免付,到期月 ×1.3)。
type DeferredPayment struct {
	AmountCNY   int64   `json:"amount_cny"`
	DueMonth    int     `json:"due_month"`
	PenaltyRate float64 `json:"penalty_rate"`
	Note        string  `json:"note,omitempty"`
}

// actDeferPayment 延期支付:本月免付,3 个月后按 ×1.3 扣款(§3.3.1)。
// 仅画像含"双曲贴现"的居民可用(该偏差提供此选项)。
func (w *World) actDeferPayment(p *Player, a Action) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if !p.hasBiasCode("hyperbolic_discount") {
		return "", errcode.Code(errcode.ErrVirtualCityDeferNotAvailable)
	}
	if a.AmountCNY < 1 {
		return "", errcode.CodeMsg(errcode.ErrVirtualCityAssetInvalid, "defer amount_cny must be >= 1")
	}
	if p.Cash < a.AmountCNY {
		return "", errcode.Code(errcode.ErrVirtualCityInsufficientCash)
	}
	note := a.Reason
	if len([]rune(note)) > 40 {
		note = string([]rune(note)[:40])
	}
	if note == "" {
		note = "延期消费"
	}
	p.DeferredPayments = append(p.DeferredPayments, DeferredPayment{
		AmountCNY:   a.AmountCNY,
		DueMonth:    w.Month + deferredPayMonths,
		PenaltyRate: deferredPayPenalty,
		Note:        note,
	})
	p.DelayedConsumeCount++
	text := fmt.Sprintf("延期支付 ¥%d(本月免付,第 %d 月按 ×%.1f 扣款)",
		a.AmountCNY, w.Month+deferredPayMonths, deferredPayDueMult)
	w.spendBudget(p, "idle", text)
	return text, nil
}

// settleDeferredPayments 月结:到期扣款(×1.3;现金不足则扣到 0 并记违约)。
// 返回本座位本月因延期支付而多付的金额(供复盘展示)。
func (w *World) settleDeferredPayments(p *Player) int64 {
	if len(p.DeferredPayments) == 0 {
		return 0
	}
	var extra int64
	kept := p.DeferredPayments[:0]
	for _, d := range p.DeferredPayments {
		if d.DueMonth > w.Month {
			kept = append(kept, d)
			continue
		}
		due := int64(float64(d.AmountCNY)*deferredPayDueMult + 0.5)
		if due > p.Cash {
			// 现金不足:扣到 0,记违约(不判负数现金)。
			if p.Cash > 0 {
				w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), p.Cash, CatConsume, "延期支付(现金不足,违约)")
				extra += p.Cash
				// 批次71 §3.3:此处 p.Cash 已被上面的 w.Pay 扣为 0 ⇒
				// 属**假账外**(钱已进账本)。仍登记一条金额恒为 0 的条目,
				// 使「所有直接写 p.Cash 的位置都已在账外登记簿声明」字面成立。
				w.offLedgerRecord(w.Month, p.Seat, OffLedgerDeferredDefault, -p.Cash, "延期支付违约现金归零")
				p.Cash = 0
			}
			p.DeferredDefaultCount++
			w.emitEvent("settle", p.Seat, fmt.Sprintf("%d 号位延期支付违约(欠 ¥%d)", p.Seat, due))
			continue
		}
		w.Pay(p.Seat, SeatEntity(p.Seat), w.consumerPayTo(), due, CatConsume, "延期支付(含 30% 贴现成本)")
		extra += due - d.AmountCNY
	}
	p.DeferredPayments = kept
	return extra
}

// ── 心理账户(§3.2) ────────────────────────────────────────────────────

// 资金来源标签(§3.2.1 六类)。标签**不影响购买力**,只用于行为追踪与展示。
const (
	MoneyTagSalary     = "salary"     // 工资
	MoneyTagInvestment = "investment" // 投资收益
	MoneyTagSide       = "side"       // 副业
	MoneyTagWindfall   = "windfall"   // 意外之财
	MoneyTagGift       = "gift"       // 礼金
	MoneyTagRefund     = "refund"     // 退款
)

// moneyTagLabels 标签中文名。
var moneyTagLabels = map[string]string{
	MoneyTagSalary:     "工资",
	MoneyTagInvestment: "投资收益",
	MoneyTagSide:       "副业",
	MoneyTagWindfall:   "意外之财",
	MoneyTagGift:       "礼金",
	MoneyTagRefund:     "退款",
}

// moneyTagOrder 展示顺序(与 §3.2.1 六类一致)。
var moneyTagOrder = []string{
	MoneyTagSalary, MoneyTagInvestment, MoneyTagSide,
	MoneyTagWindfall, MoneyTagGift, MoneyTagRefund,
}

// tagIncome 收入入账时给资金打标签(§3.2.1)。
func (p *Player) tagIncome(tag string, amount int64) {
	if p == nil || amount <= 0 {
		return
	}
	if p.MentalAccount == nil {
		p.MentalAccount = map[string]int64{}
	}
	p.MentalAccount[tag] += amount
}

// mentalAccountWindfall 可动用的"意外之财"余额(用于心理账户偏差判定)。
func (p *Player) mentalAccountWindfall() int64 {
	if p == nil || p.MentalAccount == nil {
		return 0
	}
	return p.MentalAccount[MoneyTagWindfall]
}

// actMergeAccounts 合并心理账户(§3.2.3):把资金来源标签归并为"统一资金池",
// 合并后"意外之财"的消费放大效果立即消失。
//
// 这个按钮本身就是教育工具 —— 它的存在提醒玩家"钱是可替代的"。
func (w *World) actMergeAccounts(p *Player) (string, *errcode.Error) {
	if p.ActionBudget <= 0 {
		return "", errcode.Code(errcode.ErrVirtualCityActionBudgetExhausted)
	}
	if p.MentalAccountMerged {
		return "", errcode.Code(errcode.ErrVirtualCityMergeAlreadyDone)
	}
	p.MentalAccountMerged = true
	text := "合并心理账户:所有资金来源标签归并为统一资金池(钱的标签从此不再影响你的消费决策)"
	w.spendBudget(p, "idle", text)
	return text, nil
}

// ── 锚定指数(§3.1.4) ──────────────────────────────────────────────────

// AnchorSample 一次买入的锚定样本。
type AnchorSample struct {
	Asset         string
	DecisionPrice float64 // 决策价(玩家上次看到的价格 = 上月末观测价)
	FairPrice     float64 // 当前公允价
	BuyPrice      float64 // 实际成交价
}

// anchorSampleCap 锚定样本保留上限(滚动窗口)。
const anchorSampleCap = 12

// recordAnchorSample 记录一次买入的锚定样本;分母 |买入价 − 公允价| 趋零时跳过。
func (p *Player) recordAnchorSample(s AnchorSample) {
	denom := math.Abs(s.BuyPrice - s.FairPrice)
	if denom < 1e-9 {
		return
	}
	p.AnchorSamples = append(p.AnchorSamples, s)
	if len(p.AnchorSamples) > anchorSampleCap {
		p.AnchorSamples = p.AnchorSamples[len(p.AnchorSamples)-anchorSampleCap:]
	}
}

// anchoringIndex 锚定指数 = Σ(|决策价 − 公允价| / |买入价 − 公允价|) / 样本数(§3.1.4)。
// 无样本 ⇒ 0。
func (p *Player) anchoringIndex() float64 {
	if p == nil || len(p.AnchorSamples) == 0 {
		return 0
	}
	sum := 0.0
	n := 0
	for _, s := range p.AnchorSamples {
		denom := math.Abs(s.BuyPrice - s.FairPrice)
		if denom < 1e-9 {
			continue
		}
		sum += math.Abs(s.DecisionPrice-s.FairPrice) / denom
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// ── 羊群指数(§3.4.4) ──────────────────────────────────────────────────

// herdingIndex 羊群指数 = 跟风次数 / (跟风 + 逆势 + 中性)(§3.4.4)。
func (p *Player) herdingIndex() float64 {
	if p == nil {
		return 0
	}
	total := p.HerdFollowCount + p.HerdCounterCount + p.HerdNeutralCount
	if total == 0 {
		return 0
	}
	return float64(p.HerdFollowCount) / float64(total)
}

// ── 认知跨局继承(§4.4.1) ──────────────────────────────────────────────

// cognitionCarryOver 本局结束时可带入下一局的认知点(30%,向下取整)。
func (p *Player) cognitionCarryOver() int {
	if p == nil {
		return 0
	}
	return int(math.Floor(float64(p.Cognition) * cognitionCarryOverRate))
}
