// Package city — backdrop.go: 城市背景居民(紧凑数组 + 月度演化 + 聚合快照)。
//
// 契约: 虚拟城市-大规模城市居民背景模拟设计-v1.md §4(2026-09-21);
// 2026-09-22 §17-CityHuman 全民驱动:resident 增 intent/moveTarget 位
// (≈22B,仍满足 ≤40B 预算;契约 02 §4)。
//
// 背景居民是数据,不是 goroutine:每城 1..100,000+ 名居民以紧凑结构存储,
// 数值规则逐月演化(TickMonth);LLM 深认知只给深度座位(≤12,全量引擎不变)、
// 「城市之声」抽样居民(voice.go)与**驱动层**轮次居民(driver.go)。
// 性能预算:100K 初始化 <200ms、月度 tick <100ms、常驻内存增量 <32MB。
package city

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"

	"LsmAgentGame/game/virtual_city/profession"
)

// 背景居民演化常量(契约 03 §4.3)。
const (
	unemploymentMonthly  = 0.008 // 就业者每月失业概率 0.8%
	reemploymentMonthly  = 0.15  // 失业者每月再就业概率 15%
	stressExpenseMonths  = 3.0   // 压力位:savings < 3×expense
	ageMin, ageMax       = 18.0, 70.0
	incomeMin, incomeMax = 500.0, 200000.0
	incomeLogSigma       = 0.55 // lognormal σ
	ageSigma             = 6.0
	// maxBackdropResidents 防御性硬上限(正常路径由 cfg clamp 100000;
	// 此处防内部调用方传天文数字直接 OOM)。
	maxBackdropResidents = 1_000_000
	// snapshotSampleCap 中位数抽样上限(契约:抽样 4096 人)。
	snapshotSampleCap = 4096
	// voicesRingCap 城市之声环形缓冲容量(契约:最近 20 条)。
	voicesRingCap = 20
)

// resident 单个背景居民(紧凑;2026-09-22 §17 增 intent/moveTarget 后
// ≈22B,预算 ≤40B)。未导出:外部只经 Backdrop 方法 / Snapshot 观测。
type resident struct {
	income     float32 // 月收入(元;失业置 0)
	expense    float32 // 月支出(元)
	savings    float64 // 累计储蓄
	domain     uint8   // L1 域索引 0..25(255=未知)
	district   uint8   // 城区 0..15
	age        uint8   // 岁(每 12 个 tick +1)
	flags      uint8   // bit0=employed bit1=stressed bit2=voiced(本月已发声)
	intent     uint8   // 本月意图(驱动层 set_intent 写入;TickMonth 消费后清除)
	moveTarget uint8   // 目标城区(仅 move_out;消费后清除)
}

const (
	flagEmployed uint8 = 1 << iota
	flagStressed
	flagVoiced
	domainUnknown uint8 = 255
)

// 居民意图编码(契约 02 §4:set_intent 写入 → TickMonth 按位消费 → 清除,
// 意图只生效一个月)。
const (
	intentNone       uint8 = 0
	intentJobSeeking uint8 = 1 // 失业者本月再就业概率 15% → 30%
	intentFrugal     uint8 = 2 // 本月 expense ×0.9
	intentConsume    uint8 = 3 // 本月 expense ×1.25(仅居民侧,消费品市场口径不变)
	intentSocialize  uint8 = 4 // 本月 stress 解除概率 +20%
	intentMoveOut    uint8 = 5 // 本月末迁移至 moveTarget 城区
)

// 意图消费常量(契约 02 §4 表格)。
const (
	reemploymentJobSeeking   = 0.30 // job_seeking 加成后再就业概率
	frugalExpenseFactor      = 0.9  // frugal 本月支出系数
	consumeExpenseFactor     = 1.25 // consume 本月支出系数
	socializeStressReliefPr  = 0.20 // socialize 压力位额外解除概率
)

