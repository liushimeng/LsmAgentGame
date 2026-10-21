/**
 * DataSourcesPanel — 批次 69 看板「数据源清单」维度（新增）。
 *
 * 数据源：GET /api/virtual_city/dashboard 的 `data.dashboard.data_sources[]`。
 * 按 `category` 分组渲染，每条展示「名称 / 用途 / 官网链接」。
 *
 * 降级纪律：端点不可达 / 目录为空 ⇒ 展示空态提示（**不崩溃**、不弹错误）。
 */

import { useMemo } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { VirtualCityDashboardDataSource } from '@/api/virtualCity';

export interface DataSourcesPanelProps {
  /** 后端下发的数据源清单（未取到 / 为空 ⇒ 空态）。 */
  sources: VirtualCityDashboardDataSource[];
}

export function DataSourcesPanel({ sources }: DataSourcesPanelProps) {
  const t = useT();

  /** 按 category 分组（保持后端给定顺序：Map 插入序）。 */
  const groups = useMemo(() => {
    const map = new Map<string, VirtualCityDashboardDataSource[]>();
    for (const s of sources) {
      const key = s.category || t('virtualCity.dash.sources.uncategorized' as TKey);
      const arr = map.get(key);
      if (arr) arr.push(s);
      else map.set(key, [s]);
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
      {groups.map(([category, items]) => (
        <div className="virtualCity-dash__src-group" key={category}>
          <div className="virtualCity-dash__src-group-title">{category}</div>
          <ul className="virtualCity-dash__src-list">
            {items.map((s, i) => (
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
      ))}
    </div>
  );
}

export default DataSourcesPanel;
