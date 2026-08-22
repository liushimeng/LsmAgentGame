import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useTexasHoldemStore } from '@/store/texasholdem.store';
import { useT } from '@/hooks/useT';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { RoomListTable } from '@/components/lobby/RoomListTable';
import { RoomCreateModal } from '@/components/texasholdem/RoomCreateModal';
import type { TKey } from '@/i18n';

export function TexasHoldemLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, setGameOver, patchRoom, removeRoom } = useTexasHoldemStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  const [showCreateDialog, setShowCreateDialog] = useState(false);

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

  const handleOpenCreate = () => {
    setErr('');
    setShowCreateDialog(true);
  };

  // 2026-08-19 §德州扑克Agent — 使用 RoomCreateModal 提交(含 agent_seats)
  //
  // 2026-08-22 §BUG-TEXAS-CREATE-OVERLAY — bb/buyin 已在 RoomCreateModal 内部
  // 持有,这里通过 req.big_blind / req.start_stack 接收;盲注档位选择器已移入
  // 弹窗内(避免被全屏遮罩挡住)。
  const handleCreateSubmit = async (req: {
    name?: string;
    agent_seats: Array<{ seat: number; model_key: string }>;
    big_blind: number;
    start_stack: number;
  }): Promise<boolean> => {
    try {
      const detail = await roomService.create('texasholdem', {
        name: req.name,
        big_blind: req.big_blind,
        start_stack: req.start_stack,
        agent_seats: req.agent_seats.length > 0 ? req.agent_seats : undefined,
      });
      setShowCreateDialog(false);
      nav(`/texasholdem/${detail.id}`);
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

      {/* 2026-08-19 §德州扑克Agent — RoomCreateModal 含 AI 座位配置 + 盲注/买入选择器 */}
      <RoomCreateModal
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
        onSubmit={handleCreateSubmit}
        submitting={loading}
      />
    </div>
  );
}
