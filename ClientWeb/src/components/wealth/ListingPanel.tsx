/**
 * ListingPanel — 挂单簿面板：列出房间级挂单（出售/收购/信息/借贷），支持按
 * 类型过滤、发布新挂单（资产出售/收购/信息/借贷）、取消自己的挂单、
 * 对挂单发起议价。
 *
 * 数据源：store.listingBook.listings（game.state.listing_book 快照 +
 * 增量帧 game.listing_created/listing_cancelled 合并）。
 * 发送：sendTrade({ type: 'listing_create' | 'listing_cancel' | 'negotiate_start' })。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  type WealthGameState,
  type WealthListing,
  type WealthListingType,
  type WealthMyState,
  type WealthTradeAction,
} from '@/types/wealth';
import { useWealthStore } from '@/store/wealth.store';

type FilterKey = WealthListingType | 'all';

interface Props {
  roomId: string;
  gameState: WealthGameState | null;
  mySeat: number;
  my: WealthMyState | null;
  /** 交易动作发送（来自 useWealth）。 */
  sendTrade: (action: WealthTradeAction) => void;
  /** 挂单簿快照主动拉取。 */
  onRefresh: () => void;
}

const TYPE_FILTERS: { key: FilterKey; i18nKey: TKey }[] = [
  { key: 'all', i18nKey: 'wealth.listing.type.all' },
  { key: 'asset', i18nKey: 'wealth.listing.type.asset' },
  { key: 'buy', i18nKey: 'wealth.listing.type.buy' },
  { key: 'info', i18nKey: 'wealth.listing.type.info' },
  { key: 'loan_ofr', i18nKey: 'wealth.listing.type.loan_ofr' },
  { key: 'loan_req', i18nKey: 'wealth.listing.type.loan_req' },
];

