import type { TKey } from '@/i18n';

// ─── 财商流游戏 (Wealth) types ───
//
// 与 lag_docs/财商流游戏/已实现/02-架构设计/财商流游戏-WS与HTTP协议契约-v1.md §3
// 「game.state 载荷逐字段契约」**逐字段对齐**（字段名 / 可空性一字不改；
// 后端 game/wealth/view.go::BuildClientState 是协议唯一实现）。
// 静态表（城区 / 职业色 / 动作元数据）出处：协议 §4 + 后端架构文档 §4 DistrictDefs
// + 前端架构文档 §3 职业色（P0 新定）。

/** 8 城区 id（顺序 = DistrictDefs 静态表 / market.districts 数组顺序）。 */
export type WealthDistrictId =
  | 'finance' | 'tech' | 'industry' | 'oldtown'
  | 'commerce' | 'residential' | 'suburb' | 'riverside';

/** 市场周期四阶段（《规则》§7.1）。 */
export type WealthCyclePhase = 'recovery' | 'boom' | 'recession' | 'depression';

/** 明斯基融资等级（v2.60 N11-4）。 */
export type WealthMinskyTier = 'hedge' | 'speculative' | 'ponzi';

/** 月内相位：acting=动作窗口 / settling=月结中。 */
export type WealthPhase = 'acting' | 'settling';

export type WealthStatus = 'open' | 'playing' | 'over';
/** 全 Agent 模式标志(2026-09-19 §全Agent模式): true = 全 Agent 房间,人类不能加入对局。 */
export type WealthFullAgentMode = boolean;


/** 月总收入档（《规则》§5.3）。 */
export type WealthIncomeBand = 'low' | 'mid' | 'high' | 'top';

/** 本月最近动作类别（players[].status_icon）。 */
export type WealthStatusIcon = 'working' | 'idle' | 'trading' | 'resting' | 'moved';

/** 资产种类（asset.kind；house/shop 带城区后缀）。 */
export type WealthAssetKind =
  | 'stock_index' | 'bond' | 'gold'
  | `house:${WealthDistrictId}` | `shop:${WealthDistrictId}`
  | 'side_business' | 'pension';

/** 贷款种类（loan.kind）。 */
export type WealthLoanKind =
  | 'mortgage' | 'consumer' | 'credit_tier1' | 'credit_tier2' | 'credit_tier3' | 'business';

// ── game.state 载荷（协议 §3）──────────────────────────────────────────

export interface WealthCycle {
  phase: WealthCyclePhase;
  /** 1Y LPR，小数（如 0.035），非百分数。 */
  lpr: number;
  /** 5Y LPR，小数（如 0.04），非百分数（P1 LPR 重定价）。 */
  lpr5y?: number;
  /** CPI，小数（如 0.02）。 */
  cpi: number;
  /** 阶段剩余月（含钟声重掷不确定性，仅展示）。 */
  months_left: number;
  /** 明斯基时刻累计触发次数（P1 明斯基引擎）。 */
  minsky_moment_count?: number;
  /** 当前庞氏玩家数（P1 明斯基引擎）。 */
  ponzi_count?: number;
  /** 庞氏玩家占比（0-1；P1 明斯基引擎）。 */
  ponzi_ratio?: number;
}

export interface WealthDistrictMarket {
  id: WealthDistrictId;
  /** 房价指数（初始 1.0）。 */
  price_index: number;
  /** 租金指数（price_index × 0.0016 归一）。 */
  rent_index: number;
}

export interface WealthMarket {
  /** 股票指数，元/份（初始 3.50）。 */
  stock_index: number;
  /** 黄金，元/克（初始 750）。 */
  gold_price: number;
  /** 当期新购债券年化，小数（如 0.032）。 */
  bond_yield: number;
  /** 8 项，顺序 = DistrictDefs 静态表。 */
  districts: WealthDistrictMarket[];
}

/** 央行货币政策快照（game.state.central_bank，P1 央行引擎下发）。 */
export interface WealthCentralBank {
  /** 流通中现金（元）。 */
  m0_cny: number;
  /** M1 = M0（游戏简化，元）。 */
  m1_cny: number;
  /** M2 = M1 + 定期存款（元）。 */
  m2_cny: number;
  /** 基础货币（元）。 */
  mb_cny: number;
  /** 货币乘数（M2 / MB，保留 3 位小数）。 */
  money_multiplier: number;
  /** 政策利率（小数，如 0.03）。 */
  policy_rate: number;
  /** LPR（小数）。 */
  lpr: number;
  /** CPI（小数）。 */
  cpi: number;
  /** 信贷约束系数 [0,1]（0=宽松，1=惜贷）。 */
  credit_tightness: number;
  /** 贷款额度乘数 [0.5,1]。 */
  loan_quota_factor: number;
}

export interface WealthProfession {
  /** 职业卡 id（"P01"…；文档池 "N9012345"）。 */
  id: string;
  /** 中文名，如 "外卖骑手"。 */
  title: string;
  /** 头像文件名主干（"p01" → assets/images/wealth/agents/p01.png）。 */
  avatar: string;
}

export interface WealthResources {
  energy: number;
  network: number;
  cognition: number;
}

export interface WealthPlayer {
  seat: number;              // 0..WEALTH_MAX_SEATS-1（2026-09-16 起 10–12 座位）
  account: string;           // bot 为 bot_<modelkey>
  nickname: string;
  is_bot: boolean;
  /** bot 的 agent_name；人类为 ""。 */
  model_display: string;
  profession: WealthProfession;
  /** 当前所在区 id。 */
  district: WealthDistrictId;
  /** 住房所在区 id。 */
  home_district: WealthDistrictId;
  alive: boolean;
  /** P0 恒 false（P1 提前退休预留）。 */
  retired: boolean;
  /** 个人年龄（文档池差异卡展示用）。 */
  age: number;
  resources: WealthResources;
  /** 元（公开）。 */
  net_worth: number;
  /** 0–2 封顶（公开）。 */
  fi_index: number;
  income_band: WealthIncomeBand;
  status_icon: WealthStatusIcon;
  /** 人读，如 "买入黄金 50g"；空 = 本月未动作。 */
  last_action: string;
  /** 终局结局 id；进行中为 ""。 */
  ending: string;
  /** 明斯基融资等级（P1 明斯基引擎，仅本人座位下发）。 */
  minsky_tier?: WealthMinskyTier;
  /** 月供/月收入比（0-1+；P1 明斯基引擎，仅本人座位下发）。 */
  debt_to_income?: number;
  /** 消费档位 0-3（P1 真实经济循环引擎；档位是公开生活方式，全座位下发）。 */
  consumption_level?: number;
}

