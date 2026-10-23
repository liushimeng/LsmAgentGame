// Package citychrome — recipes.go: 确定性解析配方注册表(批次78 §3.1)。
//
// 目标:把「抓网页 → 读数字」从依赖 LLM 手工切分升级为**服务端确定性解析**。
// LLM 不再自己按分隔符切行情正文,改为直接消费本包产出的结构化报价。
//
// 设计纪律:
//   - 配方是**纯函数、零 IO**(不碰浏览器、不碰网络),只读正文字符串;
//   - 强特征识别(只认 v_xxx="..." / hq_str_xxx="..." 形态),正文不像行情时
//     一律「不命中」(matched=false)且**不产出任何报价**;
//   - 涨跌幅**一律服务端自算** `(price-prev)/prev*100`(站点 pct 字段各版本
//     下标口径漂移,不信任);仅当 prev 缺失/非正时才回退取站点字段;
//   - 脏行(price<=0 / 非有限)丢弃;change_pct 钳制到 [-100,100];
//   - 单次解析上限 64 行。
package citychrome

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// 解析配方 ID(批次78 §3.1)。
const (
	RecipeAuto         = "auto"          // 依次尝试 tencent_quote → tencent_hf,首个命中 ≥1 条即用
	RecipeTencentQuote = "tencent_quote" // 腾讯行情:~ 分隔
	RecipeTencentHF    = "tencent_hf"    // 腾讯外盘(hf_):, 分隔
	RecipeSinaQuote    = "sina_quote"    // 新浪行情(hq_str_*):, 分隔
)

// 解析口径常量(批次78 §3.1)。
const (
	// parseRowCap 是单次解析的最大行数(上限 64 行)。
	parseRowCap = 64
	// parsePctLimit 是 change_pct 钳制区间(±100)。
	parsePctLimit = 100.0
	// parsePctFallbackLimit 是站点 pct 字段回退时的绝对值上限(|·| ≤ 20);
	// 站点把「涨跌额」放在 pct 位置时会超限,直接跳过。
	parsePctFallbackLimit = 20.0
)

// 腾讯行情 pct 回退字段下标顺序(批次78 §3.1 + 实机复测 2026-10-23:
// 实测布局为 [30]=时间 [31]=涨跌额 [32]=涨跌幅 [33]=最高 [34]=最低,
// 故按 [32]→[31]→[33] 取第一个能解析成数值且 |·|≤20 的值)。
var pctFallbackIndexes = []int{32, 31, 33}

var (
	// tencentVarRe 匹配腾讯行情变量:v_<name>="<payload>"。
	tencentVarRe = regexp.MustCompile(`v_([A-Za-z0-9_]+)="([^"]*)"`)
	// sinaVarRe 匹配新浪行情变量:hq_str_<name>="<payload>"。
	sinaVarRe = regexp.MustCompile(`hq_str_([A-Za-z0-9_]+)="([^"]*)"`)
)

// ParsedRow 是确定性解析出的一条行情(契约 §3.1;JSON tag 逐字对齐)。
type ParsedRow struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	ChangePct float64 `json:"change_pct"`
	Currency  string  `json:"currency,omitempty"`
	PrevClose float64 `json:"prev_close,omitempty"`
	Time      string  `json:"time,omitempty"`
}

// ParseRecipe 是解析配方(纯函数,无 IO)。
type ParseRecipe struct {
	ID    string
	Desc  string
	Parse func(body string) []ParsedRow
}

// ParseResult 是确定性解析的结构化结果(批次78 §3.1/§3.2:web_parse 的出口)。
type ParseResult struct {
	Recipe   string      `json:"recipe"`             // 请求的配方 ID(auto 表示自动匹配)
	Matched  bool        `json:"matched"`            // 是否命中行情特征
	Resolved string      `json:"resolved,omitempty"` // 命中的实际配方(auto 时为具体配方)
	Count    int         `json:"count"`              // 命中行数
	Rows     []ParsedRow `json:"rows"`               // 结构化行(未命中为空数组,非 null)
	Hint     string      `json:"hint,omitempty"`     // 未命中时的提示
}

// recipeOrder 是**显式指定**配方时的解析实现表(含 sina_quote)。
var recipeOrder = []struct {
	id string
	fn func(string) []ParsedRow
}{
	{RecipeTencentQuote, parseTencentQuote},
	{RecipeTencentHF, parseTencentHF},
	{RecipeSinaQuote, parseSinaQuote},
}

