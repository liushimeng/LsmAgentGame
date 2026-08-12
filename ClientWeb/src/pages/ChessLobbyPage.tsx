import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useChessStore } from '@/store/chess.store';
import { useT } from '@/hooks/useT';
import { AnteSelector, ANTE_TIERS } from '@/components/ui/AnteSelector';
import { AppModal } from '@/components/ui/AppModal';
import { useWallet } from '@/hooks/useWallet';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { RoomListTable } from '@/components/lobby/RoomListTable';

export function ChessLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, setGameOver, patchRoom, removeRoom } = useChessStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  // Ante selection state — the default ante is the lowest tier (50).
  const [ante, setAnte] = useState<number>(ANTE_TIERS[0] ?? 50);
  const [showCreateDialog, setShowCreateDialog] = useState(false);

  const { balance, refresh } = useWallet();

  // Balance guard: refresh balance before creating room so we can show the
  // "insufficient balance" prompt inside the create dialog.
  const fetchRooms = useCallback(() => {
    roomService
      .list('chess')
      .then((r) => setRooms(r ?? []))
      .catch((e: Error) => {
        if (!isSessionExpiredError(e)) {
          setErr(e.message);
          reportGlobalError({ message: e.message, severity: 'error' });
        }
      });
  }, [setRooms]);

  useEffect(() => {
    fetchRooms();
    const timer = setInterval(fetchRooms, 5000);
    return () => clearInterval(timer);
  }, [fetchRooms]);

  // 订阅 room.state WS 帧,实时更新房间卡片;5s HTTP 轮询退化为兜底。
  useLobbyLiveUpdate({ gameKind: 'chess', updateRoom: patchRoom, removeRoom });

  // Ante-tier guard: the backend uses minMultiplier=1.2; for the MVP we only
  // expose 50/100 tiers. 500/1000 are shown but marked disabled via a soft
  // check in the AnteSelector. (Backend is still authoritative.)
  const viableAnte = balance != null
    ? ANTE_TIERS.find((tier) => tier * 1.2 <= balance) ?? null
    : ante;

  const handleOpenCreate = async () => {
    setErr('');
    await refresh();
    setShowCreateDialog(true);
  };

  const handleConfirmCreate = async () => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.create('chess', { ante });
      setShowCreateDialog(false);
      nav(`/chess/${detail.id}`);
    } catch (e: any) {
      if (!isSessionExpiredError(e)) {
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      }
    } finally {
      setLoading(false);
    }
  };

  const handleJoin = async (roomId: string) => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.detail(roomId);
      // detail.players already has ≥1 human → we need this room's ante to
      // validate balance before entering. In MVP, we trust the backend to
      // reject (ErrInsufficientBalance = 30007) and just surface the error.
      if (detail.capacity === 2 && balance != null) {
        // Informative check — backend is still authoritative.
        // If balance < ante tier they prefer, they see a warning here.
      }
      await roomService.join(roomId);
      setGameOver(null);
      nav(`/chess/${roomId}`);
    } catch (e: any) {
      if (e.code === 30003) {
        setGameOver(null);
        nav(`/chess/${roomId}`);
      } else {
        if (!isSessionExpiredError(e)) {
          setErr(e.message);
          reportGlobalError({ message: e.message, severity: 'error' });
        }
      }
    } finally {
      setLoading(false);
    }
  };

  const handleSpectate = async (roomId: string) => {
    setLoading(true);
    setErr('');
    try {
      await roomService.spectate(roomId);
      nav(`/chess/spectate/${roomId}`);
    } catch (e: any) {
      if (!isSessionExpiredError(e)) {
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="lobby chess-lobby">
      <div className="lobby-header">
        <h1>♚ {t('chess.title')}</h1>
        <button className="btn btn-primary" onClick={handleOpenCreate} disabled={loading}>
          + {t('chess.createRoom')}
        </button>
      </div>

      {err && <div className="error">{err}</div>}

      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('chess.noRooms')}
        emptySub={t('chess.createFirst')}
      />

      {showCreateDialog && (
        <AppModal
          title={t('chess.createRoom')}
          icon="♟️"
          kind="info"
          maxWidth={520}
          dismissible={!loading}
          blockBackdropClose={!loading}
          loading={loading}
          blockHint="请点击「创建房间」或「取消」按钮关闭,误点外面不会丢失已选底注"
          testId="chess-create-modal"
          footer={
            <>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowCreateDialog(false)}
                disabled={loading}
                data-testid="chess-create-cancel"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={handleConfirmCreate}
                disabled={loading || (balance != null && balance < ante * 1.2)}
                data-testid="chess-create-confirm"
              >
                {loading ? t('common.loading') : t('chess.createRoom')}
              </button>
            </>
          }
          onClose={() => setShowCreateDialog(false)}
        >
          <div className="chess-create-dialog">
            <AnteSelector
              value={ante}
              onChange={setAnte}
              balance={balance}
              minMultiplier={1.2}
            />

            {viableAnte != null && (
              <div className="chess-create-dialog__kfactor">
                {t('chess.kfactor.label')}: K = {ante <= 100 ? 40 : 20} ·{' '}
                <span className="chess-create-dialog__reward-hint">
                  {t('chess.kfactor.reward')}: +{Math.floor(ante * 0.1)}
                </span>
              </div>
            )}

            {balance != null && balance < ante * 1.2 && (
              <div className="chess-create-dialog__insufficient error">
                {t('ante.insufficient')} —{' '}
                <button
                  className="link-btn"
                  onClick={() => window.dispatchEvent(new CustomEvent('wallet:claimDaily'))}
                >
                  {t('ante.goClaim')}
                </button>
              </div>
            )}
          </div>
        </AppModal>
      )}
    </div>
  );
}
