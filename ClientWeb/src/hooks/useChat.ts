// useChat — manages a chat subscription on the existing WS connection.
//
//   scope = 'lobby'  →  subscribes to the global lobby broadcast
//   scope = 'room'   →  subscribes to a single room (roomId required)
//
// On mount: connects the WS (if not already), subscribes, requests history.
// On WS reconnect: automatically re-subscribes and re-fetches history.
// On unmount: unsubscribes.
// On scope/roomId change: unsubscribes old, subscribes new.
//
// History pagination:
//   The backend supports keyset pagination via `before_id`. The first request
//   omits `before_id` (returns the latest HISTORY_LIMIT messages). When the
//   user scrolls to the top the caller invokes `loadMore()`, which sends
//   `chat.history { before_id: <oldest message id>, limit }` and prepends the
//   response. The response envelope carries `has_more` and `next_cursor` so the
//   caller can know when to stop asking.

import { useEffect, useRef, useState, useCallback } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useAuth } from './useAuth';
import type { ChatMessage, ChatScope } from '@/types/api';

const HISTORY_LIMIT = 50;

export interface UseChat {
  messages: ChatMessage[];
  send: (text: string) => void;
  whisper: (toUserId: string, toAccount: string, text: string) => void;
  connected: boolean;
  error: string | null;
  /** Trigger a keyset page-load of older messages. No-op when already loading
   *  or when `hasMore` is false. */
  loadMore: () => void;
  /** True when the last history response signalled more pages exist. */
  hasMore: boolean;
  /** True while a `chat.history` request is in-flight. */
  loadingMore: boolean;
}

