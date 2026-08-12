import { useEffect, useCallback, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useXiangqiStore } from '@/store/xiangqi.store';
import { useXiangqi } from '@/hooks/useXiangqi';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { useRoomPlayerStatus } from '@/hooks/useRoomPlayerStatus';
import { XiangqiBoard } from '@/components/xiangqi/XiangqiBoard';
import { GameInfoPanel } from '@/components/xiangqi/GameInfoPanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';
import { useT } from '@/hooks/useT';
import { wsClient } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import type { PieceColor } from '@/assets/images/xiangqi';
import type { TKey } from '@/i18n';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

export function XiangqiGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();
  const [resignPromptOpen, setResignPromptOpen] = useState(false);

  // 监听玩家掉线/重连状态
  const { disconnectedPlayers } = useRoomPlayerStatus({
    roomId: roomId || '',
    onPlayerRemoved: (userId) => {
      console.log(`Player ${userId} was removed from room due to timeout`);
    },
  });

  const {
    gameState,
    myColor,
    selectedPos,
    lastMove,
    style,
    selectPos,
    reset,
  } = useXiangqiStore();

  const { joinGame, spectate, unspectate, sendMove, resign, leaveGame, requestState } =
    useXiangqi(roomId!);

  // Connect WS and join (or spectate) the game room.
  useEffect(() => {
    if (!roomId) return;
    // 进入对局前清空上一次会话的 store 残留(选子 / 颜色 / 结算),避免 observer
    // 路由上看到旧棋盘或旧手数;再调 useSessionRestore 补 game.spectate/state。
    reset();
    wsClient.connect();

    // Decide once which frame to send, retry until the WS handshake lands.
    let retries = 0;
    const tryHook = () => {
      if (retries++ > 10) return; // give up after 5s
      const frame = spectator ? 'game.spectate' : 'game.join';
      const sent = wsClient.send(frame, { room_id: roomId });
      if (!sent) {
        setTimeout(tryHook, 500);
      }
    };
    const timer = setTimeout(tryHook, 300);
    // 周期同步服务端状态，避免 reload 后只看到初始空棋盘（Bug #4 修复：参照 DoudizhuGamePage）。
    const stateTimer = setInterval(() => requestState(), 8000);

    return () => {
      clearTimeout(timer);
      clearInterval(stateTimer);
      if (spectator) unspectate();
    };
  }, [roomId, spectator, joinGame, spectate, unspectate, requestState, reset]);

  const handleSelect = useCallback(
    (pos: { x: number; y: number } | null) => {
      selectPos(pos);
    },
    [selectPos],
  );

  const handleMove = useCallback(
    (from: { x: number; y: number }, to: { x: number; y: number }) => {
      sendMove(from, to);
    },
    [sendMove],
  );

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
      try {
        await roomService.leaveSpectate(roomId!);
      } catch {
        // best-effort
      }
    } else {
      leaveGame();
      try {
        await roomService.leave(roomId!);
      } catch {
        // best-effort
      }
    }
    reset();
    nav('/xiangqi');
  }, [roomId, spectator, leaveGame, unspectate, reset, nav]);

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  // Spectators see the board identically to players, but never receive a
  // `my_color` from the server (it's omitted), so we treat myColor as null in
  // that mode regardless of any stale store value.
  const effectiveColor = spectator ? null : myColor;
  const hasBoard = gameState?.board && gameState.ready;

  return (
    <div className="xiangqi-game">
      <div className="game-area">
        <div className="board-container">
          {hasBoard && effectiveColor ? (
            <XiangqiBoard
              board={gameState!.board!}
              myColor={effectiveColor}
              turn={(gameState!.turn as PieceColor) ?? 'red'}
              selectedPos={selectedPos}
              lastMove={lastMove}
              boardStyle={style}
              onSelect={handleSelect}
              onMove={handleMove}
            />
          ) : spectator ? (
            <div className="spectator-board">
              <p>{t('xiangqi.spectating')}</p>
              {hasBoard && gameState!.board && (
                <XiangqiBoard
                  board={gameState!.board!}
                  myColor={'red'} // ignored when readonly; we never call onMove
                  turn={(gameState!.turn as PieceColor) ?? 'red'}
                  selectedPos={null}
                  lastMove={lastMove}
                  boardStyle={style}
                  onSelect={() => {}}
                  onMove={() => {}}
                />
              )}
            </div>
          ) : (
            <div className="waiting-board">
              <p>{t('xiangqi.waitingOpponent')}</p>
              <div className="spinner" />
            </div>
          )}
        </div>
        <div className="game-sidebar">
          <GameInfoPanel
            myColor={effectiveColor}
            spectator={spectator}
            onResign={handleResign}
            onLeave={handleLeave}
          />
          {/* 显示玩家掉线状态 */}
          {disconnectedPlayers.size > 0 && (
            <div className="player-status-alert">
              <div className="alert alert-warning">
                {Array.from(disconnectedPlayers).map((userId) => (
                  <div key={userId} className="disconnect-notice">
                    <span className="spinner-sm" />
                    {t('xiangqi.playerDisconnecting', { userId })}
                  </div>
                ))}
                <div className="timeout-notice">
                  {t('xiangqi.autoRemoveNotice')}
                </div>
              </div>
            </div>
          )}
          <GameChatPanel roomId={roomId} />
        </div>
      </div>
      {resignPromptOpen && (
        <ConfirmModal
          messageKey={'xiangqi.confirmResign' as TKey}
          danger
          onConfirm={confirmResign}
          onCancel={cancelResign}
        />
      )}
    </div>
  );
}
