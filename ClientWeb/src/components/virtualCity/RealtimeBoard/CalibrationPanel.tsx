/**
 * CalibrationPanel — 批次 71 看板第 12 维度「校准」。
 *
 * 定位：把「这座城市跑得对不对、稳不稳、可不可复现」变成**可判定的事实**，
 * 而不是文档里的承诺（方案 §2.1 G6 / §8.2）。
 *
 * 五个区块（方案 §8.2）：
 *   ① 状态哈希   —— 当前月 `state_hash`（等宽 + 可复制）+ 所属月
 *   ② 不变量列表 —— 7 条 I1/I2/I3/I4/I5/I6/I8：✔/✘ + 实测值 + 说明
 *   ③ D 指标     —— D2 货币扩张 / D5 破产率 / Gini / 失业率 / CPI / 存活座位 / 账外净额
 *   ④ 红线       —— 触发（红）与未触发（灰），带实测 vs 阈值
 *   ⑤ 历史       —— 最近 12 月哈希 + 不变量全通过与否（迷你条）
 *
 * 口径纪律：
 *   - **整体可能不存在**（老对局 / 后端未升级）⇒ 走空态「本局尚未产生校准数据」，
 *     绝不白屏、绝不报错（方案 §9.2）。
 *   - **字段缺失 = 「—」**，与「0」区分（三态纪律，同批次 70）。
 *   - **通过 / 失败不得只靠颜色区分**（色弱可达性）⇒ 一律带 ✔ / ✘ 图标 + 文字
 *     （CLAUDE.md §26.2 反模式 ④）。
 *   - **未知枚举值回退后端 `name` 字段**（§12 纪律：映射表漏项不得显示裸 undefined）。
 *   - 百分比口径明确（`formatPct` 是「小数 → 百分比」；D2 是倍数、CPI 是指数，
 *     **不可**套百分比）。
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
  CALIBRATION_INVARIANT_KEYS,
  CALIBRATION_REDLINE_KEYS,
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
            {invariants.map((inv) => (
              <li className="virtualCity-calib__row" key={inv.id}>
                <span
                  className={
                    'virtualCity-calib__flag virtualCity-calib__flag--' +
                    (inv.passed ? 'ok' : 'bad')
                  }
                  data-testid={`virtualCity-calib-inv-${inv.id}`}
                >
                  <span className="virtualCity-calib__flag-icon" aria-hidden="true">
                    {inv.passed ? '✔' : '✘'}
                  </span>
                  <span className="virtualCity-calib__flag-text">
                    {inv.passed
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
            ))}
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
    </div>
  );
}

export default CalibrationPanel;
