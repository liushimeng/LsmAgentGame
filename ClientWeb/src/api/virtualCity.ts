/**
 * 虚拟城市 REST API 封装。
 *
 * 对齐 lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-WS与HTTP协议契约-v1.md §6。
 * 房间 CRUD（list / create / join / leave / spectate）复用 services/auth.service.ts
 * 的 roomService（参数化路由），本文件只承载 virtualCity 专属端点。
 */

import { http, ApiError, isSessionExpiredError, getAuthToken } from '@/services/http';
import type { VirtualCityCityProfileProgress, VirtualCityCityResidentProfile, VirtualCitySurvey } from '@/types/virtualCity';
import type { MissionRecord, MissionStats, RealtimeBoardRefreshResult, RealtimeFeed } from '@/types/virtualCityRealtime';
import type { VirtualCitySystemWireMessage } from '@/types/virtualCitySystem';
import type { VirtualCityScenarioCatalogResponse, VirtualCityScenarioCatalogItem } from '@/types/virtualCitySystem';

// 2026-09-22 §CityHuman全民驱动 — GET /api/games/virtual_city/professions 已随
// 职业卡精选层退役（03 号契约）：原拉取接口及其三个类型一并删除。
// 档案唯一源 = 人物卡知识库（城市居民档案两接口 fetchCityResidents /
// fetchCityResident 保留）。

// ── P1 社会调研系统（lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-社会调研系统-v1.md §3.1）──

/**
 * POST /api/games/virtual_city/rooms/:id/survey（需登录）。
 *
 * 任意登录用户可发起（在座 / 观战 / 房外均可——调研是「向 AI 提问」）。
 * 服务端限流（房间级）：同时仅 1 个 open(35017) / 每月 1 个(35018) /
 * 累计 ≤20 个(35018)；options 2-6 个且非空(35016)；仅 playing 可发起(35002)。
 * §7.1：调用方 catch 后表单内联红条展示，勿吞进 console。
 */
export function launchVirtualCitySurvey(
  roomId: string,
  question: string,
  options: string[],
): Promise<{ survey: VirtualCitySurvey }> {
  return http<{ survey: VirtualCitySurvey }>(
    `/api/games/virtual_city/rooms/${roomId}/survey`,
    {
      method: 'POST',
      body: JSON.stringify({ question, options }),
    },
  );
}

/**
 * GET /api/games/virtual_city/rooms/:id/surveys（需登录）。
 *
 * 返回全部历史调研快照（≤20，含 open 与 closed）。SurveyPanel 挂载时拉取，
 * 后续增量由 game.survey_result 帧（useVirtualCity）按 id 去重覆盖。
 */
export function fetchVirtualCitySurveys(roomId: string): Promise<{ surveys: VirtualCitySurvey[] }> {
  return http<{ surveys: VirtualCitySurvey[] }>(
    `/api/games/virtual_city/rooms/${roomId}/surveys`,
  );
}

// ── 城市居民人物卡档案（档案锚定设计 §7 REST 契约）──────────────────────

/** GET /city/residents 的 data 形状：锚定进度 + 一页档案 + 命中总数。 */
export interface VirtualCityCityResidentsPage {
  progress: VirtualCityCityProfileProgress;
  residents: VirtualCityCityResidentProfile[];
  /** q 过滤后的命中总数（分页用）。 */
  matched: number;
}

/**
 * GET /api/games/virtual_city/rooms/:id/city/residents（需登录；档案是公开合成人格）。
 *
 * query：offset（默认 0）、limit（默认 50，后端 clamp 1..200）、
 * q（可选，姓名/职业/卡号包含匹配）。房间不存在 → 35001；未建城（City=nil）→
 * data:{progress:{status:'idle'}, residents:[]}（不算错误，§7）。
 * §7.1：调用方 catch 后就地展示（抽屉内联红条），勿吞进 console。
 */
export function fetchCityResidents(
  roomId: string,
  offset = 0,
  limit = 50,
  q?: string,
): Promise<VirtualCityCityResidentsPage> {
  const params = new URLSearchParams();
  params.set('offset', String(Math.max(0, Math.floor(offset) || 0)));
  params.set('limit', String(Math.min(200, Math.max(1, Math.floor(limit) || 50))));
  if (q && q.trim()) params.set('q', q.trim());
  return http<VirtualCityCityResidentsPage>(
    `/api/games/virtual_city/rooms/${roomId}/city/residents?${params.toString()}`,
  );
}

