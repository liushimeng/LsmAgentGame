// Package citychrome — recipes_test.go: 确定性解析配方单测(批次78 §3.1)。
//
// 样本取自 2026-10-23 实机复测的腾讯行情真实格式:
//   - v_<前缀><代码>="1~名称~代码~当前价~昨收~…~时间~涨跌额~涨跌幅~最高~最低~…"
//     (实测下标:[1]名称 [2]代码 [3]当前价 [4]昨收 [30]时间 [31]涨跌额
//      [32]涨跌幅 [33]最高 [34]最低);
//   - v_hf_<代码>="最新价,涨跌幅,…,名称"(逗号口径:[0]价 [1]涨跌幅 [13]名称)。
package citychrome

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// tencentQuoteLine 造一条腾讯行情真实格式的行(按实测下标布点)。
func tencentQuoteLine(varName, name, code, price, prev, time, change, pct, high, low string) string {
	f := make([]string, 35)
	for i := range f {
		f[i] = "0"
	}
	f[0] = "1"
	f[1] = name
	f[2] = code
	f[3] = price
	f[4] = prev
	f[5] = price
	f[30] = time
	f[31] = change
	f[32] = pct
	f[33] = high
	f[34] = low
	return fmt.Sprintf("v_%s=\"%s\";", varName, strings.Join(f, "~"))
}

// tencentHFLine 造一条腾讯外盘真实格式的行(逗号口径)。
func tencentHFLine(varName, price, pct, name string) string {
	f := make([]string, 14)
	for i := range f {
		f[i] = "0"
	}
	f[0] = price
	f[1] = pct
	f[6] = "15:36:04"
	f[12] = "2026-10-20"
	f[13] = name
	return fmt.Sprintf("v_%s=\"%s\";", varName, strings.Join(f, ","))
}

// sampleAShareLine 是 2026-10-23 实测样本(上证指数):
// [3]=3658.03 [4]=3713.03 [30]=20261022161500 [31]=-55.00 [32]=-1.48。
const sampleAShareLine = `v_sh000001="1~上证指数~000001~3658.03~3713.03~3655.00~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~20261022161500~-55.00~-1.48~3714.48~3649.86";`

