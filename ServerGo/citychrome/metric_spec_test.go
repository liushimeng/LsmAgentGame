// Package citychrome — metric_spec_test.go: 批次89 口径卡注册表单测
// (设计文档 §9.1 第 1 条:口径卡副本语义 + 覆盖度 + 命中查找)。
package citychrome

import (
	"net/url"
	"strings"
	"testing"
)

// TestMetricSpecs_CopySemantics 口径卡目录返回**副本**:改动返回值(含
// Keywords 切片)**不污染**包级目录(对齐 DashboardDimensions 副本纪律)。
func TestMetricSpecs_CopySemantics(t *testing.T) {
	first := MetricSpecs()
	if len(first) == 0 {
		t.Fatal("MetricSpecs() empty")
	}
	before := MetricSpecs()
	// 改动返回值:字段 + Keywords 切片内容。
	first[0].Indicator = "被污染的指标"
	first[0].Unit = "被污染的单位"
	if len(first[0].Keywords) > 0 {
		first[0].Keywords[0] = "被污染的关键词"
	}
	second := MetricSpecs()
	if second[0].Indicator == "被污染的指标" || second[0].Unit == "被污染的单位" {
		t.Errorf("package registry polluted: %+v", second[0])
	}
	if len(second[0].Keywords) > 0 && second[0].Keywords[0] == "被污染的关键词" {
		t.Errorf("package registry keywords polluted: %+v", second[0].Keywords)
	}
	if before[0].Indicator != second[0].Indicator {
		t.Errorf("registry changed between calls: %q vs %q", before[0].Indicator, second[0].Indicator)
	}
}

// TestMetricSpecs_Coverage 覆盖 8 个类目;每条 8 字段齐全;**按类目精确计数**
// (批次89 活体验收 D1/D2 返工后,口径卡数 = 采集器实际能产出的标的集合,
// 不再是「每类目 2~4 条」的宽松区间)。
func TestMetricSpecs_Coverage(t *testing.T) {
	want := map[string]int{
		CategoryAShare:  8, // sh000001 sz399001 sz399006 sh000300 sh000905 sz399852 sh000016 sh000688
		CategoryHK:      3, // hkHSI hkHSTECH hkHSCEI
		CategoryUS:      3, // usDJI usIXIC usINX
		CategoryMetal:   2, // hf_GC hf_SI
		CategoryFutures: 3, // hf_CL hf_GC hf_SI(伦铜/伦镍刻意不登记,见已知缺口)
		CategoryCrypto:  3, // BTC ETH DOGE
		CategoryMacro:   4, // LPR_1Y CPI_YOY GDP_YOY PMI_MFG(FX_RESERVE 刻意不登记)
		CategoryNews:    1, // 财经快讯
	}
	specs := MetricSpecs()
	if len(specs) < 20 {
		t.Errorf("metric specs = %d, want >= 20", len(specs))
	}
	perCat := map[string]int{}
	ids := map[string]bool{}
	for _, s := range specs {
		perCat[s.Category]++
		if ids[s.ID] {
			t.Errorf("duplicate metric spec id %q", s.ID)
		}
		ids[s.ID] = true
		if s.Indicator == "" || s.Unit == "" || s.Caliber == "" || s.Formula == "" ||
			s.Source == "" || s.SourceURL == "" || s.Refresh == "" {
			t.Errorf("metric spec %q has empty field: %+v", s.ID, s)
		}
		if !strings.HasPrefix(s.SourceURL, "http") {
			t.Errorf("metric spec %q source_url not a link: %q", s.ID, s.SourceURL)
		}
	}
	for cat, n := range want {
		if perCat[cat] != n {
			t.Errorf("category %q has %d specs, want %d", cat, perCat[cat], n)
		}
	}
}

// TestMetricSpecs_NoOverBroadKeywords **禁止过宽的裸词关键词**(批次89 活体
// 验收 D1:裸词 "上证" ⊂ "上证50" ⇒ 上证50 错配到上证指数卡,等于给用户看
// 另一个指标的定义)。判据:同注册表内,任何一张卡的关键词**不得是**另一张
// 同类目卡 Indicator 的真子串。
func TestMetricSpecs_NoOverBroadKeywords(t *testing.T) {
	specs := MetricSpecs()
	for _, a := range specs {
		for _, j := range specs {
			if a.ID == j.ID || a.Category != j.Category {
				continue
			}
			for _, kw := range a.Keywords {
				kw = strings.TrimSpace(kw)
				if kw == "" || kw == j.Indicator {
					continue // 卡自身名称不算过宽。
				}
				if strings.Contains(j.Indicator, kw) && len(kw) < len(j.Indicator) {
					t.Errorf("over-broad keyword %q (card %q) ⊂ indicator %q (card %q) ⇒ 会错配",
						kw, a.ID, j.Indicator, j.ID)
				}
			}
		}
	}
}

