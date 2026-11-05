// Package virtual_city — scenario_catalog_batch84_market_policy.go: 批次84
// L2 精选场景条目 · 市场与经济 + 政策与治理(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/02-全维度事件体系对账表.md §1、§2。
//
// 定位:L2 = 「语义真正独立、无法由参数轴派生」的精选条目(契约 §4.1.2)。
// 本文件覆盖对账表 §1(经济与市场)与 §2(政务与社会)两条维度,含全部 🆕
// 标记条目(supply_demand_shift / counterfeit_goods / census /
// integrity_probe …),并补齐同维度的其余独立语义。
//
// 纪律(与批次81/83 同款):
//   - **只新增,不改派发**:本文件只提供 scenarioDefsBatch84MarketPolicy 切片;
//     applyScenarioToWorld 与既有 12 旋钮语义零改动;
//   - **零 rand**:幅度全部 × magnitude 线性派生;
//   - **幅度量级对齐既有同组条目**:国库一次性支出 25 万–300 万 / 月度支出
//     10 万–60 万 / 股指冲击 ±10% / 房价冲击 ±12% / 选举偏置 ±15(钳制 ±30)。
package virtual_city

// scenarioDefsBatch84MarketPolicy 批次84 L2 新增:市场与经济 16 条 +
// 政策与治理 14 条(对账表 §1 / §2)。
var scenarioDefsBatch84MarketPolicy = []scenarioDef{
	// ── 市场与经济(+16)──
	{
		spec: ScenarioSpec{
			Kind: "supply_demand_shift", Name: "供需关系调整", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "产能与需求错配:部分品类供给过剩、另一些供给收缩,商品价格指数与供给指数同时分化,企业营收受挤压、库存与就业同步承压。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.02,
			GoodsSupplyShockPct: -0.015,
			RevenueShockPct:     -0.04,
			UnemploymentAdd:     0.004,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "shop_open_close", Name: "商铺开闭业波动", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "街面商铺批量退租或集中开业,商业供给节奏错配:租金与就业短期波动,客流与消费结构随之重排。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct: -0.05,
			UnemploymentAdd: 0.006,
			HouseShockPct:   -0.01,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "emerging_industry", Name: "新兴行业兴起", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "新赛道需求爆发带动相关企业营收与平均工资上行,但传统行业产能被挤出,结构性失业与财政让利压力并存。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:  0.08,
			WageShockPct:     0.05,
			UnemploymentAdd:  -0.004,
			HouseShockPct:    0.012,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "black_market", Name: "黑市交易活跃", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "灰色交易绕开正规渠道:税收流失与价格失真同时发生,正规企业营收下滑、监管支出与信用窗口收紧。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.07,
			GoodsPriceShockPct: -0.01,
			TreasuryMonthlyCNY: 80000,
			CreditWindowDelta:  -0.08,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "financial_fraud", Name: "金融诈骗高发", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "非法集资与电信诈骗抬升居民财产风险:储蓄转向低风险资产,信贷需求走弱、股指承压,满意度偏置下行。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			StockShockPct:   -0.04,
			CreditWindowDelta: -0.1,
			UnemploymentAdd:  0.002,
			ElectionBias:     -6,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "subsidy_grant", Name: "财政补贴发放", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "定向补贴直接改善居民现金流:消费与商品价格指数短期上行,国库赤字扩大,后续财政支出压力累积。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   400000,
			TreasuryMonthlyCNY: 100000,
			GoodsPriceShockPct: 0.012,
			ElectionBias:       4,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "industrial_park", Name: "产业园区落地", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "园区开工带来基建投资与新增岗位:片区房价与工资预期上行,就业吸纳提升但财政先期投入加重。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   800000,
			TreasuryMonthlyCNY: 150000,
			HouseShockPct:      0.015,
			WageShockPct:       0.04,
			UnemploymentAdd:    -0.003,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "investment_attraction", Name: "招商引资热潮", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "税收优惠与土地让利换来的外部投资:企业营收与就业阶段性改善,代价是长期财政让利与本地竞争加剧。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.06,
			UnemploymentAdd:    -0.002,
			TreasuryMonthlyCNY: 120000,
			ElectionBias:       3,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "consumption_voucher", Name: "消费券发放", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "定向消费券撬动短期需求:餐饮与零售成交回暖、商品价格指数上行,财政补贴投放形成一次性现金流出。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   250000,
			GoodsPriceShockPct: 0.01,
			ElectionBias:       5,
		},
		defaultDuration: 3,
	},
	{
		spec: ScenarioSpec{
			Kind: "counterfeit_goods", Name: "盗版商品泛滥", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "低价仿冒品冲击正规品牌:正品溢价收窄、企业营收与就业受压,消费者短期受益但质量与信任成本上升。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: -0.012,
			RevenueShockPct:    -0.06,
			UnemploymentAdd:    0.003,
			ElectionBias:       -3,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "brand_entry_exit", Name: "品牌入驻撤出", DefaultMagnitude: 2,
			Group:       ScenarioGroupMarket,
			Description: "头部品牌集中进驻或撤出:客流与商业租金随之重排,撤出方的就业与营收短期塌陷。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:    0.05,
			HouseShockPct:      0.008,
			UnemploymentAdd:    -0.002,
			GoodsPriceShockPct: 0.006,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "currency_reform", Name: "货币发行制度改革", DefaultMagnitude: 4,
			Group:       ScenarioGroupMarket,
			Description: "货币锚与结算口径调整:存量资产被重新定价,股指与房价同步重估,信贷乘数与政策利率窗口随之收放。",
			Params:      []string{"stance"},
		},
		profile: scenarioProfile{
			StockShockPct:     -0.05,
			GoldShockPct:      0.03,
			HouseShockPct:     -0.02,
			CreditWindowDelta: -0.12,
			PolicyRateDelta:   0.002,
			ElectionBias:      -8,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "wage_shift", Name: "劳动力价格重估", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "最低工资与技能溢价同步调整:平均工资上行推高用工成本,企业营收被挤压、裁员与招工并存。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			WageShockPct:     0.08,
			RevenueShockPct:  -0.05,
			UnemploymentAdd:  0.004,
			GoodsPriceShockPct: 0.015,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "commodity_speculation", Name: "大宗商品投机", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "资金涌入大宗商品投机:原料价格被推高后向下修正,商品价格指数与下游企业营收剧烈波动。",
			Params:      []string{"goods"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct:  0.03,
			GoodsMonthlyDrift:   0.005,
			RevenueShockPct:     -0.04,
			StockDriftMonthly:   0.002,
			UnemploymentMonthly: 0.002,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "cross_border_ecommerce_surge", Name: "跨境电商爆发", DefaultMagnitude: 3,
			Group:       ScenarioGroupMarket,
			Description: "跨境订单激增:仓储与物流环节吃紧,商品价格指数承压,本地零售就业被部分替代。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			GoodsSupplyShockPct: -0.02,
			RevenueShockPct:     0.05,
			UnemploymentAdd:     -0.002,
			GoodsPriceShockPct:  -0.008,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "real_estate_leverage_crackdown", Name: "房产杠杆收紧", DefaultMagnitude: 4,
			Group:       ScenarioGroupMarket,
			Description: "按揭杠杆与违规融资被清理:购房需求骤降、房价指数与股指同步下修,信用窗口收紧放大跌幅。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			HouseShockPct:      -0.04,
			StockShockPct:      -0.035,
			CreditWindowDelta:  -0.12,
			UnemploymentAdd:    0.005,
			ElectionBias:       -6,
		},
		defaultDuration: 12,
	},
	// ── 政策与治理(+14)──
	{
		spec: ScenarioSpec{
			Kind: "census", Name: "人口普查", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "十年一次的人口与住房基线核查:统计口径短期扰动规划与财政分配,登记清理带来就业与居住结构微调。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentAdd: 0.002,
			RevenueShockPct: -0.01,
			TreasurySpendCNY: 200000,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "hukou_reform", Name: "户籍制度调整", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "落户门槛放宽或收紧:人口与劳动力重新分布,公共服务支出与工资预期随之调整,就业结构阶段性波动。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentAdd:   0.003,
			RevenueShockPct:   0.03,
			TreasuryMonthlyCNY: 90000,
			ElectionBias:      -3,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "demolition_relocation", Name: "拆迁安置", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "成片旧改与安置房交付:短期居民现金损失与建筑业就业波动,长期房价指数与公共服务支出重新定价。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			CashDamageFrac:     0.008,
			HouseShockPct:      0.02,
			UnemploymentMonthly: 0.003,
			TreasurySpendCNY:   900000,
			TreasuryMonthlyCNY: 150000,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "healthcare_policy", Name: "医疗政策变动", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "医保支付方式与报销目录调整:医疗机构营收结构重排,居民实际支出与满意度同步变化,财政分担压力上行。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:    -0.04,
			GoodsPriceShockPct: 0.008,
			TreasuryMonthlyCNY: 120000,
			ElectionBias:       -4,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "ip_dispute", Name: "知识产权纠纷", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "专利与商标诉讼集中爆发:研发与品牌投入短期冻结,相关行业营收下行,合规成本与不确定性溢价上升。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct:  -0.05,
			StockShockPct:    -0.02,
			UnemploymentAdd:  0.002,
			TreasurySpendCNY: 200000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "admin_process_reform", Name: "行政审批改革", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "审批环节精简或收紧:企业开办成本下降、营收上行,或审批趋严导致项目延期与失业上行,两者方向相反。",
			Params:      []string{"stance"},
		},
		profile: scenarioProfile{
			RevenueShockPct:   0.05,
			UnemploymentAdd:   -0.003,
			CreditWindowDelta: 0.08,
			PolicyRateDelta:   -0.001,
			ElectionBias:      3,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "official_appointment", Name: "公职人员任免", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "关键岗位人事更替:短期政策连续性与项目推进节奏波动,市场对政策预期的定价随之调整。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			StockDriftMonthly: 0.001,
			ElectionBias:      -2,
			UnemploymentAdd:   0.001,
		},
		defaultDuration: 6,
	},
	{
		spec: ScenarioSpec{
			Kind: "public_service_upgrade", Name: "公共服务升级", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "教育医疗与养老等公共服务扩容:财政支出抬升、居民满意度改善,就业结构向服务业倾斜。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			TreasurySpendCNY:   700000,
			TreasuryMonthlyCNY: 200000,
			UnemploymentAdd:    -0.004,
			ElectionBias:       6,
			RevenueShockPct:    0.02,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "antitrust_probe", Name: "反垄断调查", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "大型企业被立案调查:定价权受限、营收与利润预期下修,合规成本上升,相关行业估值承压。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			RevenueShockPct: -0.06,
			StockShockPct:   -0.025,
			UnemploymentAdd: 0.002,
			TreasurySpendCNY: 200000,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "integrity_probe", Name: "廉政监察行动", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "重点项目与关键岗位被监察:项目审批放缓、投资延后,短期就业与财政支出收缩,秩序感改善带来满意度回升。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentAdd:    0.003,
			RevenueShockPct:    -0.03,
			TreasuryMonthlyCNY: -60000,
			ElectionBias:       5,
		},
		defaultDuration: 9,
	},
	{
		spec: ScenarioSpec{
			Kind: "government_procurement_reform", Name: "政府采购改革", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "集中采购与竞价改革:财政资金使用效率提升,相关行业营收结构重排,单位采购价格下行。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			GoodsPriceShockPct: -0.008,
			RevenueShockPct:    0.03,
			TreasurySpendCNY:   300000,
			UnemploymentAdd:    -0.001,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "urban_planning_revision", Name: "城市规划修编", DefaultMagnitude: 3,
			Group:       ScenarioGroupPolicy,
			Description: "用地与交通规划修编:长期供给格局改变,受影响片区房价与基建投资预期先行调整。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			HouseShockPct:      0.012,
			TreasurySpendCNY:   600000,
			UnemploymentMonthly: 0.001,
			ElectionBias:       -2,
		},
		defaultDuration: 18,
	},
	{
		spec: ScenarioSpec{
			Kind: "fiscal_transparency_reform", Name: "财政透明度改革", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "预算与债务信息公开化:融资成本小幅下行,但短期非标支出被压缩,市场情绪先抑后扬。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			CreditWindowDelta: 0.05,
			StockDriftMonthly: 0.001,
			ElectionBias:       4,
		},
		defaultDuration: 12,
	},
	{
		spec: ScenarioSpec{
			Kind: "cadre_rotation", Name: "干部轮岗调整", DefaultMagnitude: 2,
			Group:       ScenarioGroupPolicy,
			Description: "跨层级干部轮岗:项目审批与资源分配短期重排,地方投资节奏与就业预期出现阶段性扰动。",
			Params:      []string{"sector"},
		},
		profile: scenarioProfile{
			UnemploymentMonthly: 0.0015,
			RevenueShockPct:    -0.015,
			ElectionBias:       -2,
		},
		defaultDuration: 9,
	},
}