// autoOrder 是 auto 的尝试顺序(批次78 §3.1 + 实机复测 2026-10-23:
// **tencent_quote → tencent_hf**)。sina_quote 因依赖 Referer、字段布局随
// 品种变化(简版 s_ 与个股版不同),本批次**不进 auto 链**,仅在显式指定
// recipe=sina_quote 时可用。
var autoOrder = []struct {
	id string
	fn func(string) []ParsedRow
}{
	{RecipeTencentQuote, parseTencentQuote},
	{RecipeTencentHF, parseTencentHF},
}

// ListRecipes 返回全部配方(auto 在前,便于工具描述与提示词引用)。
func ListRecipes() []ParseRecipe {
	return []ParseRecipe{
		{ID: RecipeAuto, Desc: "自动:依次尝试 tencent_quote → tencent_hf,首个命中即用", Parse: parseAuto},
		{ID: RecipeTencentQuote, Desc: "腾讯行情(~ 分隔):name=[1] code=[2] price=[3] prev=[4] time=[30],涨跌幅服务端自算", Parse: parseTencentQuote},
		{ID: RecipeTencentHF, Desc: "腾讯外盘(hf_,, 分隔):price=[0] pct=[1] name=[13],symbol=变量名去 hf_ 前缀", Parse: parseTencentHF},
		{ID: RecipeSinaQuote, Desc: "新浪行情(hq_str_*,, 分隔):s_ 简版 name=[0] price=[1] pct=[3];个股版 name=[0] prev=[2] price=[3](仅显式指定时可用,不在 auto 链)", Parse: parseSinaQuote},
	}
}

// IsParseRecipe 报告 id 是否是已知配方 ID(auto 亦算)。
func IsParseRecipe(id string) bool {
	switch strings.TrimSpace(id) {
	case RecipeAuto, RecipeTencentQuote, RecipeTencentHF, RecipeSinaQuote:
		return true
	}
	return false
}

// ParseRows 按配方解析正文(批次78 §3.1)。
// 返回 (结构化行, 命中的配方 ID, 是否命中);recipeID 空 = auto。
// 未知配方返回 (nil, "", false) —— 调用方需先校验配方 ID。
func ParseRows(recipeID, body string) ([]ParsedRow, string, bool) {
	id := strings.TrimSpace(recipeID)
	if id == "" {
		id = RecipeAuto
	}
	if id == RecipeAuto {
		for _, r := range autoOrder {
			if rows := r.fn(body); len(rows) > 0 {
				return rows, r.id, true
			}
		}
		return nil, "", false
	}
	for _, r := range recipeOrder {
		if r.id != id {
			continue
		}
		rows := r.fn(body)
		return rows, r.id, len(rows) > 0
	}
	return nil, "", false
}

// ParseText 解析任意文本(批次78 §3.2:web_parse 工具出口)。
// recipeID 空 = auto;未知配方返回错误(中文文案)。
func ParseText(text, recipeID string) (ParseResult, error) {
	id := strings.TrimSpace(recipeID)
	if id == "" {
		id = RecipeAuto
	}
	if !IsParseRecipe(id) {
		return ParseResult{}, fmt.Errorf("未知解析配方 %q(可用:auto|tencent_quote|tencent_hf|sina_quote)", id)
	}
	rows, resolved, matched := ParseRows(id, text)
	res := ParseResult{
		Recipe:   id,
		Matched:  matched,
		Resolved: resolved,
		Count:    len(rows),
		Rows:     rows,
	}
	if res.Rows == nil {
		res.Rows = []ParsedRow{}
	}
	if !matched {
		res.Hint = "未匹配到行情数据特征(只认 v_xxx=\"…\" / hq_str_xxx=\"…\");" +
			"可换显式 recipe(tencent_quote/tencent_hf/sina_quote)或回退 web_eval 手工提取"
	}
	return res, nil
}

