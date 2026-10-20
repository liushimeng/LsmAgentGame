/**
 * virtualCityFormat — 虚拟城市展示辅助函数（批次62 从 types/virtualCity.ts 拆出）。
 *
 * 拆分原因：CLAUDE.md §4「单文件 ≤ 1800 行」硬上限 —— 批次 61（城区坐标改版）+
 * 批次 62（`VirtualCityActionType` / `VirtualCityAction` / `WEALTH_BEHAVIOR_ACTIONS`）
 * 追加后 virtualCity.ts 达 1821 行超限。本段是**自包含的纯展示函数**（无类型依赖
 * 回环），搬移后 virtualCity.ts 以 re-export 保持既有导出名不变 ——
 * 29 个 import 方零改动（§4 允许的「纯代码搬移，不改逻辑/签名/导出命名」）。
 *
 * 引用方式不变：`import { formatCny } from '@/types/virtualCity'`。
 */

import type { VirtualCityMinskyTier } from './virtualCity';

/** 人民币金额人读化：≥1 亿 → "x.xx亿"；≥1 万 → "x.x万"；否则整数千分位。 */
export function formatCny(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '—';
  const abs = Math.abs(n);
  const sign = n < 0 ? '-' : '';
  if (abs >= 1e8) return `${sign}${(abs / 1e8).toFixed(2)}亿`;
  if (abs >= 1e4) return `${sign}${(abs / 1e4).toFixed(1)}万`;
  return `${sign}${Math.round(abs).toLocaleString('zh-CN')}`;
}

/** 带符号差额（+/-）用于涨跌箭头。 */
export function formatDelta(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '—';
  if (n > 0) return `+${formatCny(n)}`;
  return formatCny(n);
}

/** 百分比展示（0.035 → "3.5%"）。 */
export function formatPct(v: number | undefined | null, digits = 1): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return `${(v * 100).toFixed(digits)}%`;
}

/** FI Index 档位色（前端架构文档 §5：<0.5 灰 / 0.5–1 蓝 / ≥1 绿 / ≥1.5 金）。 */
export function fiIndexColor(fi: number): string {
  if (fi >= 1.5) return '#d4a017';
  if (fi >= 1) return '#34d399';
  if (fi >= 0.5) return '#60a5fa';
  return '#9ca3af';
}

/** 明斯基融资等级色（暗色主题 ≥5:1）。 */
export function minskyTierColor(tier: VirtualCityMinskyTier | undefined | null): string {
  switch (tier) {
    case 'hedge': return '#4caf50';       // 对冲 — 绿（白字 ≈ 5.4:1）
    case 'speculative': return '#ff9800'; // 投机 — 橙（白字 ≈ 4.6:1 on #10151d）
    case 'ponzi': return '#f44336';       // 庞氏 — 红（白字 ≈ 5.7:1）
    default: return '#9ca3af';
  }
}
