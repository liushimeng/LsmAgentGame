import { CardView } from './CardView';
import type { DoudizhuCard } from '@/types/doudizhu';
import type { StyleKey } from '@/assets/images/doudizhu';

interface Props {
  hand: DoudizhuCard[];
  selectedIndices: Set<number>;
  style: StyleKey;
  onToggleCard: (index: number) => void;
}

/**
 * HandPanel — 显示自己的完整手牌（按点数从大到小排列）。
 * 点击牌可选中/取消（已选牌上移高亮），用于出牌时挑选。
 */
export function HandPanel({ hand, selectedIndices, style, onToggleCard }: Props) {
  return (
    <div className="doudizhu-hand">
      {hand.map((card, i) => (
        <div
          key={`${card.rank}-${card.suit}-${i}`}
          className={`card-slot ${selectedIndices.has(i) ? 'selected-slot' : ''}`}
          onClick={() => onToggleCard(i)}
        >
          <CardView
            card={card}
            style={style}
            selected={selectedIndices.has(i)}
          />
        </div>
      ))}
    </div>
  );
}
