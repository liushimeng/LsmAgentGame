/**
 * FamilyPanel — 家庭 / 代际财富面板（批次52 §20261002-01，P1-3 §9.1）。
 *
 * 契约：lag_docs/虚拟城市/已实现/52-代际财富转移引擎/虚拟城市-批次52-代际财富转移引擎-实施设计-v1.md
 * §8.1 + §7（MyFamilyJSON 扩展，snake_case）。
 *
 * 三段布局（挂 EconomyPanel 末尾 CollapsibleSection，默认展开首段）：
 *   ① 父母（在世/年龄/健康档）+ 子女列表（年龄/教育档）+ 本月现金流三行（赡养/教育/回流）
 *   ② 可折叠「累计」段（赡养/教育/回流三项累计；缺 totals 时整段隐藏）
 *   ③ 操作区：教育升级按钮 per 子女（upgrade_education）+ 自愿加赡养
 *      （pay_support_extra）+ 遗产分配预览按钮（AppModal 客户端预演 §4 D8，不落账）
 *
 * 降级（§7 渐进增强）：family 新段（parents/kids/support_cny/edu_cny/child_in_cny/totals）
 * 未下发时只渲染婚育基础信息（marital/children），② ③ 升级按钮隐藏，不崩。
 *
 * 动作回执（仿 InsurancePanel）：awaiting + wsClient.on 监听（refs 防竞态）+ 8s 超时。
 * 操作失败：面板内联 formError 红条（不关面板，CLAUDE.md §7.1 优先级 1）+
 * reportGlobalError 全局 toast 双通道。现金不足按钮置灰 + cursor:not-allowed
 * （禁 opacity:0.4 反模式，§26.2-5）。
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { reportGlobalError } from '@/services/globalError';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { CollapsibleSection } from './CollapsibleSection';
import {
  formatCny,
  WEALTH_EDU_UPGRADE_MAX_AGE,
  WEALTH_EDU_UPGRADE_MIN_AGE,
  WEALTH_FAMILY_SUPPORT_CAP_RATIO,
  WEALTH_PRIVATE_EDU_INIT_CNY,
  type VirtualCityAction,
  type VirtualCityMyState,
} from '@/types/virtualCity';

const AWAIT_TIMEOUT_MS = 8000;

interface Props {
  /** game.state.my；null = 观战 / 全 Agent（渲染空态）。 */
  my: VirtualCityMyState | null;
  /** 观战 / 全 Agent → 只读，隐藏全部按钮。 */
  spectator: boolean;
  /** 发送 game.virtual_city_action（useVirtualCity.sendAction 注入）。 */
  onAction: (action: VirtualCityAction) => void;
}

/** 健康档徽章类（§26.3 三件套：JSX 拼接与 virtualCity-family.css 规则同提交）。 */
const HEALTH_CLASS: Record<string, string> = {
  good: 'virtualCity-family__badge--good',
  fair: 'virtualCity-family__badge--fair',
  poor: 'virtualCity-family__badge--poor',
};

/** 健康档文案（枚举优先，后端 health_cn 兜底）。 */
function healthLabel(
  t: ReturnType<typeof useT>,
  health: string,
  healthCn: string,
): string {
  if (health === 'good' || health === 'fair' || health === 'poor') {
    return t(`virtualCity.family.health.${health}` as TKey);
  }
  return healthCn || t('virtualCity.family.health.fair' as TKey);
}

/** 教育档文案（public/private 枚举优先，education_cn 兜底）。 */
function eduLabel(
  t: ReturnType<typeof useT>,
  education: string,
  educationCn: string,
): string {
  if (education === 'public' || education === 'private') {
    return t(`virtualCity.family.kidEdu.${education}` as TKey);
  }
  return educationCn || t('virtualCity.family.kidEdu.public' as TKey);
}

