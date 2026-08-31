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