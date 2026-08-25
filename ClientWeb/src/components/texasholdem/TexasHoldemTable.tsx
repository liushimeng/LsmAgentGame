import { CommunityCards } from './CommunityCards';
import { PlayerSeat } from './PlayerSeat';
import { BotThoughtPanel } from './BotThoughtPanel';
import { STYLE_COLORS, getBoardBg, type StyleKey } from '@/assets/images/texasholdem';
import type { TexasHoldemGameState } from '@/types/texasholdem';
import { useEffect, useState } from 'react';
import { useSeatChatBubbles, ingestServerBubbles } from './useSeatChatBubbles';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  gameState: TexasHoldemGameState;
  mySeat: number;
  style: StyleKey;
}

/**
 * TexasHoldemTable — 德州扑克牌桌。
 * 6 个座位按椭圆排列，自己在底部，其余顺时针。
 */
export function TexasHoldemTable({ gameState, mySeat, style }: Props) {
  const colors = STYLE_COLORS[style];
  const [bgFailed, setBgFailed] = useState(false);
  const bgSrc = getBoardBg(style);
  const t = useT();
  // 2026-08-23 §德扑Agent聊天 — 座位级 bot 发言气泡(chat.message 实时通道,≤8s)。
  const chatBubbles = useSeatChatBubbles(gameState.room_id);
  // §20260825-03 — 快照通道:game.state 的 bot_last_chat 覆盖重连/刷新恢复,
  // 与实时通道按 (seat, ts) 去重。
  const lastChat = gameState.bot_last_chat;
  useEffect(() => {
    if (gameState.room_id && lastChat) {
      ingestServerBubbles(gameState.room_id, lastChat);
    }
  }, [gameState.room_id, lastChat]);

  const tableStyle: React.CSSProperties = {
    backgroundColor: bgFailed ? colors.boardBg : undefined,
    backgroundImage: !bgFailed && bgSrc ? `url(${bgSrc})` : undefined,
    backgroundSize: 'cover',
  };

  // 6 个座位：自己在 s0，其余顺时针。
  // 2026-08-20 §德州扑克观战崩溃 — 观战者 mySeat=-1(view.go:97 约定的哨兵值)，
  // 而 JS 的 % 保留符号：(-1 + 0) % 6 === -1，seatOrder 首项为 -1 →
  // players[-1] 为 undefined → PlayerSeat 解引用 has_player 抛错 → 整页错误边界。
  // 修复：观战者没有「自己」，不旋转，按 0..5 自然座序渲染；玩家仍以自己为 s0。
  const baseSeat = mySeat >= 0 && mySeat < 6 ? mySeat : 0;
  const seatOrder = Array.from({ length: 6 }, (_, i) => (baseSeat + i) % 6);

  // 2026-08-20 §F1 — BotThoughtPanel 独白选座：优先「正在思考且有独白」的 bot 座位，
  // 否则取任一最近非空独白的 bot 座位；全空则不渲染(§126/§130「组件存在但未被 import
  // 等于不存在」修复 —— 面板此前零引用)。
  const botsArr = gameState.bot_seats;
  const thoughtsArr = gameState.bot_heart_thought;
  const thinkingArr = gameState.bot_thinking;
  let thoughtSeat = -1;
  if (botsArr && thoughtsArr) {
    for (let s = 0; s < 6; s++) {
      if (botsArr[s] && thinkingArr?.[s] && thoughtsArr[s]) {
        thoughtSeat = s;
        break;
      }
    }
    if (thoughtSeat < 0) {
      for (let s = 0; s < 6; s++) {
        if (botsArr[s] && thoughtsArr[s]) {
          thoughtSeat = s;
          break;
        }
      }
    }
  }

  const posClasses = ['s0-bottom', 's1-left', 's2-top-left', 's3-top', 's4-top-right', 's5-right'];

  return (
    <div
      className="texas-table"
      style={tableStyle}
      onErrorCapture={() => setBgFailed(true)}
    >
      {seatOrder.map((seat, idx) => {
        // 2026-08-19 §德州扑克Agent — 从 gameState 透传 Bot 字段(后端永远初始化长度 6 数组)
        const botSeats = gameState.bot_seats;
        const botModels = gameState.bot_models;
        const botHeartThought = gameState.bot_heart_thought;
        const botThinking = gameState.bot_thinking;
        const isBot = !!botSeats?.[seat];
        return (
          <div key={seat} className={`seat-pos ${posClasses[idx]}`}>
            <PlayerSeat
              player={gameState.players[seat]}
              style={style}
              isMe={seat === mySeat}
              isTurn={seat === gameState.turn}
              isButton={seat === gameState.button}
              // 2026-08-20 §德州扑克Web端产品界面优化 — SB/BB 座位从 gameState 读取
              // (view.go BuildClientStateWithRoom 从 Button 顺时针推导)。
              isSB={seat === gameState.sb_seat}
              isBB={seat === gameState.bb_seat}
              isBot={isBot}
              botModelName={isBot ? botModels?.[seat] : undefined}
              botThinking={isBot ? !!botThinking?.[seat] : false}
              botHeartThought={isBot ? botHeartThought?.[seat] : undefined}
              botChatBubble={isBot ? chatBubbles[seat]?.text : undefined}
            />
          </div>
        );
      })}

      {/* 公共牌 + 底池 */}
      <div className="texas-center">
        <CommunityCards
          cards={gameState.community}
          communityCount={gameState.community_count}
          style={style}
        />
        <div className="texas-pot">
          {t('texasholdem.pot' as TKey)}: ${gameState.pot}
        </div>
      </div>

      {/* §F1: Bot 内心独白面板 — 桌面右上角(设计文档 §4) */}
      {thoughtSeat >= 0 && (
        <BotThoughtPanel
          thought={thoughtsArr![thoughtSeat]}
          seat={thoughtSeat}
          thinking={!!thinkingArr?.[thoughtSeat]}
          chatPreview={gameState.chat_window_preview}
        />
      )}
    </div>
  );
}
