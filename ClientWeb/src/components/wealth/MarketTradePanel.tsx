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
import type { WealthGameState, WealthMyState, WealthTradeAction } from '@/types/wealth';
import { ListingPanel } from './ListingPanel';
import { LoanPanel } from './LoanPanel';
import { InfoMarketPanel } from './InfoMarketPanel';

type SubTab = 'listing' | 'loan' | 'infomarket';

interface Props {
  roomId: string;
  gameState: WealthGameState | null;
  mySeat: number;
  my: WealthMyState | null;
  sendTrade: (action: WealthTradeAction) => void;
}

export function MarketTradePanel({ roomId, gameState, mySeat, my, sendTrade }: Props) {
  const t = useT();
  const [sub, setSub] = useState<SubTab>('listing');

  const subs: Array<{ key: SubTab; label: string }> = [
    { key: 'listing', label: `📋 ${t('wealth.tab.listing' as TKey)}` },
    { key: 'loan', label: `🏦 ${t('wealth.tab.loan' as TKey)}` },
    { key: 'infomarket', label: `🔍 ${t('wealth.tab.infomarket' as TKey)}` },
  ];

  return (
    <div className="wealth-markettrade">
      <div className="wealth-subtabs">
        {subs.map((s) => (
          <button
            key={s.key}
            type="button"
            className={`wealth-subtabs__btn${sub === s.key ? ' wealth-subtabs__btn--active' : ''}`}
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