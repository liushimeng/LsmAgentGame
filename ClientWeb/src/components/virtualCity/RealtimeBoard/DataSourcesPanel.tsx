/**
 * DataSourcesPanel — 批次 69 看板「数据源清单」维度（批次 78 增分组实时状态条）。
 *
 * 数据源：GET /api/virtual_city/dashboard 的 `data.dashboard.data_sources[]`。
 * 按 `category` 分组渲染，每条展示「名称 / 用途 / 官网链接」。
 *
 * 批次 78：每个分组标题下方追加一条**实时状态条**（.virtualCity-dash__src-live__*），
 * 展示该登记册分组所映射的实时行情类目（GROUP_CATEGORY 静态映射）的
 * 「报价条数 / 采集来源 / 更新于」——数据取自 store.realtimeFeed（快照 + WS 帧）。
 * 无对应实时类目的分组（综合财经 / 数据接口）显示「静态登记册」占位文案。
 *
 * 降级纪律：端点不可达 / 目录为空 ⇒ 展示空态提示（**不崩溃**、不弹错误）。
 */

import { useMemo } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { VirtualCityDashboardDataSource } from '@/api/virtualCity';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import type { RealtimeFeed } from '@/types/virtualCityRealtime';
import { timeAgoKey, timeAgoParts } from './boardUtils';

/**
 * 登记册分组（后端 `dashboardDataSources` 的 Category 字面量）→ 实时行情类目。
 *
 * 口径：**静态登记册**（45 条宽清单，批次76）与**可采集实时类目**（窄，7+1）
 * 是两套东西；综合财经 / 数据接口 两组只是「可查阅的站点目录」，没有对应的
 * 实时行情通道 ⇒ 显式不映射（显示静态登记册占位，不臆造状态）。
 */
const GROUP_CATEGORY: Record<string, string> = {
  'A股市场': 'a_share',
  '港股市场': 'hk',
  '美股市场': 'us',
  '贵金属大宗': 'metal',
  '期货市场': 'futures',
  '加密货币': 'crypto',
  '宏观数据': 'macro',
  '时政军事': 'news',
};

/** 单个实时类目的可展示摘要。 */
interface LiveInfo {
  /** 报价（新闻类目为条数）条数。 */
  count: number;
  /** 采集来源站点名。 */
  source: string;
  /** Unix 秒（0 = 未知）。 */
  updatedAt: number;
}

/**
 * 取实时类目摘要；类目无数据 / feed 未就绪 ⇒ null（渲染占位文案）。
 *
 * `news` 特殊：新闻不在 feed.categories 里，而在 feed.news（扁平列表），
 * 取最新一条的 source / published_at 作「采集来源 / 更新于」。
 */
function liveInfoFor(feed: RealtimeFeed | null, category: string): LiveInfo | null {
  if (!feed) return null;
  if (category === 'news') {
    const newest = feed.news?.[0];
    if (!newest) return null;
    return {
      count: feed.news.length,
      source: newest.source || '',
      updatedAt: newest.published_at || 0,
    };
  }
  const cat = feed.categories?.[category];
  if (!cat || !cat.quotes || !cat.quotes.length) return null;
  return {
    count: cat.quotes.length,
    source: cat.source || '',
    updatedAt: cat.updated_at || 0,
  };
}

export interface DataSourcesPanelProps {
  /** 后端下发的数据源清单（未取到 / 为空 ⇒ 空态）。 */
  sources: VirtualCityDashboardDataSource[];
}

export function DataSourcesPanel({ sources }: DataSourcesPanelProps) {
  const t = useT();
  const feed = useVirtualCityStore((s) => s.realtimeFeed);

  /** 按 category 分组（保持后端给定顺序：Map 插入序）。 */
  const groups = useMemo(() => {
    const map = new Map<string, { raw: string; items: VirtualCityDashboardDataSource[] }>();
    for (const s of sources) {
      const key = s.category || t('virtualCity.dash.sources.uncategorized' as TKey);
      const arr = map.get(key);
      if (arr) arr.items.push(s);
      else map.set(key, { raw: s.category || '', items: [s] });
    }
    return [...map.entries()];
  }, [sources, t]);

  if (!groups.length) {
    return (
      <div className="virtualCity-dash__sources" data-testid="virtualCity-dash-sources">
        <div className="virtualCity-dash__src-group-title">
          {t('virtualCity.dash.sources.title' as TKey)}
        </div>
        <div className="virtualCity-panel__empty">
          <p>{t('virtualCity.dash.sources.empty' as TKey)}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="virtualCity-dash__sources" data-testid="virtualCity-dash-sources">
      <div className="virtualCity-dash__src-group-title">
        {t('virtualCity.dash.sources.title' as TKey)}
      </div>
      {groups.map(([category, group]) => {
        const catKey = GROUP_CATEGORY[group.raw];
        const live = catKey ? liveInfoFor(feed, catKey) : null;
        const ago = live && live.updatedAt ? timeAgoParts(live.updatedAt) : null;
        return (
          <div className="virtualCity-dash__src-group" key={category}>
            <div className="virtualCity-dash__src-group-title">{category}</div>
            {/* 批次 78 · 分组实时状态条（无映射类目 ⇒ 静态登记册占位）。 */}
            <div
              className="virtualCity-dash__src-live"
              data-testid={`virtualCity-dash-src-live-${catKey ?? 'static'}`}
            >
              {!catKey ? (
                <span className="virtualCity-dash__src-live__placeholder">
                  {t('virtualCity.dash.sources.live.static' as TKey)}
                </span>
              ) : !live ? (
                <span className="virtualCity-dash__src-live__placeholder">
                  {t(
                    (feed
                      ? 'virtualCity.dash.sources.live.noData'
                      : 'virtualCity.dash.sources.live.offline') as TKey,
                  )}
                </span>
              ) : (
                <>
                  <span className="virtualCity-dash__src-live__label">
                    {t('virtualCity.dash.sources.live.title' as TKey)}
                  </span>
                  <span className="virtualCity-dash__src-live__meta">
                    {t('virtualCity.dash.sources.live.count' as TKey, { n: live.count })}
                  </span>
                  {live.source && (
                    <span className="virtualCity-dash__src-live__meta">
                      {t('virtualCity.dash.sources.live.source' as TKey, {
                        source: live.source,
                      })}
                    </span>
                  )}
                  {ago && (
                    <span className="virtualCity-dash__src-live__meta">
                      {t(timeAgoKey(ago) as TKey, { n: ago.n })}
                    </span>
                  )}
                </>
              )}
            </div>
            <ul className="virtualCity-dash__src-list">
              {group.items.map((s, i) => (
                <li className="virtualCity-dash__src-item" key={`${s.name}-${i}`}>
                  <div className="virtualCity-dash__src-head">
                    <span className="virtualCity-dash__src-name">{s.name}</span>
                    {s.url ? (
                      <a
                        className="virtualCity-dash__src-link"
                        href={s.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        title={t('virtualCity.dash.sources.openLink' as TKey)}
                        aria-label={`${s.name} — ${t('virtualCity.dash.sources.openLink' as TKey)}`}
                      >
                        {t('virtualCity.dash.sources.openLink' as TKey)} ↗
                      </a>
                    ) : null}
                  </div>
                  {s.purpose ? (
                    <div className="virtualCity-dash__src-purpose">{s.purpose}</div>
                  ) : null}
                </li>
              ))}
            </ul>
          </div>
        );
      })}
    </div>
  );
}

export default DataSourcesPanel;
