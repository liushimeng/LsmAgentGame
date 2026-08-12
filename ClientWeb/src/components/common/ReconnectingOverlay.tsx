import { useEffect, useState } from 'react';
import { useConnectionStore } from '@/store/connection.store';

// ReconnectingOverlay —— 全屏 Loading 遮罩。
// 当 WS 处于「重连中」时覆盖整个视口，提示用户正在恢复连接。
// 首次握手（status='connecting'）不显示遮罩，只在「之前连上后被踢」
// （status='reconnecting'）时才显示。连接成功（status='open'）后自动隐藏。
// 仅在已登录时由 AppLayout 挂载。
export function ReconnectingOverlay() {
  const status = useConnectionStore((s) => s.status);
  // Track whether we've ever reached 'open' in this session. The overlay only
  // shows for 'reconnecting' AFTER a prior successful open — the first
  // 'connecting' is silent so F5 / login transitions don't flash a spinner.
  const [hasOpened, setHasOpened] = useState(false);
  // 进入 reconnecting 后还需保持至少 250ms 才显示遮罩 —— 防止 F5 刷新瞬间
  // 状态机抖动（connecting → open → 短暂 reconnecting → open）闪现深色全屏遮罩，
  // 肉眼感觉就是"页面变黑、没法用"。
  const [reconnectArmed, setReconnectArmed] = useState(false);

  useEffect(() => {
    if (status === 'open') {
      setHasOpened(true);
      setReconnectArmed(false);
    } else if (status === 'reconnecting') {
      const timer = window.setTimeout(() => setReconnectArmed(true), 250);
      return () => window.clearTimeout(timer);
    }
  }, [status]);

  const visible = status === 'reconnecting' && hasOpened && reconnectArmed;
  if (!visible) return null;

  return (
    <div className="ws-overlay" role="status" aria-live="polite">
      <div className="ws-overlay__box">
        <div className="ws-overlay__spinner" />
        <div className="ws-overlay__text">正在重新连接服务器…</div>
        <div className="ws-overlay__hint">请保持网络畅通，连接恢复后将自动继续</div>
      </div>
      <style>{`
        .ws-overlay {
          position: fixed;
          inset: 0;
          z-index: 9999;
          display: flex;
          align-items: center;
          justify-content: center;
          background: rgba(15, 23, 35, 0.55);
          backdrop-filter: blur(2px);
        }
        .ws-overlay__box {
          display: flex;
          flex-direction: column;
          align-items: center;
          gap: 14px;
          padding: 28px 36px;
          border-radius: 12px;
          background: #1f2933;
          color: #e6edf3;
          box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
        }
        .ws-overlay__spinner {
          width: 40px;
          height: 40px;
          border: 4px solid rgba(255, 255, 255, 0.2);
          border-top-color: #3fb950;
          border-radius: 50%;
          animation: ws-overlay-spin 0.8s linear infinite;
        }
        .ws-overlay__text {
          font-size: 16px;
          font-weight: 600;
        }
        .ws-overlay__hint {
          font-size: 12px;
          color: #8b98a5;
        }
        @keyframes ws-overlay-spin {
          to { transform: rotate(360deg); }
        }
      `}</style>
    </div>
  );
}
