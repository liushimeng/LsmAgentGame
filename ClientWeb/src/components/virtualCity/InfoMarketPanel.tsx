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
  type VirtualCityGameState,
  type VirtualCityInfoCategory,
  type VirtualCityListing,
  type VirtualCityTradeAction,
} from '@/types/virtualCity';
import { useVirtualCityStore } from '@/store/virtualCity.store';

interface Props {
  gameState: VirtualCityGameState | null;
  mySeat: number;
  sendTrade: (action: VirtualCityTradeAction) => void;
  onRefresh: () => void;
}

function seatName(gameState: VirtualCityGameState | null, seat: number): string {
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

const CATEGORIES: VirtualCityInfoCategory[] = ['market', 'intel', 'personal'];

export function InfoMarketPanel({ gameState, mySeat, sendTrade, onRefresh }: Props) {
  const t = useT();
  const listings = useVirtualCityStore((s) => s.listingBook?.listings ?? []);

  const [category, setCategory] = useState<VirtualCityInfoCategory>('market');
  const [infoTitle, setInfoTitle] = useState('');
  const [detail, setDetail] = useState('');
  const [minBid, setMinBid] = useState('500');
  const [bidAmount, setBidAmount] = useState('');

  const infoListings = useMemo(
    () => listings.filter((l) => l.type === 'info'),
    [listings],
  );

  const isMine = (l: VirtualCityListing) => l.seat === mySeat;

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

  const handleBid = (l: VirtualCityListing) => {
    const v = Math.floor(Number(bidAmount) || 0);
    const min = (l.payload.info?.min_bid_cny ?? 0);
    if (v < min) return;
    sendTrade({ type: 'bid_info', listing_id: l.id, bid_cny: v });
    setBidAmount('');
  };

  if (!gameState) {
    return <div className="virtualCity-panel__empty"><p>{t('virtualCity.panel.waiting' as TKey)}</p></div>;
  }

  return (
    <div className="virtualCity-infomarket">
      <div className="virtualCity-infomarket__head">
        <span className="virtualCity-infomarket__title">{t('virtualCity.info.title' as TKey)}</span>
        <button type="button" className="virtualCity-listingpanel__refresh" onClick={onRefresh} title={t('virtualCity.listing.view' as TKey)}>↻</button>
      </div>
      <p className="virtualCity-infomarket__subtitle">{t('virtualCity.info.subtitle' as TKey)}</p>

      <table className="virtualCity-table">
        <thead>
          <tr>
            <th>{t('virtualCity.info.col.id' as TKey)}</th>
            <th>{t('virtualCity.info.col.category' as TKey)}</th>
            <th>{t('virtualCity.info.col.title' as TKey)}</th>
            <th>{t('virtualCity.info.col.seat' as TKey)}</th>
            <th>{t('virtualCity.info.col.minBid' as TKey)}</th>
            <th>{t('virtualCity.info.col.status' as TKey)}</th>
            <th>{t('virtualCity.info.col.actions' as TKey)}</th>
          </tr>
        </thead>
        <tbody>
          {infoListings
            .filter((l) => l.status === 'open' || isMine(l))
            .map((l) => {
              const info = l.payload.info;
              if (!info) return null;
              return (
                <tr key={l.id} className={isMine(l) ? 'virtualCity-table__row--mine' : ''}>
                  <td className="virtualCity-table__id">{l.id}</td>
                  <td>
                    <span className={'virtualCity-badge virtualCity-badge--cat-' + info.category}>
                      {t(`virtualCity.info.category.${info.category}` as TKey)}
                    </span>
                  </td>
                  <td className="virtualCity-table__summary">{info.title}</td>
                  <td>{seatName(gameState, l.seat)}</td>
                  <td>¥{formatCny(info.min_bid_cny)}</td>
                  <td>
                    <span className={'virtualCity-badge virtualCity-badge--status-' + l.status}>
                      {t(`virtualCity.listing.status.${l.status}` as TKey)}
                    </span>
                  </td>
                  <td>
                    {isMine(l) ? (
                      <button type="button" className="btn btn-mini btn-danger"
                        onClick={() => sendTrade({ type: 'listing_cancel', listing_id: l.id })}>
                        {t('virtualCity.listing.cancel' as TKey)}
                      </button>
                    ) : (
                      <div className="virtualCity-table__actions">
                        <input
                          type="number"
                          className="virtualCity-infomarket__bidinput"
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
                          {t('virtualCity.info.confirmBid' as TKey)}
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              );
            })}
          {infoListings.filter((l) => l.status === 'open' || isMine(l)).length === 0 && (
            <tr><td colSpan={7} className="virtualCity-table__empty">{t('virtualCity.info.empty' as TKey)}</td></tr>
          )}
        </tbody>
      </table>

      {/* 发布信息出售表单 */}
      <div className="virtualCity-infomarket__create">
        <div className="virtualCity-infomarket__create-title">{t('virtualCity.info.createTitle' as TKey)}</div>
        <div className="virtualCity-loanpanel__form">
          <label className="virtualCity-action-form__row">
            <span>{t('virtualCity.info.selCategory' as TKey)}</span>
            <select value={category} onChange={(e) => setCategory(e.target.value as VirtualCityInfoCategory)}>
              {CATEGORIES.map((c) => (
                <option key={c} value={c}>{t(`virtualCity.info.category.${c}` as TKey)}</option>
              ))}
            </select>
          </label>
          <label className="virtualCity-action-form__row">
            <span>{t('virtualCity.info.infoTitle' as TKey)}（≤40）</span>
            <input type="text" maxLength={40} value={infoTitle} onChange={(e) => setInfoTitle(e.target.value)}
              placeholder={t('virtualCity.info.infoTitlePlaceholder' as TKey)} />
          </label>
          <label className="virtualCity-action-form__row">
            <span>{t('virtualCity.info.detail' as TKey)}（≤200）</span>
            <textarea className="virtualCity-infomarket__detail" maxLength={200} value={detail} onChange={(e) => setDetail(e.target.value)}
              placeholder={t('virtualCity.info.detailPlaceholder' as TKey)} />
          </label>
          <label className="virtualCity-action-form__row">
            <span>{t('virtualCity.info.minBid' as TKey)}（≥0）</span>
            <input type="number" min={0} step={100} value={minBid} onChange={(e) => setMinBid(e.target.value)} />
          </label>
          <p className="virtualCity-infomarket__hint">🔒 {t('virtualCity.info.sealedHint' as TKey)}</p>
          <button type="button" className="btn btn-primary" onClick={handleSell}>
            {t('virtualCity.info.confirmSell' as TKey)}
          </button>
        </div>
      </div>
    </div>
  );
}
