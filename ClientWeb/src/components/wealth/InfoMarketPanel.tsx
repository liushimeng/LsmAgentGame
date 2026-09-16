/**
 * InfoMarketPanel — 信息市场面板：挂牌出售情报 / 内幕 / 玩家概况（密封暗标），
 * 买方暗标出价，成交流后揭示详情。
 *
 * 操作：发布信息出售（选择类别 / 标题 / 详情 / 最低出价）、对挂单暗标出价、
 * 查看已揭示的详情（仅本人已售/已购且成交）。
 *
 * 数据源：store.listingBook.listings（type=info）。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  type WealthGameState,
  type WealthInfoCategory,
  type WealthListing,
  type WealthTradeAction,
} from '@/types/wealth';
import { useWealthStore } from '@/store/wealth.store';

interface Props {
  gameState: WealthGameState | null;
  mySeat: number;
  sendTrade: (action: WealthTradeAction) => void;
  onRefresh: () => void;
}

function seatName(gameState: WealthGameState | null, seat: number): string {
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

const CATEGORIES: WealthInfoCategory[] = ['market', 'intel', 'personal'];

export function InfoMarketPanel({ gameState, mySeat, sendTrade, onRefresh }: Props) {
  const t = useT();
  const listings = useWealthStore((s) => s.listingBook?.listings ?? []);

  const [category, setCategory] = useState<WealthInfoCategory>('market');
  const [infoTitle, setInfoTitle] = useState('');
  const [detail, setDetail] = useState('');
  const [minBid, setMinBid] = useState('500');
  const [bidAmount, setBidAmount] = useState('');

  const infoListings = useMemo(
    () => listings.filter((l) => l.type === 'info'),
    [listings],
  );

  const isMine = (l: WealthListing) => l.seat === mySeat;

  const handleSell = () => {
    const m = Math.floor(Number(minBid) || 0);
    if (!infoTitle.trim()) {
      setInfoTitle('');
      return;
    }
    sendTrade({
      type: 'sell_info',
      category,
      title: infoTitle.trim().slice(0, 40),
      detail: detail.slice(0, 200),
      min_bid_cny: m,
    });
    setInfoTitle('');
    setDetail('');
    setMinBid('500');
  };

  const handleBid = (l: WealthListing) => {
    const v = Math.floor(Number(bidAmount) || 0);
    const min = (l.payload.info?.min_bid_cny ?? 0);
    if (v < min) return;
    sendTrade({ type: 'bid_info', listing_id: l.id, bid_cny: v });
    setBidAmount('');
  };

  if (!gameState) {
    return <div className="wealth-panel__empty"><p>{t('wealth.panel.waiting' as TKey)}</p></div>;
  }

  return (
    <div className="wealth-infomarket">
      <div className="wealth-infomarket__head">
        <span className="wealth-infomarket__title">{t('wealth.info.title' as TKey)}</span>
        <button type="button" className="wealth-listingpanel__refresh" onClick={onRefresh} title={t('wealth.listing.view' as TKey)}>↻</button>
      </div>
      <p className="wealth-infomarket__subtitle">{t('wealth.info.subtitle' as TKey)}</p>

      <table className="wealth-table">
        <thead>
          <tr>
            <th>{t('wealth.info.col.id' as TKey)}</th>
            <th>{t('wealth.info.col.category' as TKey)}</th>
            <th>{t('wealth.info.col.title' as TKey)}</th>
            <th>{t('wealth.info.col.seat' as TKey)}</th>
            <th>{t('wealth.info.col.minBid' as TKey)}</th>
            <th>{t('wealth.info.col.status' as TKey)}</th>
            <th>{t('wealth.info.col.actions' as TKey)}</th>
          </tr>
        </thead>
        <tbody>
          {infoListings
            .filter((l) => l.status === 'open' || isMine(l))
            .map((l) => {
              const info = l.payload.info;
              if (!info) return null;
              return (
                <tr key={l.id} className={isMine(l) ? 'wealth-table__row--mine' : ''}>
                  <td className="wealth-table__id">{l.id}</td>
                  <td>
                    <span className={'wealth-badge wealth-badge--cat-' + info.category}>
                      {t(`wealth.info.category.${info.category}` as TKey)}
                    </span>
                  </td>
                  <td className="wealth-table__summary">{info.title}</td>
                  <td>{seatName(gameState, l.seat)}</td>
                  <td>¥{formatCny(info.min_bid_cny)}</td>
                  <td>
                    <span className={'wealth-badge wealth-badge--status-' + l.status}>
                      {t(`wealth.listing.status.${l.status}` as TKey)}
                    </span>
                  </td>
                  <td>
                    {isMine(l) ? (
                      <button type="button" className="btn btn-mini btn-danger"
                        onClick={() => sendTrade({ type: 'listing_cancel', listing_id: l.id })}>
                        {t('wealth.listing.cancel' as TKey)}
                      </button>
                    ) : (
                      <div className="wealth-table__actions">
                        <input
                          type="number"
                          className="wealth-infomarket__bidinput"
                          min={info.min_bid_cny}
                          step={100}
                          value={bidAmount}
                          onChange={(e) => setBidAmount(e.target.value)}
                          placeholder={String(info.min_bid_cny)}
                        />
                        <button
                          type="button"
                          className="btn btn-mini btn-primary"
                          onClick={() => handleBid(l)}
                        >
                          {t('wealth.info.confirmBid' as TKey)}
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              );
            })}
          {infoListings.filter((l) => l.status === 'open' || isMine(l)).length === 0 && (
            <tr><td colSpan={7} className="wealth-table__empty">{t('wealth.info.empty' as TKey)}</td></tr>
          )}
        </tbody>
      </table>

      {/* 发布信息出售表单 */}
      <div className="wealth-infomarket__create">
        <div className="wealth-infomarket__create-title">{t('wealth.info.createTitle' as TKey)}</div>
        <div className="wealth-loanpanel__form">
          <label className="wealth-action-form__row">
            <span>{t('wealth.info.selCategory' as TKey)}</span>
            <select value={category} onChange={(e) => setCategory(e.target.value as WealthInfoCategory)}>
              {CATEGORIES.map((c) => (
                <option key={c} value={c}>{t(`wealth.info.category.${c}` as TKey)}</option>
              ))}
            </select>
          </label>
          <label className="wealth-action-form__row">
            <span>{t('wealth.info.infoTitle' as TKey)}（≤40）</span>
            <input type="text" maxLength={40} value={infoTitle} onChange={(e) => setInfoTitle(e.target.value)}
              placeholder={t('wealth.info.infoTitlePlaceholder' as TKey)} />
          </label>
          <label className="wealth-action-form__row">
            <span>{t('wealth.info.detail' as TKey)}（≤200）</span>
            <textarea className="wealth-infomarket__detail" maxLength={200} value={detail} onChange={(e) => setDetail(e.target.value)}
              placeholder={t('wealth.info.detailPlaceholder' as TKey)} />
          </label>
          <label className="wealth-action-form__row">
            <span>{t('wealth.info.minBid' as TKey)}（≥0）</span>
            <input type="number" min={0} step={100} value={minBid} onChange={(e) => setMinBid(e.target.value)} />
          </label>
          <p className="wealth-infomarket__hint">🔒 {t('wealth.info.sealedHint' as TKey)}</p>
          <button type="button" className="btn btn-primary" onClick={handleSell}>
            {t('wealth.info.confirmSell' as TKey)}
          </button>
        </div>
      </div>
    </div>
  );
}
