// GlobalToast — highest-layer, center-top toast that surfaces API / WS errors
// reported through the services/globalError.ts bus.
//
// Mount point: AppLayout (once, for the whole app). Renders via createPortal to
// document.body so it sits ABOVE every modal (modals use z-index 200; we use
// 1000) and is never clipped by overflow/transform ancestors.
//
// Design:
//   - A small queue of messages; only the head is shown at a time. Each entry
//     auto-dismisses after its own durationMs (default 6000, errors 8000).
//   - Severity drives color: error (red) / success (green) / info (blue).
//   - Manual dismiss via the × button; clicking does NOT navigate.
//
// This is the canonical "surface the error at the highest layer" surface for
// errors that aren't tied to an active dialog. Errors that happen *inside* a
// modal (e.g. the edit-model form) should be shown inline in that modal — see
// ModelAdminPage's formError state — and only fall back to this toast when
// there is no better surface.

import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { onGlobalError, type GlobalErrorEvent } from '@/services/globalError';

interface Entry extends GlobalErrorEvent {
  id: number;
}

const DEFAULT_DURATION_MS = 6000;
const ERROR_DURATION_MS = 8000;

/** z-index must exceed AppModal's 200 so the toast is always the top layer. */
const Z_INDEX = 1000;

export function GlobalToast() {
  const [entry, setEntry] = useState<Entry | null>(null);
  const queueRef = useRef<Entry[]>([]);
  const nextId = useRef(1);
  const hideTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showNext = useCallback(() => {
    const next = queueRef.current.shift() ?? null;
    setEntry(next);
  }, []);

  const dismiss = useCallback(() => {
    if (hideTimer.current) clearTimeout(hideTimer.current);
    setEntry(null);
    // Give the CSS fade-out a beat before showing the next queued entry.
    hideTimer.current = setTimeout(showNext, 120);
  }, [showNext]);

  // Subscribe: if nothing is showing, show immediately and (re)start the
  // auto-dismiss timer; otherwise enqueue — the queued entry will be shown +
  // timed when it reaches the head via the [entry] effect below.
  useEffect(() => {
    return onGlobalError((evt) => {
      const id = nextId.current++;
      const durationMs =
        evt.durationMs ?? (evt.severity === 'error' ? ERROR_DURATION_MS : DEFAULT_DURATION_MS);
      const newEntry: Entry = { ...evt, id, durationMs };

      setEntry((cur) => {
        if (cur == null) {
          // Becomes the head right away — (re)start its auto-dismiss timer.
          if (hideTimer.current) clearTimeout(hideTimer.current);
          hideTimer.current = setTimeout(() => setEntry((c) => (c && c.id === id ? null : c)), durationMs);
          return newEntry;
        }
        queueRef.current.push(newEntry);
        return cur;
      });
    });
  }, []);

  // When the head entry clears, advance to the next queued entry after a beat.
  useEffect(() => {
    if (entry != null) return;
    const t = setTimeout(showNext, 120);
    return () => clearTimeout(t);
  }, [entry, showNext]);

  // Clean up any pending timer on unmount.
  useEffect(() => {
    return () => {
      if (hideTimer.current) clearTimeout(hideTimer.current);
    };
  }, []);

  if (!entry) return null;

  const severity = entry.severity ?? 'error';
  const className =
    'global-toast' +
    (severity === 'success' ? ' global-toast--success' : '') +
    (severity === 'info' ? ' global-toast--info' : '') +
    (severity === 'error' ? ' global-toast--error' : '');

  return createPortal(
    <div
      className={className}
      role="alert"
      data-testid="global-toast"
      data-severity={severity}
      style={{ zIndex: Z_INDEX }}
    >
      <span className="global-toast__icon" aria-hidden>
        {severity === 'success' ? '✅' : severity === 'info' ? 'ℹ️' : '⚠️'}
      </span>
      <span className="global-toast__text">{entry.message}</span>
      <button
        type="button"
        className="global-toast__close"
        onClick={dismiss}
        aria-label="关闭"
      >
        ×
      </button>
    </div>,
    document.body,
  );
}
