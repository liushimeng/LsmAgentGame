import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

/** 底注档位 —— 中国象棋 / 中国军棋共用。 */
export const ANTE_TIERS = [50, 100, 500, 1000] as const;
export type AnteTier = (typeof ANTE_TIERS)[number];

export interface AnteSelectorProps {
  /** 当前选中的底注（0 = 未选）。 */
  value: number;
  onChange: (ante: number) => void;
  /** 用户当前余额（用于置灰判断）。 */
  balance: number | null;
  /**
   * 创建房间所需的最低余额倍数。
   * - 中国象棋：ante × 1.2
   * - 中国军棋：ante × 5
   * 默认 1.2。
   */
  minMultiplier?: number;
  /** 是否禁用整个选择器。 */
  disabled?: boolean;
}

/**
 * AnteSelector —— 50 / 100 / 500 / 1000 四档底注按钮。
 * 余额不达标时置灰 + tooltip 提示。
 */
export function AnteSelector({
  value,
  onChange,
  balance,
  minMultiplier = 1.2,
  disabled = false,
}: AnteSelectorProps) {
  const t = useT();

  return (
    <div className="ante-selector">
      <div className="ante-selector__label">{t('ante.title' as TKey)}</div>
      <div className="ante-selector__tiers">
        {ANTE_TIERS.map((tier) => {
          const minRequired = Math.ceil(tier * minMultiplier);
          const insufficient = balance != null && balance < minRequired;
          const selected = value === tier;
          const tierDisabled = disabled || insufficient;

          return (
            <button
              key={tier}
              type="button"
              className={
                'ante-tier' +
                (selected ? ' ante-tier--selected' : '') +
                (tierDisabled ? ' ante-tier--disabled' : '')
              }
              onClick={() => !tierDisabled && onChange(tier)}
              disabled={tierDisabled}
              title={
                insufficient
                  ? `${t('ante.minBalance' as TKey, { amount: minRequired })} — ${t('ante.insufficient' as TKey)}`
                  : t('ante.minBalance' as TKey, { amount: minRequired })
              }
            >
              <span className="ante-tier__value">{tier}</span>
              {insufficient && (
                <span className="ante-tier__warn" title={t('ante.insufficient' as TKey)}>
                  🔒
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
