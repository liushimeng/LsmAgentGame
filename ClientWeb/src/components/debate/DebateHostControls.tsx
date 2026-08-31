/**
 * 辩论比赛房主操作面板 (2026-08-31 §20260831-04)
 *
 * 对齐 docs/辩论比赛/04 §4.2 房主交互:
 *   - 开始比赛(仅 Filling 阶段)
 *
 * 失败错误以 formError 形式在面板内展示,同时上报全局(§7.1)。
 *
 * 解散房间(disband)暂未提供 REST 端点,留待下一阶段补全。
 */
import { useState } from 'react';
import { debateService } from '@/api/debate';
import { useDebateStore } from '@/store/debate.store';
import { reportGlobalError } from '@/services/globalError';

interface Props {
  roomId: string;
}

export default function DebateHostControls({ roomId }: Props) {
  const phase = useDebateStore((s) => s.phase);
  const isOwner = useDebateStore((s) => s.currentRoom?.is_owner ?? false);

  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

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

  return (
    <div className="debate-host-controls">
      <span className="host-badge">👑 房主</span>
      <div className="host-actions">
        {canStart ? (
          <button
            type="button"
            className="btn-primary btn-sm"
            onClick={handleStart}
            disabled={loading}
          >
            {loading ? '启动中...' : '开始比赛'}
          </button>
        ) : (
          <span className="host-running">比赛进行中</span>
        )}
      </div>
      {err && <div className="host-error">{err}</div>}
    </div>
  );
}