// TestMetricSpecs_CoverCollectorSymbols **覆盖率 100% 断言**(批次89 活体
// 验收 D1/D2 根治关键):夹具**直接取自 sites.go**——
//   - a_share/hk/us/metal/futures:解析 categorySites[cat][0].URL 的 `q=`
//     参数得到站点 symbol(带市场前缀形态),并按配方换算成采集器实际写入
//     quote 的**代码形态**(sites.go 各 hint:「symbol 用代码 / HSI / DJI /
//     GC / CL」)——**两种形态都必须命中一张卡,且命中卡的 Keywords 覆盖
//     该 symbol**;
//   - crypto/macro:首选源 URL 无 symbol 参数(SPA/数据中心页),改用
//     sites.go 确定性提取配方点名的标的(BTC/ETH;CPI_YOY/GDP_YOY/PMI_MFG/
//     LPR_1Y)。
//
// 已知缺口(刻意未登记,理由见 metric_spec.go 文件头):futures 的伦铜
// (hf_HG)/伦镍(hf_NID)与 macro 的 FX_RESERVE —— 同时断言它们**确实**
// 未命中,若将来补卡须同步更新本清单。
func TestMetricSpecs_CoverCollectorSymbols(t *testing.T) {
	knownGaps := map[string]map[string]bool{
		CategoryFutures: {"hf_HG": true, "NID": true, "hf_NID": true, "HG": true}, // 伦铜/伦镍(无「美元/吨」单位)
		CategoryMacro:   {"FX_RESERVE": true},                                     // 外汇储备(无「万亿美元」单位)
	}
	for _, cat := range MissionCategories {
		for _, sym := range collectorURLSymbols(t, cat) {
			if knownGaps[cat][sym] {
				continue
			}
			// ① URL symbol(带市场前缀)必须命中。
			spec, ok := LookupMetricSpec(cat, sym, "")
			if !ok {
				t.Errorf("%s: 站点 symbol %q 未命中任何口径卡(覆盖不足)", cat, sym)
				continue
			}
			if !specHasKeyword(spec, sym) {
				t.Errorf("%s: %q 命中的卡 %q 的 Keywords 未覆盖该 symbol", cat, sym, spec.ID)
			}
			// ② 采集器实际写入 quote 的代码形态也必须命中(生产路径)。
			code := emittedSymbol(t, sym)
			spec2, ok := LookupMetricSpec(cat, code, "")
			if !ok {
				t.Errorf("%s: 采集器代码形态 %q 未命中任何口径卡(生产路径覆盖不足)", cat, code)
				continue
			}
			if !specHasKeyword(spec2, code) {
				t.Errorf("%s: %q 命中的卡 %q 的 Keywords 未覆盖该代码形态", cat, code, spec2.ID)
			}
		}
	}
	// 缺口清单自身须成立(补卡后须同步更新)。
	for cat, gaps := range knownGaps {
		for gap := range gaps {
			if _, ok := LookupMetricSpec(cat, gap, ""); ok {
				t.Errorf("%s: %q 已登记口径卡,请把它从「已知缺口」清单移除并更新注释", cat, gap)
			}
		}
	}
	// crypto / macro:配方点名的标的(首选源 URL 无 symbol 参数)。
	for _, c := range []struct{ cat, sym string }{
		{CategoryCrypto, "BTC"}, {CategoryCrypto, "ETH"},
		{CategoryMacro, "CPI_YOY"}, {CategoryMacro, "GDP_YOY"},
		{CategoryMacro, "PMI_MFG"}, {CategoryMacro, "LPR_1Y"},
	} {
		if _, ok := LookupMetricSpec(c.cat, c.sym, ""); !ok {
			t.Errorf("%s: 配方标的 %q 未命中任何口径卡", c.cat, c.sym)
		}
	}
}

