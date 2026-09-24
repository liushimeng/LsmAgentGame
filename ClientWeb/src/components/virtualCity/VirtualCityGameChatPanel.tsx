//
// VirtualCity-specialized GameChatPanel — 薄适配器（仿 werewolf/GameChatPanel 87 行版）。
// 包裹 components/chat/GameChatPanel（共享目录，合法），把 gameState.players 映射为
// roomPlayers（@mention 自动补全 + 💬 私聊快捷入口），不发财富流业务帧。
//
// 见 CLAUDE.md §16（聊天系统架构）与前端架构文档 §5 面板清单。
//

import React, { useMemo } from 'react';
import { GameChatPanel as SharedGameChatPanel } from '@/components/chat/GameChatPanel';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { VirtualCityGameState } from '@/types/virtualCity';

interface Props {
  roomId: string;
  gameState: VirtualCityGameState | null;
  /** 当前月数（标题旁展示，与 werewolf currentDay 同款）。 */
  currentMonth?: number | null;
}

/**
 * players[] → roomPlayers[]（bot 用 model_display 拼昵称；人类用 N 号）。
 *
 * 2026-09-16 §财商流10–12座位：players[] 恒为 max_seat(12) 长度，未入座的是
 * account/nickname 全空的占位对象 —— 不过滤会让 @mention 列表出现一堆「玩家N号」
 * 幽灵条目。座位号一律用权威的 p.seat（不再用数组下标 i）。
 */
function toRoomPlayers(
  gs: VirtualCityGameState | null,
  fallbackName: (seatNo: number) => string,
): { user_id: string; nickname: string }[] {
  if (!gs) return [];
  return gs.players
    .filter((p) => !!p && (!!p.account || !!p.nickname))
    .map((p) => {
      const role = p.profession?.title ? `(${p.profession.title})` : '';
      const nickname = p.is_bot
        ? `${p.model_display || 'Bot'} #${p.seat + 1}${role}`
        : `${p.nickname || fallbackName(p.seat + 1)}${role}`;
      return { user_id: p.account, nickname };
    });
}

export const VirtualCityGameChatPanel: React.FC<Props> = ({ roomId, gameState, currentMonth }) => {
  const t = useT();
  const spectator = useSpectatorMode();
  const roomPlayers = useMemo(
    () => toRoomPlayers(gameState, (n) => t('virtualCity.chat.playerFallback' as TKey, { n })),
    [gameState, t],
  );
  return (
    <SharedGameChatPanel
      roomId={roomId}
      roomPlayers={roomPlayers}
      isSpectator={spectator}
      isLocalPlayerDead={false}
      currentDay={currentMonth ?? undefined}
    />
  );
};
