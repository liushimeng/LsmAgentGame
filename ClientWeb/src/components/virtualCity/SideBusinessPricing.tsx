/**
 * SideBusinessPricing — 副业定价战 UI（批次 20 文档 2 §5 / FE-2 F2-2）。
 *
 * ActionPanel 内嵌的副业定价区块（仅 my.side_business 非空且有座位时渲染）：
 *   ① 当前档位徽章 + 「份额 xx% · 预期 ¥xxxx」（预期 = Base×m(t)×s 前端估算）；
 *   ② 三档 segmented 选择器（当前档高亮；同月已改档 → 其余档禁用 + tooltip，
 *      提交走 ActionPanel.fire({type:'set_side_price',tier})，服务端权威）；
 *   ③ 同品类对手档位 chips（game.state.side_market[kind]，默认折叠）。
 *
 * 状态色遵循 CLAUDE.md §26.1：低=蓝 / 中=绿 / 高=橙徽章白字 ≥4.5:1；
 * 禁用态实底灰 + cursor:not-allowed（禁 opacity 表意）。
 */

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  WEALTH_SIDE_BIZ_GATE,
  WEALTH_SIDE_TIERS,
  formatCny,
  virtualCitySideExpectedIncome,
  virtualCitySideTierMeta,
  type VirtualCitySidePriceTier,
  type VirtualCityGameState,
} from '@/types/virtualCity';

interface Props {
  gameState: VirtualCityGameState | null;
  mySeat: number;
  /** 动作回执等待中（弹窗/按钮提交锁定）。 */
  busy: boolean;
  /** 本自然月已改过价（前端 UX 门；服务端 TierSetMonth 权威）。 */
  lockedThisMonth: boolean;
  /** 内联失败文案（仅本区块最近一次提交为 set_side_price 时由父组件传入）。 */
  error: string | null;
  onSetPrice: (tier: VirtualCitySidePriceTier) => void;
}

/** 品类展示名：i18n 已知键优先，回落后端 kind_cn / kind。 */
function bizLabel(
  t: (k: TKey, vars?: Record<string, string | number>) => string,
  kind: string,
  kindCn: string | undefined,
): string {
  if (kind in WEALTH_SIDE_BIZ_GATE) return t(`virtualCity.biz.${kind}` as TKey);
  return kindCn || kind;
}

export function SideBusinessPricing({ gameState, mySeat, busy, lockedThisMonth, error, onSetPrice }: Props) {
  const t = useT();
  const [rivalsOpen, setRivalsOpen] = useState(false);
  const sb = gameState?.my?.side_business;
  if (!sb || !sb.kind) return null;

  const cur = virtualCitySideTierMeta(sb.price_tier);
  const sharePct = Math.round((sb.market_share ?? 1) * 100);
  const expected = virtualCitySideExpectedIncome(sb.base_income, sb.price_tier, sb.market_share);
  const rivals = (gameState?.side_market?.[sb.kind] ?? []).filter((r) => r.seat !== mySeat);

  return (
    <div className="virtualCity-sideprice" data-testid="virtualCity-sideprice">
      <div className="virtualCity-sideprice__head">
        <span className="virtualCity-sideprice__title">
          🛵 {t('virtualCity.action.sideBusiness' as TKey)} · {bizLabel(t, sb.kind, sb.kind_cn)}
        </span>
        <span
          className="virtualCity-sideprice__tier"
          style={{ background: cur.badgeColor }}
          data-testid="virtualCity-sideprice-current"
        >
          {t(`virtualCity.sidePrice.${cur.i18nKey}` as TKey)}
        </span>
        {/* 份额与预期收入行（文档 2 §5） */}
        <span className="virtualCity-sideprice__stats">
          {t('virtualCity.sideShare' as TKey, { pct: sharePct })} ·{' '}
          {t('virtualCity.sideExpected' as TKey, { amount: formatCny(expected) })}
        </span>
      </div>

      {/* 三档 segmented 选择器（当前档高亮；同月已改 = 其余档禁用 + tooltip） */}
      <div
        className="virtualCity-sideprice__segmented"
        role="group"
        aria-label={t('virtualCity.action.sideBusiness' as TKey)}
      >
        {WEALTH_SIDE_TIERS.map((meta) => {
          const isCur = meta.tier === cur.tier;
          const disabled = busy || isCur || lockedThisMonth;
          const title = lockedThisMonth && !isCur
            ? t('virtualCity.sidePriceGate' as TKey)
            : `${t(`virtualCity.sidePrice.${meta.i18nKey}` as TKey)} · ${t('virtualCity.sideShare' as TKey, { pct: Math.round(meta.weight * 100) })} · ×${meta.multiplier.toFixed(2)}`;
          return (
            <button
              key={meta.tier}
              type="button"
              className={
                'virtualCity-sideprice__btn' +
                (isCur ? ' virtualCity-sideprice__btn--active' : '') +
                (disabled && !isCur ? ' virtualCity-sideprice__btn--locked' : '')
              }
              style={isCur ? { background: meta.badgeColor } : undefined}
              disabled={disabled}
              aria-pressed={isCur}
              title={title}
              onClick={() => onSetPrice(meta.tier)}
              data-testid={`virtualCity-sideprice-tier-${meta.tier}`}
            >
              {t(`virtualCity.sidePrice.${meta.i18nKey}` as TKey)}
            </button>
          );
        })}
      </div>

      {/* 同品类对手档位 chips（默认折叠，文档 2 §5） */}
      {rivals.length > 0 && (
        <div className="virtualCity-sideprice__rivals">
          <button
            type="button"
            className="virtualCity-sideprice__rivals-toggle"
            onClick={() => setRivalsOpen((v) => !v)}
            aria-expanded={rivalsOpen}
            data-testid="virtualCity-sideprice-rivals-toggle"
          >
            {rivalsOpen ? '▲' : '▼'} {t('virtualCity.sideCompetitors' as TKey, { n: rivals.length })}
          </button>
          {rivalsOpen && (
            <div className="virtualCity-sideprice__chips">
              {rivals.map((r) => {
                const m = virtualCitySideTierMeta(r.tier);
                return (
                  <span
                    key={r.seat}
                    className="virtualCity-sideprice__chip"
                    style={{ background: m.badgeColor }}
                    title={`S${r.seat + 1} · ${t('virtualCity.sideShare' as TKey, { pct: Math.round(r.share * 100) })}`}
                    data-testid={`virtualCity-sideprice-rival-${r.seat}`}
                  >
                    S{r.seat + 1} {t(`virtualCity.sidePrice.${m.i18nKey}` as TKey)}
                  </span>
                );
              })}
            </div>
          )}
        </div>
      )}

      {error && <div className="virtualCity-sideprice__error" role="alert">{error}</div>}
    </div>
  );
}

export default SideBusinessPricing;
