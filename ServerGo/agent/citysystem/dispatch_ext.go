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
// 批次85(2026-11-05 §批次85 §3.1.2):query_city_state 全 13 scope 末尾
// 追加观察闭环两段(「本维度进行中」+「相关世界事件」;environment/security
// 重写,删除「子系统未启用」空答复);映射集中在 scopeDimension。
//
// 批次85 E2E 修复(F1):维度 scope 7 → 13(补市场与经济 / 政策与治理 /
// 社会与舆情 / 自然灾害 / 未知灾难 / 科技与新文明),13 个维度组全部可被
// query_city_state 直接查;scope 值 = ScenarioGroup* 维度名(模型从
// list_scenarios 维度索引看到的组名可直接当 scope 传),7 个英文旧键经
// cityStateScopeAliases 归一,零回归。
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
	"sort"
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
		virtual_city.ScenarioGroupInfrastructure, virtual_city.ScenarioGroupPublicHealth,
		virtual_city.ScenarioGroupEnvironment, virtual_city.ScenarioGroupSecurity,
		virtual_city.ScenarioGroupTransport, virtual_city.ScenarioGroupCulture,
		virtual_city.ScenarioGroupDailyLife,
	} {
		if s := renderExtendedScopeLocked(scope, w); s != "" {
			b.WriteString(s)
			b.WriteString("\n\n")
		}
	}
	a.room.MuUnlock()
	return strings.TrimRight(b.String(), "\n")
}

// queryCityStateExtended 批次84 新增 7 个 + 批次85 E2E F1 补齐 6 个的
// **13 个维度 scope**(契约 §4.2 #4;scope = ScenarioGroup* 维度名,
// 7 个英文别名经 canonicalCityStateScope 归一后同样可达)。
//
// 原则:**子系统缺失时如实说明「暂无该维度指标」,绝不编造数据**。逐 scope:
//
//	infrastructure  产业链(SupplyChain)+ 产业集群(Clusters)     → 缺则未启用
//	public_health   公共服务 PublicSvc(医疗/教育/养老/住房/就业) → 缺则未启用
//	environment     批次85 §3.1.2 重写:食品/杂项消费价格指数(供给传导
//	                最接近指标)+ 本维度活动场景计数;无数据明说「暂无该维度
//	                指标」,不再答「子系统未启用」
//	security        批次85 §3.1.2 重写:市场监管罚款(保留既有口径)+
//	                居民现金损失类场景;不再答「子系统未启用」
//	transport       交通类商品价格指数(Goods)+ 产业链层级        → 缺则未启用
//	culture         教育质量(PublicSvc)+ 教育类商品价格指数      → 缺则未启用
//	daily           消费篮子 8 类价格指数(Goods)+ 社会结构(Society) → 缺则未启用
//
// 批次85 §3.1.2:每个 scope 输出**末尾追加观察闭环两段**(「本维度进行中」
// +「相关世界事件」,锁外经只读桥 SystemDimensionStatus 取视图,§92a)。
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
	// 批次85 §3.1.2:末尾追加观察闭环两段(锁外调用,§92a)。
	if sec := a.dimensionObservation(scope); sec != "" {
		text += "\n" + sec
	}
	return text, false
}

