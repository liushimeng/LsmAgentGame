// Package citychrome — mcp_tools.go: MCP_Web_Use 工具集(契约 §4)。
//
// 10 个工具:web_open / web_control / web_eval / web_look / web_list /
// web_close 走 go-web-debug-tool HTTP;web_done 是本地工具(不走 HTTP),
// 结束 mission 并带 §3.4 payload schema 校验(quotes 上限/数值合法/单条
// 脏数据丢弃/未知 category 拒绝);批次77 §3.1 新增 3 个 —— web_plan 是
// 本地工具(记录结构化规划),web_read 一步完成「打开+读正文」,web_search
// 必应中国 SERP 只读提取(固定源全失败后的发现兜底,自动关页)。
//
// 所有返回保留 page_id(需求 #8:点击/开新页后回传 page_id 保持连贯);
// web_control 的 action 白名单服务端校验,表外 action 直接拒绝;
// 截图统一走 web_look(info=screenshot,支持 save_path 落盘)。
package citychrome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	llmtypes "LsmAgentGame/llm/types"
)

// MCP 工具名(契约 §4 表;批次77 §3.1 追加 3 个,7→10)。
const (
	ToolWebOpen    = "web_open"
	ToolWebControl = "web_control"
	ToolWebEval    = "web_eval"
	ToolWebLook    = "web_look"
	ToolWebList    = "web_list"
	ToolWebClose   = "web_close"
	ToolWebDone    = "web_done"

	// 批次77 §3.1 新工具。
	ToolWebPlan   = "web_plan"   // 本地:结构化规划(mission 第 1 步必须调用)
	ToolWebRead   = "web_read"   // 一步=打开页面+读正文(domcontentloaded)
	ToolWebSearch = "web_search" // 必应中国 SERP 只读提取,提取后自动关页
)

// webReadMaxCharsDefault / webReadMaxCharsMin / webReadMaxCharsMax 是
// web_read max_chars 的缺省与钳制区间(批次77 §3.1:缺省 3000,500..8000)。
const (
	webReadMaxCharsDefault = 3000
	webReadMaxCharsMin     = 500
	webReadMaxCharsMax     = 8000
)

// webSearchCountDefault / webSearchCountMin / webSearchCountMax 是
// web_search count 的缺省与钳制区间(批次77 §3.1:缺省 5,1..10)。
const (
	webSearchCountDefault = 5
	webSearchCountMin     = 1
	webSearchCountMax     = 10
)

// missionSourceMaxRunes 是 web_done payload.source 的 rune 上限(兜底截断)。
// source 语义是站点名(看板「采集」子页来源列与新闻来源列直出),Agent 塞入
// 链接说明/时间口径解释等长串时按上限截断,防污染看板。
const missionSourceMaxRunes = 40

// webControlActions 是 web_control 的 action 白名单(契约 §4;服务端校验)。
var webControlActions = map[string]bool{
	"click": true, "right_click": true, "double_click": true, "hover": true,
	"scroll": true, "scroll_to": true,
	"key_press": true, "press_sequence": true,
	"input_text": true, "human_input": true, "clear_input": true,
	"upload_file": true, "select_option": true, "drag": true,
	"navigate": true, "back": true, "forward": true, "reload": true,
	"new_tab": true, "close_tab": true,
	"screenshot": true, "add_script": true, "eval_js": true,
}

// webLookInfos 是 web_look 的 info 白名单(契约 §4)。
var webLookInfos = map[string]bool{
	"console": true, "network": true, "elements": true, "dom": true,
	"localstorage": true, "sessionstorage": true, "cookies": true,
	"performance": true, "screenshot": true,
}

// evalItem 是 web_eval items 的单项 wire 形态。
type evalItem struct {
	Label        string `json:"label"`
	Expression   string `json:"expression"`
	AwaitPromise bool   `json:"await_promise"`
}

