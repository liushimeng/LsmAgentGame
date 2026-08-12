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
import type { TKey } from '@/i18n';
import type { TexasActionType } from '@/types/texasholdem';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

export function TexasHoldemGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();
  const [resignPromptOpen, setResignPromptOpen] = useState(false);

  const { gameState, mySeat, style, gameOver, reset } = useTexasHoldemStore();
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
    return () => {
      clearTimeout(timer);
      clearInterval(stateTimer);
      if (spectator) unspectate();
    };
  }, [roomId, spectator, joinGame, spectate, unspectate, requestState, reset]);

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

  const handleLeave = useCallback(async () => {
    if (spectator) {
      unspectate();
      try { await roomService.leaveSpectate(roomId!); } catch { /* best-effort */ }
    } else {
      leaveGame();
      try { await roomService.leave(roomId!); } catch { /* best-effort */ }
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
  // canCheck / callAmount 必须使用 round_committed（本街下注），不是 chips_committed
  // (本手总投入，跨街累积，BUG-TEXAS-CANCHECK-USE-TOTAL)。
  // 历史 Bug #10：preflop chips_committed=$200 (BB + 跟注) vs current_bet 被
  // advanceToNextStreet 重置为 0 → 永不相等，但 UI 仍显示 "过牌" 按钮 → 用户点击
  // 必返回 "cannot check when there is a bet to call"。修复：改用 round_committed。
  const myRound = self?.round_committed ?? 0;
  const canCheck = isMyTurn && (gameState?.current_bet ?? 0) === myRound;
  const callAmount = Math.max(0, (gameState?.current_bet ?? 0) - myRound);
  const canCall = isMyTurn && callAmount > 0;

  return (
    <div className="texas-game">
      <div className="game-area">
        <div className="board-container">
          {isWaiting ? (
            <div className="waiting-board">
              <p>{t(spectator ? 'texasholdem.spectating' as TKey : 'texasholdem.waiting' as TKey)}</p>
              <div className="spinner" />
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
                />
              )}
              {isOver && gameOver && (
                <div className="game-over-banner">
                  <p>{t('texasholdem.winners' as TKey)} {gameOver.winners.join(', ')}</p>
                </div>
              )}
            </>
          )}
        </div>
        <div className="game-sidebar">
          <GameInfoPanel
            gameState={gameState}
            mySeat={effectiveSeat}
            style={style}
            spectator={spectator}
            onResign={handleResign}
            onLeave={handleLeave}
          />
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
