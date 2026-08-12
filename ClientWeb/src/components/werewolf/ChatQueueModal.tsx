/**
 * ChatQueueModal — 500K 队列查看面板 (2026-07-09 §13-bugfix)。
 *
 * 点击 "📚 500K 队列" 按钮后弹出,从
 *   GET /api/admin/werewolf/rooms/:room_id/chat_history
 * 拉取房间共享 500K 队列的完整内容 + 每 bot read pointer。
 *
 * 鉴权: 需要 admin / super admin。如果调用者不是管理员,后端返 403,前端显示
 * "需要管理员权限"。
 *
 * 设计原则: 公平性调试 — 7 人局可以肉眼验证 7 bot 是否看到同一份事件流。
 * 也可看每 bot read pointer 是否同步推进。
 */
import { useEffect, useState, useCallback } from 'react';
import { http, ApiError, isSessionExpiredError } from '@/services/http';

interface QueueMessage {
  seq?: number;
  from_seat: number;
  from_id: string;
  agent_name?: string;
  from_account?: string;
  is_bot?: boolean;
  is_spectator?: boolean;
  is_whisper?: boolean;
  to_seat?: number;
  text: string;
  timestamp?: string;
  is_activity?: boolean;
  event_kind?: string;
  activity_icon?: string;
}

interface QueueResponse {
  room_id: string;
  exists: boolean;
  queue_bytes: number;
  queue_count: number;
  returned: number;
  limit: number;
  messages: QueueMessage[];
  read_pointers: Record<string, number>;
}

interface Props {
  roomId: string;
  onClose: () => void;
}

