// 2026-08-23 §德扑Agent聊天 — 座位级 bot 发言气泡 store/hook。
//
// 数据源:后端 SendFromBot 广播的 chat.message 帧(from_role === 'bot',
// 携带 room_id + from_seat)。本 hook 按 (room_id, seat) 匹配,在座位旁显示
// ≤3 秒的发言气泡(设计文档 §4.3),到期自动消隐。
//
// 实现复刻 useStreamingMessages 的模块级 useSyncExternalStore 模式:
//   - 多房间按 room_id 二级 Map 隔离(不串扰);
//   - 3s TTL 到期后由 GC 定时器 / 下一次消息到达时清理;
//   - 无订阅者时卸载 WS 监听并清空,避免幽灵渲染/泄漏。
//
// 德扑私有(仅 components/texasholdem/ 引用),不进 shared/(§2.1 硬约束)。

import { useEffect, useSyncExternalStore } from 'react';
import { wsClient } from '@/services/ws';

export interface SeatChatBubble {
  seat: number;
  text: string;
  /** 气泡应显示到的时刻(Date.now() ms);过期即 GC。 */
  until: number;
  ts: number;
}

const BUBBLE_TTL_MS = 3000;
const GC_INTERVAL_MS = 1000;

// room_id -> (seat -> bubble)
const bubblesByRoom = new Map<string, Map<number, SeatChatBubble>>();
const listeners = new Set<() => void>();
let snapshot: Record<number, SeatChatBubble> = {};
let currentRoomId = '';
let wsOff: (() => void) | null = null;
let wsAttached = false;
let gcTimer: ReturnType<typeof setInterval> | null = null;

function emit(): void {
  const now = Date.now();
  for (const [roomId, room] of bubblesByRoom) {
    for (const [seat, b] of room) {
      if (now >= b.until) room.delete(seat);
    }
    if (room.size === 0) bubblesByRoom.delete(roomId);
  }
  const room = currentRoomId ? bubblesByRoom.get(currentRoomId) : undefined;
  snapshot = room ? Object.fromEntries(room) : {};
  listeners.forEach((l) => l());
}

function onFrame(env: { type: string; payload?: any }): void {
  if (env.type !== 'chat.message') return;
  const m = env.payload ?? {};
  if (m.from_role !== 'bot') return;
  const roomId = String(m.room_id ?? '');
  const seat = Number(m.from_seat ?? -1);
  const text = String(m.text ?? '').trim();
  if (!roomId || seat < 0 || !text) return;
  const now = Date.now();
  let room = bubblesByRoom.get(roomId);
  if (!room) {
    room = new Map<number, SeatChatBubble>();
    bubblesByRoom.set(roomId, room);
  }
  room.set(seat, { seat, text, until: now + BUBBLE_TTL_MS, ts: Number(m.ts ?? now) });
  emit();
}

function attachWs(): void {
  if (wsAttached) return;
  wsAttached = true;
  wsOff = wsClient.on(onFrame);
  if (!gcTimer) {
    gcTimer = setInterval(() => {
      // 仅有到期气泡需要清理时才触发重渲染(emit 内部自会过滤)。
      emit();
    }, GC_INTERVAL_MS);
  }
}

function detachWs(): void {
  if (!wsAttached) return;
  wsAttached = false;
  wsOff?.();
  wsOff = null;
  if (gcTimer) {
    clearInterval(gcTimer);
    gcTimer = null;
  }
}

function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  attachWs();
  return () => {
    listeners.delete(cb);
    if (listeners.size === 0) {
      bubblesByRoom.clear();
      snapshot = {};
      detachWs();
    }
  };
}

function getSnapshot(): Record<number, SeatChatBubble> {
  return snapshot;
}

function getServerSnapshot(): Record<number, SeatChatBubble> {
  return {};
}

/**
 * 订阅指定房间「seat → 3s bot 发言气泡」映射;无气泡时对应座位无键。
 * 返回对象引用在快照不变时稳定(useSyncExternalStore 语义)。
 */
export function useSeatChatBubbles(roomId: string): Record<number, SeatChatBubble> {
  currentRoomId = roomId;
  // 切房间时立即重建快照(不等下一帧 chat.message 到达)。
  useEffect(() => {
    currentRoomId = roomId;
    emit();
  }, [roomId]);
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
