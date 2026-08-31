/**
 * 辩论比赛 REST API 封装 (2026-08-31 §20260831-01)
 *
 * 对齐 ServerGo/api/debate_api.go,与 docs/辩论比赛/00 §4.1 端点。
 */
import { http } from '@/services/http';
import type {
  DebateClientState,
  DebateCreateRoomRequest,
  DebateHistoryDetail,
  DebateHistoryListData,
  DebateJudgeScoreboard,
  DebateModelStats,
  DebateRoomSummary,
  DebateTopic,
} from '@/types/debate';

export const debateService = {
  // GET /api/games/debate/topics
  listTopics(params?: { q?: string; type?: string; category?: string }) {
    const qs = new URLSearchParams();
    if (params?.q) qs.set('q', params.q);
    if (params?.type) qs.set('type', params.type);
    if (params?.category) qs.set('category', params.category);
    const suffix = qs.toString() ? `?${qs.toString()}` : '';
    return http<DebateTopic[]>(`/api/games/debate/topics${suffix}`);
  },

  // POST /api/games/debate/rooms
  createRoom(req: DebateCreateRoomRequest) {
    return http<{
      room_id: string;
      summary: DebateRoomSummary;
      client_state: DebateClientState;
    }>('/api/games/debate/rooms', {
      method: 'POST',
      body: JSON.stringify(req),
      timeoutMs: 30_000,
    });
  },

  // GET /api/games/debate/rooms
  listRooms(params?: { topic_type?: string; mode?: string; status?: string }) {
    const qs = new URLSearchParams();
    if (params?.topic_type) qs.set('topic_type', params.topic_type);
    if (params?.mode) qs.set('mode', params.mode);
    if (params?.status) qs.set('status', params.status);
    const suffix = qs.toString() ? `?${qs.toString()}` : '';
    return http<DebateRoomSummary[]>(`/api/games/debate/rooms${suffix}`);
  },

  // GET /api/games/debate/rooms/:id
  detail(roomId: string) {
    return http<DebateClientState>(`/api/games/debate/rooms/${encodeURIComponent(roomId)}`);
  },

  // POST /api/games/debate/rooms/:id/spectate
  spectate(roomId: string) {
    return http<DebateClientState>(
      `/api/games/debate/rooms/${encodeURIComponent(roomId)}/spectate`,
      { method: 'POST' },
    );
  },

  // POST /api/games/debate/rooms/:id/leave_spectate
  leaveSpectate(roomId: string) {
    return http<{ ok: true }>(
      `/api/games/debate/rooms/${encodeURIComponent(roomId)}/leave_spectate`,
      { method: 'POST' },
    );
  },

  // POST /api/games/debate/rooms/:id/start
  start(roomId: string) {
    return http<DebateClientState>(
      `/api/games/debate/rooms/${encodeURIComponent(roomId)}/start`,
      { method: 'POST' },
    );
  },

  // GET /api/games/debate/rooms/:id/history
  history(roomId: string) {
    return http<{
      speeches: import('@/types/debate').DebateSpeech[];
      cross_exam: import('@/types/debate').DebateCrossExamEntry[];
      results: import('@/types/debate').DebateResult | null;
    }>(`/api/games/debate/rooms/${encodeURIComponent(roomId)}/history`);
  },

  // GET /api/games/debate/stats (§20260831-06 模型胜率统计)
  stats() {
    return http<DebateModelStats[]>('/api/games/debate/stats');
  },

  // GET /api/games/debate/history (§20260831-08 大厅历史战绩分页列表)
  listHistory(page = 1, pageSize = 20) {
    const qs = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    return http<DebateHistoryListData>(`/api/games/debate/history?${qs.toString()}`);
  },

  // GET /api/games/debate/history/:id (§20260831-08 复盘详情)
  historyDetail(roomId: string) {
    return http<DebateHistoryDetail>(`/api/games/debate/history/${encodeURIComponent(roomId)}`);
  },

  // DELETE /api/games/debate/rooms/:id
  disband(roomId: string) {
    return http<{ ok: true }>(
      `/api/games/debate/rooms/${encodeURIComponent(roomId)}`,
      { method: "DELETE" },
    );
  },

  // GET /api/games/debate/rooms/:id/scoreboards (§20260831-09 裁判实时打分看板)
  scoreboards(roomId: string) {
    return http<DebateJudgeScoreboard[]>(
      `/api/games/debate/rooms/${encodeURIComponent(roomId)}/scoreboards`,
    );
  },
};
