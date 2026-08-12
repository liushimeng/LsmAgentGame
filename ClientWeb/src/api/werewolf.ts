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