/**
 * useVirtualCitySpeech — 批次 23：房间聊天面板删除后，座位居民（bot）的公开发话
 * 不再落在聊天列表，改为写入 store.speechBubbles，由 3D 地图 AgentToken 头顶冒泡。
 *
 * 数据流（方案 §3.3.1）：
 *   chat.message (from_role='bot') ──► 本 hook ──► store.speechBubbles[seat]
 *
 * 刻意不走 useChat：它会发 chat.subscribe / chat.history，本批次正要让虚拟城市
 * 与聊天服务脱钩（后端 G2 已拦截人类 chat.send/whisper）。本 hook 只被动监听
 * chat.message 广播帧，不发任何 chat.* 帧。
 */

import { useEffect } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import type { ChatMessage } from '@/types/api';
import type { VirtualCityPlayer } from '@/types/virtualCity';

/** 空座位占位对象（players[] 恒为 max_seat 长度）判定：account/nickname 全空。 */
function isOccupied(p: VirtualCityPlayer): boolean {
  return !!p && (!!p.account || !!p.nickname);
}

/** bot 显示名：`model_display #座位` 风格（参考被删面板 toRoomPlayers 拼法，省职业后缀）。 */
function displayName(p: VirtualCityPlayer): string {
  return `${p.model_display || 'Bot'} #${p.seat + 1}`;
}

export function useVirtualCitySpeech(roomId: string) {
  const setSpeechBubble = useVirtualCityStore((s) => s.setSpeechBubble);

  useEffect(() => {
    if (!roomId) return;
    const off = wsClient.on((env: WsEnvelope) => {
      if (env.type !== 'chat.message') return;
      const m = env.payload as ChatMessage;
      if (!m || m.room_id !== roomId) return;
      if (m.from_role !== 'bot') return;
      if (!m.text) return;

      const players = useVirtualCityStore.getState().gameState?.players ?? [];
      // 主匹配：from_user_id === p.account；兜底：from_agent_name === p.model_display。
      const hit =
        players.find((p) => isOccupied(p) && p.account === m.from_user_id) ??
        players.find(
          (p) =>
            isOccupied(p) &&
            !!p.model_display &&
            !!m.from_agent_name &&
            p.model_display === m.from_agent_name,
        );
      if (!hit) return;

      setSpeechBubble({
        seat: hit.seat,
        name: displayName(hit),
        text: m.text,
        ts: Date.now(),
      });
    });
    return off;
  }, [roomId, setSpeechBubble]);
}
