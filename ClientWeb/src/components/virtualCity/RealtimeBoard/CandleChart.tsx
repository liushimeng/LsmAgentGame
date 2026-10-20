/**
 * CandleChart — 批次 65 实时看板手写 SVG 蜡烛图。
 *
 * 数据源诚实口径（方案 §5.2）：服务端 History 是「自采价点环（≤48 点）」，
 * 客户端聚合 5min OHLC；聚合桶数 <2 时降级折线 sparkline 并标注「采集中」，
 * 不做伪历史。全项目纯 SVG 传统，不引入图表库。
 */

import { useMemo } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { RealtimePoint } from '@/types/virtualCityRealtime';
import { aggregateOHLC } from './boardUtils';

/** A 股涨跌色惯例（涨红跌绿，面板底 ≥4.5:1，与 .virtualCity-num--up/down 同源）。 */
const COLOR_UP = '#f87171';
const COLOR_DOWN = '#4ade80';
const COLOR_LINE = '#60a5fa';

interface Props {
  /** 自采价点序列（≤48 点 {t,p}）。 */
  points: RealtimePoint[];
  height?: number;
}

const W = 300;
const PAD_TOP = 6;
const PAD_BOTTOM = 16;

export function CandleChart({ points, height = 120 }: Props) {
  const t = useT();
  const candles = useMemo(() => aggregateOHLC(points), [points]);

  const { min, max } = useMemo(() => {
    if (!candles.length) return { min: 0, max: 1 };
    const hi = Math.max(...candles.map((c) => c.high));
    const lo = Math.min(...candles.map((c) => c.low));
    return { min: lo, max: hi };
  }, [candles]);
  const span = max - min || 1;
  const y = (v: number) => PAD_TOP + (1 - (v - min) / span) * (height - PAD_TOP - PAD_BOTTOM);

  if (candles.length < 2) {
    // ── 采集中降级：折线（<2 点连不成线就只画轴与提示）──
    const pts = [...points].sort((a, b) => a.t - b.t);
    const line = pts
      .map((p, i) => {
        const x = pts.length > 1 ? (i / (pts.length - 1)) * (W - 8) + 4 : W / 2;
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y(p.p).toFixed(1)}`;
      })
      .join(' ');
    return (
      <div className="virtualCity-rt__candle">
        <div className="virtualCity-rt__candle-tag">
          ⏳ {t('virtualCity.rt.collecting' as TKey)}
        </div>
        <svg viewBox={`0 0 ${W} ${height}`} width="100%" height={height} role="img"
          aria-label={t('virtualCity.rt.collecting' as TKey)}>
          {pts.length >= 2 && (
            <path d={line} fill="none" stroke={COLOR_LINE} strokeWidth="1.5" />
          )}
        </svg>
      </div>
    );
  }

  // ── 蜡烛图 ──
  const slot = (W - 16) / candles.length;
  const bodyW = Math.max(2, Math.min(10, slot * 0.55));
  return (
    <div className="virtualCity-rt__candle">
      <svg viewBox={`0 0 ${W} ${height}`} width="100%" height={height} role="img"
        aria-label={t('virtualCity.rt.candleAlt' as TKey)}>
        {/* 价格区间标注（左下 min / 右上 max） */}
        <text x={4} y={height - 4} className="virtualCity-rt__candle-axis">
          {min.toFixed(2)}
        </text>
        <text x={W - 4} y={12} textAnchor="end" className="virtualCity-rt__candle-axis">
          {max.toFixed(2)}
        </text>
        {candles.map((c, i) => {
          const up = c.close >= c.open;
          const color = up ? COLOR_UP : COLOR_DOWN;
          const cx = 8 + slot * i + slot / 2;
          const bodyTop = y(Math.max(c.open, c.close));
          const bodyH = Math.max(1, Math.abs(y(c.open) - y(c.close)));
          return (
            <g key={c.t}>
              {/* 影线 */}
              <line x1={cx} y1={y(c.high)} x2={cx} y2={y(c.low)} stroke={color} strokeWidth="1" />
              {/* 实体 */}
              <rect
                x={cx - bodyW / 2}
                y={bodyTop}
                width={bodyW}
                height={bodyH}
                fill={color}
                rx="0.5"
              />
            </g>
          );
        })}
      </svg>
    </div>
  );
}

export default CandleChart;
