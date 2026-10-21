// Package virtual_city — exogenous.go: 批次66 外盘情绪锚(真实行情 → 游戏内市场联动)。
//
// 真实世界 A股/港股/美股/黄金/加密行情(批次65 citychrome.FeedStore 采集)经本文件
// 收敛为确定性情绪分,给游戏内股票/黄金月度漂移加一个**微弱**加法偏置(±0.3%/±0.2%
// 封顶)。真实行情是「锚」不是「驱动」——游戏内周期仍由 PhaseTable 与批次62 泡沫
// 反身性主导(契约 01 §2 关键决策 2)。
//
// 引擎纯度:本文件无 IO、不 import citychrome;真实报价经 ExoSource(依赖反转,
// manager.SetExoSource)以中性 ExoQuote 结构注入,Feed→ExoQuote 映射在 main.go
// 装配层完成(契约 01 §2 关键决策 1)。
package virtual_city

import (
	"fmt"
	"math"
)

// ── 批次66 外盘情绪锚常量(契约 01 §3.1;确定性,禁运行时配置漂移) ──

const (
	// exoFreshWindowSec 报价保鲜窗口:超过 72h 无更新 ⇒ 全部视作过期。
	exoFreshWindowSec int64 = 72 * 3600
	// exoTanhScale 情绪分 tanh 饱和尺度:综合变化 2.2% ⇒ sentiment ≈ ±0.76。
	exoTanhScale = 2.2
	// exoStockBiasCap / exoGoldBiasCap 月漂移加法项封顶(±0.3% / ±0.2%)。
	exoStockBiasCap = 0.003
	exoGoldBiasCap  = 0.002
)

// 外盘类目字面量(与 citychrome.Category* 同值;引擎不 import citychrome,
// 由装配层保证一致性)。
const (
	exoCatAShare  = "a_share"
	exoCatHK      = "hk"
	exoCatUS      = "us"
	exoCatMetal   = "metal"
	exoCatCrypto  = "crypto"
)

// ExoQuote 中性真实报价(装配层从 citychrome.Feed 映射;引擎不感知来源)。
// ChangePct 为百分数口径(0.82 = 0.82%),与批次65 契约一致。
type ExoQuote struct {
	Category  string  // "a_share"|"hk"|"us"|"metal"|"crypto"
	ChangePct float64 // 百分数口径
	UpdatedAt int64   // Unix 秒
}

// ExoSignal 外盘情绪锚(SettleMonth ④ 市场漂移消费)。
// Active=false ⇒ 两 bias 恒精确 0.0(零回归契约 01 §2 关键决策 3)。
type ExoSignal struct {
	Sentiment float64 // [-1,+1] 风险偏好分(tanh 饱和)
	StockBias float64 // 股票月漂移加法项,封顶 ±0.003
	GoldBias  float64 // 黄金月漂移加法项,封顶 ±0.002;与 Sentiment 反向(避险)
	Active    bool    // false ⇒ 两 bias 恒 0.0
	Source    string  // 展示口径,固定 "真实外盘"
	UpdatedAt int64   // 参与计算的最新报价时间
}

// ComputeExoSignal 把真实报价收敛为外盘情绪锚。确定性:无随机、无时钟依赖
// (now 作入参)、同输入同输出。
//
// 公式(契约 01 §3.1):
//
//	eqAvg   = mean(a_share/hk/us 各类目均值)   // 缺类目跳过
//	riskAvg = mean(crypto)
//	goldAvg = mean(metal)
//	Sentiment = clamp(tanh((0.60*eqAvg + 0.25*riskAvg − 0.15*goldAvg)/2.2), −1, +1)
//	StockBias = Sentiment × 0.003
//	GoldBias  = −Sentiment × 0.002
//
// 全部报价过期(>72h)或 quotes 为空 ⇒ Active=false 且两 bias 精确 0.0。
// 类目缺省只降权不置 inactive(如 crypto mission 未完成时仅用股票类目)。
func ComputeExoSignal(quotes []ExoQuote, now int64) ExoSignal {
	sig := ExoSignal{Source: "真实外盘"}
	// 保鲜过滤 + 分桶累加。
	var eqSum, eqN, riskSum, riskN, goldSum, goldN float64
	var newest int64
	for _, q := range quotes {
		if q.UpdatedAt <= 0 || now-q.UpdatedAt > exoFreshWindowSec {
			continue
		}
		if q.UpdatedAt > newest {
			newest = q.UpdatedAt
		}
		switch q.Category {
		case exoCatAShare, exoCatHK, exoCatUS:
			eqSum += q.ChangePct
			eqN++
		case exoCatCrypto:
			riskSum += q.ChangePct
			riskN++
		case exoCatMetal:
			goldSum += q.ChangePct
			goldN++
		}
	}
	if eqN+riskN+goldN == 0 {
		return sig // Active=false,bias 零值
	}
	eqAvg := eqSum / math.Max(eqN, 1) // 各类目均值后按类目等权;此处扁平化为报价等权
	if eqN == 0 {
		eqAvg = 0
	}
	riskAvg := riskSum / math.Max(riskN, 1)
	if riskN == 0 {
		riskAvg = 0
	}
	goldAvg := goldSum / math.Max(goldN, 1)
	if goldN == 0 {
		goldAvg = 0
	}
	// 综合变化(百分数)→ tanh 饱和到 [-1,+1]。
	raw := 0.60*eqAvg + 0.25*riskAvg - 0.15*goldAvg
	sent := math.Tanh(raw / exoTanhScale)
	if sent > 1 {
		sent = 1
	} else if sent < -1 {
		sent = -1
	}
	sb := sent * exoStockBiasCap
	gb := -sent * exoGoldBiasCap
	sig.Sentiment = sent
	sig.StockBias = clampExoAbs(sb, exoStockBiasCap)
	sig.GoldBias = clampExoAbs(gb, exoGoldBiasCap)
	sig.Active = true
	sig.UpdatedAt = newest
	return sig
}

// clampExoAbs 把偏置封顶到 ±cap(防御:tanh 已在界内,封顶是双保险)。
func clampExoAbs(v, cap float64) float64 {
	if v > cap {
		return cap
	}
	if v < -cap {
		return -cap
	}
	return v
}

// exoBriefLocked 月度 prompt「■ 外盘情绪锚」小节(批次66;调用方持房间锁)。
// Active=false ⇒ 返回空串整段省略(与批次27 天气零回归范式一致)。
// 文案含 realworld_info 引导(批次65 遗留 #1 收口)。
func exoBriefLocked(w *World) string {
	if w == nil || w.Market == nil {
		return ""
	}
	e := w.Market.Exo
	if !e.Active {
		return ""
	}
	dir := "中性"
	if e.Sentiment > 0.05 {
		dir = "偏多"
	} else if e.Sentiment < -0.05 {
		dir = "偏空"
	}
	return fmt.Sprintf(
		"真实外盘风险情绪 %+.2f(%s):本月游戏内股票漂移 %+.2f%%、黄金 %+.2f%%(封顶 ±0.3%%)。行情详情可用 realworld_info(op=quotes|news) 查询(不耗动作预算)。",
		e.Sentiment, dir, e.StockBias*100, e.GoldBias*100)
}
