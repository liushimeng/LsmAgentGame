/**
 * CalibrationPanel — 批次 71 看板第 12 维度「校准」。
 *
 * 定位：把「这座城市跑得对不对、稳不稳、可不可复现」变成**可判定的事实**，
 * 而不是文档里的承诺（方案 §2.1 G6 / §8.2）。
 *
 * 五个区块（方案 §8.2）+ 批次 72 第 6 区块：
 *   ① 状态哈希   —— 当前月 `state_hash`（等宽 + 可复制）+ 所属月
 *   ② 不变量列表 —— 7 条 I1/I2/I3/I4/I5/I6/I8：✔/✘ + 实测值 + 说明；
 *                  批次 72 起 I6 可为「无样本」灰徽章（passed && no_sample，
 *                  既不是 ✔ 也不是 ✘ —— 颜色与文案双通道区分）
 *   ③ D 指标     —— D2 货币扩张 / D5 破产率 / Gini / 失业率 / CPI / 存活座位 / 账外净额
 *   ④ 红线       —— 触发（红）与未触发（灰），带实测 vs 阈值；批次 72 起 5 → 7 条
 *                  （R-E1/R-E2 追加尾部，名称直接用后端 name，前端不映射）
 *   ⑤ 历史       —— 最近 12 月哈希 + 不变量全通过与否（迷你条）
 *   ⑥ 运行历史   —— 批次 72（`vc-calib-runs-*` class 前缀）：
 *                  最近 12 局运行表（同时充当趋势数据的 table view 可访问性兜底）
 *                  + D2/D5 各自独立 y 标尺的 sparkline（量纲不同，禁止双轴）
 *                  + 品类年化 4 行小表（收益类 ⇒ pos/neg 配色组，见下）
 *
 * 口径纪律：
 *   - **整体可能不存在**（老对局 / 后端未升级）⇒ 走空态「本局尚未产生校准数据」，
 *     绝不白屏、绝不报错（方案 §9.2）。
 *   - **字段缺失 = 「—」**，与「0」区分（三态纪律，同批次 70）。空 `runs` /
 *     缺 `category_annualized`（批次 71 老对局）同样走空态 / '—'，不得报错。
 *   - **通过 / 失败不得只靠颜色区分**（色弱可达性）⇒ 一律带 ✔ / ✘ 图标 + 文字
 *     （CLAUDE.md §26.2 反模式 ④）；I6「无样本」用 — 图标 + 独立文案。
 *   - **未知枚举值回退原字符串**：不变量/红线回退后端 `name`；策略 / 品类
 *     回退原始 id（§12 纪律：映射表漏项不得显示裸 undefined / 空白）。
 *   - 百分比口径明确（`formatPct` 是「小数 → 百分比」；D2 是倍数、CPI 是指数，
 *     **不可**套百分比）。⚠ `category_annualized` 契约就是**小数年化**
 *     （0.081 ⇒ "8.1%"）⇒ 直接 `formatPct` 即可，**不得**再 ×100 一次
 *     （批次 65 formatPct 口径错配教训）。
 *   - **涨跌配色 house 规则**（virtualCity.css:1343）：`--up/--down`（涨红跌绿）
 *     仅限价格类；品类年化是**收益类** ⇒ 用 `virtualCity-num--pos/--neg`
 *     （收益绿 / 亏损红），不得用错组。
 *   - sparkline 线色复用面板既有色相（金 accent / 亮蓝），**不占用**
 *     good/serious 状态色组；趋势静态无动画（无 keyframes 需求）。
 */

import { useEffect, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type {
  VirtualCityCalibration,
  VirtualCityCalibrationInvariant,
  VirtualCityCalibrationRedline,
  VirtualCityGameState,
} from '@/types/virtualCity';
import {
  CALIBRATION_CATEGORY_KEYS,
  CALIBRATION_CATEGORY_ORDER,
  CALIBRATION_INVARIANT_KEYS,
  CALIBRATION_REDLINE_KEYS,
  CALIBRATION_STRATEGY_KEYS,
} from '@/types/virtualCityCalibration';
import { formatCny, formatPct } from '@/types/virtualCityFormat';

/** 数值 → 定点字符串；非有限数一律「—」（缺失 ≠ 0）。 */
function num(v: number | undefined | null, digits = 2): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '—';
  return v.toFixed(digits);
}

/** 整数计数；非有限数一律「—」。 */
function cnt(v: number | undefined | null): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '—';
  return String(Math.round(v));
}