// Backdrop 是一城的背景居民数组 + 月度演化状态。并发安全(内部互斥);
// 确定性:同 seed + 同 calib + 同调用序列 → 同城同人(单测断言)。
type Backdrop struct {
	mu        sync.Mutex
	residents []resident
	calib     *CalibTable
	month     int           // 已 tick 月数(b.month%12==0 时全员 +1 岁)
	voices    []VoiceRecord // 环形缓冲(最近 voicesRingCap 条)
	voiceRng  *rand.Rand    // 城市之声抽样专用(与经济演化 rng 流分离)

	// ── 2026-09-21 §档案锚定(契约 §4)──
	// profiles 与 residents 同长;未锚定位保持零值,有效范围为前 profAnchored
	// 个(锚定是一次性前缀热替换:池不足时只锚定前 len(cards) 名)。
	// profDone/profTotal/profPoolSize 为锚定流水线进度(mu 内读写即可,无需
	// atomic —— 编排 goroutine 回写与 Snapshot 读取频率都极低)。
	profiles     []ResidentProfile
	profStatus   int // ProfIdle | ProfHydrating | ProfReady | ProfFailed
	profDone     int // 已水合张数(进度)
	profTotal    int // 本轮水合总数
	profPoolSize int // 文档池索引总数(展示「卡池 10 万」)
	profAnchored int // 已锚定居民数(前缀长度)

	// ── 2026-09-22 §17-CityHuman 全民驱动(契约 02 §5)──
	// driver 驱动层(Start 时装配;nil = 关闭 → Snapshot 不下发 driver 块)。
	// 锁序:Backdrop.mu → driver.mu(driver 侧绝不反向嵌套)。
	driver *ResidentDriver
}

