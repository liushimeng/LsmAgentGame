/**
 * 辩论比赛 — 大厅 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §2 大厅页设计 + 复用 5 款游戏大厅页风格。
 * §20260831-06 — 增补模型胜率统计面板(§06 §9 历史统计)。
 * §20260831-08 — 增补「历史战绩」折叠面板(DebateHistoryListPanel)+
 *   复盘详情弹窗(DebateReplayModal),消费 /api/games/debate/history 端点。
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
import { forceDisbandDebateRoom } from '@/api/admin';
import { useDebateStore } from '@/store/debate.store';
import { useLobbyLiveUpdate } from '@/hooks/useLobbyLiveUpdate';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import DebateRoomCreateModal from '@/components/debate/DebateRoomCreateModal';
import DebateHistoryListPanel from '@/components/debate/DebateHistoryListPanel';
import DebateReplayModal from '@/components/debate/DebateReplayModal';
import { AdminDisbandButton } from '@/components/ui/AdminDisbandButton';
import type { DebateModelStats, DebateRoomSummary } from '@/types/debate';

export function DebateLobbyPage() {
  const nav = useNavigate();
  const { rooms, setRooms, patchRoom, removeRoom } = useDebateStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [myRoles, setMyRoles] = useState<Record<string, 'player' | 'spectator'>>({});
  // §20260831-06 — 模型胜率统计(§06 §9 历史统计)
  const [modelStats, setModelStats] = useState<DebateModelStats[]>([]);
  const [statsOpen, setStatsOpen] = useState(false);
  // §20260831-08 — 历史战绩面板 + 复盘详情弹窗
  const [historyOpen, setHistoryOpen] = useState(false);
  const [replayRoomId, setReplayRoomId] = useState('');
  const t = useT();

  // 大厅挂载时拉取模型胜率统计(失败静默:统计缺失不影响主流程)
  useEffect(() => {
    debateService
      .stats()
      .then((s) => setModelStats(s ?? []))
      .catch(() => { /* best-effort */ });
  }, []);

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
              <AdminDisbandButton
                roomId={r.room_id}
                roomStatus={r.status}
                hideWhenOver
                onDisbanded={removeRoom}
                onDisband={async (rid, reason) => {
                  await forceDisbandDebateRoom(rid, reason);
                }}
              />
            </div>
          </div>
        ))}
      </section>

      <section className="debate-model-stats">
        <button
          type="button"
          className="model-stats-toggle"
          onClick={() => setStatsOpen((v) => !v)}
        >
          📊 模型胜率统计{statsOpen ? ' ▲' : ' ▼'}
        </button>
        {statsOpen && (
          modelStats.length === 0 ? (
            <p className="model-stats-empty">暂无比赛数据 — 完成一场辩论后这里会展示各模型胜率。</p>
          ) : (
            <table className="model-stats-table">
              <thead>
                <tr>
                  <th>模型</th>
                  <th>场次</th>
                  <th>胜场</th>
                  <th>胜率</th>
                  <th>最佳辩手</th>
                  <th>场均总分</th>
                </tr>
              </thead>
              <tbody>
                {modelStats.map((s) => (
                  <tr key={s.model_key}>
                    <td className="model-stats-name">{shortModelKey(s.model_key)}</td>
                    <td>{s.total_games}</td>
                    <td>{s.win_count}</td>
                    <td className="model-stats-winrate">{(s.win_rate * 100).toFixed(0)}%</td>
                    <td>{s.best_debater_count}</td>
                    <td>{s.avg_total_score.toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        )}
      </section>

      {/* §20260831-08 — 历史战绩折叠面板(与「模型胜率统计」同款交互) */}
      <section className="debate-model-stats debate-history-section">
        <button
          type="button"
          className="model-stats-toggle"
          onClick={() => setHistoryOpen((v) => !v)}
        >
          📜 {t('debate.history.title' as TKey)}{historyOpen ? ' ▲' : ' ▼'}
        </button>
        {historyOpen && (
          <DebateHistoryListPanel onOpenDetail={(id) => setReplayRoomId(id)} />
        )}
      </section>

      {createOpen && (
        <DebateRoomCreateModal
          onClose={() => setCreateOpen(false)}
          onCreated={handleCreated}
        />
      )}

      {replayRoomId && (
        <DebateReplayModal
          roomId={replayRoomId}
          onClose={() => setReplayRoomId('')}
        />
      )}
    </div>
  );
}

// shortModelKey "MeiTuan-model" → "MeiTuan"(与后端口径一致)。
function shortModelKey(key: string): string {
  const idx = key.lastIndexOf('-');
  return idx > 0 ? key.slice(0, idx) : key;
}