// Toolset 是 10 个 MCP 工具的派发器。Done 是最近一次 web_done 的校验结果
// (Agent 循环据此结束 mission)。
type Toolset struct {
	client *WebUseClient
	// SymbolsPerCategory / NewsLimit 用于 web_done payload 校验。
	symbolsPerCategory int
	newsLimit          int

	// Done 非 nil 表示本 mission 已收到合法 web_done(Agent 循环结束条件)。
	Done *MissionPayload

	// Plan 是最近一次 web_plan 的摘要(goal + steps 以 " → " 连接;
	// 批次77 §3.3:遥测 MissionRecord.Plan 取此值)。
	Plan string
	// OpenedPages 记录 web_open / web_read 打开的 page_id(批次77 §3.3-4:
	// RunMission defer best-effort 逐个回收;web_search 的 SERP 页不在此列,
	// 它在提取后立即关闭)。
	OpenedPages []string
}

// NewToolset 构造派发器。
func NewToolset(client *WebUseClient, symbolsPerCategory, newsLimit int) *Toolset {
	if symbolsPerCategory <= 0 {
		symbolsPerCategory = 12
	}
	if newsLimit <= 0 {
		newsLimit = 20
	}
	return &Toolset{client: client, symbolsPerCategory: symbolsPerCategory, newsLimit: newsLimit}
}

