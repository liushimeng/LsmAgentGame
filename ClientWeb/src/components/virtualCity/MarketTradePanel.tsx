/**
 * MarketTradePanel — 市场聚合面板（15-3D城市全面真实感深化 · 阶段 Q）：
 *
 * 把 listing / loan / infomarket 三个子面板聚合到一个 Tab 下，
 * 顶部 3 个子导航按钮切换；不重写逻辑，仅条件渲染对应子组件。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/04-UI布局 §3.4。
 */

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { VirtualCityGameState, VirtualCityMyState, VirtualCityTradeAction } from '@/types/virtualCity';
import { ListingPanel } from './ListingPanel';
import { LoanPanel } from './LoanPanel';
import { InfoMarketPanel } from './InfoMarketPanel';

type SubTab = 'listing' | 'loan' | 'infomarket';

interface Props {
  roomId: string;
  gameState: VirtualCityGameState | null;
  mySeat: number;
  my: VirtualCityMyState | null;
  sendTrade: (action: VirtualCityTradeAction) => void;
}

export function MarketTradePanel({ roomId, gameState, mySeat, my, sendTrade }: Props) {
  const t = useT();
  const [sub, setSub] = useState<SubTab>('listing');

  const subs: Array<{ key: SubTab; label: string }> = [
    { key: 'listing', label: `📋 ${t('virtualCity.tab.listing' as TKey)}` },
    { key: 'loan', label: `🏦 ${t('virtualCity.tab.loan' as TKey)}` },
    { key: 'infomarket', label: `🔍 ${t('virtualCity.tab.infomarket' as TKey)}` },
  ];

  return (
    <div className="virtualCity-markettrade">
      <div className="virtualCity-subtabs">
        {subs.map((s) => (
          <button
            key={s.key}
            type="button"
            className={`virtualCity-subtabs__btn${sub === s.key ? ' virtualCity-subtabs__btn--active' : ''}`}
            onClick={() => setSub(s.key)}
          >
            {s.label}
          </button>
        ))}
      </div>
      {sub === 'listing' && (
        <ListingPanel
          roomId={roomId}
          gameState={gameState}
          mySeat={mySeat}
          my={my}
          sendTrade={sendTrade}
          onRefresh={() => sendTrade({ type: 'listing_view' })}
        />
      )}
      {sub === 'loan' && (
        <LoanPanel
          gameState={gameState}
          mySeat={mySeat}
          my={my}
          sendTrade={sendTrade}
          onRefresh={() => sendTrade({ type: 'listing_view' })}
        />
      )}
      {sub === 'infomarket' && (
        <InfoMarketPanel
          gameState={gameState}
          mySeat={mySeat}
          sendTrade={sendTrade}
          onRefresh={() => sendTrade({ type: 'listing_view' })}
        />
      )}
    </div>
  );
}

export default MarketTradePanel;