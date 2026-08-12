// Package werewolf — econ_tier.go: 经济档位感知（v4 → v5 增强）。
//
// 设计动机（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §13.2 + §16.3）：
//   - v3 经济模型"50%彩池/30%销毁/20%补偿"是硬切，无法应对不同房间的金币存量分布。
//   - v4 引入 3 档（Health / Caution / Danger），解决两端极端（>50K 富足 / <10K 通缩）
//     但忽略 Boom（>100K 富到爆 → 刺激消费）与 Critical（<5K 流动性枯竭 → 强反通胀）。
//   - v5 把档位扩到 5 档：
//     - Boom     (≥ 100K) → 销毁 20% / 彩池 60%（通胀刺激消费）
//     - Health   (50K-100K) → 销毁 30% / 彩池 50%（v4 默认）
//     - Caution  (10K-50K) → 销毁 40% / 彩池 40%（v4 中间档）
//     - Danger   (5K-10K) → 销毁 45% / 彩池 30%（v4 微调）
//     - Critical (< 5K)    → 销毁 60% / 彩池 20%（极端反通胀）
//
// 阈值可由 LsmAgentGame.conf 配置(5 个常量均为变量;ConfigureEconTier 注入);
// 默认值与常量一致。
//
// 2026-07-21 v5 重构（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §16.3）。
package werewolf

import (
	"fmt"
)

// EconTier 是房间的经济档位（v4 + v5）。
type EconTier string

const (
	// EconBoom 暴富档：房间总金币 ≥ BoomThreshold，销毁 20% / 彩池 60%（v5 新增）。
	EconBoom EconTier = "boom"
	// EconHealth 健康档：50K ~ 100K，销毁 30% / 彩池 50%（v4 默认）。
	EconHealth EconTier = "health"
	// EconCaution 警戒档：10K ~ 50K，销毁 40% / 彩池 40%（v4）。
	EconCaution EconTier = "caution"
	// EconDanger 危险档：5K ~ 10K，销毁 45% / 彩池 30%（v5 微调）。
	EconDanger EconTier = "danger"
	// EconCritical 危急档：< 5K，销毁 60% / 彩池 20%（v5 新增）。
	EconCritical EconTier = "critical"
)

// EconTier 阈值常量（v5 §16.3 配置；可由 LsmAgentGame.conf + ConfigureEconTier 注入）。
// 默认值与 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §16.3 表一致。
const (
	EconBoomThreshold     int64 = 100000 // ≥ 此值 → Boom
	EconCautionThreshold  int64 = 50000  // [CautionThreshold, BoomThreshold) → Health
	EconDangerThreshold   int64 = 10000  // [DangerThreshold, CautionThreshold) → Caution
	EconCriticalThreshold int64 = 5000   // [CriticalThreshold, DangerThreshold) → Danger；< 此值 → Critical
)

// EconTierSpec 档位的销毁/回滚比例（v5 5 档）。
// 三个值由 PropEngine 直接读，避免在热路径上做映射。
type EconTierSpec struct {
	SystemAbsorbPct int // 系统销毁百分比
	PotReturnPct    int // 彩池回滚百分比
	// TargetCompensPct 目标补偿百分比始终 = 100 - AbsorbPct - PotReturnPct
	// 由 PropEngine 计算 targetCompens = price - potReturn - systemAbsorb 兜底
	// 余数（避免整数除法丢精度）。
}

// EconTierSpecs 是档位 → 比例的查找表（v5 5 档）。
// 与配置 LsmAgentGame.conf 中 werewolf.econ_tier_* 字段同步。
var EconTierSpecs = map[EconTier]EconTierSpec{
	EconBoom:     {SystemAbsorbPct: 20, PotReturnPct: 60},
	EconHealth:   {SystemAbsorbPct: 30, PotReturnPct: 50},
	EconCaution:  {SystemAbsorbPct: 40, PotReturnPct: 40},
	EconDanger:   {SystemAbsorbPct: 45, PotReturnPct: 30},
	EconCritical: {SystemAbsorbPct: 60, PotReturnPct: 20},
}