// BuildTools 返回 10 个 MCP 工具的 LLM 定义(契约 §4 表;批次77 §3.1 追加
// web_plan/web_read/web_search,7→10)。
func BuildTools() []llmtypes.ToolDef {
	obj := func(props map[string]any, required []string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return []llmtypes.ToolDef{
		{
			Name: ToolWebOpen,
			Description: "打开新 Chrome 页面。返回 page_id/title/final_url;后续所有操作都带这个 page_id。",
			InputSchema: obj(map[string]any{
				"url":        str("目标 URL,必须含 http:// 或 https://"),
				"wait_until": str("可选:load(默认)/domcontentloaded/networkidle"),
			}, []string{"url"}),
		},
		{
			Name: ToolWebControl,
			Description: "页面交互动作(白名单 action,表外拒绝)。点击/开新页返回里若带 spawned_page_id," +
				"必须纳入你的页面索引。截图请优先走 web_look(info=screenshot)。action 取值:" +
				"click/right_click/double_click/hover/scroll/scroll_to/key_press/press_sequence/" +
				"input_text/human_input/clear_input/upload_file/select_option/drag/navigate/back/" +
				"forward/reload/new_tab/close_tab/screenshot/add_script/eval_js。",
			InputSchema: obj(map[string]any{
				"page_id": str("目标页面 id"),
				"action":  str("动作名(白名单)"),
				"params":  map[string]any{"type": "object", "description": "动作参数,不需要时传 {}"},
			}, []string{"page_id", "action", "params"}),
		},
		{
			Name: ToolWebEval,
			Description: "BatchEval 批量 DOM 数据提取:一次 HTTP 往返执行 1-64 条 eval_js 表达式," +
				"优先用它采集行情(减少往返)。每条 {label, expression, await_promise?};" +
				"下载文件=eval_js 内 fetch→base64 返回(同源限制内)。",
			InputSchema: obj(map[string]any{
				"page_id": str("目标页面 id"),
				"items": map[string]any{
					"type":        "array",
					"minItems":    1,
					"maxItems":    64,
					"description": "eval 条目数组,每条 {label,expression,await_promise?}",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label":         str("业务标签,原样回显"),
							"expression":    str("在页面上下文执行的 JS 表达式"),
							"await_promise": map[string]any{"type": "boolean", "description": "是否 await Promise 结果"},
						},
						"required": []string{"expression"},
					},
				},
			}, []string{"page_id", "items"}),
		},
		{
			Name: ToolWebLook,
			Description: "只读观察页面。info 取值:console/network/elements/dom/localstorage/" +
				"sessionstorage/cookies/performance/screenshot(支持 save_path 落盘,防数 MB " +
				"base64 在上下文里往返)。",
			InputSchema: obj(map[string]any{
				"page_id": str("目标页面 id"),
				"info":    str("信息种类(白名单)"),
				"params":  map[string]any{"type": "object", "description": "查询参数,可无"},
			}, []string{"page_id", "info"}),
		},
		{
			Name:        ToolWebList,
			Description: "列出全部受管页面(页面索引);任务开始或 2000 page not found 后用它对齐索引。",
			InputSchema: obj(map[string]any{}, []string{}),
		},
		{
			Name:        ToolWebClose,
			Description: "关闭页面回收资源;mission 结束必须关闭自己打开的所有 page_id。",
			InputSchema: obj(map[string]any{
				"page_id": str("要关闭的页面 id"),
			}, []string{"page_id"}),
		},
		{
			Name: ToolWebPlan,
			Description: "提交本次 mission 的结构化规划(本地工具,不走浏览器;批次77)。" +
				"mission 第 1 步必须调用:goal 一句话(≤80 字,可选),steps 1-8 条、每条非空 ≤80 字," +
				"规划须含目标站与换源预案。规划会入遥测供跨 mission 经验优化。",
			InputSchema: obj(map[string]any{
				"goal": str("可选:采集目标一句话(≤80 字)"),
				"steps": map[string]any{
					"type":        "array",
					"minItems":    1,
					"maxItems":    8,
					"description": "规划步骤(≤5 步为宜),每条非空 ≤80 字",
					"items":       str("一步描述(≤80 字)"),
				},
			}, []string{"steps"}),
		},
		{
			Name: ToolWebRead,
			Description: "一步完成「打开页面 + 读正文」(批次77;简单读取优先用它,比 " +
				"web_open+web_eval 省 1 步)。url 必须 http(s)://;max_chars 缺省 3000" +
				"(500..8000 钳制)。返回 page_id/title/正文片段;page_id 已登记," +
				"需要精细 DOM 提取时可用 web_eval 继续操作该页。",
			InputSchema: obj(map[string]any{
				"url": str("目标 URL,必须以 http:// 或 https:// 开头"),
				"max_chars": map[string]any{
					"type":        "integer",
					"minimum":     webReadMaxCharsMin,
					"maximum":     webReadMaxCharsMax,
					"description": "正文最多返回字符数(缺省 3000,钳制 500..8000)",
				},
			}, []string{"url"}),
		},
		{
			Name: ToolWebSearch,
			Description: "必应中国搜索,只读发现兜底(批次77):固定源与备选站全部失败时才用。" +
				"打开 cn.bing.com SERP → 提取结果(title/url/snippet≤120 字)→ 自动关页;" +
				"0 结果时返回页面前 600 字兜底。从结果里选公开可读页用 web_read 提取," +
				"仍禁止登录/验证码/表单提交。",
			InputSchema: obj(map[string]any{
				"query": str("检索词(非空,≤100 字)"),
				"count": map[string]any{
					"type":        "integer",
					"minimum":     webSearchCountMin,
					"maximum":     webSearchCountMax,
					"description": "返回条数(缺省 5,钳制 1..10)",
				},
			}, []string{"query"}),
		},
		{
			Name: ToolWebDone,
			Description: "结束本次采集 mission 并提交结构化数据(唯一使命出口,必须调用)。payload 严格按 schema:" +
				"{category, source, quotes:[{symbol,name,price,change_pct,currency?}], " +
				"news:[{title,url,published_at}]}。单条脏数据会被丢弃但不影响其它数据。",
			InputSchema: obj(map[string]any{
				"payload": map[string]any{"type": "object", "description": "结构化采集结果(见工具描述 schema)"},
			}, []string{"payload"}),
		},
	}
}

