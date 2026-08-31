/**
 * 辩论比赛房主操作面板 (2026-08-31 §20260831-04 + §20260831-05 + §20260831-12)
 *
 * 对齐 docs/辩论比赛/04 §4.2 房主交互:
 *   - 开始比赛(仅 Filling 阶段)
 *   - 解散房间(任何阶段,二次确认)
 *
 * §20260831-12 — 新增超管强制解散入口:
 *   - 超级管理员( userType >= 3 )不论身份(房主/观战者)都可见
 *   - 走 DELETE /api/admin/debate/rooms/:id 端点
 *   - 后端广播 debate.room_removed 帧,前端收到后自动跳转
 *
 * 失败错误以 formError 形式在面板内展示,同时上报全局(§7.1)。
 */
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { debateService } from '@/api/debate';
import { forceDisbandDebateRoom } from '@/api/admin';
import { useDebateStore } from '@/store/debate.store';
import { useAuthStore } from '@/store/auth.store';
import { reportGlobalError } from '@/services/globalError';
import { ApiError, isSessionExpiredError } from '@/services/http';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

interface Props {
  roomId: string;
}

export default function DebateHostControls({ roomId }: Props) {
  const nav = useNavigate();
  const phase = useDebateStore((s) => s.phase);
  const isOwner = useDebateStore((s) => s.currentRoom?.is_owner ?? false);
  const userType = useAuthStore((s) => s.userType);
  const isSuperAdmin = (userType ?? 1) >= 3;

  const [loading, setLoading] = useState(false);
  const [disbanding, setDisbanding] = useState(false);
  const [err, setErr] = useState('');
  // §20260831-12 — 超管强制解散确认态
  const [forceDisbandPending, setForceDisbandPending] = useState(false);
  const [forceDisbanding, setForceDisbanding] = useState(false);
  const [forceDisbandErr, setForceDisbandErr] = useState('');

  if (!isOwner) {
    return (
      <div className="debate-host-controls debate-host-controls--readonly">
        <span className="host-badge">👁 观战者</span>
        <small>房主可点击「开始比赛」</small>
      </div>
    );
  }

  const canStart = phase === 'filling';

  const handleStart = () => {
    setErr('');
    setLoading(true);
    debateService
      .start(roomId)
      .then(() => {
        setLoading(false);
      })
      .catch((e: Error) => {
        setLoading(false);
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      });
  };

  const handleDisband = () => {
    if (!window.confirm('确定要解散房间吗?此操作不可撤销。')) return;
    setErr('');
    setDisbanding(true);
    debateService
      .disband(roomId)
      .then(() => {
        setDisbanding(false);
        nav('/debate');
      })
      .catch((e: Error) => {
        setDisbanding(false);
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      });
  };

  // §20260831-12 — 超管强制解散处理
  const handleForceDisband = async () => {
    setForceDisbanding(true);
    setForceDisbandErr('');
    try {
      await forceDisbandDebateRoom(roomId, 'admin-force-disband');
      // 后端会广播 debate.room_removed 帧,useDebate hook 会处理跳转
      // 这里也手动跳一下作为兜底
      setTimeout(() => nav('/debate'), 300);
    } catch (e: unknown) {
      if (isSessionExpiredError(e)) {
        setForceDisbandPending(false);
        return;
      }
      const msg =
        e instanceof ApiError
          ? `[${e.code}] ${e.message}`
          : (e as Error)?.message ?? 'force disband failed';
      setForceDisbandErr(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setForceDisbanding(false);
    }
  };

  // 非房主且非超管 → 显示观战者标识
  if (!isOwner && !isSuperAdmin) {
    return (
      <div className="debate-host-controls debate-host-controls--readonly">
        <span className="host-badge">👁 观战者</span>
        <small>房主可点击「开始比赛」</small>
      </div>
    );
  }

  return (
    <div className="debate-host-controls">
      <div className="host-badges">
        {isOwner && <span className="host-badge">👑 房主</span>}
        {isSuperAdmin && <span className="host-badge host-badge--admin">🛡 超管</span>}
      </div>
      <div className="host-actions">
        {isOwner && (
          <>
            {canStart ? (
              <button
                type="button"
                className="btn-primary btn-sm"
                onClick={handleStart}
                disabled={loading || disbanding}
              >
                {loading ? '启动中...' : '开始比赛'}
              </button>
            ) : (
              <span className="host-running">比赛进行中</span>
            )}
            <button
              type="button"
              className="btn-danger btn-sm"
              onClick={handleDisband}
              disabled={loading || disbanding}
              title="房主解散房间"
            >
              {disbanding ? '解散中...' : '解散房间'}
            </button>
          </>
        )}
        {isSuperAdmin && (
          <button
            type="button"
            className="btn btn-sm btn-danger room-card__admin-disband"
            onClick={() => {
              setForceDisbandErr('');
              setForceDisbandPending(true);
            }}
            disabled={forceDisbanding}
            title="超级管理员强制解散房间"
          >
            ⛔ 强制解散
          </button>
        )}
      </div>
      {err && <div className="host-error">{err}</div>}
      {forceDisbandErr && <div className="host-error">{forceDisbandErr}</div>}
      {forceDisbandPending && (
        <ConfirmModal
          message={`确认强制解散房间 ${roomId.slice(0, 8)}…？\n该房间所有 Agent / 观战者会立即被踢出,且无法恢复。`}
          confirmLabel="强制解散"
          danger
          onConfirm={handleForceDisband}
          onCancel={() => {
            if (!forceDisbanding) setForceDisbandPending(false);
          }}
        />
      )}
    </div>
  );
}
