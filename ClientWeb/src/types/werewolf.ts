/**
 * 狼人杀 13 人标准竞技局 客户端类型定义 (历史兼容 12/7 人局)
 *
 * 与后端 ServerGo/game/werewolf/view.go 对齐。
 * 2026-07-10: 升级到 13 人标准竞技局为默认人数(4 狼 + 4 神 + 5 民)—— 新增 RoleIdiot /
 * PhaseIdiotReveal / 警徽流 (SheriffStreams) / wolf_kill 空刀=[-1]。详见
 * docs/狼人杀13人标准局规则.md。
 * 2026-07-11: 12/7 人局保留为历史兼容 game_kind (werewolf_12 / werewolf_7)。
 */

import type { RoomInfo } from './api';

export type WerewolfRole =
  | 'werewolf'
  | 'seer'
  | 'witch'
  | 'hunter'
  | 'idiot'
  | 'villager'
  // 2026-07-11: 扩展神职角色(13人随机牌组池)
  | 'guard'
  // §198 骑士角色(2026-07-30 加入 godRolePool):白天决斗 — 命中狼则对方出局,
  // 否则骑士自决出。每局限一次,发动即亮身份。详见 docs/狼人杀骑士角色设计.md。
  | 'knight'
  // §猎魔人 猎魔人角色(2026-07-30 加入 godRolePool):第 2 晚起每晚狩猎 —
  // 命中狼则对方死亡(verdict=death),命中好人则自己出局(verdict=execution)。
  // 每晚可发动,发动即亮身份。详见 docs/狼人杀猎魔人角色设计.md。
  | 'demon_hunter'
  // ⚠️ 2026-07-29 已退役:无引擎/工具/美术实现,前端隐藏。保留字符串值作历史兼容。
  // | 'magician' | 'merchant' | 'dreamer' | 'crow'
  // | 'scarecrow' | 'prince' | 'pure_white'
  | 'unknown';

export type WerewolfPhase =
  | 'filling'
  | 'pre_wolves'
  // 2026-07-29 §134 守卫角色:在 pre_wolves 之后、night_wolves 之前
  // 插入「夜间守卫」阶段。守卫「盲守」—— 此时狼刀尚未产生,
  // 守卫无法看到当晚狼刀目标,必须靠推理预判。
  | 'night_guard'
  | 'night_wolves'
  | 'night_seer'
  | 'night_witch'
  | 'dawn'
  | 'sheriff'
  | 'speak'
  | 'vote'
  | 'idiot_reveal'
  | 'death_lyric'
  | 'hunter_shoot'
  // 2026-07-10: 一局结束后 5 分钟内投票决定是否原地重开。
  | 'restart_vote'
  | 'over';

export interface WerewolfPlayerJSON {
  user_id: string;
  seat: number;
  alive: boolean;
  is_sheriff: boolean;
  role_revealed: boolean;
  role?: string;
  faction?: 'wolf' | 'good' | 'unknown' | string;
  agent_name?: string;

  // 2026-08-05 §02 — 座位级「最后一次公开发言」,**人机统一**。
  // 与只覆盖 bot 座位的 bot_contexts[].last_speech* 不同,本字段由服务端在
  // 公开发言落库时按座位记录,因此真人玩家与 Agent 走同一条路径。
  // 私聊(whisper)不写入 —— 私聊原文只对收发双方可见,而本字段全房可见。
  // 座位卡气泡以本字段为「真人/兜底源」,与 Agent 源按时间戳择新(见 WerewolfTable)。
  /** 最后一次公开发言原文(服务端已截断 ≤200 rune)。 */
  last_speech?: string;
  /** 最后一次公开发言的 unix 毫秒时间戳。 */
  last_speech_at?: number;

  // §20260811-02 U2 — 补齐后端 view.go:50 已下发但前端从未声明的字段。
  // §20260807-04 P0-3 人类反制道具命中人类玩家时的 debuff 规格。
  /** 人类反制道具 debuff（命中人类玩家时非空）。 */
  human_debuff?: HumanDebuffSpec;
}

/**
 * §20260807-04 P0-3 — 人类反制道具 debuff 规格。
 * 对齐后端 `wwtypes.HumanDebuffSpec`。
 */
export interface HumanDebuffSpec {
  effect_type?: string;
  prop_key?: string;
  prop_name_zh?: string;
  expires_at?: number;
  payload?: string;
}

/**
 * §20260811-02 U1 — 发言影响力生态。
 * 对齐后端 `werewolf.InfluenceScore`。**全员可见**（非 spectator 专属）：
 * 分数完全由公开信息计算（票型 / 发言 / 被私聊或道具指向），不含任何角色信息。
 */
export interface InfluenceScoreJSON {
  seat: number;
  /** 综合影响力 0~100。 */
  total: number;
  /** 跟票率 0~40。 */
  persuasion: number;
  /** 关注度 0~25。 */
  attention: number;
  /** 发言参与 0~20。 */
  presence: number;
  /** 存活加成 0~15。 */
  survival: number;
  /** 洞察力 0~15（§20260812-02 U2）。 */
  insight: number;
}

/**
 * §20260811-02 U2 — 多假说并行推演单条假说（spectator 专属）。
 * 对齐后端 `werewolf.HypothesisEntryJSONForView`。
 */
export interface HypothesisEntryJSON {
  target_seat: number;
  /** werewolf|seer|witch|guard|villager|idiot|knight|hunter|demon_hunter|unknown */
  role_guess: string;
  /** 置信度 0~100。 */
  confidence: number;
  supporting: string;
  refuting: string;
  updated_at: number;
}

/**
 * §20260811-02 U2 — 一个 bot 座位的假说表（spectator 专属）。
 * 对齐后端 `werewolf.BotHypothesisJSON`。
 */
export interface BotHypothesisJSON {
  seat: number;
  entries: HypothesisEntryJSON[];
  round: number;
}

/**
 * §20260811-02 U2 — 决策留痕单条记录（spectator 专属）。
 * 对齐后端 `wwplayer.DecisionEntry`。
 */
export interface DecisionEntryJSON {
  round: number;
  phase: string;
  tool_name: string;
  tool_summary: string;
  took_ms: number;
  created_at: number;
}