export function ChatQueueModal({ roomId, onClose }: Props) {
  const [data, setData] = useState<QueueResponse | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [limit, setLimit] = useState(200);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [loading, setLoading] = useState(false);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setErr(null);
    try {
      const body = await http<QueueResponse>(
        `/api/admin/werewolf/rooms/${encodeURIComponent(roomId)}/chat_history?limit=${limit}`,
        { method: 'GET' }
      );
      // 防御性归一化:后端 schema 演进 / 异常 payload 可能缺失可选字段
      // (messages / queue_bytes / queue_count / returned / read_pointers),
      // 直接渲染会触发 "Cannot read properties of undefined (reading 'length')"
      // 并使 ErrorBoundary 兜底。一律兜底为默认值,避免整体模态崩溃。
      setData({
        room_id: body?.room_id ?? roomId,
        exists: body?.exists ?? false,
        queue_bytes: body?.queue_bytes ?? 0,
        queue_count: body?.queue_count ?? 0,
        returned: body?.returned ?? 0,
        limit: body?.limit ?? limit,
        messages: body?.messages ?? [],
        read_pointers: body?.read_pointers ?? {},
      });
    } catch (e) {
      // 2026-07-12 §P0-NEW: 会话过期不应在弹窗里直接渲染 "⚠ SESSION_EXPIRED"。
      // http() 内部已经调用 reportAuthExpired() 触发全局 SessionExpiredToast
      // + AuthModal 重新唤起,我们只需把弹窗关掉,让用户走重新登录路径。
      if (isSessionExpiredError(e)) {
        onClose();
        return;
      }
      if (e instanceof ApiError) {
        if (e.status === 403) {
          // 2026-07-10 §P0-NEW: 解释 + 指引 — 普通用户拿到 403 时往往误以为
          // "按钮坏了"。明确说明这是 admin-only 调试入口,普通观众/玩家请
          // 看右侧的"房间聊天"面板,那才是公开的事件流。
          setErr(
            '需要管理员权限(admin / super admin)才能查看 500K 队列。\n' +
            '普通玩家和观战者请看右侧"💬 房间聊天"面板 — 那里的事件流和 500K 队列共享同一份数据。',
          );
        } else if (e.status === 404) {
          setErr('房间不存在或已关闭');
        } else {
          setErr(e.message || `HTTP ${e.status}`);
        }
      } else {
        setErr((e as Error).message || '网络错误');
      }
    } finally {
      setLoading(false);
    }
  }, [roomId, limit]);

  // initial + on room/limit change
  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // optional auto-refresh every 5s
  useEffect(() => {
    if (!autoRefresh) return;
    const id = window.setInterval(fetchData, 5000);
    return () => window.clearInterval(id);
  }, [autoRefresh, fetchData]);

  // ESC closes
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const fmtBytes = (b: number) => {
    if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`;
    return `${b} B`;
  };

  return (
    <div
      className="chat-queue-modal__backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="chat-queue-modal" data-testid="werewolf-chat-queue-modal">
        <header className="chat-queue-modal__header">
          <h3>📚 500K 队列 — 房间共享聊天历史</h3>
          <button
            className="chat-queue-modal__close"
            onClick={onClose}
            aria-label="close"
            data-testid="werewolf-chat-queue-close"
          >
            ×
          </button>
        </header>
        <div className="chat-queue-modal__toolbar">
          <label>
            拉取条数:
            <input
              type="number"
              min={10}
              max={2000}
              value={limit}
              onChange={(e) => setLimit(Math.max(10, Math.min(2000, Number(e.target.value) || 200)))}
              data-testid="werewolf-chat-queue-limit"
            />
          </label>
          <button
            className="btn btn-secondary"
            onClick={fetchData}
            disabled={loading}
            data-testid="werewolf-chat-queue-refresh"
          >
            {loading ? '…加载中' : '🔄 刷新'}
          </button>
          <label>
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(e) => setAutoRefresh(e.target.checked)}
            />
            每 5 秒自动刷新
          </label>
        </div>

        {err ? (
          <div className="chat-queue-modal__error" data-testid="werewolf-chat-queue-error">
            ⚠ {err}
          </div>
        ) : !data ? (
          <div className="chat-queue-modal__empty">…</div>
        ) : (
          <>
            <div className="chat-queue-modal__stats" data-testid="werewolf-chat-queue-stats">
              <span>
                <strong>队列:</strong> {fmtBytes(data.queue_bytes)} ({data.queue_count} 条)
              </span>
              <span>
                <strong>本次显示:</strong> {data.returned} 条
              </span>
              <span>
                <strong>Read Pointer:</strong>{' '}
                {Object.entries(data.read_pointers)
                  .sort(([a], [b]) => Number(a) - Number(b))
                  .map(([s, seq]) => `Bot ${Number(s) + 1}号=${seq}`)
                  .join(' · ')}
              </span>
            </div>
            <div className="chat-queue-modal__list" data-testid="werewolf-chat-queue-list">
              {data.messages.length === 0 ? (
                <div className="chat-queue-modal__empty">队列为空</div>
              ) : (
                data.messages.map((m, idx) => {
                  const ts = m.timestamp ? new Date(m.timestamp).toLocaleTimeString() : '';
                  let prefix = ts;
                  if (m.is_whisper) prefix += ' [私聊]';
                  else if (m.is_spectator) prefix += ' [观战]';
                  else if (m.is_activity) prefix += ` [${m.event_kind ?? '活动'}]`;
                  let who = m.from_account ?? '';
                  if (!who) {
                    if (m.is_bot) who = `Bot ${(m.from_seat ?? -1) + 1}号`;
                    else who = `seat=${m.from_seat}`;
                  }
                  if (m.agent_name) who += ` (${m.agent_name})`;
                  const seqLabel = m.seq != null ? `#${m.seq}` : '';
                  return (
                    <div
                      key={`${m.seq ?? '?'}-${idx}`}
                      className={
                        'chat-queue-modal__item ' +
                        (m.is_whisper ? 'is-whisper ' : '') +
                        (m.is_activity ? 'is-activity ' : '')
                      }
                    >
                      <span className="chat-queue-modal__seq">{seqLabel}</span>
                      <span className="chat-queue-modal__prefix">{prefix}</span>
                      <span className="chat-queue-modal__who">{who}:</span>
                      <span className="chat-queue-modal__text">{m.text}</span>
                    </div>
                  );
                })
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