/**
 * GET /api/games/virtual_city/rooms/:id/city/residents/:cardId（需登录）。
 *
 * 命中 → ResidentProfile 全字段（服务端包一层 data.resident，此处解包）；
 * 查不到（ErrVirtualCityResidentNotFound：后端实测 35042 / 设计文档 §7 曾写 35013，
 * 两者都兼容）→ 返回 null（§8.2 契约），其余错误照常抛出。
 */
export async function fetchCityResident(
  roomId: string,
  cardId: string,
): Promise<VirtualCityCityResidentProfile | null> {
  try {
    const data = await http<{ resident?: VirtualCityCityResidentProfile }>(
      `/api/games/virtual_city/rooms/${roomId}/city/residents/${encodeURIComponent(cardId)}`,
    );
    return data?.resident ?? null;
  } catch (e) {
    if (e instanceof ApiError && (e.code === 35042 || e.code === 35013)) return null;
    if (isSessionExpiredError(e)) return null; // 会话过期已由全局弹层接管
    throw e;
  }
}

// ── 批次 65 · City-Chrome 实时数据通道（lag_docs/虚拟城市/已实现/
//    65-CityChrome实时数据通道与实时看板/01-方案设计.md §3.5）──────────────

/**
 * GET /api/virtual_city/realtime_board（需登录）— 实时看板快照。
 *
 * 返回 {code:0, data:{feed: Feed, missions: MissionRecord[], stats: MissionStats}}；
 * late-joiner 首屏靠它补齐 game.realtime_feed 帧间隙。数据是全局的（与房间无关）。
 * 批次77：missions 为采集任务遥测（新→旧 ≤20 条，无记录为 []）——**仅走本端点**，
 * WS game.realtime_feed 帧不携带，快照/手动刷新回读更新。
 * 批次78：stats 为采集健康度聚合（MissionStats，无记录时全零）——同样**仅走本端点**。
 * §7.1：首屏拉取失败不算操作错误 —— 调用方静默兜底置空即可。
 */
export function fetchRealtimeBoard(): Promise<{
  feed: RealtimeFeed;
  missions: MissionRecord[];
  stats: MissionStats;
}> {
  return http<{ feed: RealtimeFeed; missions: MissionRecord[]; stats: MissionStats }>(
    '/api/virtual_city/realtime_board',
  );
}

/**
 * POST /api/virtual_city/realtime_board/refresh（需登录）— 人工触发一次采集。
 *
 * body {category?}（缺省 = 调度器当前轮转类目）；入任务队列立即调度一次 mission。
 * 限流：同 category 60s 内重复请求返回 429 语义错误码（抛 ApiError，§7.1 面板内联红条）。
 * 返回 {code:0, data:{accepted:true, queued_category}}。
 */
export function refreshRealtimeBoard(
  category?: string,
): Promise<RealtimeBoardRefreshResult> {
  return http<RealtimeBoardRefreshResult>('/api/virtual_city/realtime_board/refresh', {
    method: 'POST',
    body: JSON.stringify(category ? { category } : {}),
  });
}

// ── 批次 69 · 虚拟城市「实时看板」统一重构 ─────────────────────────────────
//
// 契约（backend-dev 并行实现，字段/路径逐字对齐批次 69 任务书）：
//   GET /api/virtual_city/dashboard（需登录）
//   → {code:0, message:"ok", data:{dashboard:{
//        schema_version, generated_at, dimensions[], data_sources[], realtime}}}
//
// ⚠️ **通道离线时 code = 35053，但 data.dashboard 仍完整**（dimensions / data_sources
//    齐全，realtime.channel.status = "offline"）。而 http<T>() 在 code ≠ 0 时会抛
//    ApiError 并丢弃 body —— 直接用 http() 会把完整目录一起丢掉。
//    故本函数：① 先走 http()（保留 401 会话过期 / 统一错误语义）；
//               ② 仅在 code = 35053 时用裸 fetch 兜底取回完整 data.dashboard。
//    其余错误码照常抛出，由调用方按 §7.1 展示。

