/**
 * 辩论比赛 客户端 TypeScript 类型定义 (2026-08-31 §20260831-01)
 *
 * 与后端 ServerGo/game/debate/{types.go,view.go} 对齐。
 * 6 0 玩家规则:人类仅以房主 / 观战者身份参与;辩手与裁判均为 Agent Bot。
 */

export type DebatePhase =
  | 'filling'
  | 'preparation'
  | 'opening_argument'
  | 'rebuttal'
  | 'cross_examination'
  | 'cross_exam_summary'
  | 'free_debate'
  | 'closing_argument'
  | 'judging'
  | 'result'
  | 'game_over';

export type DebateMode = 'two_team' | 'three_team' | 'four_team' | 'five_team';

export type DebateStance =
  | 'pro'
  | 'con'
  | 'neutral'
  | 'gov_upper'
  | 'gov_lower'
  | 'opp_upper'
  | 'opp_lower'
  | 'angle_1'
  | 'angle_2'
  | 'angle_3'
  | 'angle_4'
  | 'angle_5';

export type DebateRole = 'first' | 'second' | 'third' | 'fourth';

export interface DebateTopic {
  id: string;
  text: string;
  type: string;
  category?: string;
  pro_position?: string;
  con_position?: string;
  background?: string;
  keywords?: string[];
  difficulty?: number;
  is_official?: boolean;
}

export interface DebateAgentConfig {
  seat_id: number;
  role: DebateRole;
  role_name?: string;
  model_key?: string;
  bot_user_id?: string;
  name?: string;
}

export interface DebateTeamConfig {
  team_id: number;
  stance: DebateStance;
  stance_label?: string;
  agents: DebateAgentConfig[];
}

export interface DebateJudgeConfig {
  judge_id: number;
  model_key?: string;
  bot_user_id?: string;
  name?: string;
}

export interface DebatePhaseConfig {
  preparation_sec: number;
  opening_argument_sec: number;
  rebuttal_sec: number;
  cross_exam_sec: number;
  cross_exam_summary_sec: number;
  free_debate_sec: number;
  closing_argument_sec: number;
  judging_sec: number;
  result_show_sec: number;
  max_speech_chars: number;
  max_rebuttal_chars: number;
  max_cross_exam_q_chars: number;
  max_cross_exam_a_chars: number;
  max_free_debate_chars: number;
  max_closing_chars: number;
}

export interface DebateSpectatorConfig {
  allow_chat: boolean;
  reveal_agent_thought: boolean;
  allow_spectator_question: boolean;
  show_score_realtime: boolean;
  show_model_name: boolean;
}

export interface DebateSpeech {
  id: string;
  phase: DebatePhase;
  team_id: number;
  seat: number;
  speaker_name: string;
  stance: DebateStance;
  role: DebateRole;
  content: string;
  word_count: number;
  duration_sec?: number;
  timestamp: number;
  references?: string[];
  internal_thought?: string;
  model_key?: string;
}

export interface DebateCrossExamEntry {
  id: string;
  questioner: string;
  answerer?: string;
  question?: string;
  answer?: string;
  is_answer: boolean;
  timestamp: number;
}

export interface DebateScoreDimensions {
  argument_quality: number;
  logic_rigor: number;
  language_expression: number;
  team_coordination: number;
  rebuttal_effectiveness: number;
}

export interface DebateTeamRanking {
  team_id: number;
  scores: DebateScoreDimensions;
  total_score: number;
  comment: string;
  best_debater: number;
}

export interface DebateJudgeScore {
  judge_id: number;
  model_key: string;
  rankings: DebateTeamRanking[];
  overall_comment: string;
  /** §20260831-11(P2-B 修复):result.judge_details[] 条目的扁平评语字段。
   *  后端 JudgeScore 的 comment 位于 rankings[] 内;此可选字段是
   *  对部分路径(如 fallback / 历史落库展开)直接下发 comment 的兼容。 */
  comment?: string;
  winner_team_id: number;
  is_fallback: boolean;
}

export interface DebateTeamFinalScore {
  team_id: number;
  team_name: string;
  total_score: number;
  dimension_scores: Record<string, number>;
  rank: number;
}

export interface DebateBestDebater {
  seat: number;
  team_id: number;
  name: string;
  model_key: string;
  votes: number;
}

export interface DebateResult {
  winner_team_id: number;
  winner_team_name: string;
  best_debater: DebateBestDebater;
  team_scores: DebateTeamFinalScore[];
  judge_details: DebateJudgeScore[];
  is_abnormal: boolean;
  abnormal_reason?: string;
}

