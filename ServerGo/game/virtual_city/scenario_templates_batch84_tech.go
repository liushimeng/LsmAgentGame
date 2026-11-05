// Package virtual_city — scenario_templates_batch84_tech.go: 批次84 场景
// 模板 · 科技与新文明 + 市场与经济 + 政策与治理(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.2 模板清单 #12、#15–#17。
//
// 模板清单(契约 §4.1.2):
//
//	#12 tech_milestone      技术(14) × 产业化阶段(3)          = 42 变体
//	#15 market_shock        品种(12) × 渠道(3)               = 36 变体
//	#16 policy_action       工具(12) × 力度(3)               = 36 变体
//	#17 supply_chain_break  环节(8) × 行业(4)                = 32 变体
//
// 纪律:纯数据;派生逻辑见 scenario_templates.go(确定性、零 RNG)。
package virtual_city

// scenarioTemplatesBatch84TechMarket 批次84 模板 · 科技与新文明 1 个 +
// 市场与经济 2 个 + 政策与治理 1 个。
var scenarioTemplatesBatch84TechMarket = []scenarioTemplate{
	{
		Spec: ScenarioSpec{
			Kind: "tech_milestone", Name: "技术里程碑", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "关键技术取得突破:先经历研发投入与不确定性,再经产业化扩散改变成本结构与产业分工,长期产出与就业结构重排。",
		},
		Profile: scenarioProfile{
			RevenueShockPct:   0.030,
			WageShockPct:      0.030,
			UnemploymentAdd:   -0.002,
			TreasurySpendCNY:  400000,
			StockDriftMonthly: 0.002,
		},
		Duration: 18,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "技术",
				Suffix: "tech",
				Values: []scenarioTemplateValue{
					{Key: "solid_state_battery", Label: "固态电池", Scale: 1.1,
						Adjust: scenarioProfile{RevenueShockPct: 0.030, TreasurySpendCNY: 200000}},
					{Key: "network_6g", Label: "6G 网络", Scale: 1.1,
						Adjust: scenarioProfile{TreasurySpendCNY: 350000, RevenueShockPct: 0.020}},
					{Key: "quantum_compute", Label: "量子计算", Scale: 1.0,
						Adjust: scenarioProfile{StockDriftMonthly: 0.003, TreasurySpendCNY: 300000}},
					{Key: "brain_interface", Label: "脑机接口", Scale: 1.0,
						Adjust: scenarioProfile{TreasurySpendCNY: 250000, ElectionBias: -3}},
					{Key: "longevity_therapy", Label: "长寿疗法", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, ElectionBias: -4, TreasurySpendCNY: 220000}},
					{Key: "fusion_energy", Label: "聚变能源", Scale: 1.3,
						Adjust: scenarioProfile{GoodsPriceShockPct: -0.012, TreasurySpendCNY: 400000, StockDriftMonthly: 0.003}},
					{Key: "space_elevator", Label: "空间电梯", Scale: 0.8,
						Adjust: scenarioProfile{StockDriftMonthly: 0.004, TreasurySpendCNY: 350000, ElectionBias: -3}},
					{Key: "metaverse", Label: "元宇宙", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: 0.025, ElectionBias: -4}},
					{Key: "commercial_space", Label: "商业航天", Scale: 0.9,
						Adjust: scenarioProfile{StockDriftMonthly: 0.003, TreasurySpendCNY: 250000}},
					{Key: "ai_education", Label: "AI 教育", Scale: 1.0,
						Adjust: scenarioProfile{WageShockPct: 0.030, UnemploymentAdd: -0.003, TreasurySpendCNY: 180000}},
					{Key: "bionic_robot", Label: "仿生机器人", Scale: 1.0,
						Adjust: scenarioProfile{UnemploymentAdd: -0.004, RevenueShockPct: 0.028}},
					{Key: "digital_twin", Label: "数字孪生", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: 0.022, TreasurySpendCNY: 150000}},
					{Key: "agri_drone", Label: "农业无人机", Scale: 0.9,
						Adjust: scenarioProfile{GoodsSupplyShockPct: 0.014, UnemploymentAdd: -0.002}},
					{Key: "additive_manufacturing", Label: "增材制造", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: 0.020, TreasurySpendCNY: 160000}},
				},
			},
			{
				Label:  "产业化阶段",
				Suffix: "stage",
				Values: []scenarioTemplateValue{
					{Key: "lab", Label: "实验室阶段", Scale: 0.5,
						Adjust: scenarioProfile{TreasurySpendCNY: -150000, RevenueShockPct: -0.015, ElectionBias: -2}},
					{Key: "pilot", Label: "试点阶段", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "commercial", Label: "商业化阶段", Scale: 1.7,
						Adjust: scenarioProfile{RevenueShockPct: 0.035, TreasurySpendCNY: 500000, UnemploymentAdd: -0.004, StockDriftMonthly: 0.003}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "market_shock", Name: "市场价格异动", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "特定商品或资产价格出现异动:先经库存与订单重估传导至企业营收与就业,再反向影响通胀预期与政策取向。",
		},
		Profile: scenarioProfile{
			GoodsPriceShockPct:  0.018,
			GoodsSupplyShockPct: -0.008,
			StockDriftMonthly:   0.001,
			UnemploymentMonthly: 0.001,
		},
		Duration: 6,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "品种",
				Suffix: "variety",
				Values: []scenarioTemplateValue{
					{Key: "crude_oil", Label: "原油", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, RevenueShockPct: -0.030}},
					{Key: "gold", Label: "黄金", Scale: 1.0,
						Adjust: scenarioProfile{GoldShockPct: 0.020, StockShockPct: -0.010}},
					{Key: "copper", Label: "铜", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, RevenueShockPct: -0.020}},
					{Key: "iron_ore", Label: "铁矿石", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, RevenueShockPct: -0.024}},
					{Key: "grain", Label: "粮食", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.018, GoodsSupplyShockPct: -0.012, ElectionBias: -3}},
					{Key: "cotton", Label: "棉花", Scale: 0.9,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, TreasuryMonthlyCNY: 50000}},
					{Key: "coffee", Label: "咖啡", Scale: 0.8,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, RevenueShockPct: -0.012}},
					{Key: "natural_gas", Label: "天然气", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, TreasuryMonthlyCNY: 80000}},
					{Key: "lithium", Label: "锂", Scale: 1.2,
						Adjust: scenarioProfile{StockDriftMonthly: 0.003, RevenueShockPct: -0.026}},
					{Key: "rare_earth", Label: "稀土", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.018, RevenueShockPct: -0.030, TreasuryMonthlyCNY: 90000}},
					{Key: "sugar", Label: "白糖", Scale: 0.8,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, GoodsSupplyShockPct: -0.008}},
					{Key: "pork", Label: "猪肉", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, GoodsSupplyShockPct: -0.010, ElectionBias: -3}},
				},
			},
			{
				Label:  "渠道",
				Suffix: "channel",
				Values: []scenarioTemplateValue{
					{Key: "spot", Label: "现货市场", Scale: 0.9,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.008, RevenueShockPct: -0.010}},
					{Key: "futures", Label: "期货市场", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "derivatives", Label: "金融衍生品", Scale: 1.3,
						Adjust: scenarioProfile{StockShockPct: -0.030, GoldShockPct: 0.015, CreditWindowDelta: -0.06, ElectionBias: -4}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "policy_action", Name: "政策工具落地", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "某类宏观政策工具实际落地:经利率、信贷、税收与财政支出通道传导至需求与就业,力度不同导致效果与代价差异显著。",
		},
		Profile: scenarioProfile{
			PolicyRateDelta:   0.0015,
			CreditWindowDelta: -0.06,
			UnemploymentMonthly: 0.001,
			ElectionBias:      2,
		},
		Duration: 12,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "工具",
				Suffix: "instrument",
				Values: []scenarioTemplateValue{
					{Key: "rate_cut", Label: "降息", Scale: 1.0,
						Adjust: scenarioProfile{PolicyRateDelta: -0.0020, CreditWindowDelta: 0.10, UnemploymentAdd: -0.003}},
					{Key: "rate_hike", Label: "加息", Scale: 1.0,
						Adjust: scenarioProfile{PolicyRateDelta: 0.0020, CreditWindowDelta: -0.10, UnemploymentAdd: 0.003}},
					{Key: "rrr_cut", Label: "降准", Scale: 0.9,
						Adjust: scenarioProfile{CreditWindowDelta: 0.12, UnemploymentAdd: -0.002}},
					{Key: "tax_cut", Label: "减税", Scale: 1.0,
						Adjust: scenarioProfile{PolicyRateDelta: -0.0008, RevenueShockPct: 0.030, ElectionBias: 4}},
					{Key: "tax_hike", Label: "加税", Scale: 1.0,
						Adjust: scenarioProfile{PolicyRateDelta: 0.0010, RevenueShockPct: -0.030, ElectionBias: -4}},
					{Key: "fiscal_stimulus", Label: "财政刺激", Scale: 1.2,
						Adjust: scenarioProfile{TreasurySpendCNY: 900000, UnemploymentAdd: -0.004, GoodsPriceShockPct: 0.010}},
					{Key: "austerity", Label: "财政紧缩", Scale: 1.2,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: -150000, UnemploymentAdd: 0.004, ElectionBias: -5}},
					{Key: "price_cap", Label: "价格管制", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: -0.020, RevenueShockPct: -0.020, ElectionBias: -3}},
					{Key: "subsidy_expand", Label: "补贴扩围", Scale: 1.0,
						Adjust: scenarioProfile{TreasurySpendCNY: 500000, GoodsPriceShockPct: 0.012, ElectionBias: 4}},
					{Key: "credit_support", Label: "信贷支持", Scale: 0.9,
						Adjust: scenarioProfile{CreditWindowDelta: 0.14, UnemploymentAdd: -0.002}},
					{Key: "reserve_requirement", Label: "准备金调整", Scale: 0.8,
						Adjust: scenarioProfile{PolicyRateDelta: 0.0010, CreditWindowDelta: -0.08}},
					{Key: "fx_peg", Label: "汇率盯住", Scale: 0.8,
						Adjust: scenarioProfile{CreditWindowDelta: -0.06, TreasurySpendCNY: 300000}},
				},
			},
			{
				Label:  "力度",
				Suffix: "strength",
				Values: []scenarioTemplateValue{
					{Key: "mild", Label: "温和", Scale: 0.5,
						Adjust: scenarioProfile{PolicyRateDelta: -0.0010, TreasurySpendCNY: -250000}},
					{Key: "standard", Label: "常规", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "aggressive", Label: "强力", Scale: 1.7,
						Adjust: scenarioProfile{PolicyRateDelta: 0.0015, TreasurySpendCNY: 700000, UnemploymentAdd: 0.003, ElectionBias: -3}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "supply_chain_break", Name: "产业链断裂", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "产业链关键环节中断:上游缺料向下游逐级传导,产能利用率与就业先于价格反应,替代供给到位前成本持续抬升。",
		},
		Profile: scenarioProfile{
			GoodsSupplyShockPct: -0.020,
			GoodsPriceShockPct:  0.014,
			RevenueShockPct:     -0.035,
			UnemploymentMonthly: 0.002,
			TreasuryMonthlyCNY:  80000,
		},
		Duration: 9,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "环节",
				Suffix: "link",
				Values: []scenarioTemplateValue{
					{Key: "chip", Label: "芯片", Scale: 1.3,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.016, RevenueShockPct: -0.030, ElectionBias: -3}},
					{Key: "raw_material", Label: "原材料", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, TreasuryMonthlyCNY: 80000}},
					{Key: "key_component", Label: "关键零部件", Scale: 1.2,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.014, UnemploymentMonthly: 0.002}},
					{Key: "energy", Label: "能源", Scale: 1.3,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.020, RevenueShockPct: -0.035, TreasuryMonthlyCNY: 120000}},
					{Key: "chemical", Label: "化工", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, RevenueShockPct: -0.026}},
					{Key: "logistics", Label: "物流", Scale: 1.1,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, TreasuryMonthlyCNY: 70000}},
					{Key: "packaging_test", Label: "封装测试", Scale: 1.0,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.012, RevenueShockPct: -0.020}},
					{Key: "assembly", Label: "整机组装", Scale: 1.1,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.010, UnemploymentAdd: 0.004, RevenueShockPct: -0.028}},
				},
			},
			{
				Label:  "行业",
				Suffix: "industry",
				Values: []scenarioTemplateValue{
					{Key: "electronics", Label: "消费电子", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.014, RevenueShockPct: -0.032}},
					{Key: "automotive", Label: "汽车制造", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.016, UnemploymentAdd: 0.004}},
					{Key: "medical_device", Label: "医疗器械", Scale: 1.0,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 100000, ElectionBias: -3}},
					{Key: "appliance", Label: "家用电器", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, RevenueShockPct: -0.024}},
				},
			},
		},
	},
}
