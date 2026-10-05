/**
 * LsmKLBaseServer 知识库引擎 REST API 封装。
 *
 * 2026-10-05 §知识库重构 — 知识卡特征查询 + MCP 端点。
 * 契约: docs/知识库系统/LsmKLBaseServer-知识库引擎重构方案.md §6.1。
 */

import { http } from '@/services/http';

// ── 知识卡类型 ──────────────────────────────────────────────

/** 知识卡（LLM 特征抽取后的完整视图）。 */
export interface KnowledgeCard {
  id: string;
  source_path: string;
  domain: string;
  title: string;
  name: string;
  salary: number;
  expense: number;
  savings: number;
  start_age: number;
  energy: number;
  network: number;
  cognition: number;
  credit_score: number;
  home_district: string;
  risk_preference: string;
  personality: string[];
  behavior_traits: string[];
  health_grade: string;
  gender: string;
  employment: string;
  marital: string;
  children_count: number;
  elders_dependent: number;
  opening_hook: string;
  goals: string[];
  features: string[];
  extract_status: number;
}

/** 知识库统计。 */
export interface KnowledgeStats {
  total: number;
  extracted: number;
  failed: number;
  domains: Record<string, number>;
  districts: Record<string, number>;
}

/** 搜索结果。 */
export interface KnowledgeSearchResult {
  cards: KnowledgeCard[];
  matched: number;
  total: number;
}

// ── API 函数 ──────────────────────────────────────────────

/**
 * GET /api/knowledge/cards — 按条件搜索知识卡。
 *
 * @param params features(逗号分隔的特征字符串)、domain、district、offset、limit
 * §7.1：调用方 catch 后就地展示，勿吞进 console。
 */
export function searchKnowledgeCards(params: {
  features?: string;
  domain?: string;
  district?: string;
  offset?: number;
  limit?: number;
}): Promise<KnowledgeSearchResult> {
  const query = new URLSearchParams();
  if (params.features) query.set('features', params.features);
  if (params.domain) query.set('domain', params.domain);
  if (params.district) query.set('district', params.district);
  if (params.offset !== undefined) query.set('offset', String(params.offset));
  if (params.limit !== undefined) query.set('limit', String(params.limit));
  const qs = query.toString();
  return http<KnowledgeSearchResult>(`/api/knowledge/cards${qs ? `?${qs}` : ''}`);
}

/**
 * GET /api/knowledge/cards/:id — 按卡号获取单卡。
 */
export function getKnowledgeCard(id: string): Promise<{ card: KnowledgeCard }> {
  return http<{ card: KnowledgeCard }>(`/api/knowledge/cards/${encodeURIComponent(id)}`);
}

/**
 * GET /api/knowledge/stats — 获取知识库统计。
 */
export function getKnowledgeStats(): Promise<KnowledgeStats> {
  return http<KnowledgeStats>('/api/knowledge/stats');
}

/**
 * POST /api/knowledge/extract — 触发特征抽取（管理用途）。
 */
export function triggerKnowledgeExtract(cardIds?: string[]): Promise<{ pending: number; message: string }> {
  return http<{ pending: number; message: string }>('/api/knowledge/extract', {
    method: 'POST',
    body: JSON.stringify({ card_ids: cardIds ?? [] }),
  });
}

/**
 * POST /api/knowledge/mcp — MCP 协议端点（JSON-RPC 2.0）。
 *
 * 供外部 LLM Agent 通过标准 MCP 协议消费知识库。
 */
export function callKnowledgeMCP(
  method: string,
  params?: Record<string, unknown>,
  id: number = 1,
): Promise<unknown> {
  return http<unknown>('/api/knowledge/mcp', {
    method: 'POST',
    body: JSON.stringify({ jsonrpc: '2.0', id, method, params }),
  });
}
