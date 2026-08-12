import { useEffect, useCallback, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useDoudizhuStore } from '@/store/doudizhu.store';
import { useDoudizhu } from '@/hooks/useDoudizhu';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { DoudizhuTable } from '@/components/doudizhu/DoudizhuTable';
import { BidPanel } from '@/components/doudizhu/BidPanel';
import { PlayControls } from '@/components/doudizhu/PlayControls';
import { GameInfoPanel } from '@/components/doudizhu/GameInfoPanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';
import { useT } from '@/hooks/useT';
import { wsClient } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import type { TKey } from '@/i18n';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

export function DoudizhuGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();
  const [resignPromptOpen, setResignPromptOpen] = useState(false);

  const { gameState, mySeat, style, selectedCards, gameOver, reset, toggleCard, clearSelected } =
    useDoudizhuStore();

  const {
    joinGame,
    spectate,
    unspectate,
    bid,
    play,
    pass,
    resign,
    leaveGame,
    requestState,
  } = useDoudizhu(roomId!);

  // 连接并加入（或订阅观战）。
  useEffect(() => {
    if (!roomId) return;
    // 进入对局前清空上一次会话的 store 残留(选牌 / 座位 / 结算),避免 observer
    // 路由上看到旧手牌或旧手数;再调 useSessionRestore 补 game.spectate/state。
    reset();
    // 不重复连接（AppLayout 已管理连接），但确保连接状态。
    let retries = 0;
    const tryHook = () => {
      if (retries++ > 10) return;
      const frame = spectator ? 'game.spectate' : 'game.join';
      const sent = wsClient.send(frame, {
        room_id: roomId,
        game_kind: 'doudizhu',
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
    nav('/doudizhu');
  }, [roomId, spectator, leaveGame, unspectate, reset, nav]);

  const handleBid = useCallback(
    (score: number) => {
      bid(score);
    },
    [bid],
  );

  const handlePlay = useCallback(() => {
    if (!gameState) return;
    const cards = gameState.my_hand.filter((_, i) => selectedCards.has(i));
    if (cards.length === 0) return;
    play(cards);
    clearSelected();
  }, [gameState, selectedCards, play, clearSelected]);

  const handlePass = useCallback(() => {
    pass();
    clearSelected();
  }, [pass, clearSelected]);

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  // Spectators never receive a my_seat from the server; force -1 so the player
  // branches (bid panel, play controls) stay closed.
  const effectiveSeat = spectator ? -1 : mySeat;
  const isWaiting = !gameState || !gameState.ready;
  const isBidding = gameState?.phase === 'bidding';
  const isPlaying = gameState?.phase === 'playing';
  const isOver = gameState?.phase === 'over';

  return (
    <div className="doudizhu-game">
      <div className="game-area">
        <div className="board-container">
          {isWaiting ? (
            <div className="waiting-board">
              <p>{t(spectator ? 'doudizhu.spectating' as TKey : 'doudizhu.waiting' as TKey)}</p>
              <div className="spinner" />
            </div>
          ) : (
            <>
              <DoudizhuTable
                gameState={gameState!}
                mySeat={effectiveSeat}
                style={style}
                selectedCards={selectedCards}
                onToggleCard={spectator ? () => {} : toggleCard}
              />
              {!spectator && isBidding && (
                <BidPanel
                  bids={gameState!.bids}
                  currentBid={gameState!.current_bid}
                  mySeat={effectiveSeat}
                  turn={gameState!.turn}
                  onBid={handleBid}
                />
              )}
              {!spectator && (isPlaying || isOver) && (
                <PlayControls
                  isMyTurn={gameState!.turn === effectiveSeat && isPlaying}
                  canPass={gameState!.last_play !== null && gameState!.last_play.seat !== effectiveSeat}
                  isOver={isOver}
                  gameOver={gameOver}
                  onPlay={handlePlay}
                  onPass={handlePass}
                />
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
          messageKey={'doudizhu.confirmResign' as TKey}
          danger
          onConfirm={confirmResign}
          onCancel={cancelResign}
        />
      )}
    </div>
  );
}
