import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useTexasHoldemStore } from '@/store/texasholdem.store';
import { useT } from '@/hooks/useT';
import { BlindSelector, buyInRange } from '@/components/ui/BlindSelector';
import { BuyinSlider } from '@/components/ui/BuyinSlider';
import { useWallet } from '@/hooks/useWallet';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { RoomListTable } from '@/components/lobby/RoomListTable';
import { RoomCreateModal } from '@/components/texasholdem/RoomCreateModal';
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

  // 2026-08-19 §德州扑克Agent — 使用 RoomCreateModal 提交(含 agent_seats)
  const handleCreateSubmit = async (req: {
    name?: string;
    agent_seats: Array<{ seat: number; model_key: string }>;
  }): Promise<boolean> => {
    try {
      const detail = await roomService.create('texasholdem', {
        name: req.name,
        ante: texasAnteFromBb(bb),
        agent_seats: req.agent_seats.length > 0 ? req.agent_seats : undefined,
      });
      setShowCreateDialog(false);
      nav(`/texasholdem/${detail.id}?buyin=${buyin}`);
      return true;
    } catch (e: any) {
      if (!isSessionExpiredError(e)) {
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      }
      return false;
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

  const { min } = buyInRange(bb);
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

      {/* 2026-08-19 §德州扑克Agent — RoomCreateModal 含 AI 座位配置 */}
      <RoomCreateModal
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
        onSubmit={handleCreateSubmit}
        submitting={loading}
      />

      {/* 盲注/带入选择器 — 独立于 RoomCreateModal,在弹窗下方渲染 */}
      {showCreateDialog && (
        <div className="texas-create-dialog" style={{ marginTop: 8 }}>
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
      )}
    </div>
  );
}
