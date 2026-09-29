// Package city — crowd.go: 户外/户内人流分布 + 人物原型归类 + crowd manifest
// (批次34 §5.1/§6.1/§6.4,2026-09-29)。
//
// 契约: lag_docs/虚拟城市/已实现/34-人物模型与人流分布与视觉操控/01-方案设计.md
// §5.1(外观映射表)/ §6.1(非线性公式)/ §6.4(跨层 manifest)。
// 本文件是纯视觉/人流投影层:不改经济模型、不改 Agent 决策语义,所有函数为
// 纯函数(选择路径禁 rng/禁随机,确定性硬约束 §6.2)。
package city

import (
	"math"
	"strings"
)

// ─────────────────── §6.1 户外/户内非线性公式 ───────────────────

// 户外人数公式参数(§6.1)。
const (
	crowdN0    = 100.0 // 「人少就都上街」拐点
	crowdGamma = 0.45  // 次线性指数(0<γ<1 ⇒ 越多人、人均上街概率越低)
	// crowdCapHigh/crowdCapLow 全城行人画质档上限。
	// **沿用批次 28 的 110/60,本批次不抬** —— 行人不实例化(每人一个 GLB
	// clone + AnimationMixer),抬到 160 约多 100 DC,而默认视角 DC 已超预算
	// (实测 ~2230 / 目标 1500)。110 仍严格满足用户 R6:N≤100 时 V(N)=N
	// (全上街),N>100 才触顶。低画质档由前端按画质传入 CrowdSnapshot(cap,…)。
	crowdCapHigh = 110
	crowdCapLow  = 60
	// crowdIndoorCandidates 户内换班候选条数上限(§6.4:entries ≤ CAP+16)。
	crowdIndoorCandidates = 16
)

// OutdoorCount 户外可见人数 V(N)(§6.1):
//
//	V(N) = min(CAP, N, ceil(N0 · (N/N0)^γ)), N0=100, γ=0.45
//
// 验证点:N=10/50/100 → 全部上街;N=400/10000 → 触顶 160;n≤0 或 cap≤0 → 0。
// N≤N0 时 ceil(N0·(N/N0)^γ) ≥ N(因 0<γ<1,x<1 ⇒ x^γ>x),min 规则使
// V(N)=N —— 严格满足「人少就都在外面」(用户 R6 原文)。
func OutdoorCount(n, cap int) int {
	if n <= 0 || cap <= 0 {
		return 0
	}
	theory := int(math.Ceil(crowdN0 * math.Pow(float64(n)/crowdN0, crowdGamma)))
	v := n
	if theory < v {
		v = theory
	}
	if cap < v {
		v = cap
	}
	return v
}

// ─────────────────── §5.1 财富档 / 人物原型 ───────────────────

// 财富档分位阈值(§5.1「income_monthly 与 savings_stock 各分 4 分位取较大」;
// 阈值 = 10 万人物卡池离线实测分位,2026-09-29 统计:
//
//	income_monthly  n=99,823  p25≈9,000  p50≈12,750  p75≈19,000  → 取整 9,000/13,000/19,000
//	savings_stock   n=93,377  p25≈33,000 p50≈90,000  p75≈200,000 → 33,000/90,000/200,000
const (
	wealthIncomeC0, wealthIncomeC1, wealthIncomeC2    = 9000.0, 13000.0, 19000.0
	wealthSavingsC0, wealthSavingsC1, wealthSavingsC2 = 33000.0, 90000.0, 200000.0
)

// WealthTierFor 财富档 0..3(0 低 / 1 中低 / 2 中高 / 3 高):income 与
// savings 各按 4 分位切档,**取较大者**(§5.1)。负值按 0 档处理。
func WealthTierFor(income, savings float64) int {
	it := wealthBand(income, wealthIncomeC0, wealthIncomeC1, wealthIncomeC2)
	st := wealthBand(savings, wealthSavingsC0, wealthSavingsC1, wealthSavingsC2)
	if st > it {
		return st
	}
	return it
}

// wealthBand 三切点四档(v < c0 → 0;…;≥ c2 → 3)。
func wealthBand(v, c0, c1, c2 float64) int {
	switch {
	case v < c0:
		return 0
	case v < c1:
		return 1
	case v < c2:
		return 2
	}
	return 3
}

