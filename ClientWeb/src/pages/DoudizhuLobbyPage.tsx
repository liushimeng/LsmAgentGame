import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useDoudizhuStore } from '@/store/doudizhu.store';
import { useT } from '@/hooks/useT';
import { AnteDoudizhuSelector } from '@/components/doudizhu/AnteDoudizhuSelector';
import { AppModal } from '@/components/ui/AppModal';
import { useWallet } from '@/hooks/useWallet';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { RoomListTable } from '@/components/lobby/RoomListTable';
import type { TKey } from '@/i18n';

export function DoudizhuLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, setGameOver, setAnteHint, patchRoom, removeRoom } = useDoudizhuStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [ante, setAnte] = useState<number>(100);

  const { balance, refresh } = useWallet();

  const fetchRooms = useCallback(() => {
    roomService
      .list('doudizhu')
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
  useLobbyLiveUpdate({ gameKind: 'doudizhu', updateRoom: patchRoom, removeRoom });

  const handleOpenCreate = async () => {
    setErr('');
    await refresh();
    setShowCreateDialog(true);
  };

  const handleConfirmCreate = async () => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.create('doudizhu', { ante });
      setAnteHint(ante);
      setShowCreateDialog(false);
      nav(`/doudizhu/${detail.id}`);
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
      // Best-effort ante hint: room detail may carry it. Backend is authoritative.
      // For MVP we leave anteHint null for joiners — they can see it via game.state.multiplier.
      void detail;
      setGameOver(null);
      nav(`/doudizhu/${roomId}`);
    } catch (e: any) {
      if (e.code === 30003) {
        setGameOver(null);
        nav(`/doudizhu/${roomId}`);
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
      nav(`/doudizhu/spectate/${roomId}`);
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
    <div className="lobby doudizhu-lobby">
      <div className="lobby-header">
        <h1>🃏 {t('doudizhu.title' as TKey)}</h1>
        <button className="btn btn-primary" onClick={handleOpenCreate} disabled={loading}>
          + {t('doudizhu.createRoom' as TKey)}
        </button>
      </div>
      {err && <div className="error">{err}</div>}
      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('doudizhu.noRooms' as TKey)}
        emptySub={t('doudizhu.createFirst' as TKey)}
      />

      {showCreateDialog && (
        <AppModal
          title={t('doudizhu.createRoom' as TKey)}
          icon="🃏"
          kind="info"
          maxWidth={520}
          dismissible={!loading}
          blockBackdropClose={!loading}
          loading={loading}
          blockHint="请点击「创建房间」或「取消」按钮关闭,误点外面不会丢失已选底注"
          testId="doudizhu-create-modal"
          footer={
            <>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setShowCreateDialog(false)}
                disabled={loading}
                data-testid="doudizhu-create-cancel"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={handleConfirmCreate}
                disabled={loading || (balance != null && balance < ante * 4)}
                data-testid="doudizhu-create-confirm"
              >
                {loading ? t('common.loading') : t('doudizhu.createRoom' as TKey)}
              </button>
            </>
          }
          onClose={() => setShowCreateDialog(false)}
        >
          <div className="doudizhu-create-dialog">
            <AnteDoudizhuSelector value={ante} onChange={setAnte} />

            {balance != null && balance < ante * 4 && (
              <div className="doudizhu-create-dialog__insufficient error">
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
