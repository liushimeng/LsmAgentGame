// Package virtual_city — scenario_templates.go: 场景模板参数化引擎
// (2026-11-05 §批次84 全维度事件体系与 Agent 双向对话)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.2。
//
// 定位:批次84 把目录从「手写 Go 字面量」升级为「**基础条目 + 参数轴**」:
// 一个模板 = 基础 ScenarioSpec + 基准 profile + N 个参数轴,每个轴若干取值;
// 变体由**确定性公式**派生(基准 profile × Π轴系数 + Σ轴加性调整)。
//
// 派生公式(纯函数、确定性、**零 RNG**):
//
//	kind   = base.Kind + "__" + axis.Suffix + "_" + value.Key   (多轴用 __ 连接)
//	name   = base.Spec.Name + "·" + value.Label                  (多轴用 · 连接)
//	profile = base.Profile × (Π 各轴 value.Scale) 然后逐字段 += Σ 各轴 value.Adjust
//	magnitude = base.Spec.DefaultMagnitude
//	duration  = base.Duration(0 ⇒ scenarioDefaultDurationMonths)
//
// 上限保护(契约 §4.1.2):单模板变体数 ≤ 256;单轴取值数 ≤ 32;轴数 ≤ 4。
// 超限**截断/跳过**,绝不 panic(目录是只读数据,不能让进程崩)。
//
// 纪律:
//   - **零 rand**:派生过程不触碰任何随机源;
//   - **确定性**:同一模板派生两次结果逐字段相等(单测钉死);
//   - **只读既有状态**:派生只产生 scenarioDef(幅度表),不改任何 World 字段。
package virtual_city

import (
	"fmt"
	"strings"
)

// 模板引擎上限常量(契约 §4.1.2)。
const (
	// scenarioTemplateMaxVariants 单模板变体数上限。
	scenarioTemplateMaxVariants = 256
	// scenarioTemplateMaxAxisValues 单轴取值数上限。
	scenarioTemplateMaxAxisValues = 32
	// scenarioTemplateMaxAxes 单模板轴数上限。
	scenarioTemplateMaxAxes = 4
)

// scenarioTemplate 场景模板:基础条目 + 参数轴 ⇒ 确定性派生变体。
//
// Spec.Kind 为**模板基名**(如 "pandemic"),派生变体 kind 形如
// "pandemic__pathogen_dengue__route_airborne"。
type scenarioTemplate struct {
	// Spec 基础条目(Kind = 模板基名;Name/Group/Description/DefaultMagnitude 继承)。
	Spec ScenarioSpec
	// Profile 基准幅度(变体按轴系数缩放后再叠加轴加性调整)。
	Profile scenarioProfile
	// Duration 基准持续月数(0 ⇒ scenarioDefaultDurationMonths)。
	Duration int
	// Axes 参数轴(顺序即派生顺序:第一轴变化最慢)。
	Axes []scenarioTemplateAxis
}

// scenarioTemplateAxis 参数轴(一个轴 = 一个 kind 后缀维度)。
type scenarioTemplateAxis struct {
	// Label 轴中文名(如 "病原体");同时作为派生条目的 Params 之一。
	Label string
	// Suffix kind 后缀段(如 "pathogen")。
	Suffix string
	// Values 轴取值(顺序即派生顺序)。
	Values []scenarioTemplateValue
}

// scenarioTemplateValue 轴取值。
type scenarioTemplateValue struct {
	// Key kind 后缀(如 "dengue")。
	Key string
	// Label 显示名(如 "登革热")。
	Label string
	// Scale profile 整体缩放系数(缺省 0 ⇒ 视作 1.0)。
	Scale float64
	// Adjust 逐字段**加性**调整(可只填个别字段;在缩放**之后**叠加)。
	Adjust scenarioProfile
}

// scenarioTemplateScale 取轴取值的有效缩放系数(0 ⇒ 1.0)。
func (v scenarioTemplateValue) scenarioTemplateScale() float64 {
	if v.Scale == 0 {
		return 1.0
	}
	return v.Scale
}