/**
 * ⑥ 运行趋势 sparkline（批次 72）—— 单系列、独立 y 标尺、静态无动画。
 *
 * - 2px 线宽（`vector-effect: non-scaling-stroke` 保证任意容器宽下恒 2px）。
 * - 线色经 CSS 类（`vc-calib-runs__spark-line--d2/--d5`）落在样式表里复用
 *   面板既有 CSS 变量色相 —— **禁止**状态色（good/serious）做线色。
 * - 单点 / 空数据 ⇒ 退化为居中短横线（占位，绝不画误导性的平线）。
 * - `aria-hidden`：可访问性由运行表（同一数据的 table view）兜底。
 */
function RunSparkline({
  values,
  lineClass,
}: {
  values: number[];
  lineClass: string;
}) {
  const W = 132;
  const H = 30;
  const pts = values.filter((v) => Number.isFinite(v));
  let d: string;
  if (pts.length <= 1) {
    // 单点/空 ⇒ 居中短横线占位。
    const y = H / 2;
    d = `M${W / 2 - 10} ${y} L${W / 2 + 10} ${y}`;
  } else {
    const min = Math.min(...pts);
    const max = Math.max(...pts);
    const span = max - min || 1;
    d = pts
      .map((v, i) => {
        const x = 2 + (i / (pts.length - 1)) * (W - 4);
        const y = H - 4 - ((v - min) / span) * (H - 8);
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`;
      })
      .join(' ');
  }
  return (
    <svg
      className="vc-calib-runs__spark"
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <path
        className={`vc-calib-runs__spark-line ${lineClass}`}
        d={d}
        fill="none"
        strokeWidth={2}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

/** 运行表行 title 摘要：消费 outcome_counts / state_hash / finished_at（§130 接线）。 */
function runTitle(
  run: NonNullable<VirtualCityCalibration['runs']>[number],
): string {
  const outcomes = Object.entries(run.outcome_counts ?? {})
    .map(([k, n]) => `${k}:${n}`)
    .join(' ');
  const when = run.finished_at
    ? new Date(run.finished_at * 1000).toLocaleString()
    : '—';
  return `${run.run_id} · ${when}${outcomes ? ` · ${outcomes}` : ''} · hash=${run.state_hash || '—'}`;
}

export interface CalibrationPanelProps {
  /** game.state 快照（null = 对局未就绪）。 */
  gameState: VirtualCityGameState | null;
}

export function CalibrationPanel({ gameState }: CalibrationPanelProps) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const copyTimer = useRef<number | null>(null);

  // ⚠ 所有 hook 必须在早退**之前**调用（React hook-after-return 会让构建全绿照样崩）。
  useEffect(
    () => () => {
      if (copyTimer.current !== null) window.clearTimeout(copyTimer.current);
    },
    [],
  );

  const calibration: VirtualCityCalibration | null = gameState?.calibration ?? null;

  /** 复制状态哈希到剪贴板（best-effort：非 HTTPS / 浏览器拒绝时静默降级，不崩）。 */
  const handleCopy = async () => {
    const text = calibration?.state_hash ?? '';
    if (!text) return;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.setAttribute('readonly', '');
        ta.style.position = 'fixed';
        ta.style.left = '-9999px';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      setCopied(true);
      if (copyTimer.current !== null) window.clearTimeout(copyTimer.current);
      copyTimer.current = window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // best-effort：剪贴板不可用（权限 / 非安全上下文）⇒ 不改状态、不弹错。
    }
  };

  // ── 空态：老对局 / 后端未升级 ⇒ game.state.calibration 整个键缺省 ──────
  if (!calibration) {
    return (
      <div
        className="virtualCity-calib"
        data-testid="virtualCity-calib"
        data-vc-calibration-panel=""
        data-vc-calib-invariant-count="0"
        data-vc-calib-status="empty"
        title={t('virtualCity.calib.title' as TKey)}
      >
        <div className="virtualCity-panel__empty virtualCity-calib__empty">
          <span className="virtualCity-calib__empty-icon" aria-hidden="true">
            🧪
          </span>
          <p>{t('virtualCity.calib.empty' as TKey)}</p>
        </div>
      </div>
    );
  }

  const invariants = calibration.invariants ?? [];
  const redlines = calibration.redlines ?? [];
  const history = calibration.history ?? [];
  const ind = calibration.indicators ?? {};

  // ── ⑥ 运行历史数据（批次 72，均为可选键：空 runs / 缺 category_annualized
  //    的批次 71 老对局 ⇒ 空态 / '—'，不得报错、不得白屏）────────────────
  const runs = calibration.runs ?? [];
  // 趋势数据取时间序：契约 runs 为新→旧，逆序得旧→新。
  const runsChrono = [...runs].reverse();
  const d2Series = runsChrono.map((r) => r.money_expansion);
  const d5Series = runsChrono.map((r) => r.bankruptcy_rate);
  const annualized = calibration.category_annualized;

  // 策略标签：i18n 优先；未命中**回落原字符串**，绝不渲染空白（§8.4）。
  const strategyLabel = (s: string): string => {
    const key = CALIBRATION_STRATEGY_KEYS[s];
    if (key) return t(key as TKey);
    return s;
  };

  // 品类标签：复用既有资产/宏观标签键；未命中回落原始键字符串。
  const categoryLabel = (c: string): string => {
    const key = CALIBRATION_CATEGORY_KEYS[c];
    if (key) return t(key as TKey);
    return c;
  };

  // 品类年化行（固定 4 行顺序；值缺失 = '—'，缺失 ≠ 0 三态纪律）。
  const annualRows = CALIBRATION_CATEGORY_ORDER.map((cat) => {
    const v = annualized?.[cat];
    const finite = typeof v === 'number' && Number.isFinite(v);
    return { cat, value: finite ? (v as number) : null };
  });

  // 收益类涨跌配色：house 规则（virtualCity.css:1343）⇒ pos/neg 组，
  // **不**用价格类 up/down 组（同 UI 块内口径自洽）。
  const annualClass = (v: number | null): string => {
    if (v !== null && v > 0)
      return 'vc-calib-runs__annualized-val virtualCity-num--pos';
    if (v !== null && v < 0)
      return 'vc-calib-runs__annualized-val virtualCity-num--neg';
    return 'vc-calib-runs__annualized-val';
  };

  // 值已是**小数年化**（0.081 ⇒ "8.1%"）⇒ formatPct 的 ×100 即唯一一次换算。
  const annualText = (v: number | null): string => {
    if (v === null) return '—';
    return `${v > 0 ? '+' : ''}${formatPct(v, 1)}`;
  };

  // 汇总状态：有任一不变量失败 ⇒ fail；否则 ok（空态由上面的早退分支给出 empty）。
  const calibStatus = invariants.some((inv) => !inv.passed) ? 'fail' : 'ok';

  // ── ② 不变量名称：i18n 优先，未知 id 回退后端 name（绝不裸 undefined）──
  const invariantName = (inv: VirtualCityCalibrationInvariant): string => {
    const key = CALIBRATION_INVARIANT_KEYS[inv.id];
    if (key) return t(key as TKey);
    return inv.name || inv.id;
  };

  // ── ④ 红线名称：同上 ──────────────────────────────────────────────────
  const redlineName = (rl: VirtualCityCalibrationRedline): string => {
    const key = CALIBRATION_REDLINE_KEYS[rl.id];
    if (key) return t(key as TKey);
    return rl.name || rl.id;
  };

  // ── ③ D 指标（口径各异：倍数 / 小数 / 指数 / 金额 / 计数，不可统一格式化）──
  const metrics: Array<{ key: string; labelKey: string; value: string }> = [
    {
      key: 'money_expansion',
      labelKey: 'virtualCity.calib.metric.moneyExpansion',
      value: ind.money_expansion === null || ind.money_expansion === undefined
        ? '—'
        : `${num(ind.money_expansion, 2)}×`,
    },
    {
      key: 'bankruptcy_rate',
      labelKey: 'virtualCity.calib.metric.bankruptcyRate',
      value: formatPct(ind.bankruptcy_rate, 1),
    },
    {
      key: 'gini',
      labelKey: 'virtualCity.calib.metric.gini',
      value: num(ind.gini, 3),
    },
    {
      key: 'unemployment',
      labelKey: 'virtualCity.calib.metric.unemployment',
      value: formatPct(ind.unemployment, 1),
    },
    {
      key: 'cpi',
      labelKey: 'virtualCity.calib.metric.cpi',
      value: num(ind.cpi, 3),
    },
    {
      key: 'total_wealth',
      labelKey: 'virtualCity.calib.metric.totalWealth',
      value: formatCny(ind.total_wealth),
    },
    {
      key: 'alive_seats',
      labelKey: 'virtualCity.calib.metric.aliveSeats',
      value: cnt(ind.alive_seats),
    },
    {
      key: 'total_seats',
      labelKey: 'virtualCity.calib.metric.totalSeats',
      value: cnt(ind.total_seats),
    },
    {
      key: 'off_ledger_cny',
      labelKey: 'virtualCity.calib.metric.offLedger',
      value: formatCny(ind.off_ledger_cny),
    },
  ];

  return (
    <div
      className="virtualCity-calib"
      data-testid="virtualCity-calib"
      data-vc-calibration-panel=""
      data-vc-calib-invariant-count={String(invariants.length)}
      data-vc-calib-status={calibStatus}
      title={t('virtualCity.calib.title' as TKey)}
    >
      {/* ① 状态哈希 */}
      <section className="virtualCity-calib__block">
        <div className="virtualCity-calib__block-title">
          {t('virtualCity.calib.stateHash' as TKey)}
        </div>
        <div className="virtualCity-calib__hash-row">
          <code className="virtualCity-calib__hash" data-testid="virtualCity-calib-hash">
            {calibration.state_hash || '—'}
          </code>
          <button
            type="button"
            className="virtualCity-calib__copy"
            onClick={() => void handleCopy()}
            disabled={!calibration.state_hash}
            data-testid="virtualCity-calib-copy"
          >
            {copied
              ? t('virtualCity.calib.copied' as TKey)
              : t('virtualCity.calib.copy' as TKey)}
          </button>
        </div>
        <div className="virtualCity-calib__meta">
          {t('virtualCity.month' as TKey, { m: calibration.month })}
        </div>
      </section>

      {/* ② 回归不变量 */}
      <section className="virtualCity-calib__block">
        <div className="virtualCity-calib__block-title">
          {t('virtualCity.calib.invariants' as TKey)}
        </div>
        {invariants.length === 0 ? (
          <div className="virtualCity-calib__none">—</div>
        ) : (
          <ul className="virtualCity-calib__list">
            {invariants.map((inv) => {
              // I6 无样本（批次 72）：passed && no_sample ⇒ 灰色「无样本」徽章
              // （颜色 + 文案双通道，既不是 ✔ 也不是 ✘）。
              const noSample = inv.passed && inv.no_sample === true;
              return (
              <li className="virtualCity-calib__row" key={inv.id}>
                <span
                  className={
                    'virtualCity-calib__flag virtualCity-calib__flag--' +
                    (noSample ? 'na' : inv.passed ? 'ok' : 'bad')
                  }
                  data-testid={`virtualCity-calib-inv-${inv.id}`}
                  data-vc-calib-no-sample={inv.no_sample === true ? '1' : '0'}
                >
                  <span className="virtualCity-calib__flag-icon" aria-hidden="true">
                    {noSample ? '—' : inv.passed ? '✔' : '✘'}
                  </span>
                  <span className="virtualCity-calib__flag-text">
                    {noSample
                      ? t('virtualCity.calibration.noSample' as TKey)
                      : inv.passed
                        ? t('virtualCity.calib.pass' as TKey)
                        : t('virtualCity.calib.fail' as TKey)}
                  </span>
                </span>
                <span className="virtualCity-calib__row-main">
                  <span className="virtualCity-calib__row-title">
                    <span className="virtualCity-calib__row-id">{inv.id}</span>
                    {invariantName(inv)}
                  </span>
                  <span className="virtualCity-calib__row-detail">{inv.detail}</span>
                </span>
                <span className="virtualCity-calib__row-num">
                  {num(inv.actual, 2)} → {num(inv.expected, 2)}
                </span>
                <span
                  className="virtualCity-calib__sev"
                  title={inv.severity}
                  data-testid={`virtualCity-calib-sev-${inv.id}`}
                >
                  {inv.severity}
                </span>
              </li>
              );
            })}
          </ul>
        )}
      </section>

      {/* ③ 校准指标（D 指标） */}
      <section className="virtualCity-calib__block">
        <div className="virtualCity-calib__block-title">
          {t('virtualCity.calib.indicators' as TKey)}
        </div>
        <div className="virtualCity-calib__grid">
          {metrics.map((m) => (
            <div className="virtualCity-calib__item" key={m.key}>
              <span className="virtualCity-calib__item-label">
                {t(m.labelKey as TKey)}
              </span>
              <span className="virtualCity-calib__item-value">{m.value}</span>
            </div>
          ))}
        </div>
      </section>

      {/* ④ 红线告警 */}
      <section className="virtualCity-calib__block">
        <div className="virtualCity-calib__block-title">
          {t('virtualCity.calib.redlines' as TKey)}
        </div>
        {redlines.length === 0 ? (
          <div className="virtualCity-calib__none">—</div>
        ) : (
          <ul className="virtualCity-calib__list">
            {redlines.map((rl) => (
              <li
                className={
                  'virtualCity-calib__row virtualCity-calib__row--redline' +
                  (rl.triggered ? ' virtualCity-calib__row--triggered' : '')
                }
                key={rl.id}
                data-testid={`virtualCity-calib-redline-${rl.id}`}
              >
                <span
                  className={
                    'virtualCity-calib__flag virtualCity-calib__flag--' +
                    (rl.triggered ? 'bad' : 'ok')
                  }
                >
                  <span className="virtualCity-calib__flag-icon" aria-hidden="true">
                    {rl.triggered ? '✘' : '✔'}
                  </span>
                  <span className="virtualCity-calib__flag-text">
                    {rl.triggered
                      ? t('virtualCity.calib.fail' as TKey)
                      : t('virtualCity.calib.pass' as TKey)}
                  </span>
                </span>
                <span className="virtualCity-calib__row-main">
                  <span className="virtualCity-calib__row-title">
                    <span className="virtualCity-calib__row-id">{rl.id}</span>
                    {redlineName(rl)}
                  </span>
                  <span className="virtualCity-calib__row-detail">{rl.detail}</span>
                </span>
                <span className="virtualCity-calib__row-num">
                  {num(rl.actual, 2)} / {num(rl.threshold, 2)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      {/* ⑤ 历史审计 */}
      <section className="virtualCity-calib__block">
        <div className="virtualCity-calib__block-title">
          {t('virtualCity.calib.history' as TKey)}
        </div>
        {history.length === 0 ? (
          <div className="virtualCity-calib__none">—</div>
        ) : (
          <>
            <div className="virtualCity-calib__hist" data-testid="virtualCity-calib-hist">
              {history.map((h) => (
                <span
                  key={`${h.month}-${h.hash}`}
                  className={
                    'virtualCity-calib__hist-cell virtualCity-calib__hist-cell--' +
                    (h.ok ? 'ok' : 'bad')
                  }
                  title={`M${h.month} ${h.hash}`}
                >
                  <span className="virtualCity-calib__hist-icon" aria-hidden="true">
                    {h.ok ? '✔' : '✘'}
                  </span>
                </span>
              ))}
            </div>
            <ul className="virtualCity-calib__list">
              {history.map((h) => (
                <li className="virtualCity-calib__hist-row" key={`row-${h.month}-${h.hash}`}>
                  <span className="virtualCity-calib__hist-month">M{h.month}</span>
                  <code className="virtualCity-calib__hist-hash">{h.hash || '—'}</code>
                  <span className="virtualCity-calib__flag-text">
                    {h.ok
                      ? t('virtualCity.calib.pass' as TKey)
                      : t('virtualCity.calib.fail' as TKey)}
                  </span>
                </li>
              ))}
            </ul>
          </>
        )}
      </section>

      {/* ⑥ 运行历史（批次 72）：运行表 + D2/D5 独立 sparkline + 品类年化 */}
      <section
        className="virtualCity-calib__block vc-calib-runs"
        data-testid="virtualCity-calib-runs"
        data-vc-calib-runs-count={String(runs.length)}
      >
        <div className="virtualCity-calib__block-title">
          {t('virtualCity.calibration.runsTitle' as TKey)}
        </div>

        {runs.length === 0 ? (
          /* 空态：本局未终局或进程重启（真实对局无 MC 局时的预期形态）。 */
          <div className="vc-calib-runs__empty">
            {t('virtualCity.calibration.runsEmpty' as TKey)}
          </div>
        ) : (
          <>
            {/* 趋势小倍图：D2 与 D5 各自独立 y 标尺（0–7× vs 0–1 量纲不同，
                禁止合成双轴）；数据 = runs 逆序（旧→新），单点退化为短横线。 */}
            <div className="vc-calib-runs__sub-title">
              {t('virtualCity.calibration.trendTitle' as TKey)}
            </div>
            <div className="vc-calib-runs__trend-grid">
              <figure className="vc-calib-runs__trend">
                <figcaption className="vc-calib-runs__trend-title">
                  {t('virtualCity.calibration.trendD2' as TKey)}
                  <span className="vc-calib-runs__trend-value">
                    {Number.isFinite(runs[0].money_expansion)
                      ? `${num(runs[0].money_expansion, 2)}×`
                      : '—'}
                  </span>
                </figcaption>
                <RunSparkline
                  values={d2Series}
                  lineClass="vc-calib-runs__spark-line--d2"
                />
              </figure>
              <figure className="vc-calib-runs__trend">
                <figcaption className="vc-calib-runs__trend-title">
                  {t('virtualCity.calibration.trendD5' as TKey)}
                  <span className="vc-calib-runs__trend-value">
                    {formatPct(runs[0].bankruptcy_rate, 1)}
                  </span>
                </figcaption>
                <RunSparkline
                  values={d5Series}
                  lineClass="vc-calib-runs__spark-line--d5"
                />
              </figure>
            </div>

            {/* 运行表（最近 12 局，新→旧；趋势数据的 table view 可访问性兜底；
                行 title 消费 finished_at / outcome_counts / state_hash）。 */}
            <div className="vc-calib-runs__table-wrap">
              <table
                className="vc-calib-runs__table"
                data-testid="virtualCity-calib-runs-table"
              >
                <thead>
                  <tr>
                    <th className="vc-calib-runs__th">
                      {t('virtualCity.calibration.colRun' as TKey)}
                    </th>
                    <th className="vc-calib-runs__th">
                      {t('virtualCity.calibration.colSeed' as TKey)}
                    </th>
                    <th className="vc-calib-runs__th">
                      {t('virtualCity.calibration.colStrategy' as TKey)}
                    </th>
                    <th className="vc-calib-runs__th vc-calib-runs__th--num">
                      {t('virtualCity.calibration.colSteps' as TKey)}
                    </th>
                    <th className="vc-calib-runs__th vc-calib-runs__th--num">
                      {t('virtualCity.calibration.colD2' as TKey)}
                    </th>
                    <th className="vc-calib-runs__th vc-calib-runs__th--num">
                      {t('virtualCity.calibration.colD5' as TKey)}
                    </th>
                    <th className="vc-calib-runs__th vc-calib-runs__th--num">
                      {t('virtualCity.calibration.colInvFails' as TKey)}
                    </th>
                    <th className="vc-calib-runs__th vc-calib-runs__th--num">
                      {t('virtualCity.calibration.colRedlines' as TKey)}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {runs.map((r) => (
                    <tr
                      className="vc-calib-runs__tr"
                      key={r.run_id || `run-${r.seed}-${r.finished_at}`}
                      title={runTitle(r)}
                    >
                      <td className="vc-calib-runs__td">{r.run_id || '—'}</td>
                      <td className="vc-calib-runs__td vc-calib-runs__td--mono">
                        {cnt(r.seed)}
                      </td>
                      <td className="vc-calib-runs__td">
                        {strategyLabel(r.strategy ?? '')}
                      </td>
                      <td className="vc-calib-runs__td vc-calib-runs__td--num">
                        {cnt(r.total_steps)}
                      </td>
                      <td className="vc-calib-runs__td vc-calib-runs__td--num">
                        {Number.isFinite(r.money_expansion)
                          ? `${num(r.money_expansion, 2)}×`
                          : '—'}
                      </td>
                      <td className="vc-calib-runs__td vc-calib-runs__td--num">
                        {formatPct(r.bankruptcy_rate, 1)}
                      </td>
                      <td className="vc-calib-runs__td vc-calib-runs__td--num">
                        {cnt((r.invariant_fails ?? []).length)}
                      </td>
                      <td className="vc-calib-runs__td vc-calib-runs__td--num">
                        {cnt((r.redlines_triggered ?? []).length)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}

        {/* 品类年化（市场级口径，R-E1/R-E2 判定输入；值为小数年化，
            formatPct 的 ×100 即唯一一次换算；老对局缺省 ⇒ 4 行 '—'）。 */}
        <div className="vc-calib-runs__annualized">
          <div className="vc-calib-runs__sub-title">
            {t('virtualCity.calibration.categoryAnnualized' as TKey)}
          </div>
          <ul className="vc-calib-runs__annualized-list">
            {annualRows.map(({ cat, value }) => (
              <li className="vc-calib-runs__annualized-row" key={cat}>
                <span className="vc-calib-runs__annualized-name">
                  {categoryLabel(cat)}
                </span>
                <span className={annualClass(value)}>{annualText(value)}</span>
              </li>
            ))}
          </ul>
        </div>
      </section>
    </div>
  );
}

export default CalibrationPanel;