// NewBackdrop 确定性合成 n 名背景居民(契约 03 §4.2)。
// rng 由房间 seed 派生(独立于引擎 rng);calib 为 nil 时用合成默认。
// 同 seed + 同 calib → 逐字段相等(合成序:voice 种子 → 域 → 城区 → 年龄 →
// 收入 → 支出 → 储蓄 → 就业)。
func NewBackdrop(n int, rng *rand.Rand, calib *CalibTable) *Backdrop {
	if calib == nil {
		calib = SyntheticCalibTable()
	}
	if n < 0 {
		n = 0
	}
	if n > maxBackdropResidents {
		n = maxBackdropResidents
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	// 城市之声抽样种子:优先于合成流消耗一次,保证与演化流独立且可复现。
	voiceRng := rand.New(rand.NewSource(rng.Int63()))

	// 域/城区权重累积表(前缀和,O(1) 查询)。
	domainCum := make([]float64, domainCount)
	acc := 0.0
	for i, d := range calib.Domains {
		if d.Weight < 0 {
			continue
		}
		acc += d.Weight
		domainCum[i] = acc
	}
	if acc <= 0 {
		for i := range domainCum {
			domainCum[i] = float64(i+1) / domainCount
		}
		acc = 1
	}
	districtCum := make([]float64, districtCount)
	acc = 0.0
	for i, w := range calib.Districts {
		if w < 0 {
			continue
		}
		acc += w
		districtCum[i] = acc
	}
	if acc <= 0 {
		for i := range districtCum {
			districtCum[i] = float64(i+1) / districtCount
		}
		acc = 1
	}

	residents := make([]resident, n)
	for i := range residents {
		r := &residents[i]
		// 域(按权重二分/线性落点;26 项线性足够快且简单)。
		di := 0
		pick := rng.Float64() * acc
		for di < domainCount-1 && pick > domainCum[di] {
			di++
		}
		r.domain = uint8(di)
		ds := calib.Domains[di]
		// 城区。
		di2 := 0
		pick2 := rng.Float64()
		for di2 < districtCount-1 && pick2 > districtCum[di2] {
			di2++
		}
		r.district = uint8(di2)
		// 年龄 ~ clamp(round(N(AgeMean,6)), 18,70)。
		age := math.Round(ds.AgeMean + rng.NormFloat64()*ageSigma)
		r.age = uint8(clampF(age, ageMin, ageMax))
		// 收入 ~ clamp(lognormal(ln(IncomeMean),0.55), 500, 200000)。
		inc := math.Exp(math.Log(pos(ds.IncomeMean)) + rng.NormFloat64()*incomeLogSigma)
		inc = clampF(inc, incomeMin, incomeMax)
		r.income = float32(inc)
		// 支出 = 收入 × ExpenseRatioMean × U(0.85,1.15)。
		r.expense = float32(inc * pos(ds.ExpenseRatioMean) * (0.85 + 0.3*rng.Float64()))
		// 储蓄 = 收入 × SavingsMonths × U(0.5,2.0)。
		r.savings = inc * pos(ds.SavingsMonths) * (0.5 + 1.5*rng.Float64())
		// 就业位。
		if rng.Float64() < calib.Employment {
			r.flags = flagEmployed
		}
	}
	return &Backdrop{
		residents: residents,
		calib:     calib,
		voiceRng:  voiceRng,
	}
}

// TickMonth 月度演化(契约 03 §4.3;锁内互斥,cpi 为月环比通胀,调用方
// 传引擎值,缺省语义 0.002 由调用方决定)。rng 为房间城市 rng(连续流,
// 同 seed 同演化)。规则:
//   - 就业者:savings += income − expense;expense ×= (1+cpi);每月 0.8% 失业(收入 0)
//   - 失业者:每月 15% 再就业,收入 = 域均值 × U(0.8,1.1)
//   - 压力位:savings < 3×expense → stressed(回升清除)
//   - 每 12 个 tick 全员 +1 岁;月初清除 voiced 位(城市之声去重)
//
// 2026-09-22 §17-CityHuman(契约 02 §4):意图消费 —— resident.intent 由
// 驱动层 set_intent 写入,本月演化按表加成后**清除**(意图只生效一个月):
//   - job_seeking:失业者本月再就业概率 15% → 30%
//   - frugal / consume:本月 expense ×0.9 / ×1.25(基数不变,仅当月口径)
//   - socialize:本月 stress 解除概率 +20%(stressed 位清退加成)
//   - move_out:本月末迁移至 moveTarget 城区(无校验失败则忽略)
func (b *Backdrop) TickMonth(cpi float64, rng *rand.Rand) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.month++
	ageTick := b.month%12 == 0
	for i := range b.residents {
		r := &b.residents[i]
		r.flags &^= flagVoiced
		// ① 本月支出口径(frugal/consume 只调当月,基数留给 cpi 演化)。
		effExpense := float64(r.expense)
		switch r.intent {
		case intentFrugal:
			effExpense *= frugalExpenseFactor
		case intentConsume:
			effExpense *= consumeExpenseFactor
		}
		// ② 就业演化。
		if r.flags&flagEmployed != 0 {
			r.savings += float64(r.income) - effExpense
			r.expense = float32(float64(r.expense) * (1 + cpi))
			if rng.Float64() < unemploymentMonthly {
				r.flags &^= flagEmployed
				r.income = 0
			}
		} else {
			p := reemploymentMonthly
			if r.intent == intentJobSeeking {
				p = reemploymentJobSeeking
			}
			if rng.Float64() < p {
				r.flags |= flagEmployed
				r.income = float32(b.domainIncomeMeanLocked(r.domain) * (0.8 + 0.3*rng.Float64()))
			}
		}
		// ③ 压力位(消费口径用 effExpense —— 本月「实际」生活支出)。
		stressedNow := r.savings < stressExpenseMonths*effExpense
		if stressedNow && r.intent == intentSocialize && rng.Float64() < socializeStressReliefPr {
			stressedNow = false // socialize 清退加成
		}
		if stressedNow {
			r.flags |= flagStressed
		} else {
			r.flags &^= flagStressed
		}
		// ④ move_out:月末迁移(目标城区越界 → 忽略)。
		if r.intent == intentMoveOut && int(r.moveTarget) < districtCount {
			r.district = r.moveTarget
		}
		// ⑤ 意图消费完毕清除(只生效一个月)。
		r.intent = intentNone
		r.moveTarget = 0
		if ageTick && r.age < 255 {
			r.age++
		}
	}
}

// domainIncomeMeanLocked 返回域均值(未知域回落全表加权均值)。
func (b *Backdrop) domainIncomeMeanLocked(domain uint8) float64 {
	if domain != domainUnknown && int(domain) < len(b.calib.Domains) {
		return pos(b.calib.Domains[domain].IncomeMean)
	}
	total := 0.0
	for _, d := range b.calib.Domains {
		total += pos(d.Weight) * pos(d.IncomeMean)
	}
	return total
}

