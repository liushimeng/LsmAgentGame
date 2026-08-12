// 2026-07-12 §13 增强 — Bot 发言 SSE 流式预览气泡。
//
// 后端在 SendFromBot 广播 chat.message(权威最终文本)之前,先发三条
//   chat.stream_start { stream_id, room_id, seat, ts }
//   chat.stream_delta { stream_id, delta, index, ts }
//   chat.stream_end   { stream_id, full_text, ts }
// 让前端在 LLM 仍在生成时就能看到"token 瀑布流"预览。
//
// 流式气泡是「预览」:当收到同一个 bot 的权威 chat.message 后,
// 按 ts 窗口 + seat 维度匹配(±5s)删除对应的气泡,避免与正式消息重复渲染。
//
// §优化-20260730-01 — 五项 Bug 修复(原「直播」死循环 / 多房间串扰):
//   1. streams 按 room_id 二级 Map 隔离,切房间不串扰;
//   2. MAX_AGE_MS 60_000 → 15_000(无 delta 15s 视为卡死);
//   3. 后台 5s setInterval 主动 GC,不依赖 WS 帧到达;
//   4. stream_start 后 30s 无任何后续帧 → 主动 finalize + 删;
//   5. chat.message 匹配改为按 (seat, ts) 二维,避免误删他 bot 的流。

import { useSyncExternalStore } from 'react';
import { wsClient } from '@/services/ws';

interface StreamEntry {
  stream_id: string;
  room_id: string;
  seat: number;
  text: string;
  ts: number;
  /** §优化-20260730-01 — 最近一次 delta / end 到达时间;用于 30s 无动作兜底 */
  lastDeltaAt: number;
  finalized: boolean;
  indices: Set<number>;
}

// ── 模块级 store ────────────────────────────────────────────────
// §优化-20260730-01 — 二级 Map:room_id → (stream_id → entry),支持多房间隔离。
const streamsByRoom = new Map<string, Map<string, StreamEntry>>();
const listeners = new Set<() => void>();
let snapshot: StreamEntry[] = [];
let version = 0;
let wsOff: (() => void) | null = null;
let wsAttached = false;
let gcTimer: ReturnType<typeof setInterval> | null = null;

const FINALIZE_WINDOW_MS = 5000;   // chat.message 与 stream.ts 的最大匹配窗口
const MAX_AGE_MS = 15_000;          // §优化-20260730-01:无 delta 15s 视作卡死
const MAX_FINALIZED_MS = 1500;      // §优化-20260730-01:finalize 后 1.5s 即 GC
const MAX_SILENT_MS = 30_000;       // §优化-20260730-01:stream_start 后 30s 无任何帧即主动 finalize
const GC_INTERVAL_MS = 5_000;       // §优化-20260730-01:主动 GC 周期

function buildSnapshot(): StreamEntry[] {
  const out: StreamEntry[] = [];
  for (const room of streamsByRoom.values()) {
    for (const e of room.values()) out.push(e);
  }
  out.sort((a, b) => a.ts - b.ts);
  return out;
}

function emit() {
  const now = Date.now();
  for (const [roomId, room] of streamsByRoom) {
    for (const [id, e] of room) {
      if (e.finalized && now - e.lastDeltaAt > MAX_FINALIZED_MS) {
        room.delete(id);
      } else if (now - e.lastDeltaAt > MAX_AGE_MS) {
        room.delete(id);
      }
    }
    if (room.size === 0) streamsByRoom.delete(roomId);
  }
  snapshot = buildSnapshot();
  version++;
  listeners.forEach((l) => l());
}

function ensureRoom(roomId: string): Map<string, StreamEntry> {
  let room = streamsByRoom.get(roomId);
  if (!room) {
    room = new Map<string, StreamEntry>();
    streamsByRoom.set(roomId, room);
  }
  return room;
}

function pickStreamForMessage(seat: number, ts: number, roomId?: string): StreamEntry | null {
  let best: StreamEntry | null = null;
  let bestDiff = Number.POSITIVE_INFINITY;
  const rooms = roomId ? [streamsByRoom.get(roomId)].filter(Boolean) as Map<string, StreamEntry>[] : Array.from(streamsByRoom.values());
  for (const room of rooms) {
    for (const e of room.values()) {
      if (e.seat !== seat) continue;
      const diff = Math.abs(ts - e.ts);
      if (diff <= FINALIZE_WINDOW_MS && diff < bestDiff) {
        best = e;
        bestDiff = diff;
      }
    }
  }
  return best;
}

