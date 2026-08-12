// Global error channel — a module-level bus that lets ANY code (pages, stores,
// services, non-React fetch/WS paths) surface a user-visible error at the
// highest layer of the UI.
//
// Why a bus instead of a zustand field:
//   - Stores/services outside React (api/*, services/ws.ts) can't use hooks,
//     but they CAN import a plain subscriber set — same rationale as
//     services/auth.events.ts.
//   - The signal carries a severity + message so the global toast can pick the
//     right styling (error vs success) instead of every caller hand-rolling a
//     toast.
//
// Canonical error-display rule (see CLAUDE.md §7.1):
//   Every Web page's server-facing API failure MUST be shown to the user on the
//   current page at the highest layer. Two acceptable surfaces:
//     1. Inline on the dialog/form the user just submitted (preferred when the
//        user is mid-flow — e.g. the edit-model modal shows the error inside
//        the modal, not as a background toast that auto-dismisses).
//     2. A top-level toast (this bus → <GlobalToast/>) when there is no active
//        dialog, or when the error is cross-cutting (auth, network, WS).
//   Never let an API failure silently disappear into a console log or a
//   page-scoped toast that the user can't see under a modal.

export type GlobalErrorSeverity = 'error' | 'success' | 'info';

export interface GlobalErrorEvent {
  message: string;
  severity?: GlobalErrorSeverity;
  /** Auto-dismiss delay in ms. Default 6000 (errors a touch longer than success). */
  durationMs?: number;
  /**
   * Optional trace id / request id for debugging. Surfaced to the browser
   * console only — never shown to the end user.
   */
  traceId?: string;
}

type Listener = (e: GlobalErrorEvent) => void;

const listeners = new Set<Listener>();

/** Subscribe to global-error events. Returns an unsubscribe fn. */
export function onGlobalError(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/**
 * Report a user-visible error (or success/info notice) at the highest UI layer.
 *
 * Accepts either a pre-formed string or a GlobalErrorEvent. A raw Error /
 * unknown thrown value is also tolerated — its `.message` (or String()) is used,
 * severity defaults to 'error'. This makes it a safe catch-all for `catch`
 * blocks: `reportGlobalError(e)`.
 *
 * The raw message is logged to the browser console for debugging but shown to
 * the user verbatim — callers should therefore pass user-safe strings (the
 * backend's `message` field is already localized-ish Chinese; the http layer's
 * ApiError.message is that same string).
 */
export function reportGlobalError(input: GlobalErrorEvent | Error | unknown): void {
  let evt: GlobalErrorEvent;
  if (input instanceof Error) {
    evt = { message: input.message || String(input), severity: 'error' };
  } else if (typeof input === 'string') {
    evt = { message: input, severity: 'error' };
  } else if (input && typeof input === 'object') {
    const e = input as GlobalErrorEvent;
    evt = { severity: 'error', ...e };
  } else {
    evt = { message: String(input), severity: 'error' };
  }
  if (evt.traceId) {
    console.warn(`[globalError] trace=${evt.traceId}: ${evt.message}`);
  }
  // Copy the set before iterating so a subscriber that unsubscribes mid-fire
  // doesn't mutate the collection under our feet.
  for (const fn of [...listeners]) fn(evt);
}

/**
 * Translate an unknown thrown value into a user-safe message string.
 * Used by catch blocks that want to forward the message to the bus without
 * re-throwing. Honors ApiError (http.ts) by preferring `.message`.
 */
export function errorMessage(e: unknown, fallback: string): string {
  if (e && typeof e === 'object' && 'message' in e) {
    const m = (e as { message?: unknown }).message;
    if (typeof m === 'string' && m) return m;
  }
  if (e instanceof Error) return e.message || fallback;
  return fallback;
}
