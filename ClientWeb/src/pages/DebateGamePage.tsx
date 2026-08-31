/**
 * 辩论比赛 — 对局页 (2026-08-31 §20260831-01 + §20260831-04 + §20260831-05)
 *
 * 对齐 docs/辩论比赛/04 §3 对局页设计 + 复用 §6.2 zustand store / §6.3 useDebate hook。
 *
 * §20260831-05 增补:
 *   - 评委列加入 DebateSpectatorQuestionPanel(观众向裁判提问)
 *   - 聊天行上方加入 DebateHistoryPanel(完整发言历史 + 质询记录)
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useDebate } from '@/hooks/useDebate';
import { useDebateStore } from '@/store/debate.store';
import { debateService } from '@/api/debate';
import { reportGlobalError } from '@/services/globalError';
import DebateStage from '@/components/debate/DebateStage';
import DebateTeamPanel from '@/components/debate/DebateTeamPanel';
import DebateJudgePanel from '@/components/debate/DebateJudgePanel';
import DebateSpeechPanel from '@/components/debate/DebateSpeechPanel';
import DebateScorePanel from '@/components/debate/DebateScorePanel';
import DebateCommentaryPanel from '@/components/debate/DebateCommentaryPanel';
import DebateHostControls from '@/components/debate/DebateHostControls';
import DebateSpectatorQuestionPanel from '@/components/debate/DebateSpectatorQuestionPanel';
import DebateHistoryPanel from '@/components/debate/DebateHistoryPanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';

export function DebateGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const navigate = useNavigate();
  useDebate(roomId!);
  const reset = useDebateStore((s) => s.reset);
  const spectatorCount = useDebateStore((s) => s.spectatorCount);
  const phase = useDebateStore((s) => s.phase);
  const currentRoom = useDebateStore((s) => s.currentRoom);

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

  const isGameOver = phase === 'game_over' || phase === 'result';

  return (
    <div className="debate-game-page">
      <header className="game-header">
        <h1>🎓 辩论对局</h1>
        <div className="header-meta">
          <span className="room-id">房间:{roomId}</span>
          <span className="spectator-count">👥 观战 {spectatorCount}</span>
          {currentRoom?.topic?.text && (
            <span className="topic-text">「{currentRoom.topic.text}」</span>
          )}
        </div>
        <DebateHostControls roomId={roomId} />
      </header>

      <main className="game-main">
        <aside className="team-panel-col">
          <DebateTeamPanel />
        </aside>

        <section className="stage-col">
          <DebateStage />
          <DebateCommentaryPanel />
          <DebateSpeechPanel />
          <DebateSpectatorQuestionPanel roomId={roomId} />
        </section>

        <aside className="judge-col">
          <DebateJudgePanel />
          <DebateScorePanel />
          {isGameOver && (
            <div className="post-game-actions">
              <button
                type="button"
                className="btn-secondary btn-block"
                onClick={() => navigate('/debate')}
              >
                ↩ 返回大厅
              </button>
            </div>
          )}
        </aside>
      </main>

      <section className="history-col">
        <DebateHistoryPanel roomId={roomId} />
      </section>

      <footer className="chat-col">
        <GameChatPanel roomId={roomId} />
      </footer>
    </div>
  );
}
