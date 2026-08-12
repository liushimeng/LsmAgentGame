import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useWerewolfStore } from '@/store/werewolf.store';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { AgentSeatRequest, CreateRoomOptions } from '@/types/api';
import RoomCreateModal from '@/components/werewolf/RoomCreateModal';
import { RoomListTable } from '@/components/lobby/RoomListTable';
// 2026-07-30 §R210-05: 把"我在房间中的角色"缓存到 sessionStorage,
// GamePage mount 时先读,已是 player 时跳过 game.join WS(避免被 30001 拒绝)。
import { WriteCachedRoomRole, ReadCachedRoomRole } from '@/services/sessionRoomRole';

/**
 * 狼人杀 13 人标准局 — 大厅
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 * §38 自动测试统一入口:e2e 走 go-web-debug-tool,禁止新加 Python 脚本。
 *
 * 与 5 个既有 Lobby 页同构:
 *   - 列表 5s HTTP 轮询 + room.state WS 推送(由 useLobbyLiveUpdate 接入)
 *   - 创建:无 ante(狼人杀无底注),点击后直接进房入座
 *   - 加入:HTTP room join → 进游戏页
 */
export function WerewolfLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, setGameOver, patchRoom, removeRoom } = useWerewolfStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [createModalOpen, setCreateModalOpen] = useState(false);
  // BUG-R200-P2-05 (2026-07-30): 记录当前用户在各房间的角色(player/spectator),
  // 供 RoomListTable 渲染"进入房间"/"👁 观战"按钮。Key=roomId, Value=role。
  const [myRoles, setMyRoles] = useState<Record<string, 'player' | 'spectator'>>({});

  const fetchRooms = useCallback(() => {
    return roomService
      .list('werewolf')
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

  useLobbyLiveUpdate({ gameKind: 'werewolf', updateRoom: patchRoom, removeRoom });

  // BUG-R210-01 (2026-07-30): 刷新后从 server-authoritative `rooms[].my_role`
  // 派生 myRoles。ListRooms 后端会一次性带 my_role(单 query join,无 N+1),
  // 所以每次 fetchRooms / 5s 轮询后这里自动刷新 — 之前 myRoles 是纯组件
  // state,刷新就清零,导致 playing 状态房间只剩"已满"按钮。
  useEffect(() => {
    const next: Record<string, 'player' | 'spectator'> = { ...myRoles };
    let changed = false;
    for (const r of rooms) {
      if (!r.id) continue;
      if (r.my_role === 'player' || r.my_role === 'agent') {
        if (next[r.id] !== 'player') {
          next[r.id] = 'player';
          changed = true;
        }
      } else if (r.my_role === 'spectator') {
        if (next[r.id] !== 'spectator') {
          next[r.id] = 'spectator';
          changed = true;
        }
      }
    }
    if (changed) setMyRoles(next);
    // 故意不依赖 myRoles — 我们只读它,避免无限循环。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rooms]);

  // Open the AI config modal. The modal calls handleCreateWithAgents with the
  // chosen agent_seats payload, or falls back to an all-human room when empty.
  const handleCreate = () => {
    setErr('');
    setCreateModalOpen(true);
  };

  const handleCreateWithAgents = async (
    agentSeats: AgentSeatRequest[],
    name?: string,
    judge?: NonNullable<CreateRoomOptions['judge']>,
    creatorRole?: string,
    agentDifficulty?: NonNullable<CreateRoomOptions['agent_difficulty']>,
  ): Promise<boolean> => {
    setLoading(true);
    setErr('');
    try {
      // 房间名(可选):RoomCreateModal 收集的 req.name 透传给后端;为空时后端
      // 自动生成「狼人杀房间-{短ID}》。历史 BUG:onSubmit 丢弃了 req.name,
      // 导致「房间名(可选)」输入框形同虚设——此处补齐。
      // 2026-07-16 主持人 Agent 重构 — 有 Agent 时透传 judge 设置。
      const opts: CreateRoomOptions = agentSeats.length > 0
        ? {
            name,
            agent_seats: agentSeats,
            ...(judge ? { judge } : {}),
            ...(creatorRole ? { creator_role: creatorRole } : {}),
            ...(agentDifficulty && agentDifficulty !== 'normal'
              ? { agent_difficulty: agentDifficulty }
              : {}),
          }
        : { name, ...(creatorRole ? { creator_role: creatorRole } : {}) };
      const detail = await roomService.create('werewolf', opts);
      setGameOver(null);
      setCreateModalOpen(false);
      // BUG-R229-P2-01 (2026-08-01): POST 200 后 modal 不关闭不导航。
      // 根因: setMyRoles / WriteCachedRoomRole / fetchRooms 在 nav 之前抛错,
      // 异常冒泡至外层 catch 被当作 sessionExpired 静默忽略,nav 永不执行。
      // 修复: 把导航提到所有副作用之前,副作用整体 try/catch 兜底 —— 任何
      // 副作用失败都不应阻塞导航(对局已在 DB 内,用户必须能进房)。
      const isSpectator = detail.my_role === 'spectator';
      if (isSpectator) {
        nav(`/werewolf/spectate/${detail.id}`);
      } else {
        nav(`/werewolf/${detail.id}`);
      }
      // 以下为 best-effort 副作用,失败仅 log,绝不阻塞导航。
      try {
        // BUG-R200-P1-03 (2026-07-30): 创建成功后立即刷新列表,避免用户等满
        // 5s 自动轮询才看到自己创建的房间导致「误以为没创建成功」反复点击。
        // no-await: 刷新失败不影响进房。
        fetchRooms().catch((refreshErr: any) => {
          if (!isSessionExpiredError(refreshErr)) {
            // eslint-disable-next-line no-console
            console.warn('werewolf lobby: post-create list refresh failed',
              { room_id: detail.id, error: refreshErr?.message ?? String(refreshErr) });
          }
        });
        // BUG-R200-P2-05 (2026-07-30): 改用服务端显式下发的 my_role 决定路由。
        const roleKey: 'player' | 'spectator' = isSpectator ? 'spectator' : 'player';
        setMyRoles((prev) => ({ ...prev, [detail.id]: roleKey }));
        // 2026-07-30 §R210-05: 把"我在该房间的角色"也写入 sessionStorage,
        // GamePage mount 时据此跳过 game.join WS(避免 30001)。
        if (detail.my_role === 'player' || detail.my_role === 'agent') {
          WriteCachedRoomRole(detail.id, 'player');
        } else if (detail.my_role === 'spectator') {
          WriteCachedRoomRole(detail.id, 'spectator');
        }
      } catch (sideEffectErr: any) {
        if (!isSessionExpiredError(sideEffectErr)) {
          // eslint-disable-next-line no-console
          console.warn('werewolf lobby: post-create side-effect failed (nav already fired)',
            { room_id: detail.id, error: sideEffectErr?.message ?? String(sideEffectErr) });
        }
      }
      return true;
    } catch (e: any) {
      if (!isSessionExpiredError(e)) {
        // BUG-R210-04 (2026-07-30): 不要关闭弹窗,让 RoomCreateModal
        // 就地显示错误条(对齐 CLAUDE.md §7.1)。返回 false 让弹窗保留。
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      } else {
        // session expired:弹窗交由登录后处理,但这里关掉避免挡路。
        setCreateModalOpen(false);
      }
      return false;
    } finally {
      setLoading(false);
    }
  };

  const handleJoin = async (roomId: string) => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.join(roomId);
      setGameOver(null);
      // BUG-R200-P2-05 (2026-07-30): 记录加入后的角色,供 RoomListTable 后续渲染。
      // join 接口返回的 my_role 是权威值(player/agent/spectator)。
      const role = detail.my_role === 'spectator' ? 'spectator' : 'player';
      setMyRoles((prev) => ({ ...prev, [roomId]: role }));
      // 2026-07-30 §R210-05: 同步写 sessionStorage。
      if (detail.my_role === 'player' || detail.my_role === 'agent') {
        WriteCachedRoomRole(roomId, 'player');
      } else if (detail.my_role === 'spectator') {
        WriteCachedRoomRole(roomId, 'spectator');
      }
      // spectator 角色走观众路由,player/agent 走玩家路由。
      if (role === 'spectator') {
        nav(`/werewolf/spectate/${roomId}`);
      } else {
        nav(`/werewolf/${roomId}`);
      }
    } catch (e: any) {
      // 2026-07-30 §R210-05: 30001 (room is full) + 30003 (room full) —
      // playing 房间永远是这两个错误码。此时用 myRoles 缓存或 sessionStorage
      // 解析用户真实角色,走直接路由而非继续 join。
      if (e.code === 30001 || e.code === 30003) {
        const knownRole = myRoles[roomId] ?? (
          ReadCachedRoomRole(roomId) === 'player' ? 'player' :
          ReadCachedRoomRole(roomId) === 'spectator' ? 'spectator' : undefined
        );
        if (knownRole === 'player') {
          setGameOver(null);
          nav(`/werewolf/${roomId}`);
          return;
        }
        if (knownRole === 'spectator') {
          nav(`/werewolf/spectate/${roomId}`);
          return;
        }
        // 兜底: 跳玩家路由,GamePage 端 30012 自动纠正。
        setGameOver(null);
        nav(`/werewolf/${roomId}`);
      } else if (e.code === 30012) {
        // BUG-R210-03 (2026-07-30): 已是 spectator 试图 join — 跳观众路由。
        setMyRoles((prev) => ({ ...prev, [roomId]: 'spectator' }));
        WriteCachedRoomRole(roomId, 'spectator');
        nav(`/werewolf/spectate/${roomId}`);
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

  // 2026-07-30 §R210-05: RoomListTable 在「已知角色」时主按钮走 onNavigate,
  // 直接跳路由,不再发 join HTTP。这样 playing 房间的玩家点「进入房间」
  // 不会再被 30001 拒绝。
  const handleNavigateTo = (roomId: string, role: 'player' | 'spectator') => {
    setLoading(false);
    setErr('');
    if (role === 'spectator') {
      nav(`/werewolf/spectate/${roomId}`);
    } else {
      nav(`/werewolf/${roomId}`);
    }
  };

  const handleSpectate = async (roomId: string) => {
    setLoading(true);
    setErr('');
    try {
      await roomService.spectate(roomId);
      // BUG-R200-P2-05 (2026-07-30): 记录 spectator 角色,供 RoomListTable 后续渲染。
      setMyRoles((prev) => ({ ...prev, [roomId]: 'spectator' }));
      // 2026-07-30 §R210-05: 写入 sessionStorage。
      WriteCachedRoomRole(roomId, 'spectator');
      nav(`/werewolf/spectate/${roomId}`);
    } catch (e: any) {
      if (e.code === 30012) {
        // BUG-R210-03 (2026-07-30): 已是 player 试图 spectate — 跳玩家路由
        // 而不是弹 "already in this room under a different role"。
        setMyRoles((prev) => ({ ...prev, [roomId]: 'player' }));
        WriteCachedRoomRole(roomId, 'player');
        nav(`/werewolf/${roomId}`);
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

  return (
    <div className="lobby werewolf-lobby">
      <div className="lobby-header">
        <h1>🐺 {t('werewolf.title' as TKey)}</h1>
        <button className="btn btn-primary" onClick={handleCreate} disabled={loading}>
          + {t('werewolf.createRoom' as TKey)}
        </button>
      </div>
      <RoomCreateModal
        open={createModalOpen}
        onClose={() => setCreateModalOpen(false)}
        onSubmit={(req) => handleCreateWithAgents(req.agent_seats, req.name, req.judge, req.creator_role, req.agent_difficulty)}
        submitting={loading}
      />
      {err && <div className="error">{err}</div>}

      <p className="lobby-hint">{t('werewolf.capacityHint' as TKey)}</p>

      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onNavigate={handleNavigateTo}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('werewolf.noRooms' as TKey)}
        emptySub={t('werewolf.createFirst' as TKey)}
        myRoles={myRoles}
      />
    </div>
  );
}
