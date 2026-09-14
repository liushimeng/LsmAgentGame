/**
 * 财商流游戏 REST API 封装。
 *
 * 对齐 docs/财商流游戏/已实现/02-架构设计/财商流游戏-WS与HTTP协议契约-v1.md §6。
 * 房间 CRUD（list / create / join / leave / spectate）复用 services/auth.service.ts
 * 的 roomService（参数化路由），本文件只承载 wealth 专属端点。
 */

import { http } from '@/services/http';

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
