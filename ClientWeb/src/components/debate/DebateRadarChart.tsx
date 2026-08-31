/**
 * 辩论 5 维评分雷达图 (2026-08-31 §20260831-04)
 *
 * 纯 SVG 实现(不引入图表库):
 *   - 5 个维度:论证质量 / 逻辑严谨 / 语言表达 / 团队配合 / 反驳效力
 *   - 网格环:2/4/6/8/10 五档
 *   - 多队并排渲染(每队一张迷你雷达,色相区分,对齐 §26 对比度规范)
 */
import type { CSSProperties } from 'react';

/** 5 维 key → 中文短标签(与后端 ScoreDimensions json tag 一一对应) */
const DIMENSIONS: Array<{ key: string; label: string }> = [
  { key: 'argument_quality', label: '论证' },
  { key: 'logic_rigor', label: '逻辑' },
  { key: 'language_expression', label: '表达' },
  { key: 'team_coordination', label: '配合' },
  { key: 'rebuttal_effectiveness', label: '反驳' },
];

/** 每队雷达描边/填充色(暗底对比度 ≥ 4.5:1,§26.1) */
export const TEAM_RADAR_COLORS = [
  '#f5c518', // 金
  '#4dd0e1', // 青
  '#e070c8', // 品红
  '#7bd47b', // 绿
  '#ff9d5c', // 橙
];

interface RadarChartProps {
  /** 维度 → 1-10 分 */
  dimensionScores: Record<string, number>;
  /** 描边色(默认金色) */
  color?: string;
  /** SVG 视口宽高(px,默认 120) */
  size?: number;
  /** 是否填充多边形(默认 true) */
  filled?: boolean;
  style?: CSSProperties;
}

/**
 * 计算 5 边形顶点。
 * 第 i 轴角度 = -90° + i * 72°(从正上方顺时针),半径 = r * score/10。
 */
function polygonPoints(cx: number, cy: number, r: number, scores: number[]): string {
  return scores
    .map((score, i) => {
      const angle = (-90 + i * 72) * (Math.PI / 180);
      const rad = (r * Math.min(10, Math.max(0, score))) / 10;
      const x = cx + rad * Math.cos(angle);
      const y = cy + rad * Math.sin(angle);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
}

export default function DebateRadarChart({
  dimensionScores,
  color = TEAM_RADAR_COLORS[0],
  size = 120,
  filled = true,
  style,
}: RadarChartProps) {
  const cx = size / 2;
  const cy = size / 2;
  const r = size / 2 - 18; // 留出标签空间

  const scores = DIMENSIONS.map((d) => Number(dimensionScores?.[d.key] ?? 0));

  return (
    <svg
      className="debate-radar"
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      role="img"
      aria-label="五维评分雷达图"
      style={style}
    >
      {/* 网格环:2/4/6/8/10 */}
      {[2, 4, 6, 8, 10].map((ring) => (
        <polygon
          key={ring}
          className="debate-radar__ring"
          points={polygonPoints(cx, cy, r, Array(5).fill(ring))}
        />
      ))}

      {/* 轴线 */}
      {DIMENSIONS.map((_, i) => {
        const angle = (-90 + i * 72) * (Math.PI / 180);
        return (
          <line
            key={i}
            className="debate-radar__axis"
            x1={cx}
            y1={cy}
            x2={cx + r * Math.cos(angle)}
            y2={cy + r * Math.sin(angle)}
          />
        );
      })}

      {/* 评分多边形 */}
      <polygon
        className="debate-radar__poly"
        points={polygonPoints(cx, cy, r, scores)}
        stroke={color}
        fill={filled ? color : 'none'}
      />

      {/* 维度标签 */}
      {DIMENSIONS.map((d, i) => {
        const angle = (-90 + i * 72) * (Math.PI / 180);
        const lx = cx + (r + 11) * Math.cos(angle);
        const ly = cy + (r + 11) * Math.sin(angle);
        return (
          <text
            key={d.key}
            className="debate-radar__label"
            x={lx}
            y={ly}
            textAnchor="middle"
            dominantBaseline="middle"
          >
            {d.label}
          </text>
        );
      })}
    </svg>
  );
}