export interface WealthMonthlyDetail {
  /** "salary" | "tax" | "living" | …（引擎扩展开放）。 */
  key: string;
  amount_cny: number;
  text: string;
}

export interface WealthMonthly {
  income: number;
  expense: number;
  net: number;
  tax: number;
  social: number;
  detail: WealthMonthlyDetail[];
}

export interface WealthAsset {
  kind: WealthAssetKind | string;
  /** 人读名，如 "指数基金" "老城区住宅"。 */
  name: string;
  /** 份/克/套/间。 */
  units: number;
  /** 当前单价。 */
  price: number;
  value_cny: number;
  /** 月现金流（租金+/月供−；正为流入）。 */
  monthly_flow_cny: number;
}

export interface WealthLoan {
  id: string;                // "L3"
  kind: WealthLoanKind | string;
  principal: number;
  balance: number;
  annual_rate: number;
  monthly_payment: number;
  months_left: number;
}

export interface WealthFamily {
  marital: 'single' | 'married';
  children: number;
}

/** 仅本人座位填充；观战者 null。 */
export interface WealthMyState {
  cash: number;
  /** 当前基准月薪（税前）。 */
  salary: number;
  /** 配偶月收入（税后净额）。 */
  spouse_income: number;
  /** 上月副业净收入。 */
  side_income: number;
  /** 上月被动收入合计。 */
  passive_income: number;
  /** 最近一次月结（或当月预估）。 */
  monthly: WealthMonthly;
  resources: WealthResources;
  assets: WealthAsset[];
  loans: WealthLoan[];
  /** 养老金账户余额。 */
  pension_cny: number;
  /** 定期存款（M2 组成部分，元）。 */
  savings_deposit: number;
  /** 400–850。 */
  credit_score: number;
  family: WealthFamily;
  fi_index: number;
  net_worth: number;
  /** 职业卡 goals（含 5 年目标），终局对照展示。 */
  goals: string[];
  /** 上月消费结构（CPI 八大类 id → 金额元；P1 真实经济循环引擎，仅本人座位下发）。 */
  consumption_by_goods?: Record<string, number>;
}

/** Agent 思维可见性：本人座位 + 观战者可见；其他玩家不可见。 */
export interface WealthBotContext {
  seat: number;
  /** Agent 自述本月决策（≤120 字）。 */
  last_decision_summary: string;
  /** JSON 字符串。 */
  last_tool_input: string;
  /** 人读结果。 */
  last_tool_result: string;
  /** 内心独白（speak 的 internal_thought）。 */
  heart_thought: string;
}

export interface WealthLedgerEntry {
  month: number;
  from: string;
  to: string;
  amount_cny: number;
  category: string;
  note: string;
}

export interface WealthRecentEvent {
  month: number;
  type: string;
  text: string;
}

/** game.state 全量快照（按座位脱敏，BroadcastTo 单发）。 */
export interface WealthGameState {
  room_id: string;
  game_kind: 'wealth';
  status: WealthStatus;
  /** 1..420。 */
  month: number;
  /** 主时钟年龄。 */
  age: number;
  phase: WealthPhase;
  cycle: WealthCycle;
  market: WealthMarket;
  /** 央行货币政策快照（P1 央行引擎下发）。 */
  central_bank: WealthCentralBank;
  max_seat: number;
  /** unix_ms；前端倒计时 = next_month_at − now。 */
  next_month_at: number;
  /** unix_s；RoomRunningClock 同源语义。 */
  game_started_at: number;
  players: WealthPlayer[];
  /** -1 = 观战。 */
  my_seat: number;
  my: WealthMyState | null;
  bot_contexts: WealthBotContext[];
  /** 最近 50 条：本人相关 + 公共。 */
  ledger_recent: WealthLedgerEntry[];
  /** 最近 100 条。 */
  events_recent: WealthRecentEvent[];
  /** 明斯基全局概览（P1 明斯基引擎）。 */
  minsky_overview?: WealthMinskyOverview;
  /** 提前还款资格（仅本人座位下发；P1 LPR 重定价）。 */
  early_repay_eligible?: boolean;
  /** 消费品市场快照（P1 真实经济循环引擎；economy_enabled=false 时零值/缺失）。 */
  consumer_market?: WealthGoodsMarket;
  /** 劳动力市场快照（P1 真实经济循环引擎）。 */
  labor_market?: WealthLaborMarket;
  /** 社会结构统计（P1 真实经济循环引擎）。 */
  society?: WealthSociety;
  /** 社会调研（open ≤1 + 最近 4 个 closed；P1 社会调研系统）。 */
  surveys?: WealthSurvey[];
  /** 挂单簿（P2 交易系统；房间级）。 */
  listing_book?: WealthListingBook;
  /** 月度资金流向（P2 v2 §13.2.4 财富流动可视化）。 */
  flow_stat?: WealthFlowStat;
}

/** 明斯基全局概览（game.state.minsky_overview，P1 明斯基引擎）。 */
export interface WealthMinskyOverview {
  /** 庞氏玩家数。 */
  ponzi_count: number;
  /** 投机玩家数。 */
  spec_count: number;
  /** 对冲玩家数。 */
  hedge_count: number;
  /** 庞氏玩家占比（0-1）。 */
  ponzi_ratio: number;
  /** 距离下次明斯基时刻判定的剩余月（冷却期；0=可触发）。 */
  cooldown_left: number;
}

// ── P1 真实经济循环引擎（消费品市场 / 劳动力市场 / 社会结构）────────────
//
// 与 lag_docs/财商流游戏/已实现/05-P1扩展/财商流游戏-P1-真实经济循环引擎-v1.md
// §6「视图与协议」逐字段对齐（JSON 名不可改；后端 view.go 是唯一实现）。

