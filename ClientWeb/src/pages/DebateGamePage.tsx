/**
 * 辩论比赛 — 对局页 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3 对局页设计 + 复用 §6.2 zustand store / §6.3 useDebate hook。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useDebate } from '@/hooks/useDebate';
import { useDebateStore } from '@/store/debate.store';
import { debateService } from '@/api/debate';
import { reportGlobalError } from '@/services/globalError';
import DebateStage from '@/components/debate/DebateStage';
import DebateTeamPanel from '@/components/debate/DebateTeamPanel';
import DebateJudgePanel from '@/components/debate/DebateJudgePanel';
import DebateSpeechPanel from '@/components/debate/DebateSpeechPanel';
import DebateScorePanel from '@/components/debate/DebateScorePanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';

export function DebateGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  useDebate(roomId!);
  const reset = useDebateStore((s) => s.reset);

  useEffect(() => {
    if (!roomId) return;
    // 进入房间:确保已加入观战者集合
    debateService.spectate(roomId).catch((e) => {
      // 已加入是 200;其他错误才上报
      if (!String(e.message).includes('already')) {
        reportGlobalError({ message: e.message, severity: 'warn' });
      }
    });

    return () => {
      reset();
      // 不主动 leave_spectate — 让 WS 关闭自然离开
    };
  }, [roomId, reset]);

  if (!roomId) {
    return <div className="error">无效的房间 ID</div>;
  }

  return (
    <div className="debate-game-page">
      <header className="game-header">
        <h1>🎓 辩论对局</h1>
        <span className="room-id">{roomId}</span>
      </header>

      <main className="game-main">
        <aside className="team-panel-col">
          <DebateTeamPanel />
        </aside>

        <section className="stage-col">
          <DebateStage />
          <DebateSpeechPanel />
        </section>

        <aside className="judge-col">
          <DebateJudgePanel />
          <DebateScorePanel />
        </aside>
      </main>

      <footer className="chat-col">
        <GameChatPanel roomId={roomId} />
      </footer>
    </div>
  );
}