// ArchetypeFor 由居民特征推 3D 人物原型(§5.1 表;优先级自上而下,首个命中
// 即返回;兜底 char_casual)。gender 目前不参与原型判定(仅外观色板通道),
// 参数保留以对齐 §5.1 推导入口与未来扩展。
//
// 域下标 ↔ 字母对照(已核对 city/calibration.go::l1DomainNames 与
// profession/domain.go::domainIndex,A=0):
//
//	0=A农林牧渔 1=B采矿冶金 2=C食品饮料 3=D纺织服装 4=E木材家具 5=F医药生物
//	6=G化工新材料 7=H金属通用机械 8=I电子半导体 9=J汽车交装 10=K能源电力 11=L建筑房地产
//	12=M批发零售 13=N交通物流 14=O住宿餐饮 15=P信息通信 16=Q金融保险 17=R专业服务
//	18=S科研技术 19=T教育培育 20=U医疗健康 21=V文化传媒体育 22=W公共管理 23=X社会组织
//	24=Y居民生活服务 25=Z新兴交叉
//
// 判据表:
//
//	char_elder    age>=55
//	char_student  age<=24
//	char_formal   wealth==3
//	char_business domain∈{15,16,17,18}(P/Q/R/S) && wealth>=1
//	char_worker   domain∈{1,6,7,8,9,10,11}(B/G/H/I/J/K/L)
//	char_service  domain∈{12,13,14,24}(M/N/O/Y) || employment∈{平台就业,灵活就业}
//	char_parent   age∈[30,54] && childrenCount>0
//	char_casual   兜底
func ArchetypeFor(age int, domain int, wealth int, gender, employment string, childrenCount int) string {
	switch {
	case age >= 55:
		return "char_elder"
	case age <= 24:
		return "char_student"
	case wealth == 3:
		return "char_formal"
	case (domain == 15 || domain == 16 || domain == 17 || domain == 18) && wealth >= 1:
		return "char_business"
	case domain == 1 || domain == 6 || domain == 7 || domain == 8 ||
		domain == 9 || domain == 10 || domain == 11:
		return "char_worker"
	case domain == 12 || domain == 13 || domain == 14 || domain == 24 ||
		strings.Contains(employment, "平台就业") || strings.Contains(employment, "灵活就业"):
		return "char_service"
	case age >= 30 && age <= 54 && childrenCount > 0:
		return "char_parent"
	}
	return "char_casual"
}

// ─────────────────── §6.4 crowd manifest ───────────────────

// CrowdEntry 名上街居民的外观投影(批次34 §6.4;json tag 与设计文档逐字一致)。
type CrowdEntry struct {
	Index     int    `json:"index"`     // backdrop.residents 下标(可回溯)
	CardID    string `json:"card_id"`   // 人物卡编号(可点开 ResidentProfileDrawer)
	Name      string `json:"name"`      // 化名
	Archetype string `json:"archetype"` // char_business|char_casual|...(§5.1 表)
	Gender    string `json:"gender"`    // m|f|u
	Age       int    `json:"age"`
	Domain    int    `json:"domain"`   // 0..25 L1 行业域(-1=未知)
	District  int    `json:"district"` // 0..31
	Wealth    int    `json:"wealth"`   // 0..3
	Health    string `json:"health"`   // A|B|C
	Indoor    bool   `json:"indoor"`   // true = 在建筑内(不渲染,换班候选)
}

// CrowdSnapshot 人流 manifest(批次34 §6.4;随 game.state.city.crowd 下发)。
type CrowdSnapshot struct {
	Outdoor int          `json:"outdoor"` // V(N)
	Indoor  int          `json:"indoor"`  // N − V(N)
	Churn   int          `json:"churn"`   // 换班代数(前端据此重算 path)
	Entries []CrowdEntry `json:"entries"` // ≤ CAP_q 条;indoor=true 的条目仅占
	// 少量名额作为「换班候选」,不渲染
}

// CrowdSnapshot 名上街居民 + 换班候选的外观投影(§6.4 产出方)。
// **锁内纯函数视图**(复用 b.mu,与 Snapshot() 同锁同源);§92a:本方法自取
// b.mu,内部只走 *Locked 变体,绝不在锁内调用其它加锁公开方法。
// cap = 画质档行人上限(高 160 / 低 90);churn = 换班代数(同代恒同视图)。
func (b *Backdrop) CrowdSnapshot(cap, churn int) CrowdSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.crowdSnapshotLocked(cap, churn)
}

