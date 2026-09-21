/**
 * CityStatsPanel — 城市背景层面板（§20260921 建房解耦，契约 §2.4）。
 *
 * 数据源 game.state.city（WealthCitySnapshot，view.go omitempty）：
 * resident_count=0 的旧房不下发 → 本面板整块不渲染（return null）。
 *
 * 展示分三段：
 *   ① 头部 `🏙 城市 · N 人` + 指标网格（就业率 / 收入中位数 / 居民储蓄合计 /
 *      压力率；金额千分位、率百分比 —— formatCny / formatPct 复用）
 *   ② 8 城区人口迷你条形（纯 CSS div 宽度百分比，相对最大区人口；
 *      条形用城区主色实底 ≥45% 不透明度，数值以文本显式 color 标注在条外，
 *      色盲不依赖颜色单独判读 —— CLAUDE.md §26）
 *   ③ 「居民之声」最近列表（月份 + 代号 + 一句话 + 服务模型小徽标）
 *
 * 样式在同名 CityStatsPanel.css（组件内直接 import， precedent：
 * ChatSettingsModal.css / WalletModal.css；不新增 globals.css 入口）。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, formatPct, type WealthCitySnapshot } from '@/types/wealth';

interface Props {
  /** 城市背景层快照；缺省（旧房 / 尚未到达）时整面板不渲染。 */
  city?: WealthCitySnapshot | null;
}

export function CityStatsPanel({ city }: Props) {
  const t = useT();
  if (!city) return null;

  const districts = city.districts ?? [];
  const maxPop = districts.reduce((m, d) => Math.max(m, d.population || 0), 0) || 1;
  const voices = (city.voices ?? []).slice(-8).reverse();

  return (
    <div className="wealth-citypanel" data-testid="wealth-city-panel">
      {/* ① 头部：城市 · N 人 */}
      <div className="wealth-citypanel__title">
        🏙 {t('wealth.cityTitle' as TKey)} · {(city.resident_count || 0).toLocaleString()}
      </div>

      {/* 指标网格：就业率 / 收入中位数 / 居民储蓄合计 / 压力率 */}
      <div className="wealth-citypanel__stats">
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.cityEmployment' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatPct(city.employment_rate)}</span>
        </div>
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.cityMedianIncome' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatCny(city.median_income)}</span>
        </div>
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.citySavings' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatCny(city.total_savings)}</span>
        </div>
        <div className="wealth-citypanel__stat">
          <span className="wealth-citypanel__stat-k">{t('wealth.cityStress' as TKey)}</span>
          <span className="wealth-citypanel__stat-v">{formatPct(city.stressed_rate)}</span>
        </div>
      </div>

      {/* ② 8 城区人口迷你条形（div 宽度百分比，§26 对比度：实底条形 + 显式文字） */}
      {districts.length > 0 && (
        <div className="wealth-citypanel__districts">
          {districts.map((d, i) => {
            const pct = Math.max(2, Math.round(((d.population || 0) / maxPop) * 100));
            return (
              <div
                key={d.id ?? i}
                className="wealth-citypanel__district"
                data-testid={`wealth-city-district-${d.id ?? i}`}
              >
                <span className="wealth-citypanel__district-name">{d.name}</span>
                <span className="wealth-citypanel__district-bar">
                  <span
                    className="wealth-citypanel__district-fill"
                    style={{ width: `${pct}%` }}
                    aria-hidden="true"
                  />
                </span>
                <span className="wealth-citypanel__district-pop">
                  {(d.population || 0).toLocaleString()}
                </span>
              </div>
            );
          })}
        </div>
      )}

      {/* ③ 居民之声（最近若干条，倒序展示最新在前） */}
      {voices.length > 0 && (
        <div className="wealth-citypanel__voices">
          <div className="wealth-citypanel__voices-title">
            🗣 {t('wealth.cityVoices' as TKey)}
          </div>
          {voices.map((v, i) => (
            <div
              key={`${v.month}-${v.name}-${i}`}
              className="wealth-citypanel__voice"
              data-testid="wealth-city-voice"
            >
              <span className="wealth-citypanel__voice-meta">
                <span className="wealth-citypanel__voice-month">
                  {t('wealth.cityVoiceOfMonth' as TKey, { month: v.month })}
                </span>
                <span className="wealth-citypanel__voice-name">{v.name}</span>
                {v.model && (
                  <span className="wealth-citypanel__voice-model" title={v.model}>
                    🤖 {v.model}
                  </span>
                )}
              </span>
              <span className="wealth-citypanel__voice-text">{v.text}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export default CityStatsPanel;
