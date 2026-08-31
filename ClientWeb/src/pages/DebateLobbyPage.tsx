/**
 * 辩论比赛 — 大厅 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §2 大厅页设计 + 复用 5 款游戏大厅页风格。
 * 与 TexasHoldemLobbyPage / WerewolfLobbyPage 同构:
 *   - 5s HTTP 轮询 + room.state WS 推送(useLobbyLiveUpdate)
 *   - myRoles 状态(供房间卡片渲染「进入房间」/「👁 观战」按钮)
 *   - i18n 键 + useT()
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { debateService } from '@/api/debate';
import { useDebateStore } from '@/store/debate.store';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import DebateRoomCreateModal from '@/components/debate/DebateRoomCreateModal';
import type { DebateRoomSummary } from '@/types/debate';

export function DebateLobbyPage() {
  const nav = useNavigate();
  const { rooms, setRooms, patchRoom, removeRoom } = useDebateStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [myRoles, setMyRoles] = useState<Record<string, 'player' | 'spectator'>>({});
  const t = useT();

  const fetchRooms = useCallback(() => {
    return debateService
        .listRooms()
        .then((r) => setRooms(r ?? []))
        .catch((e: Error) => {
          if (!isSessionExpiredError(e)) {
            setErr(e.message);
            reportGlobalError({ message: e.message, severity: 'error' });
          }
        });
  }, [setRooms]);

  useEffect(() => {
    setLoading(true);
    fetchRooms().finally(() => setLoading(false));
  }, [fetchRooms]);

  // 5s 轮询兜底
  useEffect(() => {
    const timer = setInterval(() => { fetchRooms(); }, 5000);
    return () => clearInterval(timer);
  }, [fetchRooms]);

  // WS 实时更新(room.state 帧 — hook 期望 RoomInfo.id/status,做局部字段映射)
  useLobbyLiveUpdate({
    gameKind: 'debate',
    updateRoom: (r) =>
      patchRoom({
        room_id: r.id,
        spectator_count: r.current_count ?? 0,
        status: (r.status as 'waiting' | 'playing' | 'over') ?? 'waiting',
      }),
    removeRoom,
  });

  // 同步 myRoles(从 rooms 的 my_role 字段派生)
  useEffect(() => {
    const next: Record<string, 'player' | 'spectator'> = { ...myRoles };
    let changed = false;
    for (const r of rooms as DebateRoomSummary[]) {
      if (!r.room_id) continue;
      const myRole = (r as unknown as { my_role?: string }).my_role;
      if (myRole === 'player' || myRole === 'agent') {
        if (next[r.room_id] !== 'player') {
          next[r.room_id] = 'player';
          changed = true;
        }
      } else if (myRole === 'spectator') {
        if (next[r.room_id] !== 'spectator') {
          next[r.room_id] = 'spectator';
          changed = true;
        }
      }
    }
    if (changed) setMyRoles(next);
  }, [rooms, setMyRoles]);

  const handleSpectate = (roomId: string) => {
    debateService
      .spectate(roomId)
      .then(() => nav(`/debate/spectate/${roomId}`))
      .catch((e: Error) => {
        if (!isSessionExpiredError(e)) {
          reportGlobalError({ message: e.message, severity: 'error' });
        }
      });
  };

  const handleNavigate = (roomId: string, role: 'player' | 'spectator') => {
    if (role === 'player') {
      nav(`/debate/${roomId}`);
    } else {
      nav(`/debate/spectate/${roomId}`);
    }
  };

  const handleCreated = (roomId: string) => {
    setCreateOpen(false);
    nav(`/debate/${roomId}`);
  };

  return (
    <div className="page debate-lobby">
      <header className="page-header">
        <h1>🎓 {t('debate.title' as TKey)}</h1>
        <button className="btn-primary" onClick={() => setCreateOpen(true)} disabled={loading}>
          + {t('debate.createRoom' as TKey)}
        </button>
      </header>

      {err && <div className="error-bar">{err}</div>}

      <section className="room-list">
        {loading && <div className="loading">加载中...</div>}
        {!loading && rooms.length === 0 && (
          <div className="empty">
            <p>{t('debate.noRooms' as TKey)}</p>
            <p className="empty-hint">{t('debate.createFirst' as TKey)}</p>
          </div>
        )}
        {rooms.map((r) => (
          <div key={r.room_id} className={`room-card${myRoles[r.room_id] ? ' room-card--mine' : ''}`}>
            <div className="room-card__topic">
              <h3>{r.topic.text}</h3>
              <span className="mode-tag">
                {r.mode === 'two_team' && '双队对抗'}
                {r.mode === 'three_team' && '三队辩论'}
                {r.mode === 'four_team' && 'BP 制'}
                {r.mode === 'five_team' && '五队发散'}
              </span>
            </div>
            <div className="room-card__meta">
              <span className={`status status--${r.status}`}>
                {r.status === 'waiting' && '等待开始'}
                {r.status === 'playing' && '进行中'}
                {r.status === 'over' && '已结束'}
              </span>
              <span className="phase">{r.phase_cn}</span>
              <span className="teams">{r.team_count} 队</span>
              <span className="judges">{r.judge_count} 裁判</span>
              <span className="spectators">👁 {r.spectator_count}</span>
            </div>
            <div className="room-card__actions">
              {myRoles[r.room_id] === 'player' ? (
                <button
                  className="btn-primary btn-sm"
                  onClick={() => handleNavigate(r.room_id, 'player')}
                >
                  进入房间
                </button>
              ) : myRoles[r.room_id] === 'spectator' ? (
                <button
                  className="btn-secondary btn-sm"
                  onClick={() => handleNavigate(r.room_id, 'spectator')}
                >
                  👁 继续观战
                </button>
              ) : (
                <button
                  className="btn-secondary btn-sm"
                  onClick={() => handleSpectate(r.room_id)}
                >
                  👁 观战
                </button>
              )}
            </div>
          </div>
        ))}
      </section>

      {createOpen && (
        <DebateRoomCreateModal
          onClose={() => setCreateOpen(false)}
          onCreated={handleCreated}
        />
      )}
    </div>
  );
}