/** 看板维度目录条目（dimensions[] 元素）。 */
export interface VirtualCityDashboardDimension {
  /** 维度 id（如 "city-overview" / "market-trade"），与前端静态目录同名。 */
  id: string;
  /** 所属分组（"city" / "market" / "external"）。 */
  group: string;
  /** i18n 键（前端按 id 匹配后覆盖静态目录的同名项）。 */
  title_key: string;
  /** 数据来源（"game_state" / "realtime_feed" / "catalog" …）。 */
  source: string;
  /** 作用域（"room" / "global"）。 */
  scope: string;
  /** 单位（"mixed" / "cny" / "pct" / "text"）。 */
  unit: string;
  /** 刷新方式（"push" / "pull" / "static"）。 */
  refresh: string;
}

/** 数据源清单条目（data_sources[] 元素）。 */
export interface VirtualCityDashboardDataSource {
  /** 分类（如 "综合财经"）。 */
  category: string;
  /** 数据源名称。 */
  name: string;
  /** 用途说明。 */
  purpose: string;
  /** 官网链接。 */
  url: string;
}

/** 看板快照（data.dashboard 形状）。 */
export interface VirtualCityDashboardSnapshot {
  /** 目录结构版本（前端展示为「数据版本 v{n}」）。 */
  schema_version: number;
  /** 生成时间（Unix 秒）。 */
  generated_at: number;
  /** 维度目录（前端静态目录为权威，本项只做校验/补充）。 */
  dimensions: VirtualCityDashboardDimension[];
  /** 数据源清单（按 category 分组渲染）。 */
  data_sources: VirtualCityDashboardDataSource[];
  /** 实时通道快照（与 GET /api/virtual_city/realtime_board 的 feed 同构）。 */
  realtime: RealtimeFeed | null;
}

/** 通道离线错误码（后端在通道离线时返回，但 data.dashboard 仍完整）。 */
export const VIRTUAL_CITY_DASHBOARD_CHANNEL_OFFLINE = 35053;

/**
 * 裸 fetch 取回 data.dashboard（绕过 http() 的 code≠0 即抛错语义）。
 *
 * 仅用于 35053 兜底路径：通道离线但目录完整，必须拿得到。
 * 解析不出 dashboard 时返回 null（调用方按「目录不可用」处理，不抛错）。
 */
async function fetchDashboardTolerant(): Promise<VirtualCityDashboardSnapshot | null> {
  try {
    const headers: Record<string, string> = { Accept: 'application/json' };
    const token = getAuthToken();
    if (token) headers.Authorization = `Bearer ${token}`;
    // 10s 超时兜底：兜底请求本身挂起时不能让整个看板快照 promise 悬空。
    const ac = new AbortController();
    const timer = setTimeout(() => ac.abort(), 10_000);
    try {
      const res = await fetch('/api/virtual_city/dashboard', { headers, signal: ac.signal });
      const body = (await res.json().catch(() => null)) as
        | { data?: { dashboard?: VirtualCityDashboardSnapshot } }
        | null;
      return body?.data?.dashboard ?? null;
    } finally {
      clearTimeout(timer);
    }
  } catch {
    return null;
  }
}

/**
 * GET /api/virtual_city/dashboard（需登录）— 实时看板快照。
 *
 * 返回 {dashboard:{schema_version, dimensions, data_sources, realtime}}。
 * §7.1：调用方（VirtualCityDashboard）catch 后面板内联提示 + 静态目录兜底，
 * 绝不吞进 console。
 */
export async function fetchVirtualCityDashboard(): Promise<{
  dashboard: VirtualCityDashboardSnapshot;
}> {
  try {
    return await http<{ dashboard: VirtualCityDashboardSnapshot }>(
      '/api/virtual_city/dashboard',
    );
  } catch (e) {
    if (e instanceof ApiError && e.code === VIRTUAL_CITY_DASHBOARD_CHANNEL_OFFLINE) {
      // 通道离线：目录仍完整 → 裸 fetch 兜底取回；取不到则按「目录不可用」降级。
      const dash = await fetchDashboardTolerant();
      if (dash) return { dashboard: dash };
      return {
        dashboard: {
          schema_version: 1,
          generated_at: 0,
          dimensions: [],
          data_sources: [],
          realtime: null,
        },
      };
    }
    throw e;
  }
}