// TestParseTencentQuote_RealSample 实测样本:字段下标 + 涨跌幅服务端自算
// (与站点 [32]=-1.48 吻合)。
func TestParseTencentQuote_RealSample(t *testing.T) {
	rows, resolved, matched := ParseRows(RecipeTencentQuote, sampleAShareLine)
	if !matched || resolved != RecipeTencentQuote {
		t.Fatalf("matched=%v resolved=%q", matched, resolved)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Symbol != "000001" || r.Name != "上证指数" {
		t.Errorf("symbol/name = %q/%q, want 000001/上证指数", r.Symbol, r.Name)
	}
	if r.Price != 3658.03 {
		t.Errorf("price = %v, want 3658.03", r.Price)
	}
	wantPct := (3658.03 - 3713.03) / 3713.03 * 100
	if math.Abs(r.ChangePct-wantPct) > 1e-9 {
		t.Errorf("change_pct = %v, want %v", r.ChangePct, wantPct)
	}
	if math.Abs(r.ChangePct-(-1.48)) > 0.01 {
		t.Errorf("change_pct = %v, want ≈ -1.48(与站点 [32] 吻合)", r.ChangePct)
	}
	if r.PrevClose != 3713.03 {
		t.Errorf("prev_close = %v, want 3713.03", r.PrevClose)
	}
	if r.Currency != "CNY" {
		t.Errorf("currency = %q, want CNY", r.Currency)
	}
	if r.Time != "20261022161500" {
		t.Errorf("time = %q, want 20261022161500(实测 [30])", r.Time)
	}
}

// TestTencentQuoteLayout 实测布局守护:腾讯行情行的 [30]=时间 [31]=涨跌额
// [32]=涨跌幅 [33]=最高 [34]=最低(批次78 实机复测 2026-10-23)。
// 布局漂移 ⇒ 本断言先红(避免配方默默取错下标)。
func TestTencentQuoteLayout(t *testing.T) {
	payload := sampleAShareLine[len(`v_sh000001="`) : len(sampleAShareLine)-2]
	f := strings.Split(payload, "~")
	if len(f) < 35 {
		t.Fatalf("sample line has %d fields, want >= 35", len(f))
	}
	if f[1] != "上证指数" || f[2] != "000001" || f[3] != "3658.03" || f[4] != "3713.03" {
		t.Errorf("head fields = %v", f[:5])
	}
	if f[30] != "20261022161500" {
		t.Errorf("[30] = %q, want 20261022161500(时间)", f[30])
	}
	if f[31] != "-55.00" {
		t.Errorf("[31] = %q, want -55.00(涨跌额)", f[31])
	}
	if f[32] != "-1.48" {
		t.Errorf("[32] = %q, want -1.48(涨跌幅)", f[32])
	}
	if f[33] != "3714.48" || f[34] != "3649.86" {
		t.Errorf("high/low fields = %v/%v", f[33], f[34])
	}
}

// TestParseTencentQuote_MultiRow 多行 + 币种推断(sh/sz→CNY,hk→HKD,us→USD)。
func TestParseTencentQuote_MultiRow(t *testing.T) {
	body := strings.Join([]string{
		sampleAShareLine,
		tencentQuoteLine("sz399001", "深证成指", "399001", "10100.00", "10050.00", "20261022161500", "50.00", "0.50", "10120.00", "10040.00"),
		tencentQuoteLine("hkHSI", "恒生指数", "HSI", "22934.680", "23651.870", "2026/10/22 18:31:35", "-717.190", "-3.03", "23660.00", "22900.00"),
		tencentQuoteLine("usIXIC", "纳斯达克", ".IXIC", "26008.86", "26702.86", "2026-10-22 17:15:59", "-694.00", "-2.60", "26750.00", "25980.00"),
	}, "\n")
	rows, _, matched := ParseRows(RecipeTencentQuote, body)
	if !matched || len(rows) != 4 {
		t.Fatalf("matched=%v rows=%d, want 4", matched, len(rows))
	}
	wantCur := map[string]string{"000001": "CNY", "399001": "CNY", "HSI": "HKD", ".IXIC": "USD"}
	for _, r := range rows {
		if got := wantCur[r.Symbol]; r.Currency != got {
			t.Errorf("symbol %q currency = %q, want %q", r.Symbol, r.Currency, got)
		}
	}
	// 恒生指数自算涨跌幅:(22934.680-23651.870)/23651.870*100 ≈ -3.03
	for _, r := range rows {
		if r.Symbol == "HSI" && math.Abs(r.ChangePct-(-3.03)) > 0.01 {
			t.Errorf("HSI change_pct = %v, want ≈ -3.03", r.ChangePct)
		}
	}
}

// TestParseTencentQuote_DirtyRowsDropped 脏行丢弃:price<=0 / 非数值 / 名称空。
func TestParseTencentQuote_DirtyRowsDropped(t *testing.T) {
	body := strings.Join([]string{
		tencentQuoteLine("sh000001", "上证指数", "000001", "3658.03", "3713.03", "t", "-55.00", "-1.48", "h", "l"),
		tencentQuoteLine("sz000002", "零价", "000002", "0", "1.0", "t", "0", "0", "h", "l"),
		tencentQuoteLine("sz000003", "负价", "000003", "-3.5", "1.0", "t", "0", "0", "h", "l"),
		tencentQuoteLine("sz000004", "", "000004", "10.0", "9.0", "t", "0", "0", "h", "l"),
		tencentQuoteLine("sz000005", "非数值", "000005", "abc", "9.0", "t", "0", "0", "h", "l"),
	}, "\n")
	rows, _, matched := ParseRows(RecipeTencentQuote, body)
	if !matched || len(rows) != 1 {
		t.Fatalf("matched=%v rows=%d, want 1(脏行全丢弃): %+v", matched, len(rows), rows)
	}
	if rows[0].Symbol != "000001" {
		t.Errorf("kept row = %+v, want the 上证指数 row", rows[0])
	}
}

// TestParseTencentQuote_PrevMissingFallback prev 缺失/非正 ⇒ 回退站点 pct
// 字段([32]→[31]→[33] 顺序,|·|≤20 才取)。
func TestParseTencentQuote_PrevMissingFallback(t *testing.T) {
	// prev=0 ⇒ 走回退:[32]=-1.48(≤20)⇒ change_pct=-1.48。
	body := tencentQuoteLine("sh000001", "上证指数", "000001", "3658.03", "0", "t", "-55.00", "-1.48", "h", "l")
	rows, _, _ := ParseRows(RecipeTencentQuote, body)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ChangePct != -1.48 {
		t.Errorf("change_pct = %v, want -1.48(回退站点 [32])", rows[0].ChangePct)
	}
	if rows[0].PrevClose != 0 {
		t.Errorf("prev_close = %v, want 0", rows[0].PrevClose)
	}
	// prev=0 且 [32] 超 20(站点把涨跌额放这)⇒ 落到 [31]。
	body2 := tencentQuoteLine("sh000001", "上证指数", "000001", "3658.03", "0", "t", "-15.00", "-55.00", "h", "l")
	rows2, _, _ := ParseRows(RecipeTencentQuote, body2)
	if len(rows2) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows2))
	}
	if rows2[0].ChangePct != -15.00 {
		t.Errorf("change_pct = %v, want -15.00(回退站点 [31])", rows2[0].ChangePct)
	}
	// 全部候选都超 20 ⇒ 0(不臆造涨跌幅)。
	body3 := tencentQuoteLine("sh000001", "上证指数", "000001", "3658.03", "0", "t", "-55.00", "-55.00", "h", "l")
	rows3, _, _ := ParseRows(RecipeTencentQuote, body3)
	if len(rows3) != 1 || rows3[0].ChangePct != 0 {
		t.Errorf("out-of-range fallback should yield 0: %+v", rows3)
	}
}