export function useChat(scope: ChatScope, roomId?: string): UseChat {
  const isAuth = useAuth((s) => s.isAuthenticated);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  // Pagination state -------------------------------------------------------
  // `hasMore` is driven by the server envelope's `has_more`. We default to
  // true so the very first loadMore() call can fire immediately.
  const [hasMore, setHasMore] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  // Sequence counter on history responses. If a newer subscribe re-fetch
  // resolves before an in-flight loadMore, we drop the stale response so it
  // can't clobber the freshly-reset message list.
  const historySeq = useRef(0);
  // Guard against concurrent loadMore() calls; the WS round-trip resolves via
  // the chat.history frame, so a simple boolean flag is enough.
  const loadingMoreRef = useRef(false);
  // Remember the most recent (scope, roomId) inside refs so the WS listener
  // can filter without re-binding on every render.
  const scopeRef = useRef<ChatScope>(scope);
  const roomIdRef = useRef<string | undefined>(roomId);
  scopeRef.current = scope;
  roomIdRef.current = roomId;

  // Keep the WS open while authenticated.
  useEffect(() => {
    if (!isAuth) return;
    wsClient.connect();
    return () => {
      // We deliberately don't close here — useWebSocket (in AppLayout) owns
      // the connection lifecycle. Just leave the subscription cleanup to
      // the scope effect below.
    };
  }, [isAuth]);

  // Listen for chat frames. The handler is installed once per (scope,roomId)
  // change so it always filters against the current target.
  useEffect(() => {
    if (!isAuth) return;
    const off = wsClient.on((env: WsEnvelope) => {
      switch (env.type) {
        case 'chat.message': {
          const m = env.payload as ChatMessage;
          if (!m) return;
          if (!matches(m.scope, m.room_id, scopeRef.current, roomIdRef.current)) return;
          setMessages((prev) => {
            // De-dupe by id (history replay + live broadcast can overlap).
            if (prev.some((x) => x.id === m.id)) return prev;
            return [...prev, m];
          });
          break;
        }
        case 'chat.whisper': {
          const m = env.payload as ChatMessage;
          if (!m) return;
          if (!matches(m.scope, m.room_id, scopeRef.current, roomIdRef.current)) return;
          setMessages((prev) => {
            if (prev.some((x) => x.id === m.id)) return prev;
            return [...prev, { ...m, whisper: true }];
          });
          break;
        }
        case 'chat.activity': {
          // §115 房间聊天 — 活动事件流。Server sends structured events like
          // phase change / vote result / wolf kill / seer check. We convert
          // them into a ChatMessage with from_role='activity' so the chat
          // panel can render them as colored chips inline with regular
          // speech messages. Note: we do NOT add them to the dedupe-by-id
          // set because the server can emit them without an `id` (events
          // are transient and not persisted). 2026-07-09 §115.
          const raw = env.payload as Record<string, unknown>;
          if (!raw) return;
          if (!matches(raw.scope as ChatScope, raw.room_id as string | undefined, scopeRef.current, roomIdRef.current)) return;
          const ts = (raw.ts as number) || Date.now();
          const fakeId = -(typeof raw.id === 'number' ? (raw.id as number) : ts);
          setMessages((prev) => {
            // Dedupe against transient fake id (negative); if the same ts+kind
            // is already in the list, skip.
            if (prev.some((x) => (x.id === fakeId) || (x.text === raw.text && x.ts === ts))) return prev;
            const activityMsg: ChatMessage = {
              id: fakeId,
              scope: (raw.scope as ChatScope) || scopeRef.current,
              room_id: raw.room_id as string | undefined,
              from_user_id: 'system',
              from_account: (raw.icon as string) || 'ℹ',
              from_role: 'activity',
              text: (raw.text as string) || '',
              ts,
            };
            // Stash extra fields on the message object (typed via ChatActivity
            // at call sites). We attach them dynamically so we don't widen
            // ChatMessage for non-werewolf games.
            (activityMsg as any).event_kind = raw.event_kind;
            (activityMsg as any).phase = raw.phase;
            (activityMsg as any).round_number = raw.round_number;
            (activityMsg as any).severity = raw.severity || 'info';
            (activityMsg as any).icon = raw.icon;
            (activityMsg as any).ref_seat = raw.ref_seat;
            (activityMsg as any).ref_seat_2 = raw.ref_seat_2;
            return [...prev, activityMsg];
          });
          break;
        }
        case 'chat.history': {
          const p = env.payload as {
            scope: ChatScope;
            room_id?: string;
            messages?: ChatMessage[];
            has_more?: boolean;
            next_cursor?: number | null;
            /** Optional server echo of the requested `before_id`. We use this to
             *  disambiguate "subscribe re-fetch" vs "loadMore response" when the
             *  caller does not set it. If absent we fall back to `before_id ===
             *  undefined` meaning "latest N". */
            before_id?: number | null;
          };
          if (!p || !matches(p.scope, p.room_id, scopeRef.current, roomIdRef.current)) return;
          const incoming = p.messages ?? [];
          // Server-driven has_more takes precedence; fall back to heuristic
          // (a full page means *probably* more) so older server builds still
          // behave sensibly.
          const serverHasMore = p.has_more ?? (incoming.length >= HISTORY_LIMIT);
          const cursor = p.next_cursor ?? null;

          setMessages((prev) => {
            if (p.before_id === undefined || p.before_id === null) {
              // Latest-N fetch (subscribe / reconnect): replace wholesale,
              // de-duped defensively against any live broadcast we may have
              // received in the interim.
              const seen = new Set<number>();
              const merged: ChatMessage[] = [];
              // Prefer existing (most recent live) then history.
              for (const m of [...prev, ...incoming]) {
                if (seen.has(m.id)) continue;
                seen.add(m.id);
                merged.push(m);
              }
              return merged;
            }
            // Keyset page load (before_id present): prepend older messages,
            // de-duped against what we already hold.
            const existingIds = new Set(prev.map((m) => m.id));
            const fresh = incoming.filter((m) => !existingIds.has(m.id));
            return [...fresh, ...prev];
          });

          // If this response came from a subscribe-style fetch (no before_id),
          // reset pagination state so loadMore starts fresh.
          if (p.before_id === undefined || p.before_id === null) {
            // Cancel any stale in-flight loadMore session.
            historySeq.current += 1;
            loadingMoreRef.current = false;
            setLoadingMore(false);
            // If the server signalled a cursor, honour it; otherwise fall
            // back to the oldest message id we received.
            if (incoming.length > 0) {
              setHasMore(serverHasMore);
            } else {
              setHasMore(false);
            }
            // Stash the cursor / oldest id on a ref-like state via closure:
            // we need it in loadMore. Simplest: remember it in a ref.
            oldestIdRef.current = cursor ?? (incoming.length > 0 ? incoming[incoming.length - 1].id : null);
          } else {
            // This is a loadMore response: advance seq, clear loading flag,
            // update hasMore from the server envelope.
            historySeq.current += 1;
            loadingMoreRef.current = false;
            setLoadingMore(false);
            setHasMore(serverHasMore);
            // Update cursor. If server gave next_cursor use it; otherwise
            // fall back to oldest id in the new page (caller will use it
            // for the subsequent loadMore).
            if (incoming.length > 0) {
              oldestIdRef.current = cursor ?? incoming[incoming.length - 1].id;
            } else if (cursor == null) {
              setHasMore(false);
            }
          }
          break;
        }
        case 'chat.subscribed':
          setConnected(true);
          setError(null);
          break;
        case 'chat.unsubscribed':
          setConnected(false);
          break;
        case 'chat.error': {
          const p = env.payload as { message?: string };
          if (p?.message) setError(p.message);
          break;
        }
        case 'heartbeat':
          // implicit liveness — nothing to do
          break;
      }
    });
    return () => {
      off();
    };
  }, [isAuth, scope, roomId]);

  // Subscribe / unsubscribe + request history whenever the target changes.
  // Also resubscribe on WS (re)connection via onOpen listener.
  useEffect(() => {
    if (!isAuth) {
      setMessages([]);
      setConnected(false);
      setHasMore(true);
      setLoadingMore(false);
      oldestIdRef.current = null;
      return;
    }
    if (scope === 'room' && !roomId) return;

    // Reset pagination + clear messages on scope/roomId change. The fresh
    // subscribe will refill them via the history handler. The synchronous
    // clear avoids the old scope's tail lingering while the new history
    // request is in flight.
    setMessages([]);
    historySeq.current += 1;
    loadingMoreRef.current = false;
    oldestIdRef.current = null;
    setHasMore(true);
    setLoadingMore(false);

    const doSubscribe = () => {
      wsClient.send('chat.subscribe', { scope, room_id: roomId ?? '' });
      // Latest-N fetch (no before_id = replace semantics on the backend).
      wsClient.send('chat.history', { scope, room_id: roomId ?? '', limit: HISTORY_LIMIT });
    };

    // Attempt immediate subscription (may silently fail if WS not yet open).
    doSubscribe();

    // Re-subscribe whenever the WS connection opens or reconnects.
    const offOpen = wsClient.onOpen(doSubscribe);

    return () => {
      offOpen();
      wsClient.send('chat.unsubscribe', { scope, room_id: roomId ?? '' });
    };
  }, [isAuth, scope, roomId]);

  // Oldest message id the server has sent us. Drives the next loadMore.
  const oldestIdRef = useRef<number | null>(null);

  const loadMore = useCallback(() => {
    if (!isAuth) return;
    if (loadingMoreRef.current) return;
    if (!hasMore) return;
    // Optimistically bump the in-flight flag so concurrent calls from the
    // IntersectionObserver (it can fire a few times per frame) coalesce.
    loadingMoreRef.current = true;
    setLoadingMore(true);
    const cursor = oldestIdRef.current;
    wsClient.send('chat.history', {
      scope,
      room_id: roomId ?? '',
      limit: HISTORY_LIMIT,
      // Omit before_id when the cursor is null: the backend treats that as
      // "latest N" and we dedupe/replace upstream via the history handler.
      ...(cursor != null ? { before_id: cursor } : {}),
    });
    // The in-flight flag is cleared on the next `chat.history` frame (or
    // when the subscribe re-fetch runs, which bumps historySeq and resets
    // loadingMoreRef).
  }, [isAuth, scope, roomId, hasMore]);

  const send = (text: string) => {
    const t = text.trim();
    if (!t) return;
    wsClient.connect();
    const sent = wsClient.send('chat.send', { scope, room_id: roomId ?? '', text: t });
    if (!sent) {
      const flush = () => {
        wsClient.send('chat.send', { scope, room_id: roomId ?? '', text: t });
      };
      const off = wsClient.onOpen(() => {
        off();
        flush();
      });
    }
  };

  const whisper = (toUserId: string, toAccount: string, text: string) => {
    const t = text.trim();
    if (!t || !toUserId) return;
    wsClient.connect();
    const payload = {
      scope: 'room' as ChatScope,
      room_id: roomId ?? '',
      to_user_id: toUserId,
      to_account: toAccount,
      text: t,
    };
    const sent = wsClient.send('chat.whisper', payload);
    if (!sent) {
      const flush = () => { wsClient.send('chat.whisper', payload); };
      const off = wsClient.onOpen(() => { off(); flush(); });
    }
  };

  return { messages, send, whisper, connected, error, loadMore, hasMore, loadingMore };
}

function matches(
  msgScope: ChatScope,
  msgRoomId: string | undefined,
  curScope: ChatScope,
  curRoomId: string | undefined,
): boolean {
  if (msgScope !== curScope) return false;
  if (curScope === 'room') return msgRoomId === curRoomId;
  return true;
}
