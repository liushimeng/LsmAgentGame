/**
 * 虚拟城市 REST API 封装。
 *
 * 对齐 lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-WS与HTTP协议契约-v1.md §6。
 * 房间 CRUD（list / create / join / leave / spectate）复用 services/auth.service.ts
 * 的 roomService（参数化路由），本文件只承载 virtualCity 专属端点。
 */

import { http, ApiError, isSessionExpiredError } from '@/services/http';
import type { VirtualCityCityProfileProgress, VirtualCityCityResidentProfile, VirtualCitySurvey } from '@/types/virtualCity';
import type { RealtimeBoardRefreshResult, RealtimeFeed } from '@/types/virtualCityRealtime';

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
 * 返回 {code:0, data:{feed: Feed}}；late-joiner 首屏靠它补齐
 * game.realtime_feed 帧间隙。数据是全局的（与房间无关）。
 * §7.1：首屏拉取失败不算操作错误 —— 调用方静默兜底置空即可。
 */
export function fetchRealtimeBoard(): Promise<{ feed: RealtimeFeed }> {
  return http<{ feed: RealtimeFeed }>('/api/virtual_city/realtime_board');
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

// ── 2026-10-21 §城市模拟定位 · 场景推演（条件触发者）──────────────────────
// 契约（backend-dev 并行实现，字段/路径逐字对齐 tmpPlan/
// 虚拟城市-模拟程序化与预测运行优化_20261021_095114.md §5.1）：
//   GET  /api/games/virtual_city/scenario_catalog
//   POST /api/games/virtual_city/rooms/:id/scenario
//   GET  /api/games/virtual_city/rooms/:id/scenarios
// ⚠️ 路径一律用下划线 `virtual_city`（后端 router 的 `r.Group("/api/games/virtual_city")`；
//    未登录探针：下划线 401、连字符 404）。本文件既有的调研/居民档案函数原写作
//    `virtual-city`（连字符），已随本次修复统一为下划线，勿再改回。
// 权限：任意登录用户（含观察者）= 条件触发者。
// §7.1：调用方（ScenarioPanel）catch 后面板内联错误 + reportGlobalError 上报，
// 绝不吞进 console。

/** 场景目录条目（GET scenario_catalog 的 scenarios[] 元素）。 */
export interface VirtualCityScenarioSpec {
  /** 稳定标识（如 earthquake / meteorite / goods_price_shock）。 */
  kind: string;
  /** 展示名（服务端本地化文本，前端按原样展示）。 */
  name: string;
  /** 分组名（市场与经济 / 社会与舆情 / 自然灾害 / 未知灾难）。 */
  group: string;
  description: string;
  /** 可选强度档位（1-5）。 */
  magnitudes: number[];
  /** 默认强度。 */
  default_magnitude: number;
  /** 该场景支持的附加参数键（如 duration_months / target）。 */
  params: string[];
}

/** 注入瞬间的指标效果（effects[] 元素）。 */
export interface VirtualCityScenarioEffect {
  metric: string;
  delta: number;
  text: string;
}

/** 场景注入记录（POST 返回的 scenario / GET scenarios 列表元素）。 */
export interface VirtualCityScenarioRecord {
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

/** POST scenario 的响应 data 形状。 */
export interface VirtualCityScenarioApplyResult {
  scenario: VirtualCityScenarioRecord;
  effects: VirtualCityScenarioEffect[];
}

/** POST scenario 的请求体（duration_months / target / note 均可选）。 */
export interface VirtualCityScenarioApplyRequest {
  kind: string;
  magnitude: number;
  duration_months?: number;
  target?: string;
  note?: string;
}

/**
 * GET /api/games/virtual_city/scenario_catalog（需登录）— 场景目录。
 *
 * 返回 {code:0, data:{scenarios:[...]}}；前端每次打开面板拉取一次并按 group 分组。
 * §7.1：失败面板内联 + 全局 toast 双通道展示。
 */
export function fetchScenarioCatalog(): Promise<{ scenarios: VirtualCityScenarioSpec[] }> {
  return http<{ scenarios: VirtualCityScenarioSpec[] }>(
    '/api/games/virtual_city/scenario_catalog',
  );
}

/**
 * POST /api/games/virtual_city/rooms/:id/scenario（需登录）— 注入一个场景条件。
 *
 * 返回 {code:0, data:{scenario, effects}}；effects 为注入瞬间的指标变化，
 * 面板就地展示后刷新历史列表。错误（场景不存在 / 参数非法 / 房间未运行 /
 * 本月注入超限）由服务端错误码 + message 返回，调用方原样内联展示。
 */
export function applyVirtualCityScenario(
  roomId: string,
  body: VirtualCityScenarioApplyRequest,
): Promise<VirtualCityScenarioApplyResult> {
  return http<VirtualCityScenarioApplyResult>(
    `/api/games/virtual_city/rooms/${encodeURIComponent(roomId)}/scenario`,
    {
      method: 'POST',
      body: JSON.stringify(body),
    },
  );
}

/**
 * GET /api/games/virtual_city/rooms/:id/scenarios（需登录）— 最近场景注入记录。
 *
 * 返回 {code:0, data:{scenarios:[...]}}（后端取最近 20 条）。
 */
export function fetchVirtualCityScenarios(
  roomId: string,
): Promise<{ scenarios: VirtualCityScenarioRecord[] }> {
  return http<{ scenarios: VirtualCityScenarioRecord[] }>(
    `/api/games/virtual_city/rooms/${encodeURIComponent(roomId)}/scenarios`,
  );
}
