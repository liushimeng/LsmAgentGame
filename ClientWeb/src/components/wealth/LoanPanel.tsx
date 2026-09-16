/**
 * LoanPanel — 借贷面板：玩家间借贷市场（loan_ofr 出借挂单 / loan_req 借款挂单）
 * + 已生效的 P2P 借贷合约（我的负债 / 我的债权）。
 *
 * 操作：发布借贷挂单、接受挂单（匹配生成合约）、提前还款、为借款添加担保人。
 * 利率约束（§6.2）：0.3%/月（年化 3.6%）~ 3.6%/月（年化 43.2%）。
 *
 * 数据源：store.listingBook.p2p_loans + listings（type=loan_ofr/loan_req）。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  formatPct,
  type WealthGameState,
  type WealthListing,
  type WealthLoanDirection,
  type WealthMyState,
  type WealthP2PLoan,
  type WealthTradeAction,
} from '@/types/wealth';
import { useWealthStore } from '@/store/wealth.store';

interface Props {
  gameState: WealthGameState | null;
  mySeat: number;
  my: WealthMyState | null;
  sendTrade: (action: WealthTradeAction) => void;
  onRefresh: () => void;
}

function seatName(gameState: WealthGameState | null, seat: number): string {
  if (seat < 0) return '—';
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

type LoanTab = 'market' | 'mine';

export function LoanPanel({ gameState, mySeat, sendTrade, onRefresh }: Props) {
  const t = useT();
  const listingBook = useWealthStore((s) => s.listingBook);
  const listings = listingBook?.listings ?? [];
  const loans = listingBook?.p2p_loans ?? [];

  const [tab, setTab] = useState<LoanTab>('market');
  // 发布借贷表单字段。
  const [direction, setDirection] = useState<WealthLoanDirection>('lend');
  const [principal, setPrincipal] = useState('10000');
  const [maxRate, setMaxRate] = useState('1.0');
  const [termN, setTermN] = useState('6');
  const [needGuarantee, setNeedGuarantee] = useState(false);

  const marketListings = useMemo(
    () => listings.filter((l) => l.type === 'loan_ofr' || l.type === 'loan_req'),
    [listings],
  );

  const myLoans = useMemo(
    () => loans.filter((l) => l.borrower_seat === mySeat || l.lender_seat === mySeat),
    [loans, mySeat],
  );

  const myBorrowed = myLoans.filter((l) => l.borrower_seat === mySeat);
  const myLent = myLoans.filter((l) => l.lender_seat === mySeat);

  const isMine = (l: WealthListing) => l.seat === mySeat;

  const handleCreate = () => {
    const p = Math.floor(Number(principal) || 0);
    const r = Number(maxRate) || 0;
    const tn = Math.floor(Number(termN) || 0);
    if (p < 1000) {
      alert(t('wealth.action.minAmount' as TKey, { n: 1000 }));
      return;
    }
    if (r < 0.3 || r > 3.6) {
      alert(t('wealth.error.loanRate' as TKey));
      return;
    }
    if (tn < 1 || tn > 60) return;
    sendTrade({
      type: 'listing_create',
      listing_type: direction === 'lend' ? 'loan_ofr' : 'loan_req',
      payload: { loan: { direction, principal_cny: p, max_rate: r, term_n: tn, need_guarantee: needGuarantee } },
      ask_cny: 0,
    });
  };

  const handleAccept = (l: WealthListing) => {
    if (isMine(l)) return;
    sendTrade({ type: 'loan_accept', listing_id: l.id });
  };

  const handleRepay = (loan: WealthP2PLoan, full: boolean) => {
    if (full) sendTrade({ type: 'loan_repay', loan_id: loan.id, amount_cny: Math.ceil(loan.balance_cny) });
    else sendTrade({ type: 'loan_repay', loan_id: loan.id });
  };

  const handleGuarant = (loan: WealthP2PLoan) => {
    sendTrade({ type: 'add_guarantor', loan_id: loan.id });
  };

  if (!gameState) {
    return <div className="wealth-panel__empty"><p>{t('wealth.panel.waiting' as TKey)}</p></div>;
  }

  return (
    <div className="wealth-loanpanel">
      <div className="wealth-loanpanel__head">
        <span className="wealth-loanpanel__title">{t('wealth.loan.title' as TKey)}</span>
        <button type="button" className="wealth-listingpanel__refresh" onClick={onRefresh} title={t('wealth.listing.view' as TKey)}>↻</button>
      </div>
      <p className="wealth-loanpanel__subtitle">{t('wealth.loan.subtitle' as TKey)}</p>

      <div className="wealth-tabs">
        <button
          type="button"
          className={'wealth-tabs__btn' + (tab === 'market' ? ' wealth-tabs__btn--active' : '')}
          onClick={() => setTab('market')}
        >
          {t('wealth.loan.market' as TKey)}
        </button>
        <button
          type="button"
          className={'wealth-tabs__btn' + (tab === 'mine' ? ' wealth-tabs__btn--active' : '')}
          onClick={() => setTab('mine')}
        >
          {t('wealth.loan.mine' as TKey)}
        </button>
      </div>

      {tab === 'market' && (
        <>
          <table className="wealth-table">
            <thead>
              <tr>
                <th>{t('wealth.loan.col.id' as TKey)}</th>
                <th>{t('wealth.loan.col.direction' as TKey)}</th>
                <th>{t('wealth.loan.col.seat' as TKey)}</th>
                <th>{t('wealth.loan.col.principal' as TKey)}</th>
                <th>{t('wealth.loan.col.rate' as TKey)}</th>
                <th>{t('wealth.loan.col.term' as TKey)}</th>
                <th>{t('wealth.loan.col.actions' as TKey)}</th>
              </tr>
            </thead>
            <tbody>
              {marketListings
                .filter((l) => l.status === 'open')
                .map((l) => {
                  const pl = l.payload.loan;
                  if (!pl) return null;
                  return (
                    <tr key={l.id} className={isMine(l) ? 'wealth-table__row--mine' : ''}>
                      <td className="wealth-table__id">{l.id}</td>
                      <td>
                        <span className={'wealth-badge wealth-badge--dir-' + pl.direction}>
                          {t(`wealth.loan.${pl.direction}` as TKey)}
                        </span>
                      </td>
                      <td>{seatName(gameState, l.seat)}</td>
                      <td>¥{formatCny(pl.principal_cny)}</td>
                      <td>{formatPct(pl.max_rate / 100, 1)}/月</td>
                      <td>{pl.term_n}月</td>
                      <td>
                        {isMine(l) ? (
                          <button type="button" className="btn btn-mini btn-danger"
                            onClick={() => sendTrade({ type: 'listing_cancel', listing_id: l.id })}>
                            {t('wealth.listing.cancel' as TKey)}
                          </button>
                        ) : (
                          <button type="button" className="btn btn-mini btn-primary" onClick={() => handleAccept(l)}>
                            {t('wealth.loan.acceptBtn' as TKey)}
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              {marketListings.filter((l) => l.status === 'open').length === 0 && (
                <tr><td colSpan={7} className="wealth-table__empty">{t('wealth.loan.empty' as TKey)}</td></tr>
              )}
            </tbody>
          </table>

          {/* 发布借贷表单 */}
          <div className="wealth-loanpanel__create">
            <div className="wealth-loanpanel__create-title">{t('wealth.loan.createTitle' as TKey)}</div>
            <div className="wealth-loanpanel__form">
              <label className="wealth-action-form__row">
                <span>{t('wealth.loan.selDirection' as TKey)}</span>
                <select value={direction} onChange={(e) => setDirection(e.target.value as WealthLoanDirection)}>
                  <option value="lend">{t('wealth.loan.lend' as TKey)}</option>
                  <option value="borrow">{t('wealth.loan.borrow' as TKey)}</option>
                </select>
              </label>
              <label className="wealth-action-form__row">
                <span>{t('wealth.loan.principal' as TKey)}（≥1000）</span>
                <input type="number" min={1000} step={1000} value={principal} onChange={(e) => setPrincipal(e.target.value)} />
              </label>
              <label className="wealth-action-form__row">
                <span>{t('wealth.loan.maxRate' as TKey)}（0.3–3.6%）</span>
                <input type="number" min={0.3} max={3.6} step={0.1} value={maxRate} onChange={(e) => setMaxRate(e.target.value)} />
              </label>
              <label className="wealth-action-form__row">
                <span>{t('wealth.loan.termN' as TKey)}（1–60）</span>
                <input type="number" min={1} max={60} step={1} value={termN} onChange={(e) => setTermN(e.target.value)} />
              </label>
              {direction === 'borrow' && (
                <label className="wealth-action-form__check">
                  <input type="checkbox" checked={needGuarantee} onChange={(e) => setNeedGuarantee(e.target.checked)} />
                  {t('wealth.loan.needGuarantee' as TKey)}
                </label>
              )}
              <button type="button" className="btn btn-primary" onClick={handleCreate}>
                {t('wealth.listing.confirmCreate' as TKey)}
              </button>
            </div>
          </div>
        </>
      )}

      {tab === 'mine' && (
        <>
          {myBorrowed.length > 0 && (
            <div className="wealth-loanpanel__section-title">{t('wealth.loan.borrow' as TKey)}</div>
          )}
          <table className="wealth-table">
            <thead>
              <tr>
                <th>{t('wealth.loan.col.id' as TKey)}</th>
                <th>{t('wealth.loan.col.principal' as TKey)}</th>
                <th>{t('wealth.loan.col.rate' as TKey)}</th>
                <th>{t('wealth.loan.col.balance' as TKey)}</th>
                <th>{t('wealth.loan.col.monthsLeft' as TKey)}</th>
                <th>{t('wealth.loan.col.guarantor' as TKey)}</th>
                <th>{t('wealth.loan.col.actions' as TKey)}</th>
              </tr>
            </thead>
            <tbody>
              {myBorrowed.map((l) => (
                <tr key={l.id} className={l.overdue ? 'wealth-table__row--overdue' : ''}>
                  <td className="wealth-table__id">{l.id}</td>
                  <td>¥{formatCny(l.principal_cny)}</td>
                  <td>{formatPct(l.annual_rate, 1)}</td>
                  <td className="wealth-num--neg">¥{formatCny(l.balance_cny)}</td>
                  <td>{l.months_left}月</td>
                  <td>
                    {l.guarantor_seat >= 0 ? (
                      seatName(gameState, l.guarantor_seat)
                    ) : (
                      <button type="button" className="btn btn-mini btn-secondary" onClick={() => handleGuarant(l)}>
                        {t('wealth.loan.addGuarantor' as TKey)}
                      </button>
                    )}
                    {l.overdue && <span className="wealth-badge wealth-badge--overdue">{t('wealth.loan.overdueBadge' as TKey)}</span>}
                  </td>
                  <td>
                    <button type="button" className="btn btn-mini btn-primary" onClick={() => handleRepay(l, false)}>
                      {t('wealth.loan.repayBtn' as TKey)}
                    </button>
                    <button type="button" className="btn btn-mini btn-secondary" onClick={() => handleRepay(l, true)}>
                      {t('wealth.loan.repayFull' as TKey)}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {myBorrowed.length === 0 && <p className="wealth-table__empty">{t('wealth.loan.empty' as TKey)}</p>}

          {myLent.length > 0 && <div className="wealth-loanpanel__section-title">{t('wealth.loan.lend' as TKey)}</div>}
          {myLent.length > 0 && (
            <table className="wealth-table">
              <thead>
                <tr>
                  <th>{t('wealth.loan.col.id' as TKey)}</th>
                  <th>{t('wealth.loan.col.seat' as TKey)}</th>
                  <th>{t('wealth.loan.col.principal' as TKey)}</th>
                  <th>{t('wealth.loan.col.balance' as TKey)}</th>
                  <th>{t('wealth.loan.col.monthsLeft' as TKey)}</th>
                </tr>
              </thead>
              <tbody>
                {myLent.map((l) => (
                  <tr key={l.id}>
                    <td className="wealth-table__id">{l.id}</td>
                    <td>{seatName(gameState, l.borrower_seat)}</td>
                    <td>¥{formatCny(l.principal_cny)}</td>
                    <td className="wealth-num--pos">¥{formatCny(l.balance_cny)}</td>
                    <td>{l.months_left}月</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </div>
  );
}
