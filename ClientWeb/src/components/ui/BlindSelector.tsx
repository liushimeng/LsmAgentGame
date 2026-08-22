import { useT } from '@/hooks/useT';
import { useWallet } from '@/hooks/useWallet';
import { formatBalance } from '@/shared/utils/balance';
import type { TKey } from '@/i18n';

/** Blind tier definition for Texas Hold'em.
 *  Each option shows BB/SB/Ante along with the computed min / default / max buy-in. */
export interface BlindTier {
  /** Big-blind value. Small-blind = BB / 2, ante = SB. */
  bb: number;
  /** Whether the tier is currently selectable. */
  active?: boolean;
  /** i18n key for locked hint tooltip. */
  lockedHintKey?: TKey;
}

export const BLIND_TIERS: BlindTier[] = [
  { bb: 10, active: true },
  { bb: 50, active: true },
  { bb: 200, active: true },
  { bb: 1000, active: true },
  { bb: 5000, active: true },
];

/** Standard buy-in formula from the spec: min=BB×20, default=BB×50, max=BB×100. */
export function buyInRange(bb: number): { min: number; defaultBI: number; max: number } {
  return {
    min: bb * 20,
    defaultBI: bb * 50,
    max: bb * 100,
  };
}

interface Props {
  /** Currently selected big-blind value. */
  value: number;
  onChange: (bb: number) => void;
}

/**
 * BlindSelector — 德州扑克盲注级别选择器。
 *
 * 2026-08-22 §20260822-02 — 改用 List 下拉列表（`<select>`），把 5 档盲注折叠成
 * 单行 36px 的下拉框；选中后下方展开一条 4 列信息条（BB / SB+Ante / BuyIn range /
 * 默认买入），既保留详细档位信息又避免了大面积留白。
 *
 * 5 档 (10 / 50 / 200 / 1000 / 5000 BB)。余额 < min buy-in 时该档 `<option>`
 * 置灰 + tooltip "余额不足"。
 */
export function BlindSelector({ value, onChange }: Props) {
  const t = useT();
  const { balance } = useWallet();

  const currentTier = BLIND_TIERS.find((tier) => tier.bb === value) ?? BLIND_TIERS[0];
  const currentRange = buyInRange(currentTier.bb);
  const currentSb = currentTier.bb / 2;

  return (
    <div className="blind-selector">
      <div className="blind-selector__title">
        <span>
          {t('texasholdem.blind.title' as TKey)}
          {currentTier ? (
            <span className="blind-selector__title-current">
              {' · '}
              {t('texasholdem.blind.bb' as TKey)} {currentTier.bb}
            </span>
          ) : null}
        </span>
        {balance != null && (
          <span className="blind-selector__balance">
            {t('wallet.balance' as TKey)}: {formatBalance(balance, 'zh-CN')}
          </span>
        )}
      </div>

      <div className="blind-selector__select">
        <select
          value={value}
          onChange={(e) => onChange(Number(e.target.value))}
          data-testid="blind-selector__select"
          aria-label={t('texasholdem.blind.title' as TKey)}
        >
          {BLIND_TIERS.map((tier) => {
            const { min, max } = buyInRange(tier.bb);
            const sb = tier.bb / 2;
            const locked = tier.active === false;
            const insufficient = balance != null && balance < min;
            const disabled = locked || insufficient;
            const optionLabel =
              `${t('texasholdem.blind.bb' as TKey)} ${tier.bb}` +
              ` · ${t('texasholdem.blind.sb' as TKey)}/${t('texasholdem.blind.ante' as TKey)} ${sb}` +
              ` · ${t('texasholdem.blind.buyin' as TKey)} ${formatBalance(min, 'zh-CN')}–${formatBalance(max, 'zh-CN')}` +
              (disabled ? ' 🔒' : '');
            return (
              <option
                key={tier.bb}
                value={tier.bb}
                disabled={disabled}
                title={
                  locked
                    ? tier.lockedHintKey
                      ? t(tier.lockedHintKey)
                      : t('ante.comingSoon' as TKey)
                    : insufficient
                      ? t('ante.minBalance' as TKey, { amount: min })
                      : undefined
                }
              >
                {optionLabel}
              </option>
            );
          })}
        </select>
      </div>

      {currentTier && (
        <div className="blind-selector__info-row" data-testid="blind-selector__info">
          <div className="blind-selector__info-cell">
            <span className="blind-selector__info-label">
              {t('texasholdem.blind.bb' as TKey)}
            </span>
            <span className="blind-selector__info-value">{currentTier.bb}</span>
          </div>
          <div className="blind-selector__info-cell">
            <span className="blind-selector__info-label">
              {t('texasholdem.blind.sb' as TKey)}/{t('texasholdem.blind.ante' as TKey)}
            </span>
            <span className="blind-selector__info-value">{currentSb}</span>
          </div>
          <div className="blind-selector__info-cell">
            <span className="blind-selector__info-label">
              {t('texasholdem.blind.buyin' as TKey)}
            </span>
            <span className="blind-selector__info-value">
              {formatBalance(currentRange.min, 'zh-CN')}–{formatBalance(currentRange.max, 'zh-CN')}
            </span>
          </div>
          <div className="blind-selector__info-cell">
            <span className="blind-selector__info-label">
              {t('texasholdem.blind.default' as TKey)}
            </span>
            <span className="blind-selector__info-value">
              {formatBalance(currentRange.defaultBI, 'zh-CN')}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}