// crowdSnapshotLocked CrowdSnapshot 的锁内变体(调用方须持 b.mu;§92a)。
// 选择确定性(§6.2):户外/换班候选集合 = crowdScore(i, churn) 升序前
// (V + ≤16) 个下标 —— FNV-1a + xorshift64* 从 (下标, churn 代数) 派生,
// 禁 rng、不占演化流;同 (n, cap, churn) 恒同视图。
//
// 履历字段来源:CardID/Name/Health/Gender 取 profile 层(未锚定 → 空串/"u");
// Archetype 用 resident 合成特征推(age/domain/wealth)。resident 层不携带
// employment 形态与 children_count(§5.1 的这两路判据留给持完整卡数据的调用
// 方),故此处传 ""/0。
func (b *Backdrop) crowdSnapshotLocked(cap, churn int) CrowdSnapshot {
	n := len(b.residents)
	outdoor := OutdoorCount(n, cap)
	indoorTotal := n - outdoor
	indoorCands := crowdIndoorCandidates
	if indoorCands > indoorTotal {
		indoorCands = indoorTotal
	}
	cs := CrowdSnapshot{
		Outdoor: outdoor,
		Indoor:  indoorTotal,
		Churn:   churn,
		Entries: make([]CrowdEntry, 0, outdoor+indoorCands),
	}
	if n == 0 {
		return cs
	}
	picks := crowdSelect(n, outdoor+indoorCands, churn)
	for k, p := range picks {
		r := &b.residents[p.idx]
		e := CrowdEntry{
			Index:    p.idx,
			Gender:   "u",
			Age:      int(r.age),
			Domain:   -1,
			District: int(r.district),
			Wealth:   WealthTierFor(float64(r.income), r.savings),
			Indoor:   k >= outdoor,
		}
		if r.domain != domainUnknown {
			e.Domain = int(r.domain)
		}
		// 就业形态 / 子女数:批次34 §5.1 的 char_service / char_parent 两路判据
		// 需要它们;未锚定居民没有 profile ⇒ 传 ""/0,该两路自然不命中。
		var employment string
		var children int
		if p.idx < b.profAnchored && p.idx < len(b.profiles) {
			pf := &b.profiles[p.idx]
			e.CardID, e.Name, e.Health = pf.CardID, pf.Name, pf.HealthGrade
			e.Gender = genderOrU(pf.Gender)
			employment = pf.Employment
			children = pf.ChildrenCount
		}
		e.Archetype = ArchetypeFor(e.Age, e.Domain, e.Wealth, e.Gender, employment, children)
		cs.Entries = append(cs.Entries, e)
	}
	return cs
}

// crowdPick 一次确定性选取结果(分数 + 居民下标)。
type crowdPick struct {
	score uint64
	idx   int
}

// crowdScore (居民下标, churn 代数) → 确定性 64 位分数:FNV-1a(offset
// 14695981039346656037 / prime 1099511628211,把两整数按小端字节喂入)+
// xorshift64* 终混。同 (i, churn) 恒同分 —— 选择完全确定,禁随机(§6.2)。
func crowdScore(i, churn int) uint64 {
	h := uint64(14695981039346656037)
	for _, v := range [2]uint64{uint64(i), uint64(churn)} {
		for k := 0; k < 8; k++ {
			h ^= v & 0xff
			h *= 1099511628211
			v >>= 8
		}
	}
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	h ^= h >> 31
	return h
}

// crowdLess 全序比较(分数升序;同分按下标升序破平,保证确定性)。
func crowdLess(s uint64, i int, other crowdPick) bool {
	if s != other.score {
		return s < other.score
	}
	return i < other.idx
}

// crowdSelect 返回 crowdScore 最小的 k 个下标(升序,即分数序遍历的前 k)。
// 有界有序插入 + 满时 O(1) 早拒:hash 分数均匀,期望 O(n);k ≤ CAP+16。
func crowdSelect(n, k, churn int) []crowdPick {
	if n <= 0 || k <= 0 {
		return nil
	}
	if k > n {
		k = n
	}
	buf := make([]crowdPick, 0, k)
	for i := 0; i < n; i++ {
		s := crowdScore(i, churn)
		if len(buf) == k && !crowdLess(s, i, buf[k-1]) {
			continue // 不进前 k(含同分平局:先入下标优先,遍历序即下标升序)
		}
		pos := len(buf)
		for pos > 0 && crowdLess(s, i, buf[pos-1]) {
			pos--
		}
		if len(buf) < k {
			buf = append(buf, crowdPick{})
		}
		copy(buf[pos+1:], buf[pos:])
		buf[pos] = crowdPick{score: s, idx: i}
	}
	return buf
}
