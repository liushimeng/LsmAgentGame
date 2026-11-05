// Package virtual_city — scenario_catalog_batch84_transport_culture.go: 批次84
// L2 精选场景条目 · 交通物流 + 文化教育(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/02-全维度事件体系对账表.md §7、§8。
//
// 纪律:同 batch84_market_policy.go(只新增 / 零 rand / 幅度量级对齐既有条目)。
package virtual_city

// scenarioDefsBatch84TransportCulture 批次84 L2 新增:交通物流 16 条 +
// 文化教育 14 条(对账表 §7 / §8)。
var scenarioDefsBatch84TransportCulture = []scenarioDef{
	// ── 交通物流(+16)──
	{
		spec: ScenarioSpec{
			Kind: "traffic_congestion", Name: "交通拥堵", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "潮汐与路段瓶颈造成常态化拥堵:通勤时间拉长、燃料与物流成本上升,企业交付效率下降,居民满意度走低。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.012,
			RevenueShockPct:    -0.03,
			UnemploymentMonthly: 0.002,
			ElectionBias:       -4,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "traffic_accident_cluster", Name: "交通事故多发", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "连续多起重大事故导致路段封闭:通行能力骤降、事故赔偿与维修支出上升,通勤与货运双向受阻。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.025,
			GoodsPriceShockPct: 0.01,
			TreasurySpendCNY:   250000,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "logistics_delay", Name: "物流延误", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "干线运力与口岸拥堵叠加导致到货延后:零售与制造企业缺料停产,加急空运推高全社会物流成本。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.012,
			RevenueShockPct:     -0.04,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "express_depot_overflow", Name: "快递网点爆仓", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "大促后包裹量激增压垮末端网点:配送时效崩塌、临时用工激增,退换货率上升推高电商运营成本。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.025,
			UnemploymentAdd:    0.004,
			GoodsPriceShockPct: -0.008,
			ElectionBias:       -4,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "bus_route_adjustment", Name: "公交线路调整", DefaultMagnitude: 2,
			Group:       ScenarioGroupTransport,
			Description: "线网重构与班次削减:通勤可达性下降,沿线商业客流重排,部分区域就业岗位因出行成本上升而流失。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.02,
			UnemploymentMonthly: 0.0015,
			ElectionBias:       -3,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "ride_hailing_imbalance", Name: "网约车供需失衡", DefaultMagnitude: 2,
			Group:       ScenarioGroupTransport,
			Description: "牌照收紧与司机激增同时发生:局部运力过剩与局部叫不到车并存,出行成本与司机收入同时承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			WageShockPct:      -0.05,
			UnemploymentAdd:    0.003,
			GoodsPriceShockPct: 0.006,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "freight_driver_strike", Name: "货运司机罢工", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "运费分歧引发干线司机罢运:运力骤减、货物积压,商贸与制造企业被迫减产,物价与就业同步承压。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.025,
			GoodsPriceShockPct:  0.018,
			RevenueShockPct:     -0.05,
			UnemploymentMonthly: 0.003,
			TreasurySpendCNY:    300000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "port_shutdown", Name: "港口停运", DefaultMagnitude: 4,
			Group:       ScenarioGroupTransport,
			Description: "港口因事故或极端天气停运:进出口货物积压,外贸企业现金流断裂,转口空运成本暴涨推高物价。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.04,
			GoodsPriceShockPct:  0.03,
			RevenueShockPct:     -0.08,
			UnemploymentMonthly: 0.003,
			StockShockPct:      -0.03,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "flight_cancellation_wave", Name: "航班大面积取消", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "极端天气与空管限流导致航班大面积取消:商务出行与高价值货运受阻,旅游与会展收入大幅下滑。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.04,
			GoodsPriceShockPct: 0.008,
			UnemploymentMonthly: 0.002,
			ElectionBias:       -4,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "shared_bike_disorder", Name: "共享单车乱停放", DefaultMagnitude: 2,
			Group:       ScenarioGroupTransport,
			Description: "运维与调度失效导致车辆淤积与断供并存:市民出行体验与沿线商户通行效率双双受损。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.012,
			ElectionBias:       -3,
			TreasuryMonthlyCNY: 40000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "parking_saturation", Name: "停车位饱和", DefaultMagnitude: 2,
			Group:       ScenarioGroupTransport,
			Description: "核心区停车供给缺口扩大:临停与违停激增,商圈可达性下降、客流外溢,停车费与执法支出上升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.015,
			GoodsPriceShockPct: 0.006,
			TreasuryMonthlyCNY: 50000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "hazmat_transport_accident", Name: "危化品运输事故", DefaultMagnitude: 4,
			Group:       ScenarioGroupTransport,
			Description: "危险化学品运输途中泄漏:沿线区域临时管制与疏散,化工供应链中断,环境整治与赔偿支出沉重。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.012,
			GoodsSupplyShockPct: -0.03,
			HouseShockPct:      -0.02,
			RevenueShockPct:    -0.07,
			TreasurySpendCNY:   800000,
			UnemploymentMonthly: 0.002,
			ElectionBias:       -8,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "cross_border_truck_restriction", Name: "跨境货运受限", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "邻国收紧跨境货运许可:口岸排队拉长,出口制造业订单积压,转运成本推高出厂价与通胀。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			GoodsPriceShockPct:  0.015,
			RevenueShockPct:     -0.045,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "cold_chain_break", Name: "冷链断链", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "冷库与冷藏车环节失效:生鲜损耗率飙升、末端断供,食品价格与餐饮成本同步抬升,供应商违约风险上升。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.022,
			GoodsPriceShockPct:  0.02,
			RevenueShockPct:     -0.04,
			UnemploymentAdd:     0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "urban_logistics_pilot", Name: "城市配送试点收紧", DefaultMagnitude: 2,
			Group:       ScenarioGroupTransport,
			Description: "超标电动车与违停被集中整治:末端配送效率下降、成本抬升,即时零售与餐饮外卖时段供给收紧。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.02,
			GoodsPriceShockPct: 0.008,
			UnemploymentAdd:    0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "rail_freight_reroute", Name: "铁路货运绕行", DefaultMagnitude: 3,
			Group:       ScenarioGroupTransport,
			Description: "线路施工或封锁导致大宗货物长途绕行:运输里程与时间上升,钢铁与化工原料到厂成本显著抬升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.018,
			RevenueShockPct:     -0.035,
			TreasuryMonthlyCNY:  80000,
			UnemploymentMonthly: 0.0015,
		},
		defaultDuration: 6,
	},
	// ── 文化教育(+14)──
	{
		spec: ScenarioSpec{
			Kind: "school_opening_closure", Name: "学校开学放假", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "学期节奏切换带来家庭支出与通勤潮汐变化:教培、文具与餐饮业阶段性波动,假期消费短暂抬升。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.006,
			RevenueShockPct:    0.015,
			UnemploymentAdd:    -0.001,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "exam_organization", Name: "考试组织", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "大规模统一考试期间交通与考点周边管制:沿线商业短期停摆,家庭支出结构变化,复课后需求集中释放。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.012,
			GoodsPriceShockPct: 0.005,
			UnemploymentAdd:    0.001,
		},
		defaultDuration: 2,
	},
	{
		spec: ScenarioSpec{
			Kind: "campus_incident", Name: "校园事件", DefaultMagnitude: 3,
			Group:       ScenarioGroupCulture,
			Description: "校园安全事故或舆情事件:学校停课整顿、周边业态短期关停,家长与学生信心受损,调查与整改支出增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.02,
			ElectionBias:       -6,
			TreasurySpendCNY:   300000,
			UnemploymentAdd:    0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "cultural_festival", Name: "文化节举办", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "大型文化节落地:周边餐饮住宿与零售短期爆发,安保与公共服务成本上升,活动结束后客流回落。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.035,
			GoodsPriceShockPct: 0.008,
			TreasurySpendCNY:   400000,
			UnemploymentAdd:    -0.002,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "museum_expansion", Name: "博物馆开放扩容", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "公共文化场馆扩容开放:文创与非遗消费上行,文化服务就业增加,财政补贴与运营成本同步抬高。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.02,
			TreasuryMonthlyCNY: 80000,
			UnemploymentAdd:    -0.002,
			ElectionBias:       5,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "intangible_heritage", Name: "非遗传承", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "非遗技艺与匠人群体被系统扶持:特色商品附加值提升、就业结构改善,配套培训与展销投入扩大。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: 0.01,
			RevenueShockPct:    0.02,
			TreasuryMonthlyCNY: 60000,
			ElectionBias:       5,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "celebrity_visit", Name: "娱乐明星到访", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "明星到访引发城市打卡潮:酒店餐饮短期爆满、租金上冲,活动降温后客流回落、库存与人力过剩。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.03,
			GoodsPriceShockPct: 0.012,
			HouseShockPct:      0.01,
			ElectionBias:       4,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "book_publication", Name: "图书出版潮", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "出版补贴与阅读推广带动新书集中面世:文化消费温和上行,纸张与印刷产能阶段性紧张。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.008,
			RevenueShockPct:     0.015,
			TreasuryMonthlyCNY:  40000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "film_shooting", Name: "影视取景", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "剧组落地取景带来阶段性客流与器材消费,当地物价短期上行,拍摄期与休工期的人流落差明显。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.025,
			GoodsPriceShockPct: 0.01,
			TreasuryMonthlyCNY: 50000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "esports_tournament", Name: "电竞赛事举办", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "大型电竞赛事落地:酒店网吧与餐饮爆满,赞助与文创收入上行,场馆安保与临时用工成本激增。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.03,
			GoodsPriceShockPct: 0.008,
			TreasurySpendCNY:   350000,
		},
		defaultDuration: 4,
	},
	{
		spec: ScenarioSpec{
			Kind: "folk_festival", Name: "民俗活动", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "节庆与庙会等民俗活动举办:周边商业与交通阶段性繁忙,安全管理与公共服务的临时投入增加。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.02,
			GoodsPriceShockPct: 0.008,
			TreasuryMonthlyCNY: 40000,
			ElectionBias:       4,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "vocational_education_push", Name: "职业教育扩容", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "职业教育与技能培训扩容:技能溢价上行、平均工资结构改善,产教融合企业获得长期用工红利。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			WageShockPct:      0.04,
			UnemploymentAdd:    -0.003,
			TreasuryMonthlyCNY: 90000,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "cultural_heritage_boom", Name: "文博热持续", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "文博消费持续升温:文创与服务消费外溢至餐饮住宿,场馆承载力与公共服务成为新瓶颈。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.028,
			GoodsPriceShockPct: 0.01,
			TreasuryMonthlyCNY: 70000,
			UnemploymentAdd:    -0.002,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "public_reading_campaign", Name: "全民阅读推广", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "城市阅读推广活动铺开:图书与文创消费温和上行,公共文化设施使用率提高,配套经费持续投入。",
			Params:      []string{},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.012,
			TreasuryMonthlyCNY: 50000,
			ElectionBias:       3,
		},
		defaultDuration: 9,
	},
}