export interface DebateRoomSummary {
  room_id: string;
  topic: DebateTopic;
  mode: DebateMode;
  phase: DebatePhase;
  phase_cn: string;
  status: 'waiting' | 'playing' | 'over';
  spectator_count: number;
  team_count: number;
  judge_count: number;
  created_by: string;
  created_at: number;
  started_at: number;
}

export interface DebateClientTeam {
  team_id: number;
  stance: DebateStance;
  stance_label: string;
  agents: DebateAgentConfig[];
}

export interface DebateClientJudge {
  judge_id: number;
  model_key?: string;
  name?: string;
  bot_user_id?: string;
}

export interface DebateClientState {
  room_id: string;
  topic: DebateTopic;
  mode: DebateMode;
  status: 'waiting' | 'playing' | 'over';
  current_phase: DebatePhase;
  phase_cn: string;
  phase_deadline: number;
  time_remaining_sec: number;
  created_at: number;
  started_at: number;
  finished_at?: number;
  created_by: string;
  is_owner: boolean;
  spectator_count: number;
  current_speaker?: string;
  free_debate_owner?: string;
  teams: DebateClientTeam[];
  judges: DebateClientJudge[];
  speeches?: DebateSpeech[];
  cross_exam?: DebateCrossExamEntry[];
  judge_scores?: DebateJudgeScore[];
  result?: DebateResult;
  agent_thoughts?: Record<string, string>;
  phase_config: DebatePhaseConfig;
  spectator_config: DebateSpectatorConfig;
  /** §20260831-09 — 房间级 Agent Token 统计聚合(辩方 + 裁判)。 */
  agent_stats?: DebateRoomAgentStats;
}

/** §20260831-06 — 裁判回答观众提问帧 (debate.spectator_answer)。 */
export interface DebateSpectatorAnswer {
  room_id: string;
  question_id: string;
  question: string;
  answer: string;
  answer_judge_id: number;
  timestamp: number;
}

/** §20260831-06 — 裁判公开宣告帧 (debate.judge_announce)。 */
export interface DebateJudgeAnnounce {
  judge_id: number;
  text: string;
  timestamp: number;
}

/** §20260831-06 — 模型胜率统计 (GET /api/games/debate/stats)。 */
export interface DebateModelStats {
  model_key: string;
  total_games: number;
  win_count: number;
  best_debater_count: number;
  avg_total_score: number;
  win_rate: number;
}

/* ==========================================================================
 * §20260831-08 — 历史战绩 (大厅「历史战绩」面板 + 复盘详情弹窗)
 * 后端契约:GET /api/games/debate/history?page=&page_size= 与
 *          GET /api/games/debate/history/:id(以 ServerGo 落库 JSON tag 为准)。
 * ========================================================================== */

/**
 * 历史队伍中的辩手(落库精简版,对齐 ServerGo AgentConfig 的 wire 字段:
 * seat_id / role / role_name / model_key —— §20260831-08 契约对齐)。
 */
export interface DebateHistoryAgent {
  seat_id: number;
  role: DebateRole;
  role_name?: string;
  model_key?: string;
}

/** 历史房间 team_config[] 单项(后端持久化 []TeamConfig 原样透传)。 */
export interface DebateHistoryTeam {
  team_id: number;
  stance: DebateStance;
  stance_label?: string;
  agents: DebateHistoryAgent[];
}

/** 已结束比赛列表项(HistoryRoom)。 */
export interface DebateHistoryRoom {
  room_id: string;
  topic_text: string;
  topic_type?: string;
  mode: DebateMode;
  /** 后端落库为原始 phase 字符串(如 "game_over"),前端不消费,不做字面量收窄。 */
  status: string;
  winner_team_id: number;
  best_debater_seat: number;
  best_debater_team_id: number;
  finished_at: number;
  created_by: string;
  is_abnormal: boolean;
  /** 后端形状:顶层数组 []TeamConfig 原样透传(非 {teams:[]})。 */
  team_config?: DebateHistoryTeam[];
}

/** GET /api/games/debate/history 响应 data。 */
export interface DebateHistoryListData {
  rooms: DebateHistoryRoom[];
  total: number;
  page: number;
  page_size: number;
}

/** 落库发言记录(HistorySpeech)。 */
export interface DebateHistorySpeech {
  id: string;
  room_id: string;
  phase: DebatePhase;
  team_id: number;
  seat: number;
  stance: DebateStance;
  speaker_name: string;
  role: DebateRole;
  content: string;
  word_count: number;
  model_key?: string;
  created_at: number;
}