/** CPI 八大类消费单品（game.state.consumer_market.goods[]，按 §2.1 权重表序）。 */
export interface WealthGoodsItem {
  /** food|clothing|housing|household|transport|education|healthcare|misc。 */
  id: string;
  /** 篮子权重（0-1，八类和恒为 1）。 */
  weight: number;
  /** 价格指数（基期 = 100）。 */
  price_idx: number;
  /** 上月环比（小数，0.01 = +1%）。 */
  mom_change: number;
}

/** 消费品市场快照（game.state.consumer_market）。 */
export interface WealthGoodsMarket {
  /** CPI 同比（12 月滚动年化，小数；不足 12 月时值保留但口径未就绪）。 */
  cpi_yoy: number;
  /** CPI 上月环比（小数）。 */
  cpi_mom: number;
  /** 8 类，按权重表顺序。 */
  goods: WealthGoodsItem[];
}

/** 劳动力市场快照（game.state.labor_market）。 */
export interface WealthLaborMarket {
  /** 内生失业率（0-1，clamp [0.02,0.35]）。 */
  unemployment_rate: number;
  /** 就业率 = 1 − 失业率。 */
  employment_ratio: number;
  /** 平均工资年增长（Phillips 曲线，小数）。 */
  avg_wage_growth_yoy: number;
  /** 上月企业营收（元，居民消费 × FirmScale 2.5）。 */
  firm_revenue_cny: number;
  /** 裁员潮强度 0-3（0 平静 / 1 观察 / 2 裁员潮 / 3 已触发个体放大）。 */
  layoff_wave: number;
}

/** 三圈层人数（game.state.society.circles；《总体设计》§6）。 */
export interface WealthSocietyCircles {
  /** 生存圈（月被动收入 < 月支出）。 */
  survival: number;
  /** 积累圈（1 ≤ 比值 < 2）。 */
  accumulate: number;
  /** 自由圈（比值 ≥ 2）。 */
  freedom: number;
}

/** 社会结构统计（game.state.society，月度计算）。 */
export interface WealthSociety {
  /** 存活玩家净资产基尼系数 [0,1]。 */
  gini: number;
  /** 可支配收入五等份各组占比（低→高，和 = 1）。 */
  quintiles: number[];
  /** 圈层人数分布。 */
  circles: WealthSocietyCircles;
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
  pyramid_layers?: WealthPyramidLayer[];
}

/** 财富金字塔单层（v2 新增）。 */
export interface WealthPyramidLayer {
  name: 'survival' | 'accumulation' | 'freedom';
  count: number;
  total_wealth: number;
  avg_wealth: number;
  /** 占总财富比例 0-1。 */
  wealth_pct: number;
}

/** 月度资金流向统计（v2 新增）。 */
export interface WealthFlowStat {
  period: string;
  period_label: string;
  nodes: WealthFlowNode[];
  links: WealthFlowLink[];
  total_in_cny: number;
  total_out_cny: number;
}

/** 资金流向节点。 */
export interface WealthFlowNode {
  id: 'salary' | 'firms' | 'market' | 'bank' | 'gov' | 'player' | 'world';
  label: string;
  kind: 'source' | 'sink' | 'pass';
  amount_cny: number;
}

/** 资金流向边。 */
export interface WealthFlowLink {
  from: WealthFlowNode['id'];
  to: WealthFlowNode['id'];
  amount_cny: number;
  /** 占最大边比例 0-1，用于决定贝塞尔连线宽度。 */
  pct: number;
}

// ── P1 社会调研系统（对全体 Agent 的预测模拟）───────────────────────────
//
// 与 lag_docs/财商流游戏/已实现/05-P1扩展/财商流游戏-P1-社会调研系统-v1.md
// §5「视图与协议」逐字段对齐。

/** 调研聚合结果（WealthSurvey.result；选项文本一并下发，前端免查表）。 */
export interface WealthSurveyResult {
  options: string[];
  /** 按选项下标计数（len == options.length）。 */
  counts: number[];
  /** Counts / Total（和 ≈ 1）。 */
  percents: number[];
  /** 回答总数。 */
  total: number;
  /** 去重后前 3 条理由摘录（每条 ≤30 字）。 */
  top_reasons: string[];
}

/** 单次社会调研（game.state.surveys[] / game.survey_result.survey）。 */
export interface WealthSurvey {
  /** "SV1"…（World.SurveySeq 自增）。 */
  id: string;
  /** 问题文本（≤100 字）。 */
  question: string;
  /** 2-6 个选项（每个 ≤40 字）。 */
  options: string[];
  launch_month: number;
  /** launch_month + 2。 */
  deadline_month: number;
  /** "open" | "closed"。 */
  status: string;
  /** 已作答人数。 */
  answers_count: number;
  /** closed 时非空。 */
  result?: WealthSurveyResult;
}

// ── P2 玩家间交易与财富流动系统（lag_docs/财商流游戏/已实现/06-P2交易系统/）────
//
// 与 §3 数据结构逐字段对齐（JSON 名不可改；后端 view.go 是唯一实现）。

/** 挂单类型（Listing.Type）。 */
export type WealthListingType = 'asset' | 'buy' | 'info' | 'loan_ofr' | 'loan_req';

/** 挂单状态（Listing.Status）。 */
export type WealthListingStatus = 'open' | 'negotiating' | 'deal' | 'expired' | 'cancelled';

/** 议价动作（NegotiateTurn.Action）。 */
export type WealthNegotiateAction = 'offer' | 'accept' | 'reject' | 'counter';

/** 议价会话状态（NegotiateSession.Status）。 */
export type WealthNegotiateStatus = 'active' | 'deal' | 'reject' | 'expired';

/** 借贷方向（LoanPayload.Direction）。 */
export type WealthLoanDirection = 'lend' | 'borrow';

/** 信息类别（InfoOfferPayload.Category）。 */
export type WealthInfoCategory = 'market' | 'intel' | 'personal';

/** 拍卖类型。 */
export type WealthAuctionKind = 'english' | 'sealed' | 'take_it' | 'single_round';

/** 拍卖状态。 */
export type WealthAuctionStatus = 'pending' | 'active' | 'ended';

