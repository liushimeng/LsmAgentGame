import type { TKey } from '@/i18n';

// ─── 虚拟城市 (VirtualCity) types ───
//
// 与 lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-WS与HTTP协议契约-v1.md §3
// 「game.state 载荷逐字段契约」**逐字段对齐**（字段名 / 可空性一字不改；
// 后端 game/virtual-city/view.go::BuildClientState 是协议唯一实现）。
// 静态表（城区 / 职业色 / 动作元数据）出处：协议 §4 + 后端架构文档 §4 DistrictDefs
// + 前端架构文档 §3 职业色（P0 新定）。

/** 32 城区 id（顺序 = DistrictDefs 静态表 / market.districts 数组顺序；
 *  前 8 为 P0 原有城区，中 8 为 v2.12 阶段 2 扩展城区，后 16 为批次 20 城市扩张城区）。 */
export type VirtualCityDistrictId =
  | 'finance' | 'tech' | 'industry' | 'oldtown'
  | 'commerce' | 'residential' | 'suburb' | 'riverside'
  | 'logistics_port' | 'hightech_park' | 'edu_district' | 'medical_city'
  | 'industrial_park' | 'central_park' | 'transport_hub' | 'cultural_creative'
  // ── 批次 20 新增 16 区（下标 16–31，顺序 = 文档 1 §2 表）──
  | 'fin_sub_center' | 'software_park' | 'airport_town' | 'air_logistics'
  | 'auto_city' | 'mountain_resort' | 'chem_park' | 'agri_park'
  | 'health_town' | 'steel_town' | 'old_city_culture' | 'university_town'
  | 'wetland_park' | 'sports_new_city' | 'bay_new_town' | 'highspeed_rail_town';

/** 市场周期四阶段（《规则》§7.1）。 */
export type VirtualCityCyclePhase = 'recovery' | 'boom' | 'recession' | 'depression';

/** 明斯基融资等级（v2.60 N11-4）。 */
export type VirtualCityMinskyTier = 'hedge' | 'speculative' | 'ponzi';

/** 月内相位：acting=动作窗口 / settling=月结中。 */
export type VirtualCityPhase = 'acting' | 'settling';

export type VirtualCityStatus = 'open' | 'playing' | 'over';
/** 全 Agent 模式标志(2026-09-19 §全Agent模式): true = 全 Agent 房间,人类不能加入对局。 */
export type VirtualCityFullAgentMode = boolean;


/** 月总收入档（《规则》§5.3）。 */
export type VirtualCityIncomeBand = 'low' | 'mid' | 'high' | 'top';

/** 本月最近动作类别（players[].status_icon）。 */
export type VirtualCityStatusIcon = 'working' | 'idle' | 'trading' | 'resting' | 'moved';

/** 资产种类（asset.kind；house/shop 带城区后缀）。 */
export type VirtualCityAssetKind =
  | 'stock_index' | 'bond' | 'gold'
  | `house:${VirtualCityDistrictId}` | `shop:${VirtualCityDistrictId}`
  | 'side_business' | 'pension';

/** 贷款种类（loan.kind）。 */
export type VirtualCityLoanKind =
  | 'mortgage' | 'consumer' | 'credit_tier1' | 'credit_tier2' | 'credit_tier3' | 'business';

// ── game.state 载荷（协议 §3）──────────────────────────────────────────

