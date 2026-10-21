/**
 * virtualCityStages — 虚拟城市「城市扩张 v2.12」阶段 4/7 扩展段类型 +
 * P2 v2 可视化统计段类型。
 *
 * 拆分原因：CLAUDE.md §4「单文件 ≤ 1800 行」—— types/virtualCity.ts 已 1809 行触顶，
 * 本批（批次70）要补 treasury / supply_chain / industrial_clusters / fin_market /
 * scenarios_recent 等新段 + society 新字段 + 字段名对账，拆出独立文件。
 * 导出路径保持不变：外部仍 `import { ... } from '@/types/virtualCity'`（本文件被
 * virtualCity.ts `export *` 再导出）。
 *
 * ⚠ **字段名逐字对齐后端 JSON tag**（事实来源 = ServerGo/game/virtual_city/view.go
 *   / scenario.go / civic_election.go）。批次69 JSON tag 对账发现的前端侧两类问题：
 *   ① 字段名不一致（前端重命名误伤 ⇒ 该值恒 undefined）；② 前端类型缺失后端已下发段。
 *
 * 批次70 修正的重命名误伤（后端 tag 才是正名）：
 *   - WealthLayerJSON.WealthPct  → `wealth_pct`（前端原写 `virtualCity_pct`）
 *   - ElectionVote.WealthScore    → `wealth_score`（前端原写 `virtualCity_score`）
 *   - CycleJSON.LPR5Y             → `lpr_5y`（前端原写 `lpr5y`；后端另留同值别名）
 */

// ── 社会结构统计（P2 v2 §13.2.4 + 阶段7 社会结构指标体系）────────────────

/** 三圈层人数（game.state.society.circles；《总体设计》§6）。 */
export interface VirtualCitySocietyCircles {
  /** 生存圈（月被动收入 < 月支出）。 */
  survival: number;
  /** 积累圈（1 ≤ 比值 < 2）。 */
  accumulate: number;
  /** 自由圈（比值 ≥ 2）。 */
  freedom: number;
}

/** 社会结构月度趋势点（game.state.society.trend[]，最近 12 月，时间升序）。 */
export interface VirtualCitySocietyTrend {
  /** 主钟月份。 */
  month: number;
  /** 净资产基尼。 */
  gini_wealth: number;
  /** 收入基尼。 */
  gini_income: number;
  /** 前 10% 收入占比（0-1）。 */
  top10_pct: number;
  /** <30 岁月度流动性（0-1）。 */
  mobility_young: number;
}

/** 社会结构统计（game.state.society，月度计算）。 */
export interface VirtualCitySociety {
  /** 存活玩家净资产基尼系数 [0,1]。 */
  gini: number;
  /** 可支配收入五等份各组占比（低→高，和 = 1）。 */
  quintiles: number[];
  /** 圈层人数分布。 */
  circles: VirtualCitySocietyCircles;
  // ── P2 v2(2026-09-19 §P2-可视化 §13.2.4)。omitempty 字段;缺省时为 0/空数组。 ──
  /** 存活玩家净资产合计（分母）。 */
  total_wealth?: number;
  /** 净资产中位数（线性插值）。 */
  median_wealth?: number;
  /** 净资产均值 = total / 人数。 */
  mean_wealth?: number;
  /** 分位线 {p10, p25, p50, p75, p90}。 */
  percentiles?: Record<string, number>;
  /** 洛伦兹曲线点集 [(人口累计, 财富累计)];n+1 点。 */
  lorenz_points?: [number, number][];
  /** 财富金字塔三层（自下而上：生存 / 积累 / 自由）。 */
  pyramid_layers?: VirtualCityPyramidLayer[];
  // ── 阶段7（2026-09-21 §城市扩张 v2.12）社会结构指标体系 ──
  // 后端零值下发（economy_enabled=false 亦有值），旧帧缺省 ⇒ 一律可选。
  /** 收入基尼系数 [0,1]。 */
  gini_income?: number;
  /** 前 1% 收入占比（0-1）。 */
  top1_pct?: number;
  /** 前 10% 收入占比（0-1）。 */
  top10_pct?: number;
  /** 后 50% 收入占比（0-1）。 */
  bottom50_pct?: number;
  /** 财富五等分（区别于 quintiles = 收入五等分）。 */
  wealth_quintiles?: number[];
  /** 4 层绝对门槛金字塔人数（自下而上）。 */
  pyramid?: number[];
  /** <30 岁月度流动性（0-1）。 */
  mobility_young?: number;
  /** 最近 12 月趋势（时间升序）。 */
  trend?: VirtualCitySocietyTrend[];
}

