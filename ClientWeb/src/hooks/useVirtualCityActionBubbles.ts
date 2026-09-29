/**
 * useVirtualCityActionBubbles — 批次 35 §5.1：座位居民的动作/移动/感知事件
 * 在 3D 人物头顶实时冒泡（8s TTL，渲染端按 ts 判过期）。
 *
 * 数据流：
 *   game.event{seat≥0, type∈action|move|sense} ──► 本 hook ──► store.actionBubbles[seat]
 *
 * 刻意**不走 eventFeed**：CityMap 是 memo 的，订阅 eventFeed 会让每条 WS 事件
 * 全量打进 R3F 场景树（批次 28 A1 专门把页面级 state 挡在场景外）。本 hook 只在
 * 命中座位动作帧时写 store 新字段 actionBubbles（每座位一条，帧到达才变化，低频安全）。
 * 模式照抄 useVirtualCitySpeech（chat.message → speechBubbles）。
 */

import { useEffect } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import type { VirtualCityEventFrame } from '@/types/virtualCity';

/** 座位居民动作类事件（§5.1 图标映射：action→💭 / move→🚶 / sense→👀）。 */
const ACTION_EVENT_TYPES = new Set(['action', 'move', 'sense']);

export function useVirtualCityActionBubbles(roomId: string) {
  const setActionBubble = useVirtualCityStore((s) => s.setActionBubble);

  useEffect(() => {
    if (!roomId) return;
    const off = wsClient.on((env: WsEnvelope) => {
      if (env.type !== 'game.event') return;
      const ev = env.payload as VirtualCityEventFrame;
      if (!ev) return;
      // 只处理当前房间的帧（game.event 带 room_id；无 room_id 的帧不过滤，与
      // useVirtualCity 的 room 匹配口径一致宽松）。
      if (ev.room_id && ev.room_id !== roomId) return;
      // 背景层事件（city_voice / city_profiles 等）与无座位广播不冒泡。
      if (typeof ev.seat !== 'number' || ev.seat < 0) return;
      if (!ACTION_EVENT_TYPES.has(ev.type)) return;
      if (!ev.text) return;

      setActionBubble({
        seat: ev.seat,
        type: ev.type,
        text: ev.text,
        ts: Date.now(),
      });
    });
    return off;
  }, [roomId, setActionBubble]);
}
