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
  type VirtualCityGameState,
  type VirtualCityListing,
  type VirtualCityListingType,
  type VirtualCityMyState,
  type VirtualCityTradeAction,
} from '@/types/virtualCity';
import { useVirtualCityStore } from '@/store/virtualCity.store';

type FilterKey = VirtualCityListingType | 'all';

interface Props {
  roomId: string;
  gameState: VirtualCityGameState | null;
  mySeat: number;
  my: VirtualCityMyState | null;
  /** 交易动作发送（来自 useVirtualCity）。 */
  sendTrade: (action: VirtualCityTradeAction) => void;
  /** 挂单簿快照主动拉取。 */
  onRefresh: () => void;
}

const TYPE_FILTERS: { key: FilterKey; i18nKey: TKey }[] = [
  { key: 'all', i18nKey: 'virtualCity.listing.type.all' },
  { key: 'asset', i18nKey: 'virtualCity.listing.type.asset' },
  { key: 'buy', i18nKey: 'virtualCity.listing.type.buy' },
  { key: 'info', i18nKey: 'virtualCity.listing.type.info' },
  { key: 'loan_ofr', i18nKey: 'virtualCity.listing.type.loan_ofr' },
  { key: 'loan_req', i18nKey: 'virtualCity.listing.type.loan_req' },
];

function seatName(gameState: VirtualCityGameState | null, seat: number): string {
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

function statusBadgeKey(s: VirtualCityListing['status']): TKey {
  return `virtualCity.listing.status.${s}` as TKey;
}

/** 挂单摘要文案（资产名 / 信息标题 / 借贷方向+金额）。 */
function listingSummary(l: VirtualCityListing): string {
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
  const listingBook = useVirtualCityStore((s) => s.listingBook);
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

  const isMine = (l: VirtualCityListing) => l.seat === mySeat;
  // 购买/收购/接借贷 操作前提：不是自己的挂单。
  const canAct = (l: VirtualCityListing) => !isMine(l) && l.status === 'open';

  const handleCancel = (l: VirtualCityListing) => {
    sendTrade({ type: 'listing_cancel', listing_id: l.id });
  };

  const handleNegotiate = (l: VirtualCityListing) => {
    // 首次议价：默认出价为要价（资产/信息）或 0（由玩家在弹窗中调整）。
    sendTrade({ type: 'negotiate_start', listing_id: l.id, offer_cny: l.ask_cny });
  };

  if (!gameState) {
    return <div className="virtualCity-panel__empty"><p>{t('virtualCity.panel.waiting' as TKey)}</p></div>;
  }

  return (
    <div className="virtualCity-listingpanel">
      <div className="virtualCity-listingpanel__head">
        <span className="virtualCity-listingpanel__title">{t('virtualCity.listing.title' as TKey)}</span>
        <button
          type="button"
          className="virtualCity-listingpanel__refresh"
          onClick={onRefresh}
          title={t('virtualCity.listing.view' as TKey)}
        >
          ↻
        </button>
      </div>

      <div className="virtualCity-listingpanel__filters">
        {TYPE_FILTERS.map((f) => (
          <button
            key={f.key}
            type="button"
            className={'virtualCity-filter-btn' + (filter === f.key ? ' virtualCity-filter-btn--active' : '')}
            onClick={() => setFilter(f.key)}
          >
            {t(f.i18nKey)}
          </button>
        ))}
        <label className="virtualCity-listingpanel__integrate">
          <input
            type="checkbox"
            checked={integrateMine}
            onChange={(e) => setIntegrateMine(e.target.checked)}
          />
          {t('virtualCity.listing.integrate' as TKey)}
        </label>
      </div>

      <table className="virtualCity-table">
        <thead>
          <tr>
            <th>{t('virtualCity.listing.col.id' as TKey)}</th>
            <th>{t('virtualCity.listing.col.type' as TKey)}</th>
            <th>{t('virtualCity.listing.col.seat' as TKey)}</th>
            <th>{t('virtualCity.listing.col.ask' as TKey)}</th>
            <th>{t('virtualCity.listing.col.status' as TKey)}</th>
            <th>{t('virtualCity.listing.col.actions' as TKey)}</th>
          </tr>
        </thead>
        <tbody>
          {filtered.map((l) => (
            <tr key={l.id} className={isMine(l) ? 'virtualCity-table__row--mine' : ''}>
              <td className="virtualCity-table__id">{l.id}</td>
              <td>
                <span className={`virtualCity-badge virtualCity-badge--type-${l.type}`}>
                  {t(`virtualCity.listing.type.${l.type}` as TKey)}
                </span>
                <span className="virtualCity-table__summary">{listingSummary(l)}</span>
              </td>
              <td>{seatName(gameState, l.seat)}</td>
              <td className="virtualCity-num--pos">¥{formatCny(l.ask_cny)}</td>
              <td>
                <span className={`virtualCity-badge virtualCity-badge--status-${l.status}`}>
                  {t(statusBadgeKey(l.status))}
                </span>
              </td>
              <td>
                <div className="virtualCity-table__actions">
                  {isMine(l) ? (
                    <button
                      type="button"
                      className="btn btn-mini btn-danger"
                      onClick={() => handleCancel(l)}
                    >
                      {t('virtualCity.listing.cancel' as TKey)}
                    </button>
                  ) : (
                    <>
                      {l.type !== 'buy' && canAct(l) && (
                        <button
                          type="button"
                          className="btn btn-mini btn-primary"
                          onClick={() => handleNegotiate(l)}
                        >
                          {t('virtualCity.negotiate.start' as TKey)}
                        </button>
                      )}
                      {l.type === 'buy' && canAct(l) && (
                        <span className="virtualCity-table__hint">
                          {t('virtualCity.listing.col.seat' as TKey)} · {t('virtualCity.negotiate.start' as TKey)}
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
              <td colSpan={6} className="virtualCity-table__empty">{t('virtualCity.listing.empty' as TKey)}</td>
            </tr>
          )}
        </tbody>
      </table>

      {/* 资产库存提示：玩家可挂牌出售自己持有的资产。 */}
      {my && my.assets.filter((a) => a.kind !== 'pension').length > 0 && (
        <div className="virtualCity-listingpanel__hint">
          💡 可挂牌资产：{my.assets.filter((a) => a.kind !== 'pension').map((a) => `${a.name}×${a.units}`).join(' / ')}
        </div>
      )}
      <input type="hidden" data-my-asset-kinds={[...myAssetKinds].join(',')} readOnly />
    </div>
  );
}
