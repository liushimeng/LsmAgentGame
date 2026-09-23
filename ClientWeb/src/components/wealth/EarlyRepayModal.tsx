/**
 * EarlyRepayModal — 提前还款决策弹窗（P1 LPR 重定价 v2.60 N12-5）。
 *
 * 触发条件：game.state.early_repay_eligible === true（后端下发）。
 * 三按钮：全额还清 / 部分还清(50%) / 维持不变。
 * 展示：当前房贷利率、理财收益率、违约金、节省利息估算。
 * 危险操作确认：全额还清需二次确认。
 *
 * 错误展示（§7.1）：弹窗内联红条（formError，弹窗不关闭）。
 */

import { useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  formatPct,
  type WealthAction,
  type WealthLoan,
} from '@/types/wealth';

interface Props {
  /** 是否显示弹窗。 */
  open: boolean;
  /** 候选房贷列表（仅 mortgage 类型）。 */
  mortgageLoans: WealthLoan[];
  /** 当前理财收益率（年化，小数；用于机会成本提示）。 */
  investYield: number;
  /** 提交回调。 */
  onSubmit: (action: WealthAction) => void;
  /** 关闭回调（维持不变）。 */
  onDismiss: () => void;
}

type Mode = 'choose' | 'confirm-full' | 'confirm-partial';

/** 估算违约金（1 年内线性 1%→3%；与后端 actEarlyRepay 一致）。 */
function estimatePenalty(loan: WealthLoan): number {
  // 剩余期数 ≥ 360（≈30 年）即放款月内，简化用剩余期数反推已还期：假设 360 期总额度。
  const assumedTotal = 360;
  const monthsSinceOpen = Math.max(0, assumedTotal - loan.months_left);
  if (monthsSinceOpen >= 12) return 0;
  const penaltyRate = 0.01 + (12 - monthsSinceOpen) * 0.00167;
  return Math.round(loan.balance * penaltyRate);
}

/** 估算节省利息（剩余期数 × 月供 - 余额；简化估算）。 */
function estimateSavedInterest(loan: WealthLoan): number {
  if (loan.months_left <= 0) return 0;
  return Math.max(0, loan.monthly_payment * loan.months_left - loan.balance);
}