// Dispatch 派发一个 tool_use。返回人读结果文本 + 是否错误;web_done 校验通过时
// ts.Done 置位(Agent 循环据此 break)。
func (ts *Toolset) Dispatch(ctx context.Context, name string, input map[string]any) (string, bool) {
	getStr := func(k string) string { s, _ := input[k].(string); return s }
	switch name {
	case ToolWebOpen:
		pi, err := ts.client.NewChromePage(ctx, getStr("url"), getStr("wait_until"))
		if err != nil {
			return "失败:" + err.Error(), true
		}
		ts.OpenedPages = append(ts.OpenedPages, pi.PageID) // 批次77 §3.3-4:mission 结束统一回收。
		return fmt.Sprintf("已打开 page_id=%s title=%s final_url=%s", pi.PageID, pi.Title, pi.FinalURL), false
	case ToolWebControl:
		action := getStr("action")
		if !webControlActions[action] {
			return fmt.Sprintf("失败:action %q 不在白名单", action), true
		}
		params, _ := input["params"].(map[string]any)
		cr, err := ts.client.ControlChromePage(ctx, getStr("page_id"), action, params)
		if err != nil {
			return "失败:" + err.Error(), true
		}
			out := "已完成"
		if len(cr.Result) > 0 && string(cr.Result) != "null" {
			out += " result=" + truncate(string(cr.Result), 600)
		}
		if cr.SpawnedPageID != "" {
			out += " spawned_page_id=" + cr.SpawnedPageID
		}
		return out, false
	case ToolWebEval:
		items, err := parseEvalItems(input["items"])
		if err != nil {
			return "失败:" + err.Error(), true
		}
		br, err := ts.client.BatchEval(ctx, getStr("page_id"), items)
		if err != nil {
			return "失败:" + err.Error(), true
		}
		raw, _ := json.Marshal(map[string]any{
			"results": br.Results, "succeeded": br.Succeeded, "failed": br.Failed, "truncated": br.Truncated,
		})
		return "批量求值完成 " + truncate(string(raw), 3000), false
	case ToolWebLook:
		info := getStr("info")
		if !webLookInfos[info] {
			return fmt.Sprintf("失败:info %q 不在白名单", info), true
		}
		params, _ := input["params"].(map[string]any)
		lr, err := ts.client.LookChromePageInfo(ctx, getStr("page_id"), info, params)
		if err != nil {
			return "失败:" + err.Error(), true
		}
		return fmt.Sprintf("info=%s data=%s", lr.Info, truncate(string(lr.Data), 3000)), false
	case ToolWebList:
		pages, err := ts.client.ListChromePages(ctx)
		if err != nil {
			return "失败:" + err.Error(), true
		}
		raw, _ := json.Marshal(pages)
		return "页面索引(" + fmt.Sprint(len(pages)) + " 页): " + truncate(string(raw), 2000), false
	case ToolWebClose:
		pageID := getStr("page_id")
		if err := ts.client.CloseChromePage(ctx, pageID); err != nil {
			return "失败:" + err.Error(), true
		}
		ts.removeOpenedPage(pageID) // 已显式关闭,从待回收清单去重。
		return "已关闭 page_id=" + pageID, false
	case ToolWebPlan:
		goal := strings.TrimSpace(getStr("goal"))
		if utf8.RuneCountInString(goal) > 80 {
			return "失败:goal 超过 80 字上限", true
		}
		rawSteps, _ := input["steps"].([]any)
		if len(rawSteps) < 1 || len(rawSteps) > 8 {
			return fmt.Sprintf("失败:steps 必须是 1-8 条数组(got %d)", len(rawSteps)), true
		}
		steps := make([]string, 0, len(rawSteps))
		for i, s := range rawSteps {
			text, _ := s.(string)
			text = strings.TrimSpace(text)
			if text == "" {
				return fmt.Sprintf("失败:steps[%d] 为空", i), true
			}
			if utf8.RuneCountInString(text) > 80 {
				return fmt.Sprintf("失败:steps[%d] 超过 80 字上限", i), true
			}
			steps = append(steps, text)
		}
		if goal != "" {
			ts.Plan = goal + "：" + strings.Join(steps, " → ")
		} else {
			ts.Plan = strings.Join(steps, " → ")
		}
		return fmt.Sprintf("已记录规划 %d 步: %s", len(steps), ts.Plan), false
	case ToolWebRead:
		readURL := strings.TrimSpace(getStr("url"))
		if !strings.HasPrefix(readURL, "http://") && !strings.HasPrefix(readURL, "https://") {
			return "失败:url 必须以 http:// 或 https:// 开头", true
		}
		maxChars := clampInt(getNum(input, "max_chars"), webReadMaxCharsDefault, webReadMaxCharsMin, webReadMaxCharsMax)
		if ts.client == nil {
			return "失败:浏览器底座未就绪", true
		}
		pi, err := ts.client.NewChromePage(ctx, readURL, "domcontentloaded")
		if err != nil {
			return "失败:" + err.Error(), true
		}
		ts.OpenedPages = append(ts.OpenedPages, pi.PageID)
		br, err := ts.client.BatchEval(ctx, pi.PageID, []EvalItem{
			{Label: "title", Params: map[string]any{"expression": "document.title"}},
			{Label: "body", Params: map[string]any{"expression": fmt.Sprintf(
				`(function(){var t=(document.body&&document.body.innerText)||'';return t.slice(0,%d);})()`, maxChars)}},
		})
		if err != nil {
			return "失败:" + err.Error(), true // 页已记入 OpenedPages,RunMission defer 兜底回收。
		}
		title, body := "", ""
		if len(br.Results) > 0 && br.Results[0].OK {
			title = evalStringValue(br.Results[0].Result)
		}
		if len(br.Results) > 1 && br.Results[1].OK {
			body = evalStringValue(br.Results[1].Result)
		}
		return truncate(fmt.Sprintf("page_id=%s title=%s 正文如下:\n%s", pi.PageID, title, body), 3000), false
	case ToolWebSearch:
		query := strings.TrimSpace(getStr("query"))
		if query == "" {
			return "失败:query 不能为空", true
		}
		if utf8.RuneCountInString(query) > 100 {
			return "失败:query 超过 100 字上限", true
		}
		count := clampInt(getNum(input, "count"), webSearchCountDefault, webSearchCountMin, webSearchCountMax)
		if ts.client == nil {
			return "失败:浏览器底座未就绪", true
		}
		pi, err := ts.client.NewChromePage(ctx,
			"https://cn.bing.com/search?q="+url.QueryEscape(query)+"&ensearch=0", "domcontentloaded")
		if err != nil {
			return "失败:" + err.Error(), true
		}
		// Bing SERP 提取表达式(#b_results > li.b_algo;批次77 §3.1 内置):
		// 0 结果时返回 __no_results__ + 正文前 600 字兜底,促 Agent 自适应。
		expr := fmt.Sprintf(`(function(){var out=[];document.querySelectorAll('#b_results > li.b_algo').forEach(function(li){var a=li.querySelector('h2 a');if(!a)return;var p=li.querySelector('.b_caption p')||li.querySelector('p');out.push({title:(a.innerText||'').trim(),url:a.href,snippet:p?(p.innerText||'').trim().slice(0,120):''});});if(out.length===0){out.push({title:'__no_results__',url:location.href,snippet:((document.body&&document.body.innerText)||'').slice(0,600)});}return JSON.stringify(out.slice(0,%d));})()`, count)
		br, err := ts.client.BatchEval(ctx, pi.PageID, []EvalItem{
			{Label: "serp", Params: map[string]any{"expression": expr}},
		})
		if err != nil {
			_ = ts.client.CloseChromePage(ctx, pi.PageID) // best-effort 回收 SERP 页。
			return "失败:" + err.Error(), true
		}
		text := ""
		if len(br.Results) > 0 && br.Results[0].OK {
			text = evalStringValue(br.Results[0].Result)
		}
		// SERP 页提取完立即关闭(不记入 OpenedPages;批次77 §3.1);
		// 关页失败则登记待回收,交 RunMission defer 再试。
		if err := ts.client.CloseChromePage(ctx, pi.PageID); err != nil {
			ts.OpenedPages = append(ts.OpenedPages, pi.PageID)
			return truncate(fmt.Sprintf("%s\n(SERP 页回收失败已登记待回收: %s)", text, err.Error()), 3000), false
		}
		return truncate(text, 3000), false
	case ToolWebDone:
		payload, err := ValidatePayload(ts.symbolsPerCategory, input["payload"])
		if err != nil {
			return "失败:payload 校验拒绝 — " + err.Error(), true
		}
		ts.Done = payload
		return fmt.Sprintf("payload 已接受:category=%s quotes=%d news=%d", payload.Category, len(payload.Quotes), len(payload.News)), false
	default:
		return "未知工具: " + name, true
	}
}

