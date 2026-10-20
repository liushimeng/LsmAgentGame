/**
 * MacroPanel — 批次 62「集体杠杆泡沫」宏观面板。
 *
 * 权威来源：
 *   - 设计：`lag_docs/虚拟城市/已实现/62-居民行为金融与泡沫反身性引擎/01-方案设计.md` §5/§6
 *   - 后端产出：`ServerGo/game/virtual_city/bubble.go::bubbleSnapshot` /
 *     `liquidity.go::liquiditySpreadSnapshotOf`
 *
 * 面板内容：
 *   ① 泡沫状态徽章（none / overheat / burst / recession）+ 已持续月数
 *   ② 三项集体指标（市场杠杆率 / 资产换手率 / 现金占比）—— 带阈值标线的进度条
 *      · 杠杆率 > 60% ⇒ 信贷过热
 *      · 换手率 > 30% ⇒ 投机过热
 *      · 现金占比 < 10% ⇒ 流动性紧张（**反向**：低于阈值才是预警）
 *   ③ 破裂播报（state === 'burst' 时高亮）
 *   ④ 资产流动性价差表 + 周期修正系数 + 教学说明
 *
 * 可见性：宏观数据**房间级**（所有座位 + 观战者都能看）——这正是「泡沫是集体行为
 * 的结果」这一教学点的载体：观战者也能看到自己所在城市的杠杆率在涨。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type {
  VirtualCityBubbleSnapshot,
  VirtualCityBubbleState,
  VirtualCityLiquiditySpreadSnapshot,
} from '@/types/virtualCityBehavior';

interface Props {
  /** game.state.bubble；缺失 = 引擎未启用（渲染空态）。 */
  bubble: VirtualCityBubbleSnapshot | null;
  /** game.state.liquidity_spreads；缺失 = 引擎未启用。 */
  spreads: VirtualCityLiquiditySpreadSnapshot | null;
}

/** 三项指标的阈值（设计 §5.1）。 */
const LEVERAGE_THRESHOLD = 0.6;
const TURNOVER_THRESHOLD = 0.3;
const CASH_THRESHOLD = 0.1;

/** 泡沫状态 → i18n 键。 */
const STATE_LABEL: Record<VirtualCityBubbleState, string> = {
  none: 'virtualCity.macro.bubbleNone',
  overheat: 'virtualCity.macro.bubbleOverheat',
  burst: 'virtualCity.macro.bubbleBurst',
  recession: 'virtualCity.macro.bubbleRecession',
};

/** 价差表的展示顺序（设计 §7.2 表序）。 */
const SPREAD_ROWS: Array<{ key: keyof VirtualCityLiquiditySpreadSnapshot; label: string }> = [
  { key: 'stock', label: 'virtualCity.macro.spreadStock' },
  { key: 'bond', label: 'virtualCity.macro.spreadBond' },
  { key: 'gold', label: 'virtualCity.macro.spreadGold' },
  { key: 'house', label: 'virtualCity.macro.spreadHouse' },
  { key: 'equity', label: 'virtualCity.macro.spreadEquity' },
  { key: 'collectible', label: 'virtualCity.macro.spreadCollectible' },
];

