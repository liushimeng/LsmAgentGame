/**
 * 辩论比赛 Zustand store (2026-08-31 §20260831-01 + §20260831-04)
 *
 * 与 store/{werewolf,texasholdem}.store.ts 同构:
 *   - 进入对局页前 reset() 清掉上一个会话残留
 *   - 通过 WebSocket 帧增量更新 state
 *
 * §20260831-04 — 增补:
 *   - likedSpeeches 记录当前用户点过赞的发言 ID(避免重复点赞)
 *   - commentaries 解说缓冲(§02 §3.5 解说向观众推送)
 *
 * §20260831-06 — 增补:
 *   - spectatorAnswers 裁判回答观众提问(debate.spectator_answer 帧)
 *   - judgeAnnouncements 裁判公开宣告(debate.judge_announce 帧)
 *
 * §20260831-11 — 增补:
 *   - updatePhase 扩签名接收 phase_cn,并同步更新 currentRoom 副本
 *     (R8 P1-A:会话内开始比赛时阶段标签不推进)
 */
import { create } from 'zustand';
import type {
  DebateAgentStatsDetail,
  DebateClientState,
  DebateCrossExamEntry,
  DebateJudgeAnnounce,
  DebateJudgeScore,
  DebateJudgeScoreboard,
  DebateResult,
  DebateRoomSummary,
  DebateSpectatorAnswer,
  DebateSpeech,
} from '@/types/debate';

export interface DebateCommentaryEntry {
  text: string;
  style: string;
  timestamp: number;
}

interface DebateStore {
  // 房间列表 / 当前房间
  rooms: DebateRoomSummary[];
  currentRoom: DebateClientState | null;

  // 阶段 / 时间
  phase: string;
  timeRemaining: number;

  // 发言 / 质询
  speeches: DebateSpeech[];
  crossExam: DebateCrossExamEntry[];
  currentSpeech: DebateSpeech | null;

  // 评分 / 结果
  judgeScores: DebateJudgeScore[];
  result: DebateResult | null;

  // Agent 思考(按 team:seat key)
  agentThoughts: Record<string, string>;

  // 解说(§20260831-04)
  commentaries: DebateCommentaryEntry[];

  // 用户操作(§20260831-04)
  likedSpeeches: Record<string, number>;

  // §20260831-06 — 观众提问闭环:裁判回答(按 question_id 索引)
  spectatorAnswers: Record<string, DebateSpectatorAnswer>;
  // §20260831-06 — 裁判公开宣告(最近 10 条)
  judgeAnnouncements: DebateJudgeAnnounce[];

  // §20260831-09 — Agent 统计增量(debate.stats_update 帧)
  agentStatsDetail: DebateAgentStatsDetail | null;

  // §20260831-09 — 裁判实时打分看板(debate.judge_scoreboard 帧,按 judge_id 索引)
  scoreboards: Record<number, DebateJudgeScoreboard>;

  // UI
  spectatorCount: number;
  currentSpeaker: string;

  // Actions
  setRooms: (rooms: DebateRoomSummary[]) => void;
  setGameState: (state: DebateClientState) => void;
  /** §20260831-11(P1-A 修复):扩签名接收 phase_cn —— 除顶层 phase/timeRemaining 外
   *  必须同步更新 currentRoom 副本,否则读 currentRoom.phase_cn 的组件
   *  (DebateStage 阶段标签/计时器)永远停留在 HTTP detail 初值,不随 WS 帧推进。 */
  updatePhase: (phase: string, timeRemaining: number, phaseCn?: string) => void;
  setCurrentSpeaker: (speaker: string) => void;
  addSpeech: (speech: DebateSpeech) => void;
  addCrossExam: (entry: DebateCrossExamEntry) => void;
  setCurrentSpeech: (speech: DebateSpeech | null) => void;
  addJudgeScore: (score: DebateJudgeScore) => void;
  setResult: (result: DebateResult) => void;
  addAgentThought: (seat: string, thought: string) => void;
  pushCommentary: (entry: DebateCommentaryEntry) => void;
  addSpectatorAnswer: (answer: DebateSpectatorAnswer) => void;
  pushJudgeAnnounce: (announce: DebateJudgeAnnounce) => void;
  toggleLike: (speechId: string) => boolean;
  setSpectatorCount: (n: number) => void;
  reset: () => void;

  patchRoom: (room: Partial<DebateRoomSummary> & { room_id: string }) => void;
  removeRoom: (roomId: string) => void;

  // §20260831-09 — Agent 统计 / 裁判实时打分 actions
  setAgentStatsDetail: (detail: DebateAgentStatsDetail) => void;
  setJudgeScoreboard: (judgeId: number, board: DebateJudgeScoreboard) => void;
}

const initial: Pick<
  DebateStore,
  | 'rooms'
  | 'currentRoom'
  | 'phase'
  | 'timeRemaining'
  | 'speeches'
  | 'crossExam'
  | 'currentSpeech'
  | 'judgeScores'
  | 'result'
  | 'agentThoughts'
  | 'commentaries'
  | 'likedSpeeches'
  | 'spectatorAnswers'
  | 'judgeAnnouncements'
  | 'agentStatsDetail'
  | 'scoreboards'
  | 'spectatorCount'
  | 'currentSpeaker'