// ValidatePayload 校验 web_done.payload(契约 §3.4):
//   - category 必须是 6 个 mission 类目之一(未知 category 拒绝整包);
//   - source 非空,且按 missionSourceMaxRunes rune 截断(兜底:看板来源列直出);
//   - quotes 单条校验:symbol/name 非空、price>0 有限、change_pct∈[-100,100] 有限
//     (脏条丢弃,不整包失败);超 symbolsPerCategory 截断;
//   - news 单条:title 非空(脏条丢弃);ID 留空时由 FeedStore 计算。
//
// 全部脏条被丢弃且 quotes 为空时不拒绝(允许纯新闻 mission)。
func ValidatePayload(symbolsPerCategory int, raw any) (*MissionPayload, error) {
	if symbolsPerCategory <= 0 {
		symbolsPerCategory = 12
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("payload 不是合法 JSON: %w", err)
	}
	var p MissionPayload
	if err := json.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("payload 解析失败: %w", err)
	}
	if !IsMissionCategory(p.Category) {
		return nil, fmt.Errorf("未知 category %q(必须是 a_share|hk|us|metal|futures|crypto|macro|news)", p.Category)
	}
	if p.Source == "" {
		return nil, fmt.Errorf("source 不能为空(采集来源站点名)")
	}
	// 兜底:source 语义是站点名(看板来源列直出),Agent 塞长解释时按 rune
	// 截断防污染(批次77 E2E 实机发现:「财联社·电报(cls.cn/telegraph,发布时间
	// 为页面标注的北京时间…换算Unix秒)」这类长串)。
	p.Source = truncate(p.Source, missionSourceMaxRunes)
	quotes := make([]RealtimeQuote, 0, len(p.Quotes))
	for _, q := range p.Quotes {
		if validQuote(q) {
			quotes = append(quotes, q)
		}
	}
	if len(quotes) > symbolsPerCategory {
		quotes = quotes[:symbolsPerCategory]
	}
	p.Quotes = quotes
	news := make([]RealtimeNews, 0, len(p.News))
	for _, n := range p.News {
		if strings.TrimSpace(n.Title) != "" {
			news = append(news, n)
		}
	}
	p.News = news
	return &p, nil
}

