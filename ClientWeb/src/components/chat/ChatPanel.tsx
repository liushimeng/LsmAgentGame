// ChatPanel — right-side chat column.
//
//   scope = 'lobby' → 全频段聊天 (home page, all games lobby, profile, about)
//
// Room-scoped chat is rendered exclusively by the per-game `GameChatPanel`
// (in xiangqi/chess/junqi/doudizhu/texasholdem/werewolf pages). Mounting a
// second `useChat('room', roomId)` here would cause chat messages to appear
// twice on game pages (once in this panel, once in `GameChatPanel`), since
// the per-instance dedupe in `useChat` only protects within a single hook
// instance — not across two parallel subscriptions to the same room.
//
// Collapsible via the header chevron. State managed by useUiStore so other
// components (e.g. game fullscreen) can coordinate.

import { useEffect, useRef, useState } from 'react';
import { useChat } from '@/hooks/useChat';
import { useUiStore } from '@/store/ui.store';
import { useT } from '@/hooks/useT';
import { useAuthStore } from '@/store/auth.store';
import ChatSettingsModal from './ChatSettingsModal';

export function ChatPanel() {
  const t = useT();
  const { messages, send, connected, error, loadMore, hasMore, loadingMore } = useChat('lobby');
  const title = t('chat.lobbyTitle');

  // 管理员/超级管理员可以打开聊天设置
  const userType = useAuthStore((s) => s.userType);
  const isAdmin = userType !== null && userType >= 2;
  const [settingsOpen, setSettingsOpen] = useState(false);

  // 折叠状态来自全局 ui store,确保全屏模式可以一并收起
  const collapsed = useUiStore((s) => s.chatCollapsed);
  const toggle = useUiStore((s) => s.toggleChat);
  const setCollapsed = useUiStore((s) => s.setChatCollapsed);
  const breakpoint = useUiStore((s) => s.breakpoint);

  const [draft, setDraft] = useState('');
  const listRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  // Scroll anchoring: remember scrollHeight *before* prepending history, then
  // compensate afterwards so the user's visual viewport doesn't jump.
  const pendingAnchorRef = useRef<number | null>(null);

  // 移动端首次挂载且无持久化偏好时强制收起 chat；后续路由切换由上层 ui store 协调。
  useEffect(() => {
    if (breakpoint === 'mobile') {
      const uiState = useUiStore.getState();
      if (!uiState._hydrated) setCollapsed(true);
    }
    // 仅在挂载/断点变化时执行一次即可——切换页面路由由 ChatPanel 上层
    // (AppLayout) 维持，不需要重新订阅。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [breakpoint, setCollapsed]);
  // ------------------------------------------------------------------
  // Sentinel observer for "load more on scroll to top".
  // ------------------------------------------------------------------
  useEffect(() => {
    const sentinel = sentinelRef.current;
    const list = listRef.current;
    if (!sentinel || !list) return;
    // Only observe when there is more to load. Toggling observing on
    // hasMore changes pays off when has_more flips to false (we still
    // want to see the "no older messages" indicator without re-querying).
    const obs = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (!entry.isIntersecting) return;
        // Guard: only trigger when there's more data to fetch AND we're
        // not mid-flight. useChat.loadMore() also double-checks, so this
        // is just a fast-path to avoid the IntersectionObserver firing
        // while loadingMore is true.
        if (!hasMore || loadingMore) return;
        // Capture pre-load scroll anchor (used in the [messages] effect
        // below to compensate after state settles).
        pendingAnchorRef.current = list.scrollHeight;
        loadMore();
      },
      { root: list, rootMargin: '0px 0px 0px 0px', threshold: 0.1 },
    );
    obs.observe(sentinel);
    return () => obs.disconnect();
  }, [hasMore, loadingMore, loadMore]);

  // Auto-scroll to bottom on new messages unless the user has scrolled away.
  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    // If we are waiting to apply a scroll-anchor correction (history
    // prepend in-flight), skip the auto-bottom-scroll and let the
    // anchoring block run instead.
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (pendingAnchorRef.current != null) {
      // Apply anchor correction: we captured scrollHeight before prepend,
      // now the list grew by (newScroll - oldScroll). Jump scrollTop so
      // the viewport's top pixel stays put.
      const oldHeight = pendingAnchorRef.current;
      const newHeight = el.scrollHeight;
      el.scrollTop += Math.max(0, newHeight - oldHeight);
      pendingAnchorRef.current = null;
      return;
    }
    if (nearBottom) el.scrollTop = el.scrollHeight;
    // Both `messages.length` (new live messages) and `loadingMore`
    // (history responses drive prepend) should trigger this effect — the
    // latter needs it to run *after* React has flushed the new messages
    // into the DOM, and we use `messages.length` as our heuristic. We
    // also mirror loadingMore in deps so the effect re-runs on the tick
    // where loading transitions true→false.
  }, [messages.length, loadingMore]);

  // Release the anchor ref if loadingMore resets without new messages
  // arriving (rare — e.g. the WS dropped). Otherwise we'd lock the list.
  useEffect(() => {
    if (!loadingMore) pendingAnchorRef.current = null;
  }, [loadingMore]);

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const text = draft.trim();
    if (!text) return;
    send(text);
    setDraft('');
  };

  return (
    <>
      {/* 移动端:聊天作为底部抽屉,展开时显示遮罩 */}
      {breakpoint === 'mobile' && !collapsed && (
        <div
          className="chat-panel__backdrop"
          onClick={toggle}
          aria-hidden="true"
        />
      )}
      <aside
        className={
          'chat-panel'
          + (collapsed ? ' chat-panel--collapsed' : '')
          + (breakpoint === 'mobile' ? ' chat-panel--mobile' : '')
          + (breakpoint === 'mobile' && !collapsed ? ' chat-panel--drawer-open' : '')
        }
      >
        <header className="chat-panel__header">
          <div className="chat-panel__title">
            <span className={'chat-panel__dot' + (connected ? ' chat-panel__dot--on' : '')} />
            <span>{title}</span>
          </div>
          <div className="chat-panel__header-actions">
            {isAdmin && !collapsed && (
              <button
                type="button"
                className="ghost chat-panel__settings"
                onClick={() => setSettingsOpen(true)}
                aria-label={t('chat.settings')}
                title={t('chat.settings')}
              >
                ⚙
              </button>
            )}
            <button
              type="button"
              className="ghost chat-panel__toggle"
              onClick={toggle}
              aria-label={collapsed ? t('chat.expand') : t('chat.collapse')}
              title={collapsed ? t('chat.expand') : t('chat.collapse')}
            >
              {collapsed ? (breakpoint === 'mobile' ? '▲' : '◀') : (breakpoint === 'mobile' ? '▼' : '▶')}
            </button>
          </div>
        </header>

        {!collapsed && (
          <>
            <div
              className="chat-panel__list"
              ref={listRef}
              style={{ maxHeight: '60vh', overflowY: 'auto' }}
            >
              {/* Sentinel: 1px tall element observed by IntersectionObserver */}
              <div ref={sentinelRef} style={{ height: 1, width: '100%' }} />
              {/* Loading indicator / "no more" message rendered just below sentinel */}
              {loadingMore && (
                <div className="chat-panel__loading-more">
                  <span className="chat-panel__spinner" /> {t('chat.loadingMore')}
                </div>
              )}
              {!hasMore && messages.length > 0 && (
                <div className="chat-panel__no-more">{t('chat.noOlderMessages')}</div>
              )}
              {messages.length === 0 && !loadingMore && (
                <div className="chat-panel__empty">{t('chat.empty')}</div>
              )}
              {messages.map((m) => (
                <div key={m.id} className="chat-msg">
                  <span className="chat-msg__author">{m.from_account}</span>
                  <span className="chat-msg__time">{formatChatTime(m.ts)}</span>
                  <div className="chat-msg__text">{m.text}</div>
                </div>
              ))}
            </div>

            {error && <div className="chat-panel__error">{error}</div>}

            <form className="chat-panel__form" onSubmit={onSubmit}>
              <input
                type="text"
                value={draft}
                maxLength={1024}
                placeholder={connected ? t('chat.placeholder') : t('chat.connecting')}
                onChange={(e) => setDraft(e.target.value)}
              />
              <button type="submit" disabled={!connected || !draft.trim()}>
                {t('chat.send')}
              </button>
            </form>
          </>
        )}
      </aside>

      {/* 管理员聊天设置弹窗 */}
      {isAdmin && (
        <ChatSettingsModal
          open={settingsOpen}
          onClose={() => setSettingsOpen(false)}
        />
      )}
    </>
  );
}

// formatChatTime — today's messages show only HH:mm; older ones show MM-DD HH:mm.
function formatChatTime(ts: number): string {
  const d = new Date(ts);
  const now = new Date();
  const pad = (n: number) => n.toString().padStart(2, '0');
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDay) return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
