/**
 * InsurancePanel — 商业保险面板（P1-4 商业保险与风险转移引擎 §10.1）。
 *
 * 4 险种卡固定顺序（重疾 → 百万医疗 → 定期寿险 → 意外）：
 *   - 未投保 / 已失效：渲染 quotes 当前年龄档报价 + [立即投保]（后端对 lapsed
 *     险种也下发报价，引导重新投保）。
 *   - 持有中：状态徽章（active 绿 / waiting 黄 / grace 橙 / lapsed 灰，§26 ≥5:1）
 *     + 保额 / 年缴 / 月缴 / 已缴月数 / 累计赔付 + [退保]（window.confirm 二次确认）。
 *   - 百万医疗 coverage_cny=0 → 显示「报销 90%」而非「保额 ¥0」（§8.2 特殊语义）。
 *
 * 只读态：观战路由 / 全 Agent 模式（my=null → insurance=null）→ 隐藏全部按钮
 * + 观战提示；insurance=null 时渲染空态引导文本（§10.1 默认空态）。
 *
 * 动作回执（仿 ActionPanel）：awaiting + wsClient.on 监听（refs 防竞态）+ 8s 超时
 * + mapError（35037–35041 → INSURANCE_ERR_I18N；35007 → 现金不足；兜底后端
 * message 直显）。banner / 全局 toast 双通道由 useVirtualCity 已接线（§7.1）。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import {
  formatCny,
  INSURANCE_ERR_I18N,
  WEALTH_INSURANCE_KINDS,
  WEALTH_MEDICAL_REIMBURSE_PCT,
  type VirtualCityInsuranceAction,
  type VirtualCityInsuranceKind,
  type VirtualCityInsurancePolicy,
  type VirtualCityInsuranceQuote,
  type VirtualCityInsuranceState,
} from '@/types/virtualCity';

const AWAIT_TIMEOUT_MS = 8000;

interface Props {
  /** game.state.my.insurance；null = 观战 / 引擎关闭（渲染空态）。 */
  insurance: VirtualCityInsuranceState | null;
  /** 投保按钮禁用态 UX（非安全边界，服务端权威校验 35007）。 */
  myCash: number;
  /** 观战 / 全 Agent 模式 → 只读，隐藏全部按钮。 */
  spectator: boolean;
  /** 发送 game.virtual_city_action（useVirtualCity.sendAction 注入）。 */
  onAction: (action: VirtualCityInsuranceAction) => void;
}

/** 状态徽章类名（§26.3 三件套：JSX 拼接与 virtualCity-insurance.css 规则同提交）。 */
const STATUS_CLASS: Record<string, string> = {
  active: 'virtualCity-insurance__badge--active',
  waiting: 'virtualCity-insurance__badge--waiting',
  grace: 'virtualCity-insurance__badge--grace',
  lapsed: 'virtualCity-insurance__badge--lapsed',
};

/** 状态徽章文案（waiting 带剩余月数 {n}）。 */
function statusLabel(
  t: ReturnType<typeof useT>,
  p: VirtualCityInsurancePolicy,
): string {
  if (p.status === 'waiting') {
    return t('virtualCity.insurance.status.waiting' as TKey, { n: p.waiting_left });
  }
  const key = `virtualCity.insurance.status.${p.status}` as TKey;
  const known = p.status === 'active' || p.status === 'grace' || p.status === 'lapsed';
  return known ? t(key) : t('virtualCity.insurance.status.lapsed' as TKey);
}

/** 保额展示：百万医疗（coverage=0）→「报销 90%」，其余 →「保额 ¥X」。 */
function coverageText(
  t: ReturnType<typeof useT>,
  kind: string,
  coverageCny: number,
): string {
  if (kind === 'medical_million' || coverageCny === 0) {
    return t('virtualCity.insurance.reimburse' as TKey, {
      pct: WEALTH_MEDICAL_REIMBURSE_PCT,
    });
  }
  return `${t('virtualCity.insurance.coverage' as TKey)} ¥${formatCny(coverageCny)}`;
}