export interface VirtualCityCycle {
  phase: VirtualCityCyclePhase;
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

export interface VirtualCityDistrictMarket {
  id: VirtualCityDistrictId;
  /** 房价指数（初始 1.0）。 */
  price_index: number;
  /** 租金指数（price_index × 0.0016 归一）。 */
  rent_index: number;
}

export interface VirtualCityMarket {
  /** 股票指数，元/份（初始 3.50）。 */
  stock_index: number;
  /** 黄金，元/克（初始 750）。 */
  gold_price: number;
  /** 当期新购债券年化，小数（如 0.032）。 */
  bond_yield: number;
  /** 32 项，顺序 = DistrictDefs 静态表。 */
  districts: VirtualCityDistrictMarket[];
  // ── 批次 20 文档 3 B4：股票交易微观结构（旧局/旧后端缺省 undefined，UI 兜底）──
  /** 股票买入单价（idx × (1 + spread/2)），元/份。 */
  stock_buy_unit?: number;
  /** 股票卖出单价（idx × (1 − spread/2)），元/份。 */
  stock_sell_unit?: number;
  /** 买卖价差（基点，按市场阶段 4/8/15/20bps）。 */
  spread_bps?: number;
  /** 熔断「最后一个禁止月」（0/缺省 = 从未触发；month ≤ 此值期间禁买禁卖股票）。 */
  breaker_until?: number;
}

/** 央行货币政策快照（game.state.central_bank，P1 央行引擎下发）。 */
export interface VirtualCityCentralBank {
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

export interface VirtualCityProfession {
  /** 职业卡 id（"P01"…；文档池 "N9012345"）。 */
  id: string;
  /** 中文名，如 "外卖骑手"。 */
  title: string;
  /** 头像文件名主干（"p01" → assets/images/virtualCity/agents/p01.png）。 */
  avatar: string;
}

export interface VirtualCityResources {
  energy: number;
  network: number;
  cognition: number;
}

export interface VirtualCityPlayer {
  seat: number;              // 0..VIRTUAL_CITY_MAX_SEATS-1（2026-09-16 起 10–12 座位）
  account: string;           // bot 为 bot_<modelkey>
  nickname: string;
  is_bot: boolean;
  /** bot 的 agent_name；人类为 ""。 */
  model_display: string;
  profession: VirtualCityProfession;
  /** 当前所在区 id。 */
  district: VirtualCityDistrictId;
  /** 住房所在区 id。 */
  home_district: VirtualCityDistrictId;
  alive: boolean;
  /** P0 恒 false（P1 提前退休预留）。 */
  retired: boolean;
  /** 个人年龄（文档池差异卡展示用）。 */
  age: number;
  resources: VirtualCityResources;
  /** 元（公开）。 */
  net_worth: number;
  /** 0–2 封顶（公开）。 */
  fi_index: number;
  income_band: VirtualCityIncomeBand;
  status_icon: VirtualCityStatusIcon;
  /** 人读，如 "买入黄金 50g"；空 = 本月未动作。 */
  last_action: string;
  /** 终局结局 id；进行中为 ""。 */
  ending: string;
  /** 明斯基融资等级（P1 明斯基引擎，仅本人座位下发）。 */
  minsky_tier?: VirtualCityMinskyTier;
  /** 月供/月收入比（0-1+；P1 明斯基引擎，仅本人座位下发）。 */
  debt_to_income?: number;
  /** 消费档位 0-3（P1 真实经济循环引擎；档位是公开生活方式，全座位下发）。 */
  consumption_level?: number;
  /** Active 保单险种列表（P1-4 商业保险；是否投保是公开信息，全座位下发）。 */
  insured_kinds?: string[];
}

export interface VirtualCityMonthlyDetail {
  /** "salary" | "tax" | "living" | …（引擎扩展开放）。 */
  key: string;
  amount_cny: number;
  text: string;
}

export interface VirtualCityMonthly {
  income: number;
  expense: number;
  net: number;
  tax: number;
  social: number;
  detail: VirtualCityMonthlyDetail[];
}

export interface VirtualCityAsset {
  kind: VirtualCityAssetKind | string;
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

/** 副业定价档位（批次 20 文档 2 §2；0=中价默认，兼容旧档零值 / 1=低价 / 2=高价）。 */
export type VirtualCitySidePriceTier = 0 | 1 | 2;

export interface VirtualCitySideTierMeta {
  tier: VirtualCitySidePriceTier;
  /** i18n key：`virtualCity.sidePrice.<i18nKey>`。 */
  i18nKey: 'low' | 'mid' | 'high';
  /** 客群权重 w(t)（低价 0.50 / 中价 0.30 / 高价 0.20）。 */
  weight: number;
  /** 收入乘数 m(t)（低价 0.75 / 中价 1.00 / 高价 1.25）。 */
  multiplier: number;
  /** 档位徽章底色（§26.1：低=蓝 中=绿 高=橙，白字 ≥4.5:1）。 */
  badgeColor: string;
}

/** 三档定价静态表（文档 2 §1 数值契约；渲染顺序 = 低/中/高）。 */
export const WEALTH_SIDE_TIERS: VirtualCitySideTierMeta[] = [
  { tier: 1, i18nKey: 'low',  weight: 0.50, multiplier: 0.75, badgeColor: '#1d4ed8' },
  { tier: 0, i18nKey: 'mid',  weight: 0.30, multiplier: 1.00, badgeColor: '#15803d' },
  { tier: 2, i18nKey: 'high', weight: 0.20, multiplier: 1.25, badgeColor: '#c2410c' },
];

export function virtualCitySideTierMeta(tier: number | undefined | null): VirtualCitySideTierMeta {
  return WEALTH_SIDE_TIERS.find((x) => x.tier === tier) ?? WEALTH_SIDE_TIERS[1];
}

/** 副业品类开档认知门槛（后端 actions.go sideBizDefs.GateCognition 同值；
 *  高价档对 tutoring/freelance 另要求 认知 ≥ 门槛+1 —— 仅前端 tooltip 提示，服务端权威）。 */
export const WEALTH_SIDE_BIZ_GATE: Record<string, number> = {
  delivery: 0, content: 2, tutoring: 4, freelance: 3,
};

/** 熔断是否生效（文档 3 B2-4：breaker_until 存「最后一个禁止月」，
 *  0/缺省 = 从未触发；month ≤ breaker_until 期间禁买禁卖 stock_index）。 */
export function virtualCityStockBreakerActive(gs: VirtualCityGameState | null | undefined): boolean {
  const until = gs?.market?.breaker_until ?? 0;
  const month = gs?.month ?? 0;
  return until > 0 && month <= until;
}

/** 副业定价预期收入前端估算：Base × m(t) × s（整数；文档 2 §5）。 */
export function virtualCitySideExpectedIncome(
  baseIncome: number | undefined | null, tier: number, share: number,
): number {
  const m = virtualCitySideTierMeta(tier).multiplier;
  const s = Number.isFinite(share) ? Math.max(0, Math.min(1, share)) : 1;
  return Math.round((baseIncome ?? 0) * m * s);
}

/** 玩家侧栏副业条目（my.side_business，批次 20 文档 2 §3；无副业 = omit/null）。 */
export interface VirtualCitySideBusiness {
  kind: string;
  /** 后端人读中文名（i18n 缺键时兜底）。 */
  kind_cn?: string;
  base_income: number;
  opened_month?: number;
  price_tier: VirtualCitySidePriceTier | number;
  price_tier_cn?: string;
  /** 当前客群份额 0..1（独占 = 1.0）。 */
  market_share: number;
  /**
   * 批次 20 文档 2 §2（集成契约更新）：最近改档月（主钟月；0/缺省=未改过）。
   * 「同月限改 1 次」禁用判定 = tier_set_month === game.state.month。
   */
  tier_set_month?: number;
}

/** side_market 单经营者行（game.state.side_market[kind][]，seat 升序；文档 2 §3）。 */
export interface VirtualCitySideMarketRow {
  seat: number;
  tier: VirtualCitySidePriceTier | number;
  share: number;
}

export interface VirtualCityLoan {
  id: string;                // "L3"
  kind: VirtualCityLoanKind | string;
  principal: number;
  balance: number;
  annual_rate: number;
  monthly_payment: number;
  months_left: number;
}

export interface VirtualCityFamily {
  marital: 'single' | 'married';
  children: number;
}

/** 仅本人座位填充；观战者 null。 */
export interface VirtualCityMyState {
  cash: number;
  /** 当前基准月薪（税前）。 */
  salary: number;
  /** 配偶月收入（税后净额）。 */
  spouse_income: number;
  /** 上月副业净收入。 */
  side_income: number;
  /** 副业侧栏（批次 20 文档 2 §3：定价档 + 客群份额；无副业 = omit/null）。 */
  side_business?: VirtualCitySideBusiness | null;
  /**
   * 批次 20 文档 3 B2-3（集成契约更新）：当月新买入股票 T+1 冻结份数
   * （卖出可卖量 = 持仓 − 此值；月结开头清零）。旧后端缺省 undefined = 0。
   */
  stock_t1_locked?: number;
  /** 上月被动收入合计。 */
  passive_income: number;
  /** 最近一次月结（或当月预估）。 */
  monthly: VirtualCityMonthly;
  resources: VirtualCityResources;
  assets: VirtualCityAsset[];
  loans: VirtualCityLoan[];
  /** 养老金账户余额。 */
  pension_cny: number;
  /** 定期存款（M2 组成部分，元）。 */
  savings_deposit: number;
  /** 400–850。 */
  credit_score: number;
  family: VirtualCityFamily;
  fi_index: number;
  net_worth: number;
  /** 职业卡 goals（含 5 年目标），终局对照展示。 */
  goals: string[];
  /** 上月消费结构（CPI 八大类 id → 金额元；P1 真实经济循环引擎，仅本人座位下发）。 */
  consumption_by_goods?: Record<string, number>;
  /** 商业保险段（P1-4 保险引擎；仅本人座位下发，insurance_enabled=false 时整体 omit）。 */
  insurance?: VirtualCityInsuranceState;
}

/** Agent 思维可见性：本人座位 + 观战者可见；其他玩家不可见。 */
export interface VirtualCityBotContext {
  /** 房间当前月（服务端 BotCtxJSON.month）。 */
  month: number;
  /** last_decision_summary 所属月；0 表示尚无决策。 */
  last_decision_month: number;
  /** transcript 更新时间，unix_ms。 */
  updated_at: number;
  /** 座位是否存活；等价 players.alive，旧帧缺失时前端回退该字段。 */
  active: boolean;
  seat: number;
  /** Agent 自述本月决策（≤120 字）。 */
  last_decision_summary: string;
  /** JSON 字符串。 */
  last_tool_input: string;
  /** 人读结果。 */
  last_tool_result: string;
  /** 内心独白（speak 的 internal_thought）。 */
  heart_thought: string;
  /** 本月感知记录（2026-09-22 §CityHuman重构：see/hear/smell 结果，本人+观察者可见）。 */
  last_senses?: VirtualCitySenseResult[];
}

// ── City-Human 感知契约（2026-09-22 §CityHuman重构，文档 1 §4.3/§6）──

/** 感知结果中的邻居摘要（来自已锚定的真实人物卡档案）。 */
export interface VirtualCityNeighborBrief {
  name: string;
  occupation?: string;
  /** 焦点层座位号（背景居民省略）。 */
  seat?: number;
  /** 人物卡号；档案未就绪时省略。 */
  card_id?: string;
}

/** see/hear/smell 工具的统一返回（确定性构造，JSON 键与后端 SenseResult 逐字对齐）。 */
export interface VirtualCitySenseResult {
  /** 感知类型：see | hear | smell。 */
  kind?: 'see' | 'hear' | 'smell';
  district: string;
  /** see 专用：同区居民。 */
  people?: VirtualCityNeighborBrief[];
  /** see：挂牌/店铺/建筑。 */
  things?: string[];
  /** see/hear：本区本月事件。 */
  events?: string[];
  /** hear：近期公开发言摘录。 */
  utterances?: string[];
  /** smell：气味标签。 */
  smells?: string[];
  /** hear/smell 共用：环境声。 */
  sounds?: string[];
}

export interface VirtualCityLedgerEntry {
  month: number;
  from: string;
  to: string;
  amount_cny: number;
  category: string;
  note: string;
}

export interface VirtualCityRecentEvent {
  month: number;
  type: string;
  text: string;
}

// ── 城市背景层（2026-09-21 §建房解耦，lag_docs/财商流游戏/已实现/03-城市层）──
//
// game.state.city（view.go omitempty）：resident_count=0 的旧房整体不下发，
// CityStatsPanel 据此整面板不渲染。字段名与后端 view.go 逐字对齐。

/** 城区人口条目（city.districts[]；id 为后端 DistrictDefs 数组下标）。 */
export interface VirtualCityCityDistrictPop {
  id: number;
  name: string;
  population: number;
}

/**
 * 居民人物卡档案锚定进度（city.profiles，档案锚定设计 §8.1）。
 * status：idle=未启用（精选手卡 / 未建城）| hydrating=后台水合中 | ready=已锚定 | failed=失败（合成兜底）。
 * pool_size = 文档池人物卡总数（≈100,267）；anchored = 已锚定居民数。
 */
export interface VirtualCityCityProfileProgress {
  status: 'idle' | 'hydrating' | 'ready' | 'failed';
  done: number;
  total: number;
  pool_size: number;
  anchored: number;
}

/** 居民之声条目（city.voices[]，最近若干条；model = 服务该次生成的模型名）。
 *  resident_id / occupation 仅在档案锚定后下发（锚定前为空，前端兼容）。 */
export interface VirtualCityCityVoice {
  month: number;
  name: string;
  text: string;
  model: string;
  /** 发声居民的人物卡号（ResidentProfile.card_id）；锚定后才有。 */
  resident_id?: string;
  /** 发声居民的职业；锚定后才有。 */
  occupation?: string;
}

/**
 * 单名居民的档案投影（REST /city/residents 行 + 抽屉详情卡；档案锚定设计 §4/§8.1）。
 * 全部字段来自人物卡 frontmatter，公开合成人格（无隐私，观战者/玩家同可见）。
 */
export interface VirtualCityCityResidentProfile {
  card_id: string;
  name: string;
  occupation: string;
  /** L1 域展示名（行业）。 */
  domain_name: string;
  /** 城区展示名。 */
  district: string;
  age: number;
  /** 月收入（元，档案值；后续月结演化可变）。 */
  income: number;
  expense: number;
  savings: number;
  employed: boolean;
  /** 储蓄 < 3×月支出（对齐合成判定）。 */
  stressed: boolean;
  /** 「、」连接的 2-4 个人格词。 */
  personality: string;
  /** 档案开场白（≤60 rune）。 */
  opening_hook: string;
  /** goals[0]（5 年目标）。 */
  goal: string;
  /** single|married。 */
  marital: string;
  /** A|B|C。 */
  health_grade: string;
  /** 相对路径（审计：可回溯源 md）。 */
  source_file: string;
}

/** 城区当月氛围标签（city.ambiance，2026-09-22 §CityHuman重构；全员可见，地图氛围渲染）。 */
export interface VirtualCityDistrictAmbiance {
  smells: string[];
  sounds: string[];
}

/** 城市背景层快照（game.state.city）。 */
export interface VirtualCityCitySnapshot {
  resident_count: number;
  /** 就业率（0-1 小数）。 */
  employment_rate: number;
  /** 收入中位数（元/月）。 */
  median_income: number;
  /** 居民储蓄合计（元）。 */
  total_savings: number;
  /** 平均年龄（岁）。 */
  avg_age: number;
  /** 压力率（0-1 小数）。 */
  stressed_rate: number;
  districts: VirtualCityCityDistrictPop[];
  voices: VirtualCityCityVoice[];
  /** 人物卡档案锚定进度（档案锚定设计 §8.1；锚定未启用的旧房 omit）。 */
  profiles?: VirtualCityCityProfileProgress;
  /** 每城区当月气味/声响标签（§CityHuman重构；键 = 城区 id）。 */
  ambiance?: Record<string, VirtualCityDistrictAmbiance>;
  /** 居民驱动层快照（17-CityHuman §2 §6；驱动层未启用/旧房 omit）。 */
  driver?: {
    /** 驱动层是否启用（city_driver_enabled）。 */
    enabled: boolean;
    /** 单月并发 worker 数（city_driver_workers）。 */
    workers: number;
    /** 每月驱动居民数预算（city_driver_per_month）。 */
    per_month: number;
    /** 上月实际驱动居民数。 */
    driven_last: number;
    /** 本房生效线路配额（2026-09-25 §LLM线路池配额）。 */
    lines: number;
  };
}

/** game.state 全量快照（按座位脱敏，BroadcastTo 单发）。 */
export interface VirtualCityGameState {
  room_id: string;
  game_kind: 'virtual_city';
  status: VirtualCityStatus;
  /** 1..420。 */
  month: number;
  /** 主时钟年龄。 */
  age: number;
  phase: VirtualCityPhase;
  cycle: VirtualCityCycle;
  market: VirtualCityMarket;
  /** 央行货币政策快照（P1 央行引擎下发）。 */
  central_bank: VirtualCityCentralBank;
  max_seat: number;
  /** unix_ms；前端倒计时 = next_month_at − now。 */
  next_month_at: number;
  /** unix_s；RoomRunningClock 同源语义。 */
  game_started_at: number;
  players: VirtualCityPlayer[];
  /** -1 = 观战。 */
  my_seat: number;
  /** 本人区内坐标 [0,1]²（§CityHuman重构；仅本人/观察者下发，旧回放 omit）。 */
  my_local_pos?: { x: number; y: number };
  my: VirtualCityMyState | null;
  bot_contexts: VirtualCityBotContext[];
  /** 最近 50 条：本人相关 + 公共。 */
  ledger_recent: VirtualCityLedgerEntry[];
  /** 最近 100 条。 */
  events_recent: VirtualCityRecentEvent[];
  /** 明斯基全局概览（P1 明斯基引擎）。 */
  minsky_overview?: VirtualCityMinskyOverview;
  /** 提前还款资格（仅本人座位下发；P1 LPR 重定价）。 */
  early_repay_eligible?: boolean;
  /** 消费品市场快照（P1 真实经济循环引擎；economy_enabled=false 时零值/缺失）。 */
  consumer_market?: VirtualCityGoodsMarket;
  /** 劳动力市场快照（P1 真实经济循环引擎）。 */
  labor_market?: VirtualCityLaborMarket;
  /** 社会结构统计（P1 真实经济循环引擎）。 */
  society?: VirtualCitySociety;
  /** 社会调研（open ≤1 + 最近 4 个 closed；P1 社会调研系统）。 */
  surveys?: VirtualCitySurvey[];
  /** 挂单簿（P2 交易系统；房间级）。 */
  listing_book?: VirtualCityListingBook;
  /** 月度资金流向（P2 v2 §13.2.4 财富流动可视化）。 */
  flow_stat?: VirtualCityFlowStat;
  /** 城市背景层快照（§20260921 建房解耦；resident_count=0 旧房 omit）。 */
  city?: VirtualCityCitySnapshot;
  /** 副业定价市场（批次 20 文档 2 §3）：kind → 经营者行（seat 升序）；
   *  仅含有经营者的品类（≤12×4 条），无经营者 = omit。 */
  side_market?: Record<string, VirtualCitySideMarketRow[]>;
  /** 公共服务 + 监管 + 市长选举快照（阶段 8；economy_enabled=false 旧房 omit。
   *  FE-2 仅消费选举四字段段，其余字段原样透传不声明）。 */
  public_services?: VirtualCityPublicServices;
}

// ── 市长选举（批次 20 文档 3 A1/A3；game.state.public_services 选举段）────

/** 单座得票明细（civic_election.go ElectionVote，30/40/30 得票模型）。 */
export interface VirtualCityElectionVote {
  seat: number;
  /** 综合得分 0..100。 */
  score: number;
  /** 财富排名分 0..100。 */
  virtualCity_score: number;
  /** 人脉分 0..100。 */
  network_score: number;
  /** 社会满意度 0..100（全城同值）。 */
  satisfaction: number;
}

/** public_services 选举段（未启用：election_enabled=false / mayor_seat=-1 / votes 空）。 */
export interface VirtualCityPublicServices {
  election_enabled: boolean;
  /** 现任市长座位；-1 = 尚无。 */
  mayor_seat: number;
  last_votes?: VirtualCityElectionVote[];
  /** 下届选举月 = LastElectionMonth + 48（未启用/未选过 = omit/0）。 */
  next_election_month?: number;
  /** 上月市长津贴是否断发（国库不足；后端 omitempty —— false 不下发，缺省即未停发）。 */
  stipend_stopped?: boolean;
  /** 公共服务 / 监管等其余字段 FE-2 不消费，保留透传。 */
  [key: string]: unknown;
}

/** 市长选举届期间隔月（后端 ElectionIntervalMonths = 48，4 年一届）。 */
export const WEALTH_ELECTION_INTERVAL_MONTHS = 48;

/** 任期已过月数（lastElection = next_election_month − 48；无数据 = 0）。 */
export function virtualCityElectionTermElapsed(
  month: number | undefined | null, nextElectionMonth: number | undefined | null,
): number {
  if (!nextElectionMonth || !month) return 0;
  return Math.max(0, Math.min(
    WEALTH_ELECTION_INTERVAL_MONTHS,
    month - (nextElectionMonth - WEALTH_ELECTION_INTERVAL_MONTHS),
  ));
}

/** 明斯基全局概览（game.state.minsky_overview，P1 明斯基引擎）。 */
export interface VirtualCityMinskyOverview {
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
// 与 lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-真实经济循环引擎-v1.md
// §6「视图与协议」逐字段对齐（JSON 名不可改；后端 view.go 是唯一实现）。

/** CPI 八大类消费单品（game.state.consumer_market.goods[]，按 §2.1 权重表序）。 */
export interface VirtualCityGoodsItem {
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
export interface VirtualCityGoodsMarket {
  /** CPI 同比（12 月滚动年化，小数；不足 12 月时值保留但口径未就绪）。 */
  cpi_yoy: number;
  /** CPI 上月环比（小数）。 */
  cpi_mom: number;
  /** 8 类，按权重表顺序。 */
  goods: VirtualCityGoodsItem[];
}

/** 劳动力市场快照（game.state.labor_market）。 */
export interface VirtualCityLaborMarket {
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
export interface VirtualCitySocietyCircles {
  /** 生存圈（月被动收入 < 月支出）。 */
  survival: number;
  /** 积累圈（1 ≤ 比值 < 2）。 */
  accumulate: number;
  /** 自由圈（比值 ≥ 2）。 */
  freedom: number;
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
}

/** 财富金字塔单层（v2 新增）。 */
export interface VirtualCityPyramidLayer {
  name: 'survival' | 'accumulation' | 'freedom';
  count: number;
  total_wealth: number;
  avg_wealth: number;
  /** 占总财富比例 0-1。 */
  virtualCity_pct: number;
}

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
  id: 'salary' | 'firms' | 'market' | 'bank' | 'gov' | 'player' | 'world';
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

// ── P1 社会调研系统（对全体 Agent 的预测模拟）───────────────────────────
//
// 与 lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-社会调研系统-v1.md
// §5「视图与协议」逐字段对齐。

/** 调研聚合结果（VirtualCitySurvey.result；选项文本一并下发，前端免查表）。 */
export interface VirtualCitySurveyResult {
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
export interface VirtualCitySurvey {
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
  result?: VirtualCitySurveyResult;
}

// ── P2 玩家间交易与财富流动系统（lag_docs/虚拟城市/已实现/06-P2交易系统/）────
//
// 与 §3 数据结构逐字段对齐（JSON 名不可改；后端 view.go 是唯一实现）。

/** 挂单类型（Listing.Type）。 */
export type VirtualCityListingType = 'asset' | 'buy' | 'info' | 'loan_ofr' | 'loan_req';

/** 挂单状态（Listing.Status）。 */
export type VirtualCityListingStatus = 'open' | 'negotiating' | 'deal' | 'expired' | 'cancelled';

/** 议价动作（NegotiateTurn.Action）。 */
export type VirtualCityNegotiateAction = 'offer' | 'accept' | 'reject' | 'counter';

/** 议价会话状态（NegotiateSession.Status）。 */
export type VirtualCityNegotiateStatus = 'active' | 'deal' | 'reject' | 'expired';

/** 借贷方向（LoanPayload.Direction）。 */
export type VirtualCityLoanDirection = 'lend' | 'borrow';

/** 信息类别（InfoOfferPayload.Category）。 */
export type VirtualCityInfoCategory = 'market' | 'intel' | 'personal';

/** 拍卖类型。 */
export type VirtualCityAuctionKind = 'english' | 'sealed' | 'take_it' | 'single_round';

/** 拍卖状态。 */
export type VirtualCityAuctionStatus = 'pending' | 'active' | 'ended';

/** 资产挂单详情（ListingPayload.Asset）。 */
export interface VirtualCityTradeAssetPayload {
  kind: string;
  name: string;
  units: number;
  price: number;
  value_cny: number;
  /** 底价（卖家保密；仅本人座位下发时可填，不下发他人）。 */
  min_cny: number;
}

/** 借贷挂单详情（ListingPayload.Loan）。 */
export interface VirtualCityTradeLoanPayload {
  direction: VirtualCityLoanDirection;
  principal_cny: number;
  max_rate: number;
  term_n: number;
  need_guarantee: boolean;
}

/** 信息出售详情（ListingPayload.Info）。 */
export interface VirtualCityTradeInfoPayload {
  category: VirtualCityInfoCategory;
  title: string;
  detail_hash: string;
  min_bid_cny: number;
}

/** 挂单内容联合（ListingPayload）。 */
export interface VirtualCityListingPayload {
  asset?: VirtualCityTradeAssetPayload;
  loan?: VirtualCityTradeLoanPayload;
  info?: VirtualCityTradeInfoPayload;
}

/** 单笔挂单（挂单簿条目）。 */
export interface VirtualCityListing {
  id: string;
  type: VirtualCityListingType;
  seat: number;
  payload: VirtualCityListingPayload;
  ask_cny: number;
  status: VirtualCityListingStatus;
  create_month: number;
  expire_month: number;
}

/** 单轮议价（NegotiateSession.Turns[]）。 */
export interface VirtualCityNegotiateTurn {
  from: number;
  offer_cny: number;
  rate: number;
  comment: string;
  action: VirtualCityNegotiateAction;
}

/** 单笔议价会话。 */
export interface VirtualCityNegotiateSession {
  id: string;
  listing_id: string;
  proposer_seat: number;
  respond_seat: number;
  turns: VirtualCityNegotiateTurn[];
  status: VirtualCityNegotiateStatus;
  create_month: number;
  expire_month: number;
  last_offer_cny: number;
  last_offer_by: number;
}

/** 玩家间借贷合约（deal 后生成）。 */
export interface VirtualCityP2PLoan {
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
export interface VirtualCityAuctionBid {
  seat: number;
  amount_cny: number;
  month: number;
  /** 密封暗标揭示后有效。 */
  revealed?: boolean;
}

/** 拍卖场次。 */
export interface VirtualCityAuction {
  id: string;
  listing_id: string;
  kind: VirtualCityAuctionKind;
  status: VirtualCityAuctionStatus;
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
  bids: VirtualCityAuctionBid[];
  /** 英式拍卖：卖家保留价（< 此价可拒绝）。 */
  reserve_cny?: number;
}

/** 房间级挂单簿（game.state.listing_book）。 */
export interface VirtualCityListingBook {
  listings: VirtualCityListing[];
  negotiates: VirtualCityNegotiateSession[];
  p2p_loans: VirtualCityP2PLoan[];
  auctions: VirtualCityAuction[];
}

/**
 * 单座位回答明细（引擎 SurveyAnswer 镜像）。公开快照**不下发** Answers
 * （匿名投票原理）；本类型为 P2「按模型切片」分析预留，勿用于 UI 渲染。
 */
export interface VirtualCitySurveyAnswer {
  seat: number;
  /** 选项下标 0-based。 */
  option_idx: number;
  /** ≤50 字理由。 */
  reason: string;
  /** 回答者模型 key。 */
  model_key: string;
}

// ── P1-4 商业保险与风险转移引擎（2026-09-19 §财商流P1-4）────────────────
//
// 与 lag_docs/虚拟城市/已实现/10-P1保险系统/虚拟城市-P1-商业保险与风险转移引擎-v1.md
// §8.2 逐字段对齐（JSON 名不可改；后端 view.go/insurance.go 是唯一实现）。

/** 险种（每人每险种最多 1 张有效保单；顺序 = 后端 insuranceKindOrder）。 */
export type VirtualCityInsuranceKind =
  | 'critical_illness' | 'medical_million' | 'term_life' | 'accident';

/** 保单展示状态（后端 Policy.Status 派生，§2.3：active 有效 / waiting 等待期 / grace 宽限期 / lapsed 失效）。 */
export type VirtualCityPolicyStatus = 'active' | 'waiting' | 'grace' | 'lapsed';

/** 单张保单（my.insurance.policies[]，含失效，每险种最多 1 张、最近 8 张）。 */
export interface VirtualCityInsurancePolicy {
  kind: VirtualCityInsuranceKind | string;
  annual_premium_cny: number;
  /** 月缴 = round(年缴/12)，后端下发。 */
  monthly_premium_cny: number;
  /** 保额（投保时名义锁定）；medical_million 为 0 = 比例报销 90%（UI 显示「报销 90%」）。 */
  coverage_cny: number;
  start_month: number;
  paid_months: number;
  /** 等待期剩余月（0=已过）。 */
  waiting_left: number;
  status: VirtualCityPolicyStatus | string;
  /** 累计已赔付（展示用）。 */
  claims_total_cny: number;
}

/** 未投保 / 已失效险种的当前年龄档报价（my.insurance.quotes[]）。 */
export interface VirtualCityInsuranceQuote {
  kind: VirtualCityInsuranceKind | string;
  annual_premium_cny: number;
  coverage_cny: number;
}

/** 商业保险段（game.state.my.insurance；观战者与 insurance_enabled=false 时 omit）。 */
export interface VirtualCityInsuranceState {
  policies: VirtualCityInsurancePolicy[];
  /** Active 保单月缴合计（后端按 Active 求和）。 */
  monthly_premium: number;
  quotes: VirtualCityInsuranceQuote[];
}

/** 4 险种固定展示顺序（§10.1：重疾 → 百万医疗 → 定期寿险 → 意外）。 */
export const WEALTH_INSURANCE_KINDS: VirtualCityInsuranceKind[] = [
  'critical_illness', 'medical_million', 'term_life', 'accident',
];

/** 百万医疗报销比例（医疗险 coverage_cny=0 时的展示口径，§2.1）。 */
export const WEALTH_MEDICAL_REIMBURSE_PCT = 90;

/** 投保 / 退保动作载荷（game.virtual_city_action，§8.1；复用 Action{Type,Kind}）。 */
export type VirtualCityInsuranceAction =
  | { type: 'buy_insurance'; kind: VirtualCityInsuranceKind }
  | { type: 'cancel_insurance'; kind: VirtualCityInsuranceKind };

// ── 座位容量常量（2026-09-16 §财商流10–12座位改造）──────────────────────
//
// 与后端 ServerGo/game/virtual-city/engine.go 的 MaxSeats / MinSeats 同值同义：
//   MaxSeats = 12 → 房间容量 12（「1 人类 + 11 Agent」或「全 12 Agent」）
//   MinSeats = 10 → 已占座 < 10 时 Start 返回 35003 ErrWealthNotEnoughPlayers
// 前端一切座位相关长度（建房档位 / 座位模型下拉 / 渲染环 / 提前开始门控）
// **必须**引用这些常量，禁止再出现 7 / 8 之类的魔数（§130「声明了却从不接线」）。

/** 房间座位上限（后端 virtualCity.MaxSeats）。game.state.max_seat 缺失时的兜底值。 */
export const VIRTUAL_CITY_MAX_SEATS = 12;

/** 最少开局座位（后端 virtualCity.MinSeats）：不足则无法开局。 */
export const VIRTUAL_CITY_MIN_SEATS = 10;

/** 建房弹窗 Agent 数默认值（10 Agent + 创建者 = 11 座 ≥ MinSeats → 自动开局）。 */
export const WEALTH_DEFAULT_AGENT_COUNT = 10;

/** 座位容量解析：服务端权威 max_seat 优先，非法/未到达时回落常量。 */
export function virtualCitySeatCapacity(
  gs: Pick<VirtualCityGameState, 'max_seat'> | null | undefined,
): number {
  const n = gs?.max_seat;
  return typeof n === 'number' && n > 0 ? n : VIRTUAL_CITY_MAX_SEATS;
}

/**
 * 已占座人数。后端 players[] **恒为 max_seat 长度**（空座位是 account/nickname
 * 全空的占位对象），因此 `players.length` 永远等于容量 —— 判断「够不够开局」
 * 必须数真实占座，不能用数组长度（曾导致「提前开始」按钮恒可点）。
 */
export function virtualCityOccupiedSeats(
  players: VirtualCityPlayer[] | null | undefined,
): number {
  return (players ?? []).filter((p) => !!p && (!!p.account || !!p.nickname)).length;
}

// ── 其余 S→C 帧载荷（协议 §2）────────────────────────────────────────

/** game.joined。 */
export interface VirtualCityJoinedFrame {
  my_seat: number;
  month: number;
  phase: VirtualCityPhase;
}

/** game.started。 */
export interface VirtualCityStartedFrame {
  month: number;
  age: number;
  start_age: number;
}

export type VirtualCityEventType =
  | 'action' | 'move' | 'settle' | 'market' | 'life' | 'chat' | 'error'
  // §20260921 城市背景层 — 居民之声事件（走既有事件流 UI，无需新组件）。
  | 'city_voice'
  // 档案锚定设计 §5 — 档案锚定终态事件（hydrating 中间态不发，走 Snapshot 轮询）。
  | 'city_profiles';

/** game.event。 */
export interface VirtualCityEventFrame {
  room_id?: string;
  month: number;
  seat?: number;
  type: VirtualCityEventType | string;
  text: string;
  data?: unknown;
  /** city_profiles 事件的进度载荷（档案锚定设计 §5：{profiles:{status,done,total,…}}）。 */
  profiles?: VirtualCityCityProfileProgress;
  /** 前端入队时补的到达时间戳（store.pushEvent；批次 23 语音气泡过期判定用）。
   *  服务端帧本身不带此字段，勿用于跨端语义。 */
  ts?: number;
}

export interface VirtualCityMonthSummary {
  seat: number;
  cash_delta: number;
  net_worth: number;
  fi_index: number;
  note: string;
}

/** game.month（BroadcastRoom 全房同帧）。 */
export interface VirtualCityMonthFrame {
  room_id?: string;
  month: number;
  age: number;
  summaries: VirtualCityMonthSummary[];
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
  events: VirtualCityRecentEvent[];
}

export type VirtualCityEndingId =
  | 'winner' | 'affluent' | 'ordinary' | 'indebted' | 'bankrupt' | 'lonely_rich'
  | 'accident_death'; // P1-4 意外身故（HandleDeath，§5.3）

export interface VirtualCityScore {
  seat: number;
  fi_score: number;
  life_score: number;
  social_score: number;
  total: number;
  ending: string;
}

/** game.over。 */
export interface VirtualCityOverFrame {
  room_id?: string;
  scores: VirtualCityScore[];
  /** 人生报告（按座位索引；P0 简化为每个座位一份独立报告）。 */
  reports: Record<number, string>;
}

/** game.error。 */
export interface VirtualCityErrorFrame {
  code: number;
  message: string;
}

// ── C→S 动作（协议 §4 动作语义表；人类按钮 = 14 种，check_state/speak 仅 Agent）──

export type VirtualCityActionType =
  | 'buy_asset' | 'sell_asset' | 'buy_house' | 'take_loan' | 'repay_loan'
  | 'start_side_business' | 'stop_side_business' | 'set_side_price' | 'study' | 'socialize'
  | 'rest' | 'work_overtime' | 'move_district' | 'consume' | 'donate'
  | 'submit_month'
  | 'early_repay'
  | 'set_consumption'
  | 'buy_insurance' | 'cancel_insurance';

export type VirtualCityAction =
  | { type: 'buy_asset'; asset: 'stock_index' | 'bond' | 'gold'; amount_cny: number }
  | { type: 'sell_asset'; asset: string; units: number }
  | { type: 'buy_house'; district: VirtualCityDistrictId; downpay_ratio: number }
  | { type: 'take_loan'; kind: 'consumer' | 'credit' | 'business'; amount_cny: number }
  | { type: 'repay_loan'; loan_id: string; amount_cny: number }
  | { type: 'start_side_business'; kind: 'delivery' | 'content' | 'tutoring' | 'freelance'; tier?: VirtualCitySidePriceTier }
  | { type: 'stop_side_business' }
  | { type: 'set_side_price'; tier: VirtualCitySidePriceTier }
  | { type: 'study' }
  | { type: 'socialize' }
  | { type: 'rest' }
  | { type: 'work_overtime' }
  | { type: 'move_district'; district: VirtualCityDistrictId }
  | { type: 'consume'; amount_cny: number; reason?: string }
  | { type: 'donate'; amount_cny: number }
  | { type: 'submit_month' }
  | { type: 'early_repay'; loan_id: string; amount_cny: number }
  | { type: 'set_consumption'; level: number }
  | { type: 'buy_insurance'; kind: VirtualCityInsuranceKind }
  | { type: 'cancel_insurance'; kind: VirtualCityInsuranceKind };

// ── P2 交易系统：交易类动作（走 game.virtualCity_xxx 帧）──────────────────────

/** 挂牌出售 / 收购 / 信息 / 借贷。 */
export type VirtualCityTradeAction =
  | { type: 'listing_create'; listing_type: VirtualCityListingType; payload: VirtualCityListingPayload; ask_cny: number }
  | { type: 'listing_cancel'; listing_id: string }
  | { type: 'listing_view'; listing_type?: VirtualCityListingType }
  | { type: 'negotiate_start'; listing_id: string; offer_cny: number }
  | { type: 'negotiate_respond'; neg_id: string; action: VirtualCityNegotiateAction; offer_cny: number; comment?: string }
  | { type: 'loan_accept'; listing_id: string }
  | { type: 'loan_repay'; loan_id: string; amount_cny?: number }
  | { type: 'add_guarantor'; loan_id: string }
  | { type: 'auction_bid'; auction_id: string; amount_cny: number }
  | { type: 'sell_info'; category: VirtualCityInfoCategory; title: string; detail: string; min_bid_cny: number }
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
  [WEALTH_TRADE_ERR.ListingInvalid]: 'virtualCity.error.listingInvalid' as TKey,
  [WEALTH_TRADE_ERR.ListingExpired]: 'virtualCity.error.listingExpired' as TKey,
  [WEALTH_TRADE_ERR.NegotiateNotFound]: 'virtualCity.error.negotiateNotFound' as TKey,
  [WEALTH_TRADE_ERR.NotYourTurn]: 'virtualCity.error.notYourTurn' as TKey,
  [WEALTH_TRADE_ERR.LoanRateInvalid]: 'virtualCity.error.loanRate' as TKey,
  [WEALTH_TRADE_ERR.LoanNoCredit]: 'virtualCity.error.loanCredit' as TKey,
  [WEALTH_TRADE_ERR.GuarantorConflict]: 'virtualCity.error.guarantorConflict' as TKey,
  [WEALTH_TRADE_ERR.AuctionEnded]: 'virtualCity.error.auctionEnded' as TKey,
  [WEALTH_TRADE_ERR.BidTooLow]: 'virtualCity.error.bidTooLow' as TKey,
  [WEALTH_TRADE_ERR.NoPrivilege]: 'virtualCity.error.noPrivilege' as TKey,
  [WEALTH_TRADE_ERR.ListingFull]: 'virtualCity.error.listingFull' as TKey,
  [WEALTH_TRADE_ERR.SelfTrade]: 'virtualCity.error.selfTrade' as TKey,
};

/** P1-4 商业保险错误码（errcode.go §9：35037–35041；现金不足复用 35007）。 */
export const WEALTH_INSURANCE_ERR = {
  KindInvalid: 35037,
  Exists: 35038,
  NotFound: 35039,
  AgeGate: 35040,
  Disabled: 35041,
} as const;

/** P1-4 商业保险错误码 → i18n key 映射（InsurancePanel mapError 用）。 */
export const INSURANCE_ERR_I18N: Record<number, TKey> = {
  [WEALTH_INSURANCE_ERR.KindInvalid]: 'virtualCity.error.insuranceKindInvalid' as TKey,
  [WEALTH_INSURANCE_ERR.Exists]: 'virtualCity.error.insuranceExists' as TKey,
  [WEALTH_INSURANCE_ERR.NotFound]: 'virtualCity.error.insuranceNotFound' as TKey,
  [WEALTH_INSURANCE_ERR.AgeGate]: 'virtualCity.error.insuranceAgeGate' as TKey,
  [WEALTH_INSURANCE_ERR.Disabled]: 'virtualCity.error.insuranceDisabled' as TKey,
};

/** 批次 20 文档 3 B3：股票微观结构错误码（errcode.go 35043/35044）。 */
export const WEALTH_MICRO_ERR = {
  /** 熔断期股票交易暂停。 */
  CircuitBreak: 35043,
  /** 当月买入份额 T+1 冻结不可卖。 */
  StockT1Locked: 35044,
} as const;

/** POST /api/games/virtual-city/rooms 的 virtualCity 段（协议 §6）。 */
export interface VirtualCityRoomOptions {
  /** 1 游戏月时长 ms，3000–30000，缺省 8000。 */
  month_ms?: number;
  /** 可选随机种子（测试确定性复现）。 */
  seed?: number;
  /** 2026-09-25 §LLM线路池配额 — 本房 Agent 并发线路数 [1,64]；
   *  0/缺省 = 不指定（后端按池总量运行）。 */
  llm_lines?: number;
}

// ── 静态表 ────────────────────────────────────────────────────────────

export interface VirtualCityDistrictDef {
  id: VirtualCityDistrictId;
  /** 中文名（DistrictDefs 权威值；i18n 展示走 virtualCity.district.<id>）。 */
  nameZh: string;
  /** 主色（后端 DistrictDefs）。 */
  color: string;
  /** 120×120 地图平面坐标（前端 DistrictBlock / 小地图共用）。 */
  x: number;
  z: number;
  /** 房价 beta。 */
  houseBeta: number;
  /** 基准房价（万元/套）。 */
  basePriceWan: number;
}

/** 32 城区静态表（后端架构文档 §4 DistrictDefs，顺序即数组下标）。
 *  前 8 区为 P0 原有城区（id/顺序不可修改）；中 8 区为 v2.12 阶段 2 扩展
 *  （地图 40×40 → 80×80，位置均在 ±30 单位内）；后 16 区为批次 20 城市扩张
 *  （地图 80×80 → 120×120，位置均在 |x|≤50、|z|≤48 内，契约=文档 1 §2）。 */
export const VIRTUAL_CITY_DISTRICTS: VirtualCityDistrictDef[] = [
  { id: 'finance',     nameZh: '金融CBD', color: '#1d4ed8', x: 0,   z: 0,   houseBeta: 1.3,  basePriceWan: 800 },
  { id: 'tech',        nameZh: '科技园',  color: '#0e7490', x: -10, z: 4,   houseBeta: 1.15, basePriceWan: 500 },
  { id: 'industry',    nameZh: '工业区',  color: '#57534e', x: -12, z: -8,  houseBeta: 0.85, basePriceWan: 200 },
  { id: 'oldtown',     nameZh: '老城区',  color: '#92400e', x: 2,   z: -12, houseBeta: 0.8,  basePriceWan: 180 },
  { id: 'commerce',    nameZh: '商业中心', color: '#b91c1c', x: 10,  z: -2,  houseBeta: 1.1,  basePriceWan: 400 },
  { id: 'residential', nameZh: '居住区',  color: '#15803d', x: 0,   z: 12,  houseBeta: 1.0,  basePriceWan: 300 },
  { id: 'suburb',      nameZh: '郊区',    color: '#65a30d', x: -14, z: 14,  houseBeta: 0.7,  basePriceWan: 120 },
  { id: 'riverside',   nameZh: '滨河新区', color: '#7c3aed', x: 14,  z: 10,  houseBeta: 1.25, basePriceWan: 450 },
  // ── v2.12 阶段 2 扩展城区（80×80 地图外圈；顺序与后端 DistrictDefs 一致）──
  { id: 'logistics_port',    nameZh: '物流港',   color: '#475569', x: -22, z: -4,  houseBeta: 0.9,  basePriceWan: 250 },
  { id: 'hightech_park',     nameZh: '高新园区', color: '#0891b2', x: -22, z: 12,  houseBeta: 1.2,  basePriceWan: 600 },
  { id: 'edu_district',      nameZh: '教育园区', color: '#7c3aed', x: -8,  z: 22,  houseBeta: 0.95, basePriceWan: 350 },
  { id: 'medical_city',      nameZh: '医疗城',   color: '#db2777', x: 8,   z: 22,  houseBeta: 1.05, basePriceWan: 450 },
  { id: 'industrial_park',   nameZh: '产业基地', color: '#78716c', x: -24, z: -20, houseBeta: 0.7,  basePriceWan: 150 },
  { id: 'central_park',      nameZh: '中央公园', color: '#16a34a', x: 0,   z: -22, houseBeta: 1.0,  basePriceWan: 500 },
  { id: 'transport_hub',     nameZh: '交通枢纽', color: '#ea580c', x: 22,  z: -14, houseBeta: 0.85, basePriceWan: 280 },
  { id: 'cultural_creative', nameZh: '文创区',   color: '#e11d48', x: 24,  z: 8,   houseBeta: 1.1,  basePriceWan: 380 },
  // ── 批次 20 城市扩张新增 16 区（下标 16–31；id/顺序/坐标/颜色逐字 = 文档 1 §2 表）──
  { id: 'fin_sub_center',    nameZh: '金融副中心', color: '#1e40af', x: 24,  z: 30,  houseBeta: 1.30, basePriceWan: 650 },
  { id: 'software_park',     nameZh: '软件园',   color: '#0d9488', x: 38,  z: 14,  houseBeta: 1.20, basePriceWan: 520 },
  { id: 'airport_town',      nameZh: '空港小镇', color: '#0369a1', x: 44,  z: -20, houseBeta: 1.05, basePriceWan: 300 },
  { id: 'air_logistics',     nameZh: '航空物流园', color: '#334155', x: 36,  z: -38, houseBeta: 0.95, basePriceWan: 260 },
  { id: 'auto_city',         nameZh: '汽车城',   color: '#a16207', x: 8,   z: -40, houseBeta: 1.00, basePriceWan: 300 },
  { id: 'mountain_resort',   nameZh: '山居民宿区', color: '#4d7c0f', x: -8,  z: -44, houseBeta: 0.90, basePriceWan: 180 },
  { id: 'chem_park',         nameZh: '化工园区', color: '#52525b', x: -20, z: -44, houseBeta: 0.70, basePriceWan: 130 },
  { id: 'agri_park',         nameZh: '现代农业园', color: '#ca8a04', x: -40, z: -36, houseBeta: 0.80, basePriceWan: 160 },
  { id: 'health_town',       nameZh: '康养小镇', color: '#fb7185', x: -44, z: -10, houseBeta: 0.80, basePriceWan: 200 },
  { id: 'steel_town',        nameZh: '特钢镇',   color: '#44403c', x: -46, z: 2,   houseBeta: 0.75, basePriceWan: 150 },
  { id: 'old_city_culture',  nameZh: '古城文化区', color: '#9a3412', x: -44, z: 22,  houseBeta: 0.85, basePriceWan: 240 },
  { id: 'university_town',   nameZh: '大学城',   color: '#6366f1', x: -32, z: 34,  houseBeta: 0.95, basePriceWan: 300 },
  { id: 'wetland_park',      nameZh: '湿地公园', color: '#14b8a6', x: -12, z: 40,  houseBeta: 0.90, basePriceWan: 280 },
  { id: 'sports_new_city',   nameZh: '体育新城', color: '#facc15', x: 10,  z: 40,  houseBeta: 1.05, basePriceWan: 340 },
  { id: 'bay_new_town',      nameZh: '湾区新城', color: '#7e22ce', x: 40,  z: 28,  houseBeta: 1.25, basePriceWan: 580 },
  { id: 'highspeed_rail_town', nameZh: '高铁新城', color: '#c2410c', x: 46, z: 2,   houseBeta: 1.15, basePriceWan: 380 },
];

export const WEALTH_DISTRICT_IDS: VirtualCityDistrictId[] =
  VIRTUAL_CITY_DISTRICTS.map((d) => d.id);

/** 按 id 查城区定义。 */
export function virtualCityDistrict(id: string): VirtualCityDistrictDef | undefined {
  return VIRTUAL_CITY_DISTRICTS.find((d) => d.id === id);
}

/** 城区中心（120×120 世界坐标）；未知 id 回落原点。 */
export function districtCenter(id: string): { x: number; z: number } {
  const d = virtualCityDistrict(id);
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

export function professionColor(id: string): string {
  return PROFESSION_COLORS[id] ?? WEALTH_DEFAULT_PROFESSION_COLOR;
}

export function professionEmoji(id: string): string {
  return PROFESSION_EMOJI[id] ?? WEALTH_DEFAULT_PROFESSION_EMOJI;
}

// ── 动作元数据（ActionPanel 按钮渲染依据；语义唯一事实来源 = 协议 §4）──

/** 参数化动作的表单形态。 */
export type VirtualCityActionFormKind =
  | 'none'                       // 无参确认（study / socialize / rest / work_overtime / stop_side_business / submit_month）
  | 'buy_asset' | 'sell_asset' | 'buy_house' | 'take_loan' | 'repay_loan'
  | 'side_business' | 'move_district' | 'amount' | 'amount_reason'
  | 'consumption_level';         // 消费档位 4 按钮组（ActionPanel 专属渲染，不走通用弹窗）

export interface VirtualCityActionMeta {
  type: VirtualCityActionType;
  icon: string;
  /** i18n key：`virtualCity.action.<key>`。 */
  i18nKey: string;
  form: VirtualCityActionFormKind;
  /** 简述（tooltip / 规则页），人读。 */
  hint: string;
}

/** 14 个人类按钮动作 + P1 消费档位（顺序 = 产品设计 §7.1 动作条；set_consumption
 *  由 ActionPanel 的档位 4 按钮组专属渲染，通用按钮循环须跳过 form==='consumption_level'）。 */
export const WEALTH_ACTIONS: VirtualCityActionMeta[] = [
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

export interface VirtualCityConsumptionLevelMeta {
  /** 0-3。 */
  level: number;
  /** i18n key：`virtualCity.consumption.level.<i18nKey>`。 */
  i18nKey: 'frugal' | 'normal' | 'refined' | 'luxury';
  /** 生活支出乘数（与后端 consumptionLevelMult 同值）。 */
  multiplier: number;
  /** 精力效果（与后端 consumptionLevelEnergy 同值）。 */
  energy: number;
}

/** 消费档位 4 档（0 节俭 / 1 标准 / 2 精致 / 3 奢侈）。 */
export const WEALTH_CONSUMPTION_LEVELS: VirtualCityConsumptionLevelMeta[] = [
  { level: 0, i18nKey: 'frugal',  multiplier: 0.6, energy: -1 },
  { level: 1, i18nKey: 'normal',  multiplier: 1.0, energy: 0 },
  { level: 2, i18nKey: 'refined', multiplier: 1.5, energy: 1 },
  { level: 3, i18nKey: 'luxury',  multiplier: 2.2, energy: 2 },
];

// ── CPI 八大类静态表（P1 真实经济循环引擎 §2.1）─────────────────────────

export interface VirtualCityGoodsMeta {
  id: string;
  /** 中文名（统计口径；i18n 展示走 virtualCity.goods.<id>）。 */
  nameZh: string;
  /** PNG 缺失时的 emoji 兜底（goodsIcon(id) 返回 '' 时用）。 */
  emoji: string;
  /** 篮子权重（服务端权威下发，此表仅兜底展示）。 */
  weight: number;
}

/** 八大类顺序 = 权重表序（goodsIcon 图标 / emoji 兜底 / tooltip 中文名）。 */
export const WEALTH_GOODS_META: VirtualCityGoodsMeta[] = [
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
export function virtualCityGoodsMeta(id: string): VirtualCityGoodsMeta | undefined {
  return WEALTH_GOODS_META.find((g) => g.id === id);
}

export const WEALTH_SUBMIT_MONTH: VirtualCityActionMeta = {
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
export function minskyTierColor(tier: VirtualCityMinskyTier | undefined | null): string {
  switch (tier) {
    case 'hedge': return '#4caf50';       // 对冲 — 绿（白字 ≈ 5.4:1）
    case 'speculative': return '#ff9800'; // 投机 — 橙（白字 ≈ 4.6:1 on #10151d）
    case 'ponzi': return '#f44336';       // 庞氏 — 红（白字 ≈ 5.7:1）
    default: return '#9ca3af';
  }
}
