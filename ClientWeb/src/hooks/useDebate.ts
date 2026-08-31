/**
 * 辩论比赛 WS 帧订阅与发送 hook (2026-08-31 §20260831-01 + §20260831-04)
 *
 * 对齐 ServerGo/ws/debate_service.go 帧类型 + docs/辩论比赛/00 §4.2。
 *
 * §20260831-04 — 增补:
 *   - debate.commentary 帧订阅:把 AI 解说追加到 store.commentaries
 *   - debate.agent_thought 帧:单个 Bot 的思考过程写入 agent_thoughts
 *
 * §20260831-06 — 增补:
 *   - debate.spectator_answer 帧:裁判回答观众提问 → store.spectatorAnswers
 *   - debate.judge_announce 帧:裁判公开宣告 → store.judgeAnnouncements
 */
import { useEffect } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useDebateStore } from '@/store/debate.store';
import { debateService } from '@/api/debate';
import { reportGlobalError } from '@/services/globalError';
import type {
  DebateClientState,
  DebateCrossExamEntry,
  DebateJudgeScore,
  DebateResult,
  DebateSpectatorAnswer,
  DebateSpeech,
} from '@/types/debate';

export function useDebate(roomId: string) {
  const {
    setGameState,
    updatePhase,
    addSpeech,
    addCrossExam,
    addJudgeScore,
    setResult,
    addAgentThought,
    pushCommentary,
    addSpectatorAnswer,
    pushJudgeAnnounce,
  } = useDebateStore();

  useEffect(() => {
    let cancelled = false;

    // 1) HTTP fetch initial state
    debateService
      .detail(roomId)
      .then((state) => {
        if (cancelled) return;
        setGameState(state);
      })
      .catch((e) => {
        if (cancelled) return;
        reportGlobalError({ message: e.message ?? 'failed to load debate room', severity: 'error' });
      });

    // 2) WS subscribe
    wsClient.send('debate.subscribe', { room_id: roomId });

    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('debate.')) return;
      const p = env.payload as Record<string, unknown>;

      switch (env.type) {
        case 'debate.state': {
          const state = p as unknown as DebateClientState;
          if (state.room_id !== roomId) return;
          setGameState(state);
          break;
        }
        case 'debate.phase': {
          const phase = (p.phase ?? '') as string;
          const timeRemaining = (p.time_remaining_sec ?? 0) as number;
          if (p.room_id !== roomId) return;
          updatePhase(phase, timeRemaining);
          break;
        }
        case 'debate.speech': {
          const speech = (p.speech ?? p) as DebateSpeech;
          if (p.room_id !== roomId) return;
          addSpeech(speech);
          break;
        }
        case 'debate.cross_exam': {
          const entry = (p.cross_exam ?? p) as DebateCrossExamEntry;
          if (p.room_id !== roomId) return;
          addCrossExam(entry);
          break;
        }
        case 'debate.judge_vote': {
          const score = p as unknown as DebateJudgeScore;
          if (score && typeof score.judge_id === 'number') {
            addJudgeScore(score);
          }
          break;
        }
        case 'debate.game_over': {
          const result = (p.result ?? p) as DebateResult;
          if (result && typeof result.winner_team_id === 'number') {
            setResult(result);
          }
          break;
        }
        case 'debate.agent_thought': {
          const seat = (p.seat ?? '') as string;
          const thought = (p.thought ?? '') as string;
          if (!seat) return;
          addAgentThought(seat, thought);
          break;
        }
        // §20260831-04 — 解说帧(spectator-only 通道)。
        case 'debate.commentary': {
          const text = (p.text ?? '') as string;
          if (!text) return;
          pushCommentary({
            text,
            style: (p.style ?? 'pro') as string,
            timestamp: (p.timestamp ?? Date.now()) as number,
          });
          break;
        }
        // §20260831-06 — 裁判回答观众提问(观众提问闭环)。
        case 'debate.spectator_answer': {
          if (p.room_id !== roomId) return;
          const ans = {
            room_id: roomId,
            question_id: (p.question_id ?? '') as string,
            question: (p.question ?? '') as string,
            answer: (p.answer ?? '') as string,
            answer_judge_id: (p.answer_judge_id ?? -1) as number,
            timestamp: (p.timestamp ?? Date.now()) as number,
          } as DebateSpectatorAnswer;
          if (ans.question_id && ans.answer) addSpectatorAnswer(ans);
          break;
        }
        // §20260831-06 — 裁判公开宣告(announce 工具)。
        case 'debate.judge_announce': {
          if (p.room_id !== roomId) return;
          const text = (p.text ?? '') as string;
          if (!text) return;
          pushJudgeAnnounce({
            judge_id: (p.judge_id ?? 0) as number,
            text,
            timestamp: (p.timestamp ?? Date.now()) as number,
          });
          break;
        }
        case 'debate.spectator_question':
        case 'debate.like':
          // 只展示用,不写 store
          break;
        case 'debate.error': {
          const msg = (p.message ?? 'unknown debate error') as string;
          reportGlobalError({ message: msg, severity: 'error' });
          break;
        }
      }
    });

    return () => {
      cancelled = true;
      wsClient.send('debate.unsubscribe', { room_id: roomId });
      unsub();
    };
  }, [roomId, setGameState, updatePhase, addSpeech, addCrossExam, addJudgeScore, setResult, addAgentThought, pushCommentary, addSpectatorAnswer, pushJudgeAnnounce]);
}