/**
 * WealthPyramidPanel — 财富金字塔（生存/积累/自由圈三层柱状）
 * 2026-09-19 §P2 v2 §13.3
 *
 * 契约: lag_docs/虚拟城市/已实现/07-P2财富可视化/.../§13.3。
 * 数据源: game.state.society.pyramid_layers(v2 新增)。
 * 复用 EconomyPanel 的 circles 视觉(同生存/积累/自由配色)。
 *
 * - 数字徽章 ≥5:1(CLAUDE.md §26)。
 * - 三层按"自下而上"展示(底层 = 生存圈,顶层 = 自由圈);wealth_pct 控制条宽。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, formatPct } from '@/types/wealth';
import type { WealthPyramidLayer } from '@/types/wealth';

interface Props {
  layers: WealthPyramidLayer[] | null | undefined;
}

const LABEL_KEYS = {
  survival: 'wealth.dashboard.pyramid.survival',
  accumulation: 'wealth.dashboard.pyramid.accumulation',
  freedom: 'wealth.dashboard.pyramid.freedom',
} as const;

const DOT_CLASS = {
  survival: 'survival',
  accumulation: 'accumulation',
  freedom: 'freedom',
} as const;

export function WealthPyramidPanel({ layers }: Props) {
  const t = useT();

  if (!layers || layers.length !== 3) {
    return null; // 空态兜底由 EconomyPanel 的 circles 承担;此处不重复渲染
  }

  // 求总人数用于比例尺
  const totalCount = layers.reduce((s, l) => s + l.count, 0);
  // 求 max wealth_pct 用于宽度归一
  const maxPct = Math.max(0.01, ...layers.map((l) => l.wealth_pct || 0));

  return (
    <div className="wealth-pyramid">
      <div className="wealth-pyramid__title">
        {t('wealth.dashboard.pyramid.title' as TKey)}
      </div>
      <div className="wealth-pyramid__layers">
        {/* 自下而上:survival → accumulation → freedom */}
        {[...layers].reverse().map((layer) => {
          const label = t(LABEL_KEYS[layer.name as keyof typeof LABEL_KEYS] as TKey);
          const dotClass = DOT_CLASS[layer.name as keyof typeof DOT_CLASS];
          const barWidth = Math.min(100, (layer.wealth_pct / maxPct) * 100);
          const pctText = formatPct(layer.wealth_pct, 1);
          return (
            <div className="wealth-pyramid__row" key={layer.name}>
              <div className="wealth-pyramid__row-label">
                <i className={`wealth-pyramid__row-dot wealth-pyramid__row-dot--${dotClass}`} />
                <span>{label}</span>
              </div>
              <div className="wealth-pyramid__row-bar">
                <div
                  className={`wealth-pyramid__row-bar-fill wealth-pyramid__row-bar-fill--${dotClass}`}
                  style={{ width: `${barWidth}%` }}
                  aria-label={`${label} ${pctText}`}
                />
              </div>
              <div className="wealth-pyramid__row-meta">
                <div>{t('wealth.dashboard.pyramid.peopleCount' as TKey, { n: layer.count })}</div>
                <div style={{ fontSize: '11px' }}>
                  {t('wealth.dashboard.pyramid.wealthPct' as TKey, { pct: pctText })}
                </div>
                <div style={{ fontSize: '10.5px', color: 'var(--wealth-text-dim)' }}>
                  ¥{formatCny(layer.avg_wealth)} / 人均
                </div>
              </div>
            </div>
          );
        })}
      </div>
      {totalCount === 0 && (
        <div className="wealth-pyramid__empty">—</div>
      )}
    </div>
  );
}