export function InsurancePanel({ insurance, myCash, spectator, onAction }: Props) {
  const t = useT();
  const mySeat = useVirtualCityStore((s) => s.mySeat);
  const [error, setError] = useState<string | null>(null);
  const [awaiting, setAwaiting] = useState(false);

  // refs 防监听器闭包竞态（照 ActionPanel：mount 注册一次，经 ref 读最新值）。
  const awaitingRef = useRef(false);
  awaitingRef.current = awaiting;
  const mySeatRef = useRef(mySeat);
  mySeatRef.current = mySeat;

  // mapError 须先于 wsClient.on 监听器声明（闭包引用）。
  const mapError = useCallback(
    (code: number, message: string): string => {
      const key = INSURANCE_ERR_I18N[code];
      if (key) return t(key);
      if (code === 35007) return t('virtualCity.error.cash' as TKey);
      return message || t('virtualCity.error.generic' as TKey);
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
        // 本人动作回执（后端成功后随 game.event 单发 game.state 刷新面板）。
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

  // 超时定时器（awaiting=true 时启动）。
  useEffect(() => {
    if (!awaiting) return;
    const timer = window.setTimeout(() => {
      setError(t('virtualCity.error.timeout' as TKey));
      setAwaiting(false);
    }, AWAIT_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [awaiting, t]);

  const handleBuy = useCallback(
    (kind: VirtualCityInsuranceKind) => {
      setError(null);
      awaitingRef.current = true; // 同步置 ref，防往返极快丢事件（照 ActionPanel fire()）
      setAwaiting(true);
      onAction({ type: 'buy_insurance', kind });
    },
    [onAction],
  );

  const handleCancel = useCallback(
    (kind: VirtualCityInsuranceKind) => {
      // 消费型退保零现金价值，二次确认（§10.1）。
      if (!window.confirm(t('virtualCity.insurance.cancelConfirm' as TKey))) return;
      setError(null);
      awaitingRef.current = true;
      setAwaiting(true);
      onAction({ type: 'cancel_insurance', kind });
    },
    [onAction, t],
  );

  const policyOf = (kind: string): VirtualCityInsurancePolicy | undefined =>
    insurance?.policies.find((p) => p.kind === kind);
  const quoteOf = (kind: string): VirtualCityInsuranceQuote | undefined =>
    insurance?.quotes.find((q) => q.kind === kind);

  // 空态：观战（my=null）/ insurance_enabled=false → 引导文本 + 观战提示。
  if (!insurance) {
    return (
      <div className="virtualCity-insurance" data-testid="virtualCity-insurance-panel">
        <div className="virtualCity-insurance__head">
          <span className="virtualCity-insurance__title">🛡 {t('virtualCity.insurance.title' as TKey)}</span>
        </div>
        <div className="virtualCity-panel__empty">
          <p>{t('virtualCity.insurance.empty' as TKey)}</p>
          {spectator && (
            <p className="virtualCity-insurance__hint">
              👁 {t('virtualCity.insurance.spectatorHint' as TKey)}
            </p>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="virtualCity-insurance" data-testid="virtualCity-insurance-panel">
      <div className="virtualCity-insurance__head">
        <span className="virtualCity-insurance__title">🛡 {t('virtualCity.insurance.title' as TKey)}</span>
        <span className="virtualCity-insurance__monthly">
          {t('virtualCity.insurance.monthlyTotal' as TKey)}{' '}
          <b className="virtualCity-num--neg">¥{formatCny(insurance.monthly_premium)}</b>
        </span>
      </div>
      {spectator && (
        <p className="virtualCity-insurance__hint" data-testid="virtualCity-insurance-spectator">
          👁 {t('virtualCity.insurance.spectatorHint' as TKey)}
        </p>
      )}
      <div className="virtualCity-insurance__grid">
        {WEALTH_INSURANCE_KINDS.map((kind) => (
          <KindCard
            key={kind}
            kind={kind}
            policy={policyOf(kind) ?? null}
            quote={quoteOf(kind) ?? null}
            spectator={spectator}
            awaiting={awaiting}
            myCash={myCash}
            onBuy={handleBuy}
            onCancel={handleCancel}
          />
        ))}
      </div>
      {error && (
        <div className="virtualCity-insurance__error" role="alert">
          ⚠️ {error}
        </div>
      )}
    </div>
  );
}

/** 单险种卡：持有中渲染保单数据 + [退保]；未投保 / 已失效渲染报价 + [立即投保]。 */
function KindCard({
  kind,
  policy,
  quote,
  spectator,
  awaiting,
  myCash,
  onBuy,
  onCancel,
}: {
  kind: VirtualCityInsuranceKind;
  policy: VirtualCityInsurancePolicy | null;
  quote: VirtualCityInsuranceQuote | null;
  spectator: boolean;
  awaiting: boolean;
  myCash: number;
  onBuy: (kind: VirtualCityInsuranceKind) => void;
  onCancel: (kind: VirtualCityInsuranceKind) => void;
}) {
  const t = useT();
  const lapsed = policy?.status === 'lapsed';
  // active/waiting/grace 均为「持有中」（宽限期保障仍有效，§4.2）。
  const holding = !!policy && !lapsed;
  const monthly = policy
    ? policy.monthly_premium_cny
    : Math.round((quote?.annual_premium_cny ?? 0) / 12);
  // 现金不足首月保费 → 投保按钮禁用（UX 提示；服务端权威 35007）。
  const cashShort = !spectator && !holding && !!quote && monthly > 0 && myCash < monthly;

  return (
    <div
      className={
        'virtualCity-insurance__card' +
        (lapsed ? ' virtualCity-insurance__card--lapsed' : '') +
        (holding ? ' virtualCity-insurance__card--holding' : '')
      }
    >
      <div className="virtualCity-insurance__card-head">
        <span className="virtualCity-insurance__kind">{t(`virtualCity.insurance.kind.${kind}` as TKey)}</span>
        {policy ? (
          <span
            className={`virtualCity-insurance__badge ${STATUS_CLASS[policy.status] ?? STATUS_CLASS.lapsed}`}
          >
            ● {statusLabel(t, policy)}
          </span>
        ) : (
          <span className="virtualCity-insurance__badge virtualCity-insurance__badge--none" aria-hidden="true">
            —
          </span>
        )}
      </div>

      {policy && (
        <ul className="virtualCity-insurance__rows">
          <li>{coverageText(t, kind, policy.coverage_cny)}</li>
          <li>
            {t('virtualCity.insurance.annualPremium' as TKey)} ¥{formatCny(policy.annual_premium_cny)}
            <small className="virtualCity-insurance__sub">
              （{t('virtualCity.insurance.monthlyPremium' as TKey, {
                n: formatCny(policy.monthly_premium_cny),
              })}）
            </small>
          </li>
          <li>{t('virtualCity.insurance.paidMonths' as TKey, { n: policy.paid_months })}</li>
          {policy.claims_total_cny > 0 && (
            <li>
              {t('virtualCity.insurance.claimsTotal' as TKey)}{' '}
              <span className="virtualCity-num--pos">¥{formatCny(policy.claims_total_cny)}</span>
            </li>
          )}
          {/* 身故赔付进遗产池提示（§5.3；寿险 / 意外险已赔付的失效单）。 */}
          {lapsed &&
            policy.claims_total_cny > 0 &&
            (kind === 'term_life' || kind === 'accident') && (
              <li className="virtualCity-insurance__note">
                ⚰ {t('virtualCity.insurance.deathClaim' as TKey)}
              </li>
            )}
        </ul>
      )}

      {/* 报价区：未投保 / 已失效 → 当前年龄档报价（lapsed 卡同时展示现价对比）。 */}
      {quote && (
        <p className="virtualCity-insurance__quote">
          <small>
            {t('virtualCity.insurance.quoteAtAge' as TKey)}：{t('virtualCity.insurance.annualPremium' as TKey)}{' '}
            ¥{formatCny(quote.annual_premium_cny)}
          </small>
        </p>
      )}

      {!spectator && quote && !holding && (
        <button
          type="button"
          className="btn btn-primary virtualCity-insurance__buy"
          disabled={awaiting || cashShort}
          title={cashShort ? t('virtualCity.error.cash' as TKey) : undefined}
          data-testid={`virtualCity-insurance-buy-${kind}`}
          onClick={() => onBuy(kind)}
        >
          {t('virtualCity.insurance.buy' as TKey)}
        </button>
      )}
      {!spectator && holding && (
        <button
          type="button"
          className="btn btn-secondary virtualCity-insurance__cancel"
          disabled={awaiting}
          data-testid={`virtualCity-insurance-cancel-${kind}`}
          onClick={() => onCancel(kind)}
        >
          {t('virtualCity.insurance.cancel' as TKey)}
        </button>
      )}
    </div>
  );
}
