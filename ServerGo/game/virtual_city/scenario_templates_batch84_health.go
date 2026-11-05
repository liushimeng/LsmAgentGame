// Package virtual_city — scenario_templates_batch84_health.go: 批次84 场景
// 模板 · 公共卫生(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.2 模板清单 #1–#3。
//
// 模板清单(契约 §4.1.2):
//
//	#1 pandemic           病原体(12) × 传播途径(4) × 严重度(3) = 144 变体
//	#2 infectious_outbreak 病种(10) × 严重度(3)               =  30 变体
//	#3 healthcare_capacity 医疗资源(8) × 压力源(3) × 波及(3)  =  72 变体
//
// 纪律:纯数据(基准 Spec + 基准 profile + 轴取值表);派生逻辑见
// scenario_templates.go(确定性、零 RNG);**不改任何既有状态字段**。
package virtual_city

// scenarioTemplatesBatch84Health 批次84 模板 · 公共卫生 3 个。
var scenarioTemplatesBatch84Health = []scenarioTemplate{
	{
		Spec: ScenarioSpec{
			Kind: "pandemic", Name: "传染病大流行", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "新发传染病在人群间扩散:先冲击出行与聚集性消费,再经用工中断传导至供给与就业,财政被动进入公共卫生应急状态。",
		},
		Profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.015,
			UnemploymentAdd:     0.006,
			UnemploymentMonthly: 0.003,
			TreasurySpendCNY:    600000,
			ElectionBias:        -5,
		},
		Duration: 9,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "病原体",
				Suffix: "pathogen",
				Values: []scenarioTemplateValue{
					{Key: "dengue", Label: "登革热", Scale: 0.6,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.006, UnemploymentAdd: 0.002}},
					{Key: "influenza", Label: "流感", Scale: 1.0,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.014, UnemploymentAdd: 0.005, TreasurySpendCNY: 150000}},
					{Key: "measles", Label: "麻疹", Scale: 0.8,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.008, UnemploymentMonthly: 0.002, ElectionBias: -2}},
					{Key: "cholera", Label: "霍乱", Scale: 1.4,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.024, GoodsPriceShockPct: 0.01, TreasurySpendCNY: 400000}},
					{Key: "tuberculosis", Label: "肺结核", Scale: 0.7,
						Adjust: scenarioProfile{UnemploymentMonthly: 0.004, TreasuryMonthlyCNY: 60000, ElectionBias: -2}},
					{Key: "hfmd", Label: "手足口病", Scale: 0.5,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.005, UnemploymentAdd: 0.001}},
					{Key: "norovirus", Label: "诺如病毒", Scale: 0.6,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.010, GoodsPriceShockPct: 0.005}},
					{Key: "ebola", Label: "埃博拉", Scale: 1.8,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.030, TreasurySpendCNY: 900000, UnemploymentAdd: 0.010, ElectionBias: -8}},
					{Key: "mpox", Label: "猴痘", Scale: 0.9,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.009, TreasurySpendCNY: 250000, ElectionBias: -3}},
					{Key: "malaria", Label: "疟疾", Scale: 0.7,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.011, TreasuryMonthlyCNY: 70000}},
					{Key: "chickenpox", Label: "水痘", Scale: 0.6,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.007, UnemploymentAdd: 0.003}},
					{Key: "pertussis", Label: "百日咳", Scale: 0.7,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.008, UnemploymentMonthly: 0.002}},
				},
			},
			{
				Label:  "传播途径",
				Suffix: "route",
				Values: []scenarioTemplateValue{
					{Key: "airborne", Label: "空气传播", Scale: 1.0,
						Adjust: scenarioProfile{UnemploymentAdd: 0.004, GoodsSupplyShockPct: -0.010, ElectionBias: -3}},
					{Key: "waterborne", Label: "水源传播", Scale: 0.9,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.010, GoodsSupplyShockPct: -0.008, TreasurySpendCNY: 300000}},
					{Key: "foodborne", Label: "食物传播", Scale: 0.8,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, RevenueShockPct: -0.030, GoodsSupplyShockPct: -0.006}},
					{Key: "contact", Label: "密切接触", Scale: 1.1,
						Adjust: scenarioProfile{UnemploymentAdd: 0.006, TreasurySpendCNY: 400000, ElectionBias: -4}},
				},
			},
			{
				Label:  "严重度",
				Suffix: "severity",
				Values: []scenarioTemplateValue{
					{Key: "mild", Label: "轻症", Scale: 0.5,
						Adjust: scenarioProfile{TreasurySpendCNY: -200000, ElectionBias: 3}},
					{Key: "moderate", Label: "中度", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "severe", Label: "重症", Scale: 1.8,
						Adjust: scenarioProfile{TreasurySpendCNY: 800000, UnemploymentAdd: 0.005, ElectionBias: -6}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "infectious_outbreak", Name: "传染病局部暴发", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "特定场所或人群出现聚集性感染:局部停业与隔离先于全城扩散,相关行业订单与就业短期承压,医疗资源迅速吃紧。",
		},
		Profile: scenarioProfile{
			GoodsSupplyShockPct: -0.012,
			UnemploymentAdd:     0.004,
			TreasurySpendCNY:    300000,
			ElectionBias:        -3,
		},
		Duration: 6,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "病种",
				Suffix: "disease",
				Values: []scenarioTemplateValue{
					{Key: "salmonella", Label: "沙门氏菌", Scale: 0.7,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.008, RevenueShockPct: -0.020}},
					{Key: "staphylococcus", Label: "金黄色葡萄球菌", Scale: 0.6,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.005, UnemploymentAdd: 0.001}},
					{Key: "hepatitis_a", Label: "甲型肝炎", Scale: 0.8,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.010, TreasurySpendCNY: 200000}},
					{Key: "norovirus_outbreak", Label: "诺如聚集", Scale: 0.7,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.008, RevenueShockPct: -0.025}},
					{Key: "legionella", Label: "军团菌", Scale: 0.9,
						Adjust: scenarioProfile{TreasurySpendCNY: 350000, TreasuryMonthlyCNY: 60000}},
					{Key: "botulism", Label: "肉毒中毒", Scale: 0.8,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.006, ElectionBias: -3}},
					{Key: "scabies", Label: "疥疮", Scale: 0.5,
						Adjust: scenarioProfile{UnemploymentAdd: 0.001}},
					{Key: "ringworm", Label: "体癣", Scale: 0.4,
						Adjust: scenarioProfile{}},
					{Key: "conjunctivitis", Label: "流行性结膜炎", Scale: 0.6,
						Adjust: scenarioProfile{UnemploymentMonthly: 0.001, ElectionBias: -1}},
					{Key: "foodborne_parasite", Label: "食源性寄生虫", Scale: 0.7,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.007, TreasuryMonthlyCNY: 50000}},
				},
			},
			{
				Label:  "严重度",
				Suffix: "severity",
				Values: []scenarioTemplateValue{
					{Key: "cluster", Label: "小规模聚集", Scale: 0.5,
						Adjust: scenarioProfile{TreasurySpendCNY: -150000, ElectionBias: 2}},
					{Key: "sector", Label: "行业级暴发", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "regional", Label: "区域性暴发", Scale: 1.7,
						Adjust: scenarioProfile{TreasurySpendCNY: 600000, UnemploymentAdd: 0.006, ElectionBias: -5}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "healthcare_capacity", Name: "医疗资源承载力不足", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "特定医疗资源(床位/设备/药品)相对需求出现缺口:候诊与住院排期拉长,居民自付支出上升,相关科室产能效率下降。",
		},
		Profile: scenarioProfile{
			TreasuryMonthlyCNY:  80000,
			GoodsPriceShockPct: 0.006,
			UnemploymentMonthly: 0.0015,
			ElectionBias:        -3,
		},
		Duration: 9,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "医疗资源",
				Suffix: "resource",
				Values: []scenarioTemplateValue{
					{Key: "icu_bed", Label: "重症监护床位", Scale: 1.4,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 150000, ElectionBias: -4}},
					{Key: "ventilator", Label: "呼吸机", Scale: 1.3,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 120000, GoodsPriceShockPct: 0.008}},
					{Key: "dialysis", Label: "透析设备", Scale: 1.1,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 90000, UnemploymentMonthly: 0.002}},
					{Key: "isolation_ward", Label: "隔离病房", Scale: 1.2,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 110000, ElectionBias: -3}},
					{Key: "imaging", Label: "影像诊断设备", Scale: 0.9,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 70000}},
					{Key: "reagent", Label: "检测试剂", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.010, TreasuryMonthlyCNY: 50000}},
					{Key: "essential_drug", Label: "基本药物", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, TreasuryMonthlyCNY: 80000, ElectionBias: -3}},
					{Key: "blood_product", Label: "血液制品", Scale: 0.8,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 60000, UnemploymentMonthly: 0.001}},
				},
			},
			{
				Label:  "压力源",
				Suffix: "stressor",
				Values: []scenarioTemplateValue{
					{Key: "seasonal_flu", Label: "季节性流感", Scale: 0.8,
						Adjust: scenarioProfile{UnemploymentMonthly: 0.001}},
					{Key: "heat_wave", Label: "高温热浪", Scale: 0.9,
						Adjust: scenarioProfile{ElectionBias: -3, TreasuryMonthlyCNY: 40000}},
					{Key: "mass_casualty", Label: "群体伤亡事件", Scale: 1.6,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 200000, UnemploymentAdd: 0.005, ElectionBias: -6}},
				},
			},
			{
				Label:  "波及范围",
				Suffix: "spread",
				Values: []scenarioTemplateValue{
					{Key: "single_dept", Label: "单一科室", Scale: 0.4,
						Adjust: scenarioProfile{}},
					{Key: "single_hospital", Label: "单家医院", Scale: 0.8,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 40000}},
					{Key: "citywide", Label: "全城医疗系统", Scale: 1.5,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 180000, ElectionBias: -5, UnemploymentMonthly: 0.002}},
				},
			},
		},
	},
}
