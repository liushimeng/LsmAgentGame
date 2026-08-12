import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useTexasHoldemStore } from '@/store/texasholdem.store';
import { useT } from '@/hooks/useT';
import { BlindSelector, buyInRange } from '@/components/ui/BlindSelector';
import { BuyinSlider } from '@/components/ui/BuyinSlider';
import { AppModal } from '@/components/ui/AppModal';
import { useWallet } from '@/hooks/useWallet';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { RoomListTable } from '@/components/lobby/RoomListTable';
import type { TKey } from '@/i18n';

/** Server-side expects ante = SB ( BB/2 ) when options.ante is given. */
function texasAnteFromBb(bb: number) {
  return bb / 2;
}

export function TexasHoldemLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, setGameOver, patchRoom, removeRoom } = useTexasHoldemStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [bb, setBb] = useState<number>(50);
  const [buyin, setBuyin] = useState<number>(buyInRange(bb).defaultBI);

  const { balance, refresh } = useWallet();

  // Keep buy-in in sync when BB changes.
  const handleBbChange = useCallback(
    (nextBb: number) => {
      const { defaultBI } = buyInRange(nextBb);
      setBb(nextBb);
      setBuyin(defaultBI);
    },
    [],
  );

  const fetchRooms = useCallback(() => {
    roomService
      .list('texasholdem')
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
  useLobbyLiveUpdate({ gameKind: 'texasholdem', updateRoom: patchRoom, removeRoom });

  const handleOpenCreate = async () => {
    setErr('');
    await refresh();
    setShowCreateDialog(true);
  };

  const handleConfirmCreate = async () => {
    setLoading(true);
    setErr('');
    try {
      // The ante field is communicated to the backend via the `ante` option;
      // for texas hold'em it equals SB = BB/2 per the engine spec.
      const detail = await roomService.create('texasholdem', {
        ante: texasAnteFromBb(bb),
      });
      setShowCreateDialog(false);
      // Pass buy-in via query param so the game page knows user's intent.
      // (The actual buy-in debit happens on join, server-authoritative.)
      nav(`/texasholdem/${detail.id}?buyin=${buyin}`);
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
      await roomService.join(roomId);
      setGameOver(null);
      nav(`/texasholdem/${roomId}`);
    } catch (e: any) {
      if (e.code === 30003) {
        setGameOver(null);
        nav(`/texasholdem/${roomId}`);
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
      nav(`/texasholdem/spectate/${roomId}`);
    } catch (e: any) {
      if (!isSessionExpiredError(e)) {
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      }
    } finally {
      setLoading(false);
    }
  };

  const { min, max } = buyInRange(bb);
  const minOk = balance == null || balance >= min;

  return (
    <div className="lobby texas-lobby">
      <div className="lobby-header">
        <h1>🎰 {t('texasholdem.title' as TKey)}</h1>
        <button className="btn btn-primary" onClick={handleOpenCreate} disabled={loading}>
          + {t('texasholdem.createRoom' as TKey)}
        </button>
      </div>
      {err && <div className="error">{err}</div>}
      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('texasholdem.noRooms' as TKey)}
        emptySub={t('texasholdem.createFirst' as TKey)}
      />

      {showCreateDialog && (
        <AppModal
          title={t('texasholdem.createRoom' as TKey)}
          icon="♠️"
          kind="info"
          maxWidth={520}
          dismissible={!loading}
          blockBackdropClose={!loading}
          loading={loading}
          blockHint="请点击「创建房间」或「取消」按钮关闭,误点外面不会丢失已选盲注/带入"
          testId="texas-create-modal"
          footer={
            <>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowCreateDialog(false)}
                disabled={loading}
                data-testid="texas-create-cancel"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={handleConfirmCreate}
                disabled={loading || !minOk || buyin < min || buyin > max}
                data-testid="texas-create-confirm"
              >
                {loading ? t('common.loading') : t('texasholdem.createRoom' as TKey)}
              </button>
            </>
          }
          onClose={() => setShowCreateDialog(false)}
        >
          <div className="texas-create-dialog">
            <section className="texas-create-section">
              <BlindSelector value={bb} onChange={handleBbChange} />
            </section>

            <section className="texas-create-section">
              <BuyinSlider bb={bb} value={buyin} onChange={setBuyin} />
            </section>

            {!minOk && (
              <div className="texas-create-dialog__insufficient error">
                {t('ante.insufficient' as TKey)} —{' '}
                <button
                  className="link-btn"
                  onClick={() => window.dispatchEvent(new CustomEvent('wallet:claimDaily'))}
                >
                  {t('ante.goClaim' as TKey)}
                </button>
              </div>
            )}
          </div>
        </AppModal>
      )}
    </div>
  );
}
