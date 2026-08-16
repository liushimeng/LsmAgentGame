// ModelRadarChart — 5-dimension capability radar chart (SVG, no deps).
// §20260812-02 U1 — 模型能力雷达图。
//
// Pure SVG pentagonal radar chart. Each model is rendered as a semi-transparent
// polygon; axes are labelled at the 5 vertices. The chart auto-sizes to its
// container width (min 280px) and scales to show up to 8 models.
//
// §136 跨游戏约束:本组件放 components/common/（跨游戏共享）。

import { useMemo } from 'react';
import type { ModelRadarStats } from '@/api/llm';

const DIMS = [
  { key: 'win_rate' as const, label: '总胜率' },
  { key: 'wolf_win_rate' as const, label: '狼人胜率' },
  { key: 'good_win_rate' as const, label: '好人胜率' },
  { key: 'token_eff' as const, label: 'Token效率' },
  { key: 'coin_per_game' as const, label: '金币均值' },
] as const;

const COLORS = [
  '#4fc3f7', '#ab47bc', '#66bb6a', '#ffa726',
  '#ef5350', '#26c6da', '#8d6e63', '#ec407a',
];

function polar(cx: number, cy: number, r: number, angleDeg: number) {
  const rad = ((angleDeg - 90) * Math.PI) / 180;
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) };
}

function polygonPoints(
  cx: number, cy: number, r: number, values: number[],
): string {
  return values
    .map((v, i) => {
      const p = polar(cx, cy, (v / 100) * r, (360 / 5) * i);
      return `${p.x},${p.y}`;
    })
    .join(' ');
}

interface Props {
  data: Record<string, ModelRadarStats>;
  width?: number;
}

export function ModelRadarChart({ data, width = 480 }: Props) {
  const models = useMemo(
    () => Object.values(data).filter((m) => m.games > 0),
    [data],
  );

  const cx = width / 2;
  const cy = width / 2;
  const r = width / 2 - 60; // margin for labels

  // Background grid (3 concentric pentagons at 33/66/100%)
  const gridLevels = [0.33, 0.66, 1.0];

  return (
    <div className="model-radar-chart">
      <svg
        viewBox={`0 0 ${width} ${width}`}
        width={width}
        height={width}
        style={{ overflow: 'visible' }}
      >
        {/* Grid pentagons — §20260816-04 提升暗色主题可见性 */}
        {gridLevels.map((lv) => (
          <polygon
            key={lv}
            points={polygonPoints(cx, cy, r * lv, [100, 100, 100, 100, 100])}
            fill="none"
            stroke="rgba(255,255,255,0.18)"
            strokeWidth={lv === 1 ? 1.5 : 0.8}
          />
        ))}

        {/* Axis lines + labels — §20260816-04 增强对比度 */}
        {DIMS.map((dim, i) => {
          const p = polar(cx, cy, r + 20, (360 / 5) * i);
          const lp = polar(cx, cy, r + 36, (360 / 5) * i);
          return (
            <g key={dim.key}>
              <line
                x1={cx}
                y1={cy}
                x2={p.x}
                y2={p.y}
                stroke="rgba(255,255,255,0.18)"
                strokeWidth={0.8}
              />
              <text
                x={lp.x}
                y={lp.y}
                textAnchor="middle"
                dominantBaseline="middle"
                fill="rgba(255,255,255,0.85)"
                fontSize={11}
                fontWeight={600}
              >
                {dim.label}
              </text>
            </g>
          );
        })}

        {/* Model polygons */}
        {models.map((m, idx) => {
          const values = DIMS.map((d) => m[d.key]);
          const color = COLORS[idx % COLORS.length];
          return (
            <g key={m.provider_id}>
              <polygon
                points={polygonPoints(cx, cy, r, values)}
                fill={color}
                fillOpacity={0.12}
                stroke={color}
                strokeWidth={2}
                strokeLinejoin="round"
              />
              {/* Data point dots */}
              {values.map((v, i) => {
                const p = polar(cx, cy, (v / 100) * r, (360 / 5) * i);
                return (
                  <circle
                    key={i}
                    cx={p.x}
                    cy={p.y}
                    r={3}
                    fill={color}
                    stroke="#fff"
                    strokeWidth={1}
                  />
                );
              })}
            </g>
          );
        })}
      </svg>

      {/* Legend */}
      <div className="model-radar-legend">
        {models.map((m, idx) => (
          <div key={m.provider_id} className="model-radar-legend-item">
            <span
              className="model-radar-legend-dot"
              style={{ background: COLORS[idx % COLORS.length] }}
            />
            <span className="model-radar-legend-name">
              {m.agent_name}
              {!m.sample_ok && (
                <span className="model-radar-legend-sample" title="样本不足 8 局">
                  ⚠
                </span>
              )}
            </span>
            <span className="model-radar-legend-games">{m.games}局</span>
          </div>
        ))}
      </div>
    </div>
  );
}