// FormatParsedBlock 生成回灌 LLM 的结构化解析文本块(契约 §3.1 的输出形态)。
// 结构化块**在前**,正文片段在后;matched=false 时给出未命中提示。
func FormatParsedBlock(recipeID, resolved string, rows []ParsedRow, matched bool) string {
	label := strings.TrimSpace(recipeID)
	if label == "" {
		label = RecipeAuto
	}
	if matched && label == RecipeAuto && resolved != "" {
		label = RecipeAuto + "→" + resolved
	}
	if !matched {
		return fmt.Sprintf("【结构化解析·recipe=%s·未命中(0 条)】\n未匹配到行情数据特征;换显式 recipe 或回退 web_eval 手工提取。", label)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return fmt.Sprintf("【结构化解析·recipe=%s·命中 %d 条】\n(序列化失败)", label, len(rows))
	}
	return fmt.Sprintf("【结构化解析·recipe=%s·命中 %d 条】\n%s", label, len(rows), string(raw))
}

// ── 具体配方 ───────────────────────────────────────────────────────────

// parseAuto 依次尝试 autoOrder(tencent_quote → tencent_hf),首个命中 ≥1 条
// 即用(全不命中返回空)。
func parseAuto(body string) []ParsedRow {
	for _, r := range autoOrder {
		if rows := r.fn(body); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

// parseTencentQuote 解析腾讯行情正文(~ 分隔)。
//
// 形如 v_sh000001="1~上证指数~000001~3658.03~3713.03~…~20261022161500~-55.00~-1.48~3714.48~3649.86~…"
// 实机复测 2026-10-23 的字段布局(A股 sh / 深 sz / 港股 hk / 美股 us 一致):
//
//		[1]=名称  [2]=代码  [3]=当前价  [4]=昨收  [5]=今开
//		[30]=时间  [31]=涨跌额  [32]=涨跌幅(%)  [33]=最高  [34]=最低
//
//	  - 涨跌幅**服务端自算** (price-prev)/prev*100(prev>0 时;实测与站点 [32] 吻合);
//	  - prev 缺失/非正时回退站点 pct 字段([32]→[31]→[33],|·|≤20 才取)。
func parseTencentQuote(body string) []ParsedRow {
	rows := make([]ParsedRow, 0, 8)
	for _, m := range tencentVarRe.FindAllStringSubmatch(body, -1) {
		if len(rows) >= parseRowCap {
			break
		}
		varName, payload := m[1], m[2]
		if strings.HasPrefix(strings.ToLower(varName), "hf_") {
			continue // hf_ 是逗号口径,归 tencent_hf。
		}
		fields := strings.Split(payload, "~")
		if len(fields) < 5 {
			continue
		}
		name := strings.TrimSpace(fields[1])
		if name == "" {
			continue
		}
		price, ok := parseFloat(fields[3])
		if !ok {
			continue
		}
		symbol := strings.TrimSpace(fields[2])
		if symbol == "" {
			symbol = varName
		}
		row := ParsedRow{Symbol: symbol, Name: name, Price: price}
		if prev, prevOK := parseFloat(fields[4]); prevOK && prev > 0 {
			row.PrevClose = prev
			row.ChangePct = (price - prev) / prev * 100
		} else {
			row.ChangePct = fallbackSitePct(fields)
		}
		row.Currency = inferCurrency(varName)
		if len(fields) > 30 {
			if t := strings.TrimSpace(fields[30]); t != "" {
				row.Time = t
			}
		}
		if row, ok := normalizeRow(row); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

// parseTencentHF 解析腾讯外盘行情正文(, 分隔)。
//
// 形如 v_hf_GC="4144.15,-0.38,4144.90,…,2026-10-20,纽约黄金"
//   - price=[0] pct=[1] name=[13];symbol=变量名去掉 hf_ 前缀。
func parseTencentHF(body string) []ParsedRow {
	rows := make([]ParsedRow, 0, 8)
	for _, m := range tencentVarRe.FindAllStringSubmatch(body, -1) {
		if len(rows) >= parseRowCap {
			break
		}
		varName, payload := m[1], m[2]
		if !strings.HasPrefix(strings.ToLower(varName), "hf_") {
			continue
		}
		fields := strings.Split(payload, ",")
		if len(fields) < 14 {
			continue
		}
		name := strings.TrimSpace(fields[13])
		if name == "" {
			continue
		}
		price, ok := parseFloat(fields[0])
		if !ok {
			continue
		}
		symbol := varName
		if len(symbol) > len("hf_") {
			symbol = symbol[len("hf_"):]
		}
		row := ParsedRow{Symbol: symbol, Name: name, Price: price}
		if pct, pctOK := parseFloat(fields[1]); pctOK {
			row.ChangePct = pct
		}
		row.Currency = inferCurrency(varName)
		if row, ok := normalizeRow(row); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

// parseSinaQuote 解析新浪行情正文(, 分隔)。
//
//   - s_ 简版:var hq_str_s_sh000001="上证指数,3735.37,3.2,0.09,…"
//     ⇒ name=[0] price=[1] pct=[3];
//   - 个股版:var hq_str_sh600000="浦发银行,10.00,9.90,9.95,…"
//     ⇒ name=[0] prev=[2] price=[3],涨跌幅自算。
func parseSinaQuote(body string) []ParsedRow {
	rows := make([]ParsedRow, 0, 8)
	for _, m := range sinaVarRe.FindAllStringSubmatch(body, -1) {
		if len(rows) >= parseRowCap {
			break
		}
		varName, payload := m[1], m[2]
		fields := strings.Split(payload, ",")
		if len(fields) < 4 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" {
			continue
		}
		row := ParsedRow{Name: name}
		if strings.HasPrefix(strings.ToLower(varName), "s_") {
			price, ok := parseFloat(fields[1])
			if !ok {
				continue
			}
			row.Price = price
			if pct, pctOK := parseFloat(fields[3]); pctOK {
				row.ChangePct = pct
			}
		} else {
			price, ok := parseFloat(fields[3])
			if !ok {
				continue
			}
			row.Price = price
			if prev, prevOK := parseFloat(fields[2]); prevOK && prev > 0 {
				row.PrevClose = prev
				row.ChangePct = (price - prev) / prev * 100
			}
		}
		row.Symbol = sinaSymbol(varName)
		row.Currency = inferCurrency(varName)
		if row, ok := normalizeRow(row); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

// ── 解析小工具 ────────────────────────────────────────────────────────

// parseFloat 解析数值字段;空串/非法/非有限 → false。
func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || isNaNOrInf(v) {
		return 0, false
	}
	return v, true
}

// fallbackSitePct 回退取站点 pct 字段(按 [32]→[33]→[31] 顺序,|·|≤20)。
// 仅当 prev 缺失/非正时使用(涨跌幅优先服务端自算)。
func fallbackSitePct(fields []string) float64 {
	for _, idx := range pctFallbackIndexes {
		if idx >= len(fields) {
			continue
		}
		v, ok := parseFloat(fields[idx])
		if !ok || math.Abs(v) > parsePctFallbackLimit {
			continue
		}
		return v
	}
	return 0
}

// inferCurrency 由变量名前缀推断币种(批次78 §3.1):
// sh/sz → CNY;hk → HKD;us → USD;hf_ → USD;其余留空。
func inferCurrency(varName string) string {
	n := strings.ToLower(strings.TrimSpace(varName))
	n = strings.TrimPrefix(n, "s_")
	switch {
	case strings.HasPrefix(n, "hf_"):
		return "USD"
	case strings.HasPrefix(n, "sh"), strings.HasPrefix(n, "sz"):
		return "CNY"
	case strings.HasPrefix(n, "hk"):
		return "HKD"
	case strings.HasPrefix(n, "us"):
		return "USD"
	}
	return ""
}

// sinaSymbol 由新浪变量名推导 symbol(s_ 前缀与市场前缀都去掉)。
func sinaSymbol(varName string) string {
	n := varName
	if strings.HasPrefix(strings.ToLower(n), "s_") {
		n = n[len("s_"):]
	}
	lower := strings.ToLower(n)
	for _, p := range []string{"sh", "sz", "hk", "us"} {
		if strings.HasPrefix(lower, p) && len(n) > len(p) {
			return n[len(p):]
		}
	}
	if n == "" {
		return varName
	}
	return n
}

// normalizeRow 脏行丢弃 + 数值钳制(批次78 §3.1 校验):
// price<=0 或非有限 → ok=false(丢弃该行);change_pct 钳制到 [-100,100]。
func normalizeRow(r ParsedRow) (ParsedRow, bool) {
	if r.Price <= 0 || isNaNOrInf(r.Price) {
		return r, false
	}
	if isNaNOrInf(r.ChangePct) {
		r.ChangePct = 0
	}
	if r.ChangePct > parsePctLimit {
		r.ChangePct = parsePctLimit
	}
	if r.ChangePct < -parsePctLimit {
		r.ChangePct = -parsePctLimit
	}
	return r, true
}
