// Package virtual_city — scenario_catalog_batch84_env_security_life.go: 批次84
// L2 精选场景条目 · 环境生态 + 公共安全 + 民生日常 + 科技与新文明(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/02-全维度事件体系对账表.md §9、§10、§11、§12。
//
// 纪律:同 batch84_market_policy.go(只新增 / 零 rand / 幅度量级对齐既有条目)。
package virtual_city

// scenarioDefsBatch84EnvSecurityLife 批次84 L2 新增:环境生态 14 条 +
// 公共安全 14 条 + 民生日常 12 条 + 科技与新文明 12 条(对账表 §9–§12)。
var scenarioDefsBatch84EnvSecurityLife = []scenarioDef{
	// ── 环境生态(+14)──
	{
		spec: ScenarioSpec{
			Kind: "invasive_species", Name: "物种入侵", DefaultMagnitude: 3,
			Group:       ScenarioGroupEnvironment,
			Description: "外来物种挤压本地生态链:农业与渔业产出受损,治理与生态修复投入增加,食品供给与价格受扰。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.015,
			GoodsPriceShockPct:  0.012,
			RevenueShockPct:     -0.03,
			TreasuryMonthlyCNY:  90000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "urban_wildlife", Name: "野生动物进城", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "生态廊道断裂把野生动物引入建成区:人兽冲突事件增加,绿化养护与驱护成本上升,居民投诉集中。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 50000,
			ElectionBias:       -3,
			RevenueShockPct:    -0.008,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "vegetation_degradation", Name: "植被退化", DefaultMagnitude: 3,
			Group:       ScenarioGroupEnvironment,
			Description: "水土流失与土壤贫瘠导致植被退化:荒漠化边缘扩张、扬尘增加,农业产出与空气质量同步受损。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.018,
			GoodsPriceShockPct:  0.015,
			RevenueShockPct:     -0.028,
			TreasuryMonthlyCNY:  100000,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "river_pollution", Name: "河水污染", DefaultMagnitude: 4,
			Group:       ScenarioGroupEnvironment,
			Description: "工业与生活污水超标入河:沿河取水受限、渔业与旅游受损,治理工程与替代供水支出沉重。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.018,
			RevenueShockPct:     -0.045,
			TreasurySpendCNY:    700000,
			ElectionBias:        -6,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "waste_sorting_rollout", Name: "垃圾分类推行", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "强制分类覆盖全城:短期投放与转运成本上升、居民适应成本显现,回收再生行业产能扩张。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 70000,
			RevenueShockPct:    -0.01,
			GoodsPriceShockPct: 0.004,
			ElectionBias:       -2,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "park_construction", Name: "公园建设", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "城市公园与绿道成网建设:周边房价与租金抬升、休闲消费扩容,财政建设与养护支出同步增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   500000,
			TreasuryMonthlyCNY: 90000,
			HouseShockPct:      0.012,
			RevenueShockPct:    0.015,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "carbon_neutral_policy", Name: "碳中和政策", DefaultMagnitude: 3,
			Group:       ScenarioGroupEnvironment,
			Description: "碳排放约束落地:高耗能行业产能受限、能源成本上行,清洁产业链受益,增长与就业结构深度调整。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.02,
			RevenueShockPct:     -0.035,
			TreasurySpendCNY:    600000,
			TreasuryMonthlyCNY:  150000,
			UnemploymentMonthly: 0.002,
			ElectionBias:        -3,
		},
		defaultDuration: 24,
	},
	{
		spec: ScenarioSpec{
			Kind: "noise_pollution", Name: "噪音污染", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "施工与交通噪音超标:居民休息与工作受扰,居住品质下降、周边租金承压,隔声治理支出增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:      -0.008,
			ElectionBias:       -4,
			TreasuryMonthlyCNY:  50000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "light_pollution", Name: "光污染", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "夜间照明过量干扰生物节律与居民休息:城市能耗与光治理支出增加,居住区品质评价下滑。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 40000,
			ElectionBias:       -3,
			HouseShockPct:      -0.005,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "soil_contamination", Name: "土壤污染", DefaultMagnitude: 3,
			Group:       ScenarioGroupEnvironment,
			Description: "工业遗留与危废渗漏造成土壤污染:农用地受限、修复工程漫长,地产开发价值与财政收入下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.012,
			HouseShockPct:      -0.02,
			RevenueShockPct:     -0.035,
			TreasuryMonthlyCNY:  150000,
			ElectionBias:        -5,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "wetland_restoration", Name: "湿地修复", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "退化湿地系统修复:水质净化与生物多样性恢复,生态服务外溢改善周边环境,但土地机会成本上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   600000,
			TreasuryMonthlyCNY: 100000,
			HouseShockPct:      -0.006,
			ElectionBias:       5,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "migratory_bird_conflict", Name: "候鸟迁徙冲突", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "迁徙路线与城区重叠:农业与机场渔业受损、扑杀与驱鸟管理成本上升,生态与生产矛盾显性化。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.008,
			TreasuryMonthlyCNY:  60000,
			ElectionBias:        -2,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "air_quality_policy", Name: "空气质量管控", DefaultMagnitude: 3,
			Group:       ScenarioGroupEnvironment,
			Description: "高排放行业限产与移动源管控:短期工业产出承压,长期能源结构改善,居民健康支出下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:     -0.03,
			GoodsPriceShockPct:  0.008,
			TreasuryMonthlyCNY:  90000,
			UnemploymentMonthly: 0.0015,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "green_building_mandate", Name: "绿色建筑强制标准", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "新建建筑强制绿色标准:建造成本与工期上升、存量改造需求扩容,中长期能效与运维成本下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY:  60000,
			RevenueShockPct:     0.012,
			HouseShockPct:       0.008,
			GoodsPriceShockPct:  0.006,
		},
		defaultDuration: 18,
	},
	// ── 公共安全(+14)──
	{
		spec: ScenarioSpec{
			Kind: "theft_cases", Name: "盗窃案件", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "入室与扒窃案件高发:商户与居民现金损失上升,安防投入与保费上涨,夜间商圈客流受抑。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:    0.004,
			RevenueShockPct:    -0.02,
			TreasuryMonthlyCNY: 60000,
			ElectionBias:       -4,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "fraud_surge", Name: "诈骗高发", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "电信与网络诈骗集中爆发:居民财富转移与消费信心受损,反诈投入加大,信贷与保险风控趋严。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:    0.006,
			CreditWindowDelta: -0.06,
			ElectionBias:       -5,
			TreasuryMonthlyCNY: 70000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "street_brawl", Name: "斗殴事件", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "街头斗殴与治安事件频发:公共空间吸引力下降、周边商户受损,治安与调解支出上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.018,
			ElectionBias:       -5,
			TreasuryMonthlyCNY: 60000,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "fire_accident", Name: "火灾事故", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "商业与住宅火灾频发:直接财产损失与停产停业并存,消防整改与保险赔付支出集中抬升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.01,
			GoodsSupplyShockPct: -0.015,
			RevenueShockPct:    -0.03,
			TreasurySpendCNY:   400000,
			ElectionBias:       -5,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "counter_terrorism_drill", Name: "反恐应急", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "涉恐线索触发高等级安保:重点场所临时管控、公众活动受限,安保支出激增而社会信心受扰。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY:  120000,
			RevenueShockPct:     -0.025,
			ElectionBias:        -7,
			UnemploymentMonthly: 0.0015,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "crowd_stampede_risk", Name: "人群踩踏", DefaultMagnitude: 4,
			Group:       ScenarioGroupSecurity,
			Description: "大型活动人群失控:伤亡与场馆停业并存,客流管控改造与人流管理支出大幅增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:    0.008,
			RevenueShockPct:    -0.04,
			TreasurySpendCNY:   500000,
			ElectionBias:       -9,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "missing_person_search", Name: "走失寻人", DefaultMagnitude: 2,
			Group:       ScenarioGroupSecurity,
			Description: "走失与失踪案件引发全城寻人:公共场所人力与安保被占用,家庭情绪与社区安全感受扰。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 40000,
			ElectionBias:       -4,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "cross_border_crime", Name: "跨境犯罪", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "跨境洗钱与走私网络被端:相关行业短暂停摆、监管趋严,资产价格与跨境投资信心承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:      -0.02,
			CreditWindowDelta:  -0.08,
			RevenueShockPct:    -0.025,
			ElectionBias:       -5,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "data_leak", Name: "数据泄露", DefaultMagnitude: 4,
			Group:       ScenarioGroupSecurity,
			Description: "大规模个人信息与商业数据泄露:监管处罚与整改支出骤增,用户信任流失,金融与平台业务被迫收缩。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   800000,
			RevenueShockPct:    -0.05,
			CreditWindowDelta:  -0.08,
			ElectionBias:       -7,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "rumor_spread", Name: "谣言传播", DefaultMagnitude: 3,
			Group:       ScenarioGroupSecurity,
			Description: "未经核实的谣言在社交平台扩散:抢购与抛售放大价格波动,澄清与辟谣投入加大,公共沟通成本攀升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.015,
			StockShockPct:      -0.02,
			GoldShockPct:       0.012,
			TreasuryMonthlyCNY: 50000,
			ElectionBias:       -5,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "gas_leak_evacuation", Name: "燃气泄漏疏散", DefaultMagnitude: 4,
			Group:       ScenarioGroupSecurity,
			Description: "燃气泄漏触发大规模疏散:片区生产生活停摆,商户损失与安置支出叠加,管网安全整改压力陡增。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:    0.01,
			RevenueShockPct:    -0.045,
			TreasurySpendCNY:   500000,
			UnemploymentMonthly: 0.002,
			ElectionBias:       -7,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "cyber_attack_spillover", Name: "网络攻击外溢", DefaultMagnitude: 4,
			Group:       ScenarioGroupSecurity,
			Description: "关键系统遭攻击并外溢至民生与金融:支付与公共服务中断,企业停摆,应急恢复与合规投入巨大。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:      -0.05,
			RevenueShockPct:    -0.07,
			CreditWindowDelta:  -0.12,
			TreasurySpendCNY:   900000,
			ElectionBias:       -9,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "critical_infra_guard", Name: "关键基础设施保卫", DefaultMagnitude: 2,
			Group:       ScenarioGroupSecurity,
			Description: "关键设施安保标准升级:运营成本长期抬升,部分高耗能作业时段受限,投资回报周期拉长。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 90000,
			RevenueShockPct:    -0.015,
			ElectionBias:       -2,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "safety_training_campaign", Name: "安全整治行动", DefaultMagnitude: 2,
			Group:       ScenarioGroupSecurity,
			Description: "全市安全专项整治:违规产线关停整改,短期产能与就业承压,事故率下降带来长期保险成本改善。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.025,
			UnemploymentAdd:    0.003,
			TreasuryMonthlyCNY: 70000,
			ElectionBias:       4,
		},
		defaultDuration: 9,
	},
	// ── 民生日常(+12)──
	{
		spec: ScenarioSpec{
			Kind: "utility_outage_notice", Name: "停水停电通知", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "计划检修与故障导致片区停水停电:生活与小型商户经营中断,备用电源与桶装水支出转嫁居民。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.008,
			RevenueShockPct:    -0.015,
			ElectionBias:       -4,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "property_dispute", Name: "物业纠纷", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "物业费与业委会矛盾激化:小区公共空间停摆、业主委员会瘫痪,社区服务与商业配套品质下滑。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:       -5,
			RevenueShockPct:    -0.01,
			TreasuryMonthlyCNY: 40000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "neighbor_conflict", Name: "邻里矛盾", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "楼上漏水与噪音等邻里纠纷频发:调解资源紧张、居住体验下降,社区治理成本被动上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:       -3,
			HouseShockPct:      -0.005,
			TreasuryMonthlyCNY: 30000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "price_complaint_wave", Name: "物价投诉", DefaultMagnitude: 3,
			Group:       ScenarioGroupDailyLife,
			Description: "民生品价格异常引发集中投诉与举报:监管调查启动,相关商户营收与合规成本上升,消费情绪转弱。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsMonthlyDrift: 0.003,
			RevenueShockPct:    -0.018,
			ElectionBias:       -6,
			TreasuryMonthlyCNY: 50000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "delivery_timeout", Name: "外卖超时", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "运力紧张导致外卖大面积超时:餐饮商户曝光与补偿成本上升,平台与骑手收入结构变化。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.015,
			UnemploymentAdd:    0.002,
			WageShockPct:       -0.03,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "parcel_loss", Name: "快递丢失", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "包裹丢件与错投频发:消费者补偿诉求上升,末端网点操作规范整改,电商信任度与复购率受压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.012,
			ElectionBias:       -4,
			TreasuryMonthlyCNY: 30000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "renovation_noise", Name: "装修噪音", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "违规装修与夜间施工扰民:物业管理成本上升、投诉激增,邻里关系紧张影响居住满意度。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:       -4,
			TreasuryMonthlyCNY: 30000,
			RevenueShockPct:    -0.008,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "pet_nuisance", Name: "宠物扰民", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "宠物扰民与弃养增加:社区管理成本上行、投诉集中,无证养殖链条的医疗与防疫负担浮现。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:       -3,
			TreasuryMonthlyCNY: 40000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "square_dance_clash", Name: "广场舞矛盾", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "公共空间使用冲突升级:跳广场舞与周边居民作息矛盾激化,场地供给不足,社区治理与满意度双向承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:       -4,
			TreasuryMonthlyCNY: 40000,
			RevenueShockPct:    -0.006,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "street_vendor_occupation", Name: "摆摊占道", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "流动摊位占用道路引发交通与卫生双争议:执法与安置成本上升,沿街商户客流被分流。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 50000,
			RevenueShockPct:    -0.012,
			ElectionBias:       -4,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "property_fee_hike", Name: "物业涨价", DefaultMagnitude: 2,
			Group:       ScenarioGroupDailyLife,
			Description: "物业费调涨超出居民承受预期:居住成本抬升、房屋需求受抑,物业与业主关系趋于紧张。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:      -0.008,
			ElectionBias:       -5,
			RevenueShockPct:    -0.006,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "owner_rights_action", Name: "业主维权", DefaultMagnitude: 3,
			Group:       ScenarioGroupDailyLife,
			Description: "业主集体维权停工罢市:楼盘交付与商业运营停滞,施工与建材需求骤降,行政调解压力上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.015,
			RevenueShockPct:     -0.03,
			UnemploymentAdd:     0.004,
			ElectionBias:        -7,
			TreasuryMonthlyCNY:  60000,
		},
		defaultDuration: 9,
	},
	// ── 科技与新文明(+12)──
	{
		spec: ScenarioSpec{
			Kind: "startup_boom", Name: "创业公司兴起", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "创业生态活跃度骤升:新设企业与岗位快速涌现,办公与租赁需求上行,风险偏好抬升带动股指。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			UnemploymentAdd:  -0.004,
			StockDriftMonthly: 0.003,
			RevenueShockPct:  0.04,
			HouseShockPct:    0.01,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "patent_dispute", Name: "技术专利纠纷", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "核心专利诉讼与禁令交锋:相关企业产品下架、研发支出冻结,行业竞争格局被迫重排。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:  -0.05,
			StockShockPct:    -0.025,
			UnemploymentAdd:   0.003,
			TreasurySpendCNY:  250000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "digital_transformation", Name: "数字化转型", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "企业与公共服务数字化改造提速:效率提升与短期投入并存,软件与算力需求扩张,传统岗位被替代。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:   0.05,
			WageShockPct:      0.04,
			UnemploymentAdd:   -0.002,
			TreasurySpendCNY:  400000,
			StockDriftMonthly: 0.002,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "smart_city_landing", Name: "智慧城市场景落地", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "城市级感知与调度平台落地:公共效率提升、交通与安全支出下降,但数据合规与隐私成本上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY:  150000,
			RevenueShockPct:     0.035,
			UnemploymentAdd:     -0.002,
			GoodsPriceShockPct:  -0.004,
			ElectionBias:        3,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "network_upgrade", Name: "网络升级", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "新一代通信网络建设与切换:网络投资前置、设备需求上行,服务中断期影响数字经济产值。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   600000,
			RevenueShockPct:    0.04,
			GoodsPriceShockPct: -0.006,
			UnemploymentAdd:    -0.002,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "biotech_breakthrough", Name: "生物科技突破", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "合成生物或基因编辑取得工程化突破:医药与农业产能效率跃升,既有行业格局与监管框架被重写。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: 0.02,
			RevenueShockPct:     0.05,
			StockDriftMonthly:   0.003,
			TreasurySpendCNY:    500000,
			ElectionBias:        3,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "autonomous_driving_pilot", Name: "自动驾驶试点", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "自动驾驶扩大试点范围:出行与货运成本下降、驾培与保险业态承压,道路事故率统计口径同步变化。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:  -0.02,
			UnemploymentAdd:  -0.002,
			WageShockPct:     0.03,
			GoodsPriceShockPct: -0.004,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "open_source_boom", Name: "开源生态繁荣", DefaultMagnitude: 2,
			Group:       ScenarioGroupTech,
			Description: "开源社区与企业协作加速:软件成本下降、创新效率提升,传统闭源软件授权收入被侵蚀。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:  -0.015,
			StockDriftMonthly: 0.002,
			UnemploymentAdd:  -0.002,
			GoodsPriceShockPct: -0.006,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "cloud_computing_expansion", Name: "算力云化扩张", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "算力向云端集中:中小企业数字化门槛下降,本地机房与硬件零售萎缩,电力与能耗需求上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:   0.04,
			GoodsPriceShockPct: 0.008,
			UnemploymentAdd:   -0.002,
			TreasuryMonthlyCNY: 80000,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "space_debris_cleanup", Name: "太空碎片清理", DefaultMagnitude: 2,
			Group:       ScenarioGroupTech,
			Description: "轨道碎片清理与防护升级:卫星运营成本上升,发射窗口受限,航天产业链重新定价。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.015,
			TreasurySpendCNY:   300000,
			UnemploymentAdd:    -0.001,
			StockDriftMonthly:  0.001,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "quantum_error_correction", Name: "量子纠错突破", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "量子纠错取得工程突破:算力瓶颈松动,部分行业算力成本长期下行,既有的算力租赁格局被重估。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockDriftMonthly:   0.003,
			RevenueShockPct:     0.04,
			GoodsPriceShockPct:  -0.008,
			TreasurySpendCNY:    400000,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "ai_safety_incident", Name: "AI 安全事故", DefaultMagnitude: 4,
			Group:       ScenarioGroupTech,
			Description: "智能系统失控或被滥用造成事故:行业停摆自查、监管收紧,算力需求阶段性冻结,信心受挫。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:      -0.06,
			RevenueShockPct:    -0.06,
			CreditWindowDelta:  -0.1,
			TreasurySpendCNY:   700000,
			ElectionBias:       -8,
		},
		defaultDuration: 9,
	},
}
