// admin.ts — 管理员专用 HTTP API client
//
// §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
// 与 services/http.ts 共用 `http<T>(...)` 包装(fetch + 401 / session-expired
// 统一处理),返回的 `data` 是后端 Result.data,错误走 ApiError(code, message)。

import { http, ApiError } from '@/services/http';

export interface DisbandResult {
  room_id: string;
  game_kind?: string;
  players_deleted: number;
  reason: string;
  removed_at: string;
}

export interface ChatCleanupResult {
  deleted_count: number;
}

/**
 * 超级管理员强制解散房间。
 *
 * DELETE /api/admin/rooms/:room_id?reason=...
 * - 200 + { code:0, data: DisbandResult } → 成功
 * - 200 + { code:0, data:..., message:"room already absent" } → 房间已经不在了
 * - 401 / 403 → 通过 ApiError 抛出(http.ts 已统一处理)
 *
 * 调通后端会在 WS 推送 `game.removed` 帧;前端 use* 系列的 hook 会
 * 收到后 navigate 回大厅,所以这里调完不需要手动跳。
 */
export async function forceDisbandRoom(
  roomId: string,
  reason = 'admin-force-disband',
): Promise<DisbandResult> {
  try {
    return await http<DisbandResult>(
      `/api/admin/rooms/${encodeURIComponent(roomId)}?reason=${encodeURIComponent(reason)}`,
      { method: 'DELETE' },
    );
  } catch (e) {
    // 把 ApiError 重新抛出,UI 层 catch 后渲染 message;非 ApiError 保留堆栈。
    if (e instanceof ApiError) throw e;
    throw e;
  }
}

/**
 * 超级管理员强制解散辩论房间。
 *
 * DELETE /api/admin/debate/rooms/:room_id?reason=...
 * - 200 + { code:0, data: {...} } → 成功
 * - 200 + { code:0, data:..., message:"room already absent" } → 房间已经不在了
 * - 401 / 403 → 通过 ApiError 抛出
 *
 * 后端会广播 debate.room_removed 帧;前端 useDebate hook 收到后 navigate 回大厅。
 */
export async function forceDisbandDebateRoom(
  roomId: string,
  reason = 'admin-force-disband',
): Promise<{ room_id: string; reason: string; removed_at: string; spectators: number }> {
  try {
    return await http<{ room_id: string; reason: string; removed_at: string; spectators: number }>(
      `/api/admin/debate/rooms/${encodeURIComponent(roomId)}?reason=${encodeURIComponent(reason)}`,
      { method: 'DELETE' },
    );
  } catch (e) {
    if (e instanceof ApiError) throw e;
    throw e;
  }
}

/**
 * 管理员按时间范围清理大厅聊天消息。
 *
 * POST /api/admin/chat/cleanup
 * - 200 + { code:0, data: { deleted_count } } → 成功
 * - 401 / 403 → 通过 ApiError 抛出
 *
 * @param startTime 开始时间（RFC3339 格式）
 * @param endTime   结束时间（RFC3339 格式）
 */
export async function cleanupChatMessages(
  startTime: string,
  endTime: string,
): Promise<ChatCleanupResult> {
  try {
    return await http<ChatCleanupResult>('/api/admin/chat/cleanup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ start_time: startTime, end_time: endTime }),
    });
  } catch (e) {
    if (e instanceof ApiError) throw e;
    throw e;
  }
}