/** 资产挂单详情（ListingPayload.Asset）。 */
export interface WealthTradeAssetPayload {
  kind: string;
  name: string;
  units: number;
  price: number;
  value_cny: number;
  /** 底价（卖家保密；仅本人座位下发时可填，不下发他人）。 */
  min_cny: number;
}

/** 借贷挂单详情（ListingPayload.Loan）。 */
export interface WealthTradeLoanPayload {
  direction: WealthLoanDirection;
  principal_cny: number;
  max_rate: number;
  term_n: number;
  need_guarantee: boolean;
}

/** 信息出售详情（ListingPayload.Info）。 */
export interface WealthTradeInfoPayload {
  category: WealthInfoCategory;
  title: string;
  detail_hash: string;
  min_bid_cny: number;
}

/** 挂单内容联合（ListingPayload）。 */
export interface WealthListingPayload {
  asset?: WealthTradeAssetPayload;
  loan?: WealthTradeLoanPayload;
  info?: WealthTradeInfoPayload;
}

/** 单笔挂单（挂单簿条目）。 */
export interface WealthListing {
  id: string;
  type: WealthListingType;
  seat: number;
  payload: WealthListingPayload;
  ask_cny: number;
  status: WealthListingStatus;
  create_month: number;
  expire_month: number;
}

/** 单轮议价（NegotiateSession.Turns[]）。 */
export interface WealthNegotiateTurn {
  from: number;
  offer_cny: number;
  rate: number;
  comment: string;
  action: WealthNegotiateAction;
}

/** 单笔议价会话。 */
export interface WealthNegotiateSession {
  id: string;
  listing_id: string;
  proposer_seat: number;
  respond_seat: number;
  turns: WealthNegotiateTurn[];
  status: WealthNegotiateStatus;
  create_month: number;
  expire_month: number;
  last_offer_cny: number;
  last_offer_by: number;
}

/** 玩家间借贷合约（deal 后生成）。 */
export interface WealthP2PLoan {
  id: string;
  lender_seat: number;
  borrower_seat: number;
  principal_cny: number;
  balance_cny: number;
  annual_rate: number;
  monthly_payment: number;
  term_n: number;
  months_left: number;
  /** -1 无担保。 */
  guarantor_seat: number;
  overdue: boolean;
  create_month: number;
  listing_id: string;
}

/** 拍卖出价记录。 */
export interface WealthAuctionBid {
  seat: number;
  amount_cny: number;
  month: number;
  /** 密封暗标揭示后有效。 */
  revealed?: boolean;
}

/** 拍卖场次。 */
export interface WealthAuction {
  id: string;
  listing_id: string;
  kind: WealthAuctionKind;
  status: WealthAuctionStatus;
  /** 卖家座位。 */
  seat: number;
  /** 起拍价 / 基准价。 */
  start_cny: number;
  /** 当前最高出价。 */
  current_cny: number;
  /** 当前最高出价者座位（-1 无人）。 */
  current_seat: number;
  /** 结束月份（月结时判定）。 */
  end_month: number;
  bids: WealthAuctionBid[];
  /** 英式拍卖：卖家保留价（< 此价可拒绝）。 */
  reserve_cny?: number;
}

/** 房间级挂单簿（game.state.listing_book）。 */
export interface WealthListingBook {
  listings: WealthListing[];
  negotiates: WealthNegotiateSession[];
  p2p_loans: WealthP2PLoan[];
  auctions: WealthAuction[];
}

/**
 * 单座位回答明细（引擎 SurveyAnswer 镜像）。公开快照**不下发** Answers
 * （匿名投票原理）；本类型为 P2「按模型切片」分析预留，勿用于 UI 渲染。
 */
export interface WealthSurveyAnswer {
  seat: number;
  /** 选项下标 0-based。 */
  option_idx: number;
  /** ≤50 字理由。 */
  reason: string;
  /** 回答者模型 key。 */
  model_key: string;
}

// ── 座位容量常量（2026-09-16 §财商流10–12座位改造）──────────────────────
//
// 与后端 ServerGo/game/wealth/engine.go 的 MaxSeats / MinSeats 同值同义：
//   MaxSeats = 12 → 房间容量 12（「1 人类 + 11 Agent」或「全 12 Agent」）
//   MinSeats = 10 → 已占座 < 10 时 Start 返回 35003 ErrWealthNotEnoughPlayers
// 前端一切座位相关长度（建房档位 / 座位模型下拉 / 渲染环 / 提前开始门控）
// **必须**引用这些常量，禁止再出现 7 / 8 之类的魔数（§130「声明了却从不接线」）。

/** 房间座位上限（后端 wealth.MaxSeats）。game.state.max_seat 缺失时的兜底值。 */
export const WEALTH_MAX_SEATS = 12;

/** 最少开局座位（后端 wealth.MinSeats）：不足则无法开局。 */
export const WEALTH_MIN_SEATS = 10;

/** 建房弹窗 Agent 数默认值（10 Agent + 创建者 = 11 座 ≥ MinSeats → 自动开局）。 */
export const WEALTH_DEFAULT_AGENT_COUNT = 10;

/** 座位容量解析：服务端权威 max_seat 优先，非法/未到达时回落常量。 */
export function wealthSeatCapacity(
  gs: Pick<WealthGameState, 'max_seat'> | null | undefined,
): number {
  const n = gs?.max_seat;
  return typeof n === 'number' && n > 0 ? n : WEALTH_MAX_SEATS;
}

/**
 * 已占座人数。后端 players[] **恒为 max_seat 长度**（空座位是 account/nickname
 * 全空的占位对象），因此 `players.length` 永远等于容量 —— 判断「够不够开局」
 * 必须数真实占座，不能用数组长度（曾导致「提前开始」按钮恒可点）。
 */
export function wealthOccupiedSeats(
  players: WealthPlayer[] | null | undefined,
): number {
  return (players ?? []).filter((p) => !!p && (!!p.account || !!p.nickname)).length;
}

// ── 其余 S→C 帧载荷（协议 §2）────────────────────────────────────────

/** game.joined。 */
export interface WealthJoinedFrame {
  my_seat: number;
  month: number;
  phase: WealthPhase;
}

/** game.started。 */
export interface WealthStartedFrame {
  month: number;
  age: number;
  start_age: number;
  professions: { seat: number; profession_id: string }[];
}