// DistrictPop 是快照的城区人口行。
type DistrictPop struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Population int    `json:"population"`
}

// districtNames 32 城区展示名(顺序与 profession.DistrictIDs /
// game/virtual_city.DistrictDefs 对齐:finance/tech/industry/oldtown/commerce/
// residential/suburb/riverside + v2.12 8 区 + 批次20 16 新区;city 不 import
// wealth,此处独立维护,与 loader 的 districtIDs 同款同步策略)。
// 2026-09-21 §城市扩张v2.12 阶段2:8 → 16(前 8 P0 区顺序冻结,追加 8 新区);
// 2026-09-24 批次20:16 → 32(前 16 区顺序冻结,按契约 §2 表序追加 16 新区)。
var districtNames = [districtCount]string{
	"金融CBD", "科技园", "工业区", "老城区", "商业中心", "居住区", "郊区", "滨河新区",
	"物流港", "高新园区", "教育园区", "医疗城", "产业基地", "中央公园", "交通枢纽", "文创区",
	"金融副中心", "软件园", "空港小镇", "航空物流园", "汽车城", "山居民宿区", "化工园区", "现代农业园",
	"康养小镇", "特钢镇", "古城文化区", "大学城", "湿地公园", "体育新城", "湾区新城", "高铁新城",
}

// Snapshot 城市聚合快照(契约 03 §4.4;随 game.state.city 下发,无座位隐私,
// 观战/玩家全量可见)。
type Snapshot struct {
	ResidentCount  int           `json:"resident_count"`
	EmploymentRate float64       `json:"employment_rate"`
	MedianIncome   float64       `json:"median_income"` // 抽样 4096 人取中位数
	TotalSavings   float64       `json:"total_savings"`
	AvgAge         float64       `json:"avg_age"`
	StressedRate   float64       `json:"stressed_rate"`
	Districts      []DistrictPop `json:"districts"`        // [{id,name,population}]
	Voices         []VoiceRecord `json:"voices,omitempty"` // 最近 20 条城市之声
	// Profiles 档案锚定进度(2026-09-21 §档案锚定契约 §4;从未启动锚定的
	// 房间(nil/纯合成)恒 nil → omitempty 不下发,前端向后兼容)。
	Profiles *ProfileProgress `json:"profiles,omitempty"`
	// Ambiance 各城区当月气味/声响标签(2026-09-22 §CityHuman重构;
	// 由 wealth 层在广播前填充(基底表 + 当月事件叠加),nil → omitempty)。
	Ambiance map[string]AmbianceTags `json:"ambiance,omitempty"`
	// Driver 居民驱动层快照(2026-09-22 §17-CityHuman 契约 02 §5;omitempty
	// —— driver 未启用时不下发,前端不渲染「本月驱动」行)。
	Driver *DriverSnapshot `json:"driver,omitempty"`
}

// Snapshot 聚合快照(契约 03 §4.4;锁内计算,纯函数视图)。
// MedianIncome 抽样 ≤4096 人取中位(等距抽样,不耗 rng → 任意时刻调用
// 不影响演化确定性)。
func (b *Backdrop) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.residents)
	s := Snapshot{
		ResidentCount: n,
		Districts:     make([]DistrictPop, districtCount),
	}
	if n == 0 {
		return s
	}
	var employed, stressed, ageSum int
	var savingsTotal float64
	distCount := make([]int, districtCount)
	stride := n / snapshotSampleCap
	if stride < 1 {
		stride = 1
	}
	sample := make([]float64, 0, n/stride+1)
	for i := range b.residents {
		r := &b.residents[i]
		if r.flags&flagEmployed != 0 {
			employed++
		}
		if r.flags&flagStressed != 0 {
			stressed++
		}
		ageSum += int(r.age)
		savingsTotal += r.savings
		if int(r.district) < districtCount {
			distCount[r.district]++
		}
		if i%stride == 0 {
			sample = append(sample, float64(r.income))
		}
	}
	s.EmploymentRate = float64(employed) / float64(n)
	s.StressedRate = float64(stressed) / float64(n)
	s.AvgAge = float64(ageSum) / float64(n)
	s.TotalSavings = savingsTotal
	sort.Float64s(sample)
	if len(sample) > 0 {
		s.MedianIncome = sample[len(sample)/2]
	}
	for i := 0; i < districtCount; i++ {
		s.Districts[i] = DistrictPop{ID: i, Name: districtNames[i], Population: distCount[i]}
	}
	if len(b.voices) > 0 {
		s.Voices = append([]VoiceRecord(nil), b.voices...)
	}
	// 2026-09-21 §档案锚定(契约 §4):锚定流水线被启动过(非 idle)或已有
	// 进度时随快照下发;纯合成房(从未启动)不下发该块。
	if b.profStatus != ProfIdle || b.profTotal > 0 {
		pp := b.profileProgressLocked()
		s.Profiles = &pp
	}
	// 2026-09-22 §17(契约 02 §5):驱动层启用时随快照下发 driver 块
	// (锁序 Backdrop.mu → driver.mu;driver 侧绝不反向嵌套)。
	if b.driver != nil {
		ds := b.driver.Snapshot()
		s.Driver = &ds
	}
	return s
}

