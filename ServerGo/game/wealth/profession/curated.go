// Package profession — curated.go: 精选 14 职业卡内嵌常量(2026-09-14 §财商流P0;
// 2026-09-16 §12座扩容 补齐核心 12 职业)。
//
// 数据出处: 职业卡加载器文档 §2 全量表 —— 财务列引自《规则》§10.1(P01/P03/
// P02/P03/P04/P05/P06/P07/P08/P09/P10/P11/P12)与 §10.1.1(P15/P16);
// 人格/风险/信用分/opening_hook/goals 为 P0 新定(P02/P04/P06/P12 的决策原型
// 对齐《行为金融与投资心理机制设计》§1:P02 焦虑型、P04 从众型、P06/P12 理性型)。
// 所有精选手卡 StartAge=25。
//
// 2026-09-16 §12座扩容:房间 MaxSeats 8 → 12 后,精选池必须 ≥12 张 ——
// 它是文档池不可用时的**唯一兜底**,池小于座位数会让 Draw 重复发卡或
// (旧实现)返回不足 n 张 → 座位拿到零值卡 → 开局即「座位空洞」。
// 现补齐《规则》§10.1 核心 12 职业中缺失的 P02/P04/P06/P12,合计 14 张。
package profession

import "fmt"

// CuratedCards 返回精选 14 卡(顺序即《规则》§10.1/§10.1.1 表序;每次返回副本,
// 调用方可安全改写)。**数量硬约束:≥ wealth.MaxSeats(12)**,由 ValidateCurated 门禁。
func CuratedCards() []Card {
	return []Card{
		{
			ID: "P01", Title: "外卖骑手", Salary: 5000, Expense: 3200, Savings: 8000,
			StartAge: 25, Energy: 8, Network: 2, Cognition: 1, CreditScore: 550,
			HomeDistrict: "commerce", RiskPreference: "aggressive",
			Personality:    []string{"务实主义", "果决有力"},
			BehaviorTraits: []string{"精打细算"},
			HealthGrade:    "A", Marital: "single",
			OpeningHook: "风里雨里跑了三年，卡里就八千块。我不想送一辈子外卖，先攒出第一桶金。",
			Goals:       []string{"5 年内攒下 15 万启动资金，学会让钱替我干活。", "建立 6 个月应急金。"},
			Source:      "curated",
		},
		{
			ID: "P02", Title: "超市服务员", Salary: 4500, Expense: 2800, Savings: 5000,
			StartAge: 25, Energy: 7, Network: 2, Cognition: 1, CreditScore: 550,
			HomeDistrict: "commerce", RiskPreference: "conservative",
			Personality:    []string{"谨慎保守", "务实主义"},
			BehaviorTraits: []string{"保守储蓄", "精打细算"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "在超市理了六年货，月薪四千五。我不怕累，就怕一场病把家底掏空。",
			Goals:       []string{"5 年内攒够 6 个月生活费的应急金，不再怕突发开支。", "把工资分成生活、储蓄、保障、投资四份。"},
			Source:      "curated",
		},
		{
			ID: "P03", Title: "保安/司机", Salary: 5500, Expense: 3500, Savings: 10000,
			StartAge: 25, Energy: 7, Network: 3, Cognition: 2, CreditScore: 550,
			HomeDistrict: "oldtown", RiskPreference: "conservative",
			Personality:    []string{"顺从协作", "谨慎保守"},
			BehaviorTraits: []string{"保守储蓄"},
			HealthGrade:    "A", Marital: "single",
			OpeningHook: "站岗十小时，月薪五千五。安稳是安稳，可我不想五十岁还在替别人看大门。",
			Goals:       []string{"5 年内建立每月 2000 元被动收入，给自己多一条路。", "不碰任何高息贷款。"},
			Source:      "curated",
		},
		{
			ID: "P04", Title: "工厂技工", Salary: 6000, Expense: 3800, Savings: 12000,
			StartAge: 25, Energy: 8, Network: 2, Cognition: 2, CreditScore: 550,
			HomeDistrict: "industry", RiskPreference: "balanced",
			Personality:    []string{"顺从协作", "务实主义"},
			BehaviorTraits: []string{"勤奋自律"},
			HealthGrade:    "A", Marital: "single",
			OpeningHook: "车间六年，手上的活不含糊。工资涨得慢，我想让攒下的钱也去上班。",
			Goals:       []string{"5 年内攒下 20 万启动资金，考出高级技工证提薪。", "每月强制储蓄率 ≥ 25%。"},
			Source:      "curated",
		},
		{
			ID: "P05", Title: "小学教师", Salary: 9000, Expense: 6000, Savings: 30000,
			StartAge: 25, Energy: 6, Network: 4, Cognition: 5, CreditScore: 650,
			HomeDistrict: "residential", RiskPreference: "conservative",
			Personality:    []string{"尽责坚韧"},
			BehaviorTraits: []string{"保守储蓄", "长线规划"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "粉笔灰吃了七年，存款三万。教书育人不慌，我怕的是一眼望到头的工资条。",
			Goals:       []string{"5 年内攒够一套郊区房的首付，让家安下来。", "每月定投指数基金。"},
			Source:      "curated",
		},
		{
			ID: "P06", Title: "护士", Salary: 10000, Expense: 6500, Savings: 25000,
			StartAge: 25, Energy: 6, Network: 4, Cognition: 4, CreditScore: 650,
			HomeDistrict: "residential", RiskPreference: "conservative",
			Personality:    []string{"尽责坚韧", "平和包容"},
			BehaviorTraits: []string{"风险管理", "记账习惯"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "三班倒的病房里，见过太多因病返贫。我要先给自己家筑牢底仓。",
			Goals:       []string{"5 年内攒够首付，把房租变成自己的月供。", "配齐家庭保险，不让一场病击穿现金流。"},
			Source:      "curated",
		},
		{
			ID: "P07", Title: "公务员", Salary: 12000, Expense: 8000, Savings: 50000,
			StartAge: 25, Energy: 7, Network: 6, Cognition: 5, CreditScore: 650,
			HomeDistrict: "oldtown", RiskPreference: "conservative",
			Personality:    []string{"外向社交"},
			BehaviorTraits: []string{"长线规划", "记账习惯"},
			HealthGrade:    "A", Marital: "single",
			OpeningHook: "体制内第八年，钱不多但稳。同学都下海了，我打算稳中求进慢慢布局。",
			Goals:       []string{"5 年内完成两套住宅配置，家庭被动收入覆盖基本开销。", "子女教育金先备 10 万。"},
			Source:      "curated",
		},
		{
			ID: "P08", Title: "销售代表", Salary: 12000, Expense: 8500, Savings: 20000,
			StartAge: 25, Energy: 5, Network: 7, Cognition: 4, CreditScore: 650,
			HomeDistrict: "commerce", RiskPreference: "balanced",
			Personality:    []string{"外向社交"},
			BehaviorTraits: []string{"积极投资"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "靠嘴皮子吃饭，行情好月月超额。趁年轻胆子大，我要把提成变成资产。",
			Goals:       []string{"5 年内净资产突破 100 万，摆脱纯靠提成吃饭。", "每季度复盘一次持仓。"},
			Source:      "curated",
		},
		{
			ID: "P09", Title: "初级程序员", Salary: 15000, Expense: 10000, Savings: 40000,
			StartAge: 25, Energy: 6, Network: 4, Cognition: 6, CreditScore: 650,
			HomeDistrict: "tech", RiskPreference: "balanced",
			Personality:    []string{"开放求新"},
			BehaviorTraits: []string{"记账习惯", "积极投资"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "写代码第五年，年包二十来万。我信数据不信运气，定投+记账慢慢滚。",
			Goals:       []string{"5 年内指数基金持仓 50 万，FI 指数达到 0.5。", "每月强制储蓄率 ≥ 30%。"},
			Source:      "curated",
		},
		{
			ID: "P10", Title: "医生", Salary: 25000, Expense: 18000, Savings: 100000,
			StartAge: 25, Energy: 5, Network: 5, Cognition: 7, CreditScore: 700,
			HomeDistrict: "residential", RiskPreference: "conservative",
			Personality:    []string{"尽责坚韧"},
			BehaviorTraits: []string{"风险管理"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "白大褂下是还不完的房贷。收入高开销也高，我得学会像管理病人一样管理钱。",
			Goals:       []string{"5 年内还清一半房贷，建立孩子的教育金。", "家庭保险配置齐全。"},
			Source:      "curated",
		},
		{
			ID: "P11", Title: "律师", Salary: 30000, Expense: 20000, Savings: 150000,
			StartAge: 25, Energy: 5, Network: 7, Cognition: 7, CreditScore: 700,
			HomeDistrict: "finance", RiskPreference: "aggressive",
			Personality:    []string{"果决有力"},
			BehaviorTraits: []string{"积极投资"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "时薪三千，照样月光。见惯了财富易主，这次我要做自己案子的当事人。",
			Goals:       []string{"5 年内构建 1.5 万月被动收入，把时间从时薪里赎回来。", "扩张人脉到 9 以上。"},
			Source:      "curated",
		},
		{
			ID: "P12", Title: "高级金融/技术专家", Salary: 40000, Expense: 25000, Savings: 300000,
			StartAge: 25, Energy: 6, Network: 6, Cognition: 8, CreditScore: 700,
			HomeDistrict: "finance", RiskPreference: "aggressive",
			Personality:    []string{"果决有力", "开放求新"},
			BehaviorTraits: []string{"积极投资", "长线规划"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "在金融圈摸爬这些年，看惯了周期起落。这次我要给自己的账户做配置。",
			Goals:       []string{"5 年内把主动收入的一半换成被动收入，FI 指数破 1。", "穿越一轮完整牛熊，组合最大回撤控制在 20% 以内。"},
			Source:      "curated",
		},
		{
			ID: "P15", Title: "早餐店主", Salary: 12000, Expense: 7500, Savings: 60000,
			StartAge: 25, Energy: 4, Network: 5, Cognition: 3, CreditScore: 600,
			HomeDistrict: "oldtown", RiskPreference: "balanced",
			Personality:    []string{"独立自主"},
			BehaviorTraits: []string{"精打细算"},
			HealthGrade:    "C", Marital: "single",
			OpeningHook: "凌晨三点的豆浆香，是我全部的家当。生意稳但太单一，得想想退路。",
			Goals:       []string{"5 年内攒出第二家店的启动金，同时配置一份金融资产。", "给自己买份医疗险。"},
			Source:      "curated",
		},
		{
			ID: "P16", Title: "自媒体博主", Salary: 9000, SalaryVolatile: true, Expense: 7000, Savings: 20000,
			StartAge: 25, Energy: 5, Network: 6, Cognition: 6, CreditScore: 600,
			HomeDistrict: "tech", RiskPreference: "aggressive",
			Personality:    []string{"开放求新"},
			BehaviorTraits: []string{"冲动决策"},
			HealthGrade:    "B", Marital: "single",
			OpeningHook: "三万粉的博主，上个月爆了，这个月凉透。流量是过山车，我要把波动变成台阶。",
			Goals:       []string{"5 年内用流量收入攒下 40 万稳健资产，告别收入焦虑。", "建立 12 个月生活费的安全垫。"},
			Source:      "curated",
		},
	}
}

// P16VolatilityBand P16 波动带(《规则》§10.1.1):奇数月 U(3000,8000)、偶数月 U(5000,25000)。
const (
	P16BandLowOdd  = 3000
	P16BandHighOdd = 8000
	P16BandLowEven = 5000
	P16BandHighEvt = 25000
)

// CuratedByID 按 id 查精选卡;未命中返回 nil。
func CuratedByID(id string) *Card {
	cards := CuratedCards()
	for i := range cards {
		if cards[i].ID == id {
			return &cards[i]
		}
	}
	return nil
}

// ValidateCurated 校验全部精选卡(加载器测试 G 门禁用)。
//
// 2026-09-16 §12座扩容:数量门禁由「恒等于 10」改为「≥ MinCuratedPool(12)」——
// 精选池是文档池不可用时的唯一兜底,必须能覆盖 MaxSeats 个座位且互不重复,
// 否则 Draw 会重复发卡 / 返回不足张数(座位空洞)。
func ValidateCurated() error {
	cards := CuratedCards()
	if len(cards) < MinCuratedPool {
		return fmt.Errorf("curated pool must hold at least %d cards (MaxSeats coverage), got %d",
			MinCuratedPool, len(cards))
	}
	seen := map[string]struct{}{}
	for i := range cards {
		c := &cards[i]
		if _, dup := seen[c.ID]; dup {
			return fmt.Errorf("curated duplicate id %s", c.ID)
		}
		seen[c.ID] = struct{}{}
		if c.StartAge != 25 {
			return fmt.Errorf("curated card %s: start_age must be 25", c.ID)
		}
		if c.Source != "curated" {
			return fmt.Errorf("curated card %s: source must be curated", c.ID)
		}
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// MinCuratedPool 精选池最小张数(= 房间 MaxSeats 12;2026-09-16 §12座扩容)。
// profession 包不得反向 import game/wealth,故此处独立维护常量,
// 由 game/wealth 侧的 seats12 测试断言两者一致(§130 接线校验)。
const MinCuratedPool = 12