// §20260811-06 U3 — 公开推理链(reasoning_chain 工具产出)。字段对齐
// ServerGo/agent/wwplayer/reasoning_chain.go::ReasoningChainEntry。
export interface ReasoningChainEntryJSON {
  round: number;
  phase: string;
  topic: string;
  steps: string[];
  evidence: string[];
  conclusion: string;
  confidence: number;
  created_at: number;
}

export interface WerewolfGameState {
  room_id: string;
  game_kind: 'werewolf';
  max_seat: number;
  seats: string[];
  players: WerewolfPlayerJSON[];

  my_seat: number;
  my_role?: string;
  my_faction?: string;
  /**
   * 2026-08-11 BUG-ROLE-MISMATCH-P0 — 「自选角色未生效」仅本人可见。
   * 创建房间时选了角色(如 hunter),但本局随机牌组未抽到该角色
   * (13 人局 2~3 张神职,骑士/守卫/猎魔人常缺席)或与其他座位偏好
   * 冲突时,偏好降级为随机。后端 ApplyPreferredRoles unmet 直接下发:
   *   - my_role_pref_unmet=true + my_preferred_role=想要的角色名
   *   - 本人座位已满足或无偏好 → 两字段不下发(undefined)
   */
  my_preferred_role?: string;
  my_role_pref_unmet?: boolean;
  phase: WerewolfPhase | string;
  day: number;
  status: 'playing' | 'over' | string;
  winner: string;

  turn_acting_seat: number;
  my_turn: boolean;

  speak_turn_seat: number;
  my_speak_turn: boolean;
  speak_order?: number[];

  day_eliminated: number;
  last_night_deaths: number[];
  suicided_wolf_seat: number;
  tied_players?: number[];

  seer_last_check?: number;
  witch_antidote_used?: boolean;
  witch_poison_used?: boolean;
  witch_wolf_target?: number;

  // 2026-07-29 §134 守卫角色 — 仅守卫本人视角有真值(§13 / 44 教训脱敏硬约束)。
  // 其他玩家视角一律为 -1。守卫永远看不到当晚狼刀目标(盲守),这两个字段
  // 是守卫跨夜连守校验(G1)的依据。
  /** 上晚守护目标座位号;-1 = 上晚空守 / 无连守限制。 */
  guard_last_protect?: number;
  /** 今晚已守目标座位号;-1 = 未守 / 空守。 */
  guard_protect_target?: number;

  hunter_pending?: boolean;
  sheriff_seat: number;

  // 2026-07-10 12 人局:警徽流(SheriffStreams [2])。预言家警长在夜间死亡后,
  // dawn 阶段服务端按 §7.3 自动结算并通过 game.sheriff_stream_settle 广播。
  sheriff_streams?: number[];

  // §报告-20260804-03 BUG-04:警长竞选已参选座位列表(全场可见)。
  // 仅 phase==='sheriff' 时下发;参选状态在服务端复用 Player.HasSpoken 存储,
  // 该字段是它唯一的对外通道 —— 缺失时玩家点「参选」后 UI 零变化。
  sheriff_candidates?: number[];
  // §报告-20260804-03 BUG-05:当前玩家自己的投票状态(仅入座玩家)。
  // votes(聚合票数)是全场视角,回答不了「我投过没 / 投给了谁」。
  my_voted?: boolean;
  /** 我投票的目标座位号;-1 = 未投票 / 弃票。 */
  my_vote_target?: number;
  // 2026-07-10 12 人局:已翻牌的白痴座位列表(翻牌后丧失投票权,全场公开)。
  idiot_revealed_seats?: number[];
  // 2026-07-10 12 人局:神职/平民/狼人屠边计数(对齐 view.go refreshCounts)。
  // 服务端可选下发;前端用此展示「屠边进度」,缺省时回退本地统计。
  divine_plain_wolf_alive?: { divine: number; plain: number; wolf: number };
  // 2026-07-10 §123 增强:后端实际下发字段(扁平三字段),与 divine_plain_wolf_alive 同义,
  // 渲染层优先读这一组。GameInfoPanel 已向下兼容。
  divine_alive?: number;
  plain_alive?: number;
  wolf_alive?: number;

  votes?: Record<string, number>;

  ready: boolean;
  filled: boolean;

  // 2026-07-24 优化:UI 暂停字段。
  // paused=true 时房间被真人玩家(房主)暂停 — 所有 bot 停止调 LLM,
  // 阶段时钟冻结,watchdog 不再强制 skip。GameInfoPanel 据此渲染
  // ⏸ 暂停 / ▶ 恢复按钮(仅房主可见可操作)。
  paused?: boolean;
  paused_by?: string;
  paused_reason?: string;

  // Agent 思考可见性：每个 bot 座位的最近记忆 / 工具调用 / 摘要。由
  // broadcastWerewolfState 附带下发，前端渲染为「Agent 思考」折叠面板。
  bot_contexts?: BotContextJSON[];

  // BUG Round 40 §95: 首夜强制发言阶段扩展字段(对齐 ServerGo/game/werewolf/view.go::PhaseExtraJSON)。
  // 仅 phase === 'pre_wolves' 时填充;其余阶段为 undefined(可安全 `?.` 访问)。
  phase_extra?: PhaseExtraJSON;

  // 2026-07-10 §123 增强 — 死亡语义扩展。
  // 昨晚死的完整信息(含 cause + verdict),前端按 verdict 分色徽章。
  last_night_deaths_verbose?: DeadPlayerJSON[];
  // 2026-07-11 R96-P1 增强 — 全部历史死亡列表(phase-agnostic)。
  // 不依赖 LastNightDeaths(每晚重置),始终包含 day1..current 全部死者。
  // 在 mergeDeadInfo 优先级中位于 last_night_deaths_verbose 之后,
  // 作为兜底:即使上一晚死者 empty,已死座位仍有 verdict 渲染。
  all_dead_list_verbose?: DeadPlayerJSON[];

  // 2026-07-10 §123 增强 — Agent 法官(主持人)上下文。
  // judge_enabled = false 时不渲染 JudgePanel;true 时渲染顶部法官宣告面板。
  judge_enabled?: boolean;
  judge_context?: JudgeContextJSON;
  judge_pending_announce?: string;
  judge_speak_order?: number[];
  // 2026-07-10 §125 增强 — 法官整局总结(挂在 judge_context.last_summary 同一字段,
  // 此处为顶层字段便于前端 ReadPanel 独立渲染)。
  judge_summary?: string;
  judge_model_memories?: Record<string, string[]>;

