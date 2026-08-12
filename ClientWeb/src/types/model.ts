// Model admin types — mirrors `ServerGo/models/t_lsm_game_llm_provider.go` and
// the four related tables (game_log / chat_message / action / wallet).
//
// Front-end keeps these types in sync with the Go structs (manually — no codegen
// pipeline). If the backend renames or removes a field, update here and the
// `modelAdmin` API wrapper simultaneously.

export interface LlmProvider {
  id: string;
  agent_name: string;
  model: string;
  provider_type: string;
  api_key_hint: string;
  /** DB 行覆盖的 endpoint;为空时回退到 `default_endpoint`（来自 registry 全局配置） */
  endpoint: string;
  /** §133 — 实际生效 endpoint(DB 覆盖或全局默认);前端用它做列表 + 弹窗完整展示 */
  effective_endpoint?: string;
  /** §133 — true 表示 DB 行为空,实际值来自全局;前端打「← 全局」标识 */
  endpoint_inherited?: boolean;
  /** §135 修复 — 该 provider 对应 bot 玩家 user_id。
   *  钱包 / 对局历史 的路由参数都是 bot_user_id,而不是 provider.id。
   *  缺省表示「该 provider 尚未生成 bot user」,详情页按"无钱包"提示。*/
  bot_user_id?: string;
  /** 该 provider 对应 bot 玩家钱包余额;后端 bot user / wallet 不存在时省略。 */
  balance?: number;
  /** §R224 (2026-08-01) — 重新引入 §128 误删的 extended thinking 配置。
   *  与后端 t_lsm_game_llm_provider JSON 字段 thinking_enabled /
   *  thinking_budget_tokens 1:1 对应。true 时 anthropic.Provider 在 LLM 请求
   *  的每条 message 头注入 `{type:"thinking", budget:N}` 块;8 家代理必须。*/
  thinking_enabled: boolean;
  thinking_budget_tokens: number;
  enabled: boolean;
  remark: string;
  created_at: string;
  updated_at: string;
}

export interface LlmProviderCreate {
  agent_name: string;
  model: string;
  provider_type: string;
  /** Plaintext API key — used only by create/update endpoints; never echoed back. */
  api_key: string;
  endpoint?: string;
  /** §R224 (2026-08-01) — 重新引入 §128 误删字段。thinking_budget_tokens=0
   *  由后端兜底为 4096。*/
  thinking_enabled?: boolean;
  thinking_budget_tokens?: number;
  enabled?: boolean;
  remark?: string;
}

export interface ModelGameLog {
  id: string;
  provider_id: string;
  bot_user_id: string;
  room_id: string;
  /** Game identifier — one of xiangqi / chess / junqi / doudizhu / texasholdem / werewolf. */
  game_kind: string;
  seat: number;
  role: string;
  started_at: string;
  ended_at?: string;
  /** win / lose / draw / abandoned. */
  result: string;
  coin_delta: number;
  llm_call_count: number;
  input_tokens: number;
  output_tokens: number;
  final_hand: string;
}

export interface ModelChatMessage {
  id: number;
  game_log_id: string;
  bot_user_id: string;
  provider_id: string;
  room_id: string;
  seq: number;
  /** user / assistant / tool_result / system. */
  role: string;
  content: string;
  phase: string;
  tool_name: string;
  tool_input: string;
  thinking: string;
  stop_reason: string;
  latency_ms: number;
  created_at: string;
}

export interface ModelAction {
  id: number;
  game_log_id: string;
  bot_user_id: string;
  phase: string;
  action_type: string;
  action_target: string;
  payload: string;
  reasoning: string;
  accepted: boolean;
  created_at: string;
}

export interface BotWallet {
  user_id: string;
  balance: number;
  total_earned: number;
  total_spent: number;
  transactions: WalletTx[];
}

export interface WalletTx {
  id: string;
  user_id: string;
  tx_type: string;
  amount: number;
  balance_after: number;
  ref_type: string;
  ref_id: string;
  game_kind: string;
  remark: string;
  created_at: string;
}

/**
 * Result envelope returned by `POST /api/admin/llm/providers/:id/test`.
 *
 * 2026-07-10 重构:从"HEAD 探测"升级为"真实 Anthropic 对话"。
 * 后端会真发一次 `provider.Chat(...)` 调用并把回复写回 chat_text,
 * 前端可以直接展示模型的自我介绍。
 *
 * 2026-07-14 §134 重构:无论成功失败都填充完整的请求 / 响应诊断信息
 * (request_url / request_headers / request_body / response_status /
 *  response_headers / response_body),运维在弹窗里直接看到"调用了什么、回了什么"
 * 用于定位 placeholder / 401 / 400 / 网络层错误。
 */
export interface ProviderTestResult {
  ok: boolean;
  latency_ms: number;
  message: string;
  /** 后端 result.ok — registry + 真实 chat 都通过。 */
  result_ok?: boolean;
  /** 真实对话是否成功(可能 ok=false 但 chat_ok=true)。 */
  chat_ok?: boolean;
  /** 模型回复原文(中文自我介绍)。 */
  chat_text?: string;
  /** 真实对话失败原因(给运维看)。 */
  chat_error?: string;
  chat_latency_ms?: number;
  chat_usage_input_tokens?: number;
  chat_usage_output_tokens?: number;
  chat_stop_reason?: string;
  chat_id?: string;
  /** 发给模型的提示词原文,前端回显。 */
  prompt?: string;
  /** registry 可用 + HEAD 探测 + 模型本身。 */
  registry_ok?: boolean;
  endpoint_ok?: boolean;
  hint?: string;
  /** §134 — 出站请求的目标 URL(完整路径,含 /v1/messages 后缀)。 */
  request_url?: string;
  /** §134 — 出站请求头(Authorization 按 Bearer first8...last4 脱敏)。 */
  request_headers?: Record<string, string>;
  /** §134 — 出站请求体(JSON 字符串,indent=2 渲染)。 */
  request_body?: string;
  /** §134 — 上游返回 HTTP 状态码。0 表示未发出去(网络层失败 / ctx timeout)。 */
  response_status?: number;
  /** §134 — 上游返回 HTTP 响应头(关键字段)。 */
  response_headers?: Record<string, string>;
  /** §134 — 上游返回 HTTP 响应体 / 解码错误 / ProviderError.Message。 */
  response_body?: string;
}