/** 落库裁判评分(HistoryScore,一行 = 一位裁判对一支队伍的打分)。 */
export interface DebateHistoryScore {
  id: string;
  room_id: string;
  judge_id: number;
  judge_model_key?: string;
  team_id: number;
  argument_quality: number;
  logic_rigor: number;
  language_expression: number;
  team_coordination: number;
  rebuttal_effectiveness: number;
  total_score: number;
  comment: string;
  best_debater_seat: number;
  winner_team_id: number;
  overall_comment: string;
  is_fallback: boolean;
}

/** GET /api/games/debate/history/:id 响应 data。 */
export interface DebateHistoryDetail {
  room: DebateHistoryRoom;
  speeches: DebateHistorySpeech[];
  scores: DebateHistoryScore[];
}

export interface DebateCreateRoomRequest {
  name?: string;
  topic_id?: string;
  topic_text?: string;
  topic_type?: string;
  mode: DebateMode;
  phase_config?: Partial<DebatePhaseConfig>;
  spectator_config?: Partial<DebateSpectatorConfig>;
  agent_assignment?: 'auto' | 'manual';
  teams?: {
    team_id: number;
    stance: DebateStance;
    stance_label?: string;
    agents: {
      seat_id: number;
      role: DebateRole;
      role_name?: string;
      model_key: string;
    }[];
  }[];
  judges?: {
    judge_id: number;
    model_key: string;
  }[];
}

/* ==========================================================================
 * §20260831-09 — Agent Token + API 统计 / 裁判实时打分
 * ========================================================================== */

/** 房间级 Agent 统计聚合(debate.stats_update 帧的 aggregate 子结构)。 */
export interface DebateRoomAgentStats {
  bot_count: number;
  bot_total_input_tokens: number;
  bot_total_output_tokens: number;
  bot_total_api_tokens: number;
  bot_api_call_count: number;
  bot_api_success_count: number;
  bot_api_fail_count: number;
  judge_count: number;
  judge_total_input_tokens: number;
  judge_total_output_tokens: number;
  judge_total_api_tokens: number;
  judge_api_call_count: number;
  judge_api_success_count: number;
  judge_api_fail_count: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_api_tokens: number;
  total_api_call_count: number;
  elapsed_sec: number;
  tokens_per_hour: number;
  show_token_rate: boolean;
}

/** 单 Bot 详细统计(debate.stats_update 帧的 bots[] 子结构)。 */
export interface DebateAgentTokenSnapshot {
  team_id: number;
  seat: number;
  role: string;
  role_name?: string;
  model_key?: string;
  llm_call_count: number;
  input_tokens: number;
  output_tokens: number;
  api_tokens: number;
  api_success_count: number;
  api_fail_count: number;
}

/** 单裁判详细统计(debate.stats_update 帧的 judges[] 子结构)。 */
export interface DebateJudgeTokenSnapshot {
  judge_id: number;
  model_key?: string;
  llm_call_count: number;
  input_tokens: number;
  output_tokens: number;
  api_tokens: number;
  api_success_count: number;
  api_fail_count: number;
}

/** 完整 Agent 统计帧(debate.stats_update)。 */
export interface DebateAgentStatsDetail {
  room_id: string;
  aggregate: DebateRoomAgentStats;
  bots: DebateAgentTokenSnapshot[];
  judges: DebateJudgeTokenSnapshot[];
}

/** 单裁判对单队的累计实时打分。 */
export interface DebateAccumulatedTeamScore {
  team_id: number;
  argument_quality: number;
  logic_rigor: number;
  language_expression: number;
  team_coordination: number;
  rebuttal_effectiveness: number;
  total_score: number;
  latest_comment: string;
  latest_phase: string;
  latest_phase_cn: string;
  submission_count: number;
}

/** 裁判阶段打分帧(debate.stage_score)。 */
export interface DebateStageScore {
  judge_id: number;
  model_key: string;
  phase: string;
  phase_cn: string;
  team_scores: DebateTeamRanking[];
  winner_team_id: number;
  overall_comment: string;
  submitted_at_ms: number;
  is_final: boolean;
}

/** 单裁判实时打分看板(debate.judge_scoreboard payload)。 */
export interface DebateJudgeScoreboard {
  judge_id: number;
  model_key: string;
  team_scores: Record<number, DebateAccumulatedTeamScore>;
  stage_history: DebateStageScore[];
  is_final: boolean;
}