export type WealthEventType =
  | 'action' | 'move' | 'settle' | 'market' | 'life' | 'chat' | 'error';

/** game.event。 */
export interface WealthEventFrame {
  room_id?: string;
  month: number;
  seat?: number;
  type: WealthEventType | string;
  text: string;
  data?: unknown;
}

export interface WealthMonthSummary {
  seat: number;
  cash_delta: number;
  net_worth: number;
  fi_index: number;
  note: string;
}

/** game.month（BroadcastRoom 全房同帧）。 */
export interface WealthMonthFrame {
  room_id?: string;
  month: number;
  age: number;
  summaries: WealthMonthSummary[];
  market_changes: {
    stock_index?: number;
    gold_price?: number;
    bond_rate?: number;
    house_idx?: Record<string, number>;
    /** 本月篮子 CPIYoY（P1 真实经济循环引擎 §6.3；economy_enabled=false 时 = CB.CPI）。 */
    cpi?: number;
    /** 本月内生失业率（P1 真实经济循环引擎 §6.3）。 */
    unemployment_rate?: number;
  };
  events: WealthRecentEvent[];
}

export type WealthEndingId =
  | 'winner' | 'affluent' | 'ordinary' | 'indebted' | 'bankrupt' | 'lonely_rich';

export interface WealthScore {
  seat: number;
  fi_score: number;
  life_score: number;
  social_score: number;
  total: number;
  ending: string;
}

/** game.over。 */
export interface WealthOverFrame {
  room_id?: string;
  scores: WealthScore[];
  /** 人生报告（按座位索引；P0 简化为每个座位一份独立报告）。 */
  reports: Record<number, string>;
}

/** game.error。 */
export interface WealthErrorFrame {
  code: number;
  message: string;
}

// ── C→S 动作（协议 §4 动作语义表；人类按钮 = 14 种，check_state/speak 仅 Agent）──

export type WealthActionType =
  | 'buy_asset' | 'sell_asset' | 'buy_house' | 'take_loan' | 'repay_loan'
  | 'start_side_business' | 'stop_side_business' | 'study' | 'socialize'
  | 'rest' | 'work_overtime' | 'move_district' | 'consume' | 'donate'
  | 'submit_month'
  | 'early_repay'
  | 'set_consumption';

export type WealthAction =
  | { type: 'buy_asset'; asset: 'stock_index' | 'bond' | 'gold'; amount_cny: number }
  | { type: 'sell_asset'; asset: string; units: number }
  | { type: 'buy_house'; district: WealthDistrictId; downpay_ratio: number }
  | { type: 'take_loan'; kind: 'consumer' | 'credit' | 'business'; amount_cny: number }
  | { type: 'repay_loan'; loan_id: string; amount_cny: number }
  | { type: 'start_side_business'; kind: 'delivery' | 'content' | 'tutoring' | 'freelance' }
  | { type: 'stop_side_business' }
  | { type: 'study' }
  | { type: 'socialize' }
  | { type: 'rest' }
  | { type: 'work_overtime' }
  | { type: 'move_district'; district: WealthDistrictId }
  | { type: 'consume'; amount_cny: number; reason?: string }
  | { type: 'donate'; amount_cny: number }
  | { type: 'submit_month' }
  | { type: 'early_repay'; loan_id: string; amount_cny: number }
  | { type: 'set_consumption'; level: number };

// ── P2 交易系统：交易类动作（走 game.wealth_xxx 帧）──────────────────────

/** 挂牌出售 / 收购 / 信息 / 借贷。 */
export type WealthTradeAction =
  | { type: 'listing_create'; listing_type: WealthListingType; payload: WealthListingPayload; ask_cny: number }
  | { type: 'listing_cancel'; listing_id: string }
  | { type: 'listing_view'; listing_type?: WealthListingType }
  | { type: 'negotiate_start'; listing_id: string; offer_cny: number }
  | { type: 'negotiate_respond'; neg_id: string; action: WealthNegotiateAction; offer_cny: number; comment?: string }
  | { type: 'loan_accept'; listing_id: string }
  | { type: 'loan_repay'; loan_id: string; amount_cny?: number }
  | { type: 'add_guarantor'; loan_id: string }
  | { type: 'auction_bid'; auction_id: string; amount_cny: number }
  | { type: 'sell_info'; category: WealthInfoCategory; title: string; detail: string; min_bid_cny: number }
  | { type: 'bid_info'; listing_id: string; bid_cny: number };

/** P2 交易错误码（errcode/errcode.go §8.2，35013–35024）。 */
export const WEALTH_TRADE_ERR = {
  ListingInvalid: 35013,
  ListingExpired: 35014,
  NegotiateNotFound: 35015,
  NotYourTurn: 35016,
  LoanRateInvalid: 35017,
  LoanNoCredit: 35018,
  GuarantorConflict: 35019,
  AuctionEnded: 35020,
  BidTooLow: 35021,
  NoPrivilege: 35022,
  ListingFull: 35023,
  SelfTrade: 35024,
} as const;

/** P2 交易错误码 → i18n key 映射。 */
export const TRADE_ERR_I18N: Record<number, TKey> = {
  [WEALTH_TRADE_ERR.ListingInvalid]: 'wealth.error.listingInvalid' as TKey,
  [WEALTH_TRADE_ERR.ListingExpired]: 'wealth.error.listingExpired' as TKey,
  [WEALTH_TRADE_ERR.NegotiateNotFound]: 'wealth.error.negotiateNotFound' as TKey,
  [WEALTH_TRADE_ERR.NotYourTurn]: 'wealth.error.notYourTurn' as TKey,
  [WEALTH_TRADE_ERR.LoanRateInvalid]: 'wealth.error.loanRate' as TKey,
  [WEALTH_TRADE_ERR.LoanNoCredit]: 'wealth.error.loanCredit' as TKey,
  [WEALTH_TRADE_ERR.GuarantorConflict]: 'wealth.error.guarantorConflict' as TKey,
  [WEALTH_TRADE_ERR.AuctionEnded]: 'wealth.error.auctionEnded' as TKey,
  [WEALTH_TRADE_ERR.BidTooLow]: 'wealth.error.bidTooLow' as TKey,
  [WEALTH_TRADE_ERR.NoPrivilege]: 'wealth.error.noPrivilege' as TKey,
  [WEALTH_TRADE_ERR.ListingFull]: 'wealth.error.listingFull' as TKey,
  [WEALTH_TRADE_ERR.SelfTrade]: 'wealth.error.selfTrade' as TKey,
};

