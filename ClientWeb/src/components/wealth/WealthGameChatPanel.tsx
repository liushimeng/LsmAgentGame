//
// Wealth-specialized GameChatPanel — 薄适配器（仿 werewolf/GameChatPanel 87 行版）。
// 包裹 components/chat/GameChatPanel（共享目录，合法），把 gameState.players 映射为
// roomPlayers（@mention 自动补全 + 💬 私聊快捷入口），不发财富流业务帧。
//
// 见 CLAUDE.md §16（聊天系统架构）与前端架构文档 §5 面板清单。
//

import React, { useMemo } from 'react';
import { GameChatPanel as SharedGameChatPanel } from '@/components/chat/GameChatPanel';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import type { WealthGameState } from '@/types/wealth';

interface Props {
  roomId: string;
  gameState: WealthGameState | null;
  /** 当前月数（标题旁展示，与 werewolf currentDay 同款）。 */
  currentMonth?: number | null;
}

/** players[] → roomPlayers[]（bot 用 model_display 拼昵称；人类用 N 号）。 */
function toRoomPlayers(gs: WealthGameState | null): { user_id: string; nickname: string }[] {
  if (!gs) return [];
  return gs.players.map((p, i) => {
    const role = p.profession.title ? `(${p.profession.title})` : '';
    const nickname = p.is_bot
      ? `${p.model_display || 'Bot'} #${i + 1}${role}`
      : `${p.nickname || `玩家${i + 1}号`}${role}`;
    return { user_id: p.account, nickname };
  });
}

export const WealthGameChatPanel: React.FC<Props> = ({ roomId, gameState, currentMonth }) => {
  const spectator = useSpectatorMode();
  const roomPlayers = useMemo(() => toRoomPlayers(gameState), [gameState]);
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