> = {
  rooms: [],
  currentRoom: null,
  phase: 'filling',
  timeRemaining: 0,
  speeches: [],
  crossExam: [],
  currentSpeech: null,
  judgeScores: [],
  result: null,
  agentThoughts: {},
  commentaries: [],
  likedSpeeches: {},
  spectatorAnswers: {},
  judgeAnnouncements: [],
  agentStatsDetail: null,
  scoreboards: {},
  spectatorCount: 0,
  currentSpeaker: '',
};

export const useDebateStore = create<DebateStore>((set) => ({
  ...initial,

  setRooms: (rooms) => set({ rooms }),
  setGameState: (state) => set({
    currentRoom: state,
    phase: state.current_phase,
    timeRemaining: state.time_remaining_sec,
    speeches: state.speeches ?? [],
    crossExam: state.cross_exam ?? [],
    judgeScores: state.judge_scores ?? [],
    result: state.result ?? null,
    agentThoughts: state.agent_thoughts ?? {},
    spectatorCount: state.spectator_count,
    currentSpeaker: state.current_speaker ?? '',
  }),
  // §20260831-11(P1-A 修复):同步写 currentRoom 的 current_phase / phase_cn /
  // time_remaining_sec,currentRoom 为 null 时仅更新顶层(不崩溃)。
  updatePhase: (phase, timeRemaining, phaseCn) => set((s) => ({
    phase,
    timeRemaining,
    currentRoom: s.currentRoom
      ? {
          ...s.currentRoom,
          current_phase: phase as DebateClientState['current_phase'],
          phase_cn: phaseCn || s.currentRoom.phase_cn,
          time_remaining_sec: timeRemaining,
        }
      : null,
  })),
  setCurrentSpeaker: (speaker) => set({ currentSpeaker: speaker }),
  // §20260831-10(P1-2 修复):addSpeech 幂等去重,防止 history fallback + WS 帧重复追加。
  addSpeech: (speech) => set((s) => {
    if (speech.id && s.speeches.some((x) => x.id === speech.id)) {
      // 重复发言:仅更新 currentSpeech,不追加到列表
      return { currentSpeech: speech };
    }
    return { speeches: [...s.speeches, speech], currentSpeech: speech };
  }),
  addCrossExam: (entry) => set((s) => ({ crossExam: [...s.crossExam, entry] })),
  setCurrentSpeech: (speech) => set({ currentSpeech: speech }),
  addJudgeScore: (score) => set((s) => {
    const existing = s.judgeScores.findIndex((x) => x.judge_id === score.judge_id);
    if (existing >= 0) {
      const copy = [...s.judgeScores];
      copy[existing] = score;
      return { judgeScores: copy };
    }
    return { judgeScores: [...s.judgeScores, score] };
  }),
  setResult: (result) => set({ result }),
  addAgentThought: (seat, thought) => set((s) => ({
    agentThoughts: { ...s.agentThoughts, [seat]: thought },
  })),
  // §20260831-04 — 解说帧追加(最多保留最近 20 条,环形覆盖)。
  pushCommentary: (entry) => set((s) => {
    const next = [...s.commentaries, entry];
    if (next.length > 20) {
      return { commentaries: next.slice(next.length - 20) };
    }
    return { commentaries: next };
  }),
  // §20260831-06 — 裁判回答观众提问(按 question_id 索引,幂等覆盖)。
  addSpectatorAnswer: (answer) => set((s) => ({
    spectatorAnswers: { ...s.spectatorAnswers, [answer.question_id]: answer },
  })),

  // §20260831-06 — 裁判公开宣告(最多保留最近 10 条)。
  pushJudgeAnnounce: (announce) => set((s) => {
    const next = [...s.judgeAnnouncements, announce];
    return { judgeAnnouncements: next.length > 10 ? next.slice(next.length - 10) : next };
  }),

  // §20260831-04 — 点赞(返回是否本次切换为"已点赞")。
  toggleLike: (speechId) => {
    let added = false;
    set((s) => {
      const cur = s.likedSpeeches[speechId] ?? 0;
      const next = cur > 0 ? 0 : 1;
      added = next > 0;
      return { likedSpeeches: { ...s.likedSpeeches, [speechId]: next } };
    });
    return added;
  },

  // §20260831-04 — 大厅房间状态补丁(WS room.state 帧实时更新)。
  patchRoom: (room) => set((s) => {
    const idx = s.rooms.findIndex((r) => r.room_id === room.room_id);
    if (idx < 0) return {};
    const copy = [...s.rooms];
    copy[idx] = { ...copy[idx], ...room };
    return { rooms: copy };
  }),
  removeRoom: (roomId) => set((s) => ({
    rooms: s.rooms.filter((r) => r.room_id !== roomId),
  })),

  setSpectatorCount: (n) => set({ spectatorCount: n }),

  // §20260831-09 — Agent 统计增量(debate.stats_update)替换旧快照。
  setAgentStatsDetail: (detail) => set({ agentStatsDetail: detail }),

  // §20260831-09 — 裁判实时打分看板(debate.judge_scoreboard)按 judge_id 合并。
  setJudgeScoreboard: (judgeId, board) => set((s) => ({
    scoreboards: { ...s.scoreboards, [judgeId]: board },
  })),

  reset: () => set(initial),
}));