/** 遗产分配预演（批次52 §4 D8 客户端镜像：不落账、纯展示）。 */
function planEstate(
  my: VirtualCityMyState,
): { estate: number; spouse: number; perChild: number; toWorld: number; kids: number } {
  const assetsSum = (my.assets ?? []).reduce((s, a) => s + (a.value_cny ?? 0), 0);
  const loansSum = (my.loans ?? []).reduce((s, l) => s + (l.balance ?? 0), 0);
  const estate = Math.max(0, (my.cash ?? 0) + assetsSum - loansSum - 5000);
  const kids = my.family?.kids?.length ?? my.family?.children ?? 0;
  const married = my.family?.marital === 'married';
  if (married && kids > 0) {
    return { estate, spouse: estate * 0.5, perChild: estate * 0.5 / kids, toWorld: 0, kids };
  }
  if (married) {
    return { estate, spouse: estate, perChild: 0, toWorld: 0, kids };
  }
  if (kids > 0) {
    return { estate, spouse: 0, perChild: estate / kids, toWorld: 0, kids };
  }
  return { estate, spouse: 0, perChild: 0, toWorld: estate, kids };
}

export function FamilyPanel({ my, spectator, onAction }: Props) {
  const t = useT();
  const mySeat = useVirtualCityStore((s) => s.mySeat);
  const [error, setError] = useState<string | null>(null);
  const [awaiting, setAwaiting] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [supportAmount, setSupportAmount] = useState('');

  // refs 防监听器闭包竞态（照 InsurancePanel：mount 注册一次，经 ref 读最新值）。
  const awaitingRef = useRef(false);
  awaitingRef.current = awaiting;
  const mySeatRef = useRef(mySeat);
  mySeatRef.current = mySeat;

  const mapError = useCallback(
    (code: number, message: string): string => {
      if (code === 35007) return t('virtualCity.error.cash' as TKey);
      return message || t('virtualCity.error.generic' as TKey);
    },
    [t],
  );

  /** §7.1 双通道：内联红条 + 全局 toast（不吞 console）。 */
  const fail = useCallback(
    (msg: string) => {
      setError(msg);
      reportGlobalError({ message: msg, severity: 'error' });
      setAwaiting(false);
    },
    [],
  );

  useEffect(() => {
    const off = wsClient.on((env: WsEnvelope) => {
      if (!awaitingRef.current) return;
      if (env.type === 'game.event') {
        const p = env.payload as { seat?: number; type?: string; text?: string };
        if (p.type === 'error') {
          fail(p.text || t('virtualCity.error.generic' as TKey));
          return;
        }
        // 本人动作回执（后端成功后随 game.event 单发 game.state 刷新面板）。
        if (p.seat === mySeatRef.current && p.type === 'action') {
          setAwaiting(false);
          setError(null);
        }
      } else if (env.type === 'game.error') {
        const p = env.payload as { code: number; message: string };
        fail(mapError(p.code, p.message));
      }
    });
    return () => off();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t, mapError, fail]);

  // 超时定时器（awaiting=true 时启动）。
  useEffect(() => {
    if (!awaiting) return;
    const timer = window.setTimeout(() => {
      fail(t('virtualCity.error.timeout' as TKey));
    }, AWAIT_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [awaiting, t, fail]);

  const fire = useCallback(
    (action: VirtualCityAction) => {
      setError(null);
      awaitingRef.current = true; // 同步置 ref，防往返极快丢事件
      setAwaiting(true);
      onAction(action);
    },
    [onAction],
  );

  const handleUpgrade = useCallback(
    (childIdx: number) => {
      fire({ type: 'upgrade_education', child_idx: childIdx });
    },
    [fire],
  );

  const supportCap = Math.floor((my?.net_worth ?? 0) * WEALTH_FAMILY_SUPPORT_CAP_RATIO);
  const supportValue = Number(supportAmount);

  const handlePaySupport = useCallback(() => {
    if (!Number.isFinite(supportValue) || supportValue <= 0) {
      fail(t('virtualCity.family.paySupportInvalid' as TKey));
      return;
    }
    if (supportValue > supportCap) {
      fail(t('virtualCity.family.paySupportCap' as TKey, { n: formatCny(supportCap) }));
      return;
    }
    fire({ type: 'pay_support_extra', amount_cny: Math.floor(supportValue) });
  }, [fire, fail, supportValue, supportCap, t]);

  // 空态：观战 / 全 Agent（my=null）。
  if (!my) {
    return (
      <CollapsibleSection
        title={`👨‍👩‍👧 ${t('virtualCity.family.title' as TKey)}`}
        storageKey="virtualCity.ui.collapsed.family"
        defaultCollapsed={false}
        className="virtualCity-family-wrap"
        testId="virtualCity-family-panel"
      >
        <div className="virtualCity-panel__empty">
          <p>{t('virtualCity.family.empty' as TKey)}</p>
          {spectator && (
            <p className="virtualCity-family__hint">
              👁 {t('virtualCity.family.spectatorHint' as TKey)}
            </p>
          )}
        </div>
      </CollapsibleSection>
    );
  }

  const family = my.family;
  const parents = family?.parents;
  const kids = family?.kids;
  const totals = family?.totals;
  // 新段降级判定：parents/kids/totals 任一存在即视为新后端。
  const hasExt = !!(parents || kids || totals || family?.support_cny !== undefined);
  const childList = kids ?? [];
  const childrenCount = kids?.length ?? family?.children ?? 0;
  const canAct = !spectator && !!onAction;

  return (
    <CollapsibleSection
      title={`👨‍👩‍👧 ${t('virtualCity.family.title' as TKey)}`}
      storageKey="virtualCity.ui.collapsed.family"
      defaultCollapsed={false}
      className="virtualCity-family-wrap"
      bodyClassName="virtualCity-family"
      testId="virtualCity-family-panel"
    >
      {/* ① 父母 + 子女 + 本月现金流 */}
      <div className="virtualCity-family__section">
        <div className="virtualCity-family__row">
          <span className="virtualCity-family__label">
            {t('virtualCity.family.parents' as TKey)}
          </span>
          {parents ? (
            <span className="virtualCity-family__value">
              {parents.alive ? (
                <span className="virtualCity-family__badge virtualCity-family__badge--alive">
                  ✓ {t('virtualCity.family.parentsAlive' as TKey)}
                </span>
              ) : (
                <span className="virtualCity-family__badge virtualCity-family__badge--gone">
                  ✕ {t('virtualCity.family.parentsGone' as TKey)}
                </span>
              )}
              {parents.alive && (
                <>
                  <span className="virtualCity-family__meta">
                    {t('virtualCity.family.parentsAge' as TKey, { age: parents.age })}
                  </span>
                  <span
                    className={`virtualCity-family__badge ${HEALTH_CLASS[parents.health] ?? HEALTH_CLASS.fair}`}
                  >
                    {healthLabel(t, parents.health, parents.health_cn)}
                  </span>
                </>
              )}
            </span>
          ) : (
            <span className="virtualCity-family__value virtualCity-family__meta">
              {family?.marital === 'married'
                ? t('virtualCity.family.married' as TKey)
                : t('virtualCity.family.single' as TKey)}
            </span>
          )}
        </div>

        <div className="virtualCity-family__row">
          <span className="virtualCity-family__label">{t('virtualCity.family.kids' as TKey)}</span>
          <span className="virtualCity-family__value virtualCity-family__kids">
            {childrenCount === 0 ? (
              <span className="virtualCity-family__meta">{t('virtualCity.family.noKids' as TKey)}</span>
            ) : hasExt && childList.length > 0 ? (
              childList.map((k, i) => (
                <span key={i} className="virtualCity-family__kid">
                  {t('virtualCity.family.parentsAge' as TKey, { age: k.age })}
                  <span
                    className={`virtualCity-family__badge ${k.education === 'private' ? 'virtualCity-family__badge--private' : 'virtualCity-family__badge--public'}`}
                  >
                    {eduLabel(t, k.education, k.education_cn)}
                  </span>
                </span>
              ))
            ) : (
              <span className="virtualCity-family__meta">
                {t('virtualCity.family.childrenCount' as TKey, { n: childrenCount })}
              </span>
            )}
          </span>
        </div>

        {/* 本月现金流三行（赡养/教育/回流）；新段缺省时隐藏 */}
        {hasExt && (
          <div className="virtualCity-family__flows">
            <div className="virtualCity-family__flow">
              <span className="virtualCity-family__label">
                {t('virtualCity.family.monthlySupport' as TKey)}
              </span>
              <span className="virtualCity-family__amount virtualCity-num--neg">
                −¥{formatCny(family?.support_cny ?? 0)}
              </span>
            </div>
            <div className="virtualCity-family__flow">
              <span className="virtualCity-family__label">
                {t('virtualCity.family.monthlyEdu' as TKey)}
              </span>
              <span className="virtualCity-family__amount virtualCity-num--neg">
                −¥{formatCny(family?.edu_cny ?? 0)}
              </span>
            </div>
            <div className="virtualCity-family__flow">
              <span className="virtualCity-family__label">
                {t('virtualCity.family.monthlyChildIn' as TKey)}
              </span>
              <span className="virtualCity-family__amount virtualCity-num--pos">
                +¥{formatCny(family?.child_in_cny ?? 0)}
              </span>
            </div>
          </div>
        )}
      </div>

      {/* ② 可折叠「累计」段（缺 totals 整段隐藏） */}
      {hasExt && totals && (
        <CollapsibleSection
          title={t('virtualCity.family.totalsTitle' as TKey)}
          storageKey="virtualCity.ui.collapsed.familyTotals"
          defaultCollapsed
          className="virtualCity-family__totals-wrap"
          testId="virtualCity-family-totals"
        >
          <div className="virtualCity-family__flows">
            <div className="virtualCity-family__flow">
              <span className="virtualCity-family__label">
                {t('virtualCity.family.totalSupport' as TKey)}
              </span>
              <span className="virtualCity-family__amount">¥{formatCny(totals.support)}</span>
            </div>
            <div className="virtualCity-family__flow">
              <span className="virtualCity-family__label">
                {t('virtualCity.family.totalEdu' as TKey)}
              </span>
              <span className="virtualCity-family__amount">¥{formatCny(totals.education)}</span>
            </div>
            <div className="virtualCity-family__flow">
              <span className="virtualCity-family__label">
                {t('virtualCity.family.totalChildIn' as TKey)}
              </span>
              <span className="virtualCity-family__amount">¥{formatCny(totals.child_received)}</span>
            </div>
          </div>
        </CollapsibleSection>
      )}

      {/* ③ 操作区：教育升级 per 子女 + 自愿加赡养 + 遗产预览 */}
      <div className="virtualCity-family__actions">
        {canAct &&
          childList.map((k, i) => {
            // 仅公立 + 年龄∈[3,18] 的子女渲染升级按钮（D11；已私立/超龄隐藏而非置灰）。
            const eligible =
              k.education === 'public' &&
              k.age >= WEALTH_EDU_UPGRADE_MIN_AGE &&
              k.age <= WEALTH_EDU_UPGRADE_MAX_AGE;
            if (!eligible) return null;
            // 现金不足 → 置灰 + cursor:not-allowed（§26.2-5 禁 opacity 反模式）。
            const cashShort = my.cash < WEALTH_PRIVATE_EDU_INIT_CNY;
            return (
              <button
                key={i}
                type="button"
                className="btn btn-primary virtualCity-family__btn"
                disabled={awaiting || cashShort}
                title={cashShort ? t('virtualCity.family.eduUnaffordable' as TKey) : undefined}
                data-testid={`virtualCity-family-upgrade-${i}`}
                onClick={() => handleUpgrade(i)}
              >
                {t('virtualCity.family.upgradeEduBtn' as TKey, {
                  age: k.age,
                  price: formatCny(WEALTH_PRIVATE_EDU_INIT_CNY),
                })}
              </button>
            );
          })}

        {canAct && hasExt && (
          <div className="virtualCity-family__support">
            <label className="virtualCity-family__support-label" htmlFor="virtualCity-family-support-amount">
              {t('virtualCity.family.paySupportLabel' as TKey)}
            </label>
            <input
              id="virtualCity-family-support-amount"
              className="virtualCity-family__support-input"
              type="number"
              min={1}
              max={Math.max(1, supportCap)}
              step={100}
              value={supportAmount}
              placeholder={t('virtualCity.family.paySupportAmount' as TKey)}
              onChange={(e) => setSupportAmount(e.target.value)}
            />
            <button
              type="button"
              className="btn btn-secondary virtualCity-family__btn"
              disabled={awaiting || supportCap <= 0 || !(supportValue > 0)}
              data-testid="virtualCity-family-pay-support"
              onClick={handlePaySupport}
            >
              {t('virtualCity.family.paySupportBtn' as TKey)}
            </button>
            <small className="virtualCity-family__meta">
              {t('virtualCity.family.paySupportCap' as TKey, { n: formatCny(supportCap) })}
            </small>
          </div>
        )}

        <button
          type="button"
          className="btn btn-secondary virtualCity-family__btn"
          data-testid="virtualCity-family-preview"
          onClick={() => setPreviewOpen(true)}
        >
          {t('virtualCity.family.previewBtn' as TKey)}
        </button>
      </div>

      {/* 操作失败内联红条（§7.1 优先级 1：不关面板，就地重试） */}
      {error && (
        <div className="virtualCity-family__error" role="alert">
          ⚠️ {error}
        </div>
      )}

      {/* 遗产分配预览（AppModal；纯客户端预演 §4 D8，不落账） */}
      {previewOpen && (
        <AppModal
          title={t('virtualCity.family.previewTitle' as TKey)}
          icon="📜"
          kind="info"
          maxWidth={440}
          dismissible
          onClose={() => setPreviewOpen(false)}
          footer={
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => setPreviewOpen(false)}
            >
              {t('virtualCity.action.confirm' as TKey)}
            </button>
          }
        >
          {(() => {
            const plan = planEstate(my);
            if (plan.estate <= 0) {
              return <p className="virtualCity-family__meta">{t('virtualCity.family.previewEmpty' as TKey)}</p>;
            }
            return (
              <div className="virtualCity-family__preview">
                <div className="virtualCity-family__flow">
                  <span className="virtualCity-family__label">
                    {t('virtualCity.family.previewEstate' as TKey)}
                  </span>
                  <span className="virtualCity-family__amount">¥{formatCny(plan.estate)}</span>
                </div>
                <div className="virtualCity-family__flow">
                  <span className="virtualCity-family__label">
                    {t('virtualCity.family.previewFuneral' as TKey)}
                  </span>
                  <span className="virtualCity-family__amount">¥5,000</span>
                </div>
                {plan.spouse > 0 && (
                  <div className="virtualCity-family__flow">
                    <span className="virtualCity-family__label">
                      {t('virtualCity.family.previewSpouse' as TKey)}
                    </span>
                    <span className="virtualCity-family__amount">¥{formatCny(plan.spouse)}</span>
                  </div>
                )}
                {plan.perChild > 0 && (
                  <div className="virtualCity-family__flow">
                    <span className="virtualCity-family__label">
                      {t('virtualCity.family.previewPerChild' as TKey, { n: plan.kids })}
                    </span>
                    <span className="virtualCity-family__amount">¥{formatCny(plan.perChild)}</span>
                  </div>
                )}
                {plan.toWorld > 0 && (
                  <div className="virtualCity-family__flow">
                    <span className="virtualCity-family__label">
                      {t('virtualCity.family.previewWorld' as TKey)}
                    </span>
                    <span className="virtualCity-family__amount">¥{formatCny(plan.toWorld)}</span>
                  </div>
                )}
                <p className="virtualCity-family__meta">{t('virtualCity.family.previewNote' as TKey)}</p>
              </div>
            );
          })()}
        </AppModal>
      )}
    </CollapsibleSection>
  );
}

export default FamilyPanel;