// AppendVoice 把一条城市之声写入环形缓冲(最近 voicesRingCap 条)。
func (b *Backdrop) AppendVoice(vr VoiceRecord) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.voices = append(b.voices, vr)
	if len(b.voices) > voicesRingCap {
		b.voices = append([]VoiceRecord(nil), b.voices[len(b.voices)-voicesRingCap:]...)
	}
}

// ResidentCount 返回背景居民总数。
func (b *Backdrop) ResidentCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.residents)
}

// PickVoiceCandidates 抽本月城市之声候选(契约 03 §5:stressed/失业优先,
// 其余随机;每名居民每月至多一次)。立即置 voiced 位 —— LLM 失败的候选本
// 月不再补抽(丢弃本条,下月再抽)。
func (b *Backdrop) PickVoiceCandidates(count int) []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.residents)
	if count <= 0 || n == 0 {
		return nil
	}
	if count > n {
		count = n
	}
	prio := make([]int, 0, n)
	rest := make([]int, 0, n)
	for i := range b.residents {
		r := &b.residents[i]
		if r.flags&flagVoiced != 0 {
			continue
		}
		if r.flags&flagStressed != 0 || r.flags&flagEmployed == 0 {
			prio = append(prio, i)
		} else {
			rest = append(rest, i)
		}
	}
	// 内部 voiceRng 洗牌(与经济演化流分离)。
	b.voiceRng.Shuffle(len(prio), func(i, j int) { prio[i], prio[j] = prio[j], prio[i] })
	b.voiceRng.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	out := make([]int, 0, count)
	for _, idx := range prio {
		if len(out) >= count {
			break
		}
		out = append(out, idx)
	}
	for _, idx := range rest {
		if len(out) >= count {
			break
		}
		out = append(out, idx)
	}
	for _, idx := range out {
		b.residents[idx].flags |= flagVoiced
	}
	return out
}

// residentBrief 是单居民的可观测摘要(城市之声 prompt 用)。
type residentBrief struct {
	DomainName   string
	DistrictName string
	Employed     bool
	Stressed     bool
	SavingsCNY   float64
	MonthsRunway float64 // 储蓄可支撑月数(savings/expense)

	// ── 2026-09-21 §档案锚定(契约 §6):锚定后增补的真实档案字段。
	// 未锚定居民全零值(CardID=="" 即 voice.go 的「未锚定」判定信号),
	// 城市之声回退代号语义,零行为变化。
	CardID      string
	Name        string
	Occupation  string
	Personality string
	OpeningHook string
	Goal        string
	Age         int
}

