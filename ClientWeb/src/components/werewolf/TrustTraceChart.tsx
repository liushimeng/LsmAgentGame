// §20260812-01 U4 — 发言信任度轨迹图(纯前端 SVG)。
//
// 13 玩家 4 色折线(W=狼阵营=G 好人阵营=D 神职=未知),X 轴 Day 1..N,Y 轴 -1~+1。
// 颜色遵循 §26.4 状态徽章色板(线宽 ≥ 2px 防 hover 漏接)。
import { useMemo } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

export interface TrustTraceEntry {
  seat: number;
  day: number;
  score: number; // -1.0 ~ +1.0
}

export interface TrustTraceChartProps {
  entries: TrustTraceEntry[];
  factionBySeat?: Record<number, 'wolf' | 'good' | 'divine' | 'unknown'>;
}

const WIDTH = 480;
const HEIGHT = 240;
const PADDING = 32;

const COLORS: Record<string, string> = {
  wolf: '#E74C3C',    // 红 §26.4
  good: '#2ECC71',    // 绿
  divine: '#9B59B6',  // 紫
  unknown: '#95A5A6', // 灰
};

export function TrustTraceChart({ entries, factionBySeat }: TrustTraceChartProps) {
  const t = useT();
  const { days, bySeat, maxDay } = useMemo(() => {
    const daySet = new Set<number>();
    const grouped: Record<number, { day: number; score: number }[]> = {};
    for (const e of entries) {
      daySet.add(e.day);
      if (!grouped[e.seat]) grouped[e.seat] = [];
      grouped[e.seat].push({ day: e.day, score: e.score });
    }
    const sortedDays = Array.from(daySet).sort((a, b) => a - b);
    return { days: sortedDays, bySeat: grouped, maxDay: sortedDays.length };
  }, [entries]);

  if (entries.length === 0 || maxDay === 0) {
    return (
      <div className="trust-trace-chart trust-trace-chart--empty">
        <p>{t('werewolf.trustTrace.title' as TKey)} — —</p>
      </div>
    );
  }

  const innerW = WIDTH - 2 * PADDING;
  const innerH = HEIGHT - 2 * PADDING;
  const xScale = (d: number) => PADDING + ((d - 1) / Math.max(maxDay - 1, 1)) * innerW;
  const yScale = (s: number) => PADDING + (1 - (s + 1) / 2) * innerH; // s=-1→bottom, +1→top

  return (
    <div className="trust-trace-chart" data-testid="trust-trace-chart">
      <h4 className="trust-trace-chart__title">📈 {t('werewolf.trustTrace.title' as TKey)}</h4>
      <svg width={WIDTH} height={HEIGHT} className="trust-trace-chart__svg" role="img" aria-label="trust-trace">
        {/* Y 轴 0 线 */}
        <line
          x1={PADDING}
          y1={yScale(0)}
          x2={WIDTH - PADDING}
          y2={yScale(0)}
          stroke="rgba(255,255,255,0.3)"
          strokeWidth={1}
        />
        {/* X 轴 Day 标签 */}
        {days.map((d) => (
          <text
            key={`x-${d}`}
            x={xScale(d)}
            y={HEIGHT - 8}
            textAnchor="middle"
            fontSize={11}
            fill="rgba(255,255,255,0.7)"
          >
            D{d}
          </text>
        ))}
        {/* 折线 */}
        {Object.entries(bySeat).map(([seat, points]) => {
          const faction = (factionBySeat?.[Number(seat)] ?? 'unknown') as string;
          const color = COLORS[faction] ?? COLORS.unknown;
          const path = points
            .map((p, i) => `${i === 0 ? 'M' : 'L'} ${xScale(p.day)} ${yScale(p.score)}`)
            .join(' ');
          return (
            <g key={seat}>
              <path d={path} fill="none" stroke={color} strokeWidth={2} strokeLinecap="round" />
              {points.map((p, i) => (
                <circle
                  key={i}
                  cx={xScale(p.day)}
                  cy={yScale(p.score)}
                  r={3}
                  fill={color}
                />
              ))}
            </g>
          );
        })}
      </svg>
      <div className="trust-trace-chart__legend">
        <span style={{ color: COLORS.wolf }}>● {t('werewolf.trustTrace.legendWolf' as TKey)}</span>
        <span style={{ color: COLORS.good }}>● {t('werewolf.trustTrace.legendGood' as TKey)}</span>
        <span style={{ color: COLORS.divine }}>● {t('werewolf.trustTrace.legendDivine' as TKey)}</span>
      </div>
    </div>
  );
}
