import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  bids: number[];
  currentBid: number;
  mySeat: number;
  turn: number;
  onBid: (score: number) => void;
}

/**
 * BidPanel — 叫地主面板。
 * 轮到自己时显示不叫/1/2/3 分按钮；否则显示等待提示。
 */
export function BidPanel({ bids, currentBid, mySeat, turn, onBid }: Props) {
  const t = useT();
  const isMyTurn = turn === mySeat;

  return (
    <div className="doudizhu-bid-panel">
      <div className="bid-status">
        {bids.map((b, i) => (
          <span key={i} className="bid-record">
            {i === mySeat ? t('doudizhu.seatYou' as TKey) : `${t('doudizhu.seat' as TKey)}${i}`}
            : {b === -1 ? '...' : b === 0 ? t('doudizhu.bidPass' as TKey) : `${b} ${t('doudizhu.bidScore' as TKey)}`}
          </span>
        ))}
      </div>
      {isMyTurn ? (
        <div className="bid-buttons">
          <button className="btn btn-ghost" onClick={() => onBid(0)}>
            {t('doudizhu.bidPass' as TKey)}
          </button>
          {[1, 2, 3].map((s) => (
            <button
              key={s}
              className="btn btn-primary"
              disabled={s <= currentBid}
              onClick={() => onBid(s)}
            >
              {s} {t('doudizhu.bidScore' as TKey)}
            </button>
          ))}
        </div>
      ) : (
        <div className="bid-waiting">
          {t('doudizhu.bidWaiting' as TKey)}…
        </div>
      )}
    </div>
  );
}
