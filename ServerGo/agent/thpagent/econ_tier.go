// Package thpagent — econ_tier.go: 德州扑克经济档位(2026-08-19 §德州扑克金币 §132 §133)。
//
// 设计动机:与狼人杀 econ_tier 同源,但抽水率映射不同(德扑赢家扣抽水,狼人杀系统吸收彩池份额),
// 按 §133 教训 1「EconTier 独立常量原则」**不复用** werewolf.EconTier 常量;
// 与 thpagent/agent.go 同包,自然访问(无循环 import)。
//
// 抽水率映射(德扑赢家份额):
//   - Health(房间总金币 ≥ 50K) → 5%  标准抽水
//   - Caution(房间总金币 10K~50K) → 7%  适度抽水
//   - Danger(房间总金币 < 10K)  → 10% 反通胀高抽水
//
// 与狼人杀对比:
//   - 狼人杀系统吸收彩池份额(§133),德扑赢家按比例扣抽水
//   - 触发时机不同:狼人杀在 UseProp,德扑在手牌结算
//   - 阈值相同(50K / 10K),便于 operator 直观调档
package thpagent

// EconTier 是房间经济档位枚举(独立常量,不与 werewolf.EconTier 复用)。
type EconTier string

const (
	// EconHealth 健康档:房间总金币 ≥ 50K,抽水 5%。
	EconHealth EconTier = "health"
	// EconCaution 警戒档:房间总金币 10K-50K,抽水 7%。
	EconCaution EconTier = "caution"
	// EconDanger 危险档:房间总金币 < 10K,抽水 10%。
	EconDanger EconTier = "danger"
)

const (
	// EconCautionThreshold 健康档下限(单位:金币)。
	EconCautionThreshold int64 = 50000
	// EconDangerThreshold 警戒档下限。
	EconDangerThreshold int64 = 10000
)

// RakeRatePct 返回档位对应的抽水率(百分数整数,例 5 表示 5%)。
func (t EconTier) RakeRatePct() int {
	switch t {
	case EconCaution:
		return 7
	case EconDanger:
		return 10
	default:
		return 5 // 默认 Health
	}
}

// RakeRate 返回档位对应的抽水率(0.0-1.0)。
func (t EconTier) RakeRate() float64 {
	return float64(t.RakeRatePct()) / 100.0
}

// ComputeEconTier 根据房间总金币存量判定档位。
//   - ≥ 50K → Health
//   - 10K-50K → Caution
//   - < 10K → Danger
func ComputeEconTier(roomTotalCoin int64) EconTier {
	switch {
	case roomTotalCoin >= EconCautionThreshold:
		return EconHealth
	case roomTotalCoin >= EconDangerThreshold:
		return EconCaution
	default:
		return EconDanger
	}
}

// ApplyRake 按档位抽水(赢家份额 = payout - payout * rake_rate,封顶原 payout)。
//   - payout > 0:扣抽水后净额(同 §132 potReturn)
//   - payout <= 0:透传(输家不抽水)
//
// 仅做整数运算(与 wallet service 整数接口对齐);调用方应保证金额单位一致(筹码/金币)。
func ApplyRake(payout int64, tier EconTier) (netPayout int64, rakeAmount int64) {
	if payout <= 0 {
		return 0, 0
	}
	rake := int64(float64(payout) * tier.RakeRate())
	if rake < 0 {
		rake = 0
	}
	return payout - rake, rake
}