// collectorURLSymbols 解析类目首选源 URL 的 q= 参数,得到站点 symbol 清单
// (与 sites.go 自动同步:URL 增删标的 ⇒ 本测试随之红/绿)。
func collectorURLSymbols(t *testing.T, cat string) []string {
	t.Helper()
	sites, ok := categorySites[cat]
	if !ok || len(sites) == 0 {
		return nil
	}
	u, err := url.Parse(sites[0].URL)
	if err != nil {
		t.Fatalf("parse %s first source url: %v", cat, err)
	}
	q := u.Query().Get("q")
	if q == "" {
		return nil // crypto/macro/news 的首选源无 symbol 参数。
	}
	var out []string
	for _, s := range strings.Split(q, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// emittedSymbol 把 URL symbol(带市场前缀)换算成采集器实际写入 quote 的
// 代码形态(sites.go 各类目 hint 的「symbol 用代码」约定):
// sh/sz/bj → 去前 2 字符;hk/us → 去前 2 字符;hf_ → 去前 3 字符。
func emittedSymbol(t *testing.T, urlSymbol string) string {
	t.Helper()
	switch {
	case strings.HasPrefix(urlSymbol, "hf_"):
		return urlSymbol[len("hf_"):]
	case strings.HasPrefix(urlSymbol, "sh"), strings.HasPrefix(urlSymbol, "sz"),
		strings.HasPrefix(urlSymbol, "bj"), strings.HasPrefix(urlSymbol, "hk"),
		strings.HasPrefix(urlSymbol, "us"):
		return urlSymbol[2:]
	}
	t.Fatalf("unexpected url symbol %q", urlSymbol)
	return urlSymbol
}

// TestDefaultUnit 类目兜底单位(设计文档 §3.3;批次89 D2 修订:a_share→point)。
func TestDefaultUnit(t *testing.T) {
	want := map[string]string{
		CategoryAShare:  UnitPoint, // D2 修复:该类目采集的全是指数,个股不在清单内
		CategoryHK:      UnitHKD,
		CategoryUS:      UnitUSD,
		CategoryMetal:   UnitUSDPerOz,
		CategoryFutures: UnitUSDPerBbl,
		CategoryCrypto:  UnitUSD,
		CategoryMacro:   UnitText,
		CategoryNews:    UnitText,
	}
	for cat, u := range want {
		if got := DefaultUnit(cat); got != u {
			t.Errorf("DefaultUnit(%s) = %q, want %q", cat, got, u)
		}
	}
}

// TestLookupMetricSpec_SSE50NotBoundToSHIndex D1 回归:上证50 必须绑定到
// **自己的**口径卡,绝不允许错配到「上证指数」卡(等于给用户看另一个指标
// 的定义);且裸词 "上证" 不得命中 "上证50"。
func TestLookupMetricSpec_SSE50NotBoundToSHIndex(t *testing.T) {
	spec, ok := LookupMetricSpec(CategoryAShare, "000016", "上证50")
	if !ok {
		t.Fatal("LookupMetricSpec(a_share, 000016, 上证50) 必须命中(采集清单内标的)")
	}
	if spec.ID == "a_share.index.sh" {
		t.Fatalf("上证50 错配到上证指数卡(a_share.index.sh)—— D1 复发")
	}
	if spec.Indicator != "上证50" {
		t.Fatalf("上证50 命中的卡 Indicator = %q, want 上证50", spec.Indicator)
	}
	if spec.SourceURL != "https://qt.gtimg.cn/q=sh000016" {
		t.Errorf("上证50 的 source_url = %q, want sh000016 的源", spec.SourceURL)
	}
	// 反向:上证指数卡不得被 "上证50" 之名带走 —— 裸词 "上证" 不进注册表。
	for _, s := range MetricSpecs() {
		for _, kw := range s.Keywords {
			if kw == "上证" || kw == "深证" || kw == "创业板" {
				t.Errorf("卡 %q 仍含过宽裸词 %q(D1 根因)", s.ID, kw)
			}
		}
	}
	// 其余三个此前无卡的指数也必须有卡(D2:单位不得回退成人民币)。
	for _, c := range []struct{ sym, name string }{
		{"000905", "中证500"}, {"399852", "中证1000"}, {"000688", "科创50"},
	} {
		s, ok := LookupMetricSpec(CategoryAShare, c.sym, c.name)
		if !ok {
			t.Errorf("%s %s 未命中口径卡", c.sym, c.name)
			continue
		}
		if s.Unit != UnitPoint {
			t.Errorf("%s unit = %q, want point", c.name, s.Unit)
		}
	}
}

// TestLookupMetricSpec_Hit 按 symbol / name 命中口径卡(大小写不敏感)。
func TestLookupMetricSpec_Hit(t *testing.T) {
	cases := []struct {
		cat, symbol, name, wantUnit string
	}{
		{CategoryAShare, "000001", "上证指数", UnitPoint},
		{CategoryAShare, "sh000300", "沪深300", UnitPoint},
		{CategoryHK, "HSI", "恒生指数", UnitPoint},
		{CategoryUS, "IXIC", "纳斯达克", UnitPoint},
		{CategoryMetal, "GC", "纽约黄金", UnitUSDPerOz},
		{CategoryFutures, "CL", "纽约原油", UnitUSDPerBbl},
		{CategoryCrypto, "btc", "比特币", UnitUSD},
		{CategoryMacro, "CPI_YOY", "CPI同比", UnitPct},
	}
	for _, c := range cases {
		spec, ok := LookupMetricSpec(c.cat, c.symbol, c.name)
		if !ok {
			t.Errorf("LookupMetricSpec(%s,%s,%s) not hit", c.cat, c.symbol, c.name)
			continue
		}
		if spec.Unit != c.wantUnit {
			t.Errorf("LookupMetricSpec(%s,%s) unit = %q, want %q", c.cat, c.symbol, spec.Unit, c.wantUnit)
		}
		if spec.Category != c.cat {
			t.Errorf("LookupMetricSpec(%s) category = %q", c.cat, spec.Category)
		}
	}
	// 未命中 ⇒ (零值, false)。
	if spec, ok := LookupMetricSpec(CategoryAShare, "999999", "查无此股"); ok {
		t.Errorf("unexpected hit: %+v", spec)
	}
}

// TestLookupMetricSpec_CategoryScoped 命中限定在指定类目内(同名指标不串类目)。
func TestLookupMetricSpec_CategoryScoped(t *testing.T) {
	// GC 同时出现在 metal 与 futures 口径卡中(单位不同)。
	m, ok := LookupMetricSpec(CategoryMetal, "GC", "纽约黄金")
	if !ok || m.Unit != UnitUSDPerOz {
		t.Errorf("metal GC = %+v (ok=%v)", m, ok)
	}
	f, ok := LookupMetricSpec(CategoryFutures, "CL", "纽约原油")
	if !ok || f.Unit != UnitUSDPerBbl {
		t.Errorf("futures CL = %+v (ok=%v)", f, ok)
	}
}

// TestAnomalyThresholdPct 异常阈值表(设计文档 §4.2;单一事实来源)。
func TestAnomalyThresholdPct(t *testing.T) {
	want := map[string]float64{
		CategoryAShare:  3.0,
		CategoryHK:      4.0,
		CategoryUS:      3.5,
		CategoryMetal:   2.0,
		CategoryFutures: 3.0,
		CategoryCrypto:  5.0,
		CategoryMacro:   1.0,
	}
	for cat, v := range want {
		if got := AnomalyThresholdPct(cat); got != v {
			t.Errorf("AnomalyThresholdPct(%s) = %v, want %v", cat, got, v)
		}
	}
}

// TestSourceAuthority 采集源权威度静态表(设计文档 §4.3)。
func TestSourceAuthority(t *testing.T) {
	cases := []struct {
		source string
		want   float64
	}{
		{"腾讯行情", 1.0},
		{"东方财富", 1.0},
		{"新浪财经", 1.0},
		{"上海黄金交易所", 1.0},
		{"纳斯达克", 1.0},
		{"路透", 1.0},
		{"bloomberg", 1.0},
		{"财联社·电报", 0.9},
		{"华尔街见闻·快讯", 0.9},
		{"第一财经", 0.9},
		{"同花顺行情中心", 0.9},
		{"CoinGecko", 0.75},
		{"金投网", 0.75},
		{"", 0.6},
		{"某不知名站点", 0.6},
	}
	for _, c := range cases {
		if got := sourceAuthority(c.source); got != c.want {
			t.Errorf("sourceAuthority(%q) = %v, want %v", c.source, got, c.want)
		}
	}
}
