// modelAdmin.ts — Admin LLM model management API client.
// §13 frontend-dev: only modifies ClientWeb/.
//
// All paths mirror plan §2.5 (admin/llm/*).
// Backend implementation is in `ServerGo/api/model_admin_api.go`,
// `model_log_api.go`, `model_wallet_api.go` — none of which exist yet, but the
// URL shapes and request/response bodies here are *contracts* the backend will
// implement (see plan §1.1–1.6 for field definitions).
//
// Reuses the same `http<T>()` wrapper as the other admin endpoints so 401/403
// handling, JSON envelopes, and Bearer-token injection stay consistent.

import { http, ApiError } from '@/services/http';
import type {
  LlmProvider,
  LlmProviderCreate,
  ModelGameLog,
  ModelChatMessage,
  ModelAction,
  BotWallet,
  ProviderTestResult,
} from '@/types/model';

// ---------------------------------------------------------------------------
// Provider CRUD
// ---------------------------------------------------------------------------

/**
 * 后端 GET /api/admin/llm/providers 的实际返回结构是:
 *   { code, message, data: { providers: LlmProvider[], total: number, source: string } }
 * 旧版前端直接用 http<LlmProvider[]>() 拿到 data 后会拿到 {providers, total, source} 对象,
 * 然后 .map() 报 "providers.map is not a function" 触发整个页面渲染异常(R85 后端已上线
 * §118 admin_llm_provider 表后立即可见)。这里拆出 ListProvidersResponse 显式声明
 * data 形状,listProviders 提取 .providers 返回数组。
 * 2026-07-10 §121 修复 — 同时修 ListProviderGames 的 items vs games 字段不一致。
 */
export interface ListProvidersResponse {
  providers: LlmProvider[];
  total: number;
  source?: string;
  /** §133 — registry 全局默认 endpoint(DB 行为空时回退到这里) */
  default_endpoint?: string;
  /** §20260816-03 — 本次响应是否包含已停用(软删除)的行 */
  include_disabled?: boolean;
  /** §20260816-03 — 库中 enabled=false 的行数,用于"另有 N 个已停用"提示 */
  disabled_count?: number;
}

/**
 * 后端 POST/PUT /api/admin/llm/providers(/id) 的实际返回结构是:
 *   { code, message, data: { provider: LlmProvider, warning?: string } }
 * CreateProvider 可能带 warning（bot provisioning 失败的非致命提示）。
 * §133.2 修复 — provider 现为完整 view,含 effective_endpoint / endpoint_inherited。
 */
export interface ProviderMutationResponse {
  provider: LlmProvider;
  warning?: string;
}

/**
 * §20260816-03 — 后端默认只返回 enabled=true 的行(删除是软删除)。
 * includeDisabled=true 时追加 ?include_disabled=1 拿回已停用的行。
 */
export async function listProviders(includeDisabled = false): Promise<LlmProvider[]> {
  const resp = await http<ListProvidersResponse>(providersUrl(includeDisabled));
  return resp.providers ?? [];
}

/** 拼装列表 URL,集中处理 include_disabled 查询参数。 */
function providersUrl(includeDisabled: boolean): string {
  return includeDisabled
    ? '/api/admin/llm/providers?include_disabled=1'
    : '/api/admin/llm/providers';
}

/**
 * §133 — 返回 listProviders 的完整响应(含 default_endpoint)。
 * 用于 store 把全局默认 endpoint 缓存,前端展示「实际生效 endpoint」。
 */
export async function listProvidersResponse(
  includeDisabled = false,
): Promise<ListProvidersResponse> {
  const resp = await http<ListProvidersResponse>(providersUrl(includeDisabled));
  return {
    providers: resp.providers ?? [],
    total: resp.total ?? 0,
    source: resp.source,
    default_endpoint: resp.default_endpoint,
    include_disabled: resp.include_disabled ?? includeDisabled,
    disabled_count: resp.disabled_count ?? 0,
  };
}

export async function createProvider(body: LlmProviderCreate): Promise<LlmProvider> {
  const resp = await http<ProviderMutationResponse>('/api/admin/llm/providers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return resp.provider;
}

export async function updateProvider(
  id: string,
  body: Partial<LlmProviderCreate>,
): Promise<LlmProvider> {
  const resp = await http<ProviderMutationResponse>(
    `/api/admin/llm/providers/${encodeURIComponent(id)}`,
    { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) },
  );
  return resp.provider;
}

// 2026-07-10 §121 修复 — 后端 delete 为软删除,data 返回 {id, enabled:false, soft:true};
// 旧前端期望 {ok: boolean}。统一封装为 {ok} 由 enabled/soft 推断。
//
// §20260816-03 — 新增 hard 参数。hard=true 走 ?hard=1 物理删除(需超级管理员,
// 且该模型无对局日志/对话记录引用,否则后端 409)。返回体多出 hard 与
// deleted_bot_users 两个字段,供 store 区分提示文案。
export async function deleteProvider(
  id: string,
  hard = false,
): Promise<{
  ok: boolean;
  id?: string;
  enabled?: boolean;
  soft?: boolean;
  hard?: boolean;
  deleted_bot_users?: number;
}> {
  const url = `/api/admin/llm/providers/${encodeURIComponent(id)}${hard ? '?hard=1' : ''}`;
  const resp = await http<{
    id: string;
    enabled?: boolean;
    soft?: boolean;
    hard?: boolean;
    deleted_bot_users?: number;
  }>(url, { method: 'DELETE' });
  return { ok: !!resp?.id, ...resp };
}

