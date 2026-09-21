/**
 * 虚拟城市 REST API 封装。
 *
 * 对齐 lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-WS与HTTP协议契约-v1.md §6。
 * 房间 CRUD（list / create / join / leave / spectate）复用 services/auth.service.ts
 * 的 roomService（参数化路由），本文件只承载 wealth 专属端点。
 */

import { http } from '@/services/http';
import type { WealthSurvey } from '@/types/wealth';

/** GET /api/games/wealth/professions 返回的职业卡公开字段。 */
export interface WealthProfessionCard {
  id: string;            // "P01"（文档池 "N9012345"）
  title: string;         // 中文名
  avatar: string;        // 头像文件名主干
  salary: number;        // 月薪（税前，元）
  expense: number;       // 月支出基数（元）
  savings: number;       // 初始储蓄（元）
  opening_hook?: string; // 开场白
  goals?: string[];      // 人生目标（含 5 年目标）
}

export interface WealthProfessionPool {
  /** 文档池是否可用（不可用时建房自动回退精选手卡）。 */
  available: boolean;
  /** 池内卡总数（懒加载前可为估算）。 */
  total: number;
  /** 已建索引数。 */
  indexed: number;
}

export interface WealthProfessionsResponse {
  curated: WealthProfessionCard[];
  pool: WealthProfessionPool;
}

/**
 * GET /api/games/wealth/professions（需登录）。
 *
 * §7.1：调用方需在 catch 块就地展示失败（建房弹窗内联红条或 reportGlobalError），
 * 不允许吞进 console。失败时调用方可回落 CURATED_PROFESSIONS 静态镜像。
 */
export function fetchProfessions(): Promise<WealthProfessionsResponse> {
  return http<WealthProfessionsResponse>('/api/games/wealth/professions');
}

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
