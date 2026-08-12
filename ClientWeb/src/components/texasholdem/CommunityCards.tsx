import { TexasCardView, TexasCardBack } from './CardView';
import type { TexasCard } from '@/types/texasholdem';
import type { StyleKey } from '@/assets/images/texasholdem';

interface Props {
  cards: TexasCard[];
  communityCount: number;
  style: StyleKey;
}

export function CommunityCards({ cards, communityCount, style }: Props) {
  const slots = 5;
  // 服务端可能在未翻牌时把 community 序列化为 null；兜底为空数组，避免后续访问抛错。
  const safeCards = Array.isArray(cards) ? cards : [];
  return (
    <div className="texas-community">
      {Array.from({ length: slots }, (_, i) => {
        if (i < communityCount && safeCards[i]) {
          return <TexasCardView key={i} card={safeCards[i]} style={style} small />;
        }
        return <TexasCardBack key={i} style={style} small />;
      })}
    </div>
  );
}