// ── 批次 68 · 预测运行：基线快照 · 指标对比 · 预测报告 ─────────────────────
// 契约（backend-dev 并行实现，字段/路径逐字对齐 tmpPlan/
// 虚拟城市-预测运行报告_20261021_113136.md §4/§5）：
//   GET /api/games/virtual_city/rooms/:id/prediction             // 最新 run
//   GET /api/games/virtual_city/rooms/:id/prediction?run_id=PR2  // 指定 run
//   → {code:0, data:{runs:[...], report: PredictionReport | null}}
// 权限：任意登录用户（观察者/条件触发者同口径）。无 run 不报错——runs:[] 且
// report:null，前端展示「暂无预测运行」。run_id 不在该房 → 35058。
// §7.1：调用方（系统对话报告卡）catch 后面板内联错误 + reportGlobalError 上报。
// 类型字段名与 §4 JSON snake_case 逐字一致（免映射层），注释标注语义。
// ⚠️ 路径一律用下划线 `virtual_city`（后端 router 的 `r.Group("/api/games/virtual_city")`；
//    未登录探针：下划线 401、连字符 404），勿改回连字符。
// 批次81：场景推演台（ScenarioPanel）的目录/注入/历史 API 与配套类型
// （Spec / ScenarioRecord / Effect / ApplyRequest / ApplyResult）已删除；
// meta.injections[] 元素形状并入本段（VirtualCityPredictionInjection）。

/** report.meta.injections[] 元素（ScenarioAppliedRecord 子集：
 *  id/kind/magnitude/applied_month/note；批次81 起随场景推演台 API 一并
 *  收编进预测报告契约）。 */
export interface VirtualCityPredictionInjection {
  id: string;
  kind: string;
  magnitude: number;
  applied_month: number;
  note: string;
  /**
   * 注入时间戳。后端为 unix 毫秒（int64）；类型放宽为 number | string，
   * 兼容历史数据/其它序列化形态（展示层两种都解析）。
   */
  at?: number | string;
}

/** report.meta（复现层级 R1 = 观测对照：基线为注入时点 T0 观测快照）。 */
export interface VirtualCityPredictionMeta {
  /** run id，如 "PR1"。 */
  run_id: string;
  /** 复现层级；本批次唯一取值 "R1"（R0 同种子孪生未实现）。 */
  tier: string;
  /** 基线快照 hash id（fnv/基线点字段序列化）。 */
  baseline_snapshot_id: string;
  seed: number;
  /** 注入时点（T0）月份。 */
  start_month: number;
  /** 已观测月数（trajectory 点数 − 1，≤36）。 */
  months_observed: number;
  /** 生成时间戳（unix 毫秒）。 */
  generated_at: number;
  /** ScenarioAppliedRecord 子集：id/kind/magnitude/applied_month/note。 */
  injections: VirtualCityPredictionInjection[];
}

/** report.scenarios[] 元素（并入本 run 的场景详情）。 */
export interface VirtualCityPredictionScenario {
  kind: string;
  name: string;
  group: string;
  magnitude: number;
  duration_months: number;
  target: string;
  note: string;
  applied_month: number;
  /** 衰减形态描述（如 "线性衰减(remaining/total)"）。 */
  decay: string;
}

/** report.first_month_impact[] 元素（T0+1 点 vs 基线；T0 即基线 ⇒ delta 0）。 */
export interface VirtualCityPredictionImpact {
  metric: string;
  baseline: number;
  value: number;
  /** 相对基线偏离百分比（%，24.0 = +24%）。 */
  delta_pct: number;
  /** 指标单位（如 ratio / index / currency）。 */
  unit: string;
  /** 服务端生成的展示文本。 */
  text: string;
}

/** report.trajectory[] 元素（逐月指标点，T0 也记一点，≤36 点滚动）。 */
export interface VirtualCityPredictionTrajectoryPoint {
  month: number;
  /** metric → 当月取值。 */
  values: Record<string, number>;
  /** metric → T0 基线值（平直基线，恒定）。 */
  baseline: Record<string, number>;
}

/** report.peak_recovery[] 元素（偏离回落基线 ±5% 带内视为恢复）。 */
export interface VirtualCityPredictionPeakRecovery {
  metric: string;
  /** 最大正/负偏离峰值（%）。 */
  peak_dev_pct: number;
  peak_month: number;
  /** 反向谷值偏离（%）。 */
  trough_dev_pct: number;
  trough_month: number;
  /** 偏离回落 ±5% 带且其后保持的首月；未恢复 = null。 */
  recovery_month: number | null;
  recovered: boolean;
}

