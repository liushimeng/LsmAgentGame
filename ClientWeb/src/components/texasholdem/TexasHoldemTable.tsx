import { CommunityCards } from './CommunityCards';
import { PlayerSeat } from './PlayerSeat';
import { STYLE_COLORS, getBoardBg, type StyleKey } from '@/assets/images/texasholdem';
import type { TexasHoldemGameState } from '@/types/texasholdem';
import { useState } from 'react';
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

  const tableStyle: React.CSSProperties = {
    backgroundColor: bgFailed ? colors.boardBg : undefined,
    backgroundImage: !bgFailed && bgSrc ? `url(${bgSrc})` : undefined,
    backgroundSize: 'cover',
  };

  // 6 个座位：自己在 s0，其余顺时针
  const seatOrder = Array.from({ length: 6 }, (_, i) => (mySeat + i) % 6);

  const posClasses = ['s0-bottom', 's1-left', 's2-top-left', 's3-top', 's4-top-right', 's5-right'];

  return (
    <div
      className="texas-table"
      style={tableStyle}
      onErrorCapture={() => setBgFailed(true)}
    >
      {seatOrder.map((seat, idx) => (
        <div key={seat} className={`seat-pos ${posClasses[idx]}`}>
          <PlayerSeat
            player={gameState.players[seat]}
            style={style}
            isMe={seat === mySeat}
            isTurn={seat === gameState.turn}
            isButton={seat === gameState.button}
            isSB={false}
            isBB={false}
          />
        </div>
      ))}

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
    </div>
  );
}
