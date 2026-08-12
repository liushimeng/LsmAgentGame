import { useT } from '@/hooks/useT';
import { useWallet } from '@/hooks/useWallet';
import { formatBalance } from '@/shared/utils/balance';
import type { TKey } from '@/i18n';
import { buyInRange } from './BlindSelector';

interface Props {
  /** Currently selected big-blind value (determines the buy-in range). */
  bb: number;
  /** Current buy-in value. */
  value: number;
  onChange: (buyin: number) => void;
}

/** Risk threshold — if buy-in / balance ≥ this ratio we show a red warning. */
const RISK_RED_THRESHOLD = 0.25;

/**
 * BuyinSlider — 德州扑克买入选择器。
 * 提供 min / default / max × BB 三档快捷按钮 + 滑块。
 * 当前买入占钱包 ≥ 25% 时红色风险提示。
 */
export function BuyinSlider({ bb, value, onChange }: Props) {
  const t = useT();
  const { balance } = useWallet();
  const { min, defaultBI, max } = buyInRange(bb);

  const ratio = balance != null && balance > 0 ? value / balance : 0;
  const highRisk = ratio >= RISK_RED_THRESHOLD;

  const presets: Array<{ key: TKey; val: number }> = [
    { key: 'texasholdem.buyin.min' as TKey, val: min },
    { key: 'texasholdem.buyin.default' as TKey, val: defaultBI },
    { key: 'texasholdem.buyin.max' as TKey, val: max },
  ];

  return (
    <div className="buyin-slider">
      <div className="buyin-slider__title">{t('texasholdem.buyin.title' as TKey)}</div>

      <div className="buyin-slider__presets">
        {presets.map((p) => (
          <button
            key={p.key}
            type="button"
            className={'btn btn-sm ' + (value === p.val ? 'btn-primary' : 'btn-ghost')}
            onClick={() => onChange(p.val)}
          >
            {t(p.key)} ({formatBalance(p.val, 'zh-CN')})
          </button>
        ))}
      </div>

      <input
        type="range"
        className="buyin-slider__range"
        min={min}
        max={max}
        step={bb}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
      />

      <div className="buyin-slider__info">
        <span className="buyin-slider__value">
          {t('texasholdem.buyin.current' as TKey)}: <strong>{formatBalance(value, 'zh-CN')}</strong>
        </span>
        {balance != null && (
          <span className={`buyin-slider__ratio ${highRisk ? 'risk' : ''}`}>
            {t('texasholdem.buyin.pctOfBalance' as TKey)}: {(ratio * 100).toFixed(1)}%
          </span>
        )}
      </div>

      {highRisk && balance != null && (
        <div className="buyin-slider__warn">
          ⚠ {t('texasholdem.buyin.riskWarning' as TKey, { pct: Math.round(RISK_RED_THRESHOLD * 100) })}
        </div>
      )}
    </div>
  );
}
