// WSS client. Opens a connection with the JWT in the query string and exposes
// a small pub/sub API. Reconnects with exponential backoff within a 5-minute
// window; after that, attempts a token refresh before giving up.

import { getAuthToken, setAuthToken } from './http';
import { authService } from './auth.service';
import { reportAuthExpired } from './auth.events';
import { useAuthStore } from '@/store/auth.store';
import { protoRegistry, unmarshalEnvelope, marshalEnvelope, type ProtoMessageListener } from './protoRegistry';
import { ProtoCapability, ProtoCapabilityAck } from '@/proto/common/envelope';
import { MessageType } from '@protobuf-ts/runtime';

type Listener = (env: WsEnvelope) => void;
type OpenListener = () => void;

/** Connection lifecycle status, surfaced to the UI (Loading overlay). */
export type WsStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed';
type StatusListener = (status: WsStatus) => void;

export interface WsEnvelope {
  type: string;
  seq?: number;
  payload?: unknown;
}

/** Maximum time (ms) to keep reconnecting after disconnection. */
const RECONNECT_WINDOW_MS = 5 * 60 * 1000; // 5 minutes

/**
 * Defensive sanity-check on the `expires_at` Unix-seconds returned by
 * authService.refresh(). Mirrors validateExpiresAt() in
 * store/auth.store.ts — see docs/design/login-bug-fix-design.md §2.2.2.
 * We never reject the token (next API call's 401 path arbitrates), but we
 * log a warning so clock-skew can be diagnosed in the devtools console.
 */
function validateExpiresAt(
  expiresAt: number | null | undefined,
  tag: 'refresh',
): void {
  if (expiresAt == null || !Number.isFinite(expiresAt) || expiresAt <= 0) {
    console.warn(`[auth] ${tag}: expects positive expires_at, got ${expiresAt}; token still applied, API will arbitrate`);
    return;
  }
  const nowSec = Math.floor(Date.now() / 1000);
  if (expiresAt <= nowSec) {
    const drift = nowSec - expiresAt;
    console.warn(`[auth] ${tag}: expires_at already past (server=${expiresAt}, client=${nowSec}, drift=+${drift}s). Token still applied — client clock may be fast, or server clock slow. API call will arbitrate.`);
  }
}

/**
 * Application-layer liveness check.
 *
 * Server pushes a `heartbeat` envelope every 15s via Hub.RunHeartbeat. We treat
 * it as an implicit "the server is alive" signal and use it to detect a half-
 * dead socket (e.g. NAT timeout, Wi-Fi roam) where the underlying TCP looks
 * healthy but no frames have arrived for too long. Without this, the UI would
 * stay in `open` while the server has already given up.
 *
 * Threshold = 60s: 4x server heartbeat interval, well below the 5-minute token
 * refresh window so we don't conflate "the network is dead" with "the token is
 * dead". The transport-level ping/pong (gorilla/WS, 54s) covers the truly
 * silent case; this covers the "frames stop arriving but TCP is fine" case.
 */
const STALE_FRAME_THRESHOLD_MS = 60 * 1000;