/** POST /api/games/wealth/rooms 的 wealth 段（协议 §6）。 */
export interface WealthRoomOptions {
  /** 1 游戏月时长 ms，3000–30000，缺省 8000。 */
  month_ms?: number;
  /** "curated" | "docs"，缺省 curated。 */
  pool?: 'curated' | 'docs';
  /** 可选随机种子（测试确定性复现）。 */
  seed?: number;
}

// ── 静态表 ────────────────────────────────────────────────────────────

export interface WealthDistrictDef {
  id: WealthDistrictId;
  /** 中文名（DistrictDefs 权威值；i18n 展示走 wealth.district.<id>）。 */
  nameZh: string;
  /** 主色（后端 DistrictDefs）。 */
  color: string;
  /** 40×40 地图平面坐标（前端 DistrictBlock / 小地图共用）。 */
  x: number;
  z: number;
  /** 房价 beta。 */
  houseBeta: number;
  /** 基准房价（万元/套）。 */
  basePriceWan: number;
}

/** 8 城区静态表（后端架构文档 §4 DistrictDefs，顺序即数组下标）。 */
export const WEALTH_DISTRICTS: WealthDistrictDef[] = [
  { id: 'finance',     nameZh: '金融CBD', color: '#1d4ed8', x: 0,   z: 0,   houseBeta: 1.3,  basePriceWan: 800 },
  { id: 'tech',        nameZh: '科技园',  color: '#0e7490', x: -10, z: 4,   houseBeta: 1.15, basePriceWan: 500 },
  { id: 'industry',    nameZh: '工业区',  color: '#57534e', x: -12, z: -8,  houseBeta: 0.85, basePriceWan: 200 },
  { id: 'oldtown',     nameZh: '老城区',  color: '#92400e', x: 2,   z: -12, houseBeta: 0.8,  basePriceWan: 180 },
  { id: 'commerce',    nameZh: '商业中心', color: '#b91c1c', x: 10,  z: -2,  houseBeta: 1.1,  basePriceWan: 400 },
  { id: 'residential', nameZh: '居住区',  color: '#15803d', x: 0,   z: 12,  houseBeta: 1.0,  basePriceWan: 300 },
  { id: 'suburb',      nameZh: '郊区',    color: '#65a30d', x: -14, z: 14,  houseBeta: 0.7,  basePriceWan: 120 },
  { id: 'riverside',   nameZh: '滨河新区', color: '#7c3aed', x: 14,  z: 10,  houseBeta: 1.25, basePriceWan: 450 },
];

export const WEALTH_DISTRICT_IDS: WealthDistrictId[] =
  WEALTH_DISTRICTS.map((d) => d.id);

/** 按 id 查城区定义。 */
export function wealthDistrict(id: string): WealthDistrictDef | undefined {
  return WEALTH_DISTRICTS.find((d) => d.id === id);
}

/** 城区中心（40×40 世界坐标）；未知 id 回落原点。 */
export function districtCenter(id: string): { x: number; z: number } {
  const d = wealthDistrict(id);
  return d ? { x: d.x, z: d.z } : { x: 0, z: 0 };
}

/** 职业显示元数据（前端架构文档 §3，P0 新定；对比度 ≥4.5:1 于深色地图）。 */
export const PROFESSION_COLORS: Record<string, string> = {
  P01: '#f59e0b', P03: '#94a3b8', P05: '#34d399', P07: '#60a5fa',
  P08: '#fb7185', P09: '#a78bfa', P10: '#f87171', P11: '#fbbf24',
  P15: '#fdba74', P16: '#e879f9',
};

/** 头像 PNG 缺失时的职业 emoji 兜底（降级策略 §9）。 */
export const PROFESSION_EMOJI: Record<string, string> = {
  P01: '🛵', P03: '🚔', P05: '📚', P07: '🏛️', P08: '💼',
  P09: '💻', P10: '🩺', P11: '⚖️', P15: '🥐', P16: '📱',
};

export const WEALTH_DEFAULT_PROFESSION_COLOR = '#9ca3af';
export const WEALTH_DEFAULT_PROFESSION_EMOJI = '🧑‍💼';

/** 精选 10 卡静态镜像（职业卡与加载器设计 §1；仅用于 UI 兜底展示，
 *  权威数据走 GET /api/games/wealth/professions）。 */
export interface CuratedProfession {
  id: string;
  title: string;
  avatar: string;
  color: string;
  emoji: string;
  salary: number;
  expense: number;
  savings: number;
  homeDistrict: WealthDistrictId;
  openingHook: string;
  goal: string;
}