function seatName(gameState: WealthGameState | null, seat: number): string {
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

function statusBadgeKey(s: WealthListing['status']): TKey {
  return `wealth.listing.status.${s}` as TKey;
}

/** 挂单摘要文案（资产名 / 信息标题 / 借贷方向+金额）。 */
function listingSummary(l: WealthListing): string {
  const pl = l.payload;
  if (pl.asset) return `${pl.asset.name} ×${pl.asset.units}`;
  if (pl.info) return pl.info.title;
  if (pl.loan) {
    const dir = pl.loan.direction === 'lend' ? '出借' : '借入';
    return `${dir} ¥${formatCny(pl.loan.principal_cny)}`;
  }
  return l.id;
}

export function ListingPanel({ gameState, mySeat, my, sendTrade, onRefresh }: Props) {
  const t = useT();
  const listingBook = useWealthStore((s) => s.listingBook);
  const listings = listingBook?.listings ?? [];

  const [filter, setFilter] = useState<FilterKey>('all');
  const [integrateMine, setIntegrateMine] = useState(true);

  const myAssetKinds = new Set((my?.assets ?? []).map((a) => a.kind));

  const filtered = useMemo(() => {
    return listings
      .filter((l) => integrateMine || l.seat !== mySeat)
      .filter((l) => filter === 'all' || l.type === filter)
      .filter((l) => l.status === 'open' || l.status === 'negotiating')
      .sort((a, b) => b.create_month - a.create_month);
  }, [listings, filter, integrateMine, mySeat]);

  const isMine = (l: WealthListing) => l.seat === mySeat;
  // 购买/收购/接借贷 操作前提：不是自己的挂单。
  const canAct = (l: WealthListing) => !isMine(l) && l.status === 'open';

  const handleCancel = (l: WealthListing) => {
    sendTrade({ type: 'listing_cancel', listing_id: l.id });
  };

  const handleNegotiate = (l: WealthListing) => {
    // 首次议价：默认出价为要价（资产/信息）或 0（由玩家在弹窗中调整）。
    sendTrade({ type: 'negotiate_start', listing_id: l.id, offer_cny: l.ask_cny });
  };

  if (!gameState) {
    return <div className="wealth-panel__empty"><p>{t('wealth.panel.waiting' as TKey)}</p></div>;
  }

  return (
    <div className="wealth-listingpanel">
      <div className="wealth-listingpanel__head">
        <span className="wealth-listingpanel__title">{t('wealth.listing.title' as TKey)}</span>
        <button
          type="button"
          className="wealth-listingpanel__refresh"
          onClick={onRefresh}
          title={t('wealth.listing.view' as TKey)}
        >
          ↻
        </button>
      </div>

      <div className="wealth-listingpanel__filters">
        {TYPE_FILTERS.map((f) => (
          <button
            key={f.key}
            type="button"
            className={'wealth-filter-btn' + (filter === f.key ? ' wealth-filter-btn--active' : '')}
            onClick={() => setFilter(f.key)}
          >
            {t(f.i18nKey)}
          </button>
        ))}
        <label className="wealth-listingpanel__integrate">
          <input
            type="checkbox"
            checked={integrateMine}
            onChange={(e) => setIntegrateMine(e.target.checked)}
          />
          {t('wealth.listing.integrate' as TKey)}
        </label>
      </div>

      <table className="wealth-table">
        <thead>
          <tr>
            <th>{t('wealth.listing.col.id' as TKey)}</th>
            <th>{t('wealth.listing.col.type' as TKey)}</th>
            <th>{t('wealth.listing.col.seat' as TKey)}</th>
            <th>{t('wealth.listing.col.ask' as TKey)}</th>
            <th>{t('wealth.listing.col.status' as TKey)}</th>
            <th>{t('wealth.listing.col.actions' as TKey)}</th>
          </tr>
        </thead>
        <tbody>
          {filtered.map((l) => (
            <tr key={l.id} className={isMine(l) ? 'wealth-table__row--mine' : ''}>
              <td className="wealth-table__id">{l.id}</td>
              <td>
                <span className={`wealth-badge wealth-badge--type-${l.type}`}>
                  {t(`wealth.listing.type.${l.type}` as TKey)}
                </span>
                <span className="wealth-table__summary">{listingSummary(l)}</span>
              </td>
              <td>{seatName(gameState, l.seat)}</td>
              <td className="wealth-num--pos">¥{formatCny(l.ask_cny)}</td>
              <td>
                <span className={`wealth-badge wealth-badge--status-${l.status}`}>
                  {t(statusBadgeKey(l.status))}
                </span>
              </td>
              <td>
                <div className="wealth-table__actions">
                  {isMine(l) ? (
                    <button
                      type="button"
                      className="btn btn-mini btn-danger"
                      onClick={() => handleCancel(l)}
                    >
                      {t('wealth.listing.cancel' as TKey)}
                    </button>
                  ) : (
                    <>
                      {l.type !== 'buy' && canAct(l) && (
                        <button
                          type="button"
                          className="btn btn-mini btn-primary"
                          onClick={() => handleNegotiate(l)}
                        >
                          {t('wealth.negotiate.start' as TKey)}
                        </button>
                      )}
                      {l.type === 'buy' && canAct(l) && (
                        <span className="wealth-table__hint">
                          {t('wealth.listing.col.seat' as TKey)} · {t('wealth.negotiate.start' as TKey)}
                        </span>
                      )}
                    </>
                  )}
                </div>
              </td>
            </tr>
          ))}
          {filtered.length === 0 && (
            <tr>
              <td colSpan={6} className="wealth-table__empty">{t('wealth.listing.empty' as TKey)}</td>
            </tr>
          )}
        </tbody>
      </table>

      {/* 资产库存提示：玩家可挂牌出售自己持有的资产。 */}
      {my && my.assets.filter((a) => a.kind !== 'pension').length > 0 && (
        <div className="wealth-listingpanel__hint">
          💡 可挂牌资产：{my.assets.filter((a) => a.kind !== 'pension').map((a) => `${a.name}×${a.units}`).join(' / ')}
        </div>
      )}
      <input type="hidden" data-my-asset-kinds={[...myAssetKinds].join(',')} readOnly />
    </div>
  );
}
