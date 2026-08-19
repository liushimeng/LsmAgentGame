// Shared API types for the frontend.

export interface ApiEnvelope<T> {
  code: number;
  message: string;
  data: T;
}

export interface AuthData {
  user_id: string;
  token: string;
  expires_at: number;
  language: string;
  user_type: number; // 1=normal, 2=admin, 3=super admin
  my_invite_code: string;
}

export interface ReferredUser {
  user_id: string;
  account: string;
  nickname: string;
  // Backend (UserService.ListReferrals) currently ships unix seconds (int64).
  // Other shapes have used ISO 8601 strings; tolerate both at the renderer.
  created_at: string | number;
}

export interface UserProfile {
  user_id: string;
  account: string;
  nickname: string;
  language: string;
  my_invite_code: string;
  referral_count: number;
  referrer_user_id: string;
  referrals: ReferredUser[];
}

export interface GameInfo {
  id: string;
  name: string;
  kind: string;
  // §20260819-02 大厅菜单分组;旧 backend 可能缺失,前端用 'traditional' 兜底
  category?: 'traditional' | 'agent';
  online: number;
}

export interface VersionInfo {
  version: string;
  build_time: string;
  git_sha?: string;  // 服务端新增字段；旧版本 API 可能缺失，可选
}

// Git 提交记录
export interface CommitEntry {
  id: string;          // 40 位完整 hash
  short_id: string;    // git abbrev hash
  author: string;
  author_email: string;
  time: string;        // ISO 时间
  subject: string;
  files_changed: number;
  insertions: number;
  deletions: number;
}

export interface CommitFileStat {
  path: string;
  insertions: number;
  deletions: number;
}

export interface CommitDetail extends CommitEntry {
  files: CommitFileStat[];
}

export interface CommitListPayload {
  entries: CommitEntry[];
  total: number;
  skip: number;
  limit: number;
}

// Wiki —— 项目文档列表与内容查看（docs/ 目录）
export interface WikiEntry {
  name: string;     // 相对文件名（用于 /api/wiki/content?name=X）
  title: string;    // 人类可读标题
  size: number;     // 文件字节数
  mtime: string;    // 修改时间（RFC3339）
  excerpt: string;  // 80 字摘要
}

export interface WikiListPayload {
  entries: WikiEntry[];
}

export interface WikiContentPayload {
  name: string;     // 文件名
  content: string;  // 完整 markdown 文本
}

// 源码统计 —— 标题栏"源码统计"按钮触发的弹窗数据源
export interface SourceStatsGroup {
  name: string;            // 显示名（如 "前端" / "后端" / "总计"）
  path?: string;           // 实际扫描的目录(总计行无 path)
  files: number;           // 文件个数
  lines: number;           // 总行数
  bytes: number;           // 总字节数
  error?: string;          // 该组扫描失败时填充(其他组不受影响)
}

export interface SourceStatsExtension {
  ext: string;             // 扩展名(含点，如 ".go" / ".tsx")
  files: number;
  lines: number;
  bytes: number;
}

export interface SourceStatsPayload {
  groups: SourceStatsGroup[];        // 各组统计
  total: SourceStatsGroup;           // 全部组合计
  extensions: SourceStatsExtension[]; // 按扩展名聚合(全量,按字节数倒序)
  built_at: string;                  // 启动期 epoch (RFC3339)
}

// Chat
export type ChatScope = 'lobby' | 'room';