class WsClient {
  private ws: WebSocket | null = null;
  private listeners = new Set<Listener>();
  private openListeners = new Set<OpenListener>();
  private statusListeners = new Set<StatusListener>();
  private url = '';
  private backoff = 500;
  private closed = false;
  /** Set while a scheduled reconnect timer is in flight, so we don't double-schedule. */
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private _connected = false;
  private _status: WsStatus = 'idle';
  private disconnectTime: number | null = null;
  /**
   * Monotonic counter incremented on every fresh open() call. Each WebSocket's
   * onopen/onmessage/onclose/onerror handlers capture the value at creation
   * time; when they fire, they compare against the current counter and bail
   * out if the socket has been superseded. This is what stops a stale
   * WebSocket from a previous open() call from clobbering state owned by a
   * newer one — the root cause of the "reconnecting overlay never clears"
   * bug under React StrictMode's double-mount.
   */
  private generation = 0;
  /** Timestamp of the last inbound frame (any type, including heartbeat). */
  private lastInboundAt = 0;
  /** Liveness monitor: ticks every 10s and force-closes a stale socket. */
  private livenessTimer: ReturnType<typeof setInterval> | null = null;
  /**
   * Pending send buffer.
   *
   * BUG R40 P0-2 (spectator UI 永久 spinner): When the upper layer calls
   * `send()` while the underlying WebSocket is CONNECTING (post-F5 app restart,
   * wallet refresh → logout() → close() → reconnect) the message was silently
   * dropped because the previous return-false path didn't queue. The retry
   * loops in WerewolfGamePage et al. got stuck calling `send()` against a
   * stale `this.ws` reference that never sees OPEN again.
   *
   * Fix: when send() finds the socket non-OPEN, instead of returning false we
   * enqueue the envelope. The next onopen (or, in the normal CONNECTING case,
   * the existing socket finishing its handshake) drains the queue. Also cap the
   * queue so a pathological send-loop cannot grow unbounded.
   */
  private pendingSend: Array<{ type: string; payload?: unknown }> = [];
  /** Hard cap on the size of the pending-send buffer. */
  private static readonly MAX_PENDING_SEND = 256;

  // ─────────── Proto 二进制模式（新增） ───────────
  /** 是否已协商启用 proto 二进制模式 */
  private protoEnabled = false;
  /** 协商后的 proto 协议版本 */
  private protoVersion = 0;
  /** Proto 发送缓冲（连接未建立时使用） */
  private pendingProtoSend: Array<{ type: string; msgClass: MessageType<any> | null; msg: object | null; seq: number }> = [];
  /** Proto 监听器数量阈值 */
  private static readonly MAX_PENDING_PROTO_SEND = 256;

  /** Whether the WebSocket is currently in OPEN state. */
  get connected(): boolean {
    return this._connected;
  }

  /** Current connection lifecycle status. */
  get status(): WsStatus {
    return this._status;
  }

  private setStatus(s: WsStatus) {
    if (this._status === s) return;
    this._status = s;
    this.statusListeners.forEach((fn) => fn(s));
  }

  /**
   * True if the underlying socket is currently CONNECTING (handshake in flight).
   * Used to avoid creating duplicate WebSockets under React StrictMode, which
   * mounts effects twice in dev and races connect() with close().
   */
  private get isConnecting(): boolean {
    return !!this.ws && this.ws.readyState === WebSocket.CONNECTING;
  }

  private get isOpen(): boolean {
    return !!this.ws && this.ws.readyState === WebSocket.OPEN;
  }

  connect(): void {
    const token = getAuthToken();
    if (!token) {
      // Defensive: if no token, the SPA is either pre-login or in a strange
      // state where the auth store rehydrated but the http.ts module-level
      // cache hasn't seen the lsm.token value yet. This branch was the
      // root cause of "ws never opens after F5 with persisted auth" — the
      // AppLayout effect calls connect(), getAuthToken() reads the module
      // cache first (empty on a fresh page load), falls through to
      // localStorage, but if a previous code path cleared the cache without
      // writing localStorage, we silently no-op'd. Now we also try to
      // re-bootstrap from the persisted zustand state as a last resort.
      const persisted = useAuthStore.getState();
      if (persisted.token && (!persisted.expiresAt || persisted.expiresAt * 1000 > Date.now())) {
        setAuthToken(persisted.token);
        return this.connect();
      }
      // eslint-disable-next-line no-console
      console.debug('[ws] connect() skipped: no auth token');
      return;
    }
    // Already up — nothing to do. Guard prevents StrictMode double-mount from
    // tearing down a healthy connection right after it's established.
    if (this.isOpen || this.isConnecting) return;
    this.closed = false;
    this.disconnectTime = null;
    this.backoff = 500;
    // Cancel any pending reconnect — connect() means "user wants a fresh attempt now".
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    // Use same-origin path so the dev proxy / reverse-proxy routes the upgrade.
    this.url = `${proto}://${location.host}/ws?token=${encodeURIComponent(token)}`;
    // eslint-disable-next-line no-console
    console.debug('[ws] connect() opening', this.url.replace(/token=[^&]+/, 'token=…'));
    this.open();
  }