  // 2026-07-11: 预言家发起投票状态(全员可见)
  vote_proposed?: boolean; // 是否已由预言家发起投票
  vote_proposer?: number;  // 发起投票的座位号

  // 2026-07-18 §UX-运行时:整局开始 Unix 秒。0 表示尚未开局(filling 阶段)。
  // 前端 <RoomRunningClock> 据此渲染"已运行 X 分 Y 秒"。
  game_started_at?: number;

  // 2026-07-17: 狼人夜间投票视图(仅狼人玩家在 night_wolves 阶段可见)。
  // 携带投票快照 + 计票结果,前端据此渲染队友投票状态。
  wolf_vote_view?: WolfPeerView;

  // 2026-07-21 §13 道具系统 — 道具使用事件流(挂在 game.state 上,所有人都可见)。
  // 对齐 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §4.2 步骤 8:服务端广播道具使用公开事件。
  // 前端 <PropPanel> 监听此字段追加到最近事件流。
  prop_events?: PropUseEvent[];
  // 道具使用后,服务端下发的"我方最新金币余额"——客户端 store 不必每次 GET /api/games/werewolf/props。
  prop_my_balance?: number;
  // 道具使用后,服务端下发的"我方本局剩余次数"。
  prop_my_remaining?: number;
  // 道具使用后,服务端下发的"我方下次可用的剩余冷却秒数"。
  prop_cooldown_remaining_sec?: number;
  // §R183-P2-2:服务端可选下发的"房间经济档位"echo 字段 —— PropPanel 立即同步,
  // 无需等待 fetchProps。当前服务端未实现此字段透传;预留以便补发。
  prop_econ_tier?: 'boom' | 'health' | 'caution' | 'danger' | 'critical';
  // §R183-P2-2:服务端可选下发的"经济档位销毁率百分比"echo 字段(20/30/40/45/60)。
  prop_econ_tier_absorb_pct?: number;

  // 2026-07-30 §统计增强 — 房间级聚合 Agent + 法官 API/Token 统计。
  // 纯内存态，不进 DB，房间解散自动释放。前端 WerewolfStatusBar 据此渲染。
  agent_stats?: AgentRoomStats;

  // 2026-08-10 §20260810-05 — 信息账本(Information Ledger)观战者快照。
  // 仅 spectator 视图下发;玩家视图与 REST 房间视图不下发。
  // 一期「只写不读」:为二期前端「信息传播时序图」提供数据通道。
  // Fact 已在服务端写入侧剔除身份明文(§119/§135)。
  info_ledger?: InfoEntryJSON[];

  // 2026-08-10 §20260810-08 — 信息账本二期「说漏嘴检测」观战者快照。
  // 仅 spectator 视图下发;绝不进入 GameContext / prompt / chat 通道(§119/§135)。
  // 由后端 information_leak.go 懒计算 + seq 缓存;UI 文案必须写明「疑似 / 仅供复盘参考」
  // —— 这是复盘线索,不是裁决器,绝不用于惩罚/quarantine/扣分。
  info_leaks?: InfoLeakJSON[];

  // 2026-08-10 §20260810-06 — 行为承诺列表（按视角脱敏）。
  // 观战者可见全部真实状态；玩家仅见自己的承诺(含真实状态) + 他人的 pending。
  commitments?: CommitmentJSON[];

  // §20260810-09 — 上帝视角观战快照。**仅** spectator 视图下发；
  // 玩家侧永远 omitempty(§135 公平性 + §121 数据形状契约)。
  // 前端用 localStorage.ww_god_mode === "1" 控制是否渲染 —— 服务端**不**做开关。
  god_mode?: GodModeSnapshot;

  // §20260812-03 U1 — 阵营胜率热力图概率数组(13 长度,0..12 对应 1..13 号位)。
  // **仅** spectator 视图下发,玩家侧永远 omitempty(§132 隐私隔离 + §135 不含身份明文)。
  // 值为该座位"是狼人"的启发式概率(0.0~1.0),数据源:服务端
  // ServerGo/game/werewolf/win_predict.go computeWinRateProbabilityLocked。
  win_rate_probability?: number[];

  // §20260810-09 — 警长定序状态(全场可见,非上帝视角专属)。
  sheriff_order_set?: boolean;
  sheriff_speak_direction?: string; // "cw" / "ccw"
  sheriff_speak_self_pos?: string;  // "first" / "last"

  // §20260811-01 U3 — 投票阶段「半公开计票」悬念配置。
  /** true 时投票结束后延迟显示完整票型（仅显示谁投了谁）。 */
  vote_suspense?: boolean;
  /** 悬念持续毫秒数，默认 3000。 */
  vote_suspense_delay_ms?: number;

  // §20260811-02 U1 — 发言影响力生态（全员可见）。
  /** 全场每座位的公开影响力分数（0~100 + 4 个分项）。 */
  influence_scores?: InfluenceScoreJSON[];

  // §20260811-02 U2 — 补齐后端已下发但前端从未消费的 2 个 spectator 字段。
  /** 各 bot 的身份假说表（spectator 专属；后端 viewer>=0 时 omitempty）。 */
  bot_hypotheses?: BotHypothesisJSON[];
  /**
   * §20260814-01 U1 — 发言信任度轨迹（终局后由法官整局总结解析）。
   *
   * 对齐后端 `werewolf.TrustTraceEntryJSON`。§135：每条只有
   * `{seat, day, score}`，**不含**身份字段，故对全员下发（非 spectator-only）。
   * 对局中后端 omitempty 不下发 → 前端 TrustTraceChart 渲染空态。
   */
  trust_trace?: Array<{ seat: number; day: number; score: number }>;
  /** 死者身份终局延时揭晓分钟数（0/5/15；0 时 omitempty）。 */
  death_reveal_delay_min?: number;
  /** §20260811-09 U2 — Agent 难度档位（easy/normal/hard/hell；normal 时 omitempty）。 */
  agent_difficulty?: 'easy' | 'normal' | 'hard' | 'hell';
  /** §20260811-09 U1 — AI 实时解说 feed（spectator 专属；最近 20 条 + seq 单调递增）。 */
  commentary_feed?: CommentaryLineJSON[];
  /**
   * §20260817-03 U1 — AI 解说是否开启（房间级开关，后端不带 omitempty 恒下发）。
   * 观战视图为真实值；玩家视图恒 false（解说仅观众可见，§119）。
   * SpectatorCompactBar 据此决定是否渲染解说席：false 且无押注交互时整个
   * 底栏不渲染，把垂直空间让给座位网格。
   */
  commentary_enabled?: boolean;
}