// scenarioTemplateVariants 派生模板的全部变体(纯函数;确定性;零 RNG)。
//
// 保护:轴数 > 4 ⇒ 取前 4 轴;单轴取值 > 32 ⇒ 取前 32 个;
// 变体总数 > 256 ⇒ 截断(按派生顺序保留前 256 个)。
func (t scenarioTemplate) scenarioTemplateVariants() []scenarioDef {
	axes := t.Axes
	if len(axes) > scenarioTemplateMaxAxes {
		axes = axes[:scenarioTemplateMaxAxes]
	}
	// 轴取值截断(先做,避免组合爆炸)。
	trimmed := make([]scenarioTemplateAxis, 0, len(axes))
	for _, ax := range axes {
		if len(ax.Values) > scenarioTemplateMaxAxisValues {
			ax.Values = ax.Values[:scenarioTemplateMaxAxisValues]
		}
		trimmed = append(trimmed, ax)
	}
	axes = trimmed

	base := t.Spec
	if base.DefaultMagnitude == 0 {
		base.DefaultMagnitude = 3
	}
	duration := t.Duration
	if duration <= 0 {
		duration = scenarioDefaultDurationMonths
	}
	// Params = 轴名标注(模型据此知道该模板可调哪些参数)。
	params := make([]string, 0, len(axes))
	for _, ax := range axes {
		params = append(params, ax.Label)
	}

	out := make([]scenarioDef, 0, 32)
	// 递归展开(第一轴最慢):组合索引 = 各轴取值的笛卡尔积。
	var walk func(depth int, kindParts, nameParts, labelParts []string,
		scale float64, adjust scenarioProfile)
	walk = func(depth int, kindParts, nameParts, labelParts []string,
		scale float64, adjust scenarioProfile) {
		if len(out) >= scenarioTemplateMaxVariants {
			return
		}
		if depth == len(axes) {
			prof := scenarioProfileScale(t.Profile, scale)
			prof = scenarioProfileAdd(prof, adjust)
			kind := base.Kind
			name := base.Name
			if len(kindParts) > 0 {
				kind = base.Kind + "__" + strings.Join(kindParts, "__")
			}
			if len(nameParts) > 0 {
				name = base.Name + "·" + strings.Join(nameParts, "·")
			}
			desc := base.Description
			if len(labelParts) > 0 {
				desc = fmt.Sprintf("%s(变体:%s)", desc, strings.Join(labelParts, " / "))
			}
			out = append(out, scenarioDef{
				spec: ScenarioSpec{
					Kind:             kind,
					Name:             name,
					Group:            base.Group,
					Description:      desc,
					Magnitudes:       append([]int(nil), scenarioMagnitudes...),
					DefaultMagnitude: base.DefaultMagnitude,
					Params:           append([]string(nil), params...),
				},
				profile:         prof,
				defaultDuration: duration,
			})
			return
		}
		ax := axes[depth]
		for _, v := range ax.Values {
			if len(out) >= scenarioTemplateMaxVariants {
				return
			}
			walk(depth+1,
				append(append([]string(nil), kindParts...), ax.Suffix+"_"+v.Key),
				append(append([]string(nil), nameParts...), v.Label),
				append(append([]string(nil), labelParts...), ax.Label+"="+v.Label),
				scale*v.scenarioTemplateScale(),
				scenarioProfileAdd(adjust, v.Adjust))
		}
	}
	walk(0, nil, nil, nil, 1.0, scenarioProfile{})
	return out
}

// scenarioProfileScale 按系数缩放 profile 的全部数值字段。
// int64 字段按比例缩放后四舍五入(支出/营收类)。
func scenarioProfileScale(p scenarioProfile, k float64) scenarioProfile {
	if k == 1.0 {
		return p
	}
	out := p
	out.CashDamageFrac *= k
	out.StockShockPct *= k
	out.GoldShockPct *= k
	out.HouseShockPct *= k
	out.GoodsPriceShockPct *= k
	out.GoodsSupplyShockPct *= k
	out.GoodsMonthlyDrift *= k
	out.StockDriftMonthly *= k
	out.UnemploymentAdd *= k
	out.UnemploymentMonthly *= k
	out.PolicyRateDelta *= k
	out.CreditWindowDelta *= k
	out.ElectionBias *= k
	out.WageShockPct *= k
	out.RevenueShockPct *= k
	out.TreasurySpendCNY = int64(float64(out.TreasurySpendCNY)*k + 0.5)
	out.TreasuryMonthlyCNY = int64(float64(out.TreasuryMonthlyCNY)*k + 0.5)
	return out
}

// scenarioProfileAdd 逐字段加性叠加两个 profile(可只填个别字段)。
func scenarioProfileAdd(a, b scenarioProfile) scenarioProfile {
	out := a
	out.CashDamageFrac += b.CashDamageFrac
	out.StockShockPct += b.StockShockPct
	out.GoldShockPct += b.GoldShockPct
	out.HouseShockPct += b.HouseShockPct
	out.GoodsPriceShockPct += b.GoodsPriceShockPct
	out.GoodsSupplyShockPct += b.GoodsSupplyShockPct
	out.GoodsMonthlyDrift += b.GoodsMonthlyDrift
	out.StockDriftMonthly += b.StockDriftMonthly
	out.UnemploymentAdd += b.UnemploymentAdd
	out.UnemploymentMonthly += b.UnemploymentMonthly
	out.PolicyRateDelta += b.PolicyRateDelta
	out.CreditWindowDelta += b.CreditWindowDelta
	out.TreasurySpendCNY += b.TreasurySpendCNY
	out.TreasuryMonthlyCNY += b.TreasuryMonthlyCNY
	out.ElectionBias += b.ElectionBias
	out.WageShockPct += b.WageShockPct
	out.RevenueShockPct += b.RevenueShockPct
	return out
}