  private open() {
    if (this.closed) return;
    // Don't open a second socket while the first is still handshaking.
    if (this.isConnecting || this.isOpen) return;
    // Bump the generation: any callbacks from a previous socket are now stale.
    const gen = ++this.generation;
    // Decide what status to show *before* the WebSocket is created. We use
    // 'reconnecting' only when we are recovering from a prior disconnect
    // (disconnectTime set). A first-time connect starts as 'connecting'.
    this.setStatus(this.disconnectTime ? 'reconnecting' : 'connecting');
    let socket: WebSocket;
    try {
      socket = new WebSocket(this.url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    this.ws = socket;

    socket.onopen = () => {
      if (this.closed || gen !== this.generation) return;
      this.backoff = 500;
      this.disconnectTime = null;
      this._connected = true;
      this.lastInboundAt = Date.now();
      // 重置 proto 协商状态（每次新连接都重新协商）
      this.protoEnabled = false;
      this.protoVersion = 0;
      // 发起 proto 能力协商
      this.negotiateProto();
      this.startLivenessMonitor();
      this.setStatus('open');
      // Drain anything callers tried to send during the CONNECTING window
      // (R40 P0-2: was silently lost, now flushed once the socket is OPEN).
      this.flushPendingSend();
      // Notify all registered open listeners (used by hooks to resubscribe).
      this.openListeners.forEach((fn) => fn());
    };
    socket.onmessage = async (ev) => {
      if (this.closed || gen !== this.generation) return;
      this.lastInboundAt = Date.now();

      // ── Proto 二进制帧 ──
      if (ev.data instanceof ArrayBuffer || ev.data instanceof Uint8Array) {
        try {
          const env = unmarshalEnvelope(ev.data);
          // 协议协商响应
          if (env.type === 'system.proto_ack') {
            const ack = ProtoCapabilityAck.fromBinary(env.payload);
            this.protoEnabled = true;
            this.protoVersion = ack.version;
            // eslint-disable-next-line no-console
            console.debug('[ws] proto negotiated, version=' + ack.version + ', encoding=' + ack.encoding);
            this.flushPendingProtoSend();
            return;
          }
          // 分发给 proto 注册表
          protoRegistry.dispatch(env);
        } catch (e) {
          // eslint-disable-next-line no-console
          console.warn('[ws] proto frame decode error', e);
        }
        return;
      }

      // ── JSON 文本帧（legacy） ──
      try {
        const env = JSON.parse(ev.data) as WsEnvelope;
        this.listeners.forEach((l) => l(env));
      } catch {
        // ignore malformed frames
      }
    };
    socket.onclose = () => {
      // CRITICAL: a stale socket's onclose (from a generation we no longer own)
      // must not touch this.ws / this._connected / this.disconnectTime, and must
      // not schedule a reconnect — that work belongs to the current generation.
      if (gen !== this.generation) return;
      this.ws = null;
      this._connected = false;
      this.stopLivenessMonitor();
      if (!this.disconnectTime) this.disconnectTime = Date.now();
      this.setStatus('reconnecting');
      this.scheduleReconnect();
    };
    socket.onerror = () => {
      // Swallow errors from stale generations; the browser will fire onclose
      // right after, and onclose is the place that decides what to do.
      if (gen !== this.generation) return;
      socket.close();
    };
  }

  private async scheduleReconnect() {
    if (this.closed) return;
    // If a reconnect is already pending, don't double-schedule.
    if (this.reconnectTimer !== null) return;
    // If the user has already opened/started a fresh connection, skip.
    if (this.isOpen || this.isConnecting) return;

    // If the reconnect window has elapsed, try refreshing the JWT first.
    if (this.disconnectTime && Date.now() - this.disconnectTime > RECONNECT_WINDOW_MS) {
      try {
        const data = await authService.refresh();
        // Mirror the login-time expires_at sanity check (login-bug-fix-design.md
        // §2.2.2). Refresh is the second most likely place to surface a clock
        // skew between server and client, since it runs after a long disconnect
        // (network down, laptop sleep) where the client clock may have drifted.
        validateExpiresAt(data.expires_at, 'refresh');
        setAuthToken(data.token);
        // Rebuild URL with fresh token and reset the window.
        const proto = location.protocol === 'https:' ? 'wss' : 'ws';
        this.url = `${proto}://${location.host}/ws?token=${encodeURIComponent(data.token)}`;
        this.disconnectTime = Date.now(); // restart the window
      } catch {
        // Token refresh failed (typically the refresh token itself is expired).
        // Stop the reconnect loop WITHOUT surfacing anything on its own — the
        // very next REST call from a page will hit http()'s 401 path and open
        // the friendly login modal. Mirrored here in case no page makes one:
        this.closed = true;
        if (this.reconnectTimer !== null) {
          clearTimeout(this.reconnectTimer);
          this.reconnectTimer = null;
        }
        this.setStatus('closed');
        // Tell the UI to show the friendly notice + reopen AuthModal.
        void useAuthStore.getState().logout().catch(() => undefined);
        reportAuthExpired('expired');
        return;
      }
    }

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      // Re-check guards inside the timer: another open() may have run in between.
      if (this.closed) return;
      if (this.isOpen || this.isConnecting) return;
      this.open();
    }, this.backoff);
    this.backoff = Math.min(this.backoff * 2, 8000);
  }