export const CURATED_PROFESSIONS: CuratedProfession[] = [
  { id: 'P01', title: '外卖骑手',  avatar: 'p01', color: PROFESSION_COLORS.P01, emoji: PROFESSION_EMOJI.P01,
    salary: 5000,  expense: 3200, savings: 8000,   homeDistrict: 'commerce',
    openingHook: '风里雨里跑了三年，卡里就八千块。我不想送一辈子外卖，先攒出第一桶金。',
    goal: '5 年内攒下 15 万启动资金，学会让钱替我干活。' },
  { id: 'P03', title: '保安/司机', avatar: 'p03', color: PROFESSION_COLORS.P03, emoji: PROFESSION_EMOJI.P03,
    salary: 5500,  expense: 3500, savings: 10000,  homeDistrict: 'oldtown',
    openingHook: '站岗十小时，月薪五千五。安稳是安稳，可我不想五十岁还在替别人看大门。',
    goal: '5 年内建立每月 2000 元被动收入，给自己多一条路。' },
  { id: 'P05', title: '小学教师',  avatar: 'p05', color: PROFESSION_COLORS.P05, emoji: PROFESSION_EMOJI.P05,
    salary: 9000,  expense: 6000, savings: 30000,  homeDistrict: 'residential',
    openingHook: '粉笔灰吃了七年，存款三万。教书育人不慌，我怕的是一眼望到头的工资条。',
    goal: '5 年内攒够一套郊区房的首付，让家安下来。' },
  { id: 'P07', title: '公务员',    avatar: 'p07', color: PROFESSION_COLORS.P07, emoji: PROFESSION_EMOJI.P07,
    salary: 12000, expense: 8000, savings: 50000,  homeDistrict: 'oldtown',
    openingHook: '体制内第八年，钱不多但稳。同学都下海了，我打算稳中求进慢慢布局。',
    goal: '5 年内完成两套住宅配置，家庭被动收入覆盖基本开销。' },
  { id: 'P08', title: '销售代表',  avatar: 'p08', color: PROFESSION_COLORS.P08, emoji: PROFESSION_EMOJI.P08,
    salary: 12000, expense: 8500, savings: 20000,  homeDistrict: 'commerce',
    openingHook: '靠嘴皮子吃饭，行情好月月超额。趁年轻胆子大，我要把提成变成资产。',
    goal: '5 年内净资产突破 100 万，摆脱纯靠提成吃饭。' },
  { id: 'P09', title: '初级程序员', avatar: 'p09', color: PROFESSION_COLORS.P09, emoji: PROFESSION_EMOJI.P09,
    salary: 15000, expense: 10000, savings: 40000, homeDistrict: 'tech',
    openingHook: '写代码第五年，年包二十来万。我信数据不信运气，定投+记账慢慢滚。',
    goal: '5 年内指数基金持仓 50 万，FI 指数达到 0.5。' },
  { id: 'P10', title: '医生',      avatar: 'p10', color: PROFESSION_COLORS.P10, emoji: PROFESSION_EMOJI.P10,
    salary: 25000, expense: 18000, savings: 100000, homeDistrict: 'residential',
    openingHook: '白大褂下是还不完的房贷。收入高开销也高，我得学会像管理病人一样管理钱。',
    goal: '5 年内还清一半房贷，建立孩子的教育金。' },
  { id: 'P11', title: '律师',      avatar: 'p11', color: PROFESSION_COLORS.P11, emoji: PROFESSION_EMOJI.P11,
    salary: 30000, expense: 20000, savings: 150000, homeDistrict: 'finance',
    openingHook: '时薪三千，照样月光。见惯了财富易主，这次我要做自己案子的当事人。',
    goal: '5 年内构建 1.5 万月被动收入，把时间从时薪里赎回来。' },
  { id: 'P15', title: '早餐店主',  avatar: 'p15', color: PROFESSION_COLORS.P15, emoji: PROFESSION_EMOJI.P15,
    salary: 12000, expense: 7500, savings: 60000,  homeDistrict: 'oldtown',
    openingHook: '凌晨三点的豆浆香，是我全部的家当。生意稳但太单一，得想想退路。',
    goal: '5 年内攒出第二家店的启动金，同时配置一份金融资产。' },
  { id: 'P16', title: '自媒体博主', avatar: 'p16', color: PROFESSION_COLORS.P16, emoji: PROFESSION_EMOJI.P16,
    salary: 9000,  expense: 7000, savings: 20000,  homeDistrict: 'tech',
    openingHook: '三万粉的博主，上个月爆了，这个月凉透。流量是过山车，我要把波动变成台阶。',
    goal: '5 年内用流量收入攒下 40 万稳健资产，告别收入焦虑。' },
];

export function professionColor(id: string): string {
  return PROFESSION_COLORS[id] ?? WEALTH_DEFAULT_PROFESSION_COLOR;
}

export function professionEmoji(id: string): string {
  return PROFESSION_EMOJI[id] ?? WEALTH_DEFAULT_PROFESSION_EMOJI;
}

// ── 动作元数据（ActionPanel 按钮渲染依据；语义唯一事实来源 = 协议 §4）──

/** 参数化动作的表单形态。 */
export type WealthActionFormKind =
  | 'none'                       // 无参确认（study / socialize / rest / work_overtime / stop_side_business / submit_month）
  | 'buy_asset' | 'sell_asset' | 'buy_house' | 'take_loan' | 'repay_loan'
  | 'side_business' | 'move_district' | 'amount' | 'amount_reason'
  | 'consumption_level';         // 消费档位 4 按钮组（ActionPanel 专属渲染，不走通用弹窗）

export interface WealthActionMeta {
  type: WealthActionType;
  icon: string;
  /** i18n key：`wealth.action.<key>`。 */
  i18nKey: string;
  form: WealthActionFormKind;
  /** 简述（tooltip / 规则页），人读。 */
  hint: string;
}

/** 14 个人类按钮动作 + P1 消费档位（顺序 = 产品设计 §7.1 动作条；set_consumption
 *  由 ActionPanel 的档位 4 按钮组专属渲染，通用按钮循环须跳过 form==='consumption_level'）。 */
export const WEALTH_ACTIONS: WealthActionMeta[] = [
  { type: 'buy_asset',  icon: '💵', i18nKey: 'buyAsset',  form: 'buy_asset',    hint: '按市价买入指数基金 / 债券 / 黄金（≥1000 元）' },
  { type: 'sell_asset', icon: '📈', i18nKey: 'sellAsset', form: 'sell_asset',   hint: '卖出持仓资产（房产整售 1 套）' },
  { type: 'buy_house',  icon: '🏠', i18nKey: 'buyHouse',  form: 'buy_house',    hint: '首付 ≥30%，余额 30 年等额本息房贷' },
  { type: 'take_loan',  icon: '🏦', i18nKey: 'takeLoan',  form: 'take_loan',    hint: '消费贷 / 信用贷（5/10/20 万三档）/ 经营贷' },
  { type: 'repay_loan', icon: '💳', i18nKey: 'repayLoan', form: 'repay_loan',   hint: '提前还本 ≥1 万元或结清' },
  { type: 'start_side_business', icon: '🛵', i18nKey: 'sideBusiness', form: 'side_business', hint: '副业月入约 2000–6000 元，月耗精力 2' },
  { type: 'stop_side_business',  icon: '🛑', i18nKey: 'stopBusiness', form: 'none',          hint: '停止副业，精力释放' },
  { type: 'study',      icon: '📚', i18nKey: 'study',      form: 'none',         hint: '2000 元 / 精力-1 / 认知+1' },
  { type: 'socialize',  icon: '🤝', i18nKey: 'socialize',  form: 'none',         hint: '1000 元 / 人脉+1' },
  { type: 'rest',       icon: '😴', i18nKey: 'rest',       form: 'none',         hint: '精力 +2' },
  { type: 'work_overtime', icon: '⚡', i18nKey: 'workOvertime', form: 'none',    hint: '精力-2 / 当月工资 ×0.3 奖金' },
  { type: 'move_district', icon: '🚚', i18nKey: 'moveDistrict', form: 'move_district', hint: '搬家费 3000 元 / 精力-1' },
  { type: 'consume',    icon: '🛍', i18nKey: 'consume',    form: 'amount_reason', hint: '自由消费（记事，无机制效果）' },
  { type: 'donate',     icon: '❤',  i18nKey: 'donate',     form: 'amount',       hint: '每万元 1 社会贡献分；人脉+1（累计前 3 次）' },
  { type: 'set_consumption', icon: '🧾', i18nKey: 'setConsumption', form: 'consumption_level', hint: '消费档位：0 节俭(×0.6/精力-1) / 1 标准 / 2 精致(×1.5/+1) / 3 奢侈(×2.2/+2)' },
];