// briefLocked 锁内取单居民摘要。
func (b *Backdrop) briefLocked(idx int) (residentBrief, bool) {
	if idx < 0 || idx >= len(b.residents) {
		return residentBrief{}, false
	}
	r := &b.residents[idx]
	br := residentBrief{
		Employed:     r.flags&flagEmployed != 0,
		Stressed:     r.flags&flagStressed != 0,
		SavingsCNY:   r.savings,
		MonthsRunway: 0,
	}
	if int(r.domain) < len(b.calib.Domains) {
		br.DomainName = b.calib.Domains[r.domain].Name
	} else {
		br.DomainName = "未知行业"
	}
	if int(r.district) < districtCount {
		br.DistrictName = districtNames[r.district]
	}
	if r.expense > 0 {
		br.MonthsRunway = float64(r.savings) / float64(r.expense)
	}
	// 档案锚定字段(前 profAnchored 个居民已锚定;同一把锁内直读 profiles,
	// 城市之声 prompt 由此拿到真实姓名/职业/人格)。
	if idx < b.profAnchored && idx < len(b.profiles) {
		p := &b.profiles[idx]
		br.CardID = p.CardID
		br.Name = p.Name
		br.Occupation = p.Occupation
		br.Personality = p.Personality
		br.OpeningHook = p.OpeningHook
		br.Goal = p.Goal
		br.Age = p.Age
	}
	return br, true
}

// codenameLocked 居民代号 `<域字母><序号>·<城区>`(契约 03 §5:未锚定的
// 背景居民无真实姓名卡,用代号;已锚定居民由 voice.go 改用档案真实姓名,
// 代号仅作回退)。
func (b *Backdrop) codenameLocked(idx int) string {
	letter := byte('U')
	if idx >= 0 && idx < len(b.residents) {
		if d := b.residents[idx].domain; int(d) < len(b.calib.Domains) {
			letter = l1DomainNames[d][0]
		}
	}
	district := ""
	if idx >= 0 && idx < len(b.residents) && int(b.residents[idx].district) < districtCount {
		district = districtNames[b.residents[idx].district]
	}
	return string(letter) + itoa(idx) + "·" + district
}

// VoiceBrief/Codename 导出锁包装(voice.go 消费)。
func (b *Backdrop) voiceBrief(idx int) (residentBrief, string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	br, ok := b.briefLocked(idx)
	if !ok {
		return residentBrief{}, "", false
	}
	return br, b.codenameLocked(idx), true
}

// ── 2026-09-22 §17-CityHuman 全民驱动(契约 02 §2/§4)──

// SetDriver 登记驱动层(room startCityLocked 装配;nil = 关闭)。Snapshot
// 经此透出 driver 块;driver 关闭时保持 nil → omitempty 不下发。
func (b *Backdrop) SetDriver(d *ResidentDriver) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.driver = d
}

// PickDriverCandidates 返回本月经线路池驱动的居民下标(契约 02 §2.2)。
//
// 规则:**cursor 全域轮转**保证跨月公平(N=10万、预算 8/月 → 约 416 个月
// 覆盖全员一圈)—— 本月窗口 = 从 cursor 起顺时针 count 个下标
// (count = min(n, len(residents)));窗口内 **stressed/失业居民优先**
// (与 PickVoiceCandidates 同权重,排前先服务),其余随后。窗口内天然
// 同月不重复,并置 voiced 位(与城市之声共享月度去重语义)。选完后
// cursor += count(原子推进;LLM 失败不回退,漏抽者下圈补上)。
//
// 立即置 voiced 位 —— LLM 失败的候选本月不再补抽(丢弃本条,下月轮转)。
func (b *Backdrop) PickDriverCandidates(n int, cursor *uint64) []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	N := len(b.residents)
	if n <= 0 || N == 0 || cursor == nil {
		return nil
	}
	count := n
	if count > N {
		count = N
	}
	start := int(atomic.LoadUint64(cursor) % uint64(N))
	prio := make([]int, 0, count)
	rest := make([]int, 0, count)
	for k := 0; k < count; k++ {
		idx := (start + k) % N
		r := &b.residents[idx]
		if r.flags&flagVoiced != 0 {
			continue // 同月不重复(含城市之声已发声者)
		}
		if r.flags&flagStressed != 0 || r.flags&flagEmployed == 0 {
			prio = append(prio, idx)
		} else {
			rest = append(rest, idx)
		}
	}
	out := append(prio, rest...)
	for _, idx := range out {
		b.residents[idx].flags |= flagVoiced
	}
	atomic.AddUint64(cursor, uint64(count))
	return out
}

