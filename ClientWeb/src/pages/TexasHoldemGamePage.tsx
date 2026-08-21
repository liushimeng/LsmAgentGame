import { useEffect, useCallback, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useTexasHoldemStore } from '@/store/texasholdem.store';
import { useTexasHoldem } from '@/hooks/useTexasHoldem';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { TexasHoldemTable } from '@/components/texasholdem/TexasHoldemTable';
import { ActionControls } from '@/components/texasholdem/ActionControls';
import { GameInfoPanel } from '@/components/texasholdem/GameInfoPanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';
import { useT } from '@/hooks/useT';
import { wsClient } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import { reportGlobalError } from '@/services/globalError';
import type { TKey } from '@/i18n';
import type { TexasActionType } from '@/types/texasholdem';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

export function TexasHoldemGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();
  const [resignPromptOpen, setResignPromptOpen] = useState(false);
  // §20260819-02 BUG-FIX: 加载超时(15s)降级提示,避免「观战中…」 spinner 永久卡死。
  const [joinTimeout, setJoinTimeout] = useState(false);

  const { gameState, mySeat, style, gameOver, lastError, reset } = useTexasHoldemStore();
  const {
    joinGame,
    spectate,
    unspectate,
    sendAction,
    resign,
    leaveGame,
    requestState,
  } = useTexasHoldem(roomId!);

  useEffect(() => {
    if (!roomId) return;
    // 进入对局前清空上一次会话的 store 残留(选子 / 座位 / 结算),避免 observer
    // 路由上看到旧牌桌或旧手数;再调 useSessionRestore 补 game.spectate/state。
    reset();
    setJoinTimeout(false);
    let retries = 0;
    const tryHook = () => {
      if (retries++ > 10) return;
      const frame = spectator ? 'game.spectate' : 'game.join';
      const sent = wsClient.send(frame, {
        room_id: roomId,
        game_kind: 'texasholdem',
      });
      if (sent === false) setTimeout(tryHook, 500);
    };
    const timer = setTimeout(tryHook, 300);
    const stateTimer = setInterval(() => requestState(), 8000);
    // §20260819-02 BUG-FIX: 加载超时 15s 后降级提示。
    const timeoutTimer = setTimeout(() => {
      setJoinTimeout((prev) => {
        if (!prev) {
          reportGlobalError({
            message: '加载游戏状态超时,可能房间已被解散',
            severity: 'warning',
          });
        }
        return true;
      });
    }, 15000);
    return () => {
      clearTimeout(timer);
      clearInterval(stateTimer);
      clearTimeout(timeoutTimer);
      if (spectator) unspectate();
    };
  }, [roomId, spectator, joinGame, spectate, unspectate, requestState, reset]);

  // §20260819-02 P0-2 — 监听 store.lastError 同步本地显示位。hook 内部
  // 已 reportGlobalError 兜底,这里只补 §7.1「在当前页面最高可见位置」展示。
  // 错误来源:hook 收到的 game.error 帧(罕见但 P0-1 触发面必备)。
  const showLastError = !!lastError;

  const handleResign = useCallback(() => {
    setResignPromptOpen(true);
  }, []);

  const confirmResign = useCallback(() => {
    setResignPromptOpen(false);
    resign();
  }, [resign]);

  const cancelResign = useCallback(() => {
    setResignPromptOpen(false);
  }, []);

  // §20260819-02 BUG-FIX: 退出按钮永远可点(无论 spectator / player / 加载中);
  // 即使 unspectate/leaveSpectate 失败,也强行返回大厅,避免用户被困。
  const handleLeave = useCallback(async () => {
    setResignPromptOpen(false);
    if (spectator) {
      unspectate();
      try {
        await roomService.leaveSpectate(roomId!);
      } catch {
        /* best-effort */
      }
    } else {
      leaveGame();
      try {
        await roomService.leave(roomId!);
      } catch {
        /* best-effort */
      }
    }
    reset();
    nav('/texasholdem');
  }, [roomId, spectator, leaveGame, unspectate, reset, nav]);

  const handleAction = useCallback(
    (type: TexasActionType, amount?: number) => {
      sendAction(type, amount);
    },
    [sendAction],
  );

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  // Spectators never receive a my_seat; force -1 so the player branches
  // (action controls, "your turn" labels) stay closed.
  const effectiveSeat = spectator ? -1 : mySeat;
  const isWaiting = !gameState || !gameState.ready;
  const isPlaying = gameState?.status === 'playing';
  const isOver = gameState?.status === 'over' || gameState?.status === 'showdown';
  const isMyTurn = !spectator && isPlaying && gameState?.turn === effectiveSeat;
  const self = gameState?.players[effectiveSeat];
  // canCheck / callAmount 必须使用 round_committed(本街下注),不是 chips_committed
  // (本手总投入,跨街累积,BUG-TEXAS-CANCHECK-USE-TOTAL)。
  // 历史 Bug #10:preflop chips_committed=$200 (BB + 跟注) vs current_bet 被
  // advanceToNextStreet 重置为 0 → 永不相等,但 UI 仍显示 "过牌" 按钮 → 用户点击
  // 必返回 "cannot check when there is a bet to call"。修复:改用 round_committed。
  const myRound = self?.round_committed ?? 0;
  const canCheck = isMyTurn && (gameState?.current_bet ?? 0) === myRound;
  const callAmount = Math.max(0, (gameState?.current_bet ?? 0) - myRound);
  const canCall = isMyTurn && callAmount > 0;

  return (
    <div className="texas-game">
      <div className="game-area">
        <div className="board-container">
          {/* §20260819-02 P0-2 — 错误帧到达立即显示,不等 15s。hook 内部已
              reportGlobalError 全局兜底,这里补当前页面最高可见位置展示(§7.1)。
              优先于 15s joinTimeout banner:即使是「数据在路上但 WS 已报错」
              也能立即看到根因,而不是转圈后降级。 */}
          {showLastError && lastError && (
            <div className="error-banner" role="alert">
              <p>⚠️ {lastError.message}</p>
              <button className="btn btn-primary" onClick={handleLeave}>
                ← {t('common.backToLobby' as TKey)}
              </button>
            </div>
          )}
          {/* §20260819-02 BUG-FIX: 加载超时降级提示 + 返回大厅按钮 */}
          {joinTimeout && !gameState && !showLastError && (
            <div className="error-banner">
              <p>⚠️ 加载游戏状态超时,可能房间已被解散。</p>
              <button className="btn btn-primary" onClick={handleLeave}>
                ← {t('common.backToLobby' as TKey)}
              </button>
            </div>
          )}
          {isWaiting ? (
            <div className="waiting-board">
              {/* §20260819-02 P1-3 — 区分观战「数据在路上」与「等待开局」。
                  观战者拿到 gameState 但 ready=false 时,显示「等待 N/6 玩家入座」,
                  不再 spinner-only 让观战者误以为是加载问题;
                  只有 !gameState(完全没收到首帧)才显示 spinner。 */}
              {spectator && gameState && !gameState.ready ? (
                <p>
                  👁 {t('texasholdem.spectating' as TKey)} ·{' '}
                  {t('texasholdem.spectatingWaiting' as TKey, {
                    count: gameState.players?.filter((p) => p.has_player).length ?? 0,
                  } as any)}
                </p>
              ) : (
                <>
                  <p>
                    {t(
                      (spectator
                        ? 'texasholdem.spectating'
                        : 'texasholdem.waiting') as TKey,
                    )}
                  </p>
                  <div className="spinner" />
                </>
              )}
            </div>
          ) : (
            <>
              <TexasHoldemTable
                gameState={gameState!}
                mySeat={effectiveSeat}
                style={style}
              />
              {!spectator && isPlaying && (
                <ActionControls
                  isMyTurn={isMyTurn}
                  canCheck={canCheck}
                  canCall={canCall}
                  callAmount={callAmount}
                  isOver={isOver}
                  gameOver={gameOver}
                  onAction={handleAction}
                  bigBlind={gameState!.big_blind}
                  pot={gameState!.pot}
                  stack={self?.stack ?? 0}
                  // 2026-08-20 §德州扑克Agent — 当轮到 bot 且 bot_thinking=true 时,
                  // ActionControls 渲染「🤖 AI 决策中…」提示。
                  botTurnThinking={
                    !isMyTurn &&
                    !!(gameState?.bot_seats?.[gameState!.turn]) &&
                    !!gameState?.bot_thinking?.[gameState!.turn]
                  }
                />
              )}
              {isOver && gameOver && (
                <div className="game-over-banner">
                  <p>
                    {t('texasholdem.winners' as TKey)}{' '}
                    {gameOver.winners.join(', ')}
                  </p>
                </div>
              )}
            </>
          )}
        </div>
        {/* §20260821-02 — 三栏布局:信息(左) + 牌桌(中) + 聊天(右)。
            原 .game-sidebar 单列 280px 拆为两个独立列,
            grid-template-columns: 240px minmax(0,1fr) 300px(详情见 texasholem.css)。 */}
        <div className="game-info-column">
          <GameInfoPanel
            gameState={gameState}
            mySeat={effectiveSeat}
            style={style}
            spectator={spectator}
            onResign={handleResign}
            onLeave={handleLeave}
          />
        </div>
        <div className="game-chat-column">
          <GameChatPanel roomId={roomId} />
        </div>
      </div>
      {resignPromptOpen && (
        <ConfirmModal
          messageKey={'texasholdem.confirmResign' as TKey}
          danger
          onConfirm={confirmResign}
          onCancel={cancelResign}
        />
      )}
    </div>
  );
}