  close() {
    this.closed = true;
    this._connected = false;
    this.disconnectTime = null;
    this.stopLivenessMonitor();
    // Invalidate any in-flight socket handlers so they become no-ops.
    this.generation++;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    // Drop any buffered envelopes — close() means the consumer is done; the
    // next open() drains against a fresh queue.
    this.pendingSend.length = 0;
    this.setStatus('idle');
    this.ws?.close();
    this.ws = null;
  }

  /**
   * startLivenessMonitor begins a 10-second polling loop that, while we believe
   * the socket is open, force-closes it if no inbound frame has arrived in the
   * last STALE_FRAME_THRESHOLD_MS (60s). This protects against silent network
   * failures (NAT timeout, captive portal, Wi-Fi roam) where TCP looks healthy
   * but the server has stopped reaching us. Closing forces onclose → reconnect.
   */
  private startLivenessMonitor() {
    this.stopLivenessMonitor();
    this.livenessTimer = setInterval(() => {
      if (!this._connected) return;
      if (this.lastInboundAt === 0) return;
      if (Date.now() - this.lastInboundAt < STALE_FRAME_THRESHOLD_MS) return;
      // Force-close the underlying socket. onclose will then transition us to
      // `reconnecting` and the standard backoff schedule will kick in.
      try {
        this.ws?.close();
      } catch {
        // best-effort; onclose has already happened in pathological cases
      }
    }, 10_000);
  }

  private stopLivenessMonitor() {
    if (this.livenessTimer !== null) {
      clearInterval(this.livenessTimer);
      this.livenessTimer = null;
    }
  }

  on(fn: Listener) {
    this.listeners.add(fn);
    return () => {
      this.listeners.delete(fn);
    };
  }

  /** Register a callback that fires each time the WS connection opens (including reconnects). */
  onOpen(fn: OpenListener) {
    this.openListeners.add(fn);
    return () => {
      this.openListeners.delete(fn);
    };
  }

  /** Register a callback for connection-status changes (drives the Loading overlay). */
  onStatus(fn: StatusListener) {
    this.statusListeners.add(fn);
    return () => {
      this.statusListeners.delete(fn);
    };
  }

