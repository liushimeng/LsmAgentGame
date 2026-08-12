import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useXiangqiStore } from '@/store/xiangqi.store';
import { useT } from '@/hooks/useT';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { RoomListTable } from '@/components/lobby/RoomListTable';

export function XiangqiLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, patchRoom, removeRoom } = useXiangqiStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  const fetchRooms = () => {
    roomService
      .list('xiangqi')
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
  useLobbyLiveUpdate({ gameKind: 'xiangqi', updateRoom: patchRoom, removeRoom });

  const handleCreate = async () => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.create('xiangqi');
      nav(`/xiangqi/${detail.id}`);
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
      nav(`/xiangqi/${roomId}`);
    } catch (e: any) {
      // ErrRoomAlreadyIn (30003) — allow navigation for reconnecting players.
      if (e.code === 30003) {
        nav(`/xiangqi/${roomId}`);
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

  // Enter the room as a spectator — does NOT consume a seat and does NOT
  // require the room to have any open seats.
  const handleSpectate = async (roomId: string) => {
    setLoading(true);
    setErr('');
    try {
      await roomService.spectate(roomId);
      nav(`/xiangqi/spectate/${roomId}`);
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
    <div className="xiangqi-lobby">
      <div className="lobby-header">
        <h1>♟ {t('xiangqi.title')}</h1>
        <button className="btn btn-primary" onClick={handleCreate} disabled={loading}>
          + {t('xiangqi.createRoom')}
        </button>
      </div>

      {err && <div className="error">{err}</div>}

      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('xiangqi.noRooms')}
        emptySub={t('xiangqi.createFirst')}
      />
    </div>
  );
}