// intentCodes set_intent 意图名 → 编码(契约 02 §3.3 枚举)。
var intentCodes = map[string]uint8{
	"job_seeking": intentJobSeeking,
	"frugal":      intentFrugal,
	"consume":     intentConsume,
	"socialize":   intentSocialize,
	"move_out":    intentMoveOut,
}

// ApplyIntent 写入居民本月意图(驱动层 set_intent 消费;锁内短临界区,
// 契约 02 §4)。非法意图 / 越界下标静默忽略;move_out 无有效目标城区时
// 整条忽略(契约 §4「无校验失败则忽略」)。意图在下个 TickMonth 消费后清除。
func (b *Backdrop) ApplyIntent(idx int, intent, targetDistrict string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if idx < 0 || idx >= len(b.residents) {
		return
	}
	code, ok := intentCodes[intent]
	if !ok || code == intentNone {
		return
	}
	r := &b.residents[idx]
	if code == intentMoveOut {
		di := DistrictIndexOf(targetDistrict)
		if di < 0 {
			return
		}
		r.moveTarget = uint8(di)
	}
	r.intent = code
}

// DriverBrief 是驱动层单居民月度快照(锁内取;driver.go runOne 消费)。
type DriverBrief struct {
	residentBrief // 档案人格 + 基础状态(锚定判定信号 CardID 同 voice)

	Codename    string             // 代号(未锚定时 VoiceRecord.Name 回退)
	Income      float64            // 月收入(元)
	Expense     float64            // 月支出(元)
	Marital     string             // 婚姻(锚定档案;未锚定空)
	HealthGrade string             // 健康档(锚定档案;未锚定空)
	DistrictID  string             // 城区 id(氛围查询;未命中空)
	Neighbors   []DistrictNeighbor // 同城区 ≤3 名(排除本人;锁外补取)
}

// anchored 已锚定判定(与 voice.go 同信号:CardID 非空)。
func (d *DriverBrief) anchored() bool { return d.CardID != "" }

// displayName VoiceRecord.Name:已锚定用真实姓名(空名回退代号);未锚定
// 用代号。
func (d *DriverBrief) displayName() string {
	if d.anchored() && d.Name != "" {
		return d.Name
	}
	return d.Codename
}

// MaritalOr / HealthGradeOr 未锚定缺省(契约 §3.1 模板占位)。
func (d *DriverBrief) MaritalOr(fallback string) string {
	if d.Marital == "" {
		return fallback
	}
	return d.Marital
}
func (d *DriverBrief) HealthGradeOr(fallback string) string {
	if d.HealthGrade == "" {
		return fallback
	}
	return d.HealthGrade
}

// DriverBrief 锁内取驱动层快照:档案人格 + 月度状态 + 婚姻/健康档 +
// 城区 id;邻居在锁外补取(SampleDistrictNeighbors 自持锁,绝不嵌套)。
func (b *Backdrop) DriverBrief(idx int) (DriverBrief, bool) {
	b.mu.Lock()
	if idx < 0 || idx >= len(b.residents) {
		b.mu.Unlock()
		return DriverBrief{}, false
	}
	r := &b.residents[idx]
	br, _ := b.briefLocked(idx)
	db := DriverBrief{
		residentBrief: br,
		Codename:      b.codenameLocked(idx),
		Income:        float64(r.income),
		Expense:       float64(r.expense),
	}
	distIdx := int(r.district)
	if idx < b.profAnchored && idx < len(b.profiles) {
		db.Marital = b.profiles[idx].Marital
		db.HealthGrade = b.profiles[idx].HealthGrade
	}
	b.mu.Unlock()
	if distIdx >= 0 && distIdx < districtCount {
		if ids := professionDistrictIDs(); distIdx < len(ids) {
			db.DistrictID = ids[distIdx]
		}
		db.Neighbors = b.sampleNeighborsExcluding(distIdx, idx, driverNeighborCap)
	}
	return db, true
}

// professionDistrictIDs 城区 id 表薄包装(profession.DistrictIDs 每次拷贝,
// 邻居/氛围查询低频,开销可忽略)。
func professionDistrictIDs() []string { return profession.DistrictIDs() }

// clampF / pos / itoa 小工具。
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func pos(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [12]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		buf[p] = '-'
	}
	return string(buf[p:])
}
