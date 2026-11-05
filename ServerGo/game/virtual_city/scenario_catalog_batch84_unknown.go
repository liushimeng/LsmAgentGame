// Package virtual_city — scenario_catalog_batch84_unknown.go: 批次84 L2 精选
// 场景条目 · 未知灾难(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/02-全维度事件体系对账表.md §4。
//
// 纪律:同 batch84_market_policy.go(只新增 / 零 rand / 幅度量级对齐既有条目)。
package virtual_city

// scenarioDefsBatch84Unknown 批次84 L2 新增:未知灾难 10 条(对账表 §4,
// 含全部 🆕 标记条目 geomagnetic_reversal / asteroid_graze /
// otherworld_gate / mechanical_collapse / energy_core_meltdown /
// spacetime_rift)。
var scenarioDefsBatch84Unknown = []scenarioDef{
	{
		spec: ScenarioSpec{
			Kind: "geomagnetic_reversal", Name: "地磁反转", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "地磁极性倒转事件:电网与通信防护大面积失效,导航与物流系统紊乱,避险资产飙升而实体生产全面停摆。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.015,
			StockShockPct:       -0.07,
			GoldShockPct:        0.06,
			GoodsSupplyShockPct: -0.035,
			RevenueShockPct:     -0.08,
			TreasurySpendCNY:    1200000,
			UnemploymentMonthly: 0.005,
			ElectionBias:        -10,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "asteroid_graze", Name: "小行星掠过", DefaultMagnitude: 3,
			Group:       ScenarioGroupUnknown,
			Description: "小行星近距离掠过并引发区域破坏:碎片雨与冲击波造成局部伤亡,避险需求与科研投入同步激增。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.01,
			StockShockPct:       -0.05,
			GoldShockPct:        0.04,
			HouseShockPct:       -0.02,
			TreasurySpendCNY:    900000,
			UnemploymentMonthly: 0.003,
			ElectionBias:        -8,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "otherworld_gate", Name: "异世界通道开启", DefaultMagnitude: 5,
			Group:       ScenarioGroupUnknown,
			Description: "异世界通道在城区稳定打开:未知资源与规则涌入扰动既有产业链,风险定价与资本流动陷入极端不确定。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.02,
			StockShockPct:       -0.12,
			GoldShockPct:        0.10,
			GoodsSupplyShockPct: -0.05,
			GoodsPriceShockPct:  0.04,
			RevenueShockPct:     -0.10,
			TreasurySpendCNY:    2000000,
			UnemploymentMonthly: 0.008,
			ElectionBias:        -12,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "mechanical_collapse", Name: "机械故障大爆发", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "同批次设备系统性缺陷同时暴露:制造与运输环节大面积停摆,召回与检修支出激增,供应链按级联方式断裂。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.05,
			GoodsPriceShockPct:  0.035,
			RevenueShockPct:     -0.09,
			UnemploymentAdd:     0.009,
			TreasurySpendCNY:    1000000,
			ElectionBias:        -9,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "energy_core_meltdown", Name: "能源核心失控", DefaultMagnitude: 5,
			Group:       ScenarioGroupUnknown,
			Description: "核心设施熔毁导致能源供给体系崩溃:全面停电停水,产业停摆与撤离同时发生,长期重建需求压垮财政。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.025,
			StockShockPct:       -0.10,
			GoldShockPct:        0.08,
			GoodsSupplyShockPct: -0.07,
			GoodsPriceShockPct:  0.05,
			RevenueShockPct:     -0.12,
			TreasurySpendCNY:    3000000,
			TreasuryMonthlyCNY:  500000,
			UnemploymentMonthly: 0.010,
			ElectionBias:        -15,
		},
		defaultDuration: 24,
	},
	{
		spec: ScenarioSpec{
			Kind: "spacetime_rift", Name: "时空裂隙出现", DefaultMagnitude: 5,
			Group:       ScenarioGroupUnknown,
			Description: "时空裂隙在城郊持续存在:局部时间与距离异常扰乱物流与生产秩序,避险与管制成本激增,社会预期彻底重置。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.02,
			StockShockPct:       -0.09,
			GoldShockPct:        0.07,
			GoodsSupplyShockPct: -0.04,
			RevenueShockPct:     -0.09,
			TreasurySpendCNY:    2500000,
			UnemploymentMonthly: 0.008,
			ElectionBias:        -14,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "nanite_swarm", Name: "纳米机器蜂群失控", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "自复制纳米机器在城市管网扩散:基础设施被侵蚀性破坏,生产与医疗系统近乎停摆,清理行动本身带来巨大成本。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.018,
			GoodsSupplyShockPct: -0.06,
			GoodsPriceShockPct:  0.05,
			RevenueShockPct:     -0.11,
			TreasurySpendCNY:    2200000,
			UnemploymentMonthly: 0.009,
			ElectionBias:        -13,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "reality_distortion", Name: "现实规则失序", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "局部因果与计量规则失灵:统计与契约失去可执行性,市场报价机制停摆,货币与信用体系被迫临时接管。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:       -0.09,
			GoldShockPct:        0.06,
			RevenueShockPct:     -0.10,
			CreditWindowDelta:   -0.2,
			TreasurySpendCNY:    2000000,
			UnemploymentMonthly: 0.008,
			ElectionBias:        -12,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "gravity_anomaly", Name: "局部重力异常", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "城区出现局部重力异常区:建筑与物流体系失效,人员被迫撤离,科研与管制支出集中释放。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:      0.02,
			GoodsSupplyShockPct: -0.05,
			GoodsPriceShockPct:  0.04,
			RevenueShockPct:     -0.10,
			TreasurySpendCNY:    1800000,
			UnemploymentMonthly: 0.007,
			ElectionBias:        -11,
		},
		defaultDuration: 15,
	},
	{
		spec: ScenarioSpec{
			Kind: "alien_artifact", Name: "外星遗物出土", DefaultMagnitude: 3,
			Group:       ScenarioGroupUnknown,
			Description: "疑似外星遗物被发掘:科研与产业投资涌入,资本与人才集聚带动局部繁荣,同时引发社会恐慌与管制收紧。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockDriftMonthly:  0.004,
			GoldShockPct:       0.03,
			RevenueShockPct:    0.06,
			TreasurySpendCNY:   600000,
			UnemploymentAdd:    -0.003,
			ElectionBias:       -4,
		},
		defaultDuration: 12,
	},
}