// ── 消费档位静态表（P1 真实经济循环引擎 §3.2）────────────────────────────

export interface WealthConsumptionLevelMeta {
  /** 0-3。 */
  level: number;
  /** i18n key：`wealth.consumption.level.<i18nKey>`。 */
  i18nKey: 'frugal' | 'normal' | 'refined' | 'luxury';
  /** 生活支出乘数（与后端 consumptionLevelMult 同值）。 */
  multiplier: number;
  /** 精力效果（与后端 consumptionLevelEnergy 同值）。 */
  energy: number;
}

/** 消费档位 4 档（0 节俭 / 1 标准 / 2 精致 / 3 奢侈）。 */
export const WEALTH_CONSUMPTION_LEVELS: WealthConsumptionLevelMeta[] = [
  { level: 0, i18nKey: 'frugal',  multiplier: 0.6, energy: -1 },
  { level: 1, i18nKey: 'normal',  multiplier: 1.0, energy: 0 },
  { level: 2, i18nKey: 'refined', multiplier: 1.5, energy: 1 },
  { level: 3, i18nKey: 'luxury',  multiplier: 2.2, energy: 2 },
];

// ── CPI 八大类静态表（P1 真实经济循环引擎 §2.1）─────────────────────────

export interface WealthGoodsMeta {
  id: string;
  /** 中文名（统计口径；i18n 展示走 wealth.goods.<id>）。 */
  nameZh: string;
  /** PNG 缺失时的 emoji 兜底（goodsIcon(id) 返回 '' 时用）。 */
  emoji: string;
  /** 篮子权重（服务端权威下发，此表仅兜底展示）。 */
  weight: number;
}

/** 八大类顺序 = 权重表序（goodsIcon 图标 / emoji 兜底 / tooltip 中文名）。 */
export const WEALTH_GOODS_META: WealthGoodsMeta[] = [
  { id: 'food',        nameZh: '食品烟酒',     emoji: '🍚', weight: 0.30 },
  { id: 'clothing',    nameZh: '衣着',         emoji: '👕', weight: 0.06 },
  { id: 'housing',     nameZh: '居住',         emoji: '🏠', weight: 0.20 },
  { id: 'household',   nameZh: '生活用品及服务', emoji: '🧻', weight: 0.06 },
  { id: 'transport',   nameZh: '交通通信',     emoji: '🚌', weight: 0.13 },
  { id: 'education',   nameZh: '教育文化娱乐', emoji: '📚', weight: 0.11 },
  { id: 'healthcare',  nameZh: '医疗保健',     emoji: '💊', weight: 0.09 },
  { id: 'misc',        nameZh: '其他用品及服务', emoji: '🛍', weight: 0.05 },
];

/** 八大类 id → 静态元数据。 */
export function wealthGoodsMeta(id: string): WealthGoodsMeta | undefined {
  return WEALTH_GOODS_META.find((g) => g.id === id);
}

export const WEALTH_SUBMIT_MONTH: WealthActionMeta = {
  type: 'submit_month', icon: '✅', i18nKey: 'submitMonth', form: 'none',
  hint: '标记本月完成；全员提交提前进入月结（不耗动作预算）',
};

// ── 展示辅助 ──────────────────────────────────────────────────────────

/** 人民币金额人读化：≥1 亿 → "x.xx亿"；≥1 万 → "x.x万"；否则整数千分位。 */
export function formatCny(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '—';
  const abs = Math.abs(n);
  const sign = n < 0 ? '-' : '';
  if (abs >= 1e8) return `${sign}${(abs / 1e8).toFixed(2)}亿`;
  if (abs >= 1e4) return `${sign}${(abs / 1e4).toFixed(1)}万`;
  return `${sign}${Math.round(abs).toLocaleString('zh-CN')}`;
}

/** 带符号差额（+/-）用于涨跌箭头。 */
export function formatDelta(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '—';
  if (n > 0) return `+${formatCny(n)}`;
  return formatCny(n);
}

/** 百分比展示（0.035 → "3.5%"）。 */
export function formatPct(v: number | undefined | null, digits = 1): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return `${(v * 100).toFixed(digits)}%`;
}

/** FI Index 档位色（前端架构文档 §5：<0.5 灰 / 0.5–1 蓝 / ≥1 绿 / ≥1.5 金）。 */
export function fiIndexColor(fi: number): string {
  if (fi >= 1.5) return '#d4a017';
  if (fi >= 1) return '#34d399';
  if (fi >= 0.5) return '#60a5fa';
  return '#9ca3af';
}

/** 明斯基融资等级色（暗色主题 ≥5:1）。 */
export function minskyTierColor(tier: WealthMinskyTier | undefined | null): string {
  switch (tier) {
    case 'hedge': return '#4caf50';       // 对冲 — 绿（白字 ≈ 5.4:1）
    case 'speculative': return '#ff9800'; // 投机 — 橙（白字 ≈ 4.6:1 on #10151d）
    case 'ponzi': return '#f44336';       // 庞氏 — 红（白字 ≈ 5.7:1）
    default: return '#9ca3af';
  }
}