/** 财富金字塔单层（v2 新增）。 */
export interface VirtualCityPyramidLayer {
  name: 'survival' | 'accumulation' | 'freedom';
  count: number;
  total_wealth: number;
  avg_wealth: number;
  /**
   * 占总财富比例 0-1。
   * ⚠ 批次70 修正：后端 tag 是 `wealth_pct`（WealthLayerJSON.WealthPct）；
   *   前端原写 `virtualCity_pct`（批次21 wealth→virtualCity 全栈重命名的误伤）
   *   ⇒ 金字塔三条条宽与百分比恒为 0。
   */
  wealth_pct: number;
}

// ── P2 v2 资金流向可视化（game.state.flow_stat）─────────────────────────

/** 月度资金流向统计（v2 新增）。 */
export interface VirtualCityFlowStat {
  period: string;
  period_label: string;
  nodes: VirtualCityFlowNode[];
  links: VirtualCityFlowLink[];
  total_in_cny: number;
  total_out_cny: number;
}

/** 资金流向节点。 */
export interface VirtualCityFlowNode {
  // ⚠ 必须与后端 `ServerGo/game/virtual_city/distribution.go` 的 FlowNode* 常量同步
  //   （批次52 新增 family；此前前端漏项 ⇒ t(undefined) ⇒ 桑基图面板崩溃）。
  id: 'salary' | 'firms' | 'market' | 'bank' | 'gov' | 'player' | 'world' | 'family';
  label: string;
  kind: 'source' | 'sink' | 'pass';
  amount_cny: number;
}

/** 资金流向边。 */
export interface VirtualCityFlowLink {
  from: VirtualCityFlowNode['id'];
  to: VirtualCityFlowNode['id'];
  amount_cny: number;
  /** 占最大边比例 0-1，用于决定贝塞尔连线宽度。 */
  pct: number;
}

// ── 阶段4（2026-09-21 §城市扩张 v2.12）财政 / 产业链 / 金融市场 ──────────

/** 国库快照（game.state.treasury；政府/观战者全量可见，无座位隐私）。 */
export interface VirtualCityTreasury {
  /** 国库现金（元）。 */
  cash_cny: number;
  /** 未偿债券余额（元）。 */
  bonds_outstanding_cny: number;
  /** 上月财政收入（元）。 */
  last_month_revenue_cny: number;
  /** 上月财政支出（元）。 */
  last_month_expense_cny: number;
  /** 赤字连续月数（0 = 非赤字）。 */
  deficit_run: number;
  /** 公共服务指数（0-1，越高越好）。 */
  public_service_idx: number;
}

/** 产业链供给节点（game.state.supply_chain.top_nodes[]；利用率 top5）。 */
export interface VirtualCitySupplyNode {
  /** 节点 id。 */
  id: string;
  /** 所属行业。 */
  industry: string;
  /** 节点中文名。 */
  name_cn: string;
  /** 供给层级 0-3。 */
  tier: number;
  /** 产能利用率 0..1。 */
  utilization: number;
  /** 库存可用月数。 */
  inventory_months: number;
}

/** 产业链瓶颈告警（game.state.supply_chain.bottlenecks[]，≤5 条）。 */
export interface VirtualCityBottleneck {
  /** 节点 id。 */
  node_id: string;
  /** 所属行业。 */
  industry: string;
  /** 节点中文名。 */
  name_cn: string;
  /** 瓶颈原因（服务端文本）。 */
  reason: string;
  /** 严重度 1-2（2 = 已实际缺货）。 */
  severity: number;
}

/** 产业链快照（game.state.supply_chain）。 */
export interface VirtualCitySupplyChain {
  /** 利用率 top5 节点。 */
  top_nodes: VirtualCitySupplyNode[];
  /** Tier 0-3 价格指数（基 1.0）。 */
  price_idx: number[];
  /** 瓶颈警告（≤5 条）。 */
  bottlenecks: VirtualCityBottleneck[];
}

