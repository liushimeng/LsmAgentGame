/**
 * VirtualCityPyramidPanel — 财富金字塔（生存/积累/自由圈三层柱状）
 * 2026-09-19 §P2 v2 §13.3
 *
 * 契约: lag_docs/虚拟城市/已实现/07-P2财富可视化/.../§13.3。
 * 数据源: game.state.society.pyramid_layers(v2 新增)。
 * 复用 EconomyPanel 的 circles 视觉(同生存/积累/自由配色)。
 *
 * - 数字徽章 ≥5:1(CLAUDE.md §26)。
 * - 三层按"自下而上"展示(底层 = 生存圈,顶层 = 自由圈);wealth_pct 控制条宽
 *   （批次70：后端 tag 是 wealth_pct，前端原写 virtualCity_pct ⇒ 条宽恒 0）。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, formatPct } from '@/types/virtualCity';
import type { VirtualCityPyramidLayer } from '@/types/virtualCity';

interface Props {
  layers: VirtualCityPyramidLayer[] | null | undefined;
}

const LABEL_KEYS = {
  survival: 'virtualCity.dashboard.pyramid.survival',
  accumulation: 'virtualCity.dashboard.pyramid.accumulation',
  freedom: 'virtualCity.dashboard.pyramid.freedom',
} as const;

const DOT_CLASS = {
  survival: 'survival',
  accumulation: 'accumulation',
  freedom: 'freedom',
} as const;

export function VirtualCityPyramidPanel({ layers }: Props) {
  const t = useT();

  if (!layers || layers.length !== 3) {
    return null; // 空态兜底由 EconomyPanel 的 circles 承担;此处不重复渲染
  }

  // 求总人数用于比例尺
  const totalCount = layers.reduce((s, l) => s + l.count, 0);
  // 求 max wealth_pct 用于宽度归一
  const maxPct = Math.max(0.01, ...layers.map((l) => l.wealth_pct || 0));

  return (
    <div className="virtualCity-pyramid">
      <div className="virtualCity-pyramid__title">
        {t('virtualCity.dashboard.pyramid.title' as TKey)}
      </div>
      <div className="virtualCity-pyramid__layers">
        {/* 自下而上:survival → accumulation → freedom */}
        {[...layers].reverse().map((layer) => {
          // ⚠ 兜底：后端新增层级名时 LABEL_KEYS 会漏项 ⇒ t(undefined) 返回 undefined
          //   ⇒ 渲染空标签（不崩，但信息丢失）。查不到时用**原始 name** 兜底显示。
          const labelKey = LABEL_KEYS[layer.name as keyof typeof LABEL_KEYS];
          const label = labelKey ? t(labelKey as TKey) : layer.name;
          const dotClass = DOT_CLASS[layer.name as keyof typeof DOT_CLASS];
          const barWidth = Math.min(100, (layer.wealth_pct / maxPct) * 100);
          const pctText = formatPct(layer.wealth_pct, 1);
          return (
            <div className="virtualCity-pyramid__row" key={layer.name}>
              <div className="virtualCity-pyramid__row-label">
                <i className={`virtualCity-pyramid__row-dot virtualCity-pyramid__row-dot--${dotClass}`} />
                <span>{label}</span>
              </div>
              <div className="virtualCity-pyramid__row-bar">
                <div
                  className={`virtualCity-pyramid__row-bar-fill virtualCity-pyramid__row-bar-fill--${dotClass}`}
                  style={{ width: `${barWidth}%` }}
                  aria-label={`${label} ${pctText}`}
                />
              </div>
              <div className="virtualCity-pyramid__row-meta">
                <div>{t('virtualCity.dashboard.pyramid.peopleCount' as TKey, { n: layer.count })}</div>
                <div style={{ fontSize: '11px' }}>
                  {t('virtualCity.dashboard.pyramid.virtualCityPct' as TKey, { pct: pctText })}
                </div>
                <div style={{ fontSize: '10.5px', color: 'var(--virtualCity-text-dim)' }}>
                  ¥{formatCny(layer.avg_wealth)} / 人均
                </div>
              </div>
            </div>
          );
        })}
      </div>
      {totalCount === 0 && (
        <div className="virtualCity-pyramid__empty">—</div>
      )}
    </div>
  );
}