/**
 * 辩论比赛 Zustand store (2026-08-31 §20260831-01)
 *
 * 与 store/{werewolf,texasholdem}.store.ts 同构:
 *   - 进入对局页前 reset() 清掉上一个会话残留
 *   - 通过 WebSocket 帧增量更新 state
 */
import { create } from 'zustand';
import type {
  DebateClientState,
  DebateCrossExamEntry,
  DebateJudgeScore,
  DebateResult,
  DebateRoomSummary,
  DebateSpeech,
} from '@/types/debate';

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

  // UI
  spectatorCount: number;
  currentSpeaker: string;

  // Actions
  setRooms: (rooms: DebateRoomSummary[]) => void;
  setGameState: (state: DebateClientState) => void;
  updatePhase: (phase: string, timeRemaining: number) => void;
  setCurrentSpeaker: (speaker: string) => void;
  addSpeech: (speech: DebateSpeech) => void;
  addCrossExam: (entry: DebateCrossExamEntry) => void;
  setCurrentSpeech: (speech: DebateSpeech | null) => void;
  addJudgeScore: (score: DebateJudgeScore) => void;
  setResult: (result: DebateResult) => void;
  addAgentThought: (seat: string, thought: string) => void;
  setSpectatorCount: (n: number) => void;
  reset: () => void;
}

const initial: Pick<
  DebateStore,
  'rooms' | 'currentRoom' | 'phase' | 'timeRemaining' | 'speeches' | 'crossExam' | 'currentSpeech' | 'judgeScores' | 'result' | 'agentThoughts' | 'spectatorCount' | 'currentSpeaker'
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
  updatePhase: (phase, timeRemaining) => set({ phase, timeRemaining }),
  setCurrentSpeaker: (speaker) => set({ currentSpeaker: speaker }),
  addSpeech: (speech) => set((s) => ({
    speeches: [...s.speeches, speech],
    currentSpeech: speech,
  })),
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
  setSpectatorCount: (n) => set({ spectatorCount: n }),
  reset: () => set(initial),
}));