export interface ChatMessage {
  id: number;
  scope: ChatScope;
  room_id?: string;
  from_user_id: string;
  from_account: string;
  /** "player" | "spectator" | "bot" | "activity" | "" — surfaced by the
   *  server when the sender's role is known. The chat panel renders a 👁
   *  badge for "spectator", an "AI" badge for "bot", and an activity chip
   *  for "activity" (see ChatActivity below). 2026-07-09 §115. */
  from_role?: 'player' | 'spectator' | 'bot' | 'activity' | 'judge' | '';
  /** Bot model display name (e.g. "美团 LongCat-2.0"). Populated by the
   *  server when from_role === "bot" and the bot has a configured model in
   *  the LLM registry. Used by the room chat panel to render the model's
   *  AgentName next to the bot's seat label. */
  from_agent_name?: string;
  /** Whisper (private message) target. Non-empty when this is a DM. */
  to_user_id?: string;
  to_account?: string;
  /** True when this message is a private whisper. Server broadcasts to all
   *  room subscribers; the frontend filters visibility by role. */
  whisper?: boolean;
  /** True when this message is a bot "out-of-turn" chat during the speak
   *  phase (werewolf-agent interject tool). The chat panel renders it with
   *  a 💬 badge so users can distinguish a bot's casual aside from the
   *  formal speak turn. BUG-WEREWOLF-AGENT-INTERJECT (R38). */
  is_interject?: boolean;
  text: string;
  ts: number; // unix milliseconds
}

/** Structured game activity event (phase change / vote / kill / seer check
 *  / etc.) broadcast into the room chat stream. The server emits a
 *  `chat.activity` envelope that useChat converts into a ChatMessage with
 *  `from_role: 'activity'` and the activity fields attached (extension-only,
 *  the ChatMessage type isn't widened to keep compat with other games). */
export interface ChatActivity extends ChatMessage {
  from_role: 'activity';
  event_kind: string;
  phase?: string;
  round_number?: number;
  severity?: 'info' | 'success' | 'warn' | 'error';
  icon?: string;
  ref_seat?: number;
  ref_seat_2?: number;
}

export interface ChatHistoryPayload {
  scope: ChatScope;
  room_id?: string;
  messages: ChatMessage[];
  /** True when there are older messages available via before_id pagination. */
  has_more?: boolean;
  /** Cursor (message id) to pass as before_id for the next page, or null. */
  next_cursor?: number | null;
}

export interface ChatErrorPayload {
  code: number;
  message: string;
}

// 2026-07-12 §13 增强 — Bot 发言 SSE 流式帧(对齐后端 chat_service.SendBotStreamStart/Delta/End)。
// 流式气泡是"预览",真正完整发言由 chat.message(SendFromBot)广播。
// 这里不扩宽 ChatMessage(保持跨游戏兼容),单独声明一个小 interface。

/** chat.stream_start payload — 插入占位流式气泡。 */
export interface ChatStreamStart {
  stream_id: string;
  seat: number;
  ts: number;
}

/** chat.stream_delta payload — 增量 token 追加。 */
export interface ChatStreamDelta {
  stream_id: string;
  delta: string;
  index: number;
  ts: number;
}

/** chat.stream_end payload — 流式结束(带完整文本)。 */
export interface ChatStreamEnd {
  stream_id: string;
  full_text: string;
  ts: number;
}

// ─── Room Management ───

export interface RoomInfo {
  id: string;
  name: string;
  game_kind: string;
  capacity: number;
  current_count: number;
  /** Optional — number of currently attached spectators. Populated by the
   *  detail endpoint; the lobby list call may omit this. */
  spectator_count?: number;
  status: string;
  /** ISO 8601 creation timestamp (server-authoritative). Used by the lobby
   *  list to show + sort by 创建时间. */
  created_at: string;
  /**
   * BUG-R210-01 (2026-07-30): 当前请求者在此房间的角色。
   * 仅 ListRooms 响应填充;Detail/Join/Spectate 的 my_role 在 RoomDetail.my_role。
   * 取值: "player" / "agent" / "spectator" / 缺省(无关)。
   * 刷新后 lobby 用此字段决定按钮是 "进入房间" / "👁 观战" / "已满"。
   */
  my_role?: string;
}

export interface RoomPlayerInfo {
  user_id: string;
  seat: number;
}

export interface RoomSpectatorInfo {
  user_id: string;
}