/** §20260811-09 U1 — 单条解说载荷（来自后端 ws.Envelope "chat.commentary" + cs.commentary_feed）。 */
export interface CommentaryLineJSON {
  seq: number;
  text: string;
  style: 'pro' | 'fun';
  model_key?: string;
  kind: string;
  ts_ms: number;
}

/** §20260810-09 — 上帝视角观战快照(spectator 专属)。 */
export interface GodModeSnapshot {
  enabled: boolean;
  roles: Record<number, string>;
  factions: Record<number, string>;
  wolf_kill_target: number; // -1 = 无/已守/已救
  wolf_votes: Record<number, number>;
  seer_checks: SeerCheckEntry[];
  witch_decisions: WitchDecision[];
  guard_protects: number[];
  /** §20260813-02 U4 — 狼刀历史(每夜最终刀口,夜间血迹图 S2)。 */
  wolf_kills?: WolfKillEntry[];
  /** §20260813-02 U4 — 守卫守护结构版(Day+Seat+Target,血迹图渲染)。 */
  guard_protect_entries?: GuardProtectEntry[];
  // §20260810-11 V1 — PerSeatPOV(spectator 视角切换面板)
  per_seat_pov?: Record<number, PerSeatPOV>;
  // §20260811-08 U3 — 已公开的技能行动(猎人开枪/骑士决斗/猎魔人狩猎/白痴翻牌)。
  public_actions?: PublicActionEntry[];
}

/** §20260813-02 U4 — 单夜狼队最终刀口(夜间血迹图 S2)。 */
export interface WolfKillEntry {
  day: number;
  target: number;
}

/** §20260813-02 U4 — 守卫守护结构版条目(夜间血迹图 S2)。 */
export interface GuardProtectEntry {
  day: number;
  seat: number;
  target: number;
}

/**
 * §20260811-08 U3 — 已公开的技能行动条目(spectator 上帝视角)。
 *
 * 这 4 类事件属 §135 身份公开白名单,本就全房可见,聚合它们不构成
 * 新的身份下发通道。
 */
export interface PublicActionEntry {
  day: number;
  kind: 'hunter_shot' | 'knight_duel' | 'demon_hunter' | 'idiot_reveal';
  seat: number;
  /** 目标座位;-1 = 无(如白痴翻牌)。 */
  target: number;
  /** 仅决斗/狩猎有意义;undefined = 不适用,与 false「没打中狼」语义不同。 */
  hit_wolf?: boolean;
}

// §20260810-11 V1 — 单座位的「第一视角」快照(spectator 可见)
export interface PerSeatPOV {
  role: string;
  role_revealed: boolean;
  faction: string;
  heart_thought: string;
  last_decision: string;
  night_actions: string[];
  tool_call_count: number;
  llm_call_count: number;
  last_emotion: string;
  challenge_count: number;
  public_commitments: string[];
}

export interface SeerCheckEntry {
  day: number;
  seat: number;
  target: number;
  result: 'good' | 'werewolf';
}

export interface WitchDecision {
  day: number;
  seat: number;
  antidote_use: number; // -1 = 未用
  poison_use: number;   // -1 = 未用
}

/** 2026-08-10 §20260810-06 — 承诺条目（按视角脱敏后下发）。 */
export interface CommitmentJSON {
  id: number;
  seat: number;
  round: number;
  template: string; // seer_check / vote_target / no_vote_for / no_use_skill / apology_if_good
  param_seat: number;
  param_text: string;
  reason: string;
  status: string; // pending / fulfilled / broken / expired
  created_at: number;
}

/** 2026-08-10 §20260810-05 — 信息账本条目(观战者快照投影)。 */
export interface InfoEntryJSON {
  seq: number;
  round: number;
  phase: string;
  source: string;         // public_speech / whisper / wolf_pack / night_* / day_vote_map / ...
  fact: string;           // 已脱敏(不含身份明文)
  knower_seats: number[]; // 有序 0-indexed 座位数组
  ts: number;             // UnixMilli
}

/** 2026-08-10 §20260810-08 — 「疑似说漏嘴」观战者检测条目(信息账本二期)。
 *  触发场景:某座位的公开发言引用了只可能在私密来源出现的座位号。
 *  仅 spectator 视图下发;绝不下发给对局内玩家(含 bot),避免污染博弈(§135)。
 *  UI 文案必须标注「疑似 / 仅供复盘参考」——这是观战侧复盘线索,不是裁决器。 */
export interface InfoLeakJSON {
  /** 触发检测的公开发言条目 Seq(对应 info_ledger 中的一条 public_speech)。 */
  seq: number;
  /** 疑似说漏嘴的座位(0-indexed)。 */
  seat: number;
  /** 发言所在天(0-indexed round)。 */
  round: number;
  /** 发言所在阶段。 */
  phase: string;
  /** 被提及、且仅私密可知的座位号(0-indexed)。 */
  hint_seat: number;
  /** 该私密信息的来源(InfoSource 取值之一,如 wolf_pack / whisper / night_seer 等)。 */
  from_source: string;
  /** 公开发言摘录(≤60 rune)。 */
  excerpt: string;
}

/** 2026-07-30 §统计增强 — 房间级聚合 Agent + 法官 API/Token 统计。 */
export interface AgentRoomStats {
  total_input_tokens: number;
  total_output_tokens: number;
  total_api_tokens: number;
  api_call_count: number;
  api_success_count: number;
  api_fail_count: number;
  agent_count: number;
  judge_enabled: boolean;
  judge_total_input_tokens: number;
  judge_total_output_tokens: number;
  judge_total_api_tokens: number;
  judge_api_call_count: number;
  judge_api_success_count: number;
  judge_api_fail_count: number;
}

