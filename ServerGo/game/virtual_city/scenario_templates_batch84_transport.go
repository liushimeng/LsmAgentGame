// Package virtual_city — scenario_templates_batch84_transport.go: 批次84 场景
// 模板 · 交通物流(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.2 模板清单 #6–#7。
//
// 模板清单(契约 §4.1.2):
//
//	#6 transport_disruption 方式(10) × 诱因(4) × 波及(3) = 120 变体
//	#7 logistics_shock      环节(8) × 冲击源(3) × 行业(3) =  72 变体
//
// 纪律:纯数据;派生逻辑见 scenario_templates.go(确定性、零 RNG)。
package virtual_city

// scenarioTemplatesBatch84Transport 批次84 模板 · 交通物流 2 个。
var scenarioTemplatesBatch84Transport = []scenarioTemplate{
	{
		Spec: ScenarioSpec{
			Kind: "transport_disruption", Name: "交通系统中断", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "某类交通方式大面积中断:通勤与货运同时受阻,到货周期拉长推高物流成本,沿线商业客流与就业受冲击。",
		},
		Profile: scenarioProfile{
			GoodsPriceShockPct: 0.010,
			GoodsSupplyShockPct: -0.012,
			RevenueShockPct:     -0.030,
			UnemploymentMonthly: 0.0015,
		},
		Duration: 4,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "方式",
				Suffix: "mode",
				Values: []scenarioTemplateValue{
					{Key: "metro", Label: "地铁", Scale: 1.3,
						Adjust: scenarioProfile{UnemploymentAdd: 0.004, GoodsPriceShockPct: 0.008, ElectionBias: -3}},
					{Key: "bus", Label: "公交车", Scale: 1.1,
						Adjust: scenarioProfile{UnemploymentAdd: 0.003, TreasuryMonthlyCNY: 60000}},
					{Key: "urban_rail", Label: "市域铁路", Scale: 1.2,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.010, RevenueShockPct: -0.025}},
					{Key: "expressway", Label: "高速公路", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, TreasuryMonthlyCNY: 90000}},
					{Key: "urban_air", Label: "城市航空", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, RevenueShockPct: -0.030}},
					{Key: "port", Label: "港口", Scale: 1.4,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.020, RevenueShockPct: -0.045, ElectionBias: -4}},
					{Key: "airport", Label: "机场", Scale: 1.3,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.016, RevenueShockPct: -0.040, ElectionBias: -4}},
					{Key: "ride_hailing", Label: "网约车", Scale: 0.8,
						Adjust: scenarioProfile{WageShockPct: -0.040, UnemploymentAdd: 0.002}},
					{Key: "bike_share", Label: "共享单车", Scale: 0.5,
						Adjust: scenarioProfile{}},
					{Key: "inland_waterway", Label: "内河航道", Scale: 1.0,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.012, TreasuryMonthlyCNY: 70000}},
				},
			},
			{
				Label:  "诱因",
				Suffix: "cause",
				Values: []scenarioTemplateValue{
					{Key: "weather", Label: "恶劣天气", Scale: 0.8,
						Adjust: scenarioProfile{ElectionBias: 2, TreasurySpendCNY: -100000}},
					{Key: "equipment", Label: "设备故障", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "accident", Label: "安全事故", Scale: 1.2,
						Adjust: scenarioProfile{TreasurySpendCNY: 250000, ElectionBias: -4}},
					{Key: "strike", Label: "罢工停运", Scale: 1.3,
						Adjust: scenarioProfile{UnemploymentAdd: 0.005, ElectionBias: -5, TreasuryMonthlyCNY: 90000}},
				},
			},
			{
				Label:  "波及",
				Suffix: "spread",
				Values: []scenarioTemplateValue{
					{Key: "line", Label: "单条线路", Scale: 0.4,
						Adjust: scenarioProfile{}},
					{Key: "network", Label: "整张网络", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "intercity", Label: "跨城干线", Scale: 1.7,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.016, RevenueShockPct: -0.030, ElectionBias: -5}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "logistics_shock", Name: "物流链条震荡", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "物流链条某一环节受冲击:全链路成本重算,到货节奏与库存周转率同步改变,相关行业被迫重排产能。",
		},
		Profile: scenarioProfile{
			GoodsPriceShockPct: 0.012,
			GoodsSupplyShockPct: -0.010,
			RevenueShockPct:     -0.028,
			UnemploymentMonthly: 0.0015,
		},
		Duration: 6,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "物流环节",
				Suffix: "link",
				Values: []scenarioTemplateValue{
					{Key: "linehaul", Label: "干线运输", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.010, TreasuryMonthlyCNY: 60000}},
					{Key: "warehousing", Label: "仓储环节", Scale: 1.0,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.008, RevenueShockPct: -0.020}},
					{Key: "sorting", Label: "分拨中心", Scale: 0.9,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.007, UnemploymentAdd: 0.001}},
					{Key: "city_delivery", Label: "城市配送", Scale: 0.8,
						Adjust: scenarioProfile{WageShockPct: -0.030, RevenueShockPct: -0.015}},
					{Key: "cold_chain", Label: "冷链运输", Scale: 1.3,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, ElectionBias: -3}},
					{Key: "cross_border", Label: "跨境物流", Scale: 1.4,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, RevenueShockPct: -0.035, ElectionBias: -4}},
					{Key: "last_mile", Label: "末端派送", Scale: 0.7,
						Adjust: scenarioProfile{UnemploymentAdd: 0.002, WageShockPct: -0.020}},
					{Key: "returns", Label: "逆向退货", Scale: 0.6,
						Adjust: scenarioProfile{GoodsPriceShockPct: -0.005, RevenueShockPct: -0.012}},
				},
			},
			{
				Label:  "冲击源",
				Suffix: "source",
				Values: []scenarioTemplateValue{
					{Key: "capacity_shortage", Label: "运力短缺", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "fuel_cost", Label: "燃油成本飙升", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, TreasuryMonthlyCNY: 50000}},
					{Key: "demand_surge", Label: "需求骤增", Scale: 0.9,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.010, UnemploymentAdd: 0.003}},
				},
			},
			{
				Label:  "行业",
				Suffix: "industry",
				Values: []scenarioTemplateValue{
					{Key: "retail", Label: "零售业", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "manufacturing", Label: "制造业", Scale: 1.2,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.014, RevenueShockPct: -0.030}},
					{Key: "agri_food", Label: "农食品", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, ElectionBias: -3}},
				},
			},
		},
	},
}
