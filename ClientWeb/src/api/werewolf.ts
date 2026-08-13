/**
 * 狼人杀 REST API 封装(道具 + 房间)。
 *
 * 与 ServerGo/api/prop_api.go + ServerGo/api/werewolf_api.go 对齐。
 * 2026-07-21: §13 道具系统 — 新增 fetchProps()。
 */

import { http } from '@/services/http';
import type { PropListResponse } from '@/types/werewolf';

/**
 * GET /api/games/werewolf/props — 拉取当前可用道具目录 + 我的余额/剩余次数/冷却。
 *
 * §7.1:调用方需在 catch 块用 `reportGlobalError` 把失败消息报到全局 toast,
 * 否则用户看不到。返回 null 时调用方应自行决定是否静默重试。
 */
export async function fetchProps(): Promise<PropListResponse | null> {
  try {
    return await http<PropListResponse>('/api/games/werewolf/props');
  } catch {
    // http<T> 内部已经 throw ApiError;调用方按 §7.1 上报到 UI。
    return null;
  }
}

/** §20260813-02 U2 (T13) — 单道具经济聚合行(与后端 PropEconomyEntry 对齐)。 */
export interface PropEconomyEntry {
  prop_id: string;
  prop_key: string;
  name_zh: string;
  price: number;
  base_hit_rate: number; // 目录基础中招率(%)
  uses: number;
  hits: number;
  hit_rate: number;      // 实测中招率 0..100
  total_spent: number;
  pot_return: number;
  system_absorb: number;
  target_compens: number;
}

/** §20260813-02 U2 — 道具经济顶层汇总。 */
export interface PropEconomySummary {
  total_uses: number;
  total_hits: number;
  overall_hit_rate: number; // 0..100
  total_spent: number;
  total_pot_return: number;
  total_system_absorb: number;
  total_target_compens: number;
}

/**
 * §20260813-02 U2 — wrapper 响应形状(§121:http<T> 直接展开 data,
 * 后端 data 是 {summary, entries} wrapper 对象,必须显式声明本类型)。
 */
export interface PropEconomyResponse {
  summary: PropEconomySummary;
  entries: PropEconomyEntry[];
}

/** GET /api/games/werewolf/prop-economy — 道具经济分析聚合。 */
export async function fetchPropEconomy(): Promise<PropEconomyResponse | null> {
  try {
    return await http<PropEconomyResponse>('/api/games/werewolf/prop-economy');
  } catch {
    return null;
  }
}