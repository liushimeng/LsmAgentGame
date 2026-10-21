/**
 * BoardOverview — 批次 65 实时看板 · 总览子 Tab。
 *
 * 跨类目核心指数卡（上证/恒指/纳指/黄金/原油/BTC，有数据才显示，
 * 匹配不到对应报价的卡整条不渲染 —— 绝不显示全 0 伪数据）+ 最新头条 3 条。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import type { RealtimeFeed } from '@/types/virtualCityRealtime';
import { pickQuote, pctText, RT_CORE_INDICES, timeAgoKey, timeAgoParts } from './boardUtils';

export function BoardOverview({ feed }: { feed: RealtimeFeed }) {
  const t = useT();
  // 批次 66:窄读取游戏内市场锚(gameState 可能为空/无对局 ⇒ 整卡不渲染)。
  const exoAnchor = useVirtualCityStore((s) => s.gameState?.market?.exo_anchor);
  const cards = RT_CORE_INDICES
    .map((def) => {
      const cat = feed.categories[def.category];
      if (!cat) return null;
      const q = pickQuote(cat.quotes, def.keywords);
      return q ? { key: def.category, quote: q } : null;
    })
    .filter((x): x is NonNullable<typeof x> => x !== null);

  const headlines = feed.news.slice(0, 3);

  return (
    <div className="virtualCity-rt__overview">
      {cards.length > 0 && (
        <div className="virtualCity-rt__cards">
          {cards.map(({ key, quote }) => {
            const up = quote.change_pct > 0;
            const down = quote.change_pct < 0;
            return (
              <div className="virtualCity-rt__card" key={key} title={quote.source}>
                <span className="virtualCity-rt__card-name">{quote.name}</span>
                <span className="virtualCity-rt__card-price">
                  {quote.price.toLocaleString()}
                  {quote.currency ? <small> {quote.currency}</small> : null}
                </span>
                <span
                  className={
                    'virtualCity-rt__chg' +
                    (up ? ' virtualCity-rt__chg--up' : down ? ' virtualCity-rt__chg--down' : '')
                  }
                >
                  {quote.change_pct > 0 ? '+' : ''}{pctText(quote.change_pct, 2)}
                </span>
              </div>
            );
          })}
        </div>
      )}

      {exoAnchor && (
        <div
          className={`virtualCity-rt__exo virtualCity-rt__exo--on`}
          data-testid="virtualCity-rt-exo-card"
        >
          <div className="virtualCity-rt__exo-head">
            <span className="virtualCity-rt__exo-title">{t('virtualCity.rt.exo.effectHint' as TKey)}</span>
            <span
              className={`virtualCity-badge virtualCity-rt__exo-badge ${
                exoAnchor.active
                  ? exoAnchor.sentiment > 0.05
                    ? 'virtualCity-rt__exo-badge--up'
                    : exoAnchor.sentiment < -0.05
                      ? 'virtualCity-rt__exo-badge--down'
                      : ''
                  : 'virtualCity-rt__exo-badge--off'
              }`}
            >
              {exoAnchor.active
                ? `${exoAnchor.sentiment >= 0 ? '+' : ''}${exoAnchor.sentiment.toFixed(2)}`
                : t('virtualCity.rt.exo.inactive' as TKey)}
            </span>
          </div>
          {exoAnchor.active && (
            <div className="virtualCity-rt__exo-row">
              <span className="virtualCity-rt__exo-label">{t('virtualCity.rt.exo.stockBias' as TKey)}</span>
              <span className={exoAnchor.stock_bias >= 0 ? 'virtualCity-rt__chg virtualCity-rt__chg--up' : 'virtualCity-rt__chg virtualCity-rt__chg--down'}>
                {exoAnchor.stock_bias >= 0 ? '+' : ''}{(exoAnchor.stock_bias * 100).toFixed(2)}%
              </span>
              <span className="virtualCity-rt__exo-label">{t('virtualCity.rt.exo.goldBias' as TKey)}</span>
              <span className={exoAnchor.gold_bias >= 0 ? 'virtualCity-rt__chg virtualCity-rt__chg--up' : 'virtualCity-rt__chg virtualCity-rt__chg--down'}>
                {exoAnchor.gold_bias >= 0 ? '+' : ''}{(exoAnchor.gold_bias * 100).toFixed(2)}%
              </span>
            </div>
          )}
        </div>
      )}

      {headlines.length > 0 && (
        <div className="virtualCity-rt__headlines">
          <div className="virtualCity-rt__section-title">
            {t('virtualCity.rt.headline' as TKey)}
          </div>
          {headlines.map((n) => {
            const ago = timeAgoParts(n.published_at);
            return (
              <a
                key={n.id}
                className="virtualCity-rt__headline"
                href={n.url}
                target="_blank"
                rel="noreferrer"
              >
                <span className="virtualCity-rt__headline-title">{n.title}</span>
                <span className="virtualCity-rt__headline-meta">
                  {n.source} · {t(timeAgoKey(ago) as TKey, { n: ago.n })}
                </span>
              </a>
            );
          })}
        </div>
      )}

      {cards.length === 0 && headlines.length === 0 && (
        <div className="virtualCity-panel__empty virtualCity-rt__empty">
          <span className="virtualCity-rt__empty-icon" aria-hidden="true">📊</span>
          <p>{t('virtualCity.rt.empty.feed' as TKey)}</p>
        </div>
      )}
    </div>
  );
}

export default BoardOverview;