// TestParseTencentQuote_ChangePctClamped 涨跌幅钳制到 [-100,100]。
func TestParseTencentQuote_ChangePctClamped(t *testing.T) {
	body := tencentQuoteLine("sh000001", "上证指数", "000001", "3658.03", "1.00", "t", "0", "0", "h", "l")
	rows, _, _ := ParseRows(RecipeTencentQuote, body)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ChangePct != 100 {
		t.Errorf("change_pct = %v, want 100(钳制上限)", rows[0].ChangePct)
	}
}

// TestParseTencentHF 腾讯外盘(逗号口径):price=[0] pct=[1] name=[13]。
func TestParseTencentHF(t *testing.T) {
	body := strings.Join([]string{
		tencentHFLine("hf_GC", "4036.75", "-0.58", "纽约黄金"),
		tencentHFLine("hf_CL", "99.35", "-0.12", "纽约原油"),
	}, "\n")
	rows, resolved, matched := ParseRows(RecipeTencentHF, body)
	if !matched || resolved != RecipeTencentHF {
		t.Fatalf("matched=%v resolved=%q", matched, resolved)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(rows), rows)
	}
	if rows[0].Symbol != "GC" || rows[0].Name != "纽约黄金" || rows[0].Price != 4036.75 {
		t.Errorf("row0 = %+v", rows[0])
	}
	if rows[0].ChangePct != -0.58 {
		t.Errorf("row0 change_pct = %v, want -0.58", rows[0].ChangePct)
	}
	if rows[0].Currency != "USD" {
		t.Errorf("row0 currency = %q, want USD", rows[0].Currency)
	}
	if rows[1].Symbol != "CL" || rows[1].Price != 99.35 {
		t.Errorf("row1 = %+v", rows[1])
	}
}

// TestParseSinaQuote_ExplicitOnly sina_quote 仅显式指定时可用(不在 auto 链)。
func TestParseSinaQuote_ExplicitOnly(t *testing.T) {
	simple := `var hq_str_s_sh000001="上证指数,3735.37,3.2,0.09,...";`
	rows, _, matched := ParseRows(RecipeSinaQuote, simple)
	if !matched || len(rows) != 1 {
		t.Fatalf("explicit sina: matched=%v rows=%d", matched, len(rows))
	}
	if rows[0].Name != "上证指数" || rows[0].Price != 3735.37 || rows[0].ChangePct != 0.09 {
		t.Errorf("sina simple row = %+v", rows[0])
	}
	// auto 不含 sina ⇒ 未命中。
	if _, _, matched := ParseRows(RecipeAuto, simple); matched {
		t.Error("auto must not include sina_quote(批次78 实机复测:sina 不进 auto 链)")
	}
	// 个股版:prev=[2] price=[3],涨跌幅自算。
	single := `var hq_str_sh600000="浦发银行,10.00,9.90,9.95,...";`
	rows2, _, _ := ParseRows(RecipeSinaQuote, single)
	if len(rows2) != 1 {
		t.Fatalf("sina single rows = %d, want 1", len(rows2))
	}
	want := (9.95 - 9.90) / 9.90 * 100
	if math.Abs(rows2[0].ChangePct-want) > 1e-9 {
		t.Errorf("sina single change_pct = %v, want %v", rows2[0].ChangePct, want)
	}
	if rows2[0].Symbol != "600000" {
		t.Errorf("sina single symbol = %q, want 600000", rows2[0].Symbol)
	}
}

