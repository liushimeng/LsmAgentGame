import { useEffect, useCallback, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useJunqiStore } from '@/store/junqi.store';
import { useJunqi } from '@/hooks/useJunqi';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { JunqiBoard } from '@/components/junqi/JunqiBoard';
import { LayoutPanel } from '@/components/junqi/LayoutPanel';
import { GameInfoPanel } from '@/components/junqi/GameInfoPanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';
import { useT } from '@/hooks/useT';
import { wsClient } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import type { PieceColor } from '@/assets/images/junqi';
import type { TKey } from '@/i18n';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

export function JunqiGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();
  const [resignPromptOpen, setResignPromptOpen] = useState(false);

  const {
    gameState,
    myColor,
    selectedPos,
    lastMove,
    style,
    selectPos,
    reset,
  } = useJunqiStore();

  const {
    joinGame,
    spectate,
    unspectate,
    submitLayout,
    sendMove,
    resign,
    leaveGame,
    requestState,
  } = useJunqi(roomId!);

  // Connect WS and join (or spectate) the game room.
  useEffect(() => {
    if (!roomId) return;
    // 进入对局前清空上一次会话的 store 残留(选子 / 颜色 / 结算),避免 observer
    // 路由上看到旧棋盘或旧手数;再调 useSessionRestore 补 game.spectate/state。
    reset();
    wsClient.connect();

    let retries = 0;
    const tryHook = () => {
      if (retries++ > 10) return;
      const frame = spectator ? 'game.spectate' : 'game.join';
      const sent = wsClient.send(frame, {
        room_id: roomId,
        game_kind: 'junqi',
        mode: 'hidden',
      });
      if (sent === false) {
        setTimeout(tryHook, 500);
      }
    };
    const timer = setTimeout(tryHook, 300);

    // Also poll state periodically as a safety net.
    const stateTimer = setInterval(() => requestState(), 5000);

    return () => {
      clearTimeout(timer);
      clearInterval(stateTimer);
      if (spectator) unspectate();
    };
  }, [roomId, spectator, joinGame, spectate, unspectate, requestState, reset]);

  const handleSelect = useCallback(
    (pos: { x: number; y: number } | null) => selectPos(pos),
    [selectPos],
  );

  const handleMove = useCallback(
    (from: { x: number; y: number }, to: { x: number; y: number }) => sendMove(from, to),
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
    nav('/junqi');
  }, [roomId, spectator, leaveGame, unspectate, reset, nav]);

  const handleSubmitLayout = useCallback(
    (placements: import('@/types/junqi').JunqiPlacement[]) => {
      submitLayout(placements);
    },
    [submitLayout],
  );

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  // Spectators never receive a my_color from the server; force it null so the
  // player-only branches (layout submission, move handling) stay out of the
  // spectator's view.
  const effectiveColor = spectator ? null : myColor;
  const isLayoutPhase = !gameState || gameState.phase === 'layout';
  const hasBoard = gameState?.board_view && (gameState.phase === 'playing' || gameState.phase === 'over');

  return (
    <div className="junqi-game">
      <div className="game-area">
        <div className="board-container">
          {isLayoutPhase || !hasBoard ? (
            effectiveColor ? (
              <LayoutPanel
                myColor={effectiveColor as PieceColor}
                boardStyle={style}
                onSubmit={handleSubmitLayout}
              />
            ) : (
              <div className="waiting-board">
                <p>{t(spectator ? 'junqi.spectating' as TKey : 'junqi.waitingOpponent' as TKey)}</p>
                <div className="spinner" />
              </div>
            )
          ) : (
            <JunqiBoard
              boardView={gameState!.board_view!}
              myColor={(effectiveColor ?? 'red') as PieceColor}
              turn={(gameState!.turn ?? 'red') as PieceColor}
              selectedPos={spectator ? null : selectedPos}
              lastMove={lastMove}
              boardStyle={style}
              onSelect={spectator ? () => {} : handleSelect}
              onMove={spectator ? () => {} : handleMove}
            />
          )}
        </div>
        <div className="game-sidebar">
          <GameInfoPanel
            myColor={effectiveColor}
            spectator={spectator}
            onResign={handleResign}
            onLeave={handleLeave}
          />
          <GameChatPanel roomId={roomId} />
        </div>
      </div>
      {resignPromptOpen && (
        <ConfirmModal
          messageKey={'junqi.confirmResign' as TKey}
          danger
          onConfirm={confirmResign}
          onCancel={cancelResign}
        />
      )}
    </div>
  );
}