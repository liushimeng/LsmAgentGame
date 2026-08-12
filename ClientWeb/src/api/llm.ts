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
