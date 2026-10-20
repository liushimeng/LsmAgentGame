/**
 * BehaviorPanel — 批次 62「居民行为金融与泡沫反身性引擎」本人行为面板。
 *
 * 权威来源：
 *   - 设计：`lag_docs/虚拟城市/已实现/62-居民行为金融与泡沫反身性引擎/01-方案设计.md`
 *   - 后端产出：`ServerGo/game/virtual_city/bias.go` / `bias_settle.go`（`behaviorJSONFor`）
 *
 * 面板内容（对应设计 §3）：
 *   ① 认知点 + 心智状态
 *   ② 我的倾向（心魔画像）—— **未识破的偏差后端已置空 name_cn/effect/explanation，
 *      前端只显示占位文案，绝不用 code 反查中文名**（教学闭环不变量）
 *   ③ 偏差免疫进度（≥80% ⇒ 已免疫）
 *   ④ 四个指数指标（锚定 / 羊群 / 连续储蓄 / 延迟满足）
 *   ⑤ 心理账户（按标签分组 + 合并账户）
 *   ⑥ 延期支付（到期月 / 罚息率 / 逾期红显）
 *   ⑦ 主动反思三档（轻度 −1 / 深度 −2 / 预习 −1；每回合 1 次，认知 ≤1 不可用）
 *
 * 错误处理（§7.1）：动作失败在**面板内联红条**展示（最高层级），不吞进 console。
 * 观战 / 全 Agent 模式（my.behavior 缺失）⇒ 只读空态。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import type {
  VirtualCityBehaviorSnapshot,
  VirtualCityReflectMode,
} from '@/types/virtualCityBehavior';
import {
  formatCny,
  WEALTH_BEHAVIOR_ACTIONS,
  type VirtualCityAction,
} from '@/types/virtualCity';

const AWAIT_TIMEOUT_MS = 8000;

interface Props {
  /** game.state.my.behavior；null = 观战 / 全 Agent 模式（渲染只读空态）。 */
  behavior: VirtualCityBehaviorSnapshot | null;
  /** 观战 / 全 Agent 模式 → 只读，隐藏全部动作按钮。 */
  spectator: boolean;
  /** 发送 game.virtual_city_action（useVirtualCity.sendAction 注入）。 */
  onAction: (action: VirtualCityAction) => void;
}

/** 反思档位 → 展示顺序（设计 §4.3）。 */
const REFLECT_MODES: VirtualCityReflectMode[] = ['light', 'deep', 'preview'];

/**
 * 动作元数据查表（§130：WEALTH_BEHAVIOR_ACTIONS 必须真的被消费，不能只是声明）。
 * 三个批次62 动作的图标与文案统一来自这张表，避免面板里再写一份。
 */
const ACTION_META = {
  reflect: WEALTH_BEHAVIOR_ACTIONS.find((a) => a.type === 'reflect'),
  defer_payment: WEALTH_BEHAVIOR_ACTIONS.find((a) => a.type === 'defer_payment'),
  merge_accounts: WEALTH_BEHAVIOR_ACTIONS.find((a) => a.type === 'merge_accounts'),
} as const;

/** 反思档位 → i18n 键（按钮名 / 消耗说明）。 */
const REFLECT_LABEL: Record<VirtualCityReflectMode, string> = {
  light: 'virtualCity.behavior.reflectLight',
  deep: 'virtualCity.behavior.reflectDeep',
  preview: 'virtualCity.behavior.reflectPreview',
};
const REFLECT_COST: Record<VirtualCityReflectMode, string> = {
  light: 'virtualCity.behavior.reflectCostLight',
  deep: 'virtualCity.behavior.reflectCostDeep',
  preview: 'virtualCity.behavior.reflectCostPreview',
};

/** 百分比文案（0.42 → 42%）。 */
function pct(v: number): string {
  return `${Math.round(v * 100)}%`;
}

/** 偏差 code → 展示用兜底名（**仅在已识破且后端给了名字时使用**；
 *  未识破时后端 name_cn 为空 ⇒ 这里也返回空，前端再落到占位文案）。 */