export interface RoomDetail extends RoomInfo {
  players: RoomPlayerInfo[];
  spectators?: RoomSpectatorInfo[];
  /**
   * 当前请求者在该房间的角色:player / agent / spectator。
   * BUG-R200-P2-05 (2026-07-30):服务端显式下发,前端据此决定走玩家路由
   * (/werewolf/:id)还是观众路由(/werewolf/spectate/:id),不再依赖
   * agent_seats.length >= capacity 推断。列表接口(ListRooms)留空。
   */
  my_role?: string;
}

// ─── Xiangqi (Chinese Chess) ───

export interface XiangqiPiece {
  color: 'red' | 'black';
  type: 'king' | 'advisor' | 'elephant' | 'horse' | 'chariot' | 'cannon' | 'soldier';
  name: string;
}

export type XiangqiBoard = (XiangqiPiece | null)[][]; // [row][col]

export interface XiangqiMove {
  from: { x: number; y: number };
  to: { x: number; y: number };
  piece: XiangqiPiece;
  captured?: XiangqiPiece | null;
}

export interface XiangqiGameState {
  room_id: string;
  red_id: string;
  black_id: string;
  ready: boolean;
  board?: XiangqiBoard;
  turn?: 'red' | 'black';
  my_color?: 'red' | 'black';
  status?: 'playing' | 'red_win' | 'black_win' | 'draw';
  check: boolean;
  move_count: number;
}

export interface XiangqiMoveResult {
  room_id: string;
  move: XiangqiMove;
  turn: string;
  status: string;
  check: boolean;
  board: XiangqiBoard;
}

export interface XiangqiGameOver {
  room_id: string;
  winner: 'red' | 'black' | '';
  reason: string;
  status: string;
}

// ─── Chess (International / Western Chess) ───

export interface ChessPiece {
  color: 'white' | 'black';
  type: 'king' | 'queen' | 'rook' | 'bishop' | 'knight' | 'pawn';
  name: string;
}

export type ChessBoard = (ChessPiece | null)[][]; // [row][col], row 0 = rank 1 (White's side)

export type ChessPromotion = 'queen' | 'rook' | 'bishop' | 'knight';

export interface ChessMove {
  from: { x: number; y: number };
  to: { x: number; y: number };
  piece: ChessPiece;
  captured?: ChessPiece | null;
  promotion?: ChessPromotion;
  castle_kind?: 'king' | 'queen';
  en_passant?: boolean;
}

export interface ChessGameState {
  room_id: string;
  white_id: string;
  black_id: string;
  ready: boolean;
  board?: ChessBoard;
  turn?: 'white' | 'black';
  my_color?: 'white' | 'black' | null;
  status?: 'playing' | 'white_win' | 'black_win' | 'draw';
  check: boolean;
  move_count: number;
  reason?: string;
}

export interface ChessGameOver {
  room_id: string;
  winner: 'white' | 'black' | '';
  reason: string;
  status: string;
}

// ─── Wallet ───

// Tx types returned by the backend wallet ledger. These are the raw values
// stored in t_lsm_game_wallet_tx.tx_type (see ServerGo/service/wallet_service.go).
export type WalletTxReason =
  | 'register_bonus'
  | 'daily_login'
  | 'win_reward'
  | 'lose_deduct'
  | 'ante_buyin'
  | 'ante_refund'
  | 'task_reward'
  | 'referral_bonus'
  | 'admin_adjust'
  // Legacy / frontend shortcuts kept for backwards-compat with older snapshots.
  | 'game_win'
  | 'game_lose'
  | 'daily_bonus'
  | 'ante'
  | 'settle'
  | 'other';

export interface WalletBalance {
  balance: number;
  total_earned?: number;
  total_spent?: number;
}

