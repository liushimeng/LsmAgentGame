// Package virtual_city — scenario_catalog_batch83.go: 批次83 场景目录扩展
// (40 → 101;2026-11-04 §批次83 系统对话窗口优化与场景目录扩充)。
//
// 契约: lag_docs/虚拟城市/已实现/83-系统对话窗口优化与场景目录扩充/01-方案设计.md §4.1。
//
// 定位:批次81 后目录为 40 个(市场与经济 11 / 政策与治理 5 / 社会与舆情 7 /
// 自然灾害 9 / 未知灾难 5 / 科技与新文明 3);本文件补齐 6 组共 101 个 ——
// 市场 +10 / 政策 +10 / 社会 +8 / 自然 +12 / 未知 +11 / 科技 +10。
//
// 纪律(与批次81 同款):
//   - **只新增,不改派发**:本文件只提供 scenarioDefsBatch83 切片;
//     applyScenarioToWorld 与 scenarioProfile 字段集零改动(契约 §4.1 零回归);
//   - **零 rand**:幅度全部 × magnitude 线性派生,不消耗 w.Rand;
//   - **数值量级对齐既有同组条目**:国库一次性支出 25 万–300 万 / 月度支出
//     10 万–60 万 / 股指冲击 ±10% / 房价冲击 ±12% / 选举偏置 ±15(钳制 ±30);
//     利率/信贷字段符号即方向(批次81 起非 policy_shock 的 policyDir 缺省 +1,
//     正 delta = 收紧;信贷窗口负 = 收紧)。
package virtual_city

// 批次83 新增场景 kind 常量(契约 §4.1 清单 61 条;既有 40 个见
// scenario.go 与 scenario_catalog_batch81.go)。集中在本文件 —— 新增 kind
// 随新增目录同文件维护,scenario.go 只动 append 链与计数注释。
const (
	// 市场与经济(+10)
	ScenarioLiquidityCrisis      = "liquidity_crisis"
	ScenarioOilPriceShock        = "oil_price_shock"
	ScenarioFoodCrisis           = "food_crisis"
	ScenarioTechBubble           = "tech_bubble"
	ScenarioHousingBoom          = "housing_boom"
	ScenarioConsumerDowngrade    = "consumer_downgrade"
	ScenarioExportBoom           = "export_boom"
	ScenarioStockMeltup          = "stock_meltup"
	ScenarioCurrencyAppreciation = "currency_appreciation"
	ScenarioBondMarketStorm      = "bond_market_storm"
	// 政策与治理(+10)
	ScenarioTaxReform             = "tax_reform"
	ScenarioPropertyTaxPilot      = "property_tax_pilot"
	ScenarioDigitalCurrency       = "digital_currency"
	ScenarioAntiCorruption        = "anti_corruption"
	ScenarioImmigrationTightening = "immigration_tightening"
	ScenarioSnapElection          = "snap_election"
	ScenarioRecallReferendum      = "recall_referendum"
	ScenarioPoliticalDeadlock     = "political_deadlock"
	ScenarioPriceControls         = "price_controls"
	ScenarioEducationReform       = "education_reform"
	// 社会与舆情(+8)
	ScenarioGeneralStrike      = "general_strike"
	ScenarioViralScandal       = "viral_scandal"
	ScenarioInfluencerEconomy  = "influencer_economy"
	ScenarioCharityScandal     = "charity_scandal"
	ScenarioMentalHealthCrisis = "mental_health_crisis"
	ScenarioTalentInflux       = "talent_influx"
	ScenarioBirthrateIncentive = "birthrate_incentive"
	ScenarioSocialTrustCrisis  = "social_trust_crisis"
	// 自然灾害(+12)
	ScenarioBlizzard           = "blizzard"
	ScenarioColdWave           = "cold_wave"
	ScenarioSandstorm          = "sandstorm"
	ScenarioLandslide          = "landslide"
	ScenarioLocustPlague       = "locust_plague"
	ScenarioHailstorm          = "hailstorm"
	ScenarioSevereThunderstorm = "severe_thunderstorm"
	ScenarioUrbanWaterlogging  = "urban_waterlogging"
	ScenarioGroundSubsidence   = "ground_subsidence"
	ScenarioSpringFlood        = "spring_flood"
	ScenarioTornado            = "tornado"
	ScenarioVectorBorneDisease = "vector_borne_disease"
	// 未知灾难(+11)
	ScenarioGridBlackout        = "grid_blackout"
	ScenarioDroneSwarmAttack    = "drone_swarm_attack"
	ScenarioUfoWave             = "ufo_wave"
	ScenarioDeepSeaSignal       = "deep_sea_signal"
	ScenarioUnknownVirusX       = "unknown_virus_x"
	ScenarioChemicalSpill       = "chemical_spill"
	ScenarioNuclearBrinkmanship = "nuclear_brinkmanship"
	ScenarioAIRunawayIncident   = "ai_runaway_incident"
	ScenarioSatelliteDebris     = "satellite_debris"
	ScenarioAnomalousFog        = "anomalous_fog"
	ScenarioTimeAnomaly         = "time_anomaly"
	// 科技与新文明(+10)
	ScenarioAGIBreakthrough         = "agi_breakthrough"
	ScenarioFusionCommercialization = "fusion_commercialization"
	ScenarioQuantumPractical        = "quantum_practical"
	ScenarioBrainMachineInterface   = "brain_machine_interface"
	ScenarioRobotLaborShift         = "robot_labor_shift"
	ScenarioLongevityTherapy        = "longevity_therapy"
	ScenarioSpaceElevator           = "space_elevator"
	ScenarioMetaverseMigration      = "metaverse_migration"
	ScenarioAlienSignalDecoded      = "alien_signal_decoded"
	ScenarioCloneEthicsCrisis       = "clone_ethics_crisis"
)