function onFrame(env: { type: string; payload?: any }): void {
  switch (env.type) {
    case 'chat.stream_start': {
      const p = env.payload ?? {};
      const streamId = String(p.stream_id ?? '');
      const roomId = String(p.room_id ?? '');
      if (!streamId || !roomId) return;
      const room = ensureRoom(roomId);
      // §优化-20260730-01 — 同 stream_id 重复 start 时覆盖,避免断网重连后重复气泡。
      const existing = room.get(streamId);
      if (existing && !existing.finalized) {
        // 已经在流 → 保留 lastDeltaAt 不变,仅刷新 ts 让前端感知重启
        existing.ts = Number(p.ts ?? Date.now());
      } else {
        room.set(streamId, {
          stream_id: streamId,
          room_id: roomId,
          seat: Number(p.seat ?? -1),
          text: '',
          ts: Number(p.ts ?? Date.now()),
          lastDeltaAt: Date.now(),
          finalized: false,
          indices: new Set<number>(),
        });
      }
      emit();
      break;
    }
    case 'chat.stream_delta': {
      const p = env.payload ?? {};
      const streamId = String(p.stream_id ?? '');
      if (!streamId) return;
      // §优化-20260730-01 — 反查 room,因为端上可能不送 room_id。
      let entry: StreamEntry | undefined;
      for (const room of streamsByRoom.values()) {
        entry = room.get(streamId);
        if (entry) break;
      }
      if (!entry) return;
      const idx = Number(p.index ?? -1);
      if (idx >= 0 && entry.indices.has(idx)) return;
      if (idx >= 0) entry.indices.add(idx);
      entry.text += (p.delta ?? '');
      entry.lastDeltaAt = Date.now();
      emit();
      break;
    }
    case 'chat.stream_end': {
      const p = env.payload ?? {};
      const streamId = String(p.stream_id ?? '');
      if (!streamId) return;
      let entry: StreamEntry | undefined;
      for (const room of streamsByRoom.values()) {
        entry = room.get(streamId);
        if (entry) break;
      }
      if (!entry) return;
      if (typeof p.full_text === 'string') entry.text = p.full_text;
      entry.finalized = true;
      entry.lastDeltaAt = Date.now();
      emit();
      break;
    }
    case 'chat.message': {
      // §优化-20260730-01 — 改为按 (room_id, seat) 二维匹配,避免误删他 bot 流。
      const m = env.payload ?? {};
      if (m.from_role !== 'bot') return;
      const ts = Number(m.ts ?? 0);
      if (!ts) return;
      const seat = Number(m.from_seat ?? -1);
      if (seat < 0) return;
      const roomId = String(m.room_id ?? '');
      const hit = pickStreamForMessage(seat, ts, roomId || undefined);
      if (hit) {
        const room = streamsByRoom.get(hit.room_id);
        room?.delete(hit.stream_id);
        if (room && room.size === 0) streamsByRoom.delete(hit.room_id);
        emit();
      }
      break;
    }
    default:
      break;
  }
}

function startGcTimer() {
  if (gcTimer) return;
  // §优化-20260730-01 — 周期性 GC,不依赖 WS 帧到达;同时把超时无动作的 stream 标 finalize。
  gcTimer = setInterval(() => {
    const now = Date.now();
    for (const room of streamsByRoom.values()) {
      for (const e of room.values()) {
        if (!e.finalized && now - e.ts > MAX_SILENT_MS) {
          // stream_start 后 30s 还没 end → 主动 finalize,前端 MAX_FINALIZED_MS 后会自动消失。
          e.finalized = true;
          e.lastDeltaAt = now;
        }
      }
    }
    emit();
  }, GC_INTERVAL_MS);
}

function stopGcTimer() {
  if (!gcTimer) return;
  clearInterval(gcTimer);
  gcTimer = null;
}

function attachWs() {
  if (wsAttached) return;
  wsAttached = true;
  wsOff = wsClient.on(onFrame);
  startGcTimer();
}

function detachWs() {
  if (!wsAttached) return;
  wsAttached = false;
  wsOff?.();
  wsOff = null;
  stopGcTimer();
}

// ── 订阅 API ────────────────────────────────────────────────────
function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  attachWs();
  return () => {
    listeners.delete(cb);
    if (listeners.size === 0) {
      // 无订阅者时清空残流 + 卸载 WS 监听,避免幽灵渲染/泄漏。
      streamsByRoom.clear();
      snapshot = [];
      detachWs();
    }
  };
}

function getSnapshot(): StreamEntry[] {
  return snapshot;
}

function getServerSnapshot(): StreamEntry[] {
  return [];
}

/**
 * 订阅当前房间的 Bot 流式预览气泡数组(按 ts 升序)。
 * 在 GameChatPanel 中渲染 token 瀑布流;权威 chat.message 到达后自动消隐。
 *
 * §优化-20260730-01 — 返回前过滤掉「空文本且超时 3s」的僵尸气泡,避免显示
 *   "Bot #N 直播 ▍" 但下面无任何内容的垃圾信息。
 */
export function useStreamingMessages(): StreamEntry[] {
  const all = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
  const now = Date.now();
  return all.filter((e) => {
    if (e.text.trim() !== '') return true;
    if (e.finalized) return now - e.lastDeltaAt <= MAX_FINALIZED_MS;
    return now - e.ts <= 3000;
  });
}