// EconTierThresholds 当前生效的阈值（可由 ConfigureEconTier 覆盖）。
// 默认值与常量一致;运维可通过 LsmAgentGame.conf 调整。
var EconTierThresholds = struct {
	Boom     int64
	Caution  int64
	Danger   int64
	Critical int64
}{
	Boom:     EconBoomThreshold,
	Caution:  EconCautionThreshold,
	Danger:   EconDangerThreshold,
	Critical: EconCriticalThreshold,
}

// ConfigureEconTier 注入自定义阈值(由 config.LsmAgentGame.conf 加载)。
// 四个值必须单调：Boom > Caution > Danger > Critical。
// 非法输入 → panic(启动期配置错误应当致命)。
func ConfigureEconTier(boom, caution, danger, critical int64) {
	if !(boom > caution && caution > danger && danger > critical && critical >= 0) {
		panic(fmt.Sprintf("ConfigureEconTier: 阈值必须单调 Boom>Caution>Danger>Critical>=0; got boom=%d caution=%d danger=%d critical=%d",
			boom, caution, danger, critical))
	}
	EconTierThresholds.Boom = boom
	EconTierThresholds.Caution = caution
	EconTierThresholds.Danger = danger
	EconTierThresholds.Critical = critical
}

// ComputeEconTier 根据房间总金币存量判定档位（v5 5 档）。
// roomTotalCoin: 房间内所有存活玩家的钱包余额之和（Human + Bot）。
// 返回档位枚举；负数或 0 输入 → EconCritical（最严档防刷道具）。
func ComputeEconTier(roomTotalCoin int64) EconTier {
	if roomTotalCoin < 0 {
		roomTotalCoin = 0
	}
	switch {
	case roomTotalCoin >= EconTierThresholds.Boom:
		return EconBoom
	case roomTotalCoin >= EconTierThresholds.Caution:
		return EconHealth
	case roomTotalCoin >= EconTierThresholds.Danger:
		return EconCaution
	case roomTotalCoin >= EconTierThresholds.Critical:
		return EconDanger
	default:
		return EconCritical
	}
}

// GetEconTierSpec 返回指定档位的比例配置（v5）。
// 未知档位回退到 EconHealth（最宽松，等价 v3 默认）。
func GetEconTierSpec(tier EconTier) EconTierSpec {
	if spec, ok := EconTierSpecs[tier]; ok {
		return spec
	}
	return EconTierSpecs[EconHealth]
}

// EconTierDisplayName 返回档位的本地化显示名（中文 + 徽章 + 英文 key）。
// 供 PropUserPromptBlock 末尾的【当前经济档位】段渲染。
func EconTierDisplayName(tier EconTier) string {
	switch tier {
	case EconBoom:
		return "🟣 Boom（暴富）"
	case EconHealth:
		return "🟢 Health（健康）"
	case EconCaution:
		return "🟡 Caution（警戒）"
	case EconDanger:
		return "🟠 Danger（危险）"
	case EconCritical:
		return "🔴 Critical（危急）"
	}
	return "❓ Unknown"
}

// EconTierAbsorbPct 是 PropEngine 用的快速路径 helper（v5）。
// 未知档位 → 30%（v3 默认值），避免热路径 panic。
func EconTierAbsorbPct(tier EconTier) int {
	return GetEconTierSpec(tier).SystemAbsorbPct
}

// EconTierPotPct 是 PropEngine 用的快速路径 helper（v5）。
func EconTierPotPct(tier EconTier) int {
	return GetEconTierSpec(tier).PotReturnPct
}

// RoomTotalCoin 公共 helper：返回房间总金币存量(api 层使用)。
// 委托 r.roomTotalCoin()(持锁私有方法)。调用方不需要自己持锁——
// 本函数内部直接调私有方法,因此要求 r.mu 已持锁。
//
// 若 r 为 nil 或 walletSvc 不可用,返回 0 → EconCritical(最严档)。
func RoomTotalCoin(r *WerewolfRoom) int64 {
	if r == nil {
		return 0
	}
	return r.roomTotalCoin()
}