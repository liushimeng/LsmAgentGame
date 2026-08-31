/**
 * 辩论比赛 — 大厅 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §2 大厅页设计 + 复用 5 款游戏大厅页风格。
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
import DebateRoomCreateModal from '@/components/debate/DebateRoomCreateModal';

export function DebateLobbyPage() {
  const nav = useNavigate();
  const { rooms, setRooms } = useDebateStore();
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [createOpen, setCreateOpen] = useState(false);

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

  // 自动刷新(对齐其他 lobby 的 5s 轮询 + WS 推送)
  useLobbyLiveUpdate({ gameKind: 'debate', updateRoom: () => {}, removeRoom: () => {} });

  const handleSpectate = (roomId: string) => {
    debateService
      .spectate(roomId)
      .then(() => nav(`/debate/${roomId}`))
      .catch((e: Error) => {
        if (!isSessionExpiredError(e)) {
          reportGlobalError({ message: e.message, severity: 'error' });
        }
      });
  };

  const handleCreated = (roomId: string) => {
    setCreateOpen(false);
    nav(`/debate/${roomId}`);
  };

  return (
    <div className="page debate-lobby">
      <header className="page-header">
        <h1>🎓 辩论比赛</h1>
        <button className="btn-primary" onClick={() => setCreateOpen(true)}>
          + 创建房间
        </button>
      </header>

      {err && <div className="error-bar">{err}</div>}

      <section className="room-list">
        {loading && <div className="loading">加载中...</div>}
        {!loading && rooms.length === 0 && (
          <div className="empty">暂无房间,点击「+ 创建房间」开始一场辩论</div>
        )}
        {rooms.map((r) => (
          <div key={r.room_id} className="room-card">
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
              <button
                className="btn-secondary"
                onClick={() => handleSpectate(r.room_id)}
              >
                👁 观战
              </button>
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