/** 2026-07-17 狼人夜间投票视图(对齐 ServerGo/game/werewolf/view.go::WolfPeerView)。 */
export interface WolfPeerView {
  /** 存活狼人座位(0-indexed)。 */
  wolf_seats: number[];
  /** 最终击杀目标 seat(计票后);-1=未决。 */
  kill_target: number;
  /** seat -> target 投票快照(已投票的狼人)。 */
  votes: Record<number, number>;
  /** 已弃权(含未投票)的狼人座位。 */
  abstain: number[];
  /** 已提交投票(含弃权)的狼人数。 */
  votes_cast: number;
  /** 存活狼总数。 */
  total_wolves: number;
  /** true=投票中;false=已结算。 */
  voting: boolean;
  /** 计票结果(voting=false 时填充)。 */
  tally?: WolfVoteTally;
}

/** 2026-07-17 狼人夜间投票计票结果。 */
export interface WolfVoteTally {
  /** target seat -> 得票数。 */
  counts: Record<number, number>;
  /** 最高票并列目标。 */
  tied: number[];
  /** "majority" | "random_tie_break" | "random_all_abstain"。 */
  reason: string;
  /** 最终击杀目标 seat。 */
  final: number;
}

/** 2026-07-16 主持人 Agent 重构 — 法官一举一动活动流单元(对齐 agent.JudgeActivity)。 */
export interface JudgeActivityJSON {
  /** 毫秒时间戳。 */
  at: number;
  /** 工具名(announce / prompt_actor / summary / declare_cause / idle_silent)。 */
  tool: string;
  /** 参数摘要(≤120 字符)。 */
  input: string;
  /** 产物文本。 */
  out: string;
  /** 本次 LLM 耗时(毫秒,可选)。 */
  llm_ms?: number;
}

/** 2026-07-10 §123 增强 — Agent 法官上下文(对齐 agent.JudgeTranscript)。 */
export interface JudgeContextJSON {
  model: string;
  last_announcement?: string;
  last_tool?: string;
  recent_announcements?: string[];
  tool_calls?: string[];
  last_updated_at: number;
  quarantined?: boolean;
  quarantine_reason?: string;
  // 2026-07-10 §125 增强 — 整局总结字段。
  last_summary?: string;
  last_summary_at?: number;
  last_summary_sections?: JudgeSummarySectionsJSON;
  // 2026-07-16 主持人 Agent 重构 — 一举一动活动流(最近 30 条)+ 最近一次 LLM 耗时。
  activities?: JudgeActivityJSON[];
  last_llm_ms?: number;
}

/** 2026-07-10 §125 增强 — 法官 5 段总结的结构化字段。 */
export interface JudgeSummarySectionsJSON {
  outcome: string;
  turning_point: string;
  role_timeline: string;
  mvp: string;
  wolf_deception: string;
  generated_at: number;
  model?: string;
}

/** BUG Round 40 §95:首夜强制发言阶段扩展视图。 */
export interface PhaseExtraJSON {
  /** 总轮数(每名玩家至少发言次数,默认 1,clamp 1-3)。 */
  rounds_total: number;
  /** 当前轮 (0-based)。 */
  rounds_current: number;
  /** 每座位已发言次数(全 12 座位,公开信息)。 */
  speak_count_per_seat: number[];
  /** 缓冲期剩余秒数(< 0 表示已超时)。 */
  grace_remaining_sec: number;
  /** 缓冲期截止时间(RFC3339,前端可做进度条)。 */
  deadline_at?: string;
  // 2026-07-09 §13 增强 — 时钟机制(所有阶段)
  /** 阶段截止时间(RFC3339);所有阶段都填充(零值除外)。PhaseClock 组件用。 */
  phase_deadline_at?: string;
  /** 阶段剩余秒数(> 0 = 未到;= 0 = 到点;< 0 = 逾期)。 */
  remaining_sec?: number;
  // 2026-07-09: 遗言阶段进度 + 死亡列表(对齐 view.go DeathLyricExtra / DeadList)
  death_lyric?: DeathLyricExtra;
  dead_list?: DeadPlayerJSON[];
  // 2026-07-10: 重开局投票扩展(对齐 view.go RestartVoteExtra)。
  // 仅 phase === 'restart_vote' 时填充;其他阶段 undefined。
  restart_vote?: RestartVoteExtra;
  // 2026-07-21 §人类玩家操作重构 — 「轮到我了」专属标记 + 倒计时
  // (对齐 view.go PhaseExtraJSON.MyTurnNow / MyTurnRemainingSec)。
  // 仅当房间有人类且 viewer 入座时填充;全 AI 房间永远 false/undefined。
  // 前端 <MyTurnIndicator> 组件据此渲染"轮到 #N 行动" + 红黄倒计时。
  my_turn_now?: boolean;
  /** 轮到我的剩余秒数;仅当 my_turn_now=true 时填。 */
  my_turn_remaining_sec?: number;
}

/** 2026-07-10: 重开局投票阶段扩展视图。 */
export interface RestartVoteExtra {
  deadline_at?: string;
  remaining_sec: number;
  yes: number[];
  no: number[];
  abstain: number[];
  decided: boolean;
  result?: 'passed' | 'rejected' | 'timeout';
  winner?: 'wolf' | 'good' | '';
  eligible_count: number;
  yes_quota: number;
  my_choice?: 'yes' | 'no' | 'abstain';
}

/** 2026-07-09: 遗言阶段扩展(对齐 view.go DeathLyricExtra)。 */
export interface DeathLyricExtra {
  current_seat: number;
  total: number;
  done: number;
}

/** 2026-07-09: 已死亡玩家遗言状态(对齐 view.go DeadPlayerJSON)。 */
export interface DeadPlayerJSON {
  seat: number;
  account: string;
  role: string;
  last_words_status: 'spoken' | 'skipped' | 'pending' | 'ineligible';
  cause: string;
  // 2026-07-10 §123: 处决 / 死亡 决断。前端按 verdict 分色徽章。
  verdict?: 'execution' | 'death' | string;
  day: number;
}

