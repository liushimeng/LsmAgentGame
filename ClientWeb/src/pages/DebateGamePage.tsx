/**
 * 辩论比赛 — 对局页 (2026-08-31 §20260831-01 + §20260831-04 + §20260831-05 + §20260831-07 + §20260831-03)
 *
 * §20260831-03 — 布局重构 (三栏自适应 + 侧栏折叠):
 *   - 左栏: 发言历史 + 队伍信息 (可折叠)
 *   - 中栏: 主舞台 + AI 解说 (核心显示区)
 *   - 右栏: 房间聊天 + 裁判评审 + Agent 统计 (可折叠)
 *   - 移除底部横向聊天栏,聊天移入右栏
 *
 * §20260831-07 — R6 测试报告 §3.4 修复:
 *   DebateSpeechPanel(中列,WS 实时推送,按 phase 分组折叠)与
 *   DebateHistoryPanel(下方 .history-col,REST 拉取)展示的是同一份
 *   发言数据,出现两份相同信息。改为只保留 SpeechPanel,删除 history-col
 *   整列布局,组件本身保留(供未来「完整历史回放」页面复用)。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useDebate } from '@/hooks/useDebate';
import { useDebateStore } from '@/store/debate.store';
import { debateService } from '@/api/debate';
import { reportGlobalError } from '@/services/globalError';
import { useSidebarCollapse } from '@/hooks/useSidebarCollapse';
import DebateStage from '@/components/debate/DebateStage';
import DebateTeamPanel from '@/components/debate/DebateTeamPanel';
import DebateJudgePanel from '@/components/debate/DebateJudgePanel';
import DebateSpeechPanel from '@/components/debate/DebateSpeechPanel';
import DebateScorePanel from '@/components/debate/DebateScorePanel';
import DebateCommentaryPanel from '@/components/debate/DebateCommentaryPanel';
import DebateHostControls from '@/components/debate/DebateHostControls';
import DebateSpectatorQuestionPanel from '@/components/debate/DebateSpectatorQuestionPanel';
import DebateAgentStatsPanel from '@/components/debate/DebateAgentStatsPanel';
import DebateJudgeScoreboardPanel from '@/components/debate/DebateJudgeScoreboardPanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';

export function DebateGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const navigate = useNavigate();
  useDebate(roomId!);
  const reset = useDebateStore((s) => s.reset);
  const spectatorCount = useDebateStore((s) => s.spectatorCount);
  const phase = useDebateStore((s) => s.phase);
  const currentRoom = useDebateStore((s) => s.currentRoom);
  const { leftCollapsed, rightCollapsed, toggleLeft, toggleRight } = useSidebarCollapse();

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
        {/* 左栏: 发言历史 + 队伍信息 (可折叠) */}
        <aside className={`sidebar-left${leftCollapsed ? ' sidebar-collapsed' : ''}`}>
          {!leftCollapsed && (
            <div className="sidebar-content sidebar-left-content">
              <DebateTeamPanel />
              <DebateSpeechPanel />
            </div>
          )}
          <button
            type="button"
            className="sidebar-toggle sidebar-toggle-left"
            onClick={toggleLeft}
            aria-label={leftCollapsed ? '展开发言历史' : '折叠发言历史'}
            title={leftCollapsed ? '展开发言历史' : '折叠发言历史'}
          >
            {leftCollapsed ? '📜 ▶' : '◀ 📜'}
          </button>
        </aside>

        {/* 中栏: 主舞台 (核心显示区) */}
        <section className="main-col">
          <DebateStage />
          <DebateCommentaryPanel />
          <DebateSpectatorQuestionPanel roomId={roomId} />
        </section>

        {/* 右栏: 房间聊天 + 裁判评审 + Agent 统计 (可折叠) */}
        <aside className={`sidebar-right${rightCollapsed ? ' sidebar-collapsed' : ''}`}>
          <button
            type="button"
            className="sidebar-toggle sidebar-toggle-right"
            onClick={toggleRight}
            aria-label={rightCollapsed ? '展开聊天与裁判' : '折叠聊天与裁判'}
            title={rightCollapsed ? '展开聊天与裁判' : '折叠聊天与裁判'}
          >
            {rightCollapsed ? '💬 ◀' : '▶ 💬'}
          </button>
          {!rightCollapsed && (
            <div className="sidebar-content sidebar-right-content">
              <div className="sidebar-chat">
                <GameChatPanel roomId={roomId} />
              </div>
              <div className="sidebar-aux">
                <DebateAgentStatsPanel />
                <DebateJudgePanel />
                <DebateJudgeScoreboardPanel />
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
              </div>
            </div>
          )}
        </aside>
      </main>
    </div>
  );
}
