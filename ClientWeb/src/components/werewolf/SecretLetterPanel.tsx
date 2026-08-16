// §20260812-03 U2 — 私下通道面板(SecretLetterPanel)。
//
// 白天 speak→vote 窗口内,玩家可向任意非自己非死亡玩家发送 ≤200 字短消息。
// §119 协议层三重隔离:不入 chat_message / chat_history / BotTranscript.HeartThought。
// §122 限流:5s/条(服务端 §20260812-03 共享 speakLimiter 复用)。
// §130 接线:仅自己收发的信件可见(其他玩家通讯物理隔离)。
import React, { useEffect, useState, useCallback } from 'react';
import { http } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';

const LS_KEY = 'ww_panel_collapsed_secret_letter';

interface SecretLetterItem {
  id: string;
  from_seat: number;
  to_seat: number;
  body: string;
  round: number;
  is_read: boolean;
  created_at: number;
}

interface SecretLetterPanelProps {
  roomId: string;
  mySeat: number; // -1 = observer
  aliveSeats: number[]; // 存活座位列表
  windowOpen: boolean; // 仅 speak 阶段可发
  t: (k: any) => string;
}

export const SecretLetterPanel: React.FC<SecretLetterPanelProps> = ({
  roomId,
  mySeat,
  aliveSeats,
  windowOpen,
  t,
}) => {
  const [letters, setLetters] = useState<SecretLetterItem[]>([]);
  const [target, setTarget] = useState<number | ''>('');
  const [body, setBody] = useState('');
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  // §20260816-01 折叠态:默认折叠以节省中栏空间,持久化到 localStorage
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try {
      const v = localStorage.getItem(LS_KEY);
      return v !== null ? v === '1' : !windowOpen;
    } catch {
      return !windowOpen;
    }
  });

  const toggle = useCallback(() => {
    setCollapsed((c) => {
      const next = !c;
      try { localStorage.setItem(LS_KEY, next ? '1' : '0'); } catch {}
      return next;
    });
  }, []);

  const refresh = useCallback(async () => {
    if (mySeat < 0) return;
    try {
      setLoading(true);
      const res = await http<{ data: { letters: SecretLetterItem[] } }>(
        `/api/games/werewolf/rooms/${roomId}/secret-letter/inbox`,
      );
      setLetters(res.data?.letters ?? []);
    } catch (e: any) {
      const msg = e?.message || '获取私下信件失败';
      setErr(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setLoading(false);
    }
  }, [roomId, mySeat]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // 窗口开启时自动展开,方便玩家直接使用;关闭时尊重用户折叠选择
  useEffect(() => {
    if (windowOpen) setCollapsed(false);
  }, [windowOpen]);

  const send = useCallback(async () => {
    if (target === '' || !body.trim() || !windowOpen) return;
    if (body.length > 200) {
      setErr('内容不能超过 200 字');
      return;
    }
    try {
      setLoading(true);
      setErr(null);
      await http(`/api/games/werewolf/rooms/${roomId}/secret-letter`, {
        method: 'POST',
        body: JSON.stringify({ target_seat: target, body }),
      });
      setBody('');
      setTarget('');
      await refresh();
    } catch (e: any) {
      const msg = e?.message || '发送失败';
      setErr(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setLoading(false);
    }
  }, [target, body, windowOpen, roomId, refresh]);

  // 观战者或自己不在座位时不显示
  if (mySeat < 0) return null;

  return (
    <div
      className={`ww-secret-letter${collapsed ? ' is-collapsed' : ''}`}
      data-testid="ww-secret-letter"
    >
      <header className="ww-secret-letter__header" onClick={toggle} role="button" tabIndex={0}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); } }}
        aria-expanded={!collapsed}
      >
        <h4>
          ✉️ {t('werewolf.letter.title')}
          {!windowOpen && <span className="ww-secret-letter__closed">· {t('werewolf.letter.window_closed')}</span>}
        </h4>
        <span className="ww-panel__arrow" aria-hidden="true">▼</span>
      </header>
      {windowOpen && (
        <div className="ww-secret-letter__compose">
          <select
            value={target}
            onChange={(e) => setTarget(e.target.value === '' ? '' : Number(e.target.value))}
            aria-label={t('werewolf.letter.target_label')}
          >
            <option value="">{t('werewolf.letter.target_label')}</option>
            {aliveSeats
              .filter((s) => s !== mySeat)
              .map((s) => (
                <option key={s} value={s}>
                  {s + 1}号
                </option>
              ))}
          </select>
          <textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder={t('werewolf.letter.body_placeholder')}
            maxLength={200}
            disabled={!windowOpen}
            rows={2}
          />
          <div className="ww-secret-letter__meta">
            <span>
              {body.length}/200
            </span>
            <button
              type="button"
              onClick={send}
              disabled={loading || target === '' || !body.trim() || !windowOpen}
            >
              {t('werewolf.letter.send')}
            </button>
          </div>
        </div>
      )}
      {err && <p className="ww-secret-letter__err" role="alert">{err}</p>}
      <div className="ww-secret-letter__list">
        {letters.length === 0 && !collapsed && (
          <p className="ww-secret-letter__empty">{t('werewolf.letter.inbox_empty')}</p>
        )}
        {letters.map((l) => (
          <div
            key={l.id}
            className={`ww-secret-letter__item${l.is_read ? ' is-read' : ' is-unread'}`}
          >
            <div className="ww-secret-letter__item-meta">
              {l.from_seat === mySeat
                ? `→ ${l.to_seat + 1}号`
                : `${l.from_seat + 1}号 → 你`}
              <span className="ww-secret-letter__item-round">R{l.round}</span>
            </div>
            <div className="ww-secret-letter__item-body">{l.body}</div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default SecretLetterPanel;
