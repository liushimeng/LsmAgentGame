/**
 * NewsList — 批次 65 实时看板新闻列表。
 *
 * 行 = 标题外链（target=_blank rel=noreferrer）/ 来源 · N 分钟前。
 * 空态沿用 BehaviorPanel 范式（图标 + 说明占位块）。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { RealtimeNews } from '@/types/virtualCityRealtime';
import { timeAgoKey, timeAgoParts } from './boardUtils';

export function NewsList({ news }: { news: RealtimeNews[] }) {
  const t = useT();
  if (!news.length) {
    return (
      <div className="virtualCity-panel__empty virtualCity-rt__empty">
        <span className="virtualCity-rt__empty-icon" aria-hidden="true">📰</span>
        <p>{t('virtualCity.rt.empty.news' as TKey)}</p>
      </div>
    );
  }
  return (
    <div className="virtualCity-rt__news">
      {news.map((n) => {
        const ago = timeAgoParts(n.published_at);
        return (
          <div className="virtualCity-rt__news-item" key={n.id}>
            <a
              className="virtualCity-rt__news-link"
              href={n.url}
              target="_blank"
              rel="noreferrer"
              title={n.url}
            >
              {n.title}
            </a>
            <div className="virtualCity-rt__news-meta">
              <span>{n.source}</span>
              <span aria-hidden="true"> · </span>
              <span>{t(timeAgoKey(ago) as TKey, { n: ago.n })}</span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

export default NewsList;
