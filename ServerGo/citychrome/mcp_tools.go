// Package citychrome — mcp_tools.go: MCP_Web_Use 工具集(契约 §4)。
//
// 11 个工具:web_open / web_control / web_eval / web_look / web_list /
// web_close 走 go-web-debug-tool HTTP;web_done 是本地工具(不走 HTTP),
// 结束 mission 并带 §3.4 payload schema 校验(quotes 上限/数值合法/单条
// 脏数据丢弃/未知 category 拒绝);批次77 §3.1 新增 3 个 —— web_plan 是
// 本地工具(记录结构化规划),web_read 一步完成「打开+读正文」,web_search
// 必应中国 SERP 只读提取(固定源全失败后的发现兜底,自动关页);批次78 §3.2
// 新增 web_parse(本地确定性解析),web_read 增 wait_selector/wait_ms/parse
// 三参(等 SPA 元素 + 结构化报价前置),web_control 白名单解锁 wait,
// web_search 增 engine(bing|baidu) + 反爬识别。
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
	"regexp"
	"strings"
	"unicode/utf8"

	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// MCP 工具名(契约 §4 表;批次77 §3.1 追加 3 个,7→10;批次78 §3.2 追加
// web_parse,10→11)。
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

	// 批次78 §3.2 新工具(纯本地,不走浏览器)。
	ToolWebParse = "web_parse" // 确定性解析(配方见 recipes.go)
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

// 批次78 §3.2 新增参数的钳制常量。
const (
	// webReadWaitSelectorMaxRunes 是 wait_selector 的 rune 上限。
	webReadWaitSelectorMaxRunes = 200
	// webReadWaitMsMax 是 wait_ms 的上限(15s)。
	webReadWaitMsMax = 15000
	// webReadWaitTimeoutDefaultMs 是 wait_selector 的缺省等待上限(15s);
	// 与 wait_ms 同时给时取较大者(显式 wait_ms 优先,否则用本缺省值)。
	webReadWaitTimeoutDefaultMs = 15000
	// webReadResultMax 是 web_read 整体结果的 rune 上限(结构化块优先保留)。
	webReadResultMax = 4000
	// webParseTextMaxRunes 是 web_parse text 的 rune 上限。
	webParseTextMaxRunes = 20000
	// visitedURLCap 是 Toolset.VisitedURLs 的内部条数上限(写盘时再按
	// MissionRecord.Visited 的 ≤6 条口径裁剪)。
	visitedURLCap = 16
)

// webSearchBlockRe 是 SERP 反爬识别关键词(批次78 §3.2/§6:只用这 4 个,
// **不含裸 robot** —— 正常页面也常含该词,会误判)。
var webSearchBlockRe = regexp.MustCompile(`安全验证|请完成下方验证|403 Forbidden|unusual traffic`)

// missionSourceMaxRunes 是 web_done payload.source 的 rune 上限(兜底截断)。
// source 语义是站点名(看板「采集」子页来源列与新闻来源列直出),Agent 塞入
// 链接说明/时间口径解释等长串时按上限截断,防污染看板。
const missionSourceMaxRunes = 40

// webControlActions 是 web_control 的 action 白名单(契约 §4;服务端校验)。
// 批次78 §3.2:解锁 wait(底座 actWait:params.selector + params.timeout_ms,
// 或 params.duration_ms 静默等待)—— SPA 页面等元素出现。
var webControlActions = map[string]bool{
	"click": true, "right_click": true, "double_click": true, "hover": true,
	"scroll": true, "scroll_to": true,
	"key_press": true, "press_sequence": true,
	"input_text": true, "human_input": true, "clear_input": true,
	"upload_file": true, "select_option": true, "drag": true,
	"navigate": true, "back": true, "forward": true, "reload": true,
	"new_tab": true, "close_tab": true,
	"screenshot": true, "add_script": true, "eval_js": true,
	"wait": true,
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

	// VisitedURLs 记录本 mission 访问过的 URL(web_open / web_read;批次78
	// §3.3-5:mission 出口写入 MissionRecord.Visited,看板「采集」子页的
	// 「站点尝试」列直出)。新→旧去重,内部上限 visitedURLCap 条。
	VisitedURLs []string
}

