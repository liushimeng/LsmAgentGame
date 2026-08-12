import { useT } from '@/hooks/useT';
import { useWallet } from '@/hooks/useWallet';
import { formatBalance } from '@/shared/utils/balance';
import type { TKey } from '@/i18n';

/** Doudizhu ante tiers as specified in 斗地主金币设计.md. */
export const DOUDIZHU_ANTE_TIERS = [100, 500, 1000, 5000] as const;
export type DoudizhuAnte = (typeof DOUDIZHU_ANTE_TIERS)[number];

/** Single-game safety cap (per spec). */
const DOUDIZHU_CAP = 25000;

interface Props {
  value: number;
  onChange: (ante: number) => void;
}

/**
 * AnteDoudizhuSelector —— 斗地主底注选择器。
 * 100 / 500 / 1000 / 5000 四档。
 * 入座冻结 ante × 4；显示底注、冻结额、单局上限保护 25000。
 * 余额不足最低 4× 时整张卡片置灰。
 */
export function AnteDoudizhuSelector({ value, onChange }: Props) {
  const t = useT();
  const { balance } = useWallet();
  const tableAnte = value;

  return (
    <div className="ante-doudizhu-selector">
      <div className="ante-doudizhu-selector__title">
        {t('ante.title' as TKey)}
        {balance != null && (
          <span className="ante-doudizhu-selector__balance">
            {' · '}
            {t('wallet.balance' as TKey)}: {formatBalance(balance, 'zh-CN')}
          </span>
        )}
      </div>

      <div className="ante-doudizhu-selector__grid">
        {DOUDIZHU_ANTE_TIERS.map((ante) => {
          const frozen = ante * 4;
          const enabled = balance == null || balance >= frozen;
          const selected = tableAnte === ante;
          const cls = [
            'ante-doudizhu-card',
            selected ? 'selected' : '',
            !enabled ? 'disabled' : '',
          ].filter(Boolean).join(' ');

          return (
            <button
              key={ante}
              className={cls}
              disabled={!enabled}
              onClick={() => enabled && onChange(ante)}
              title={
                !enabled
                  ? t('ante.minBalance' as TKey, { amount: frozen })
                  : t('doudizhu.ante.tooltip' as TKey, { ante, frozen })
              }
            >
              <span className="ante-doudizhu-card__value">{ante}</span>
              <span className="ante-doudizhu-card__frozen">
                {t('doudizhu.ante.frozenPerSeat' as TKey)}: {formatBalance(frozen, 'zh-CN')}
              </span>
              <span className="ante-doudizhu-card__cap">
                {t('ante.cap' as TKey)}: {DOUDIZHU_CAP.toLocaleString()}
              </span>
              {!enabled && <span className="ante-doudizhu-card__no-funds">⚠</span>}
            </button>
          );
        })}
      </div>

      {tableAnte > 0 && (
        <div className="ante-doudizhu-selector__hint">
          {t('doudizhu.ante.yourAnte' as TKey)} = {tableAnte} ·{' '}
          {t('doudizhu.ante.maxWinFormula' as TKey)} = {tableAnte} × 2 × M
        </div>
      )}
    </div>
  );
}