// scenarioDefsBatch83 批次83 新增 61 个场景(分组/名称/默认强度/持续月数/
// 描述 → profile 数值)。字段结构与 scenario.go 的 scenarioDef 完全一致。
//
// 顺序 = 前端/工具分组展示顺序:市场与经济 → 政策与治理 → 社会与舆情 →
// 自然灾害 → 未知灾难 → 科技与新文明(与 scenarioDefsBatch81 同款)。
var scenarioDefsBatch83 = []scenarioDef{
	// ── 市场与经济(+10)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioLiquidityCrisis, Name: "流动性危机", DefaultMagnitude: 4,
			Group:       ScenarioGroupMarket,
			Description: "金融市场流动性骤紧:信贷窗口大幅收缩、股指急跌,国库出手注入救市资金稳住金融体系。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CreditWindowDelta: -0.35,
			StockShockPct:     -0.07,
			TreasurySpendCNY:  1800000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioOilPriceShock, Name: "原油价格冲击", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "国际油价飙升推高运输与生产成本:消费品价格指数跳升后逐月续涨,股指随成本上行而承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.04,
			GoodsMonthlyDrift:  0.015,
			StockDriftMonthly:  -0.003,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioFoodCrisis, Name: "粮食安全危机", DefaultMagnitude: 4,
			Group:       ScenarioGroupMarket,
			Description: "主粮供给受损引发粮食安全危机:食品价格大幅上行、供给指数收缩,居民实际购买力与满意度同步承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.07,
			GoodsSupplyShockPct: -0.06,
			ElectionBias:        -6,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioTechBubble, Name: "科技股泡沫", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "科技板块估值持续膨胀:股指月度漂移转正,避险黄金遭抛售;泡沫后期的反身性破裂由既有市场引擎接管。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockDriftMonthly: 0.006,
			GoldShockPct:      -0.02,
		},
		defaultDuration: 8,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioHousingBoom, Name: "房地产繁荣", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "信贷宽松推动房价上行:八区房价指数走高、建材家居景气改善,股指风险偏好同步升温。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:     0.05,
			CreditWindowDelta: 0.08,
			StockDriftMonthly: 0.003,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioConsumerDowngrade, Name: "消费降级", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "居民压缩非必需消费:商品价格指数回落、失业率逐月上行,股指随企业盈利预期下修而阴跌。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  -0.015,
			UnemploymentMonthly: 0.004,
			StockDriftMonthly:   -0.003,
		},
		defaultDuration: 8,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioExportBoom, Name: "出口繁荣", DefaultMagnitude: 1,
			Group:       ScenarioGroupMarket,
			Description: "外需强劲带动出口部门扩产招工:失业率逐月下行,股指风险偏好改善,城市景气度稳步抬升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: -0.003,
			StockDriftMonthly:   0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioStockMeltup, Name: "股市狂热", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "风险偏好骤升引发股市狂热:股指一次性急涨且月度漂移持续向上,避险黄金遭资金抛售降温。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:     0.06,
			StockDriftMonthly: 0.005,
			GoldShockPct:      -0.015,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioCurrencyAppreciation, Name: "本币升值", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "本币汇率走强压低进口成本:消费品价格指数温和回落,出口部门竞争力受损、失业率小幅上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  -0.015,
			UnemploymentMonthly: 0.003,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioBondMarketStorm, Name: "债市风暴", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "债券收益率跳升引发债市风暴:政策利率被迫上行、融资成本全面抬升,信贷窗口收紧、股指重挫。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			PolicyRateDelta:   0.004,
			StockShockPct:     -0.04,
			CreditWindowDelta: -0.2,
		},
		defaultDuration: 6,
	},
	// ── 政策与治理(+10)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioTaxReform, Name: "减税改革", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "大规模减税改革落地:企业信贷窗口放松、股指风险偏好回暖,国库以月度减收为代价支撑实体经济。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CreditWindowDelta:  0.08,
			StockDriftMonthly:  0.003,
			TreasuryMonthlyCNY: 150000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioPropertyTaxPilot, Name: "房产税试点", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "房产税试点压制多套持有:八区房价指数中枢下移、多品类物价温和回落,选民满意度因持有成本上升而承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:      -0.06,
			ElectionBias:       -5,
			GoodsPriceShockPct: -0.01,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioDigitalCurrency, Name: "数字货币推行", DefaultMagnitude: 1,
			Group:       ScenarioGroupPolicy,
			Description: "央行数字货币全面推行:支付结算效率提升带动供给链运转更顺,信贷窗口随交易透明化而微松。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CreditWindowDelta:   0.05,
			GoodsSupplyShockPct: 0.01,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioAntiCorruption, Name: "反腐风暴", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "系统性反腐风暴提升政府公信力:选民满意度明显改善,商业信贷短期转入观望、窗口被动收紧。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:      4,
			CreditWindowDelta: -0.1,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioImmigrationTightening, Name: "移民政策收紧", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "移民政策收紧使劳动力流入放缓:部分行业供给受制、用工成本上行,失业率随岗位出清缓慢下行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			UnemploymentMonthly: -0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSnapElection, Name: "提前选举", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "提前大选带来政策不确定性:选民满意度偏置下行,市场观望情绪升温、股指月度漂移转负。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:      -4,
			StockDriftMonthly: -0.002,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioRecallReferendum, Name: "罢免公投", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "罢免公投叠加街头集会:治理效能受质疑、选民满意度明显下挫,集会扰动推高局部商品价格。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:       -6,
			GoodsPriceShockPct: 0.01,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioPoliticalDeadlock, Name: "政治僵局", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "议会僵局拖慢财政拨付:股指漂移与选民满意度双降,国库以月度额外维持开支兜底基础公共服务。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockDriftMonthly:  -0.002,
			ElectionBias:       -4,
			TreasuryMonthlyCNY: 100000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioPriceControls, Name: "价格管制", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "行政限价全面压低名义物价:商品价格指数明显回落、满意度短期改善,代价是供给收缩与黑市滋生。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  -0.04,
			GoodsSupplyShockPct: -0.03,
			ElectionBias:        3,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioEducationReform, Name: "教育改革", DefaultMagnitude: 1,
			Group:       ScenarioGroupPolicy,
			Description: "教育支出结构调整:家庭养育负担下降、教育类物价回落,选民对治理体系的满意度温和改善。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: -0.005,
			ElectionBias:       3,
		},
		defaultDuration: 9,
	},
	// ── 社会与舆情(+8)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioGeneralStrike, Name: "总罢工", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "关键行业总罢工:供给链大面积停摆、失业率即时跳升,资本市场同步受挫,经济活动短期休克。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.04,
			UnemploymentAdd:     0.004,
			StockShockPct:       -0.03,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioViralScandal, Name: "舆论风暴", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "突发舆情全网刷屏:消费情绪快速回落、商品价格指数走低,政府公信力与选民满意度同步受损。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: -0.01,
			ElectionBias:       -3,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioInfluencerEconomy, Name: "网红经济崛起", DefaultMagnitude: 1,
			Group:       ScenarioGroupSociety,
			Description: "直播电商爆发式增长:带货消费带动商品价格温和上行,相关板块获资金青睐、股指漂移改善。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.01,
			StockDriftMonthly:  0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioCharityScandal, Name: "慈善信任危机", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "头部公益机构丑闻冲击社会信任:捐赠与消费同步回落,商品价格指数走低、选民满意度受损。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:       -3,
			GoodsPriceShockPct: -0.008,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioMentalHealthCrisis, Name: "心理健康危机", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "职场过劳与心理问题蔓延:劳动参与率缓慢下滑、失业率逐月上行,公共服务压力拉低满意度。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: 0.003,
			ElectionBias:        -3,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioTalentInflux, Name: "高端人才流入", DefaultMagnitude: 1,
			Group:       ScenarioGroupSociety,
			Description: "产业政策吸引高端人才净流入:创新板块景气度改善、失业率缓慢下行,股指风险偏好同步回暖。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: -0.003,
			StockDriftMonthly:   0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioBirthrateIncentive, Name: "出生率激励见效", DefaultMagnitude: 1,
			Group:       ScenarioGroupSociety,
			Description: "育儿补贴与托育扩容推动出生率回升:母婴类消费价格温和上行,国库以月度补贴支出托底人口长期预期。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.008,
			TreasuryMonthlyCNY: 100000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSocialTrustCrisis, Name: "社会信任危机", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "连环失信事件压低全社会交易意愿:消费与物价指数双降、选民满意度下挫,避险黄金受资金追捧。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoldShockPct:       0.04,
			GoodsPriceShockPct: -0.02,
			ElectionBias:       -6,
		},
		defaultDuration: 6,
	},
	// ── 自然灾害(+12)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioBlizzard, Name: "暴雪寒潮", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "极端暴雪阻断物流干线:鲜食价格跳升、供给指数明显收缩,市政除雪救灾开支大幅增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.04,
			GoodsPriceShockPct:  0.03,
			TreasurySpendCNY:    400000,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioColdWave, Name: "极端寒潮", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "持续极端低温推高能源需求:供暖与电力价格即时上行并逐月续涨,户外作业效率明显下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.02,
			GoodsMonthlyDrift:  0.008,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSandstorm, Name: "沙尘暴", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "强沙尘侵袭迫使户外停工停运:供给指数收缩、呼吸类医疗消费价格上行,城市运转短期降速。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.02,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioLandslide, Name: "山体滑坡", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "山体滑坡损毁道路与部分房屋:八区房价指数承压,国库拨付应急抢险与基础设施重建资金。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:    -0.02,
			TreasurySpendCNY: 300000,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioLocustPlague, Name: "蝗灾", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "蝗群过境毁损大片农田:粮食供给持续收缩、食品价格即时跳升并逐月续涨,主粮安全承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.05,
			GoodsPriceShockPct:  0.05,
			GoodsMonthlyDrift:   0.02,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioHailstorm, Name: "冰雹灾害", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "强冰雹砸损车辆作物与屋顶:农业供给收缩、八区房价指数小幅下修,保险理赔压力上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			HouseShockPct:       -0.015,
		},
		defaultDuration: 2,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSevereThunderstorm, Name: "强对流雷暴", DefaultMagnitude: 1,
			Group:       ScenarioGroupNature,
			Description: "短时强对流天气扰动户外作业与即时配送:供给指数小幅收缩,城市经济运转短暂放缓后自行修复。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.01,
		},
		defaultDuration: 2,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioUrbanWaterlogging, Name: "城市内涝", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "排水系统超负荷引发内涝:低洼区商铺停业、供给收缩、房屋受损,市政排涝与修复开支上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.03,
			HouseShockPct:       -0.02,
			TreasurySpendCNY:    350000,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioGroundSubsidence, Name: "地面塌陷", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "地下水位变化引发地面沉降:涉事街区房产指数持续贬值,国库按月投入地基加固与搬迁安置支出。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:      -0.02,
			TreasuryMonthlyCNY: 100000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSpringFlood, Name: "春汛凌汛", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "融雪凌汛淹泡河滩与农田:春播进度受阻、农产品供给收缩,食品价格随供给缺口的形成温和上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.03,
			GoodsPriceShockPct:  0.02,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioTornado, Name: "龙卷风", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "龙卷风扫过城郊:厂房与仓库受损、局部供给中断、房价指数下修,国库拨付应急救灾资金。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:       -0.02,
			GoodsSupplyShockPct: -0.02,
			TreasurySpendCNY:    250000,
		},
		defaultDuration: 2,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioVectorBorneDisease, Name: "虫媒疫病", DefaultMagnitude: 2,
			Group:       ScenarioGroupNature,
			Description: "蚊媒传染病进入季节性流行期:户外消费与服务需求收缩、相关品类价格上行,劳动力市场缓慢承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.015,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 6,
	},
	// ── 未知灾难(+11)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioGridBlackout, Name: "大停电", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "区域电网连锁故障引发大停电:生产与商业大面积停摆、供给指数深度收缩,股指重挫,国库紧急抢修。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.08,
			StockShockPct:       -0.07,
			TreasurySpendCNY:    1500000,
		},
		defaultDuration: 2,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioDroneSwarmAttack, Name: "无人机蜂群袭击", DefaultMagnitude: 3,
			Group:       ScenarioGroupUnknown,
			Description: "不明无人机蜂群袭扰要害设施:资本市场避险情绪升温、股指下挫,安防与要地防护开支按月飙升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:      -0.04,
			GoldShockPct:       0.03,
			TreasuryMonthlyCNY: 150000,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioUfoWave, Name: "不明飞行物目击潮", DefaultMagnitude: 2,
			Group:       ScenarioGroupUnknown,
			Description: "连续不明飞行物目击引发围观与恐慌:避险黄金受捧、囤货推高商品价格,政府公信力受质疑。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoldShockPct:       0.03,
			GoodsPriceShockPct: 0.02,
			ElectionBias:       -3,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioDeepSeaSignal, Name: "深海异常信号", DefaultMagnitude: 2,
			Group:       ScenarioGroupUnknown,
			Description: "近海探测到规律性未知深海信号:科考与防务开支按月增加,不确定性升温使股指月度漂移转负。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 120000,
			StockDriftMonthly:  -0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioUnknownVirusX, Name: "未知病毒X", DefaultMagnitude: 4,
			Group:       ScenarioGroupUnknown,
			Description: "高传染性未知病原体出现:供给链深度收缩、失业率逐月快速上行,居民满意度大幅下挫,强于常规疫情冲击。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.08,
			UnemploymentMonthly: 0.012,
			ElectionBias:        -10,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioChemicalSpill, Name: "化学品泄漏", DefaultMagnitude: 3,
			Group:       ScenarioGroupUnknown,
			Description: "化工园区泄漏污染水源地:涉事城区供给收缩、房价指数下修,国库拨付应急处置与居民赔偿资金。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.04,
			HouseShockPct:       -0.03,
			TreasurySpendCNY:    600000,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioNuclearBrinkmanship, Name: "核冲突边缘", DefaultMagnitude: 5,
			Group:       ScenarioGroupUnknown,
			Description: "地缘核对抗白热化:市场恐慌极端定价 —— 股指深度崩跌、避险黄金飙升,物资囤积推高物价。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:      -0.12,
			GoldShockPct:       0.12,
			GoodsPriceShockPct: 0.05,
		},
		defaultDuration: 2,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioAIRunawayIncident, Name: "AI 失控事故", DefaultMagnitude: 3,
			Group:       ScenarioGroupUnknown,
			Description: "关键基础设施的 AI 系统失控致灾:股指下挫、供给链受扰,监管紧急收紧引发全社会满意度下滑。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockShockPct:       -0.05,
			GoodsSupplyShockPct: -0.03,
			ElectionBias:        -6,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSatelliteDebris, Name: "卫星碎裂雨", DefaultMagnitude: 2,
			Group:       ScenarioGroupUnknown,
			Description: "卫星解体碎片再入大气层:通信与导航服务受扰、科技板块集体重估,股指漂移转负、局部供给受制。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockDriftMonthly:   -0.002,
			GoodsSupplyShockPct: -0.02,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioAnomalousFog, Name: "异常浓雾", DefaultMagnitude: 2,
			Group:       ScenarioGroupUnknown,
			Description: "持续异常浓雾笼罩全城:物流干线中断、户外经济停摆,供给收缩叠加囤积情绪推高商品价格。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.03,
			GoodsPriceShockPct:  0.015,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioTimeAnomaly, Name: "时空异常现象", DefaultMagnitude: 3,
			Group:       ScenarioGroupUnknown,
			Description: "全城报告时空感知错乱:恐慌性消费与避险需求齐升,物价与黄金同步走高,政府公信力受挫。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.03,
			GoldShockPct:       0.04,
			ElectionBias:       -5,
		},
		defaultDuration: 3,
	},
	// ── 科技与新文明(+10)──
	{
		spec: ScenarioSpec{
			Kind: ScenarioAGIBreakthrough, Name: "通用人工智能突破", DefaultMagnitude: 2,
			Group:       ScenarioGroupTech,
			Description: "AGI 能力跃迁:生产效率长周期抬升、供给改善、股指漂移向上,岗位结构剧变使失业率同步上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockDriftMonthly:   0.004,
			UnemploymentMonthly: 0.004,
			GoodsSupplyShockPct: 0.02,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioFusionCommercialization, Name: "可控核聚变商用", DefaultMagnitude: 2,
			Group:       ScenarioGroupTech,
			Description: "首座聚变商用机组并网:能源成本长周期下行带动物价回落,科技板块重估上行,国库承担先期投入。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: -0.01,
			StockDriftMonthly:  0.003,
			TreasurySpendCNY:   1000000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioQuantumPractical, Name: "量子计算实用化", DefaultMagnitude: 1,
			Group:       ScenarioGroupTech,
			Description: "量子优越性走向商用加密与制药落地:科技板块获资金重估、股指漂移向上,避险黄金吸引力下降。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			StockDriftMonthly: 0.002,
			GoldShockPct:      -0.01,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioBrainMachineInterface, Name: "脑机接口普及", DefaultMagnitude: 1,
			Group:       ScenarioGroupTech,
			Description: "消费级脑机接口放量:医疗与电子消费价格温和上行,相关科技板块景气改善、股指漂移转正。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.01,
			StockDriftMonthly:  0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioRobotLaborShift, Name: "机器人劳动力替代", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "通用机器人规模化上岗:生产率抬升带动供给扩张、股指上行,被替代岗位使失业率逐月快速攀升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: 0.008,
			GoodsSupplyShockPct: 0.03,
			StockDriftMonthly:   0.003,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioLongevityTherapy, Name: "长生临床突破", DefaultMagnitude: 1,
			Group:       ScenarioGroupTech,
			Description: "抗衰疗法临床取得突破性成功:医疗健康类消费价格上行,居民对未来预期改善、治理满意度温和提升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.01,
			ElectionBias:       2,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioSpaceElevator, Name: "太空电梯开工", DefaultMagnitude: 2,
			Group:       ScenarioGroupTech,
			Description: "太空电梯超级工程开工:建材与工程板块景气上行、物价温和走高,国库进入长周期巨额投入阶段。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 300000,
			GoodsPriceShockPct: 0.01,
			StockDriftMonthly:  0.002,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioMetaverseMigration, Name: "元宇宙移民潮", DefaultMagnitude: 2,
			Group:       ScenarioGroupTech,
			Description: "工作与消费加速向虚拟世界迁移:商办地产需求萎缩、房价指数承压,虚拟消费与科技板块上行。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			HouseShockPct:      -0.03,
			GoodsPriceShockPct: 0.01,
			StockDriftMonthly:  0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioAlienSignalDecoded, Name: "未知文明信号解码", DefaultMagnitude: 3,
			Group:       ScenarioGroupTech,
			Description: "深空信号被证实为非自然来源:黄金避险买盘激增、股指受挫,范式冲击拉低满意度,国库转入地外科研投入。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoldShockPct:       0.05,
			StockShockPct:      -0.06,
			ElectionBias:       -6,
			TreasuryMonthlyCNY: 200000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: ScenarioCloneEthicsCrisis, Name: "克隆人伦理危机", DefaultMagnitude: 2,
			Group:       ScenarioGroupTech,
			Description: "克隆人临床灰产曝光:伦理争议席卷舆论、选民满意度受损,医疗与生物板块遭抛售、股指阴跌。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			ElectionBias:      -3,
			StockDriftMonthly: -0.002,
		},
		defaultDuration: 6,
	},
}
