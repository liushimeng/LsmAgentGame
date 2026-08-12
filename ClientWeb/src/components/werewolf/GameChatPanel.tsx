//
// Werewolf-specialized GameChatPanel. Wraps the shared xiangqi variant and
// injects gameState.players (mapped to roomPlayers). This lets the werewolf
// page offer @mention autocomplete and the 💬 whisper shortcut without
// duplicating the chat UI.
//
// See docs/狼人杀/00-游戏信息与Agent现状综合文档.md §3.4.

import React, { useMemo } from 'react';
import { GameChatPanel as SharedGameChatPanel } from '@/components/chat/GameChatPanel';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { useT } from '@/hooks/useT';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  roomId: string;
  gameState: WerewolfGameState | null;
  myUserId?: string | null;
  /** R235 §4.2: pass the dead-state of the local player so the shared chat
   *  panel can wipe any pending draft on the rising edge of death.
   *  False for spectators and pre-game lobbies. */
  isLocalPlayerDead?: boolean;
  /** 2026-08-07 §房间聊天优化 — 透传给内层 GameChatPanel 显示 Day N。 */
  currentDay?: number | null;
}

function toRoomPlayers(
  gs: WerewolfGameState | null,
  myUserId?: string | null,
  t?: (key: any) => string,
): { user_id: string; nickname: string }[] {
  if (!gs) return [];
  const out: { user_id: string; nickname: string }[] = [];
  for (let i = 0; i < gs.max_seat; i++) {
    const uid = gs.seats[i];
    if (!uid || uid === myUserId) continue;
    const p = gs.players?.[i];
    // §135 纵深防御:@mention 候选项只在服务端明确 role_revealed 时才带角色名。
    // 此前无条件拼 `(角色名)`,完全依赖后端脱敏 —— 后端一旦回归就全场泄露。
    const role =
      p?.role_revealed && p?.role
        ? `(${t ? t(`werewolf.role.${p.role}` as any) : p.role})`
        : '';
    // BUG-R73-P1 (@mention 显示): bot 的 user_id 以 "bot_" 开头,用
    // agent_name(Xiaomi mimo-v2.5-pro 等 LLM 展示名)拼昵称; 缺失时回退
    // 到 "Bot #N"。真人玩家显示 "N号"(1-indexed,与 UI 一致)。
    const isBot = p?.user_id?.startsWith('bot_');
    const agentName = p?.agent_name;
    const acct = isBot
      ? (agentName ? `${agentName} #${i + 1}${role}` : `Bot #${i + 1}${role}`)
      : `玩家${i + 1}号${role}`;
    out.push({ user_id: uid, nickname: acct });
  }
  return out;
}

export const WerewolfGameChatPanel: React.FC<Props> = ({
  roomId,
  gameState,
  myUserId,
  isLocalPlayerDead,
  currentDay,
}) => {
  const spectator = useSpectatorMode();
  const t = useT();
  const roomPlayers = useMemo(
    () => toRoomPlayers(gameState, myUserId, t),
    [gameState, myUserId, t],
  );
  // R100 P1 FIX (spectator chat disabled): the chat panel used to render the
  // same disabled-while-connecting state for spectators as for offline players,
  // which the R100 test (Bot 6 caught the API/UI discrepancy) misread as
  // "观战者聊天 disabled". Pass an explicit `isSpectator` flag down so the
  // shared panel can show a spectator-specific placeholder ("👁 观战者可发言,
  // Agents 将看到你的消息") and keep the send button usable as soon as the
  // WS round-trip completes (which R40 P0-2 already guarantees via
  // pendingSend queue + onopen flush).
  return (
    <SharedGameChatPanel
      roomId={roomId}
      roomPlayers={roomPlayers}
      isSpectator={spectator}
      isLocalPlayerDead={!!isLocalPlayerDead}
      currentDay={currentDay}
    />
  );
};