// BotContextJSON：game.state.bot_contexts[] 单元，对齐 ServerGo/game/werewolf/view.go。
// §128 对话即思考重构:LastThinking / FullThinking / RecentMessages 字段已从后端删除。
export interface BotContextJSON {
  seat: number;
  model: string;
  /** 工具名(与 last_tool 重复保留,新前端使用 last_tool)。 */
  last_tool: string;
  /** 兼容字段,保留 tool_calls 用于决策可观测。 */
  tool_calls: string[];
  updated_at: number;
  // BUG-WEREWOLF-P1-NEW-46 (Round 39): 当 bot 被永久 quarantine (5+ 次
  // 连续 LLM 失败) 后,服务端会在 transcript 里置 true 并附上最后一次错误。
  // 前端 AgentInteractionPanel 渲染"已禁用 / 5连失败"徽章,而非保持空白。
  quarantined?: boolean;
  quarantine_reason?: string;

  // §128 对话即思考重构 - "对话即思考":5 字段决策可观测,完全替代旧 CoT 展示。
  /** 一句话总结本次决策(如 "speak(3号):我怀疑4号是狼人" / "vote → 2号" / "idle (沉默)")。
   *  ≤ 50 字,空表示本轮无有效决策。供 AgentInteractionPanel 主区展示。 */
  last_decision_summary?: string;
  /** 工具入参的 JSON 字符串(经敏感表脱敏,如 wolf_kill.target 字段为 [已隐藏])。
   *  供观众/玩家看到"LLM 决定调什么工具 + 入参是什么"。 */
  last_tool_input?: string;
  /** 工具结果的前 80 字截断。供观众/玩家看到"工具调用结果"。 */
  last_tool_result?: string;
  /** 决策结果分类:OK / FAIL / skip / idle / quarantine;
   *  供 AgentInteractionPanel 决策输出区右侧徽章展示。 */
  last_outcome?: string;
  /** 决策输入摘要(阶段 / 角色 / 轮数 / 存活 / 收到发言数 / 收到 whisper 数 / 500K 队列增量),
   *  ≤ 200 字,无 CoT 文本。 */
  decision_inputs?: string;

  // 2026-07-09 §13 增强 — 500K 聊天历史队列统计
  /** 当前 chat_history 队列字节数(展示「500K 队列: 234KB / 500KB」)。 */
  chat_history_bytes?: number;
  /** chat_history 容量上限(默认 500K)。 */
  chat_history_cap?: number;
  /** 上次 chat_history 压缩的 unix millis;0 = 未压缩。 */
  last_compression_at?: number;
  /** 60s 窗口内 speak 累计(供 summary 行「最近 60s 发言 N 次」)。 */
  speak_count_last_min?: number;
  // 2026-07-09 §13 增强 - LLM 调用实时指示器(跨端契约,字段名与后端 view.go 完全一致)。
  // 当 bot 正在调用大模型时后端置 true;前端用此字段 + started_at 渲染"正在调用…已等待 N 秒"。
  /** 该 bot 当前是否正在调用大模型。 */
  llm_call_in_progress?: boolean;
  /** 本次 LLM 调用开始的 unix 毫秒时间戳(用于前端倒计时)。 */
  llm_call_started_at?: number; // unix ms

  // 2026-07-10 §119 增强 — 「心口不一」机制 (speak_with_thought 工具)。
  // HeartThought 是 LLM 调用 speak_with_thought 时填的 internal_thought,
  // 真实内心独白(狼人悍跳、好人装身份等欺骗场景的「真相」);
  // 仅 BotTranscript 上持久化,**绝不**进 chat_message / chat_history。
  // 前端 AgentInteractionPanel 据此渲染 💭 内心独白面板;其他真人玩家
  // 通过 chat_message 拿不到此字段(协议层隔离)。
  /** 内心独白(LLM 真实想法),仅 BotTranscript + 观战者可见。 */
  heart_thought?: string;
  /** 内心独白写入的 unix 毫秒时间戳。 */
  heart_thought_at?: number;

  // 2026-08-05 §Agent聊天显示优化 — 最后一次公开发言(座位卡实时气泡)。
  // 对齐 ServerGo/agent/agent.go BotTranscript.LastSpeech* 4 字段。
  //
  // **与 heart_thought 的语义区别(必须分清,否则会误泄漏)**:
  //   - heart_thought  = 「没说出口的」内心独白,§119/§128 协议层隔离,
  //                      **绝不**进 chat_message / chat_history,仅观战者面板可见;
  //   - last_speech    = 「已经广播给所有人的」公开发言原文,公屏上本就看得到,
  //                      因此**无需**任何 spectator 守卫,所有人可见。
  //
  // 承载范围:speak / speak_with_thought 的 public text / emotion_switch_speak /
  // SpeakAuto / interject / last_words。
  // whisper 只记 kind + 时间,**不记原文**(私聊原文只对收发双方可见);
  // wolf_whisper **完全不记**(§133 狼队协议层隔离)。
  /** 最后一次公开发言原文(服务端 ≤200 rune 截断);whisper 时为空。 */
  last_speech?: string;
  /** 该发言广播成功的 unix 毫秒时间戳(前端据此算相对时间 + 3s 新发言高亮)。 */
  last_speech_at?: number;
  /** 发言来源种类:speak | emotion_speak | interject | whisper | last_words。 */
  last_speech_kind?: string;
  /** 发言发生时的天数(0 = 夜间 / 不分)。 */
  last_speech_round?: number;

  // 2026-07-10 §120 增强 — API 调用耗时统计(公平性机制可见性数据)。
  /** 最近一次 LLM 调用耗时(毫秒),前端展示 "上次 X.X s"。 */
  last_llm_latency_ms?: number;
  /** 本局累计指数加权滑动平均(α=0.3),前端展示 "平均 Y.Y s"。 */
  avg_llm_latency_ms?: number;
  /** 本局累计 LLM 调用次数,前端展示 "已调 N 次"。 */
  total_llm_calls?: number;

  // 2026-07-30 §统计增强 — Token + API 统计（纯内存态，不进 DB）。
  /** 最近一次 LLM 调用的 input tokens(0 表示命中缓存)。 */
  last_input_tokens?: number;
  /** 最近一次 LLM 调用的 output tokens。 */
  last_output_tokens?: number;
  /** 最近一次 LLM 调用的 input+output tokens。 */
  last_api_tokens?: number;
  /** 本局累计 input tokens。 */
  total_input_tokens?: number;
  /** 本局累计 output tokens。 */
  total_output_tokens?: number;
  /** 本局累计 input+output tokens。 */
  total_api_tokens?: number;
  /** 本局累计 API 调用次数(含失败)。 */
  api_call_count?: number;
  /** 本局累计成功次数。 */
  api_success_count?: number;
  /** 本局累计失败次数。 */
  api_fail_count?: number;
  /** 最近一次 LLM 调用的 unix 毫秒时间戳(0 表示从未调用)。对齐后端
   *  BotTranscript.LastLLMCallAtMs(json:"last_llm_call_at_ms")。前端据此渲染
   *  「Xs 前」相对时间 + 实时脉冲(BotCallTimeBadge)。 */
  last_llm_call_at_ms?: number;