/** 产业集群（game.state.industrial_clusters[]）。 */
export interface VirtualCityIndustrialCluster {
  /** 集群 id。 */
  id: string;
  /** 集群名。 */
  name: string;
  /** 所在城区 id。 */
  district_id: string;
  /** 所在城区名。 */
  district_name: string;
  /** 构成行业。 */
  industries: string[];
  /** 企业数。 */
  firms: number;
  /** 税收优惠月数。 */
  tax_break_months: number;
  /** 累计已减免（万元）。 */
  tax_break_used_wan: number;
  /** 年度减免上限（万元）。 */
  tax_break_cap_wan: number;
  /** 上月实际减免（元）。 */
  last_break_cny: number;
}

/** 量化策略（game.state.fin_market.quant_strategies[]，5 策略权重）。 */
export interface VirtualCityQuantStrategy {
  /** 策略名。 */
  name: string;
  /** 组合权重（0-1）。 */
  weight: number;
  /** 上月收益（小数）。 */
  last_return: number;
}

/** 同业存单快照（game.state.fin_market.cd）。 */
export interface VirtualCityCDSnapshot {
  /** 存量加权平均票面（小数）。 */
  avg_rate: number;
  /** 挂牌存量（万元）。 */
  outstanding_wan: number;
  /** 期限 → 票面（"1"/"3"/"6"/"12"）。 */
  rates: Record<string, number>;
}

/** 可转债（game.state.fin_market.convertibles[]，按市价 top3）。 */
export interface VirtualCityConvertibleBond {
  /** 债券 id。 */
  id: string;
  /** 发行人。 */
  issuer: string;
  /** 发行主体对应的供给节点 id。 */
  issuer_node: string;
  /** 市价（元/张）。 */
  price: number;
  /** 转股价值。 */
  conv_value: number;
  /** 债底。 */
  bond_floor: number;
  /** 票息率（小数）。 */
  coupon_rate: number;
  /** 状态 active|redeemed|put|matured。 */
  status: string;
  /** 剩余月数。 */
  months_left: number;
}

/** 融券快照（game.state.fin_market.short）。 */
export interface VirtualCityShortSnapshot {
  /** 融券余额（元）。 */
  balance_cny: number;
  /** 保证金冻结（元）。 */
  margin_cny: number;
  /** 未平仓笔数。 */
  open_count: number;
  /** 上月强平笔数。 */
  margin_calls_mon: number;
}

/** 基金评级（game.state.fin_market.fund_ratings[]，按星数 top5）。 */
export interface VirtualCityFundRating {
  /** 基金 id。 */
  fund_id: string;
  /** 基金名。 */
  name: string;
  /** 星级（1-5）。 */
  stars: number;
  /** 夏普比率。 */
  sharpe: number;
  /** 最大回撤。 */
  max_drawdown: number;
  /** 规模（万元）。 */
  aum_wan: number;
}

/** 金融市场快照（game.state.fin_market）。 */
export interface VirtualCityFinMarket {
  /** 量化基金指数（基 100）。 */
  quant_index: number;
  /** 上月组合收益（小数）。 */
  quant_last_return: number;
  /** 5 策略权重。 */
  quant_strategies: VirtualCityQuantStrategy[];
  /** 同业存单快照。 */
  cd: VirtualCityCDSnapshot;
  /** 可转债 top3（按市价）。 */
  convertibles: VirtualCityConvertibleBond[];
  /** 融券快照。 */
  short: VirtualCityShortSnapshot;
  /** 基金评级 top5（按星数）。 */
  fund_ratings: VirtualCityFundRating[];
}

/** 场景注入记录（game.state.scenarios_recent[]；条件触发者注入历史）。 */
export interface VirtualCityScenarioAppliedRecord {
  /** 记录 id（"SC1"…）。 */
  id: string;
  /** 场景 kind。 */
  kind: string;
  /** 强度 L1–L5。 */
  magnitude: number;
  /** 注入时的主钟月份。 */
  applied_month: number;
  /** 条件触发者备注（≤60 rune）。 */
  note: string;
  /** 注入时刻 unix 毫秒。 */
  at: number;
}
