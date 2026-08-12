import { useT } from '@/hooks/useT';
import { useWallet } from '@/hooks/useWallet';
import { formatBalance } from '@/shared/utils/balance';
import type { TKey } from '@/i18n';

/** Blind tier definition for Texas Hold'em.
 *  Each card shows BB/SB/Ante along with the computed min / default / max buy-in. */
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
 * 5 档 (10 / 50 / 200 / 1000 / 5000 BB)。显示 SB / Ante / min / max buy-in。
 * 余额 < min buy-in 时置灰 + tooltip "余额不足"。
 */
export function BlindSelector({ value, onChange }: Props) {
  const t = useT();
  const { balance } = useWallet();

  return (
    <div className="blind-selector">
      <div className="blind-selector__title">
        {t('texasholdem.blind.title' as TKey)}
        {balance != null && (
          <span className="blind-selector__balance">
            {' · '}
            {t('wallet.balance' as TKey)}: {formatBalance(balance, 'zh-CN')}
          </span>
        )}
      </div>

      <div className="blind-selector__grid">
        {BLIND_TIERS.map((tier) => {
          const { min, defaultBI, max } = buyInRange(tier.bb);
          const sb = tier.bb / 2;
          const locked = tier.active === false;
          const insufficient = balance != null && balance < min;
          const disabled = locked || insufficient;
          const selected = value === tier.bb;
          const cls = [
            'blind-card',
            selected ? 'selected' : '',
            disabled ? 'disabled' : '',
            locked ? 'locked' : '',
            insufficient && !locked ? 'insufficient' : '',
          ].filter(Boolean).join(' ');

          return (
            <button
              key={tier.bb}
              className={cls}
              disabled={disabled}
              onClick={() => !disabled && onChange(tier.bb)}
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
              <span className="blind-card__bb">{t('texasholdem.blind.bb' as TKey)} {tier.bb}</span>
              <span className="blind-card__sb">
                {t('texasholdem.blind.sb' as TKey)} {sb} · {t('texasholdem.blind.ante' as TKey)} {sb}
              </span>
              <span className="blind-card__buyin">
                {t('texasholdem.blind.buyin' as TKey)}: {formatBalance(min, 'zh-CN')}–{formatBalance(max, 'zh-CN')}
              </span>
              <span className="blind-card__def">
                {t('texasholdem.blind.default' as TKey)}: {formatBalance(defaultBI, 'zh-CN')}
              </span>
              {insufficient && !locked && <span className="blind-card__no-funds">⚠</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}