/** report.transmission（传导链贡献度；启发式估计非计量因果分解）。 */
export interface VirtualCityPredictionTransmission {
  chains: {
    /** 链路名（如 "⑥资产→③就业→④消费"）。 */
    chain: string;
    /** 归一化贡献度 [0,1]。 */
    contribution: number;
    dominant: boolean;
    evidence: string;
  }[];
  note: string;
}

/** report.distribution（财富/收入分配结构变化）。 */
export interface VirtualCityPredictionDistribution {
  gini_wealth_delta: number;
  top10_delta_pct: number;
  bottom50_delta_pct: number;
  /** 金字塔层级份额位移。 */
  pyramid_shift: number[];
  text: string;
}

/** report.extrapolation[] 元素（3/6/12 月外推点估计 + 置信带）。 */
export interface VirtualCityPredictionExtrapolation {
  horizon_months: number;
  metric: string;
  point: number;
  lo: number;
  hi: number;
}

/** report.resilience（韧性画像；仅 group ∈ {自然灾害, 未知灾难} 时 applicable）。 */
export interface VirtualCityPredictionResilience {
  applicable: boolean;
  /** 评分 ∈ [0,100]。 */
  recovery_score: number;
  adaptation_score: number;
  transform_score: number;
  basis: string[];
}

/** 预测报告全文（§4 契约；boundaries 首条为 R1 观测对照声明）。 */
export interface VirtualCityPredictionReport {
  meta: VirtualCityPredictionMeta;
  scenarios: VirtualCityPredictionScenario[];
  first_month_impact: VirtualCityPredictionImpact[];
  trajectory: VirtualCityPredictionTrajectoryPoint[];
  peak_recovery: VirtualCityPredictionPeakRecovery[];
  transmission: VirtualCityPredictionTransmission;
  distribution: VirtualCityPredictionDistribution;
  extrapolation: VirtualCityPredictionExtrapolation[];
  resilience: VirtualCityPredictionResilience;
  boundaries: string[];
}

/** GET prediction 的 runs[] 元素（环形容量保留最近 3 个 run）。 */
export interface VirtualCityPredictionRunSummary {
  id: string;
  start_month: number;
  months_observed: number;
  tier: string;
  /** 并入本 run 的注入记录 id 列表。 */
  injections: string[];
}

/** GET prediction 的 data 形状（无 run → runs:[] 且 report:null，不算错误）。 */
export interface VirtualCityPredictionState {
  runs: VirtualCityPredictionRunSummary[];
  report: VirtualCityPredictionReport | null;
}

/**
 * GET /api/games/virtual_city/rooms/:id/prediction（需登录）— 预测运行报告。
 *
 * runId 缺省取该房最新 run；指定 run_id 不在该房 → 35058。
 * 返回 {runs, report}；无任何 run 时 runs:[] 且 report:null（前端引导文案）。
 */
export function getPredictionReport(
  roomId: string,
  runId?: string,
): Promise<VirtualCityPredictionState> {
  const params = runId ? `?run_id=${encodeURIComponent(runId)}` : '';
  return http<VirtualCityPredictionState>(
    `/api/games/virtual_city/rooms/${encodeURIComponent(roomId)}/prediction${params}`,
  );
}

// ── 批次 81 · City-System 系统对话历史（§7.3-6 断线恢复的 HTTP 兜底）──────

/** GET system_agent/history 的 data 形状（无会话 → messages:[]，不算错误）。 */
export interface VirtualCitySystemAgentHistory {
  /** wire 形状（llmtypes.Message JSON），消费前经 normalizeSystemHistory 归一化。 */
  messages: VirtualCitySystemWireMessage[];
}

/**
 * GET /api/games/virtual_city/rooms/:id/system_agent/history（需登录）——
 * 当前用户在此房的系统对话会话。WS 是主通道，本函数仅作 WS 2s 未回时的兜底。
 *
 * 错误体（房间不存在 35001 / 未接线等）不携带 messages，故无需 35053 式
 * 裸 fetch 保 body——http() 抛 ApiError 即可，由调用方按 §7.1 上报。
 */
export function fetchVirtualCitySystemAgentHistory(
  roomId: string,
): Promise<VirtualCitySystemAgentHistory> {
  return http<VirtualCitySystemAgentHistory>(
    `/api/games/virtual_city/rooms/${encodeURIComponent(roomId)}/system_agent/history`,
    { timeoutMs: 8000 },
  ).then((d) => ({ messages: Array.isArray(d?.messages) ? d.messages : [] }));
}

