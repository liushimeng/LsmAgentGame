// Package virtual_city — scenario_templates_batch84_culture.go: 批次84 场景
// 模板 · 文化教育 + 环境生态(2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.1.2 模板清单 #8–#11。
//
// 模板清单(契约 §4.1.2):
//
//	#8  cultural_event       事件(12) × 规模(3)              = 36 变体
//	#9  education_event     教育事项(8) × 范围(3)            = 24 变体
//	#10 environmental_issue 问题(12) × 区域(4)               = 48 变体
//	#11 ecology_program     生态工程(8) × 力度(3)            = 24 变体
//
// 纪律:纯数据;派生逻辑见 scenario_templates.go(确定性、零 RNG)。
package virtual_city

// scenarioTemplatesBatch84CultureEnv 批次84 模板 · 文化教育 2 个 + 环境生态 2 个。
var scenarioTemplatesBatch84CultureEnv = []scenarioTemplate{
	{
		Spec: ScenarioSpec{
			Kind: "cultural_event", Name: "文化活动", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "各类文化活动落地:短期客流与文创消费集中释放,配套安保与公共服务支出增加,活动结束后需求回落。",
		},
		Profile: scenarioProfile{
			RevenueShockPct:    0.020,
			GoodsPriceShockPct: 0.006,
			TreasurySpendCNY:   250000,
			UnemploymentAdd:    -0.001,
		},
		Duration: 4,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "事件",
				Suffix: "event",
				Values: []scenarioTemplateValue{
					{Key: "music_festival", Label: "音乐节", Scale: 1.2,
						Adjust: scenarioProfile{RevenueShockPct: 0.020, GoodsPriceShockPct: 0.006, TreasurySpendCNY: 120000}},
					{Key: "film_festival", Label: "电影节", Scale: 1.1,
						Adjust: scenarioProfile{RevenueShockPct: 0.018, TreasurySpendCNY: 100000}},
					{Key: "comic_con", Label: "漫展", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: 0.016, GoodsPriceShockPct: 0.005}},
					{Key: "book_fair", Label: "书展", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: 0.014, TreasuryMonthlyCNY: 60000}},
					{Key: "art_biennale", Label: "艺术双年展", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: 0.016, TreasurySpendCNY: 110000}},
					{Key: "food_festival", Label: "美食节", Scale: 1.1,
						Adjust: scenarioProfile{RevenueShockPct: 0.022, GoodsPriceShockPct: 0.008}},
					{Key: "heritage_show", Label: "非遗展演", Scale: 0.8,
						Adjust: scenarioProfile{RevenueShockPct: 0.012, TreasuryMonthlyCNY: 60000, ElectionBias: 3}},
					{Key: "opera_tour", Label: "戏曲巡演", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: 0.013, ElectionBias: 3}},
					{Key: "anime_con", Label: "动漫大会", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: 0.016, TreasurySpendCNY: 90000}},
					{Key: "photo_week", Label: "摄影周", Scale: 0.8,
						Adjust: scenarioProfile{RevenueShockPct: 0.011, TreasuryMonthlyCNY: 50000}},
					{Key: "drama_festival", Label: "戏剧节", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: 0.012, ElectionBias: 2}},
					{Key: "maker_fair", Label: "创客嘉年华", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: 0.014, TreasurySpendCNY: 80000}},
				},
			},
			{
				Label:  "规模",
				Suffix: "scale",
				Values: []scenarioTemplateValue{
					{Key: "small", Label: "小型", Scale: 0.5,
						Adjust: scenarioProfile{TreasurySpendCNY: -120000, RevenueShockPct: -0.010}},
					{Key: "medium", Label: "中型", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "large", Label: "大型", Scale: 1.7,
						Adjust: scenarioProfile{TreasurySpendCNY: 400000, RevenueShockPct: 0.020, ElectionBias: 3}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "education_event", Name: "教育事件", DefaultMagnitude: 2,
			Group:       ScenarioGroupCulture,
			Description: "教育系统节奏性事件:家庭支出结构与通学安排改变,相关行业需求阶段性波动,考试与假期形成明显潮汐。",
		},
		Profile: scenarioProfile{
			RevenueShockPct:    0.012,
			GoodsPriceShockPct: 0.005,
			TreasuryMonthlyCNY: 60000,
			UnemploymentAdd:    -0.001,
		},
		Duration: 4,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "教育事项",
				Suffix: "matter",
				Values: []scenarioTemplateValue{
					{Key: "term_start", Label: "开学季", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: 0.016, GoodsPriceShockPct: 0.008, TreasuryMonthlyCNY: 80000}},
					{Key: "exam", Label: "统一考试", Scale: 1.1,
						Adjust: scenarioProfile{RevenueShockPct: -0.010, UnemploymentAdd: 0.001}},
					{Key: "graduation", Label: "毕业季", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: 0.012, UnemploymentAdd: -0.002}},
					{Key: "vocational_enroll", Label: "职校招生", Scale: 0.9,
						Adjust: scenarioProfile{RevenueShockPct: 0.010, TreasuryMonthlyCNY: 70000}},
					{Key: "parent_meeting", Label: "家长会", Scale: 0.5,
						Adjust: scenarioProfile{}},
					{Key: "field_trip", Label: "研学旅行", Scale: 0.7,
						Adjust: scenarioProfile{RevenueShockPct: 0.008, TreasuryMonthlyCNY: 40000}},
					{Key: "teacher_training", Label: "教师培训", Scale: 0.6,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 60000, UnemploymentMonthly: 0.001}},
					{Key: "sports_meet", Label: "运动会", Scale: 0.7,
						Adjust: scenarioProfile{RevenueShockPct: 0.009, TreasurySpendCNY: 60000}},
				},
			},
			{
				Label:  "范围",
				Suffix: "scope",
				Values: []scenarioTemplateValue{
					{Key: "single_school", Label: "单校", Scale: 0.4,
						Adjust: scenarioProfile{}},
					{Key: "district", Label: "城区", Scale: 1.0,
						Adjust: scenarioProfile{RevenueShockPct: 0.008}},
					{Key: "citywide", Label: "全市", Scale: 1.6,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 120000, GoodsPriceShockPct: 0.006, ElectionBias: 3}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "environmental_issue", Name: "环境问题", DefaultMagnitude: 3,
			Group:       ScenarioGroupEnvironment,
			Description: "特定环境要素出现问题:通过污染传导链影响生产与健康,治理支出与合规成本抬升,受影响区域的产业受约束。",
		},
		Profile: scenarioProfile{
			GoodsSupplyShockPct: -0.010,
			GoodsPriceShockPct:  0.008,
			RevenueShockPct:     -0.022,
			TreasuryMonthlyCNY:  70000,
		},
		Duration: 9,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "问题",
				Suffix: "issue",
				Values: []scenarioTemplateValue{
					{Key: "water_pollution", Label: "水体污染", Scale: 1.3,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.014, TreasuryMonthlyCNY: 120000, ElectionBias: -4}},
					{Key: "soil_pollution", Label: "土壤污染", Scale: 1.2,
						Adjust: scenarioProfile{HouseShockPct: -0.014, TreasuryMonthlyCNY: 100000}},
					{Key: "air_pollution", Label: "空气污染", Scale: 1.2,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.010, RevenueShockPct: -0.030, ElectionBias: -4}},
					{Key: "noise", Label: "噪声污染", Scale: 0.7,
						Adjust: scenarioProfile{ElectionBias: -2, TreasuryMonthlyCNY: 40000}},
					{Key: "light_pollution", Label: "光污染", Scale: 0.6,
						Adjust: scenarioProfile{ElectionBias: -2, TreasuryMonthlyCNY: 30000}},
					{Key: "invasive_species", Label: "物种入侵", Scale: 1.0,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.010, RevenueShockPct: -0.022}},
					{Key: "vegetation_loss", Label: "植被退化", Scale: 0.9,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.008, TreasuryMonthlyCNY: 70000}},
					{Key: "wetland_loss", Label: "湿地萎缩", Scale: 1.0,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.008, TreasuryMonthlyCNY: 80000, ElectionBias: -3}},
					{Key: "waste_mountain", Label: "垃圾围城", Scale: 1.0,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 90000, ElectionBias: -4}},
					{Key: "coastal_erosion", Label: "海岸侵蚀", Scale: 0.9,
						Adjust: scenarioProfile{HouseShockPct: -0.010, TreasurySpendCNY: 300000}},
					{Key: "mine_rehab", Label: "矿山修复", Scale: 0.9,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 90000, UnemploymentAdd: 0.002}},
					{Key: "plastic_pollution", Label: "塑料污染", Scale: 0.8,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.008, ElectionBias: -3}},
				},
			},
			{
				Label:  "区域",
				Suffix: "area",
				Values: []scenarioTemplateValue{
					{Key: "core", Label: "中心城区", Scale: 1.2,
						Adjust: scenarioProfile{ElectionBias: -4, HouseShockPct: -0.008, TreasuryMonthlyCNY: 100000}},
					{Key: "suburb", Label: "近郊", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "rural", Label: "远郊", Scale: 0.8,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.008, TreasuryMonthlyCNY: 60000}},
					{Key: "river_basin", Label: "全流域", Scale: 1.6,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.016, ElectionBias: -6, TreasuryMonthlyCNY: 150000}},
				},
			},
		},
	},
	{
		Spec: ScenarioSpec{
			Kind: "ecology_program", Name: "生态治理工程", DefaultMagnitude: 2,
			Group:       ScenarioGroupEnvironment,
			Description: "系统性生态工程投入:短期财政与施工支出前置,环境质量改善带来长期供给与健康收益,相关行业产能重新分配。",
		},
		Profile: scenarioProfile{
			TreasurySpendCNY:   500000,
			TreasuryMonthlyCNY: 100000,
			RevenueShockPct:    0.015,
			GoodsPriceShockPct: -0.004,
			ElectionBias:       4,
		},
		Duration: 18,
		Axes: []scenarioTemplateAxis{
			{
				Label:  "生态工程",
				Suffix: "program",
				Values: []scenarioTemplateValue{
					{Key: "waste_sorting", Label: "垃圾分类工程", Scale: 1.0,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 120000, GoodsPriceShockPct: 0.006, RevenueShockPct: -0.010}},
					{Key: "park_network", Label: "公园绿地网络", Scale: 1.0,
						Adjust: scenarioProfile{HouseShockPct: 0.008, RevenueShockPct: 0.012}},
					{Key: "carbon_neutral", Label: "碳中和工程", Scale: 1.3,
						Adjust: scenarioProfile{GoodsPriceShockPct: 0.012, RevenueShockPct: -0.020, TreasuryMonthlyCNY: 180000}},
					{Key: "wetland_restore", Label: "湿地修复", Scale: 1.1,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 130000, ElectionBias: 5}},
					{Key: "river_restoration", Label: "河道整治", Scale: 1.1,
						Adjust: scenarioProfile{GoodsSupplyShockPct: -0.008, TreasuryMonthlyCNY: 110000}},
					{Key: "soil_remediation", Label: "土壤修复", Scale: 1.2,
						Adjust: scenarioProfile{TreasurySpendCNY: 300000, HouseShockPct: 0.006}},
					{Key: "noise_control", Label: "噪声治理", Scale: 0.8,
						Adjust: scenarioProfile{ElectionBias: 3, TreasuryMonthlyCNY: 60000}},
					{Key: "green_roof", Label: "绿色屋顶推广", Scale: 0.8,
						Adjust: scenarioProfile{TreasuryMonthlyCNY: 70000, HouseShockPct: 0.005}},
				},
			},
			{
				Label:  "力度",
				Suffix: "intensity",
				Values: []scenarioTemplateValue{
					{Key: "pilot", Label: "试点", Scale: 0.4,
						Adjust: scenarioProfile{TreasurySpendCNY: -250000, TreasuryMonthlyCNY: -40000}},
					{Key: "rollout", Label: "推广", Scale: 1.0,
						Adjust: scenarioProfile{}},
					{Key: "mandatory", Label: "强制推行", Scale: 1.6,
						Adjust: scenarioProfile{TreasurySpendCNY: 600000, TreasuryMonthlyCNY: 120000, ElectionBias: -3}},
				},
			},
		},
	},
}
