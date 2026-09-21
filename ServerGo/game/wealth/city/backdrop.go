// Package city — backdrop.go: 城市背景居民(紧凑数组 + 月度演化 + 聚合快照)。
//
// 契约: 虚拟城市-大规模城市居民背景模拟设计-v1.md §4(2026-09-21)。
//
// 背景居民是数据,不是 goroutine:每城 1..100,000+ 名居民以 ≈20B(预算 ≤40B)
// 的紧凑结构存储,数值规则逐月演化(TickMonth);LLM 深认知只给焦点座位(≤12,
// 全量引擎不变)与「城市之声」抽样居民(voice.go)。性能预算:100K 初始化
// <200ms、月度 tick <100ms、常驻内存增量 <32MB。
package city

import (
	"math"
	"math/rand"
	"sort"
	"sync"
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

// resident 单个背景居民(紧凑;字段顺序保证对齐后 ≈20B,预算 ≤40B)。
// 未导出:外部只经 Backdrop 方法 / Snapshot 观测。
type resident struct {
	income   float32 // 月收入(元;失业置 0)
	expense  float32 // 月支出(元)
	savings  float64 // 累计储蓄
	domain   uint8   // L1 域索引 0..25(255=未知)
	district uint8   // 城区 0..7
	age      uint8   // 岁(每 12 个 tick +1)
	flags    uint8   // bit0=employed bit1=stressed bit2=voiced(本月已发声)
}

const (
	flagEmployed uint8 = 1 << iota
	flagStressed
	flagVoiced
	domainUnknown uint8 = 255
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
func (b *Backdrop) TickMonth(cpi float64, rng *rand.Rand) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.month++
	ageTick := b.month%12 == 0
	for i := range b.residents {
		r := &b.residents[i]
		r.flags &^= flagVoiced
		if r.flags&flagEmployed != 0 {
			r.savings += float64(r.income) - float64(r.expense)
			r.expense = float32(float64(r.expense) * (1 + cpi))
			if rng.Float64() < unemploymentMonthly {
				r.flags &^= flagEmployed
				r.income = 0
			}
		} else if rng.Float64() < reemploymentMonthly {
			r.flags |= flagEmployed
			r.income = float32(b.domainIncomeMeanLocked(r.domain) * (0.8 + 0.3*rng.Float64()))
		}
		if r.savings < stressExpenseMonths*float64(r.expense) {
			r.flags |= flagStressed
		} else {
			r.flags &^= flagStressed
		}
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

// districtNames 8 城区展示名(顺序与 profession.DistrictIDs /
// game/wealth.DistrictDefs 对齐:finance/tech/industry/oldtown/commerce/
// residential/suburb/riverside;city 不 import wealth,此处独立维护,
// 与 loader 的 districtIDs 同款同步策略)。
var districtNames = [districtCount]string{
	"金融CBD", "科技园", "工业区", "老城区", "商业中心", "居住区", "郊区", "滨河新区",
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
	return br, true
}

// codenameLocked 居民代号 `<域字母><序号>·<城区>`(契约 03 §5:背景居民
// 不解析真实姓名卡,真实姓名只属于焦点座位)。
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
