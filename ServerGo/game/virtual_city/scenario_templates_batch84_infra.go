// Package virtual_city — scenario_templates_batch84_infra.go: 批次84 场景
// 模板 · 基础设施(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.2 模板清单 #4–#5。
//
// 模板清单(契约 §4.1.2):
//
//	#4 infrastructure_failure 设施(14) × 影响面(3)              = 42 变体
//	#5 utility_outage        公用事业(6) × 停摆范围(3) × 诱因(3) = 54 变体
//
// 纪律:纯数据;派生逻辑见 scenario_templates.go(确定性、零 RNG)。
package virtual_city

// scenarioTemplatesBatch84Infra 批次84 模板 · 基础设施 2 个。
var scenarioTemplatesBatch84Infra = []scenarioTemplate{
	{
		Spec: ScenarioSpec{
			Kind: "infrastructure_failure", Name: "基础设施失效", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "某类基础设施出现批量故障:对应生命线供给中断,企业停产与居民生活受扰,抢修与替代方案支出骤增。",
		},
		Profile: scenarioProfile{
			GoodsSupplyShockPct: -0.015,
			GoodsPriceShockPct:  0.010,
			RevenueShockPct:     -0.035,
			UnemploymentMonthly: 0.002,
			TreasurySpendCNY:    350000,
		},
		Duration: 6,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "设施",
				Suffix: "facility",
				Values: []scenarioTemplateValue{
					{Key: "water_mains", Label: "供水管道", Scale: 1.3,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.012, TreasurySpendCNY: 400000, ElectionBias: -4}},
					{Key: "gas_pipeline", Label: "燃气管道", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, TreasurySpendCNY: 350000}},
					{Key: "power_substation", Label: "变电站", Scale: 1.4,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.018, RevenueShockPct: -0.040, TreasurySpendCNY: 500000}},
					{Key: "telecom_hub", Label: "通信枢纽", Scale: 1.1,
						Adjust: scenarioProfile{StockDriftMonthly: -0.002, RevenueShockPct: -0.030}},
					{Key: "bridge", Label: "桥梁", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.010, TreasurySpendCNY: 450000, UnemploymentMonthly: 0.002}},
					{Key: "tunnel", Label: "隧道", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.008, TreasurySpendCNY: 300000}},
					{Key: "storm_drain", Label: "排水管网", Scale: 1.1,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.010, TreasuryMonthlyCNY: 80000, ElectionBias: -3}},
					{Key: "sewage_plant", Label: "污水处理厂", Scale: 1.0,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 90000, ElectionBias: -4}},
					{Key: "waste_plant", Label: "垃圾处理厂", Scale: 0.8,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 60000}},
					{Key: "traffic_signal", Label: "交通信号灯", Scale: 0.7,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.006, UnemploymentMonthly: 0.001}},
					{Key: "water_plant", Label: "自来水厂", Scale: 1.3,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.016, TreasurySpendCNY: 450000, ElectionBias: -5}},
					{Key: "substation_battery", Label: "储能电站", Scale: 0.9,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.009, RevenueShockPct: -0.025}},
					{Key: "pump_station", Label: "泵站", Scale: 0.9,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.009, TreasurySpendCNY: 250000}},
					{Key: "cable_tray", Label: "综合管廊", Scale: 0.8,
						Adjust: scenarioProfile{TreasurySpendCNY: 280000, UnemploymentMonthly: 0.001}},
				},
			},
			{
				Label:  "影响面",
				Suffix: "scope",
				Values: []scenarioTemplateValue{
					{Key: "single_site", Label: "单点失效", Scale: 0.4,
						Adjust: scenarioProfile{TreasurySpendCNY: -150000, ElectionBias: 2}},
					{Key: "district", Label: "片区影响", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "citywide", Label: "全网失效", Scale: 1.8,
						Adjust: scenarioProfile{TreasurySpendCNY: 900000, UnemploymentAdd: 0.006, ElectionBias: -7}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "utility_outage", Name: "公用事业停摆", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "供能/供水/供热/通信等公用事业在特定范围停摆:生产与生活同时受阻,企业自购发电与替代供给成本骤升。",
		},
		Profile: scenarioProfile{
			GoodsSupplyShockPct: -0.018,
			GoodsPriceShockPct:  0.014,
			RevenueShockPct:     -0.040,
			UnemploymentMonthly: 0.002,
			TreasurySpendCNY:    300000,
		},
		Duration: 4,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "公用事业",
				Suffix: "utility",
				Values: []scenarioTemplateValue{
					{Key: "electricity", Label: "供电", Scale: 1.4,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.020, RevenueShockPct: -0.050, TreasurySpendCNY: 400000}},
					{Key: "water", Label: "供水", Scale: 1.2,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.016, TreasurySpendCNY: 350000, ElectionBias: -4}},
					{Key: "gas", Label: "燃气", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, RevenueShockPct: -0.035}},
					{Key: "heating", Label: "供热", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.018, TreasuryMonthlyCNY: 90000, ElectionBias: -3}},
					{Key: "telecom", Label: "通信", Scale: 0.9,
						Adjust: scenarioProfile{StockDriftMonthly: -0.002, RevenueShockPct: -0.030}},
					{Key: "waste_collection", Label: "垃圾收运", Scale: 0.6,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 50000, ElectionBias: -2}},
				},
			},
			{
				Label:  "停摆范围",
				Suffix: "scope",
				Values: []scenarioTemplateValue{
					{Key: "neighborhood", Label: "单个社区", Scale: 0.5,
						Adjust: scenarioProfile{TreasurySpendCNY: -150000, ElectionBias: 2}},
					{Key: "district", Label: "整个城区", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "whole_city", Label: "全城停摆", Scale: 1.9,
						Adjust: scenarioProfile{TreasurySpendCNY: 900000, UnemploymentAdd: 0.007, ElectionBias: -8}},
				},
			},
			{
				Label:  "诱因",
				Suffix: "trigger",
				Values: []scenarioTemplateValue{
					{Key: "planned_maintenance", Label: "计划检修", Scale: 0.5,
						Adjust: scenarioProfile{TreasurySpendCNY: -150000, ElectionBias: 2}},
					{Key: "equipment_failure", Label: "设备故障", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "cyber_intrusion", Label: "网络入侵", Scale: 1.5,
						Adjust: scenarioProfile{TreasurySpendCNY: 500000, StockShockPct: -0.030, ElectionBias: -6}},
				},
			},
		},
	},
}
