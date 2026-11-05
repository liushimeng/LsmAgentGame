// Package virtual_city — scenario_catalog_batch84_health_infra.go: 批次84
// L2 精选场景条目 · 公共卫生 + 基础设施(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/02-全维度事件体系对账表.md §5、§6。
//
// 纪律:同 batch84_market_policy.go(只新增 / 零 rand / 幅度量级对齐既有条目)。
package virtual_city

// scenarioDefsBatch84HealthInfra 批次84 L2 新增:公共卫生 18 条 +
// 基础设施 16 条(对账表 §5 / §6)。
var scenarioDefsBatch84HealthInfra = []scenarioDef{
	// ── 公共卫生(+18)──
	{
		spec: ScenarioSpec{
			Kind: "food_poisoning", Name: "食品中毒事件", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "聚餐或加工环节污染导致群体食物中毒:餐饮与食品零售营收重挫,医疗支出骤增,赔偿与舆情压力同时上升。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			RevenueShockPct:     -0.06,
			GoodsPriceShockPct:  0.008,
			UnemploymentAdd:     0.004,
			TreasuryMonthlyCNY:  100000,
			ElectionBias:        -5,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "public_health_education", Name: "公共卫生科普推进", DefaultMagnitude: 2,
			Group:       ScenarioGroupPublicHealth,
			Description: "系统化健康科普与筛查铺开:预防性支出替代高额治疗支出,居民健康预期改善,满意度与医疗负担双向优化。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:  300000,
			TreasuryMonthlyCNY: 60000,
			ElectionBias:      4,
			UnemploymentAdd:   -0.002,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "occupational_disease", Name: "职业病高发", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "粉尘、化学与久坐岗位职业病集中显现:劳动年龄提前退出,企业用工成本与医保支出双升,产能受影响。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			WageShockPct:      -0.06,
			UnemploymentAdd:    0.006,
			RevenueShockPct:    -0.04,
			TreasuryMonthlyCNY: 120000,
			ElectionBias:       -6,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "drinking_water_contamination", Name: "饮用水污染", DefaultMagnitude: 4,
			Group:       ScenarioGroupPublicHealth,
			Description: "水源地被工业废水或藻类污染:供水中断与瓶装水抢购推高饮水成本,水体相关疾病增加,居民信任受损。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.025,
			GoodsPriceShockPct:  0.03,
			UnemploymentAdd:     0.003,
			TreasurySpendCNY:    600000,
			ElectionBias:        -8,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "severe_air_pollution", Name: "空气重度污染", DefaultMagnitude: 4,
			Group:       ScenarioGroupPublicHealth,
			Description: "静稳天气叠加逆温层导致重度污染:户外产能受限、呼吸道就诊激增,口罩与净化需求推高物价,企业停产损失扩大。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.018,
			RevenueShockPct:     -0.06,
			UnemploymentMonthly: 0.003,
			TreasuryMonthlyCNY:  100000,
			ElectionBias:        -7,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "medical_malpractice", Name: "医疗事故纠纷", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "诊疗事故引发纠纷与赔偿:医疗机构风险定价抬升,患者信任下降,医疗资源向头部集中,基层利用率走低。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.04,
			TreasurySpendCNY:   300000,
			ElectionBias:       -5,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "emergency_public_health_response", Name: "突发公共卫生事件响应", DefaultMagnitude: 4,
			Group:       ScenarioGroupPublicHealth,
			Description: "突发聚集性疫情触发应急响应:人员活动受限、场所停业,供给与就业骤降,财政集中投入公共卫生体系。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.03,
			GoodsPriceShockPct:  0.02,
			UnemploymentAdd:     0.01,
			RevenueShockPct:     -0.07,
			TreasurySpendCNY:    1000000,
			TreasuryMonthlyCNY:  200000,
			ElectionBias:        -8,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "hospital_overcrowding", Name: "医院超负荷运转", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "就医需求超过床位容量:候诊时间拉长与非医疗支出被挤出,医护超负荷导致服务质量与满意度下滑。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 150000,
			GoodsPriceShockPct: 0.01,
			ElectionBias:       -6,
			UnemploymentAdd:    0.002,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "vaccine_supply_crunch", Name: "疫苗供应紧张", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "关键疫苗与药品产能不足:接种与治疗延后,预防性支出转嫁给居民,相关行业产能闲置,满意度下降。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.025,
			RevenueShockPct:     -0.03,
			ElectionBias:        -5,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "maternal_health_gap", Name: "孕产妇保健缺口", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "孕产与新生儿照护资源分布不均:高危孕产妇风险上升,家庭生育与消费计划推迟,医疗支出前置。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY:  90000,
			ElectionBias:       -4,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "elderly_care_outbreak", Name: "养老机构聚集性感染", DefaultMagnitude: 4,
			Group:       ScenarioGroupPublicHealth,
			Description: "养老机构集中感染:高死亡率引发家属恐慌与入住意愿下降,机构现金流恶化,财政需兜底转移支付。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.05,
			TreasurySpendCNY:   600000,
			TreasuryMonthlyCNY: 120000,
			ElectionBias:       -7,
			UnemploymentAdd:    0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "telemedicine_gap", Name: "远程医疗缺口", DefaultMagnitude: 2,
			Group:       ScenarioGroupPublicHealth,
			Description: "优质医生供给集中而网络诊疗覆盖不足:分级诊疗失效,基层机构客流闲置,居民自付医疗支出上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.02,
			TreasuryMonthlyCNY: 70000,
			ElectionBias:       -4,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "health_data_leak", Name: "医疗数据泄露", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "患者信息大规模泄露:隐私风险抬升医疗消费意愿,机构合规成本骤增,保险与信贷风控趋严。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.03,
			CreditWindowDelta:  -0.06,
			TreasurySpendCNY:   300000,
			ElectionBias:       -5,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "antibiotic_resistance", Name: "抗生素耐药加剧", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "抗生素滥用导致耐药菌扩散:治疗周期拉长、费用上升,医疗与养殖行业产能效率同步下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY:  100000,
			RevenueShockPct:     -0.03,
			GoodsPriceShockPct:  0.012,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "maternal_health_emergency", Name: "孕产妇急救网络超载", DefaultMagnitude: 4,
			Group:       ScenarioGroupPublicHealth,
			Description: "急危重症孕产妇救治网络超载:转运与监护资源挤兑,高危孕产妇风险外溢,区域医疗负担急剧加重。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   500000,
			TreasuryMonthlyCNY: 150000,
			ElectionBias:       -7,
			UnemploymentAdd:    0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "nursing_shortage", Name: "护理人力短缺", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "一线护理人员流失与补充不足:床位无法开放,住院与门诊容量受限,患者自付比例被动上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			WageShockPct:      0.06,
			RevenueShockPct:    -0.03,
			TreasuryMonthlyCNY: 120000,
			ElectionBias:       -6,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "disease_surveillance_gap", Name: "疾病监测盲区", DefaultMagnitude: 3,
			Group:       ScenarioGroupPublicHealth,
			Description: "监测网络覆盖不足使疫情发现滞后:防控窗口后移,后续封控成本与就业损失被动放大。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 80000,
			UnemploymentAdd:    0.003,
			ElectionBias:       -4,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "mental_health_service_gap", Name: "心理服务供给缺口", DefaultMagnitude: 2,
			Group:       ScenarioGroupPublicHealth,
			Description: "心理咨询与治疗资源不足:求助者转向药物与替代疗法,医患关系紧张,劳动生产率与家庭稳定受扰。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 70000,
			UnemploymentMonthly: 0.002,
			ElectionBias:       -5,
		},
		defaultDuration: 12,
	},
	// ── 基础设施(+16)──
	{
		spec: ScenarioSpec{
			Kind: "pipe_network_aging", Name: "管网老化", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "供水管网超过设计年限:漏损率飙升、供水压力不稳,抢修与更换支出集中,供水企业现金流承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.012,
			RevenueShockPct:     -0.03,
			TreasuryMonthlyCNY:  120000,
			ElectionBias:        -4,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "renewable_station_failure", Name: "新能源电站故障", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "风光电站并网设备批量故障:清洁电力供给缺口,火电补位抬高能耗成本,电价与工业营收同步承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.018,
			GoodsPriceShockPct:  0.022,
			RevenueShockPct:     -0.05,
			StockDriftMonthly:   -0.002,
			TreasurySpendCNY:    500000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "power_grid_overload", Name: "电网承载力吃紧", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "空调与工业用电同时冲高导致有序用电:高耗能产线限产,产能利用率与工业营收下滑,家庭用电成本上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.018,
			RevenueShockPct:     -0.045,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "telecom_cable_cut", Name: "通信光缆中断", DefaultMagnitude: 2,
			Group:       ScenarioGroupInfrastructure,
			Description: "施工或外力切断骨干光缆:片区通信与数据服务降级,电商与金融交易受阻,企业营收与效率同步受损。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.03,
			TreasurySpendCNY:   200000,
			UnemploymentAdd:    0.001,
			ElectionBias:       -3,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "bridge_maintenance_gap", Name: "桥梁养护缺位", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "桥梁检测与加固长期缺位:限行导致物流绕行与运费上升,区域物流成本抬升,安全风险累积。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.008,
			RevenueShockPct:    -0.02,
			TreasuryMonthlyCNY: 90000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "sewage_overflow", Name: "污水管网溢流", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "污水管网超负荷溢流:沿街与河道污染,环境处罚与清理支出上升,公共卫生风险与居民投诉同步增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   400000,
			GoodsPriceShockPct: 0.008,
			ElectionBias:       -5,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "water_pump_failure", Name: "供水泵站故障", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "关键泵站与水厂设备故障:片区停水,企业生产与居民生活受阻,应急送水与抢修支出激增。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			RevenueShockPct:     -0.04,
			TreasurySpendCNY:    500000,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "gas_network_leak", Name: "燃气管网泄漏", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "燃气管线泄漏与爆炸风险:片区停气,餐饮与工业用能中断,安全整治要求推高合规成本。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.035,
			TreasurySpendCNY:   350000,
			ElectionBias:       -5,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "elevator_entrapment_wave", Name: "电梯困人集中", DefaultMagnitude: 2,
			Group:       ScenarioGroupInfrastructure,
			Description: "老旧电梯批量故障与维保缺位:居民出行与商业客流受阻,维保行业需求激增但监管问责压力上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.015,
			TreasuryMonthlyCNY: 60000,
			ElectionBias:       -4,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "tunnel_flooding", Name: "隧道积水封闭", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "强降雨导致下穿隧道与地下空间积水封闭:干道通行能力骤降,拥堵与绕行推高物流成本,零售客流分流。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.012,
			RevenueShockPct:    -0.03,
			TreasurySpendCNY:   300000,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "charging_station_shortage", Name: "充电桩供给不足", DefaultMagnitude: 2,
			Group:       ScenarioGroupInfrastructure,
			Description: "新能源车充电桩建设滞后于保有量增长:出行成本抬升抑制消费,设备厂商订单积压,电网投资被迫前置。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.02,
			GoodsPriceShockPct: 0.01,
			TreasuryMonthlyCNY: 80000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "data_center_outage", Name: "数据中心宕机", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "供电或制冷故障导致数据中心宕机:在线服务与数字经济产值中断,相关企业营收骤降并触发赔付。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.05,
			StockShockPct:      -0.02,
			TreasurySpendCNY:   400000,
			ElectionBias:       -4,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "reservoir_siltation", Name: "水库淤积调蓄下降", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "泥沙淤积使水库调蓄与供水能力下降:枯水期供水紧张、旱情风险上升,水利工程清淤与改造支出增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.015,
			GoodsPriceShockPct:  0.012,
			TreasuryMonthlyCNY:  100000,
			UnemploymentMonthly: 0.0015,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "road_maintenance_backlog", Name: "道路养护欠账", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "路面与桥梁养护长期欠账:事故率与故障率上升,物流与通勤效率下降,公众满意度随路况恶化走低。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.01,
			RevenueShockPct:    -0.02,
			TreasuryMonthlyCNY: 90000,
			ElectionBias:       -5,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "waste_transfer_overflow", Name: "垃圾中转站超负荷", DefaultMagnitude: 2,
			Group:       ScenarioGroupInfrastructure,
			Description: "垃圾处理设施能力落后于清运量:垃圾露天堆积引发卫生与邻里投诉,处置费与临时清运支出上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY:  80000,
			GoodsPriceShockPct:  0.006,
			ElectionBias:        -4,
			UnemploymentAdd:     0.001,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "elevated_road_safety", Name: "高架桥安全隐患", DefaultMagnitude: 3,
			Group:       ScenarioGroupInfrastructure,
			Description: "高架与快速路安全排查暴露隐患:限速限行降低通行效率,货运绕行推高物流成本,整改投资压财政。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.012,
			RevenueShockPct:    -0.025,
			TreasurySpendCNY:   450000,
			ElectionBias:       -5,
		},
		defaultDuration: 9,
	},
}