export interface WalletTransaction {
  id: number;
  amount: number;       // positive = gain, negative = spend
  balance_after: number;
  // Backend serializes tx_type as a string. The frontend uses a string
  // union to drive the i18n map; unknown values fall through to "other".
  tx_type: WalletTxReason;
  reason?: WalletTxReason;  // legacy alias (older payloads)
  game_kind?: string;
  remark?: string;
  ref_type?: string;
  ref_id?: string;
  room_id?: string;
  // Backend returns ISO 8601 (RFC 3339) — the JSON serializer on time.Time.
  // Older drafts of this file assumed unix seconds; that produced
  // "Invalid Date" in the UI. We accept either so stale payloads still render.
  created_at: string | number;
}

export interface WalletTxList {
  entries: WalletTransaction[];
  total: number;
}

/** Server → client WS push: balance change notification. */
export interface WalletBalancePush {
  type: 'wallet.balance';
  balance: number;
  delta: number;
  reason: WalletTxReason | string;
}

// ─── Ante room extensions ───

/** When creating a room, pass these fields to the backend. */
export interface CreateRoomOptions {
  name?: string;           // optional human-readable room name; auto-generated when omitted
  ante?: number;           // 0 = practice room
  mode?: 'hidden' | 'open'; // junqi only
  agent_seats?: AgentSeatRequest[]; // werewolf bot seats (Phase 4)
  // 2026-08-06 §20260806-03 自选角色 — 创建者(人类座位)角色偏好。
  // 取值同 AgentSeatRequest.role;缺省 / 'random' = 随机。
  creator_role?: string;
  // 2026-07-30 §重构 — 法官模式两选项:`agent`(Agent 法官,原"主持人 Agent / AI 法官")
  // 与 `human`(真人法官;当前后端无真人接入实现,行为等同 agent)。旧 ai/off 已废弃。
  judge?: { mode?: 'agent' | 'human'; model_key?: string };
  // 2026-08-11 §20260811-09 U2 — Agent 难度分级。easy=新手 / normal=默认 /
  // hard=熟练 / hell=高手。未知值后端归一化为 normal(零回归)。
  agent_difficulty?: 'easy' | 'normal' | 'hard' | 'hell';
  // 2026-08-19 §德州扑克盲注透传 — texasholdem only。两字段必须同时设置或
  // 同时缺省;缺省后端用默认值 big_blind=200 / start_stack=10000。
  // big_blind ∈ {10,50,200,1000,5000};start_stack ∈ [20bb,100bb]。
  big_blind?: number;
  start_stack?: number;
}

/** One bot seat requested at room-creation time (werewolf only). */
export interface AgentSeatRequest {
  seat: number;       // 0..12 (13 人标准竞技局)
  model_key: string;  // e.g. "MeiTuan-model"
  // 2026-08-06 §20260806-03 自选角色(可选):
  // werewolf/seer/witch/hunter/idiot/guard/knight/demon_hunter/villager,
  // 或 'random'/缺省 = 随机。牌组组成不变,仅座位置换;牌组无此角色时降级随机。
  role?: string;
}

/** Extended game.over with settlement细节. Ante games fill these in; the
 *  backend is authoritative — the frontend only displays them. */
export interface XiangqiGameOverDetail extends XiangqiGameOver {
  ante?: number;
  streakBonus?: number;
  netGain?: number;
  finalBalance?: number;
}

export interface JunqiGameOverDetail {
  room_id: string;
  winner: 'red' | 'black' | '';
  reason: string;
  ante?: number;
  platformFee?: number;
  netGain?: number;
  finalBalance?: number;
}

/** 2026-07-17 金池结算 — 后端 per-user WS 推送 game.settlement 帧(payload)。
 * 玩家据此渲染 SettlementModal,展示本局底注/净收益/最终余额/结果。 */
export interface WerewolfSettlement {
  room_id: string;
  game_kind: string;
  winner: string;
  result: 'win' | 'lose' | 'draw';
  ante: number;
  netGain: number;
  finalBalance: number;
}
