// Package virtual_city — events.go: 人生事件(钟声 + 月度失业)(2026-09-14 §财商流P0)。
//
// 契约: 后端架构文档 §10。掷骰全部走注入的 *rand.Rand;负面事件概率 ×
// (1 − min(0.05×认知, 0.30))(《规则》§2.6)。所有事件产生 EventRecord
// + Ledger 记录(健康费用 → world 医疗;婚礼 → world;失业期间无 Ledger)。
package virtual_city

import (
	"fmt"
	"strings"
)

// 城市背景层事件类型(EventRecord.Type;2026-09-21 §虚拟城市)。
const (
	// EventCityVoice 城市之声单条(月结后异步产出,room_city.go emit)。
	EventCityVoice = "city_voice"
	// EventCityProfiles 居民人物卡档案锚定终态(2026-09-21 §档案锚定契约 §5:
	// 仅终态发一条 —— hydrating 中间态走 Snapshot 轮询,不发事件)。
	EventCityProfiles = "city_profiles"
)

// negativeGuard 负面事件认知折扣:×(1 − min(0.05×K, 0.30))。
func negativeGuard(cognition int) float64 {
	f := 0.05 * float64(cognition)
	if f > 0.30 {
		f = 0.30
	}
	return 1 - f
}

// MonthlyEvents 月度事件(§10.2):失业判定。acting 窗口结束后、月结前调用。
func (w *World) MonthlyEvents() {
	for _, seat := range w.alivePlayers() {
		p := w.Players[seat]
		if p.UnemployedMonths > 0 {
			continue // 已在失业期。
		}
		if p.SalaryBase <= 0 && !p.SalaryVolatile {
			continue
		}
		var base float64
		switch w.Market.CyclePhase {
		case PhaseRecession:
			base = 0.15
		case PhaseDepression:
			base = 0.25
		default:
			base = 0.05
		}
		// P1: 个体失业概率 × 内生失业率倍率(§4.3)。U=自然率 5% 时 p=base
		// (与 P0 等价);危机 U=12.5% → ×2.5;过热 U=2% → ×0.4→clamp 0.5。
		// economy_enabled=false → 完整回退 P0(factor 恒 1,rand 消费序列不变)。
		unemployFactor := 1.0
		if w.EconomyEnabled && w.Labor != nil {
			unemployFactor = clampF(w.Labor.Unemployment/UnemployNatural, 0.5, 2.5)
		}
		if w.Rand.Float64() >= base*unemployFactor*negativeGuard(p.Cognition) {
			continue
		}
		months := 2 + w.Rand.Intn(5) // U(2,6) 月,不变
		ratio := w.sampleRehireRatio()
		p.UnemployedMonths = months
		p.RehireSalaryRatio = ratio
		w.emitEvent("life", seat, fmt.Sprintf("%d 号位(%s)被裁员,失业 %d 个月", seat, p.Card.Title, months))
	}

	// P1-4(§财商流P1-4 §5.2): 尾部意外事件掷骰。insurance_enabled=false 时
	// 不掷骰(零 rand 消费,固定种子存量对局回归零偏移)。
	if w.InsuranceEnabled {
		for _, seat := range w.alivePlayers() {
			w.rollAccident(seat)
		}
	}
}

// advanceUnemployment 失业期推进(月结步骤1 前调用):期满自动再就业
// 80–120% 工资。
func (w *World) advanceUnemployment(p *Player) {
	if p.UnemployedMonths <= 0 {
		return
	}
	p.UnemployedMonths--
	if p.UnemployedMonths == 0 {
		ratio := p.RehireSalaryRatio
		if ratio <= 0 {
			ratio = 1
		}
		p.SalaryBase = int64(float64(p.SalaryBase)*ratio + 0.5)
		p.SalaryLow = int64(float64(p.SalaryLow) * ratio)
		p.SalaryHigh = int64(float64(p.SalaryHigh) * ratio)
		p.StatusIcon = "working"
		w.emitEvent("life", p.Seat, fmt.Sprintf("%d 号位再就业,月薪调整为 ¥%d(%d%%)", p.Seat, p.SalaryBase, int(ratio*100)))
	}
}

