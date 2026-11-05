// Package virtual_city — scenario_templates_batch84_security.go: 批次84 场景
// 模板 · 公共安全 + 民生日常 + 未知灾难(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.2 模板清单 #13、#14、#18。
//
// 模板清单(契约 §4.1.2):
//
//	#13 security_incident   案件(12) × 烈度(3)              = 36 变体
//	#14 daily_life_friction 事务(12) × 波及(3)              = 36 变体
//	#18 extreme_disaster   事件(12) × 波及(3)              = 36 变体
//
// 纪律:纯数据;派生逻辑见 scenario_templates.go(确定性、零 RNG)。
package virtual_city

// scenarioTemplatesBatch84Security 批次84 模板 · 公共安全 1 个 +
// 民生日常 1 个 + 未知灾难 1 个。
var scenarioTemplatesBatch84Security = []scenarioTemplate{
	{
		Spec: ScenarioSpec{
			Kind: "security_incident", Name: "公共安全事件", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "某类公共安全案件发生:财产与人身风险上升先抑消费,再经安保投入与合规成本传导至财政与产业,秩序感直接反馈到满意度。",
		},
		Profile: scenarioProfile{
			CashDamageFrac:     0.004,
			RevenueShockPct:    -0.018,
			TreasuryMonthlyCNY: 60000,
			ElectionBias:       -3,
		},
		Duration: 6,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "案件",
				Suffix: "case",
				Values: []scenarioTemplateValue{
					{Key: "theft", Label: "入室盗窃", Scale: 1.0,
						Adjust: scenarioProfile{CashDamageFrac: 0.006, ElectionBias: -2}},
					{Key: "fraud", Label: "电信诈骗", Scale: 1.1,
						Adjust: scenarioProfile{CashDamageFrac: 0.008, CreditWindowDelta: -0.05, ElectionBias: -3}},
					{Key: "brawl", Label: "聚众斗殴", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: -0.012, ElectionBias: -3}},
					{Key: "arson", Label: "纵火", Scale: 1.3,
						Adjust: scenarioProfile{CashDamageFrac: 0.010, GoodsSupplyShockPct: -0.012, TreasurySpendCNY: 350000}},
					{Key: "chemical_leak", Label: "危化品泄漏", Scale: 1.4,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.016, TreasurySpendCNY: 500000, ElectionBias: -5}},
					{Key: "counter_terror", Label: "反恐处置", Scale: 1.2,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 120000, ElectionBias: -5}},
					{Key: "stampede", Label: "人群踩踏", Scale: 1.2,
						Adjust: scenarioProfile{TreasurySpendCNY: 300000, ElectionBias: -6}},
					{Key: "missing_person", Label: "走失寻人", Scale: 0.6,
						Adjust: scenarioProfile{ElectionBias: -2, TreasuryMonthlyCNY: 30000}},
					{Key: "data_leak_case", Label: "数据泄露", Scale: 1.1,
						Adjust: scenarioProfile{CreditWindowDelta: -0.06, TreasurySpendCNY: 300000, ElectionBias: -4}},
					{Key: "rumor_case", Label: "谣言传播", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, StockShockPct: -0.012, ElectionBias: -3}},
					{Key: "cross_border", Label: "跨境犯罪", Scale: 1.1,
						Adjust: scenarioProfile{StockShockPct: -0.015, CreditWindowDelta: -0.06, ElectionBias: -4}},
					{Key: "gang_activity", Label: "地下制毒", Scale: 1.1,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 100000, ElectionBias: -5}},
				},
			},
			{
				Label:  "烈度",
				Suffix: "severity",
				Values: []scenarioTemplateValue{
					{Key: "minor", Label: "轻微", Scale: 0.4,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: -30000, ElectionBias: 2}},
					{Key: "serious", Label: "较大", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "major", Label: "重大", Scale: 1.8,
						Adjust: scenarioProfile{TreasurySpendCNY: 600000, UnemploymentAdd: 0.004, ElectionBias: -6}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "daily_life_friction", Name: "民生琐事摩擦", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "日常生活高频摩擦事件:单次损失小但持续消耗居民时间与情绪,投诉与调解占用行政资源,长期影响城市宜居评价。",
		},
		Profile: scenarioProfile{
			ElectionBias:       -2,
			TreasuryMonthlyCNY: 30000,
		},
		Duration: 3,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "事务",
				Suffix: "matter",
				Values: []scenarioTemplateValue{
					{Key: "property_dispute", Label: "物业纠纷", Scale: 1.2,
						Adjust: scenarioProfile{ElectionBias: -3, TreasuryMonthlyCNY: 40000}},
					{Key: "neighbor_conflict", Label: "邻里矛盾", Scale: 1.0,
						Adjust: scenarioProfile{ElectionBias: -2, TreasuryMonthlyCNY: 30000}},
					{Key: "price_complaint", Label: "物价投诉", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.006, ElectionBias: -3}},
					{Key: "delivery_delay", Label: "外卖超时", Scale: 0.7,
						Adjust: scenarioProfile{WageShockPct: -0.020, ElectionBias: -1}},
					{Key: "parcel_loss", Label: "快递丢失", Scale: 0.7,
						Adjust: scenarioProfile{RevenueShockPct: -0.010, ElectionBias: -1}},
					{Key: "renovation_noise", Label: "装修噪音", Scale: 1.0,
						Adjust: scenarioProfile{ElectionBias: -3, TreasuryMonthlyCNY: 30000}},
					{Key: "pet_nuisance", Label: "宠物扰民", Scale: 0.8,
						Adjust: scenarioProfile{ElectionBias: -2, TreasuryMonthlyCNY: 25000}},
					{Key: "square_dance", Label: "广场舞矛盾", Scale: 0.8,
						Adjust: scenarioProfile{ElectionBias: -2, TreasuryMonthlyCNY: 25000}},
					{Key: "street_vendor", Label: "摆摊占道", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: -0.008, ElectionBias: -2}},
					{Key: "fee_hike", Label: "物业涨价", Scale: 1.1,
						Adjust: scenarioProfile{HouseShockPct: -0.006, ElectionBias: -3}},
					{Key: "owner_rights", Label: "业主维权", Scale: 1.2,
						Adjust: scenarioProfile{HouseShockPct: -0.010, UnemploymentAdd: 0.002, ElectionBias: -4}},
					{Key: "utility_notice", Label: "停水停电通知", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.008, ElectionBias: -3}},
				},
			},
			{
				Label:  "波及",
				Suffix: "spread",
				Values: []scenarioTemplateValue{
					{Key: "household", Label: "个别家庭", Scale: 0.3,
						Adjust: scenarioProfile{ElectionBias: 1}},
					{Key: "community", Label: "整个小区", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "district", Label: "片区级", Scale: 1.8,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 90000, ElectionBias: -4, RevenueShockPct: -0.015}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "extreme_disaster", Name: "极端灾难事件", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "未知来源的极端灾难事件:多指标同时恶化、避险情绪主导定价,财政进入应急状态,社会预期与风险偏好被整体重置。",
		},
		Profile: scenarioProfile{
			CashDamageFrac:      0.012,
			StockShockPct:       -0.060,
			GoldShockPct:        0.040,
			HouseShockPct:       -0.025,
			GoodsPriceShockPct:  0.030,
			GoodsSupplyShockPct: -0.035,
			StockDriftMonthly:   -0.003,
			UnemploymentMonthly: 0.005,
			TreasurySpendCNY:    1200000,
			TreasuryMonthlyCNY:  250000,
			ElectionBias:        -8,
		},
		Duration: 12,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "事件",
				Suffix: "event",
				Values: []scenarioTemplateValue{
					{Key: "meteor_shower", Label: "陨石雨", Scale: 1.2,
						Adjust: scenarioProfile{CashDamageFrac: 0.010, HouseShockPct: -0.020, TreasurySpendCNY: 600000}},
					{Key: "super_volcano", Label: "超级火山喷发", Scale: 1.4,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.030, GoldShockPct: 0.030, ElectionBias: -6}},
					{Key: "geomagnetic_storm", Label: "地磁风暴", Scale: 1.0,
						Adjust: scenarioProfile{StockShockPct: -0.030, TreasuryMonthlyCNY: 120000, ElectionBias: -4}},
					{Key: "alien_contact", Label: "外星文明接触", Scale: 1.3,
						Adjust: scenarioProfile{StockShockPct: -0.040, GoldShockPct: 0.040, ElectionBias: -6}},
					{Key: "time_anomaly", Label: "时空异常", Scale: 1.2,
						Adjust: scenarioProfile{StockDriftMonthly: -0.003, ElectionBias: -5}},
					{Key: "unknown_virus", Label: "未知病毒", Scale: 1.1,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.025, UnemploymentMonthly: 0.003, ElectionBias: -5}},
					{Key: "energy_crisis", Label: "能源核心失控", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.025, GoodsSupplyShockPct: -0.020, TreasurySpendCNY: 500000}},
					{Key: "mechanical_collapse", Label: "机械故障大爆发", Scale: 1.1,
						Adjust: scenarioProfile{RevenueShockPct: -0.050, UnemploymentMonthly: 0.004, ElectionBias: -5}},
					{Key: "gravity_wave", Label: "引力波扰动", Scale: 0.9,
						Adjust: scenarioProfile{StockDriftMonthly: -0.002, TreasuryMonthlyCNY: 150000}},
					{Key: "deep_sea_signal", Label: "深海信号", Scale: 1.0,
						Adjust: scenarioProfile{StockShockPct: -0.025, GoldShockPct: 0.025, ElectionBias: -4}},
					{Key: "anomalous_fog", Label: "异象迷雾", Scale: 1.0,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.020, RevenueShockPct: -0.030, ElectionBias: -5}},
					{Key: "ai_runaway", Label: "AI 失控事件", Scale: 1.2,
						Adjust: scenarioProfile{StockShockPct: -0.035, CreditWindowDelta: -0.10, ElectionBias: -6}},
				},
			},
			{
				Label:  "波及",
				Suffix: "reach",
				Values: []scenarioTemplateValue{
					{Key: "district", Label: "城区局部", Scale: 0.5,
						Adjust: scenarioProfile{TreasurySpendCNY: -400000, ElectionBias: 3}},
					{Key: "citywide", Label: "全城", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "global", Label: "全球性", Scale: 1.6,
						Adjust: scenarioProfile{TreasurySpendCNY: 1500000, GoldShockPct: 0.030, ElectionBias: -6}},
				},
			},
		},
	},
}
