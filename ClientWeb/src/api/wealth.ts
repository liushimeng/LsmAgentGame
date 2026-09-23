/**
 * 虚拟城市 REST API 封装。
 *
 * 对齐 lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-WS与HTTP协议契约-v1.md §6。
 * 房间 CRUD（list / create / join / leave / spectate）复用 services/auth.service.ts
 * 的 roomService（参数化路由），本文件只承载 wealth 专属端点。
 */

import { http, ApiError, isSessionExpiredError } from '@/services/http';
import type { WealthCityProfileProgress, WealthCityResidentProfile, WealthSurvey } from '@/types/wealth';

// 2026-09-22 §CityHuman全民驱动 — GET /api/games/wealth/professions 已随
// 职业卡精选层退役（03 号契约）：原拉取接口及其三个类型一并删除。
// 档案唯一源 = 人物卡知识库（城市居民档案两接口 fetchCityResidents /
// fetchCityResident 保留）。

// ── P1 社会调研系统（lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-社会调研系统-v1.md §3.1）──

/**
 * POST /api/games/wealth/rooms/:id/survey（需登录）。
 *
 * 任意登录用户可发起（在座 / 观战 / 房外均可——调研是「向 AI 提问」）。
 * 服务端限流（房间级）：同时仅 1 个 open(35017) / 每月 1 个(35018) /
 * 累计 ≤20 个(35018)；options 2-6 个且非空(35016)；仅 playing 可发起(35002)。
 * §7.1：调用方 catch 后表单内联红条展示，勿吞进 console。
 */
export function launchWealthSurvey(
  roomId: string,
  question: string,
  options: string[],
): Promise<{ survey: WealthSurvey }> {
  return http<{ survey: WealthSurvey }>(
    `/api/games/wealth/rooms/${roomId}/survey`,
    {
      method: 'POST',
      body: JSON.stringify({ question, options }),
    },
  );
}

/**
 * GET /api/games/wealth/rooms/:id/surveys（需登录）。
 *
 * 返回全部历史调研快照（≤20，含 open 与 closed）。SurveyPanel 挂载时拉取，
 * 后续增量由 game.survey_result 帧（useWealth）按 id 去重覆盖。
 */
export function fetchWealthSurveys(roomId: string): Promise<{ surveys: WealthSurvey[] }> {
  return http<{ surveys: WealthSurvey[] }>(
    `/api/games/wealth/rooms/${roomId}/surveys`,
  );
}

// ── 城市居民人物卡档案（档案锚定设计 §7 REST 契约）──────────────────────

/** GET /city/residents 的 data 形状：锚定进度 + 一页档案 + 命中总数。 */
export interface WealthCityResidentsPage {
  progress: WealthCityProfileProgress;
  residents: WealthCityResidentProfile[];
  /** q 过滤后的命中总数（分页用）。 */
  matched: number;
}

/**
 * GET /api/games/wealth/rooms/:id/city/residents（需登录；档案是公开合成人格）。
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
): Promise<WealthCityResidentsPage> {
  const params = new URLSearchParams();
  params.set('offset', String(Math.max(0, Math.floor(offset) || 0)));
  params.set('limit', String(Math.min(200, Math.max(1, Math.floor(limit) || 50))));
  if (q && q.trim()) params.set('q', q.trim());
  return http<WealthCityResidentsPage>(
    `/api/games/wealth/rooms/${roomId}/city/residents?${params.toString()}`,
  );
}

/**
 * GET /api/games/wealth/rooms/:id/city/residents/:cardId（需登录）。
 *
 * 命中 → ResidentProfile 全字段（服务端包一层 data.resident，此处解包）；
 * 查不到（ErrWealthResidentNotFound：后端实测 35042 / 设计文档 §7 曾写 35013，
 * 两者都兼容）→ 返回 null（§8.2 契约），其余错误照常抛出。
 */
export async function fetchCityResident(
  roomId: string,
  cardId: string,
): Promise<WealthCityResidentProfile | null> {
  try {
    const data = await http<{ resident?: WealthCityResidentProfile }>(
      `/api/games/wealth/rooms/${roomId}/city/residents/${encodeURIComponent(cardId)}`,
    );
    return data?.resident ?? null;
  } catch (e) {
    if (e instanceof ApiError && (e.code === 35042 || e.code === 35013)) return null;
    if (isSessionExpiredError(e)) return null; // 会话过期已由全局弹层接管
    throw e;
  }
}