// ── 批次 84 · 场景目录浏览器（只读检索端点）────────────────────────────

/** 目录检索参数（全部可选；空 = 全目录）。 */
export interface VirtualCityScenarioCatalogQuery {
  /** 分组名（维度）；空 = 全部。 */
  group?: string;
  /** 关键词（kind / 名称 / 描述 子串匹配）；空 = 不过滤。 */
  q?: string;
  /** 返回条数上限（后端 clamp 1..200）。 */
  limit?: number;
}

/**
 * GET /api/games/virtual_city/scenario_catalog（需登录）—— 场景目录检索。
 *
 * 批次 84 §7.2（只读端点，零副作用）：返回
 * `{dimensions:[{group,count,samples}], items:[{kind,name,group,description,
 *   default_magnitude,params}], total}`。
 *
 * ⚠️ **双形状兼容**：既有后端端点（批次 21 起）返回的是
 * `data.scenarios: ScenarioSpec[]`（全量目录、忽略 query 参数）。批次 84 的
 * 检索形状（dimensions/items/total）与之并存期间，前端**两种都吃**：
 *   - 有 `dimensions` / `items` ⇒ 直接用（新契约，服务端已做检索 + 分页）；
 *   - 只有 `scenarios` ⇒ 前端按 group / q 本地过滤 + 截断（兼容路径）。
 * 两条路径都返回同一形状 ⇒ 组件层无分支。
 *
 * 说明：目录规模随批次 84 模板化派生可达 ≥1,100 条 ⇒ 前端**必须**按维度 /
 * 关键词分页检索，不得一次性全量拉取（limit 缺省 30）。
 */
export function fetchVirtualCityScenarioCatalog(
  query: VirtualCityScenarioCatalogQuery = {},
): Promise<VirtualCityScenarioCatalogResponse> {
  const params = new URLSearchParams();
  if (query.group && query.group.trim()) params.set('group', query.group.trim());
  if (query.q && query.q.trim()) params.set('q', query.q.trim());
  const limit = Math.min(200, Math.max(1, Math.floor(query.limit ?? 30)));
  params.set('limit', String(limit));
  return http<VirtualCityScenarioCatalogResponse & { scenarios?: VirtualCityScenarioCatalogItem[] }>(
    `/api/games/virtual_city/scenario_catalog?${params.toString()}`,
    { timeoutMs: 10000 },
  ).then((d) => normalizeScenarioCatalog(d, query, limit));
}

/** 目录响应归一化（新契约直用；旧契约 `scenarios[]` 本地过滤）。 */
export function normalizeScenarioCatalog(
  raw: (VirtualCityScenarioCatalogResponse & { scenarios?: VirtualCityScenarioCatalogItem[] }) | undefined,
  query: VirtualCityScenarioCatalogQuery,
  limit: number,
): VirtualCityScenarioCatalogResponse {
  const dimensions = Array.isArray(raw?.dimensions) ? raw.dimensions : [];
  const items = Array.isArray(raw?.items) ? raw.items : [];
  if (dimensions.length > 0 || items.length > 0) {
    return {
      dimensions,
      items,
      total: typeof raw?.total === 'number' ? raw.total : items.length,
    };
  }
  // 兼容路径：既有端点返回全量 scenarios[]。
  const all = Array.isArray(raw?.scenarios) ? raw.scenarios : [];
  const group = (query.group ?? '').trim();
  const q = (query.q ?? '').trim().toLowerCase();
  const filtered = all.filter((it) => {
    if (group && (it.group ?? '') !== group) return false;
    if (!q) return true;
    const hay = `${it.kind ?? ''} ${it.name ?? ''} ${it.description ?? ''} ${it.group ?? ''}`.toLowerCase();
    return hay.includes(q);
  });
  // 维度索引：按 group 聚合 count + 前 3 个代表 kind。
  const byGroup = new Map<string, { count: number; samples: string[] }>();
  for (const it of all) {
    const g = it.group ?? '';
    const hit = byGroup.get(g) ?? { count: 0, samples: [] };
    hit.count += 1;
    if (hit.samples.length < 3 && it.kind) hit.samples.push(it.kind);
    byGroup.set(g, hit);
  }
  return {
    dimensions: [...byGroup.entries()].map(([g, v]) => ({ group: g, count: v.count, samples: v.samples })),
    items: filtered.slice(0, limit),
    total: filtered.length,
  };
}
