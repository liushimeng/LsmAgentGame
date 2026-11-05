// Package virtual_city — scenario_catalog_batch84_society_nature.go: 批次84
// L2 精选场景条目 · 社会与舆情 + 自然灾害(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/02-全维度事件体系对账表.md §2、§3。
//
// 纪律:同 batch84_market_policy.go(只新增 / 零 rand / 幅度量级对齐既有条目)。
package virtual_city

// scenarioDefsBatch84SocietyNature 批次84 L2 新增:社会与舆情 14 条 +
// 自然灾害 12 条(对账表 §2 / §3)。
var scenarioDefsBatch84SocietyNature = []scenarioDef{
	// ── 社会与舆情(+14)──
	{
		spec: ScenarioSpec{
			Kind: "petition_incident", Name: "信访事件", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "群体性信访与舆情发酵:相关部门暂停受理相关项目,企业营收与就业受扰,处置不当会放大不满情绪。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: 0.002,
			RevenueShockPct:    -0.03,
			ElectionBias:       -7,
			TreasuryMonthlyCNY: 60000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "community_conflict", Name: "社区矛盾激化", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "邻里与小区公共事务冲突升级:物业与商业活动停滞,相关街区零售营收下滑,调解成本转嫁给财政。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.025,
			UnemploymentAdd:    0.002,
			ElectionBias:       -5,
			TreasuryMonthlyCNY: 40000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "public_opinion_polarization", Name: "舆论两极分化", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "议题对立化撕裂共识:消费信心与投资意愿同步走弱,选举满意度偏置下移,政策落地阻力上升。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			ElectionBias:       -8,
			StockDriftMonthly:  -0.002,
			UnemploymentAdd:    0.002,
			GoodsMonthlyDrift:  0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "trust_deficit", Name: "社会信任赤字", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "机构与企业公信力普遍下滑:消费与储蓄行为转向保守,信贷需求萎缩、风险资产被系统性折价。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			StockShockPct:     -0.035,
			CreditWindowDelta: -0.08,
			ElectionBias:      -9,
			UnemploymentAdd:   0.003,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "education_competition_heat", Name: "升学竞争升温", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "教育资源供给不足推高家庭支出:教育消费与培训行业营收上行,居民实际购买力被挤压、其他消费收缩。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.02,
			RevenueShockPct:    0.04,
			UnemploymentAdd:    0.002,
			ElectionBias:       -4,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "marriage_birth_boom", Name: "婚育集中释放", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "政策窗口叠加形成婚育高峰:母婴与家居消费阶段性走强,住房需求上行,随后回落留下产能错配。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.015,
			HouseShockPct:      0.01,
			RevenueShockPct:    0.03,
			ElectionBias:       3,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "elderly_care_crisis", Name: "养老照护缺口", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "失能老人照护供给不足:家庭照料者退出劳动力市场,劳动参与率与家庭营收同时下滑,财政补贴需求急升。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentAdd:    0.005,
			WageShockPct:       -0.04,
			TreasuryMonthlyCNY: 150000,
			ElectionBias:       -6,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "social_stress_escalation", Name: "社会压力传导", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "心理服务供给缺口显性化:劳动生产率与在职意愿下滑,医疗服务支出上升,满意度偏置持续走低。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: 0.003,
			TreasuryMonthlyCNY:  120000,
			ElectionBias:        -7,
			WageShockPct:        -0.03,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "volunteer_shortage", Name: "志愿者短缺", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "公益组织人手与资金同时吃紧:社区服务供给收缩,弱势群体保障依赖财政兜底,居民满意度小幅下滑。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 80000,
			ElectionBias:       -3,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "migrant_integration", Name: "流动人口融入", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "新迁入人口在教育、医疗与住房上集中承压:短期支出上升、就业竞争加剧,长期看消费潜力被激活。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY:  100000,
			UnemploymentAdd:     0.003,
			RevenueShockPct:     0.02,
			HouseShockPct:       0.008,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "digital_divide", Name: "数字鸿沟扩大", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "老年与低技能群体被数字服务排斥:公共服务可及性下降,就业与消费渠道收窄,结构性不平等加剧。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentAdd:     0.003,
			GoodsPriceShockPct: 0.008,
			ElectionBias:        -5,
			TreasuryMonthlyCNY:  60000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "rumor_panic", Name: "谣言引发恐慌", DefaultMagnitude: 3,
			Group:       ScenarioGroupSociety,
			Description: "未经证实的消息触发抢购与避险:商品价格指数短期跳涨后回落,股指与黄金同步剧烈波动,监管澄清成本上升。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.025,
			GoodsMonthlyDrift:  0.006,
			StockShockPct:      -0.03,
			GoldShockPct:       0.02,
			ElectionBias:       -4,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "charity_controversy", Name: "慈善公信力争议", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "公益机构募捐或用途被质疑:捐赠意愿骤降,依赖捐赠的社会服务供给收缩,公众对机构信任受损。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			TreasuryMonthlyCNY: 60000,
			ElectionBias:       -5,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "subculture_boom", Name: "亚文化消费热潮", DefaultMagnitude: 2,
			Group:       ScenarioGroupSociety,
			Description: "细分亚文化带动的圈层消费激增:部分行业营收与就业上行,主流消费被分流,监管与舆情成本增加。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.01,
			RevenueShockPct:    0.03,
			UnemploymentAdd:    -0.002,
		},
		defaultDuration: 6,
	},
	// ── 自然灾害(+12)──
	{
		spec: ScenarioSpec{
			Kind: "debris_flow", Name: "泥石流", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "山体松散物裹挟下泄:沿线道路与桥涵损毁,居民现金损失与供给中断同时发生,重建支出沉重。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.015,
			GoodsSupplyShockPct: -0.025,
			HouseShockPct:      -0.02,
			TreasurySpendCNY:   700000,
			TreasuryMonthlyCNY: 150000,
			UnemploymentMonthly: 0.003,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "permafrost_melt", Name: "冻土融化", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "多年冻土持续消融:地基沉降与基础设施错位,公路铁路维护成本激增,沿线房价与供给承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.015,
			HouseShockPct:      -0.012,
			TreasuryMonthlyCNY: 120000,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "red_tide", Name: "赤潮生态灾害", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "近海藻类暴发导致水体缺氧:水产供给断崖式下跌,食品价格上行,渔业就业与沿海旅游同时受损。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.03,
			GoodsPriceShockPct:  0.025,
			RevenueShockPct:     -0.05,
			UnemploymentAdd:     0.004,
			ElectionBias:        -3,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "flash_flood", Name: "山洪暴发", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "短时强降雨引发山洪:低洼城区受淹,居民财产与库存双重受损,排水与救灾支出集中释放。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.014,
			GoodsSupplyShockPct: -0.02,
			HouseShockPct:      -0.018,
			TreasurySpendCNY:   600000,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "ice_storm", Name: "冰冻灾害", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "持续低温雨雪覆冰:管线与电力设施大面积受损,供给中断与取暖需求激增同时推高物价。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.025,
			GoodsPriceShockPct:  0.02,
			HouseShockPct:      -0.012,
			TreasurySpendCNY:   500000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "earthquake_aftershock", Name: "强震后余震序列", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "主震后余震密集:重建进度被打断、避险性消费抬头,房产与供给价格持续承压,保险与维修支出上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.008,
			HouseShockPct:      -0.01,
			GoodsSupplyShockPct: -0.012,
			TreasuryMonthlyCNY: 120000,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "landslide_cluster", Name: "滑坡群发", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "连续强降雨诱发多点滑坡:交通干线与村鎮受损,救援与重建支出集中,沿线居民被迫转移。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.01,
			GoodsSupplyShockPct: -0.018,
			TreasurySpendCNY:   500000,
			TreasuryMonthlyCNY: 100000,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "drought_agri_failure", Name: "农业绝收", DefaultMagnitude: 4,
			Group:       ScenarioGroupNature,
			Description: "长期干旱叠加灌溉失灵导致主粮绝收:食品供给断档、价格暴涨,农业就业崩溃,财政启动储备投放。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.06,
			GoodsPriceShockPct:  0.06,
			UnemploymentAdd:     0.008,
			TreasurySpendCNY:    1200000,
			TreasuryMonthlyCNY:  200000,
			ElectionBias:        -8,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "typhoon_landfall", Name: "台风正面登陆", DefaultMagnitude: 4,
			Group:       ScenarioGroupNature,
			Description: "强台风直接登陆沿海:港口停摆与厂房损毁叠加,出口订单延迟、物价短期上行,保险赔付集中。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.018,
			GoodsSupplyShockPct: -0.04,
			HouseShockPct:      -0.025,
			RevenueShockPct:    -0.08,
			TreasurySpendCNY:   1000000,
			UnemploymentMonthly: 0.003,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "wildfire_spread", Name: "山火蔓延", DefaultMagnitude: 4,
			Group:       ScenarioGroupNature,
			Description: "林火越界烧向城郊:空气与能见度恶化迫使企业降产,烟霾推高健康支出,消防与清剿支出激增。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.015,
			RevenueShockPct:     -0.05,
			TreasurySpendCNY:    700000,
			ElectionBias:        -5,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "cold_snap_extreme", Name: "极端寒潮", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "历史级寒潮南下:能源需求激增而供给受限,取暖与燃料价格跳涨,户外作业大面积停摆。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.03,
			GoodsSupplyShockPct: -0.012,
			UnemploymentMonthly: 0.003,
			TreasurySpendCNY:    400000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "hail_storm_damage", Name: "强冰雹袭击", DefaultMagnitude: 3,
			Group:       ScenarioGroupNature,
			Description: "大范围冰雹砸伤农作物与车辆:农业减产与维修支出同时发生,保险理赔推高短期消费支出。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.015,
			GoodsPriceShockPct:  0.012,
			CashDamageFrac:      0.006,
			TreasurySpendCNY:    300000,
		},
		defaultDuration: 3,
	},
}
