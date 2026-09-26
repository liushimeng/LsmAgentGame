/**
 * 虚拟城市 — 大厅页（/virtualCity）。
 *
 * 与既有 6 款 Lobby 页同构：banner 头图（PNG 缺失回落 CSS 渐变）+ 房间列表
 * （RoomListTable 复用 + 5s HTTP 轮询 + useLobbyLiveUpdate room.state WS 实时更新）
 * + 建房弹窗（VirtualCityCreateRoomModal：resident_count / month_ms / seed）
 * + 观战入口。加入 / 观战错误处理对齐 WerewolfLobbyPage（30001/30003/30012）。
 */

import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError, isTimeoutError } from '@/services/http';
import { reportGlobalError, errorMessage } from '@/services/globalError';
import { roomService } from '@/services/auth.service';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { RoomListTable } from '@/components/lobby/RoomListTable';
import {
  VirtualCityCreateRoomModal,
  type VirtualCityCreateRequest,
} from '@/components/virtualCity/VirtualCityCreateRoomModal';
import { VIRTUAL_CITY_BANNER } from '@/assets/images/virtualCity';

export function VirtualCityLobbyPage() {
  const t = useT();
  const nav = useNavigate();
  const { rooms, setRooms, patchRoom, removeRoom } = useVirtualCityStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [bannerFailed, setBannerFailed] = useState(false);
  const [myRoles, setMyRoles] = useState<Record<string, 'player' | 'spectator'>>({});

  const fetchRooms = useCallback(() => {
    return roomService
      .list('virtual_city')
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

  useLobbyLiveUpdate({ gameKind: 'virtual_city', updateRoom: patchRoom, removeRoom });

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
    async (req: VirtualCityCreateRequest): Promise<boolean> => {
      setLoading(true);
      setErr('');
      try {
        const detail = await roomService.create('virtual_city', {
          name: req.name,
          // §20260921 建房解耦 — 背景居民规模（顶层字段，仅 virtualCity 生效）。
          // 2026-09-22 §CityHuman全民驱动 — 不再发送 agent_seats/pool
          // （后端自动合成 12 抽样展示居民，档案唯一源 = 人物卡知识库）。
          resident_count: req.resident_count,
          // 2026-09-25 §建房400修复 — 键名 virtualCity → virtual_city 对齐后端
          // json tag（DisallowUnknownFields 严格校验，camelCase 会 400）。
          virtual_city: { month_ms: req.month_ms, ...(req.seed ? { seed: req.seed } : {}) },
          full_agent: req.full_agent === true,
          // 批次 20 文档 3 A2：市长选举启用（顶层字段，仅 virtualCity 生效）。
          civic_election_enabled: req.civic_election_enabled === true,
          // 2026-09-25 §LLM线路池配额 — 本房 Agent 并发线路数（顶层字段，
          // 仅 virtualCity 生效；undefined 不发送，后端按池总量运行）。
          llm_lines: req.llm_lines,
        });
        // 先导航，副作用 best-effort（BUG-R229 教训）。
        if (detail.my_role === 'spectator') {
          nav(`/virtual-city/spectate/${detail.id}`);
        } else {
          nav(`/virtual-city/${detail.id}`);
        }
        setCreateOpen(false);
        fetchRooms().catch(() => undefined);
        return true;
      } catch (e: any) {
        // 2026-09-25 §建房超时修复 — 建房 HTTP 超时(30s)≠ 建房失败:服务端极可能
        // 仍在跑冷启动链路并最终成功。必须关弹窗防重复提交 —— 全 Agent 城无 owner
        // 无法暂停/解散,重复建出的房会持续烧 LLM 配额。改为引导刷新列表观战
        // (§7.1 页面内联 + 全局 toast 两层表面都做;return true 让弹窗不叠加
        // 「create.failed」内联错误)。
        if (isTimeoutError(e)) {
          setCreateOpen(false);
          const hint = t('virtualCity.create.timeoutHint' as TKey);
          const msg = `${errorMessage(e, e?.message ?? '')} — ${hint}`;
          setErr(msg);
          reportGlobalError({ message: msg, severity: 'error' });
          fetchRooms().catch(() => undefined);
          return true;
        }
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
    [nav, fetchRooms, t],
  );

  const handleJoin = async (roomId: string) => {
    setLoading(true);
    setErr('');
    try {
      const detail = await roomService.join(roomId);
      const role = detail.my_role === 'spectator' ? 'spectator' : 'player';
      setMyRoles((prev) => ({ ...prev, [roomId]: role }));
      if (role === 'spectator') {
        nav(`/virtual-city/spectate/${roomId}`);
      } else {
        nav(`/virtual-city/${roomId}`);
      }
    } catch (e: any) {
      if (e.code === 35036) {
        // 2026-09-19 §全Agent模式: 全 Agent 房间拒绝人类加入,自动跳转观战。
        // 2026-09-25 §观战35036修复 — 后端实际返回 ErrVirtualCityFullAgentReject
        // = 35036(errcode.go);此处曾误用 P2 交易错误码旧编号(设计文档漂移),
        // 永不命中。
        setMyRoles((prev) => ({ ...prev, [roomId]: 'spectator' }));
        nav(`/virtual-city/spectate/${roomId}`);
      } else if (e.code === 30001 || e.code === 30003) {
        // playing / 已满：已知角色直接走路由；未知兜底玩家路由由 GamePage 纠正。
        const known = myRoles[roomId];
        if (known === 'spectator') {
          nav(`/virtual-city/spectate/${roomId}`);
        } else {
          nav(`/virtual-city/${roomId}`);
        }
      } else if (e.code === 30012) {
        setMyRoles((prev) => ({ ...prev, [roomId]: 'spectator' }));
        nav(`/virtual-city/spectate/${roomId}`);
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
      nav(`/virtual-city/spectate/${roomId}`);
    } catch (e: any) {
      if (e.code === 30012) {
        setMyRoles((prev) => ({ ...prev, [roomId]: 'player' }));
        nav(`/virtual-city/${roomId}`);
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
    if (role === 'spectator') nav(`/virtual-city/spectate/${roomId}`);
    else nav(`/virtual-city/${roomId}`);
  };

  return (
    <div className="lobby virtualCity-lobby">
      <div className="virtualCity-lobby__banner">
        {VIRTUAL_CITY_BANNER && !bannerFailed ? (
          <img
            className="virtualCity-lobby__banner-img"
            src={VIRTUAL_CITY_BANNER}
            alt={t('virtualCity.title' as TKey)}
            onError={() => setBannerFailed(true)}
          />
        ) : (
          <div className="virtualCity-lobby__banner-fallback" aria-hidden="true" />
        )}
        <div className="virtualCity-lobby__banner-text">
          <h1>💰 {t('virtualCity.title' as TKey)}</h1>
          <p>{t('virtualCity.subtitle' as TKey)}</p>
        </div>
        <button
          className="btn btn-primary virtualCity-lobby__create"
          data-testid="virtualCity-lobby__create-room"
          onClick={handleCreate}
          disabled={loading}
        >
          + {t('virtualCity.createRoom' as TKey)}
        </button>
      </div>

      {err && <div className="error">{err}</div>}
      <p className="lobby-hint">{t('virtualCity.lobbyHint' as TKey)}</p>

      <RoomListTable
        rooms={rooms}
        onJoin={handleJoin}
        onSpectate={handleSpectate}
        onNavigate={handleNavigateTo}
        onRemove={removeRoom}
        busy={loading}
        emptyText={t('virtualCity.noRooms' as TKey)}
        emptySub={t('virtualCity.createFirst' as TKey)}
        myRoles={myRoles}
      />

      {createOpen && (
        <VirtualCityCreateRoomModal
          open={createOpen}
          onClose={() => setCreateOpen(false)}
          onSubmit={handleCreateSubmit}
          submitting={loading}
        />
      )}
    </div>
  );
}
