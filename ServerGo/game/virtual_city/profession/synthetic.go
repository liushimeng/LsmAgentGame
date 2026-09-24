// Package profession — synthetic.go: 确定性合成职业卡兜底(2026-09-22
// §17-CityHuman 全民驱动,契约 03 §2.2 —— 精选 14 卡层整体退役后的新兜底)。
//
// 文档池不可用时的兜底:模板职业表(12 基础 + 批次20 追加 16 新区 = 28 条)
// × 收入档 ± 抖动 × 人格词库抽样,
// 同 rng 序 → 同卡集(确定性,单测断言)。Source 标注 "synthetic";失败语义:
// 文档池可用时(常态,10 万卡)零行为变化,不可用时城市照常运行,档案锚定
// 降级为合成姓名(city/profile.go 现有兜底链不变)。
package profession

import (
	"fmt"
	"math/rand"
)

// syntheticTemplate 合成卡模板(收入档为月薪基数,抖动由 rng 现场派生)。
type syntheticTemplate struct {
	Title       string
	Salary      int64
	District    string
	Risk        string
	Personality []string
	Hook        string // 30–50 字开场白(Validate 要求 [20,60] rune)
}

// syntheticTemplates 模板职业表(契约 03 §2.2 固定 12 条:外卖骑手/教师/
// 程序员/医生/律师/店主/博主/保安/销售/公务员/护工/技工;批次20 §2.4 再追加
// 16 条,覆盖 16 个新区,共 28 条)。人格词全部取自
// Schema v1.1 十三词库(personalityVocab)。
// ⚠️ 前 12 条顺序冻结:n≤12 的座位发卡(合成兜底路径)逐张不变;新区模板仅在
// n>12 与背景居民抽样时循环进入。
var syntheticTemplates = [28]syntheticTemplate{
	{"外卖骑手", 5000, "commerce", "balanced", []string{"务实主义", "果决有力"}, "风里雨里跑了三年,卡里的积蓄不多,但我不打算送一辈子外卖。"},
	{"教师", 9000, "edu_district", "conservative", []string{"尽责坚韧", "谨慎保守"}, "教书第七年,工资条一眼望得到头,我想让存款稳稳变多。"},
	{"程序员", 15000, "hightech_park", "balanced", []string{"开放求新", "内省深思"}, "写代码第五年,我信数据不信运气,定投和记账都在坚持。"},
	{"医生", 25000, "medical_city", "conservative", []string{"尽责坚韧", "平和包容"}, "病房里见过太多因病返贫,我要先给自己家筑牢现金流的底。"},
	{"律师", 30000, "finance", "aggressive", []string{"果决有力", "冒险敢为"}, "见惯了财富易主,这一次我要做自己案子的当事人。"},
	{"店主", 12000, "oldtown", "balanced", []string{"独立自主", "务实主义"}, "小店生意稳但单一,我得起第二条路,不能吊死在一扇门上。"},
	{"博主", 9000, "cultural_creative", "aggressive", []string{"开放求新", "乐观豁达"}, "流量像过山车,上个月爆了这个月凉透,我要把波动变成台阶。"},
	{"保安", 5500, "industry", "conservative", []string{"顺从协作", "谨慎保守"}, "站岗十小时,月薪五千五,安稳是安稳,可我也想要条出路。"},
	{"销售", 12000, "commerce", "aggressive", []string{"外向社交", "进取心强"}, "靠嘴皮子吃饭,行情好月月超额,趁年轻胆子大要敢想敢干。"},
	{"公务员", 12000, "central_park", "conservative", []string{"外向社交", "长线规划"}, "体制内第八年,钱不多但稳,我打算稳中求进慢慢布局。"},
	{"护工", 6000, "residential", "conservative", []string{"尽责坚韧", "平和包容"}, "照顾老人这份工不体面但踏实,我想给家里攒出一份保障。"},
	{"技工", 6000, "industrial_park", "balanced", []string{"顺从协作", "勤奋自律"}, "车间六年手上的活不含糊,我想让攒下的钱也去上班生钱。"},
	// ── 批次20 §2.4:16 个新区各 1 条(前 12 条顺序不可动,仅尾部追加) ──
	{"风控经理", 20000, "fin_sub_center", "conservative", []string{"尽责坚韧", "谨慎保守"}, "风控见得越多越信复利,副中心深夜的灯里有我攒首付的影子。"},
	{"测试工程师", 14000, "software_park", "balanced", []string{"开放求新", "内省深思"}, "在软件园写用例第三年,涨薪慢,但我信攒钱和跳槽都能主动。"},
	{"免税店导购", 8000, "airport_town", "balanced", []string{"外向社交", "乐观豁达"}, "免税店提成看天吃饭,旺季想拼命存,淡季最怕一口气泄了。"},
	{"仓库调度员", 7000, "air_logistics", "conservative", []string{"顺从协作", "务实主义"}, "管着仓库几千个货位,我自己也想给将来理出一条清楚的道。"},
	{"整车工艺工程师", 16000, "auto_city", "balanced", []string{"尽责坚韧", "开放求新"}, "总装线每下线一辆车,我心里就同步下线一个攒钱的小目标。"},
	{"民宿管家", 7000, "mountain_resort", "conservative", []string{"平和包容", "独立自主"}, "山里客栈账本薄,可我想给客人和自己都攒出一份底气。"},
	{"化工巡检员", 9000, "chem_park", "conservative", []string{"谨慎保守", "尽责坚韧"}, "巡检一圈圈地走,工资一条条地攒,我要的是稳稳的现金流。"},
	{"大棚技术员", 6500, "agri_park", "balanced", []string{"乐观豁达", "务实主义"}, "大棚里用数据种地第一年,我想把技术换成城里首付。"},
	{"康养理疗师", 7500, "health_town", "conservative", []string{"平和包容", "尽责坚韧"}, "照护别人的健康,也得规划自己的钱包,康养先从储蓄开始。"},
	{"轧钢技师", 8500, "steel_town", "balanced", []string{"务实主义", "进取心强"}, "轧机声里干了八年,我想让攒下的钱像钢卷一样成卷地滚。"},
	{"非遗手作人", 6000, "old_city_culture", "aggressive", []string{"开放求新", "独立自主"}, "老手艺不算暴利,我边摆摊边学运营,想体体面面地攒钱。"},
	{"高校辅导员", 9500, "university_town", "conservative", []string{"尽责坚韧", "顺从协作"}, "辅导员薪水不高但稳定,我想把每年的结余理成清晰的台阶。"},
	{"生态管护员", 6000, "wetland_park", "conservative", []string{"谨慎保守", "乐观豁达"}, "守湖第三年,工资不见涨,我想靠攒钱给将来多留一片余地。"},
	{"场馆运营专员", 8000, "sports_new_city", "balanced", []string{"外向社交", "进取心强"}, "场馆里赛事一场接一场,我想让存款也跟着赛季屡创新高。"},
	{"游艇会经理", 18000, "bay_new_town", "aggressive", []string{"外向社交", "冒险敢为"}, "湾区游艇来客多,流水诱人,我打算把钱从消费变成资产。"},
	{"TOD招商专员", 12000, "highspeed_rail_town", "balanced", []string{"进取心强", "外向社交"}, "高铁站旁做招商,见过太多起落,我信慢慢攒才睡得安稳。"},
}