  // 2026-07-10 12 人局 — 警徽流 + 白痴翻牌(对齐 view.go)。
  /** bot 座位是否为当前警长(映射 GameContext.SheriffSeat==seat),渲染 ★ 徽章。 */
  is_sheriff?: boolean;
  /** 警徽流目标(长度 2;-1 表示未声明/槽位空)。预言家警长本人可见他人不可见,
   *  但 BotTranscript 为观战者/调试暴露摘要,前端仅笼统展示「已声明 N 段」。 */
  sheriff_stream?: number[];
  /** bot 白痴是否已翻牌(丧失投票权),渲染 🃏 徽章。 */
  idiot_revealed?: boolean;

  // 2026-07-10 §124 增强 — 情绪模块字段(对齐 agent.BotTranscript JSON tags)。
  /** 当前 bot 情绪 key (confident/excited/calm/panic/wary/irritated/
   *  grievance/confused/guilty/tired)。**走 wire 层公开**,真人玩家 + 其它 Agent + 观众
   *  都能看到(与 heart_thought 协议层隔离形成对照)。 */
  emotion?: string;
  /** 情绪切换原因(LLM 在 emotion_switch.reason 给出,≤80 字)。 */
  emotion_reason?: string;
  /** 情绪最近一次切换的 unix 毫秒时间戳(供前端「刚切换」高亮动画)。 */
  emotion_updated_at?: number;
  /** 最近 5 次情绪切换历史(供前端展示「情绪曲线」tooltip)。 */
  emotion_history?: Array<{
    emotion: string;
    reason: string;
    at_ms: number;
  }>;

  // 2026-08-04 §表情特效(设计文档 20260804-02)— emotion_switch_speak 扩展参数下发。
  // 全部 omitempty,旧客户端零感知;前端 SeatCell EmotionAvatar 据此渲染特效层。
  /** 特效种类: pulse/shake/sweat/rage/tears/spin_question/glow/drowsy。 */
  emotion_effect?: string;
  /** 特效强度: low/mid/high(默认 mid)。 */
  emotion_intensity?: string;
  /** ≤20 字表情文字气泡(仅 bot_contexts,不进公屏/chat 表 — §119 协议层隔离)。 */
  emotion_caption?: string;
  /** 特效开始 unix ms。 */
  emotion_fx_started_at_ms?: number;
  /** 特效持续 ms(clamp 8–30s,默认 12000)。 */
  emotion_fx_duration_ms?: number;

  // 2026-07-10 §123 增强 — 最近死亡事件(由 Room.appendRoomMessageLocked 同步填充)。
  /** 最近一次该 bot 观测到的死亡事件决断:execution(处决)/ death(死亡)。 */
  last_death_verdict?: string;
  /** 配合 last_death_verdict:wolf/vote/hunter/witch_poison/suicide。 */
  last_death_cause?: string;
  /** 最近一次死亡的座位号(0-indexed)。 */
  last_death_seat?: number;
  /** 最近一次死亡发生在第几轮白天(0 = 夜间 / 不分)。 */
  last_death_round?: number;

  // 2026-07-10 §重构 — LLM 调用相位状态机,驱动前端 BotPhaseIndicator 多态指示器。
  // 5 态:idle / calling / streaming / retrying / quarantined。
  // 之前只能从 llm_call_in_progress 二态推断;现在 retry loop / 退避等待 / 永久禁用
  // 等中间状态都能在 BotTranscript 上呈现,前端据此渲染不同徽章 + 文案。
  //
  // 状态转移参考 ServerGo/agent/agent.go 的 SetLLMCallPhase 调用点:
  //   safety-net / limiter / semaphore → retrying
  //   MarkLLMCallStart                  → calling
  //   retry loop 入口                    → retrying
  //   retry loop 内 backoff              → calling (重试新一轮)
  //   MarkLLMCallEnd 成功                → idle (经 ResetConsecutiveFailures)
  //   SetQuarantined                     → quarantined
  /** LLM 调用所处阶段(对齐 ServerGo/agent.PhaseIdle/PhaseCalling/PhaseStreaming/PhaseRetrying/PhaseQuarantined)。 */
  llm_call_phase?: 'idle' | 'calling' | 'streaming' | 'retrying' | 'quarantined';
  /** 当前 retry 轮次(1-based,0=首次调用未在 retry loop)。 */
  retry_attempt?: number;
  /** retry 最大次数(默认 llmMaxRetries=1,业务上展示 N/M 时 +1)。 */
  retry_max_attempts?: number;
  /** 下一次 retry 的 unix 毫秒时间戳(仅 retrying 时有效,前端可计算倒计时)。 */
  next_retry_at_ms?: number;
  /** 上次失败分类:none / 5xx / 429 / timeout / permanent / queued / throttled。 */
  last_error_class?: 'none' | '5xx' | '429' | 'timeout' | 'permanent' | 'queued' | 'throttled';
  /** 2026-07-12 §127 — quarantine 时向所有玩家广播的系统提示(一次性)。 */
  quarantine_broadcast?: string;

  // §20260811-02 U2 — 补齐后端 agent.go:651 已下发但前端从未消费的字段。
  // spectator 专属:后端 sanitizeBotTranscript 在人类玩家分支清空(§135)。
  /** 本局决策留痕(最多 30 条 FIFO;werewolf.bots_log_decisions=false 时为空)。 */
  decision_trail?: DecisionEntryJSON[];
  // §20260811-06 U3 — 公开推理链(spectator only,后端 room_state.go 已清空玩家分支)。
  reasoning_chains?: ReasoningChainEntryJSON[];
}

