import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useJunqiStore } from '@/store/junqi.store';
import { useT } from '@/hooks/useT';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { RoomListTable } from '@/components/lobby/RoomListTable';
import type { TKey } from '@/i18n';

export function JunqiLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, patchRoom, removeRoom } = useJunqiStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  const fetchRooms = () => {
    roomService
      .list('junqi')
      .then((r) => setRooms(r ?? []))
      .catch((e: Error) => {
        if (!isSessionExpiredError(e)) {
          setErr(e.message);
          reportGlobalError({ message: e.message, severity: 'error' });
        }
      });
  };

  useEffect(() => {
    fetchRooms();
    const timer = setInterval(fetchRooms, 5000);
    return () => clearInterval(timer);
  }, []);

  // 订阅 room.state WS 帧,实时更新房间卡片;5s HTTP 轮询退化为兜底。
  useLobbyLiveUpdate({ gameKind: 'junqi', updateRoom: patchRoom, removeRoom });

  const handleCreate = async () => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.create('junqi');
      nav(`/junqi/${detail.id}`);
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
      nav(`/junqi/${roomId}`);
    } catch (e: any) {
      if (e.code === 30003) {
        nav(`/junqi/${roomId}`);
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
      nav(`/junqi/spectate/${roomId}`);
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
    <div className="junqi-lobby">
      <div className="lobby-header">
        <h1>🚩 {t('junqi.title' as TKey)}</h1>
        <button className="btn btn-primary" onClick={handleCreate} disabled={loading}>
          + {t('junqi.createRoom' as TKey)}
        </button>
      </div>

      {err && <div className="error">{err}</div>}

      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('junqi.noRooms' as TKey)}
        emptySub={t('junqi.createFirst' as TKey)}
      />
    </div>
  );
}