// SyntheticCards 确定性合成 n 张职业卡(文档池不可用时的兜底,替代原精选
// 14 卡;契约 03 §2.2)。由 rng 派生:模板职业表 × 收入档 ± 抖动 × 数值
// 档位抽样;StartAge clamp [20,55];Source 标注 "synthetic"。同 rng 序 →
// 同卡集(确定性测试断言)。恒返回 n 张(座位不会因零值卡变成空洞)。
func SyntheticCards(n int, rng *rand.Rand) []Card {
	if n <= 0 {
		return nil
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	out := make([]Card, 0, n)
	for i := 0; i < n; i++ {
		tpl := &syntheticTemplates[i%len(syntheticTemplates)]
		salary := int64(float64(tpl.Salary) * (0.85 + 0.3*rng.Float64()))
		expense := int64(float64(salary) * (0.55 + 0.2*rng.Float64()))
		savings := salary * int64(3+rng.Intn(4))  // 3..6 个月开支
		age := 20 + rng.Intn(36)                  // clamp [20,55]
		energy := 5 + rng.Intn(4)                 // 5..8
		network := 2 + rng.Intn(5)                // 2..6
		cognition := 2 + rng.Intn(6)              // 2..7
		credit := 550 + 50*rng.Intn(4)            // 550..700
		health := "A"                             // 健康档按收入档位(低档 B)
		if salary < 8000 {
			health = "B"
		}
		out = append(out, Card{
			ID:             fmt.Sprintf("S%04d", i+1),
			Title:          tpl.Title,
			Salary:         salary,
			Expense:        expense,
			Savings:        savings,
			StartAge:       age,
			Energy:         energy,
			Network:        network,
			Cognition:      cognition,
			CreditScore:    credit,
			HomeDistrict:   tpl.District,
			RiskPreference: tpl.Risk,
			Personality:    append([]string(nil), tpl.Personality...),
			BehaviorTraits: []string{"精打细算"},
			HealthGrade:    health,
			Marital:        "single",
			OpeningHook:    tpl.Hook,
			Goals:          []string{"5 年内把储蓄翻一番,并攒够 6 个月生活费的安全垫。"},
			Source:         "synthetic",
		})
	}
	return out
}