export function EarlyRepayModal({ open, mortgageLoans, investYield, onSubmit, onDismiss }: Props) {
  const t = useT();
  const [mode, setMode] = useState<Mode>('choose');
  const [loanId, setLoanId] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  // 默认选中第一笔房贷。
  const selectedLoan = mortgageLoans.find((l) => l.id === loanId) ?? mortgageLoans[0] ?? null;

  if (!open) return null;
  if (!selectedLoan) {
    // 无房贷可还 → 自动关闭。
    return null;
  }

  const penalty = estimatePenalty(selectedLoan);
  const savedInterest = estimateSavedInterest(selectedLoan);
  const partialAmount = Math.round(selectedLoan.balance * 0.5);
  const partialPenalty = Math.round(partialAmount * (penalty / (selectedLoan.balance || 1)));

  const handleFull = () => {
    setFormError(null);
    setMode('confirm-full');
  };

  const handlePartial = () => {
    setFormError(null);
    setMode('confirm-partial');
  };

  const handleConfirm = () => {
    if (!selectedLoan) {
      setFormError(t('wealth.action.noLoan' as TKey));
      return;
    }
    if (mode === 'confirm-full') {
      onSubmit({ type: 'early_repay', loan_id: selectedLoan.id, amount_cny: selectedLoan.balance });
    } else if (mode === 'confirm-partial') {
      onSubmit({ type: 'early_repay', loan_id: selectedLoan.id, amount_cny: partialAmount });
    }
    setMode('choose');
  };

  const handleCancel = () => {
    setMode('choose');
    setFormError(null);
  };

  const handleHold = () => {
    setMode('choose');
    setFormError(null);
    onDismiss();
  };

  // 机会成本提示：房贷利率 vs 理财收益率。
  const rateGap = selectedLoan.annual_rate - investYield;
  const showTriggerHint = rateGap > 0;

  const modalTitle = mode === 'choose'
    ? t('earlyrepay.title' as TKey)
    : mode === 'confirm-full'
      ? t('earlyrepay.confirmFullTitle' as TKey)
      : t('earlyrepay.confirmPartialTitle' as TKey);

  const modalKind = mode === 'choose' ? 'info' : 'warning';

  return (
    <AppModal
      title={modalTitle}
      icon={mode === 'choose' ? '💰' : '⚠️'}
      kind={modalKind}
      maxWidth={500}
      dismissible={mode === 'choose'}
      blockBackdropClose={mode !== 'choose'}
      onClose={mode === 'choose' ? handleHold : handleCancel}
      footer={mode === 'choose' ? (
        <>
          <button type="button" className="btn btn-secondary" onClick={handleHold}>
            {t('earlyrepay.hold' as TKey)}
          </button>
          <button type="button" className="btn btn-secondary" onClick={handlePartial}>
            {t('earlyrepay.partial' as TKey)}
          </button>
          <button type="button" className="btn btn-danger" onClick={handleFull}>
            {t('earlyrepay.full' as TKey)}
          </button>
        </>
      ) : (
        <>
          <button type="button" className="btn btn-secondary" onClick={handleCancel}>
            {t('common.cancel')}
          </button>
          <button type="button" className="btn btn-danger" onClick={handleConfirm}>
            {t('common.confirm')}
          </button>
        </>
      )}
    >
      {/* 18/04 AB-2：wealth-modal__body 标记驱动统一高度/收缩链（见 wealth-city3d.css）。 */}
      <div className="wealth-earlyrepay wealth-modal__body">
        {/* 机会成本提示 */}
        {showTriggerHint && (
          <div className="wealth-earlyrepay__trigger">
            <span className="wealth-earlyrepay__trigger-icon" aria-hidden="true">📊</span>
            <span>
              {t('earlyrepay.trigger' as TKey, {
                rate: (selectedLoan.annual_rate * 100).toFixed(2),
                yield: (investYield * 100).toFixed(2),
              })}
            </span>
          </div>
        )}

        {/* 贷款选择器（多笔房贷时） */}
        {mortgageLoans.length > 1 && mode === 'choose' && (
          <label className="wealth-earlyrepay__row">
            <span>{t('earlyrepay.loanLabel' as TKey)}</span>
            <select
              value={selectedLoan.id}
              onChange={(e) => {
                setLoanId(e.target.value);
                setFormError(null);
              }}
            >
              {mortgageLoans.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.id} · {t('wealth.loan.balance' as TKey)} ¥{formatCny(l.balance)} · {formatPct(l.annual_rate)}
                </option>
              ))}
            </select>
          </label>
        )}

        {/* 贷款详情 */}
        <div className="wealth-earlyrepay__details">
          <div className="wealth-earlyrepay__detail-row">
            <span>{t('earlyrepay.principal' as TKey)}</span>
            <b>¥{formatCny(selectedLoan.principal)}</b>
          </div>
          <div className="wealth-earlyrepay__detail-row">
            <span>{t('wealth.loan.balance' as TKey)}</span>
            <b>¥{formatCny(selectedLoan.balance)}</b>
          </div>
          <div className="wealth-earlyrepay__detail-row">
            <span>{t('wealth.loan.rate' as TKey)}</span>
            <b>{formatPct(selectedLoan.annual_rate, 2)}</b>
          </div>
          <div className="wealth-earlyrepay__detail-row">
            <span>{t('wealth.loan.monthly' as TKey)}</span>
            <b>¥{formatCny(selectedLoan.monthly_payment)} × {selectedLoan.months_left}</b>
          </div>
          <div className="wealth-earlyrepay__detail-row">
            <span>{t('earlyrepay.penalty' as TKey)}</span>
            <b className={penalty > 0 ? 'wealth-num--neg' : ''}>
              ¥{formatCny(penalty)}
            </b>
          </div>
          <div className="wealth-earlyrepay__detail-row">
            <span>{t('earlyrepay.savedInterest' as TKey)}</span>
            <b className="wealth-num--pos">¥{formatCny(savedInterest)}</b>
          </div>
        </div>

        {/* 部分还款金额 */}
        {mode === 'confirm-partial' && (
          <div className="wealth-earlyrepay__partial-info">
            <p>
              {t('earlyrepay.partialAmount' as TKey)}：<b>¥{formatCny(partialAmount)}</b>
            </p>
            <p>
              {t('earlyrepay.penalty' as TKey)}：<b className={partialPenalty > 0 ? 'wealth-num--neg' : ''}>¥{formatCny(partialPenalty)}</b>
            </p>
          </div>
        )}

        {/* 危险操作二次确认提示 */}
        {mode === 'confirm-full' && (
          <div className="wealth-earlyrepay__danger">
            <p className="wealth-earlyrepay__danger-title">
              ⚠️ {t('earlyrepay.dangerTitle' as TKey)}
            </p>
            <p>
              {t('earlyrepay.dangerBody' as TKey, {
                amount: formatCny(selectedLoan.balance + penalty),
              })}
            </p>
          </div>
        )}

        {formError && <div className="wealth-action-form__error" role="alert">{formError}</div>}
      </div>
    </AppModal>
  );
}