// BellEvents 钟声事件(§10.1):每 60 月,按卡数据。年调整后调用。
func (w *World) BellEvents() {
	for _, seat := range w.alivePlayers() {
		p := w.Players[seat]

		// 结婚:单身 40%。
		if p.Family.Marital == "single" && w.Rand.Float64() < 0.40 {
			p.Family.Marital = "married"
			p.Family.SpouseIncome = 6000
			p.Energy -= 2
			p.Network += 2
			if p.Network > 10 {
				p.Network = 10
			}
			wedding := 30000 + int64(w.Rand.Intn(70001)) // U(30,000, 100,000)
			if p.Cash < wedding {
				wedding = p.Cash // 有多少花多少(现金不为负)
			}
			if wedding > 0 {
				w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), wedding, CatWedding, "婚礼开销")
			}
			w.emitEvent("life", seat, fmt.Sprintf("%d 号位结婚了!婚礼支出 ¥%d,配偶月入 ¥6000", seat, wedding))
		}

		// 生育:已婚 50%(子女 <3)。
		if p.Family.Marital == "married" && p.Family.Children < 3 && w.Rand.Float64() < 0.50 {
			p.Family.Children++
			// 阶段4(2026-09-21 §城市扩张v2.12):记录生育月份 ——
			// 儿童津贴"6 岁以下"判定口径(transfer_payment.go::childrenUnder6)。
			p.BirthMonths = append(p.BirthMonths, w.Month)
			p.Energy -= 3
			p.Cognition++
			if p.Cognition > 10 {
				p.Cognition = 10
			}
			w.emitEvent("life", seat, fmt.Sprintf("%d 号位迎来第 %d 个孩子(月支出 +¥5000)", seat, p.Family.Children))
		}

		// 健康事件:按卡 health_grade A 5% / B 12% / C 25%。
		var healthP float64
		switch p.Card.HealthGrade {
		case "A":
			healthP = 0.05
		case "C":
			healthP = 0.25
		default:
			healthP = 0.12
		}
		if w.Rand.Float64() < healthP*negativeGuard(p.Cognition) {
			cost := 10000 + int64(w.Rand.Intn(190001)) // U(10,000, 200,000)
			if cost > p.Cash {
				cost = p.Cash
			}
			if cost > 0 {
				w.Pay(seat, SeatEntity(seat), w.consumerPayTo(), cost, CatMedical, "医疗支出")
			}
			p.HadIllnessYear = true
			p.Energy -= 5
			if p.Energy < -3 {
				p.Energy = -3
			}
			if cost >= 100000 {
				// 重疾:置 MajorIllness、停 1 月(以精力代价近似,停赛走 StoppedMonths)。
				p.MajorIllness = true
				if p.StoppedMonths < 1 {
					p.StoppedMonths = 1
				}
				w.emitEvent("life", seat, fmt.Sprintf("%d 号位罹患重疾,医疗支出 ¥%d,休养 1 个月", seat, cost))
			} else {
				w.emitEvent("life", seat, fmt.Sprintf("%d 号位健康亮红灯,医疗支出 ¥%d", seat, cost))
			}
			// P1-4(§财商流P1-4 §5.1): 医疗事件理赔管线(百万医疗报销 90% +
			// 重疾定额给付;等待期/未投保不赔)。isMajor 与既有重疾判定同阈值。
			w.SettleMedicalClaims(seat, cost, cost >= 100000)
		}

		// 生日恢复:当年无大病 → 精力 +2(《规则》§2.4)。
		if !p.HadIllnessYear {
			p.Energy += 2
			if p.Energy > 10 {
				p.Energy = 10
			}
		}
		p.HadIllnessYear = false // 年度标记滚动
	}
}

// ── 事件 → 感官标签映射(2026-09-22 §CityHuman重构,设计文档 1 §3.2 动态叠加) ──
//
// ambianceOverlay 把本月事件流水映射为气味/声响标签,叠加到城区感官基底上,
// 供 smell/hear 感知工具与 city.ambiance 下发使用。规则式确定性映射,不调 LLM。

// ambianceOverlay 从本月事件提取动态气味/声响标签(去重,各 ≤4 条)。
func ambianceOverlay(events []EventRecord) (smells, sounds []string) {
	seenS, seenH := map[string]bool{}, map[string]bool{}
	add := func(dst *[]string, seen map[string]bool, tags ...string) {
		for _, t := range tags {
			if len(*dst) >= 4 {
				return
			}
			if !seen[t] {
				seen[t] = true
				*dst = append(*dst, t)
			}
		}
	}
	for _, ev := range events {
		text := ev.Text
		switch {
		case containsAny(text, "裁员", "失业", "破产"):
			add(&smells, seenS, "焦虑汗味")
			add(&sounds, seenH, "叹息声")
		case containsAny(text, "重疾", "医疗", "健康亮红灯", "身故"):
			add(&smells, seenS, "消毒水味")
			add(&sounds, seenH, "救护车笛")
		case containsAny(text, "结婚", "婚礼"):
			add(&smells, seenS, "喜糖甜香", "酒宴香气")
			add(&sounds, seenH, "婚礼鞭炮", "喜乐声")
		case containsAny(text, "孩子", "生育"):
			add(&sounds, seenH, "婴儿啼哭")
		case containsAny(text, "迁居", "搬家"):
			add(&sounds, seenH, "搬家货车", "家具搬运声")
		case containsAny(text, "调研", "集市"):
			add(&smells, seenS, "烟火与食物香气")
			add(&sounds, seenH, "集市叫卖")
		}
		// 城市之声本身也是一种环境声。
		if ev.Type == EventCityVoice {
			add(&sounds, seenH, "街头议论")
		}
	}
	return smells, sounds
}

// containsAny 报告 s 是否包含任一子串。
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
