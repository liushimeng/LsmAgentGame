/**
 * 虚拟城市 — 大厅页（/wealth）。
 *
 * 与既有 6 款 Lobby 页同构：banner 头图（PNG 缺失回落 CSS 渐变）+ 房间列表
 * （RoomListTable 复用 + 5s HTTP 轮询 + useLobbyLiveUpdate room.state WS 实时更新）
 * + 建房弹窗（WealthCreateRoomModal：agent_seats / month_ms / pool / seed）
 * + 观战入口。加入 / 观战错误处理对齐 WerewolfLobbyPage（30001/30003/30012）。
 */

import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useWealthStore } from '@/store/wealth.store';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { RoomListTable } from '@/components/lobby/RoomListTable';
import {
  WealthCreateRoomModal,
  type WealthCreateRequest,
} from '@/components/wealth/WealthCreateRoomModal';
import { WEALTH_BANNER } from '@/assets/images/wealth';

export function WealthLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, patchRoom, removeRoom } = useWealthStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [bannerFailed, setBannerFailed] = useState(false);
  const [myRoles, setMyRoles] = useState<Record<string, 'player' | 'spectator'>>({});

  const fetchRooms = useCallback(() => {
    return roomService
      .list('wealth')
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

  useLobbyLiveUpdate({ gameKind: 'wealth', updateRoom: patchRoom, removeRoom });

  // 服务端权威 my_role → 按钮态（WerewolfLobbyPage 同款）。
  useEffect(() => {
    setMyRoles((prev) => {
      const next = { ...prev };
      let changed = false;
      for (const r of rooms) {
        if (!r.id) continue;
        const role: 'player' | 'spectator' | undefined =
          r.my_role === 'player' || r.my_role === 'agent'
            ? 'player'
            : r.my_role === 'spectator'
              ? 'spectator'
              : undefined;
        if (role && next[r.id] !== role) {
          next[r.id] = role;
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [rooms]);

  const handleCreate = useCallback(() => {
    setErr('');
    setCreateOpen(true);
  }, []);

  const handleCreateSubmit = useCallback(
    async (req: WealthCreateRequest): Promise<boolean> => {
      setLoading(true);
      setErr('');
      try {
        const detail = await roomService.create('wealth', {
          name: req.name,
          agent_seats: req.agent_seats,
          wealth: { month_ms: req.month_ms, pool: req.pool, ...(req.seed ? { seed: req.seed } : {}) },
          ...(req.full_agent !== undefined ? { full_agent: req.full_agent } : {}),
        });
        // 先导航，副作用 best-effort（BUG-R229 教训）。
        if (detail.my_role === 'spectator') {
          nav(`/wealth/spectate/${detail.id}`);
        } else {
          nav(`/wealth/${detail.id}`);
        }
        setCreateOpen(false);
        fetchRooms().catch(() => undefined);
        return true;
      } catch (e: any) {
        if (!isSessionExpiredError(e)) {
          setErr(e.message);
          reportGlobalError({ message: e.message, severity: 'error' });
        } else {
          setCreateOpen(false);
        }
        return false;
      } finally {
        setLoading(false);
      }
    },
    [nav, fetchRooms],
  );

  const handleJoin = async (roomId: string) => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.join(roomId);
      const role = detail.my_role === 'spectator' ? 'spectator' : 'player';
      setMyRoles((prev) => ({ ...prev, [roomId]: role }));
      if (role === 'spectator') {
        nav(`/wealth/spectate/${roomId}`);
      } else {
        nav(`/wealth/${roomId}`);
      }
    } catch (e: any) {
      if (e.code === 35013) {
        // 2026-09-19 §全Agent模式: 全 Agent 房间拒绝人类加入,自动跳转观战
        setMyRoles((prev) => ({ ...prev, [roomId]: 'spectator' }));
        nav(`/wealth/spectate/${roomId}`);
      } else if (e.code === 30001 || e.code === 30003) {
        // playing / 已满：已知角色直接走路由；未知兜底玩家路由由 GamePage 纠正。
        const known = myRoles[roomId];
        if (known === 'spectator') {
          nav(`/wealth/spectate/${roomId}`);
        } else {
          nav(`/wealth/${roomId}`);
        }
      } else if (e.code === 30012) {
        setMyRoles((prev) => ({ ...prev, [roomId]: 'spectator' }));
        nav(`/wealth/spectate/${roomId}`);
      } else if (!isSessionExpiredError(e)) {
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
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
      setMyRoles((prev) => ({ ...prev, [roomId]: 'spectator' }));
      nav(`/wealth/spectate/${roomId}`);
    } catch (e: any) {
      if (e.code === 30012) {
        setMyRoles((prev) => ({ ...prev, [roomId]: 'player' }));
        nav(`/wealth/${roomId}`);
        return;
      }
      if (!isSessionExpiredError(e)) {
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      }
    } finally {
      setLoading(false);
    }
  };

  const handleNavigateTo = (roomId: string, role: 'player' | 'spectator') => {
    setLoading(false);
    setErr('');
    if (role === 'spectator') nav(`/wealth/spectate/${roomId}`);
    else nav(`/wealth/${roomId}`);
  };

  return (
    <div className="lobby wealth-lobby">
      <div className="wealth-lobby__banner">
        {WEALTH_BANNER && !bannerFailed ? (
          <img
            className="wealth-lobby__banner-img"
            src={WEALTH_BANNER}
            alt={t('wealth.title' as TKey)}
            onError={() => setBannerFailed(true)}
          />
        ) : (
          <div className="wealth-lobby__banner-fallback" aria-hidden="true" />
        )}
        <div className="wealth-lobby__banner-text">
          <h1>💰 {t('wealth.title' as TKey)}</h1>
          <p>{t('wealth.subtitle' as TKey)}</p>
        </div>
        <button
          className="btn btn-primary wealth-lobby__create"
          data-testid="wealth-lobby__create-room"
          onClick={handleCreate}
          disabled={loading}
        >
          + {t('wealth.createRoom' as TKey)}
        </button>
      </div>

      {err && <div className="error">{err}</div>}
      <p className="lobby-hint">{t('wealth.lobbyHint' as TKey)}</p>

      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onNavigate={handleNavigateTo}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('wealth.noRooms' as TKey)}
        emptySub={t('wealth.createFirst' as TKey)}
        myRoles={myRoles}
      />

      {createOpen && (
        <WealthCreateRoomModal
          open={createOpen}
          onClose={() => setCreateOpen(false)}
          onSubmit={handleCreateSubmit}
          submitting={loading}
        />
      )}
    </div>
  );
}
