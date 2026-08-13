// Anthropic-protocol LLM model metadata — mirrors /api/llm/models.
// Used by the werewolf room-create modal to populate the AI-model picker.
// §20260812-02 U1: Added radar stats endpoint.

import { http } from '@/services/http';

export interface ModelInfo {
  agent_name: string;
  model: string;
  provider_type: string;
}

export async function listModels(): Promise<ModelInfo[]> {
  return http<ModelInfo[]>('/api/llm/models');
}

/** §20260812-02 U1 — 5-dimension capability radar per model. */
export interface ModelRadarStats {
  provider_id: string;
  agent_name: string;
  games: number;
  win_rate: number;       // 0..100
  wolf_win_rate: number;  // 0..100
  good_win_rate: number;  // 0..100
  token_eff: number;      // 0..100
  coin_per_game: number;  // 0..100
  sample_ok: boolean;     // games >= 8
}

/** GET /api/llm/radar — returns map keyed by provider_id. */
export async function getRadarStats(): Promise<Record<string, ModelRadarStats>> {
  return http<Record<string, ModelRadarStats>>('/api/llm/radar');
}

/** §20260813-02 U1 (T12) — 单日胜负点(趋势折线)。 */
export interface WinTrendDayPoint {
  day: string;        // "2006-01-02"
  games: number;
  wins: number;
  win_rate: number;   // 0..100
}

/** §20260813-02 U1 — 单一维度(角色/座位)胜率切片。 */
export interface WinTrendSlice {
  key: string;        // role_key 或座位号字符串
  games: number;
  wins: number;
  win_rate: number;   // 0..100
}

/** §20260813-02 U1 — 单模型胜率趋势聚合(与后端 ModelWinTrend 对齐)。 */
export interface ModelWinTrend {
  provider_id: string;
  agent_name: string;
  games: number;
  wins: number;
  win_rate: number;   // 总胜率 0..100
  trend: WinTrendDayPoint[];
  by_role: WinTrendSlice[];
  by_seat: WinTrendSlice[];
  sample_ok: boolean;
}

/** GET /api/llm/win-trends — returns map keyed by provider_id(§121 直解 map)。 */
export async function getWinTrends(): Promise<Record<string, ModelWinTrend>> {
  return http<Record<string, ModelWinTrend>>('/api/llm/win-trends');
}
