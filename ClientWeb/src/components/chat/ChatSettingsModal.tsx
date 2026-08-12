// ChatSettingsModal.tsx — 大厅聊天设置弹窗（管理员/超级管理员专用）
//
// 功能：
// - 按时间范围清理聊天消息

import React, { useState, useCallback } from 'react';
import { useT } from '@/hooks/useT';
import { cleanupChatMessages } from '@/api/admin';
import './ChatSettingsModal.css';

interface ChatSettingsModalProps {
  open: boolean;
  onClose: () => void;
}

/** 将 Date 转为本地 datetime-local input 的 value（YYYY-MM-DDTHH:mm） */
function toLocalInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** 将 datetime-local input value 转为 RFC3339 格式 */
function toRFC3339(localValue: string): string {
  // datetime-local value 是 "YYYY-MM-DDTHH:mm"，需要加上秒和时区
  return localValue + ':00+08:00';
}

const ChatSettingsModal: React.FC<ChatSettingsModalProps> = ({ open, onClose }) => {
  const t = useT();

  // 默认时间范围：过去 24 小时
  const now = new Date();
  const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000);

  const [startTime, setStartTime] = useState<string>(toLocalInputValue(yesterday));
  const [endTime, setEndTime] = useState<string>(toLocalInputValue(now));
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<{ success: boolean; message: string } | null>(null);

  const handleCleanup = useCallback(async () => {
    if (!startTime || !endTime) {
      setResult({ success: false, message: t('chatSettings.errorTimeRange') });
      return;
    }

    const startRFC = toRFC3339(startTime);
    const endRFC = toRFC3339(endTime);

    if (new Date(startRFC) >= new Date(endRFC)) {
      setResult({ success: false, message: t('chatSettings.errorEndBeforeStart') });
      return;
    }

    setLoading(true);
    setResult(null);

    try {
      const res = await cleanupChatMessages(startRFC, endRFC);
      setResult({
        success: true,
        message: t('chatSettings.cleanupSuccess', { count: res.deleted_count }),
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : t('chatSettings.cleanupFailed');
      setResult({ success: false, message: msg });
    } finally {
      setLoading(false);
    }
  }, [startTime, endTime, t]);

  const handleClose = useCallback(() => {
    setResult(null);
    onClose();
  }, [onClose]);

  if (!open) return null;

  return (
    <div className="chat-settings-overlay" onClick={handleClose}>
      <div className="chat-settings-modal" onClick={(e) => e.stopPropagation()}>
        <div className="chat-settings-header">
          <h3>{t('chatSettings.title')}</h3>
          <button className="chat-settings-close" onClick={handleClose}>
            &times;
          </button>
        </div>

        <div className="chat-settings-body">
          <div className="chat-settings-section">
            <h4>{t('chatSettings.cleanupTitle')}</h4>
            <p className="chat-settings-desc">{t('chatSettings.cleanupDesc')}</p>

            <div className="chat-settings-form">
              <div className="chat-settings-field">
                <label>{t('chatSettings.startTime')}</label>
                <input
                  type="datetime-local"
                  value={startTime}
                  onChange={(e) => setStartTime(e.target.value)}
                  disabled={loading}
                />
              </div>

              <div className="chat-settings-field">
                <label>{t('chatSettings.endTime')}</label>
                <input
                  type="datetime-local"
                  value={endTime}
                  onChange={(e) => setEndTime(e.target.value)}
                  disabled={loading}
                />
              </div>

              <button
                className="chat-settings-btn chat-settings-btn-danger"
                onClick={handleCleanup}
                disabled={loading}
              >
                {loading ? t('chatSettings.cleaning') : t('chatSettings.cleanupBtn')}
              </button>
            </div>

            {result && (
              <div className={`chat-settings-result ${result.success ? 'success' : 'error'}`}>
                {result.message}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default ChatSettingsModal;