export function MacroPanel({ bubble, spreads }: Props) {
  const t = useT();

  if (!bubble && !spreads) {
    return (
      <div className="virtualCity-macro" data-testid="virtualCity-macro-panel">
        <div className="virtualCity-macro__head">
          <span className="virtualCity-macro__title">
            🌐 {t('virtualCity.macro.title' as TKey)}
          </span>
        </div>
        <div className="virtualCity-panel__empty">
          <p>{t('virtualCity.macro.empty' as TKey)}</p>
        </div>
      </div>
    );
  }

  const b = bubble;
  const state: VirtualCityBubbleState = b?.state ?? 'none';

  return (
    <div className="virtualCity-macro" data-testid="virtualCity-macro-panel">
      <div className="virtualCity-macro__head">
        <span className="virtualCity-macro__title">
          🌐 {t('virtualCity.macro.title' as TKey)}
        </span>
      </div>

      {/* ① 泡沫状态 */}
      <div className="virtualCity-macro__section">
        <span className="virtualCity-macro__section-title">
          {t('virtualCity.macro.bubbleState' as TKey)}
        </span>
        <div className="virtualCity-macro__state">
          <span
            className={`virtualCity-macro__state-badge virtualCity-macro__state-badge--${state}`}
            data-testid="virtualCity-macro-state"
          >
            {t(STATE_LABEL[state] as TKey)}
          </span>
          {b && b.months_in_state > 0 && (
            <span className="virtualCity-macro__state-months">
              {t('virtualCity.macro.monthsInState' as TKey, { n: b.months_in_state })}
            </span>
          )}
        </div>
      </div>

      {/* ② 三项集体指标 */}
      {b && (
        <div className="virtualCity-macro__section">
          <MetricRow
            label={t('virtualCity.macro.metricLeverage' as TKey)}
            value={b.leverage_ratio}
            threshold={LEVERAGE_THRESHOLD}
            thresholdText={t('virtualCity.macro.threshold' as TKey, { pct: '60%' })}
            hint={t('virtualCity.macro.metricLeverageHint' as TKey)}
            /** 杠杆率：**超过**阈值即预警。 */
            mode="above"
          />
          <MetricRow
            label={t('virtualCity.macro.metricTurnover' as TKey)}
            value={b.turnover_ratio}
            threshold={TURNOVER_THRESHOLD}
            thresholdText={t('virtualCity.macro.threshold' as TKey, { pct: '30%' })}
            hint={t('virtualCity.macro.metricTurnoverHint' as TKey)}
            mode="above"
          />
          <MetricRow
            label={t('virtualCity.macro.metricCash' as TKey)}
            value={b.cash_ratio}
            threshold={CASH_THRESHOLD}
            thresholdText={t('virtualCity.macro.threshold' as TKey, { pct: '10%' })}
            hint={t('virtualCity.macro.metricCashHint' as TKey)}
            /** 现金占比：**低于**阈值才是预警（反向指标）。 */
            mode="below"
          />
        </div>
      )}

      {/* ③ 破裂播报 */}
      {b && state === 'burst' && b.message && (
        <div className="virtualCity-macro__burst" data-testid="virtualCity-macro-burst">
          <span className="virtualCity-macro__burst-title">
            {t('virtualCity.macro.bubbleMessageTitle' as TKey)}
          </span>
          {b.message}
        </div>
      )}

      {/* ④ 流动性价差表 */}
      {spreads && (
        <div className="virtualCity-macro__section">
          <span className="virtualCity-macro__section-title">
            {t('virtualCity.macro.spreadTitle' as TKey)}
          </span>
          <table className="virtualCity-macro__spread-table">
            <thead>
              <tr>
                <th>{t('virtualCity.macro.spreadTitle' as TKey)}</th>
                <th style={{ textAlign: 'right' }}>Bid-Ask</th>
              </tr>
            </thead>
            <tbody>
              {SPREAD_ROWS.map((row) => (
                <tr key={row.key} data-testid={`virtualCity-macro-spread-${row.key}`}>
                  <td>{t(row.label as TKey)}</td>
                  <td>{(spreads[row.key] * 100).toFixed(1)}%</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="virtualCity-macro__phase">
            {t('virtualCity.macro.spreadPhase' as TKey, {
              mul: spreads.phase_multiplier.toFixed(2),
            })}
          </p>
          <p className="virtualCity-macro__spread-hint">
            {t('virtualCity.macro.spreadHint' as TKey)}
          </p>
        </div>
      )}
    </div>
  );
}

/**
 * 单条集体指标：数值 + 进度条 + 阈值标线。
 *
 * `mode`：
 *   - 'above'：**超过**阈值即预警（杠杆率 / 换手率）
 *   - 'below' ：**低于**阈值即预警（现金占比）
 * 进度条按「阈值 × 2」为满格归一（0.6 的阈值线落在 30% 处，视觉上直观）。
 */
function MetricRow({
  label,
  value,
  threshold,
  thresholdText,
  hint,
  mode,
}: {
  label: string;
  value: number;
  threshold: number;
  thresholdText: string;
  hint: string;
  mode: 'above' | 'below';
}) {
  const full = Math.max(threshold * 2, 0.0001);
  const ratio = Math.max(0, Math.min(1, value / full));
  const thresholdPos = Math.max(0, Math.min(1, threshold / full));
  const warn = mode === 'above' ? value > threshold : value < threshold;
  const hit = mode === 'above' ? value > threshold * 1.2 : value < threshold * 0.6;

  return (
    <div
      className={
        'virtualCity-macro__metric' +
        (hit ? ' virtualCity-macro__metric--hit' : warn ? ' virtualCity-macro__metric--warn' : '')
      }
    >
      <div className="virtualCity-macro__metric-head">
        <span>{label}</span>
        <span className="virtualCity-macro__metric-value">{(value * 100).toFixed(1)}%</span>
      </div>
      <div className="virtualCity-macro__metric-track">
        <span className="virtualCity-macro__metric-fill" style={{ width: `${ratio * 100}%` }} />
        <span
          className="virtualCity-macro__metric-threshold"
          style={{ left: `${thresholdPos * 100}%` }}
          title={thresholdText}
        />
      </div>
      <p className="virtualCity-macro__metric-hint">{hint}</p>
    </div>
  );
}

export default MacroPanel;