// noteVisited 登记一次访问过的 URL(新→旧去重;批次78 §3.3-5)。
func (ts *Toolset) noteVisited(rawURL string) {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return
	}
	for _, v := range ts.VisitedURLs {
		if v == u {
			return
		}
	}
	ts.VisitedURLs = append([]string{u}, ts.VisitedURLs...)
	if len(ts.VisitedURLs) > visitedURLCap {
		ts.VisitedURLs = ts.VisitedURLs[:visitedURLCap]
	}
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
				"forward/reload/new_tab/close_tab/screenshot/add_script/eval_js/wait。" +
				"wait 用法:params 传 {selector:\"CSS 选择器\", timeout_ms:15000} 等元素出现(≤15s)," +
				"或 {duration_ms:2000} 静默等待;等元素出现也可直接用 web_read 的 wait_selector。",
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
			Description: "一步完成「打开页面 + 等元素 + 读正文 + 结构化解析」(批次78;简单读取优先用它," +
				"比 web_open+web_eval 省 1 步)。url 必须 http(s)://;max_chars 缺省 3000" +
				"(500..8000 钳制)。wait_selector(CSS 选择器,≤200 字)+wait_ms(≤15000)用于 SPA " +
				"页面等元素出现;parse 可指定确定性解析配方(auto 默认,tencent_quote/tencent_hf/sina_quote)。" +
				"返回 page_id/title/正文片段,行情页会额外返回【结构化解析】块(报价 JSON," +
				"**不要自己按分隔符切数字**);page_id 已登记,需要精细 DOM 提取时可用 web_eval 继续操作该页。",
			InputSchema: obj(map[string]any{
				"url": str("目标 URL,必须以 http:// 或 https:// 开头"),
				"max_chars": map[string]any{
					"type":        "integer",
					"minimum":     webReadMaxCharsMin,
					"maximum":     webReadMaxCharsMax,
					"description": "正文最多返回字符数(缺省 3000,钳制 500..8000)",
				},
				"wait_selector": str("可选:CSS 选择器,等该元素出现后再读正文(SPA 页面用,≤200 字)"),
				"wait_ms": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"maximum":     webReadWaitMsMax,
					"description": "可选:等待毫秒数(0..15000);与 wait_selector 同给时取较大者",
				},
				"parse": str("可选:确定性解析配方(auto|tencent_quote|tencent_hf|sina_quote);缺省 auto"),
			}, []string{"url"}),
		},
		{
			Name: ToolWebParse,
			Description: "确定性解析任意文本(批次78;本地工具,不走浏览器,不耗网络)。" +
				"把行情正文转成结构化报价 JSON:symbol/name/price/change_pct/currency/prev_close/time。" +
				"配方:auto(缺省,依次尝试腾讯行情 tencent_quote → 腾讯外盘 tencent_hf)、tencent_quote、" +
				"tencent_hf、sina_quote(仅显式指定时可用,不在 auto 链)。" +
				"未命中返回 matched=false(正文不含行情特征);涨跌幅由服务端自算,别再手工换算。",
			InputSchema: obj(map[string]any{
				"text":   str("待解析文本(非空,≤20000 字)"),
				"recipe": str("可选:解析配方(auto|tencent_quote|tencent_hf|sina_quote),缺省 auto"),
			}, []string{"text"}),
		},
		{
			Name: ToolWebSearch,
			Description: "搜索发现兜底(批次78):固定源与备选站全部失败时才用。" +
				"engine=bing(缺省,cn.bing.com)|baidu(www.baidu.com);打开 SERP → 提取结果" +
				"(title/url/snippet≤120 字)→ 自动关页;0 结果时返回页面前 600 字兜底。" +
				"命中安全验证/403 等反爬特征时直接报错(该源被拦截,请换备选站),不要当成 0 结果。" +
				"从结果里选公开可读页用 web_read 提取,仍禁止登录/验证码/表单提交。",
			InputSchema: obj(map[string]any{
				"query": str("检索词(非空,≤100 字)"),
				"count": map[string]any{
					"type":        "integer",
					"minimum":     webSearchCountMin,
					"maximum":     webSearchCountMax,
					"description": "返回条数(缺省 5,钳制 1..10)",
				},
				"engine": str("可选:搜索引擎 bing(缺省)|baidu"),
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
		ts.noteVisited(getStr("url"))                      // 批次78 §3.3-5:站点尝试轨迹。
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
		waitSelector := strings.TrimSpace(getStr("wait_selector"))
		if utf8.RuneCountInString(waitSelector) > webReadWaitSelectorMaxRunes {
			return fmt.Sprintf("失败:wait_selector 超过 %d 字上限", webReadWaitSelectorMaxRunes), true
		}
		waitMs := clampInt(getNum(input, "wait_ms"), 0, 0, webReadWaitMsMax)
		recipeInput := strings.TrimSpace(getStr("parse"))
		if recipeInput != "" && !IsParseRecipe(recipeInput) {
			return fmt.Sprintf("失败:未知解析配方 %q(可用:auto|tencent_quote|tencent_hf|sina_quote)", recipeInput), true
		}
		if ts.client == nil {
			return "失败:浏览器底座未就绪", true
		}
		pi, err := ts.client.NewChromePage(ctx, readURL, "domcontentloaded")
		if err != nil {
			return "失败:" + err.Error(), true
		}
		ts.OpenedPages = append(ts.OpenedPages, pi.PageID)
		ts.noteVisited(readURL) // 批次78 §3.3-5:站点尝试轨迹。
		// 等待(可选):SPA 页面等元素出现(底座 actWait)。失败不致命 ——
		// 继续读正文,失败原因随结果回灌,让 Agent 决定是否换源。
		waitNote := ""
		if waitSelector != "" || waitMs > 0 {
			params := map[string]any{}
			if waitSelector != "" {
				timeout := waitMs
				if timeout <= 0 {
					timeout = webReadWaitTimeoutDefaultMs
				}
				params["selector"] = waitSelector
				params["timeout_ms"] = timeout
			} else {
				params["duration_ms"] = waitMs
			}
			if _, werr := ts.client.ControlChromePage(ctx, pi.PageID, "wait", params); werr != nil {
				waitNote = fmt.Sprintf("(等待未成功:%s)", werr.Error())
				logger.L().Warn("citychrome web_read wait failed",
					zap.String("page_id", pi.PageID),
					zap.String("wait_selector", waitSelector),
					zap.Error(werr))
			}
		}
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
		// 确定性解析(批次78 §3.2):结构化块在前、正文在后;结构化块**不被截断**。
		effRecipe := recipeInput
		if effRecipe == "" {
			effRecipe = RecipeAuto
		}
		rows, resolved, matched := ParseRows(effRecipe, body)
		block := ""
		if matched || recipeInput != "" {
			block = FormatParsedBlock(effRecipe, resolved, rows, matched)
		}
		tail := fmt.Sprintf("%spage_id=%s title=%s 正文如下:\n%s", waitNote, pi.PageID, title, body)
		budget := webReadResultMax - utf8.RuneCountInString(block) - 1
		if budget < 200 {
			budget = 200
		}
		tail = truncate(tail, budget)
		if block == "" {
			return tail, false
		}
		return block + "\n" + tail, false
	case ToolWebParse:
		text := getStr("text")
		if strings.TrimSpace(text) == "" {
			return "失败:text 不能为空", true
		}
		if utf8.RuneCountInString(text) > webParseTextMaxRunes {
			return fmt.Sprintf("失败:text 超过 %d 字上限", webParseTextMaxRunes), true
		}
		recipe := strings.TrimSpace(getStr("recipe"))
		if recipe != "" && !IsParseRecipe(recipe) {
			return fmt.Sprintf("失败:未知解析配方 %q(可用:auto|tencent_quote|tencent_hf|sina_quote)", recipe), true
		}
		res, err := ParseText(text, recipe)
		if err != nil {
			return "失败:" + err.Error(), true
		}
		raw, err := json.Marshal(res)
		if err != nil {
			return "失败:解析结果序列化失败:" + err.Error(), true
		}
		return string(raw), false
	case ToolWebSearch:
		query := strings.TrimSpace(getStr("query"))
		if query == "" {
			return "失败:query 不能为空", true
		}
		if utf8.RuneCountInString(query) > 100 {
			return "失败:query 超过 100 字上限", true
		}
		count := clampInt(getNum(input, "count"), webSearchCountDefault, webSearchCountMin, webSearchCountMax)
		engine := strings.ToLower(strings.TrimSpace(getStr("engine")))
		if engine == "" {
			engine = "bing"
		}
		if engine != "bing" && engine != "baidu" {
			return "失败:engine 只支持 bing|baidu", true
		}
		if ts.client == nil {
			return "失败:浏览器底座未就绪", true
		}
		pi, err := ts.client.NewChromePage(ctx, searchURL(engine, query), "domcontentloaded")
		if err != nil {
			return "失败:" + err.Error(), true
		}
		br, err := ts.client.BatchEval(ctx, pi.PageID, []EvalItem{
			{Label: "serp", Params: map[string]any{"expression": serpExpr(engine, count)}},
			{Label: "body", Params: map[string]any{"expression": "(document.body&&document.body.innerText||'').slice(0,1500)"}},
		})
		if err != nil {
			_ = ts.client.CloseChromePage(ctx, pi.PageID) // best-effort 回收 SERP 页。
			return "失败:" + err.Error(), true
		}
		text, pageBody := "", ""
		if len(br.Results) > 0 && br.Results[0].OK {
			text = evalStringValue(br.Results[0].Result)
		}
		if len(br.Results) > 1 && br.Results[1].OK {
			pageBody = evalStringValue(br.Results[1].Result)
		}
		// 反爬识别(批次78 §3.2):命中安全验证/403/异常流量 ⇒ 明确报「被拦截」,
		// 不静默当 0 结果(否则 Agent 会误判为「无结果」继续重试)。
		// 只看页面正文(正文取不到时才回退看 SERP 兜底文本,避免结果摘要里的
		// 关键词造成误判)。
		blocked := webSearchBlockRe.MatchString(pageBody)
		if !blocked && pageBody == "" {
			blocked = webSearchBlockRe.MatchString(text)
		}
		if blocked {
			_ = ts.client.CloseChromePage(ctx, pi.PageID)
			logger.L().Warn("citychrome web_search blocked by anti-bot",
				zap.String("engine", engine), zap.String("query", query))
			return "失败:该源被拦截(安全验证/403/异常流量),请换备选站点,不要重试同一搜索引擎", true
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

// evalStringValue 从 EvalItemResult.Result 提取字符串正文。
//
// ⚠️ 底座(go-web-debug-tool)BatchEval 的 result 实际形态是**裸 JSON 字符串**
// (convertRemoteObject 对 string 直接返回 Go string):
//
//	{"index":0,"ok":true,"result":"v_sh000001=\"1~上证指数~…\""}
//
// RawMessage 里保留的是原始 JSON 编码(带外层引号 + \" 转义),必须先按
// JSON 字符串解一次,否则正则 `v_xxx="([^"]*)"` 永远匹配不到(确定性解析
// 彻底失效),且回灌 LLM 的正文全是转义噪声(批次78 实机复测修正)。
//
// 兼容形状:{"type":"string","value":"..."}(旧版 RemoteObject 包装)。
func evalStringValue(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	// 真实形状:裸 JSON 字符串。
	var s string
	if err := json.Unmarshal(r, &s); err == nil {
		return s
	}
	// 兼容形状:{"type":"string","value":"..."}。
	var ro struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(r, &ro); err == nil {
		if v, ok := ro.Value.(string); ok {
			return v
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

// searchURL 返回搜索引擎的 SERP URL(批次78 §3.2:engine=bing 缺省 / baidu)。
func searchURL(engine, query string) string {
	q := url.QueryEscape(query)
	if engine == "baidu" {
		return "https://www.baidu.com/s?wd=" + q + "&ie=utf-8"
	}
	return "https://cn.bing.com/search?q=" + q + "&ensearch=0"
}

// serpExpr 返回 SERP 提取表达式(只读 title/url/snippet≤120 字)。
// 0 结果时返回 __no_results__ + 正文前 600 字兜底,促 Agent 自适应。
func serpExpr(engine string, count int) string {
	if engine == "baidu" {
		return fmt.Sprintf(`(function(){var out=[];document.querySelectorAll('#content_left .result,#content_left .c-container').forEach(function(li){var a=li.querySelector('h3 a');if(!a)return;var p=li.querySelector('.c-abstract')||li.querySelector('.content-right_8Zs40')||li.querySelector('p');out.push({title:(a.innerText||'').trim(),url:a.href,snippet:p?(p.innerText||'').trim().slice(0,120):''});});if(out.length===0){out.push({title:'__no_results__',url:location.href,snippet:((document.body&&document.body.innerText)||'').slice(0,600)});}return JSON.stringify(out.slice(0,%d));})()`, count)
	}
	return fmt.Sprintf(`(function(){var out=[];document.querySelectorAll('#b_results > li.b_algo').forEach(function(li){var a=li.querySelector('h2 a');if(!a)return;var p=li.querySelector('.b_caption p')||li.querySelector('p');out.push({title:(a.innerText||'').trim(),url:a.href,snippet:p?(p.innerText||'').trim().slice(0,120):''});});if(out.length===0){out.push({title:'__no_results__',url:location.href,snippet:((document.body&&document.body.innerText)||'').slice(0,600)});}return JSON.stringify(out.slice(0,%d));})()`, count)
}
