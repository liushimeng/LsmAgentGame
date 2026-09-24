/**
 * AuctionModal — 拍卖弹窗：展示拍卖场次详情（类型 / 当前最高 / 出价记录），
 * 允许活跃拍卖中出价（英式叫价加价 / 密封暗标出价）。
 *
 * 数据源：store.listingBook.auctions（增量帧 game.auction_bid / auction_ended）。
 * 发送：sendTrade({ type: 'auction_bid', auction_id, amount_cny })。
 *
 * 错误展示（§7.1）：弹窗内联红条（formError，弹窗不关闭）。
 */

import { useEffect, useMemo, useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  type VirtualCityAuction,
  type VirtualCityGameState,
  type VirtualCityTradeAction,
} from '@/types/virtualCity';
import { useVirtualCityStore } from '@/store/virtualCity.store';

interface Props {
  open: boolean;
  gameState: VirtualCityGameState | null;
  mySeat: number;
  /** 指定拍卖 id；为空时取第一个活跃拍卖。 */
  auctionId?: string;
  sendTrade: (action: VirtualCityTradeAction) => void;
  onClose: () => void;
}

function seatName(gameState: VirtualCityGameState | null, seat: number): string {
  if (seat < 0) return '—';
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

export function AuctionModal({ open, gameState, mySeat, auctionId, sendTrade, onClose }: Props) {
  const t = useT();
  const auctions = useVirtualCityStore((s) => s.listingBook?.auctions ?? []);
  const [amount, setAmount] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  const auction: VirtualCityAuction | undefined = useMemo(() => {
    if (!auctions.length) return undefined;
    if (auctionId) return auctions.find((a) => a.id === auctionId);
    return auctions.find((a) => a.status === 'active') ?? auctions[0];
  }, [auctions, auctionId]);

  useEffect(() => {
    if (open) {
      setFormError(null);
      setAmount('');
    }
  }, [open]);

  if (!open) return null;

  if (!auction) {
    return (
      <AppModal title={t('virtualCity.auction.title' as TKey)} icon="🔨" maxWidth={460} onClose={onClose}
        footer={<button type="button" className="btn btn-secondary" onClick={onClose}>{t('common.cancel')}</button>}>
        <div className="virtualCity-auction virtualCity-modal__body"><p className="virtualCity-table__empty">{t('virtualCity.listing.empty' as TKey)}</p></div>
      </AppModal>
    );
  }

  const minBid =
    auction.current_cny > 0
      ? auction.current_cny + 10000
      : auction.start_cny;
  const isEnded = auction.status === 'ended';
  const isSeller = auction.seat === mySeat;

  const handleBid = () => {
    const v = Math.floor(Number(amount) || 0);
    if (v < minBid) {
      setFormError(t('virtualCity.auction.tooLow' as TKey) + `（≥ ¥${formatCny(minBid)}）`);
      return;
    }
    setFormError(null);
    sendTrade({ type: 'auction_bid', auction_id: auction.id, amount_cny: v });
    setAmount('');
  };

  return (
    <AppModal
      title={`${t('virtualCity.auction.title' as TKey)} · ${auction.id}`}
      icon="🔨"
      kind={isEnded ? 'info' : 'warning'}
      maxWidth={500}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-secondary" onClick={onClose}>{t('common.cancel')}</button>
          {!isEnded && !isSeller && (
            <button type="button" className="btn btn-primary" onClick={handleBid}>
              {t('virtualCity.auction.confirmBid' as TKey)}
            </button>
          )}
        </>
      }
    >
      {/* 18/04 AB-2：virtualCity-modal__body 标记驱动统一高度/收缩链（见 virtualCity-city3d.css）。 */}
      <div className="virtualCity-auction virtualCity-modal__body">
        <div className="virtualCity-auction__head">
          <span className={`virtualCity-badge virtualCity-badge--auction-${auction.kind}`}>
            {t(`virtualCity.auction.${auction.kind}` as TKey)}
          </span>
          <span className={'virtualCity-badge virtualCity-badge--status-' + auction.status}>
            {t(`virtualCity.auction.status.${auction.status}` as TKey)}
          </span>
          <span className="virtualCity-auction__listid">{t('virtualCity.auction.listId' as TKey)}: {auction.listing_id}</span>
        </div>

        <div className="virtualCity-auction__stats">
          <div className="virtualCity-auction__stat">
            <span className="virtualCity-auction__stat-label">{t('virtualCity.auction.highest' as TKey)}</span>
            <span className="virtualCity-auction__stat-value virtualCity-num--pos">¥{formatCny(auction.current_cny)}</span>
          </div>
          <div className="virtualCity-auction__stat">
            <span className="virtualCity-auction__stat-label">{t('virtualCity.auction.highestSeat' as TKey)}</span>
            <span className="virtualCity-auction__stat-value">{seatName(gameState, auction.current_seat)}</span>
          </div>
          {auction.reserve_cny !== undefined && (
            <div className="virtualCity-auction__stat">
              <span className="virtualCity-auction__stat-label">{t('virtualCity.auction.reserve' as TKey)}</span>
              <span className="virtualCity-auction__stat-value">¥{formatCny(auction.reserve_cny)}</span>
            </div>
          )}
          <div className="virtualCity-auction__stat">
            <span className="virtualCity-auction__stat-label">{t('virtualCity.auction.endMonth' as TKey)}</span>
            <span className="virtualCity-auction__stat-value">第 {auction.end_month} 月</span>
          </div>
        </div>

        {auction.kind === 'sealed' && !isEnded && (
          <p className="virtualCity-auction__sealedhint">🔒 {t('virtualCity.auction.sealedHint' as TKey)}</p>
        )}

        {auction.bids.length > 0 && (
          <ul className="virtualCity-auction__bids">
            {auction.bids.map((b, i) => (
              <li key={i} className={'virtualCity-auction__bid' + (b.seat === mySeat ? ' virtualCity-auction__bid--mine' : '')}>
                <span>{seatName(gameState, b.seat)}</span>
                <span className={b.revealed || auction.status === 'ended' ? 'virtualCity-num--pos' : 'virtualCity-auction__sealed'}>
                  {b.revealed || auction.status === 'ended' ? `¥${formatCny(b.amount_cny)}` : '🔒'}
                </span>
              </li>
            ))}
          </ul>
        )}

        {isEnded && (
          <p className="virtualCity-auction__ended">
            {t('virtualCity.auction.ended' as TKey)}
            {auction.current_seat >= 0 && ` · ${t('virtualCity.auction.winner' as TKey)}: ${seatName(gameState, auction.current_seat)}`}
          </p>
        )}

        {!isEnded && !isSeller && (
          <div className="virtualCity-auction__bidbox">
            <label className="virtualCity-action-form__row">
              <span>{t('virtualCity.auction.bidAmount' as TKey)}（≥ ¥{formatCny(minBid)}）</span>
              <input
                type="number"
                min={minBid}
                step={10000}
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                placeholder={String(minBid)}
              />
            </label>
          </div>
        )}

        {formError && <div className="virtualCity-auction__error" role="alert">{formError}</div>}
      </div>
    </AppModal>
  );
}