// TestParseRows_AutoOrder auto 顺序:tencent_quote → tencent_hf;
// 两者同在时优先 tencent_quote;全不命中 ⇒ matched=false 且零报价。
func TestParseRows_AutoOrder(t *testing.T) {
	both := sampleAShareLine + "\n" + tencentHFLine("hf_GC", "4036.75", "-0.58", "纽约黄金")
	rows, resolved, matched := ParseRows(RecipeAuto, both)
	if !matched || resolved != RecipeTencentQuote {
		t.Fatalf("auto resolved = %q, want tencent_quote", resolved)
	}
	if len(rows) != 1 || rows[0].Symbol != "000001" {
		t.Errorf("auto rows = %+v", rows)
	}
	// 只有 hf ⇒ 命中 tencent_hf。
	onlyHF := tencentHFLine("hf_GC", "4036.75", "-0.58", "纽约黄金")
	rows2, resolved2, matched2 := ParseRows(RecipeAuto, onlyHF)
	if !matched2 || resolved2 != RecipeTencentHF || len(rows2) != 1 {
		t.Fatalf("auto(hf) matched=%v resolved=%q rows=%d", matched2, resolved2, len(rows2))
	}
	// 非行情正文 ⇒ 不命中、零报价。
	rows3, resolved3, matched3 := ParseRows(RecipeAuto, "上证指数收涨,今日两市成交额破万亿。")
	if matched3 || len(rows3) != 0 || resolved3 != "" {
		t.Errorf("auto must not match news text: matched=%v resolved=%q rows=%d", matched3, resolved3, len(rows3))
	}
}

// TestParseRows_RowCap 单次解析上限 64 行。
func TestParseRows_RowCap(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 70; i++ {
		sb.WriteString(tencentQuoteLine(fmt.Sprintf("sh%06d", i), "指数", fmt.Sprintf("%06d", i),
			"100.00", "99.00", "t", "1.00", "1.01", "h", "l"))
		sb.WriteString("\n")
	}
	rows, _, _ := ParseRows(RecipeTencentQuote, sb.String())
	if len(rows) != parseRowCap {
		t.Errorf("rows = %d, want %d(单次解析上限)", len(rows), parseRowCap)
	}
}

// TestParseText_Result web_parse 的结构化出口:matched/resolved/count/rows。
func TestParseText_Result(t *testing.T) {
	res, err := ParseText(sampleAShareLine, "")
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	if !res.Matched || res.Resolved != RecipeTencentQuote || res.Count != 1 {
		t.Errorf("result = %+v", res)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"recipe"`, `"matched"`, `"resolved"`, `"count"`, `"rows"`} {
		if !strings.Contains(string(raw), k) {
			t.Errorf("parse result json missing %q: %s", k, raw)
		}
	}
	// 未命中:matched=false + rows 空数组 + hint。
	res2, err := ParseText("今天天气不错", "")
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	if res2.Matched || len(res2.Rows) != 0 || res2.Hint == "" {
		t.Errorf("unmatched result = %+v", res2)
	}
	// 未知配方 ⇒ 错误。
	if _, err := ParseText("x", "nope"); err == nil {
		t.Error("unknown recipe should fail")
	}
}

// TestListRecipes_IsParseRecipe 配方注册表与校验。
func TestListRecipes_IsParseRecipe(t *testing.T) {
	recipes := ListRecipes()
	if len(recipes) != 4 {
		t.Fatalf("recipes = %d, want 4", len(recipes))
	}
	ids := map[string]bool{}
	for _, r := range recipes {
		ids[r.ID] = true
		if r.Desc == "" || r.Parse == nil {
			t.Errorf("recipe %q incomplete", r.ID)
		}
	}
	for _, want := range []string{RecipeAuto, RecipeTencentQuote, RecipeTencentHF, RecipeSinaQuote} {
		if !ids[want] {
			t.Errorf("missing recipe %q", want)
		}
	}
	if !IsParseRecipe(RecipeAuto) || IsParseRecipe("nope") {
		t.Error("IsParseRecipe mismatch")
	}
}

// TestFormatParsedBlock 结构化块的输出形态(契约 §3.1)。
func TestFormatParsedBlock(t *testing.T) {
	rows, resolved, matched := ParseRows(RecipeAuto, sampleAShareLine)
	block := FormatParsedBlock(RecipeAuto, resolved, rows, matched)
	if !strings.Contains(block, "【结构化解析·recipe=auto→tencent_quote·命中 1 条】") {
		t.Errorf("block header mismatch: %s", block)
	}
	if !strings.Contains(block, `"symbol":"000001"`) || !strings.Contains(block, `"price":3658.03`) {
		t.Errorf("block rows mismatch: %s", block)
	}
	// 未命中块。
	miss := FormatParsedBlock(RecipeTencentQuote, "", nil, false)
	if !strings.Contains(miss, "未命中") {
		t.Errorf("miss block = %s", miss)
	}
}