// 2026-08-05 §02 — 「发言流」的前端类型与 game.state 字段已一并删除:
// 服务端不再向前端投影发言流(wire 层同步移除),前端发言展示统一走
// 「房间聊天」+「座位卡气泡(players[].last_speech*)」两条职责正交的通道。
// 注意:服务端侧供 Agent prompt 与法官总结使用的发言缓冲**保留**,勿混淆。

export type WerewolfAction =
  | 'wolf_kill'
  | 'seer_check'
  | 'witch_act'
  // 2026-07-29 §134 守卫角色 — 守卫夜间守护动作,复用 game.werewolf_action 帧
  // 携带 { room_id, action: 'guard_protect', target: <seat|-1> }。
  | 'guard_protect'
  // §198 骑士角色 — 白天决斗动作,复用 game.werewolf_action 帧
  // 携带 { room_id, action: 'knight_duel', target: <seat|-1> }。
  // target=-1 表示本轮放弃不消耗机会(LLM 主动选择)。
  | 'knight_duel'
  // §猎魔人 猎魔人角色 — 夜间狩猎动作,复用 game.werewolf_action 帧
  // 携带 { room_id, action: 'demon_hunter_hunt', target: <seat|-1> }。
  // target=-1 表示空过(LLM 主动选择)。
  | 'demon_hunter_hunt'
  | 'vote'
  | 'suicide'
  | 'shoot'
  | 'sheriff_candidate'
  | 'sheriff_vote'
  | 'sheriff_elect'
  | 'sheriff_stream'
  | 'idiot_reveal'
  | 'speak_finish'
  | 'start_day';

export type WerewolfStyle = 'dark_medieval';

/** Lobby 房间视图(精简版,用于列表)。*/
export type WerewolfRoomInfo = RoomInfo & { game_kind: 'werewolf' };

// ============================================================================
// 2026-07-21 §13 道具系统 — TS 类型契约(对齐 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §6)
//
// 字段命名遵循 §121 教训:后端 wrapper 形状与前端类型严格对齐。
// ============================================================================

/**
 * 道具 prop_key 枚举(对齐 ServerGo/game/werewolf/prop_catalog.go 的 10 种道具)。
 *
 * §20260810-02 E3:补齐 4 个后端已注册但 TS 未声明的道具 —— task_disguise_v3
 * (§133 v3 强化版) + 3 个 TargetCamp:"human" 人类反制道具(§20260807-04)。
 * 此前 useWerewolf.ts 的 `as WerewolfPropKey` 断言对这 4 个取值是失真的:
 * 后端推送 md_bomb_human 时,运行时值根本不在联合类型取值域内。
 *
 * 新增道具时必须同步此处 —— 规范清单见 ServerGo/game/werewolf/prop_test.go。
 */
export type WerewolfPropKey =
  | 'markdown_bomb'
  | 'nested_maze'
  | 'char_confuse'
  | 'long_swear'
  | 'task_disguise'
  | 'task_disguise_v3'
  | 'emotion_plea'
  | 'md_bomb_human'
  | 'nested_maze_human'
  | 'char_confuse_human';

/** 单个道具条目(对齐 ServerGo/models/t_lsm_game_prop.go)。 */
export interface WerewolfProp {
  /** 数据库 UUID。 */
  id: string;
  /** 道具枚举 key(对齐 WerewolfPropKey)。 */
  prop_key: WerewolfPropKey;
  /** 中文名(对应 ServerGo prop_catalog.name_zh)。 */
  name_zh: string;
  /** 英文名。 */
  name_en: string;
  /** 日文名。 */
  name_ja: string;
  /** 道具 emoji(后端从 prop_catalog 同步;前端仅在缺失时回退本地映射)。 */
  prop_emoji: string;
  /** 道具价格(金币)。 */
  price: number;
  /** 基础中招率(百分比整数,例如 30 表示 30%)。 */
  base_hit_rate: number;
  /** 是否范围(AOE)效果。true 时忽略 target_seat。 */
  is_aoe: boolean;
  /** 目标阵营限制:'any' | 'wolf' | 'good'。 */
  target_camp: 'any' | 'wolf' | 'good' | string;
  /** 道具描述(后端按 Accept-Language 或语言字段选语种)。 */
  description: string;
  /** 是否启用(false 时前端禁用按钮)。 */
  enabled?: boolean;
}

/**
 * GET /api/games/werewolf/props 的 data 形状。
 *
 * §121 教训:后端 wrapper 直接展开 data 给前端类型 — 此处类型必须包含 wrapper。
 */
export interface PropListResponse {
  /** 可用道具目录(可能为空,例如管理员全部禁用)。 */
  props: WerewolfProp[];
  /** 当前用户的金币余额(从钱包 service 同步)。 */
  my_balance: number;
  /** 当前用户本局剩余可购买次数(默认 3)。 */
  my_props_remaining: number;
  /** 冷却剩余秒数;0=可立即使用。 */
  cooldown_remaining_sec: number;
  /** v5 EconTier 当前档位(后端 ComputeEconTier 输出)。可选字段,缺省按 health 处理。 */
  econ_tier?: 'boom' | 'health' | 'caution' | 'danger' | 'critical';
  /** v5 EconTier 当前销毁率百分比(20/30/40/45/60)。 */
  econ_tier_absorb_pct?: number;
}

/**
 * 道具使用公开事件(挂在 game.state.prop_events[] / 单独的
 * game.werewolf_prop_used WS 帧上)。
 * 对齐 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §4.2 步骤 8。
 */
export interface PropUseEvent {
  /** 使用者座位(0-indexed)。 */
  from_seat: number;
  /** 使用者账号/agent 名(供 UI 展示,可空)。 */
  from_account?: string;
  /** 道具 key。 */
  prop_key: WerewolfPropKey;
  /** 道具显示名(后端按当前用户语言选 zh/en/ja)。 */
  prop_name: string;
  /** 道具 emoji。 */
  prop_emoji: string;
  /** 目标座位;AOE 时为 -1。 */
  target_seat: number;
  /** 实际支付金币(可能与目录价格不同——后端允许管理员调价)。 */
  price_paid: number;
  /** 是否中招(true 时附带 effect_text)。 */
  hit: boolean;
  /** 效果摘要(≤200 字,后端从 prop_usage_log 同步)。 */
  effect_text?: string;
  /** 使用时的阶段(如 'speak')。 */
  phase?: string;
  /** 发生时间 unix 毫秒。 */
  at: number;
}