export async function testProvider(id: string): Promise<ProviderTestResult> {
  // 后端测试调用按 llm.timeout_ms(默认 600s)对齐 provider 超时,慢模型
  // (Kimi/GLM/DeepSeek)首字节 1-3 分钟是预期场景;前端 fetch 预算必须 ≥ 后端,
  // 否则浏览器先 abort,用户看到的是误导性的"请求超时"而非真实 chat 结果。
  return http<ProviderTestResult>(
    `/api/admin/llm/providers/${encodeURIComponent(id)}/test`,
    { method: 'POST', timeoutMs: 660_000 },
  );
}

// 2026-07-10 §121 修复 — 后端 reload 返回 {reloaded, source, usable},旧前端
// 期望 {count};统一以 reloaded 为准作为 count 别名,避免 UI 拿到 undefined。
export async function reloadProviders(): Promise<{ count: number; reloaded?: number; source?: string; usable?: number }> {
  const resp = await http<{ reloaded: number; source: string; usable: number }>(
    '/api/admin/llm/providers/reload',
    { method: 'POST' },
  );
  return { count: resp.reloaded, ...resp };
}

// ---------------------------------------------------------------------------
// Log queries
// ---------------------------------------------------------------------------

// 2026-07-10 §121 修复 — 后端 ListProviderGames 返回 data.games[] 但旧前端类型
// 写成 items[],访问 detail.items.map() 会抛 TypeError。统一以 games 为准,
// items 保留为兼容 alias(便于旧组件过渡)。
export interface ProviderGamesResponse {
  games: ModelGameLog[];
  items: ModelGameLog[];
  total: number;
  limit: number;
  offset: number;
  provider_id?: string;
}

export async function listProviderGames(
  id: string,
  params: { limit?: number; offset?: number } = {},
): Promise<ProviderGamesResponse> {
  const qs = new URLSearchParams();
  if (params.limit != null) qs.set('limit', String(params.limit));
  if (params.offset != null) qs.set('offset', String(params.offset));
  const suffix = qs.toString() ? `?${qs.toString()}` : '';
  const resp = await http<{
    games: ModelGameLog[];
    total: number;
    limit: number;
    offset: number;
    provider_id?: string;
  }>(
    `/api/admin/llm/providers/${encodeURIComponent(id)}/games${suffix}`,
  );
  // 旧前端按 items 取;新前端可按 games 取;同时填充别名,避免后续组件读 undefined.map 崩溃。
  const games = resp.games ?? [];
  return { ...resp, games, items: games };
}

// 2026-07-10 §121 修复 — 后端 GetGameLog 直接返回 data.row (TLsmGameModelGameLog),
// 旧前端把类型定义为 {game, actions} 取 detail.game.id 时拿到 undefined;
// 改用 alias 同时暴露 game = row 以兼容旧组件,actions 留 [] 占位
// (具体 actions 通过 ListGameActions 端点拉取)。
export interface GameLogDetail {
  game: ModelGameLog;
  actions: ModelAction[];
}

export async function getGameLog(gameLogID: string): Promise<GameLogDetail> {
  const row = await http<ModelGameLog>(
    `/api/admin/llm/games/${encodeURIComponent(gameLogID)}`,
  );
  return { game: row, actions: [] };
}

export async function getGameMessages(gameLogID: string): Promise<ModelChatMessage[]> {
  return http<ModelChatMessage[]>(
    `/api/admin/llm/games/${encodeURIComponent(gameLogID)}/messages`,
  );
}

// ---------------------------------------------------------------------------
// Bot wallet
// ---------------------------------------------------------------------------

export async function getBotWallet(botUserID: string): Promise<BotWallet> {
  return http<BotWallet>(
    `/api/admin/llm/bots/${encodeURIComponent(botUserID)}/wallet`,
  );
}

export async function adjustBotWallet(
  botUserID: string,
  body: { amount: number; remark: string },
): Promise<BotWallet> {
  return http<BotWallet>(
    `/api/admin/llm/bots/${encodeURIComponent(botUserID)}/wallet/adjust`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

// ---------------------------------------------------------------------------
// Super-admin daily grant (§135)
// ---------------------------------------------------------------------------

/**
 * 2026-07-14 §135 — 超级管理员可对所有（或单个）LLM Provider 的 bot 钱包
 * 一次性发放金币；后端以 (provider_id, grant_date) 复合唯一键保证每天每
 * 模型最多一次。
 *
 * `provider_id` 缺省 → 后端遍历所有 enabled provider 批量发放。
 * 响应分三段：
 *   - granted: 本次新发放的 provider
 *   - skipped: 今日已发过的 provider（重复点击的去重结果）
 *   - date:    UTC+8 日期 YYYY-MM-DD
 */
export interface GrantDailyRequest {
  provider_id?: string;
  amount: number;
  remark: string;
}

export interface GrantedItem {
  provider_id: string;
  provider_name: string;
  bot_user_id: string;
  amount: number;
  balance_after: number;
}

export interface GrantDailyResponse {
  granted: GrantedItem[];
  skipped: GrantedItem[];
  date: string;
}

export async function grantDailyToAll(body: GrantDailyRequest): Promise<GrantDailyResponse> {
  return http<GrantDailyResponse>('/api/admin/llm/bots/grant-daily', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

// ---------------------------------------------------------------------------
// Convenience namespace — grouped object (see plan §2.5 description).
// ---------------------------------------------------------------------------

export const modelAdminApi = {
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  testProvider,
  reloadProviders,
  listProviderGames,
  getGameLog,
  getGameMessages,
  getBotWallet,
  adjustBotWallet,
  grantDailyToAll,
};

export { ApiError };