  // send pushes a JSON envelope to the server. When the underlying socket is
  // OPEN the envelope is sent immediately and the call returns true. If the
  // socket is in any other state (CONNECTING, CLOSING, CLOSED — including
  // "currently reconnecting after a transient disconnect") the envelope is
  // buffered and flushed once the next onopen fires. The buffer is bounded
  // by WsClient.MAX_PENDING_SEND so a pathological send-loop cannot leak
  // memory.
  //
  // Returns true if the envelope was sent (or kept in the pending buffer for
  // immediate flush on next open), false only when the client has been
  // deliberately closed via close() and is not coming back.
  send(type: string, payload?: unknown) {
    if (this.closed) return false;
    if (this.isOpen && this.ws) {
      const env = { type, payload };
      try {
        this.ws.send(JSON.stringify(env));
        return true;
      } catch {
        // ws.send can throw if the browser races the close handshake; fall
        // through to enqueue so the next open() flushes it.
      }
    }
    // Not open (or send threw). Enqueue for the next onopen. If the queue is
    // already saturated we drop the *oldest* entries — newer state-changing
    // frames are usually more useful than stale ones.
    if (this.pendingSend.length >= WsClient.MAX_PENDING_SEND) {
      const drop = this.pendingSend.length - WsClient.MAX_PENDING_SEND + 1;
      this.pendingSend.splice(0, drop);
    }
    this.pendingSend.push({ type, payload });
    return true;
  }

  /**
   * Flush every buffered envelope to the currently-OPEN socket. Must only be
   * called from socket.onopen after the state has transitioned to OPEN and
   * the generation check has passed.
   */
  private flushPendingSend() {
    if (this.pendingSend.length === 0) return;
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const drain = this.pendingSend;
    this.pendingSend = [];
    for (const env of drain) {
      try {
        this.ws.send(JSON.stringify(env));
      } catch {
        // If a send throws mid-drain we put the failed envelope back at the
        // head of the queue and stop; onopen for the next generation will
        // retry it.
        this.pendingSend.unshift(env);
        return;
      }
    }
  }

  // ─────────── Proto 二进制方法 ───────────

  /**
   * 发起 proto 协议协商
   * 发送 system.proto_capability 帧，等待服务端回 system.proto_ack
   */
  private negotiateProto(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    // 用 JSON 发送协商请求（服务端在 JSON 通道也能接收）
    const cap: ProtoCapability = { version: 1, encodings: ['binary'] };
    try {
      // 用 JSON 发送（确保兼容旧服务端）
      const jsonEnv = { type: 'system.proto_capability', payload: cap };
      this.ws.send(JSON.stringify(jsonEnv));
    } catch {
      // 忽略
    }
  }

  /**
   * 发送 proto 二进制消息
   * 如果 proto 尚未协商，消息会被缓冲，协商完成后自动发送。
   */
  sendProto<T extends object>(type: string, msgClass: MessageType<T>, msg: T, seq = 0): boolean {
    if (this.closed) return false;

    if (this.protoEnabled && this.isOpen && this.ws) {
      try {
        const data = marshalEnvelope(type, msgClass, msg, seq);
        this.ws.send(data);
        return true;
      } catch {
        // 发送失败，入缓冲
      }
    }

    // 缓冲等待
    if (this.pendingProtoSend.length >= WsClient.MAX_PENDING_PROTO_SEND) {
      const drop = this.pendingProtoSend.length - WsClient.MAX_PENDING_PROTO_SEND + 1;
      this.pendingProtoSend.splice(0, drop);
    }
    this.pendingProtoSend.push({ type, msgClass, msg, seq });
    return true;
  }

  /**
   * 订阅 proto 消息类型
   * 返回取消订阅函数
   */
  onProto<T extends object>(
    type: string,
    msgClass: MessageType<T>,
    listener: ProtoMessageListener<T>,
  ): () => void {
    return protoRegistry.on(type, msgClass, listener);
  }

  /** 是否启用了 proto 二进制模式 */
  get isProtoEnabled(): boolean {
    return this.protoEnabled;
  }

  /** 协商后的 proto 协议版本（0 = 未协商） */
  get negotiatedProtoVersion(): number {
    return this.protoVersion;
  }

  /**
   * 刷新 proto 发送缓冲（协商成功后调用）
   */
  private flushPendingProtoSend(): void {
    if (this.pendingProtoSend.length === 0) return;
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const drain = this.pendingProtoSend;
    this.pendingProtoSend = [];
    for (const item of drain) {
      try {
        const data = marshalEnvelope(item.type, item.msgClass, item.msg, item.seq);
        this.ws!.send(data);
      } catch {
        this.pendingProtoSend.unshift(item);
        return;
      }
    }
  }
}

export const wsClient = new WsClient();