function biasName(b: { name_cn: string }): string {
  return b.name_cn;
}

export function BehaviorPanel({ behavior, spectator, onAction }: Props) {
  const t = useT();
  const mySeat = useVirtualCityStore((s) => s.mySeat);
  const [error, setError] = useState<string | null>(null);
  const [awaiting, setAwaiting] = useState(false);
  /** 延期支付金额输入（批次62 §3.3.1）。 */
  const [deferAmount, setDeferAmount] = useState('');

  const awaitingRef = useRef(false);
  awaitingRef.current = awaiting;
  const mySeatRef = useRef(mySeat);
  mySeatRef.current = mySeat;

  // 后端错误码（批次62 errcode.go 35047–35052）→ 友好文案。
  const mapError = useCallback(
    (code: number, message: string): string => {
      switch (code) {
        case 35047:
          return t('virtualCity.behavior.reflectCognitionLow' as TKey);
        case 35048:
          return t('virtualCity.behavior.reflectLimit' as TKey);
        case 35049:
          return t('virtualCity.behavior.reflectModeInvalid' as TKey);
        case 35050:
          return t('virtualCity.behavior.reflectNothingToResolve' as TKey);
        case 35051:
          return t('virtualCity.behavior.deferNotAvailable' as TKey);
        case 35052:
          return t('virtualCity.behavior.mentalAlreadyMerged' as TKey);
        case 35007:
          return t('virtualCity.error.cash' as TKey);
        default:
          return message || t('virtualCity.error.generic' as TKey);
      }
    },
    [t],
  );

  useEffect(() => {
    const off = wsClient.on((env: WsEnvelope) => {
      if (!awaitingRef.current) return;
      if (env.type === 'game.event') {
        const p = env.payload as { seat?: number; type?: string; text?: string };
        if (p.type === 'error') {
          setError(p.text || t('virtualCity.error.generic' as TKey));
          setAwaiting(false);
          return;
        }
        if (p.seat === mySeatRef.current && p.type === 'action') {
          setAwaiting(false);
          setError(null);
        }
      } else if (env.type === 'game.error') {
        const p = env.payload as { code: number; message: string };
        setError(mapError(p.code, p.message));
        setAwaiting(false);
      }
    });
    return () => off();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t, mapError]);

  useEffect(() => {
    if (!awaiting) return;
    const timer = window.setTimeout(() => {
      setError(t('virtualCity.error.timeout' as TKey));
      setAwaiting(false);
    }, AWAIT_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [awaiting, t]);

  const fire = useCallback(
    (action: VirtualCityAction) => {
      setError(null);
      awaitingRef.current = true;
      setAwaiting(true);
      onAction(action);
    },
    [onAction],
  );

  // 只读空态：观战 / 全 Agent 模式。
  if (!behavior) {
    return (
      <div className="virtualCity-behavior" data-testid="virtualCity-behavior-panel">
        <div className="virtualCity-behavior__head">
          <span className="virtualCity-behavior__title">
            🧠 {t('virtualCity.behavior.title' as TKey)}
          </span>
        </div>
        {/* 批次 63 L2：空态改「带说明的中性占位块」（图标 + 两行说明），
            与面板其它 section 同宽同内边距，避免观战态下整块空白。 */}
        <div className="virtualCity-panel__empty virtualCity-behavior__empty">
          <span className="virtualCity-behavior__empty-icon" aria-hidden="true">🧠</span>
          <p>{t('virtualCity.behavior.empty' as TKey)}</p>
          {spectator && (
            <p className="virtualCity-behavior__hint">
              👁 {t('virtualCity.behavior.spectatorHint' as TKey)}
            </p>
          )}
        </div>
      </div>
    );
  }

  const reflectDone = behavior.last_reflect && behavior.last_reflect !== 'none';
  const reflectDisabled = awaiting || reflectDone || behavior.cognition <= 1;
  const merged = behavior.mental_account_merged;
  /** 存在「双曲贴现」倾向（只看 code，不看 name_cn ⇒ 不泄漏名称与效果）。 */
  const hasHyperbolicTendency = behavior.bias_profile.some((b) => b.code === 'hyperbolic_discount');
  const deferAmountValid = Number(deferAmount) >= 1 && Number.isFinite(Number(deferAmount));
  const immunityByCode = new Map(behavior.bias_immunity.map((i) => [i.code, i.progress]));

  return (
    <div className="virtualCity-behavior" data-testid="virtualCity-behavior-panel">
      <div className="virtualCity-behavior__head">
        <span className="virtualCity-behavior__title">
          🧠 {t('virtualCity.behavior.title' as TKey)}
        </span>
        <span className="virtualCity-behavior__cognition">
          <span className="virtualCity-behavior__cognition-label">
            {t('virtualCity.behavior.cognition' as TKey)}
          </span>
          <span className="virtualCity-behavior__cognition-value">{behavior.cognition}</span>
        </span>
      </div>

      {spectator && (
        <p className="virtualCity-behavior__hint" data-testid="virtualCity-behavior-spectator">
          👁 {t('virtualCity.behavior.spectatorHint' as TKey)}
        </p>
      )}

      {/* ① 认知点说明 */}
      <div className="virtualCity-behavior__section">
        <p className="virtualCity-behavior__cognition-hint">
          {t('virtualCity.behavior.cognitionHint' as TKey)}
        </p>
      </div>

      {/* ② 我的倾向 */}
      <div className="virtualCity-behavior__section">
        <span className="virtualCity-behavior__section-title">
          {t('virtualCity.behavior.biasTitle' as TKey)}
        </span>
        {behavior.bias_profile.length === 0 ? (
          <p className="virtualCity-behavior__empty">
            {t('virtualCity.behavior.biasEmpty' as TKey)}
          </p>
        ) : (
          <ul className="virtualCity-behavior__bias-list">
            {behavior.bias_profile.map((b) => {
              const name = biasName(b);
              const known = !!name;
              const immunity = immunityByCode.get(b.code) ?? 0;
              const immune = immunity >= 0.8;
              return (
                <li
                  key={b.code}
                  className={
                    'virtualCity-behavior__bias' +
                    (known ? ' virtualCity-behavior__bias--detected' : ' virtualCity-behavior__bias--unknown') +
                    (immune ? ' virtualCity-behavior__bias--immune' : '')
                  }
                  data-testid={`virtualCity-behavior-bias-${b.code}`}
                >
                  <div className="virtualCity-behavior__bias-head">
                    <span className="virtualCity-behavior__bias-name">
                      {known
                        ? name
                        : t('virtualCity.behavior.biasUnknown' as TKey)}
                    </span>
                    <span
                      className={
                        'virtualCity-behavior__bias-badge' +
                        (known ? ' virtualCity-behavior__bias-badge--detected' : '')
                      }
                    >
                      {immune
                        ? t('virtualCity.behavior.biasImmune' as TKey)
                        : known
                          ? t('virtualCity.behavior.biasDetected' as TKey)
                          : t('virtualCity.behavior.biasUndetected' as TKey)}
                    </span>
                  </div>
                  {/* 未识破时 effect / explanation 后端已置空 ⇒ 不渲染任何效果描述 */}
                  {known && b.effect && (
                    <span className="virtualCity-behavior__bias-meta">
                      {t('virtualCity.behavior.biasEffect' as TKey)}：{b.effect} ·{' '}
                      {t('virtualCity.behavior.biasThreshold' as TKey)} {b.threshold}
                    </span>
                  )}
                  {known && b.explanation && (
                    <p className="virtualCity-behavior__bias-explain">{b.explanation}</p>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {/* ③ 偏差免疫进度 */}
      <div className="virtualCity-behavior__section">
        <span className="virtualCity-behavior__section-title">
          {t('virtualCity.behavior.immunityTitle' as TKey)}
        </span>
        {behavior.bias_immunity.length === 0 ? (
          <p className="virtualCity-behavior__empty">
            {t('virtualCity.behavior.immunityEmpty' as TKey)}
          </p>
        ) : (
          <ul className="virtualCity-behavior__immunity-list">
            {behavior.bias_immunity.map((im) => {
              const entry = behavior.bias_profile.find((b) => b.code === im.code);
              const label = entry ? biasName(entry) : '';
              const ok = im.progress >= 0.8;
              return (
                <li className="virtualCity-behavior__immunity-row" key={im.code}>
                  <span className="virtualCity-behavior__immunity-name">
                    {label || t('virtualCity.behavior.biasUnknown' as TKey)}
                  </span>
                  <span className="virtualCity-behavior__bar">
                    <span
                      className={
                        'virtualCity-behavior__bar-fill' + (ok ? ' virtualCity-behavior__bar-fill--ok' : '')
                      }
                      style={{ width: `${Math.min(100, Math.round(im.progress * 100))}%` }}
                    />
                  </span>
                  <span className="virtualCity-behavior__bar-pct">{pct(im.progress)}</span>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {/* ④ 四个指数指标 */}
      <div className="virtualCity-behavior__metrics">
        <Metric
          label={t('virtualCity.behavior.metricAnchoring' as TKey)}
          value={behavior.anchoring_index.toFixed(2)}
          hint={t('virtualCity.behavior.metricAnchoringHint' as TKey)}
        />
        <Metric
          label={t('virtualCity.behavior.metricHerding' as TKey)}
          value={pct(behavior.herding_index)}
          hint={t('virtualCity.behavior.metricHerdingHint' as TKey)}
        />
        <Metric
          label={t('virtualCity.behavior.metricSavings' as TKey)}
          value={`${behavior.savings_streak_months}`}
          hint={t('virtualCity.behavior.metricSavingsHint' as TKey)}
        />
        <Metric
          label={t('virtualCity.behavior.metricDelayed' as TKey)}
          value={pct(behavior.delayed_pay_index)}
          hint={t('virtualCity.behavior.metricDelayedHint' as TKey)}
        />
      </div>

      {/* ⑤ 心理账户 */}
      <div className="virtualCity-behavior__section">
        <span className="virtualCity-behavior__section-title">
          {t('virtualCity.behavior.mentalTitle' as TKey)}
          {merged && ` · ${t('virtualCity.behavior.mentalMerged' as TKey)}`}
        </span>
        {behavior.mental_accounts.length === 0 ? (
          <p className="virtualCity-behavior__empty">
            {t('virtualCity.behavior.mentalEmpty' as TKey)}
          </p>
        ) : (
          <ul className="virtualCity-behavior__mental-list">
            {behavior.mental_accounts.map((m) => (
              <li className="virtualCity-behavior__mental-row" key={m.tag}>
                <span>{m.label_cn || m.tag}</span>
                <b>¥{formatCny(m.amount_cny)}</b>
              </li>
            ))}
          </ul>
        )}
        <p className="virtualCity-behavior__mental-hint">
          {t('virtualCity.behavior.mentalMergeHint' as TKey)}
        </p>
        {!spectator && !merged && (
          <button
            type="button"
            className="btn btn-secondary"
            disabled={awaiting}
            data-testid="virtualCity-behavior-merge-accounts"
            onClick={() => fire({ type: 'merge_accounts' })}
          >
            {ACTION_META.merge_accounts?.icon ?? '🧺'}{' '}
            {t('virtualCity.behavior.mentalMergeBtn' as TKey)}
          </button>
        )}
      </div>

      {/* ⑥ 延期支付 */}
      <div className="virtualCity-behavior__section">
        <span className="virtualCity-behavior__section-title">
          {t('virtualCity.behavior.deferTitle' as TKey)}
        </span>
        {behavior.deferred_payments.length === 0 ? (
          <p className="virtualCity-behavior__empty">
            {t('virtualCity.behavior.deferEmpty' as TKey)}
          </p>
        ) : (
          <ul className="virtualCity-behavior__defer-list">
            {behavior.deferred_payments.map((d, i) => (
              <li
                className="virtualCity-behavior__defer-row"
                key={`${d.due_month}-${i}`}
                data-testid="virtualCity-behavior-defer-row"
              >
                <span>
                  ¥{formatCny(d.amount_cny)}
                  <span className="virtualCity-behavior__defer-sub">
                    {' '}
                    · {t('virtualCity.behavior.deferDue' as TKey, { month: d.due_month })} ·{' '}
                    {t('virtualCity.behavior.deferPenalty' as TKey, {
                      pct: `×${(1 + d.penalty_rate).toFixed(1)}`,
                    })}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        )}
        {/* 延期支付入口：仅当存在「双曲贴现」倾向时可用。
            ⚠ 判据用**后端下发的 code**（code 在快照里恒在，与 name_cn 是否置空无关），
            这里只用它决定「按钮是否出现」，**不展示名称与效果** ⇒ 不破坏教学闭环。 */}
        {!spectator && hasHyperbolicTendency && (
          <>
            <label className="virtualCity-behavior__defer-label" htmlFor="virtualCity-behavior-defer-amount">
              {t('virtualCity.behavior.deferBtn' as TKey)}（¥）
            </label>
            <input
              id="virtualCity-behavior-defer-amount"
              className="virtualCity-behavior__defer-input"
              type="number"
              min={1}
              step={100}
              inputMode="numeric"
              value={deferAmount}
              placeholder="1000"
              disabled={awaiting}
              data-testid="virtualCity-behavior-defer-input"
              onChange={(e) => setDeferAmount(e.target.value)}
            />
            <button
              type="button"
              className="btn btn-secondary"
              disabled={awaiting || !deferAmountValid}
              title={t('virtualCity.behavior.deferHint' as TKey)}
              data-testid="virtualCity-behavior-defer-btn"
              onClick={() => {
                const amt = Math.floor(Number(deferAmount));
                if (!amt || amt < 1) return;
                setDeferAmount('');
                fire({ type: 'defer_payment', amount_cny: amt });
              }}
            >
              {ACTION_META.defer_payment?.icon ?? '⏳'}{' '}
              {t('virtualCity.behavior.deferBtn' as TKey)}
            </button>
            <p className="virtualCity-behavior__mental-hint">
              {t('virtualCity.behavior.deferHint' as TKey)}
            </p>
          </>
        )}
      </div>

      {/* ⑦ 主动反思 */}
      {!spectator && (
        <div className="virtualCity-behavior__section">
          <span className="virtualCity-behavior__section-title">
            {t('virtualCity.behavior.reflectTitle' as TKey)}
          </span>
          <div className="virtualCity-behavior__reflect-btns">
            {REFLECT_MODES.map((mode) => (
              <button
                key={mode}
                type="button"
                className="virtualCity-behavior__reflect-btn"
                disabled={reflectDisabled}
                title={
                  reflectDone
                    ? t('virtualCity.behavior.reflectDone' as TKey)
                    : t('virtualCity.behavior.reflectLimit' as TKey)
                }
                data-testid={`virtualCity-behavior-reflect-${mode}`}
                onClick={() => fire({ type: 'reflect', reflect_mode: mode })}
              >
                <span>
                  {ACTION_META.reflect?.icon ?? '🧘'} {t(REFLECT_LABEL[mode] as TKey)}
                </span>
                <span className="virtualCity-behavior__reflect-cost">
                  {t(REFLECT_COST[mode] as TKey)}
                </span>
              </button>
            ))}
          </div>
          <p className="virtualCity-behavior__reflect-note">
            {reflectDone
              ? t('virtualCity.behavior.reflectDone' as TKey)
              : t('virtualCity.behavior.reflectLimit' as TKey)}
          </p>
        </div>
      )}

      {error && (
        <div className="virtualCity-behavior__error" role="alert">
          ⚠️ {error}
        </div>
      )}
    </div>
  );
}

/** 指标卡（label + value + 一句话说明）。 */
function Metric({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="virtualCity-behavior__metric">
      <span className="virtualCity-behavior__metric-label">{label}</span>
      <span className="virtualCity-behavior__metric-value">{value}</span>
      <span className="virtualCity-behavior__metric-hint">{hint}</span>
    </div>
  );
}

export default BehaviorPanel;
