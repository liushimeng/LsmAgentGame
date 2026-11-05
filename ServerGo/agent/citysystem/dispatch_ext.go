// Package citysystem — dispatch_ext.go: 批次84 新增 / 扩展工具的派发实现
// (2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/
//       01-方案设计.md §4.2(工具表) / §4.3.2(Agent 双向通讯) / §4.4(指标历史)。
//
// 覆盖批次84 的 6 个新工具(search_scenarios / query_metric_history /
// query_districts / query_surveys / apply_display / ask_agent)与 3 个扩展工具
// (list_scenarios 维度索引 / query_city_state 扩 scope / list_agents 双层目录)。
//
// 纪律(与 dispatch.go 一致):
//   - 一律 isErr=true + "失败:" + err.Error();*errcode.Error 的 350xx 码随
//     Error() 保留在文本里回灌给模型;
//   - **只读**:查询类工具不改任何房间状态;
//   - **不编造**:子系统缺失时明确回「未启用」,绝不凑数。
package citysystem

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"
)

// listScenarios 重实现(批次84):无 group ⇒ 维度索引;传 group ⇒ 该组条目
// (limit 缺省 60,上限 200);未知组名 ⇒ 失败文本(35064)回灌模型。
func listScenarios(group string, limit int) (string, bool) {
	group = strings.TrimSpace(group)
	if group == "" {
		return ScenarioDimensionIndexText(), false
	}
	if !scenarioGroupKnown(group) {
		// 35064(ErrVirtualCityScenarioGroupUnknown);文案沿用批次81 的
		// 「未知分组 X,可用分组:…」口径,既有测试锚点不变。
		return "失败:" + errcode.CodeMsg(errcode.ErrVirtualCityScenarioGroupUnknown,
			fmt.Sprintf("未知分组 %s,可用分组:%s", group, strings.Join(knownScenarioGroups(), "|"))).Error(), true
	}
	if limit <= 0 {
		limit = scenarioGroupListLimitDefault
	}
	if limit > scenarioSearchLimitMax {
		limit = scenarioSearchLimitMax
	}
	specs := virtual_city.ScenarioCatalogSearch("", group, limit)
	if len(specs) == 0 {
		return fmt.Sprintf("分组 %s 暂无场景条目。", group), false
	}
	// 该组真实条目数(维度索引口径;不随 limit 截断)。
	total := len(specs)
	for _, d := range virtual_city.ScenarioDimensionIndex() {
		if d.Group == group {
			total = d.Count
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] 共 %d 条(显示 %d 条):\n", group, total, len(specs))
	for _, spec := range specs {
		fmt.Fprintf(&b, "- %s %s(默认 L%d):%s", spec.Kind, spec.Name,
			spec.DefaultMagnitude, spec.Description)
		if len(spec.Params) > 0 {
			fmt.Fprintf(&b, " 参数:%s", strings.Join(spec.Params, "|"))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), false
}

// searchScenarios 检索场景目录(批次84 §4.1.4):query / group / limit。
// 未知组名 ⇒ 35064;无命中 ⇒ 明确提示(不空手回)。
func searchScenarios(query, group string, limit int) (string, bool) {
	specs, ec := virtual_city.ScenarioCatalogSearchErr(query, group, limit)
	if ec != nil {
		return "失败:" + ec.Error(), true
	}
	if len(specs) == 0 {
		return fmt.Sprintf("检索无命中(query=%q, group=%q)。可换关键词或去掉 group 再试。",
			query, group), false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "命中 %d 条(按分组顺序):\n", len(specs))
	for _, spec := range specs {
		fmt.Fprintf(&b, "- %s %s[%s](默认 L%d):%s",
			spec.Kind, spec.Name, spec.Group, spec.DefaultMagnitude, spec.Description)
		if len(spec.Params) > 0 {
			fmt.Fprintf(&b, " 参数:%s", strings.Join(spec.Params, "|"))
		}
		b.WriteByte('\n')
	}
	b.WriteString("注入请用 inject_scenario(kind=上述 kind)。")
	return b.String(), false
}

// queryMetricHistory 查询指标时间序列(批次84 §4.4)。
// metric 非法 ⇒ 失败文本;历史为空 ⇒ 明确「尚无历史」(不编造)。
func (a *Agent) queryMetricHistory(metric string, months int) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	metric = strings.ToLower(strings.TrimSpace(metric))
	if metric == "" {
		metric = "cpi"
	}
	if !metricHistoryKnown(metric) {
		return fmt.Sprintf("失败:metric 只能是 %s", strings.Join(metricHistoryMetrics, "|")), true
	}
	snaps := a.room.SystemMetricHistory(metric, months)
	if len(snaps) == 0 {
		return fmt.Sprintf("指标 %s 尚无历史采样(月结未完成或该指标未启用)。", metric), false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "指标 %s 近 %d 条(旧→新):\n", metric, len(snaps))
	for _, s := range snaps {
		fmt.Fprintf(&b, "M%d %s\n", s.Month, metricSnapshotValue(metric, s))
	}
	if len(snaps) < months && months > 0 {
		fmt.Fprintf(&b, "(历史不足 %d 条,已给出全部 %d 条)", months, len(snaps))
	}
	return strings.TrimRight(b.String(), "\n"), false
}

// metricHistoryKnown 报告 metric 是否合法(与 virtual_city.SystemMetricNames 同源)。
func metricHistoryKnown(metric string) bool {
	for _, m := range metricHistoryMetrics {
		if m == metric {
			return true
		}
	}
	return false
}

// metricSnapshotValue 按 metric 取单指标数值并格式化(带量纲)。
//
// SystemMetricSnapshot 是**单指标视图**(上游只填请求的 metric 字段),
// 故按 metric 分支取字段;百分比量纲(cpi/unemployment)转百分数。
func metricSnapshotValue(metric string, s virtual_city.SystemMetricSnapshot) string {
	switch metric {
	case "cpi":
		return fmt.Sprintf("%.1f%%", s.CPI*100)
	case "unemployment":
		return fmt.Sprintf("%.1f%%", s.Unemployment*100)
	case "wage":
		return fmt.Sprintf("%.0f 元", s.Wage)
	case "stock":
		return fmt.Sprintf("%.1f", s.Stock)
	case "gold":
		return fmt.Sprintf("%.1f", s.Gold)
	case "house":
		return fmt.Sprintf("%.3f", s.House)
	case "treasury":
		return fmt.Sprintf("%.0f 元", s.Treasury)
	case "satisfaction":
		return fmt.Sprintf("%+.1f", s.Satisfaction)
	default:
		return "(未知指标)"
	}
}

// queryDistricts 查询城区目录(批次84 §4.2 #6):id / 名称 / 人口 /
// 房价指数 / 产业 / 设施。支持 district 过滤(id 或名称前缀)。
//
// 数据源(全部真实状态,不编造):
//   - id / 名称 / 坐标 / 基准价:virtual_city.DistrictDefs(静态表);
//   - 房价指数:World.Market.DistrictIdx;
//   - 人口:World.Players 中**当前位于该区**的存活居民数;
//   - 产业:World.Clusters 的主导产业(该区集群);
//   - 设施:PublicSvc 为**全市级**口径(引擎无城区级设施台账,如实说明)。
func (a *Agent) queryDistricts(filter string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	filter = strings.TrimSpace(filter)
	a.room.MuLock()
	w := a.room.EngineLocked()
	type row struct {
		id      string
		name    string
		pop     int
		idx     float64
		hasIdx  bool
		industry []string
	}
	rows := make([]row, 0, virtual_city.DistrictCount)
	for _, d := range virtual_city.DistrictDefs {
		if filter != "" &&
			d.ID != filter && !strings.HasPrefix(d.NameCN, filter) &&
			!strings.HasPrefix(d.ID, filter) {
			continue
		}
		r := row{id: d.ID, name: d.NameCN}
		if w != nil {
			if w.Market != nil {
				if v, ok := w.Market.DistrictIdx[d.ID]; ok {
					r.idx, r.hasIdx = v, true
				}
			}
			for _, p := range w.Players {
				if p != nil && p.Alive && p.District == d.ID {
					r.pop++
				}
			}
			for _, c := range w.Clusters {
				if c != nil && c.DistrictID == d.ID {
					r.industry = append(r.industry, c.Industries...)
				}
			}
		}
		rows = append(rows, r)
	}
	a.room.MuUnlock()

	if len(rows) == 0 {
		return fmt.Sprintf("未找到匹配的城区(filter=%q)。", filter), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "城区目录 %d 个:\n", len(rows))
	for _, r := range rows {
		idxText := "未建模"
		if r.hasIdx {
			idxText = fmt.Sprintf("%.3f", r.idx)
		}
		industry := "未标注"
		if len(r.industry) > 0 {
			industry = strings.Join(uniqueStrings(r.industry), "/")
		}
		fmt.Fprintf(&b, "- %s %s | 人口 %d | 房价指数 %s | 产业 %s\n",
			r.id, r.name, r.pop, idxText, industry)
	}
	b.WriteString("设施:公共服务为全市级口径(教育/医疗/养老/住房可负担/就业),引擎无城区级设施台账;见 query_city_state scope=public_health。")
	return b.String(), false
}

// uniqueStrings 去重保序。
func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// querySurveys 查询社会调研列表 + 各选项占比聚合(批次84 §4.2 #9)。
func (a *Agent) querySurveys() (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	surveys := a.room.ListSurveys()
	if len(surveys) == 0 {
		return "暂无社会调研记录。", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "社会调研 %d 条:\n", len(surveys))
	for _, sv := range surveys {
		status := "进行中"
		if sv.Status == "closed" {
			status = "已结算"
		}
		fmt.Fprintf(&b, "- %s [%s] %s(发起 M%d,截止 M%d)\n",
			sv.ID, status, sv.Question, sv.LaunchMonth, sv.DeadlineMonth)
		for i, opt := range sv.Options {
			if sv.Result != nil && i < len(sv.Result.Percents) {
				pct := sv.Result.Percents[i] * 100
				cnt := 0
				if i < len(sv.Result.Counts) {
					cnt = sv.Result.Counts[i]
				}
				fmt.Fprintf(&b, "    %c %s — %.0f%%(%d 票)\n", rune('A'+i), opt, pct, cnt)
			} else {
				fmt.Fprintf(&b, "    %c %s — 未结算\n", rune('A'+i), opt)
			}
		}
		if sv.Result != nil {
			if sv.Result.Total > 0 {
				fmt.Fprintf(&b, "    共 %d 票\n", sv.Result.Total)
			}
			if len(sv.Result.TopReasons) > 0 {
				fmt.Fprintf(&b, "    居民理由:%s\n", strings.Join(sv.Result.TopReasons, ";"))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), false
}

// allExtendedScopes 渲染批次84 新增 7 个子系统的**全部**摘要(scope=all 用)。
// 任一子系统未启用时按各 scope 的口径明确「未启用」(不编造)。
func (a *Agent) allExtendedScopes() string {
	if a.room == nil {
		return ""
	}
	a.room.MuLock()
	w := a.room.EngineLocked()
	var b strings.Builder
	for _, scope := range []string{
		"infrastructure", "public_health", "environment", "security",
		"transport", "culture", "daily",
	} {
		if s := renderExtendedScopeLocked(scope, w); s != "" {
			b.WriteString(s)
			b.WriteString("\n\n")
		}
	}
	a.room.MuUnlock()
	return strings.TrimRight(b.String(), "\n")
}

// queryCityStateExtended 批次84 新增的 7 个 scope(契约 §4.2 #4)。
//
// 原则:**对应子系统未启用时明确返回「未启用」,绝不编造数据**。逐 scope:
//
//	infrastructure  产业链(SupplyChain)+ 产业集群(Clusters)     → 缺则未启用
//	public_health   公共服务 PublicSvc(医疗/教育/养老/住房/就业) → 缺则未启用
//	environment     引擎无环境生态子系统                          → 恒「未启用」
//	security        引擎无治安子系统(Regulators 是市场监管)      → 恒「未启用」
//	transport       交通类商品价格指数(Goods)+ 产业链层级        → 缺则未启用
//	culture         教育质量(PublicSvc)+ 教育类商品价格指数      → 缺则未启用
//	daily           消费篮子 8 类价格指数(Goods)+ 社会结构(Society) → 缺则未启用
//
// 持 r.mu 只读快照;**不回调房间**(§92a)。
func (a *Agent) queryCityStateExtended(scope string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	a.room.MuLock()
	w := a.room.EngineLocked()
	text := renderExtendedScopeLocked(scope, w)
	a.room.MuUnlock()
	if text == "" {
		return fmt.Sprintf("失败:scope 只能是 %s", strings.Join(queryCityStateScopes, "|")), true
	}
	return text, false
}

// renderExtendedScopeLocked 渲染一个批次84 新增 scope 的状态文本
// (持锁只读;w 可为 nil)。未知 scope 返回空串。
func renderExtendedScopeLocked(scope string, w *virtual_city.World) string {
	var b strings.Builder
	header := func(name string) { fmt.Fprintf(&b, "%s(M%d):\n", name, monthOf(w)) }
	switch scope {
	case "infrastructure":
		header("基础设施")
		if w != nil && w.SupplyChain != nil {
			n := len(w.SupplyChain.Nodes)
			fmt.Fprintf(&b, "产业链节点 %d 个;层级价格指数 ", n)
			for i, v := range w.SupplyChain.PriceIndex {
				if i > 0 {
					b.WriteString(" / ")
				}
				fmt.Fprintf(&b, "%.2f", v)
			}
			b.WriteString("\n")
		} else {
			b.WriteString("产业链子系统未启用\n")
		}
		if w != nil && len(w.Clusters) > 0 {
			firms := 0
			for _, c := range w.Clusters {
				if c != nil {
					firms += c.Firms
				}
			}
			fmt.Fprintf(&b, "产业集群 %d 个,入驻企业 %d 家\n", len(w.Clusters), firms)
		} else {
			b.WriteString("产业集群子系统未启用\n")
		}
	case "public_health":
		header("公共卫生")
		if w != nil && w.PublicSvc != nil {
			fmt.Fprintf(&b, "医疗质量 %.2f | 教育质量 %.2f | 养老水平 %.2f | 住房可负担 %.2f | 就业服务 %.2f\n",
				w.PublicSvc.MedQuality, w.PublicSvc.EduQuality, w.PublicSvc.PensionLevel,
				w.PublicSvc.HousingAfford, w.PublicSvc.Employment)
			if w.PublicSvc.Med != nil {
				fmt.Fprintf(&b, "公共医疗累积投入 %.0f 万元\n", w.PublicSvc.Med.PublicMedStock)
			}
		} else {
			b.WriteString("公共服务(医疗)子系统未启用\n")
		}
	case "environment":
		header("环境生态")
		b.WriteString("环境生态子系统未启用(当前引擎未建模环境/生态维度;可注入「环境生态」维度场景观察影响)\n")
	case "security":
		header("公共安全")
		b.WriteString("公共安全(治安)子系统未启用(当前引擎未建模治安维度)\n")
		if w != nil && w.Regulators != nil {
			fmt.Fprintf(&b, "参考:市场监管(证券/反垄断/消协/隐私)累计罚款 %d 元,上月警告 %d 次\n",
				w.Regulators.FinesTotalCNY, w.Regulators.LastMonthWarnings)
		}
	case "transport":
		header("交通物流")
		if w != nil && w.Goods != nil {
			if it, ok := w.Goods.Items["transport"]; ok && it != nil {
				fmt.Fprintf(&b, "交通类商品价格指数 %.1f(环比 %+.1f%%)\n", it.PriceIdx, it.MomChange*100)
			} else {
				b.WriteString("交通类商品价格未启用\n")
			}
		} else {
			b.WriteString("商品市场子系统未启用\n")
		}
		if w != nil && w.SupplyChain != nil {
			fmt.Fprintf(&b, "物流依托产业链 %d 个节点(层级价格指数见 scope=infrastructure)\n", len(w.SupplyChain.Nodes))
		}
	case "culture":
		header("文化教育")
		if w != nil && w.PublicSvc != nil {
			fmt.Fprintf(&b, "教育质量 %.2f\n", w.PublicSvc.EduQuality)
			if w.PublicSvc.Edu != nil {
				fmt.Fprintf(&b, "公共教育累积投入 %.0f 万元\n", w.PublicSvc.Edu.PublicEduStock)
			}
		} else {
			b.WriteString("公共服务(教育)子系统未启用\n")
		}
		if w != nil && w.Goods != nil {
			if it, ok := w.Goods.Items["education"]; ok && it != nil {
				fmt.Fprintf(&b, "教育类商品价格指数 %.1f\n", it.PriceIdx)
			}
		}
	case "daily":
		header("民生日常")
		if w != nil && w.Goods != nil {
			fmt.Fprintf(&b, "CPI 同比 %.1f%% 环比 %.1f%%\n", w.Goods.CPIYoY*100, w.Goods.CPIMom*100)
			for _, it := range goodsSorted(w.Goods) {
				fmt.Fprintf(&b, "  %s 价格指数 %.1f\n", it.ID, it.PriceIdx)
			}
		} else {
			b.WriteString("商品市场子系统未启用\n")
		}
		if w != nil && w.Society != nil {
			fmt.Fprintf(&b, "财富基尼 %.3f | 总财富 %.0f 元 | 圈层 %d/%d/%d\n",
				w.Society.Gini, float64(w.Society.TotalWealth),
				w.Society.Circles[0], w.Society.Circles[1], w.Society.Circles[2])
		} else {
			b.WriteString("社会结构统计未启用\n")
		}
	default:
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// monthOf 返回世界主钟月份(w 为 nil → 0)。
func monthOf(w *virtual_city.World) int {
	if w == nil {
		return 0
	}
	return w.Month
}

// goodsSorted 按 id 升序输出 8 类消费篮子(map 迭代序不确定;确定性渲染)。
func goodsSorted(g *virtual_city.GoodsMarket) []*virtual_city.GoodsItem {
	if g == nil {
		return nil
	}
	ids := make([]string, 0, len(g.Items))
	for id := range g.Items {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	out := make([]*virtual_city.GoodsItem, 0, len(ids))
	for _, id := range ids {
		out = append(out, g.Items[id])
	}
	return out
}

// askAgentTimeout 是 ask_agent 等待目标 Agent 回复的上限(契约 §4.3.2)。
const askAgentTimeout = 30 * time.Second

// askAgent 双向提问:向目标 Agent 发消息并**等待回复**(批次84 §4.3.2)。
//
//   - 目标未注册 ⇒ busFailure 映射 [35061];
//   - 超时(30s)⇒ [35062] 对方未在时限内回应;
//   - 回包超长 ⇒ 截断到 tool_result_limit 并标注 [35063];
//   - 正常 ⇒ 回灌回复内容。
//
// 等待实现:hub.Send 在独立 goroutine 执行(handler 在调用方 goroutine),
// 以 select 限时返回 —— 目标 handler 挂住时不拖垮本轮。
func (a *Agent) askAgent(ctx context.Context, targetAgent, message string) (string, bool) {
	if a.hub == nil {
		return "失败:Agent 总线未绑定", true
	}
	targetAgent = strings.TrimSpace(targetAgent)
	if targetAgent == "" {
		return "失败:缺少目标 Agent(target_agent)", true
	}
	if strings.TrimSpace(message) == "" {
		return "失败:消息内容为空", true
	}
	if targetAgent == string(agentroot.AgentClassCitySystem) {
		return "失败:不能向自己(本 Agent)提问,请换目标", true
	}
	type sendResult struct {
		v   any
		err error
	}
	timeout := a.askTimeout
	if timeout <= 0 {
		timeout = askAgentTimeout
	}
	ch := make(chan sendResult, 1)
	go func() {
		v, err := a.hub.Send(ctx, a.RoomID, string(agentroot.AgentClassCitySystem),
			targetAgent, BusTypeMessage, map[string]any{"text": message})
		ch <- sendResult{v, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return busFailure(r.err), true
		}
		reply := agentReplyText(r.v)
		if reply == "" {
			return fmt.Sprintf("%s 未返回文字回复。", targetAgent), false
		}
		limit := a.cfg.ToolResultLimit
		if len([]rune(reply)) > limit {
			trunc := truncateRunes(reply, limit)
			return "失败:" + errcode.CodeMsg(errcode.ErrVirtualCityAgentReplyTooLong,
				fmt.Sprintf("目标 %s 回包超长,已截断", targetAgent)).Error() + "\n" + trunc, true
		}
		return reply, false
	case <-time.After(timeout):
		return "失败:" + errcode.CodeMsg(errcode.ErrVirtualCityAgentNoReply,
			fmt.Sprintf("目标 %s 未在时限内回应", targetAgent)).Error(), true
	case <-ctx.Done():
		return "失败:" + errcode.CodeMsg(errcode.ErrVirtualCityAgentNoReply,
			fmt.Sprintf("目标 %s 未在时限内回应(本轮已取消)", targetAgent)).Error(), true
	}
}

// agentReplyText 把总线返回值转成回包文本:string 原样;map 取 text/reply;
// 其它形态 JSON 序列化兜底。
func agentReplyText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	case map[string]any:
		for _, k := range []string{"text", "reply", "message"} {
			if s, ok := t[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return strings.TrimSpace(fmt.Sprint(v))
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// listAgentsExtended 返回**类级 + 房间级** Agent 目录(批次84 §4.2 #17)。
// 每行:类名 | 作用域 | 支持的消息类型 | 一句话能力描述。
func (a *Agent) listAgentsExtended() (string, bool) {
	if a.hub == nil {
		return "失败:Agent 总线未绑定", true
	}
	list := a.hub.Describe(a.RoomID)
	if len(list) == 0 {
		return "当前无已注册的 Agent。", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已注册 Agent(%d):\n", len(list))
	for _, d := range list {
		scope := "类级"
		if d.Scope == citybus.ScopeRoom {
			scope = "房间级"
		}
		types := strings.Join(d.MessageTypes, "/")
		if types == "" {
			types = "(未声明)"
		}
		desc := d.Description
		if desc == "" {
			desc = "(未登记描述)"
		}
		fmt.Fprintf(&b, "- %s | %s | 消息类型 %s | %s\n", d.ClassName, scope, types, desc)
	}
	b.WriteString("ask_agent / send_agent_message 的 target_agent 请用上述类名。")
	return b.String(), false
}
