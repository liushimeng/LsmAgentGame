import { CardView, CardBack } from './CardView';
import { HandPanel } from './HandPanel';
import { STYLE_COLORS, getBoardBg, type StyleKey } from '@/assets/images/doudizhu';
import type { DoudizhuGameState } from '@/types/doudizhu';
import { useState } from 'react';

interface Props {
  gameState: DoudizhuGameState;
  mySeat: number;
  style: StyleKey;
  selectedCards: Set<number>;
  onToggleCard: (index: number) => void;
}

/**
 * DoudizhuTable — 斗地主牌桌主视图。
 *
 * 布局（从自己视角）：
 *   - 顶部居中：底牌（3 张）+ 对家手牌背（左）和（右）
 *   - 底部居中：自己手牌（可点选）
 *   - 中部出牌区：上一手牌（last_play）
 *   - 各座位用小标记显示地主/农民标识
 *
 * 自己永远在底部，左手座在左，右手座在右（3人逆时针）。
 */
export function DoudizhuTable({
  gameState,
  mySeat,
  style,
  selectedCards,
  onToggleCard,
}: Props) {
  const colors = STYLE_COLORS[style];
  const [bgFailed, setBgFailed] = useState(false);
  const bgSrc = getBoardBg(style);

  // 计算左右对手座位（3人逆时针：0→1→2→0）。
  const leftSeat = (mySeat + 1) % 3;
  const rightSeat = (mySeat + 2) % 3;

  const tableStyle: React.CSSProperties = {
    backgroundColor: bgFailed ? colors.boardBg : undefined,
    backgroundImage: !bgFailed && bgSrc ? `url(${bgSrc})` : undefined,
    backgroundSize: 'cover',
  };

  return (
    <div
      className="doudizhu-table"
      style={tableStyle}
      onErrorCapture={() => setBgFailed(true)}
    >
      {/* 底牌区 */}
      <div className="doudizhu-bottom-cards">
        <span className="bottom-label">底牌</span>
        <div className="bottom-row">
          {gameState.bottom && gameState.bottom.length > 0 ? (
            gameState.bottom.map((card, i) => (
              <CardView key={i} card={card} style={style} small />
            ))
          ) : (
            [0, 1, 2].map((i) => (
              <CardBack key={i} style={style} />
            ))
          )}
        </div>
      </div>

      {/* 上家（左手） */}
      <div className="seat-left">
        <div className="seat-label">
          {gameState.seats[leftSeat]?.slice(0, 6) || '...'}
          {gameState.landlord_seat === leftSeat && ' 👑'}
        </div>
        <div className="seat-card-count">
          {gameState.hand_counts[leftSeat]} {gameState.last_play?.seat === leftSeat ? '👆' : ''}
        </div>
        {gameState.last_play?.seat === leftSeat && (
          <div className="last-play-cards">
            {gameState.last_play.cards.map((card, i) => (
              <CardView key={i} card={card} style={style} small />
            ))}
          </div>
        )}
      </div>

      {/* 下家（右手） */}
      <div className="seat-right">
        <div className="seat-label">
          {gameState.seats[rightSeat]?.slice(0, 6) || '...'}
          {gameState.landlord_seat === rightSeat && ' 👑'}
        </div>
        <div className="seat-card-count">
          {gameState.hand_counts[rightSeat]} {gameState.last_play?.seat === rightSeat ? '👆' : ''}
        </div>
        {gameState.last_play?.seat === rightSeat && (
          <div className="last-play-cards">
            {gameState.last_play.cards.map((card, i) => (
              <CardView key={i} card={card} style={style} small />
            ))}
          </div>
        )}
      </div>

      {/* 自己出的最后一手 */}
      {gameState.last_play?.seat === mySeat && (
        <div className="last-play-mine">
          {gameState.last_play.cards.map((card, i) => (
            <CardView key={i} card={card} style={style} small />
          ))}
        </div>
      )}

      {/* 自己手牌（底部，可交互） */}
      <div className="seat-bottom">
        <div className="seat-label">
          {gameState.seats[mySeat]?.slice(0, 6) || '...'}
          {gameState.landlord_seat === mySeat && ' 👑'}
          {' '}(你)
        </div>
        <HandPanel
          hand={gameState.my_hand ?? []}
          selectedIndices={selectedCards}
          style={style}
          onToggleCard={onToggleCard}
        />
      </div>
    </div>
  );
}