// parseEvalItems 解析 web_eval items(1-64 条,逐条取 expression)。
func parseEvalItems(raw any) ([]EvalItem, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil, fmt.Errorf("items 必须是 1-64 条的数组")
	}
	if len(arr) > 64 {
		return nil, fmt.Errorf("items 超过 64 条上限(got %d)", len(arr))
	}
	out := make([]EvalItem, 0, len(arr))
	for i, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("items[%d] 不是对象", i)
		}
		expr, _ := m["expression"].(string)
		if expr == "" {
			return nil, fmt.Errorf("items[%d].expression 为空", i)
		}
		params := map[string]any{"expression": expr}
		if b, ok := m["await_promise"].(bool); ok && b {
			params["await_promise"] = true
		}
		out = append(out, EvalItem{Label: getMapStr(m, "label"), Params: params})
	}
	return out, nil
}

func getMapStr(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// getNum 取 input[k] 的数值(LLM JSON number 解出为 float64;缺省/非数值 → 0)。
func getNum(input map[string]any, k string) int {
	switch v := input[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// clampInt 数值钳制:v<=0(缺省)取 def,再夹到 [min,max](批次77 §3.1)。
func clampInt(v, def, min, max int) int {
	if v <= 0 {
		v = def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// evalStringValue 从 EvalItemResult.Result(CDP RemoteObject JSON,
// 形如 {"type":"string","value":"..."})提取字符串 value;形状不符时退回
// 原始 JSON 文本(web_read/web_search 提取结果展示用,批次77 §3.1)。
func evalStringValue(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	var ro struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(r, &ro); err == nil {
		if s, ok := ro.Value.(string); ok {
			return s
		}
	}
	return strings.TrimSpace(string(r))
}

// removeOpenedPage 从 OpenedPages 移除已显式关闭的 page_id(幂等回收去重;
// 批次77 §3.3-4)。
func (ts *Toolset) removeOpenedPage(pageID string) {
	out := ts.OpenedPages[:0]
	for _, id := range ts.OpenedPages {
		if id != pageID {
			out = append(out, id)
		}
	}
	ts.OpenedPages = out
}

// truncate 按 rune 截断(工具结果回灌 LLM 防上下文膨胀)。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…(截断)"
}