// renderExtendedScopeLocked 渲染一个维度 scope 的状态文本(持锁只读;
// w 可为 nil)。scope 先经 canonicalCityStateScope 归一(7 个英文别名亦可)。
// 未知 scope 返回空串。
//
// 批次85 E2E F1:维度 scope 7 → 13(补市场与经济 / 政策与治理 /
// 社会与舆情 / 自然灾害 / 未知灾难 / 科技与新文明)。新增 6 个的实质内容
// 只用既有真实指标(§29.11 无数据明说「暂无该维度指标」,绝不编造):
//
//	市场与经济  CPI / 股指 / 黄金 / 八区房价(Goods+Market)     → 缺则暂无
//	政策与治理  国库 / 政策利率 / 市长选举(Treasury+CB+Election)→ 缺则暂无
//	社会与舆情  财富基尼 / 总财富 / 中位数 / 圈层(Society)+
//	            满意度偏置(Election.SentimentBias,最接近指标)→ 缺则暂无
//	自然灾害    食品价格指数 + 产业链层级价格指数(供给受损传导最接近
//	            指标)                                       → 缺则暂无
//	未知灾难    引擎无未知灾难专属指标,如实说明 + 活动场景计数
//	科技与新文明 引擎无科技研发子系统,如实说明 + 活动场景计数
func renderExtendedScopeLocked(scope string, w *virtual_city.World) string {
	scope = canonicalCityStateScope(scope)
	var b strings.Builder
	header := func(name string) { fmt.Fprintf(&b, "%s(M%d):\n", name, monthOf(w)) }
	switch scope {
	case virtual_city.ScenarioGroupInfrastructure:
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
	case virtual_city.ScenarioGroupPublicHealth:
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
	case virtual_city.ScenarioGroupEnvironment:
		// 批次85 §3.1.2:重写 —— 删除「子系统未启用」空答复,改为
		// 「活动场景 + 世界事件(见末尾观察闭环两段)+ 可得的最接近指标」。
		// 引擎无独立环境生态子系统;如实给出供给侧传导的最接近指标
		// (食品/杂项消费价格指数,真实 Goods 字段),没有就说「暂无该维度
		// 指标」,绝不编造。
		header("环境生态")
		foodIdx := false
		if w != nil && w.Goods != nil {
			if it := w.Goods.Items["food"]; it != nil {
				fmt.Fprintf(&b, "食品类商品价格指数 %.1f(环比 %+.1f%%)\n", it.PriceIdx, it.MomChange*100)
				foodIdx = true
			}
			if it := w.Goods.Items["misc"]; it != nil {
				fmt.Fprintf(&b, "杂项(含文娱旅游)商品价格指数 %.1f(环比 %+.1f%%)\n", it.PriceIdx, it.MomChange*100)
				foodIdx = true
			}
		}
		if !foodIdx {
			b.WriteString("暂无该维度指标(消费篮子数据不可用;生态影响经供给传导)\n")
		}
		n := 0
		if w != nil {
			for _, sc := range w.Scenarios {
				if sc != nil && virtual_city.ScenarioDimensionOf(sc.Kind) == virtual_city.ScenarioGroupEnvironment {
					n++
				}
			}
		}
		fmt.Fprintf(&b, "本维度活动场景 %d 个(明细见「本维度进行中」)\n", n)
	case virtual_city.ScenarioGroupSecurity:
		// 批次85 §3.1.2:重写 —— 删除「子系统未启用」空答复;保留既有
		// 市场监管罚款口径(零回归),补居民现金损失类场景
		// (CashDamageFrac>0,真实 profile 字段,跨维度如实列出)。
		header("公共安全")
		if w != nil && w.Regulators != nil {
			// 既有内容一字不动(仅删除其上方的「未启用」行)。
			fmt.Fprintf(&b, "参考:市场监管(证券/反垄断/消协/隐私)累计罚款 %d 元,上月警告 %d 次\n",
				w.Regulators.FinesTotalCNY, w.Regulators.LastMonthWarnings)
		} else {
			b.WriteString("暂无该维度指标(市场监管数据不可用)\n")
		}
		cash := virtual_city.CashDamageScenarioBriefs(w)
		if len(cash) == 0 {
			b.WriteString("居民现金损失类场景:无\n")
		} else {
			fmt.Fprintf(&b, "居民现金损失类场景 %d 个:\n", len(cash))
			for _, s := range cash {
				fmt.Fprintf(&b, "  - %s L%d(剩 %d 月)\n", s.Name, s.Magnitude, s.RemainingMonths)
			}
		}
	case virtual_city.ScenarioGroupTransport:
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
	case virtual_city.ScenarioGroupCulture:
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
	case virtual_city.ScenarioGroupDailyLife:
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
	case virtual_city.ScenarioGroupMarket:
		// 批次85 E2E F1:市场与经济 —— 复用 market scope 的真实指标
		// (CPI / 股指 / 黄金 / 八区房价)。
		header("市场与经济")
		ok := false
		if w != nil && w.Goods != nil {
			fmt.Fprintf(&b, "CPI 同比 %.1f%% 环比 %.1f%%\n", w.Goods.CPIYoY*100, w.Goods.CPIMom*100)
			ok = true
		}
		if w != nil && w.Market != nil {
			chg := ""
			if w.Market.LastStockRawDelta != 0 {
				chg = fmt.Sprintf("(%+.1f%%)", w.Market.LastStockRawDelta*100)
			}
			fmt.Fprintf(&b, "股指 %.1f%s | 黄金 %.1f\n", w.Market.StockIndex, chg, w.Market.GoldPrice)
			if n := len(w.Market.DistrictIdx); n > 0 {
				sum := 0.0
				for _, v := range w.Market.DistrictIdx {
					sum += v
				}
				fmt.Fprintf(&b, "八区房价指数 平均 %.3f\n", sum/float64(n))
			}
			ok = true
		}
		if !ok {
			b.WriteString("暂无该维度指标(市场/消费数据不可用)\n")
		}
		fmt.Fprintf(&b, "本维度活动场景 %d 个(明细见「本维度进行中」)\n",
			scenarioCountOfDimension(w, virtual_city.ScenarioGroupMarket))
	case virtual_city.ScenarioGroupPolicy:
		// 批次85 E2E F1:政策与治理 —— 复用 treasury / election scope 的
		// 真实指标(国库 / 政策利率 / 市长选举)。
		header("政策与治理")
		ok := false
		if w != nil && w.Treasury != nil {
			fmt.Fprintf(&b, "国库 ¥%d(上月收 %d / 支 %d,连续赤字 %d 月)\n",
				w.Treasury.Cash, w.Treasury.LastMonthRevenue,
				w.Treasury.LastMonthExpense, w.Treasury.DeficitRun)
			ok = true
		}
		if w != nil && w.CB != nil {
			fmt.Fprintf(&b, "政策利率 %.2f%% | 信贷窗口 %.2f\n", w.CB.PolicyRate*100, w.CB.CreditWindowFactor)
			ok = true
		}
		if w != nil && w.Election != nil {
			state := "未启用"
			if w.Election.Enabled {
				state = fmt.Sprintf("已启用(每 %d 月一届,上次 M%d,现任市长座位 %d)",
					w.Election.IntervalMonths, w.Election.LastElectionMonth, w.Election.MayorSeat)
			}
			fmt.Fprintf(&b, "市长选举:%s | 满意度偏置 %+.1f\n", state, w.Election.SentimentBias)
			ok = true
		}
		if !ok {
			b.WriteString("暂无该维度指标(财政/选举数据不可用)\n")
		}
		fmt.Fprintf(&b, "本维度活动场景 %d 个(明细见「本维度进行中」)\n",
			scenarioCountOfDimension(w, virtual_city.ScenarioGroupPolicy))
	case virtual_city.ScenarioGroupSociety:
		// 批次85 E2E F1:社会与舆情 —— 社会结构(Society)+ 满意度偏置
		// (Election.SentimentBias,舆情最接近指标;如实标注来源)。
		header("社会与舆情")
		ok := false
		if w != nil && w.Society != nil {
			fmt.Fprintf(&b, "财富基尼 %.3f | 总财富 %.0f 元 | 中位数财富 %.0f 元\n",
				w.Society.Gini, float64(w.Society.TotalWealth), float64(w.Society.MedianWealth))
			fmt.Fprintf(&b, "圈层 %d/%d/%d(生存/积累/自由)\n",
				w.Society.Circles[0], w.Society.Circles[1], w.Society.Circles[2])
			ok = true
		}
		if w != nil && w.Election != nil {
			fmt.Fprintf(&b, "满意度偏置 %+.1f(选举口径,舆情最接近指标)\n", w.Election.SentimentBias)
			ok = true
		}
		if !ok {
			b.WriteString("暂无该维度指标(社会结构/满意度数据不可用)\n")
		}
		fmt.Fprintf(&b, "本维度活动场景 %d 个(明细见「本维度进行中」)\n",
			scenarioCountOfDimension(w, virtual_city.ScenarioGroupSociety))
	case virtual_city.ScenarioGroupNature:
		// 批次85 E2E F1:自然灾害 —— 活动场景 + 世界事件(见末尾观察闭环
		// 两段)+ 可得的供给受损类指标(食品价格 / 产业链价格,供给传导
		// 最接近指标)。
		header("自然灾害")
		ok := false
		if w != nil && w.Goods != nil {
			if it := w.Goods.Items["food"]; it != nil {
				fmt.Fprintf(&b, "食品类商品价格指数 %.1f(环比 %+.1f%%;供给受损传导最接近指标)\n",
					it.PriceIdx, it.MomChange*100)
				ok = true
			}
		}
		if w != nil && w.SupplyChain != nil && len(w.SupplyChain.PriceIndex) > 0 {
			b.WriteString("产业链层级价格指数 ")
			for i, v := range w.SupplyChain.PriceIndex {
				if i > 0 {
					b.WriteString(" / ")
				}
				fmt.Fprintf(&b, "%.2f", v)
			}
			b.WriteString("(供给链受损传导)\n")
			ok = true
		}
		if !ok {
			b.WriteString("暂无该维度指标(供给数据不可用;灾害影响经供给传导)\n")
		}
		fmt.Fprintf(&b, "本维度活动场景 %d 个(明细见「本维度进行中」)\n",
			scenarioCountOfDimension(w, virtual_city.ScenarioGroupNature))
	case virtual_city.ScenarioGroupUnknown:
		// 批次85 E2E F1:未知灾难 —— 引擎无未知灾难专属指标,如实说明
		// (§29.11 不编造);影响经活动场景与世界事件观察。
		header("未知灾难")
		b.WriteString("暂无该维度专属指标(未知类影响无法归入既有指标体系;影响经活动场景与世界事件观察)\n")
		fmt.Fprintf(&b, "本维度活动场景 %d 个(明细见「本维度进行中」)\n",
			scenarioCountOfDimension(w, virtual_city.ScenarioGroupUnknown))
	case virtual_city.ScenarioGroupTech:
		// 批次85 E2E F1:科技与新文明 —— 引擎无科技研发子系统,如实说明
		// (§29.11 不编造);影响经活动场景与世界事件观察。
		header("科技与新文明")
		b.WriteString("暂无该维度专属指标(引擎无科技研发子系统;影响经活动场景与世界事件观察)\n")
		fmt.Fprintf(&b, "本维度活动场景 %d 个(明细见「本维度进行中」)\n",
			scenarioCountOfDimension(w, virtual_city.ScenarioGroupTech))
	default:
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// scenarioCountOfDimension 统计某维度分组下进行中的活动场景数(持锁只读;
// w 为 nil → 0;归不到该维度的场景不计入,宁缺勿滥)。
func scenarioCountOfDimension(w *virtual_city.World, group string) int {
	n := 0
	if w == nil {
		return 0
	}
	for _, sc := range w.Scenarios {
		if sc != nil && virtual_city.ScenarioDimensionOf(sc.Kind) == group {
			n++
		}
	}
	return n
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

// ─────────────── 批次85 §3.1.2 观察闭环(维度状态视图渲染) ───────────────

// scopeDimension 是 query_city_state scope → 维度分组名的**集中映射**
// (批次85 §3.1.2;一处定义,防 13 处硬编码漂移)。
//
// 批次85 E2E F1 扩到 **13 个维度 scope**(维度名 = ScenarioGroup* 取值,
// 自映射)+ overview/market/labor/treasury/election/all 6 个**没有**对应
// 维度分组 ⇒ 空串 = 全局口径(聚合全部维度的活动场景与世界事件,段头
// 「全局进行中」)。批次84 的 7 个英文维度键先经 canonicalCityStateScope
// 归一,不在此表重复登记。
var scopeDimension = map[string]string{
	virtual_city.ScenarioGroupMarket:         virtual_city.ScenarioGroupMarket,
	virtual_city.ScenarioGroupPolicy:         virtual_city.ScenarioGroupPolicy,
	virtual_city.ScenarioGroupSociety:        virtual_city.ScenarioGroupSociety,
	virtual_city.ScenarioGroupNature:         virtual_city.ScenarioGroupNature,
	virtual_city.ScenarioGroupUnknown:        virtual_city.ScenarioGroupUnknown,
	virtual_city.ScenarioGroupTech:           virtual_city.ScenarioGroupTech,
	virtual_city.ScenarioGroupPublicHealth:   virtual_city.ScenarioGroupPublicHealth,
	virtual_city.ScenarioGroupInfrastructure: virtual_city.ScenarioGroupInfrastructure,
	virtual_city.ScenarioGroupTransport:      virtual_city.ScenarioGroupTransport,
	virtual_city.ScenarioGroupCulture:        virtual_city.ScenarioGroupCulture,
	virtual_city.ScenarioGroupEnvironment:    virtual_city.ScenarioGroupEnvironment,
	virtual_city.ScenarioGroupSecurity:       virtual_city.ScenarioGroupSecurity,
	virtual_city.ScenarioGroupDailyLife:      virtual_city.ScenarioGroupDailyLife,
	"overview": "",
	"market":   "",
	"labor":    "",
	"treasury": "",
	"election": "",
	"all":      "",
}

// cityStateScopeAliases 是批次84 的 7 个英文维度 scope 键 → 13 个维度名的
// **向后兼容别名**(批次85 E2E F1 改用维度名后,既有调用 / 测试零回归)。
// 仅作归一化入口,不进工具枚举(queryCityStateScopes)。
var cityStateScopeAliases = map[string]string{
	"infrastructure": virtual_city.ScenarioGroupInfrastructure,
	"public_health":  virtual_city.ScenarioGroupPublicHealth,
	"environment":    virtual_city.ScenarioGroupEnvironment,
	"security":       virtual_city.ScenarioGroupSecurity,
	"transport":      virtual_city.ScenarioGroupTransport,
	"culture":        virtual_city.ScenarioGroupCulture,
	"daily":          virtual_city.ScenarioGroupDailyLife,
}

// canonicalCityStateScope 把 query_city_state 的 scope 归一到工具枚举口径:
// 别名(7 个英文维度键)→ 维度名;其余原样返回(6 个既有 scope 与 13 个
// 维度名本就是规范取值)。幂等。
func canonicalCityStateScope(scope string) string {
	if dim, ok := cityStateScopeAliases[scope]; ok {
		return dim
	}
	return scope
}

// dimensionObservation 渲染 query_city_state 各 scope 末尾的观察闭环
// (批次85 §3.1.2):「本维度进行中」+「相关世界事件」两段。
//
// 映射集中在 scopeDimension(scope 先经 canonicalCityStateScope 归一,
// 7 个英文别名同样命中维度口径);6 个无专属维度 scope 走全局口径
// (「全局进行中」段,聚合全部维度)。两段都空 ⇒
// 「当前该维度无进行中场景、无相关事件」(明确而非空白)。
// 调用须在**房间锁外**(内部经只读桥 SystemDimensionStatus,§92a)。
func (a *Agent) dimensionObservation(scope string) string {
	if a.room == nil {
		return ""
	}
	scope = canonicalCityStateScope(scope)
	if dim, ok := scopeDimension[scope]; ok && dim != "" {
		return renderDimensionObservation(a.room.SystemDimensionStatus(dim), false)
	}
	// 6 个无专属维度 scope(含 all / 未知 scope 兜底)⇒ 全局口径。
	return renderDimensionObservation(a.globalDimensionView(), true)
}

// globalDimensionView 聚合 13 个维度的视图(批次85 §3.1.2 全局口径)。
// 逐维度经只读桥取视图后合并、按时间重排(新→旧);事件合计截 5 条
// (与维度视图 ≤5 同口径,防回灌膨胀)。调用须在房间锁外(§92a)。
func (a *Agent) globalDimensionView() virtual_city.DimensionStatusView {
	merged := virtual_city.DimensionStatusView{
		ActiveScenarios: []virtual_city.ScenarioBrief{},
		RecentEvents:    []virtual_city.WorldEventBrief{},
	}
	if a.room == nil {
		return merged
	}
	for _, g := range knownScenarioGroups() {
		v := a.room.SystemDimensionStatus(g)
		merged.ActiveScenarios = append(merged.ActiveScenarios, v.ActiveScenarios...)
		merged.RecentEvents = append(merged.RecentEvents, v.RecentEvents...)
	}
	sort.SliceStable(merged.ActiveScenarios, func(i, j int) bool {
		return merged.ActiveScenarios[i].AppliedMonth > merged.ActiveScenarios[j].AppliedMonth
	})
	sort.SliceStable(merged.RecentEvents, func(i, j int) bool {
		return merged.RecentEvents[i].ObservedAt > merged.RecentEvents[j].ObservedAt
	})
	if len(merged.RecentEvents) > dimensionRecentEventsCap {
		merged.RecentEvents = merged.RecentEvents[:dimensionRecentEventsCap]
	}
	return merged
}

// dimensionRecentEventsCap 是观察闭环「相关世界事件」段的条数上限(≤5,
// 与 room_dimension_status.go 的维度视图同口径)。
const dimensionRecentEventsCap = 5

// renderDimensionObservation 渲染观察闭环两段(批次85 §3.1.2 样式):
//
//	【本维度进行中】
//	- 供水管道破裂 L3(剩 4 月,作用范围 全域,注入于 M7)
//	【相关世界事件】
//	- 东财行情:沪指收涨 1.2%(已注入)
//
// global=true(6 个无专属维度 scope)⇒ 段头「全局进行中」,场景行带
// [维度] 前缀。两段都空 ⇒ 「当前该维度无进行中场景、无相关事件」。
func renderDimensionObservation(v virtual_city.DimensionStatusView, global bool) string {
	if len(v.ActiveScenarios) == 0 && len(v.RecentEvents) == 0 {
		return "当前该维度无进行中场景、无相关事件"
	}
	var b strings.Builder
	if len(v.ActiveScenarios) > 0 {
		title := "本维度进行中"
		if global {
			title = "全局进行中"
		}
		fmt.Fprintf(&b, "【%s】\n", title)
		for _, s := range v.ActiveScenarios {
			prefix := ""
			if global {
				prefix = "[" + virtual_city.ScenarioDimensionOf(s.Kind) + "] "
			}
			target := strings.TrimSpace(s.Target)
			if target == "" {
				target = "全域"
			}
			fmt.Fprintf(&b, "- %s%s L%d(剩 %d 月,作用范围 %s,注入于 M%d)\n",
				prefix, s.Name, s.Magnitude, s.RemainingMonths, target, s.AppliedMonth)
		}
	}
	if len(v.RecentEvents) > 0 {
		b.WriteString("【相关世界事件】\n")
		for _, e := range v.RecentEvents {
			state := "未注入"
			if e.Injected {
				state = "已注入"
			}
			title := e.Title
			if e.Source != "" {
				title = e.Source + ":" + title
			}
			fmt.Fprintf(&b, "- %s(%s)\n", title, state)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
