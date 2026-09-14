/**
 * ActionPanel — 底部动作条：14 个动作按钮（产品设计 §7.1，映射协议 §4 动作
 * 语义表）+ 「结束本月」。参数化动作弹 AppModal 表单；无参动作直接发送。
 *
 * 禁用态（UX 而非安全边界，服务端全量校验 §7.4）：status!=="playing" /
 * phase!=="acting" / 本月动作预算耗尽（eventFeed 本人 action/move 回执计数）。
 * 错误展示（§7.1）：弹窗内联红条（formError，弹窗不关闭）+ game.error 双通道
 * 全局 toast（useWealth 已上报）；面板级失败兜底 reportGlobalError。
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  WEALTH_ACTIONS,
  WEALTH_DISTRICTS,
  WEALTH_SUBMIT_MONTH,
  wealthDistrict,
  type WealthAction,
  type WealthActionMeta,
  type WealthDistrictId,
  type WealthGameState,
} from '@/types/wealth';
import { selectActionsUsedThisMonth, WEALTH_ACTION_BUDGET, useWealthStore } from '@/store/wealth.store';

const AWAIT_TIMEOUT_MS = 8000;

type LoanKind = 'consumer' | 'credit' | 'business';
type BizKind = 'delivery' | 'content' | 'tutoring' | 'freelance';

interface Props {
  roomId: string;
  gameState: WealthGameState | null;
  mySeat: number;
  /** 发送函数由 useWealth 提供（页面注入）。 */
  sendAction: (action: WealthAction) => void;
}

export function ActionPanel({ roomId, gameState, mySeat, sendAction }: Props) {
  const t = useT();
  const eventFeed = useWealthStore((s) => s.eventFeed);
  const [active, setActive] = useState<WealthActionMeta | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [awaiting, setAwaiting] = useState(false);

  // 表单字段（同一时刻只开一个弹窗，共用状态）。
  const [assetKind, setAssetKind] = useState('stock_index');
  const [amount, setAmount] = useState('');
  const [units, setUnits] = useState('1');
  const [district, setDistrict] = useState<WealthDistrictId>('finance');
  const [ratio, setRatio] = useState(0.3);
  const [loanKind, setLoanKind] = useState<LoanKind>('consumer');
  const [loanId, setLoanId] = useState('');
  const [bizKind, setBizKind] = useState<BizKind>('delivery');
  const [reason, setReason] = useState('');
  const [repayFull, setRepayFull] = useState(false);

  const my = gameState?.my ?? null;
  const playing = gameState?.status === 'playing';
  const acting = gameState?.phase === 'acting';
  const actionsUsed = useMemo(
    () =>
      selectActionsUsedThisMonth({
        eventFeed,
        gameState,
        mySeat,
      }),
    [eventFeed, gameState, mySeat],
  );
  const budgetLeft = Math.max(0, WEALTH_ACTION_BUDGET - actionsUsed);
  const actionsDisabled = !playing || !acting || budgetLeft <= 0;
  const me = gameState?.players.find((p) => p.seat === mySeat);
  const stopped = !!me && !me.alive;

  // ── 动作结果回执（弹窗内联成功 / 失败；短窗口内仅接受最近一帧）──
  useEffect(() => {
    if (!awaiting) return;
    const off = wsClient.on((env: WsEnvelope) => {
      if (env.type === 'game.event') {
        const p = env.payload as { room_id?: string; seat?: number; type?: string; text?: string };
        if (p.room_id && p.room_id !== roomId) return;
        if (p.type === 'error') {
          setFormError(p.text || t('wealth.error.generic' as TKey));
          setAwaiting(false);
          return;
        }
        if (p.seat === mySeat && (p.type === 'action' || p.type === 'move')) {
          setAwaiting(false);
          setActive(null);
          setFormError(null);
        }
      } else if (env.type === 'game.error') {
        const p = env.payload as { code: number; message: string };
        setFormError(mapError(p.code, p.message));
        setAwaiting(false);
      }
    });
    const timer = window.setTimeout(() => {
      setFormError(t('wealth.error.timeout' as TKey));
      setAwaiting(false);
    }, AWAIT_TIMEOUT_MS);
    return () => {
      off();
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [awaiting, roomId, mySeat]);

  const mapError = useCallback(
    (code: number, message: string): string => {
      if (code === 35006) return t('wealth.error.budget' as TKey);
      if (code === 35007) return t('wealth.error.cash' as TKey);
      return message || t('wealth.error.generic' as TKey);
    },
    [t],
  );

  const openModal = (meta: WealthActionMeta) => {
    setFormError(null);
    setActive(meta);
    // 表单默认值。
    setAmount('');
    setUnits('1');
    setRatio(0.3);
    setRepayFull(false);
    const loans = my?.loans ?? [];
    setLoanId(loans.length > 0 ? loans[0].id : '');
    if (meta.type === 'buy_house') {
      // 默认推荐自己所在区。
      setDistrict(me?.district ?? 'finance');
    } else if (meta.type === 'move_district') {
      const first = WEALTH_DISTRICTS.find((d) => d.id !== me?.district);
      setDistrict(first ? first.id : 'finance');
    }
  };

  const closeModal = () => {
    if (awaiting) return; // 提交中锁定
    setActive(null);
    setFormError(null);
  };

  const fire = (action: WealthAction) => {
    setFormError(null);
    setAwaiting(true);
    sendAction(action);
  };

  const handleBtn = (meta: WealthActionMeta) => {
    if (actionsDisabled || stopped) return;
    if (meta.form === 'none') {
      fire({ type: meta.type } as WealthAction);
    } else {
      openModal(meta);
    }
  };

  const handleSubmit = () => {
    if (!active) return;
    const amt = Math.floor(Number(amount) || 0);
    const u = Math.floor(Number(units) || 0);
    try {
      switch (active.type) {
        case 'buy_asset':
          if (amt < 1000) throw new Error(t('wealth.action.minAmount' as TKey, { n: 1000 }));
          fire({ type: 'buy_asset', asset: assetKind as 'stock_index' | 'bond' | 'gold', amount_cny: amt });
          return;
        case 'sell_asset': {
          if (!assetKind) throw new Error(t('wealth.action.pickAsset' as TKey));
          const held = my?.assets.find((a) => a.kind === assetKind);
          const maxUnits = held?.units ?? 0;
          if (u < 1 || u > maxUnits) throw new Error(t('wealth.action.unitsRange' as TKey, { n: maxUnits }));
          fire({ type: 'sell_asset', asset: assetKind, units: u });
          return;
        }
        case 'buy_house': {
          const def = wealthDistrict(district);
          if (!def) throw new Error(t('wealth.action.pickDistrict' as TKey));
          fire({ type: 'buy_house', district, downpay_ratio: ratio });
          return;
        }
        case 'take_loan': {
          if (loanKind === 'credit' && amt !== 50000 && amt !== 100000 && amt !== 200000) {
            throw new Error(t('wealth.action.creditTiers' as TKey));
          }
          if (amt < 1000) throw new Error(t('wealth.action.minAmount' as TKey, { n: 1000 }));
          fire({ type: 'take_loan', kind: loanKind, amount_cny: amt });
          return;
        }
        case 'repay_loan': {
          const loan = my?.loans.find((l) => l.id === loanId);
          if (!loan) throw new Error(t('wealth.action.noLoan' as TKey));
          const value = repayFull ? Math.ceil(loan.balance) : amt;
          if (!repayFull && value < 10000) {
            throw new Error(t('wealth.action.repayMin' as TKey, { n: 10000 }));
          }
          if (value > loan.balance) throw new Error(t('wealth.action.repayOver' as TKey));
          fire({ type: 'repay_loan', loan_id: loan.id, amount_cny: value });
          return;
        }
        case 'start_side_business':
          fire({ type: 'start_side_business', kind: bizKind });
          return;
        case 'move_district':
          fire({ type: 'move_district', district });
          return;
        case 'consume':
          if (amt < 1) throw new Error(t('wealth.action.minAmount' as TKey, { n: 1 }));
          fire({ type: 'consume', amount_cny: amt, ...(reason ? { reason: reason.slice(0, 40) } : {}) });
          return;
        case 'donate':
          if (amt < 1000) throw new Error(t('wealth.action.minAmount' as TKey, { n: 1000 }));
          fire({ type: 'donate', amount_cny: amt });
          return;
        default:
          return;
      }
    } catch (e) {
      setFormError(e instanceof Error ? e.message : String(e));
    }
  };

  // 买房估算：房价 = 基准万 × 10000 × beta × price_index（《后端架构》§4）。
  const houseDef = wealthDistrict(district);
  const housePriceIdx = gameState?.market.districts.find((d) => d.id === district)?.price_index ?? 1;
  const housePrice = houseDef ? houseDef.basePriceWan * 10000 * houseDef.houseBeta * housePriceIdx : 0;
  const downpay = housePrice * ratio;

  const sellableAssets = (my?.assets ?? []).filter((a) =>
    a.kind === 'stock_index' || a.kind === 'bond' || a.kind === 'gold' || a.kind.startsWith('house:') || a.kind.startsWith('shop:'),
  );

  const modalTitle = active
    ? `${active.icon} ${t(`wealth.action.${active.i18nKey}` as TKey)}`
    : '';
  const modalBody = !active ? null : (
    <div className="wealth-action-form">
      {active.type === 'buy_asset' && (
        <>
          <label className="wealth-action-form__row">
            <span>{t('wealth.action.asset' as TKey)}</span>
            <select
              value={assetKind}
              onChange={(e) => setAssetKind(e.target.value)}
              disabled={awaiting}
            >
              <option value="stock_index">{t('wealth.asset.stock' as TKey)}</option>
              <option value="bond">{t('wealth.asset.bond' as TKey)}</option>
              <option value="gold">{t('wealth.asset.gold' as TKey)}</option>
            </select>
          </label>
          <label className="wealth-action-form__row">
            <span>{t('wealth.action.amount' as TKey)}（≥ ¥1,000）</span>
            <input
              type="number"
              min={1000}
              step={1000}
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              disabled={awaiting}
              placeholder="10000"
            />
          </label>
        </>
      )}

      {active.type === 'sell_asset' && (
        <>
          <label className="wealth-action-form__row">
            <span>{t('wealth.action.asset' as TKey)}</span>
            <select
              value={assetKind}
              onChange={(e) => {
                setAssetKind(e.target.value);
                setUnits('1');
              }}
              disabled={awaiting}
            >
              <option value="">{t('wealth.action.pickAsset' as TKey)}</option>
              {sellableAssets.map((a) => (
                <option key={a.kind} value={a.kind}>
                  {a.name} ×{a.units}（¥{Math.round(a.price)}）
                </option>
              ))}
            </select>
          </label>
          <label className="wealth-action-form__row">
            <span>{t('wealth.action.units' as TKey)}</span>
            <input
              type="number"
              min={1}
              step={1}
              value={units}
              onChange={(e) => setUnits(e.target.value)}
              disabled={awaiting}
            />
          </label>
        </>
      )}

      {active.type === 'buy_house' && (
        <>
          <label className="wealth-action-form__row">
            <span>{t('wealth.action.district' as TKey)}</span>
            <select
              value={district}
              onChange={(e) => setDistrict(e.target.value as WealthDistrictId)}
              disabled={awaiting}
            >
              {WEALTH_DISTRICTS.map((d) => (
                <option key={d.id} value={d.id}>{d.nameZh}</option>
              ))}
            </select>
          </label>
          <label className="wealth-action-form__row">
            <span>{t('wealth.action.downpay' as TKey)}：{(ratio * 100).toFixed(0)}%</span>
            <input
              type="range"
              min={0.3}
              max={1}
              step={0.05}
              value={ratio}
              onChange={(e) => setRatio(Number(e.target.value))}
              disabled={awaiting}
            />
          </label>
          <p className="wealth-action-form__hint">
            {t('wealth.action.housePrice' as TKey)}：¥{Math.round(housePrice).toLocaleString('zh-CN')} ·{' '}
            {t('wealth.action.downpayAmount' as TKey)}：¥{Math.round(downpay).toLocaleString('zh-CN')}
          </p>
        </>
      )}

      {active.type === 'take_loan' && (
        <>
          <label className="wealth-action-form__row">
            <span>{t('wealth.action.loanKind' as TKey)}</span>
            <select
              value={loanKind}
              onChange={(e) => {
                setLoanKind(e.target.value as LoanKind);
                setAmount('');
              }}
              disabled={awaiting}
            >
              <option value="consumer">{t('wealth.loan.consumer' as TKey)}</option>
              <option value="credit">{t('wealth.loan.credit' as TKey)}</option>
              <option value="business">{t('wealth.loan.business' as TKey)}</option>
            </select>
          </label>
          {loanKind === 'credit' ? (
            <div className="wealth-action-form__tiers">
              {[50000, 100000, 200000].map((v) => (
                <button
                  key={v}
                  type="button"
                  className={'wealth-tier-btn' + (amount === String(v) ? ' wealth-tier-btn--active' : '')}
                  onClick={() => setAmount(String(v))}
                  disabled={awaiting}
                >
                  ¥{v / 10000}万
                </button>
              ))}
            </div>
          ) : (
            <label className="wealth-action-form__row">
              <span>{t('wealth.action.loanAmount' as TKey)}</span>
              <input
                type="number"
                min={1000}
                step={1000}
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                disabled={awaiting}
                placeholder="50000"
              />
            </label>
          )}
        </>
      )}

      {active.type === 'repay_loan' && (
        <>
          <label className="wealth-action-form__row">
            <span>{t('wealth.loans' as TKey)}</span>
            <select
              value={loanId}
              onChange={(e) => setLoanId(e.target.value)}
              disabled={awaiting}
            >
              {(my?.loans ?? []).map((l) => (
                <option key={l.id} value={l.id}>
                  {l.id} · 余额 ¥{Math.round(l.balance).toLocaleString('zh-CN')}
                </option>
              ))}
            </select>
          </label>
          <label className="wealth-action-form__check">
            <input
              type="checkbox"
              checked={repayFull}
              onChange={(e) => setRepayFull(e.target.checked)}
              disabled={awaiting}
            />
            {t('wealth.action.repayFull' as TKey)}
          </label>
          {!repayFull && (
            <label className="wealth-action-form__row">
              <span>{t('wealth.action.amount' as TKey)}（≥ ¥10,000）</span>
              <input
                type="number"
                min={10000}
                step={1000}
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                disabled={awaiting}
                placeholder="10000"
              />
            </label>
          )}
        </>
      )}

      {active.type === 'start_side_business' && (
        <label className="wealth-action-form__row">
          <span>{t('wealth.action.businessKind' as TKey)}</span>
          <select
            value={bizKind}
            onChange={(e) => setBizKind(e.target.value as BizKind)}
            disabled={awaiting}
          >
            <option value="delivery">{t('wealth.biz.delivery' as TKey)}</option>
            <option value="content">{t('wealth.biz.content' as TKey)}（K≥2）</option>
            <option value="freelance">{t('wealth.biz.freelance' as TKey)}（K≥3）</option>
            <option value="tutoring">{t('wealth.biz.tutoring' as TKey)}（K≥4）</option>
          </select>
        </label>
      )}

      {active.type === 'move_district' && (
        <label className="wealth-action-form__row">
          <span>{t('wealth.action.district' as TKey)}（¥3,000 / 精力-1）</span>
          <select
            value={district}
            onChange={(e) => setDistrict(e.target.value as WealthDistrictId)}
            disabled={awaiting}
          >
            {WEALTH_DISTRICTS.filter((d) => d.id !== me?.district).map((d) => (
              <option key={d.id} value={d.id}>{d.nameZh}</option>
            ))}
          </select>
        </label>
      )}

      {(active.type === 'consume' || active.type === 'donate') && (
        <label className="wealth-action-form__row">
          <span>
            {t('wealth.action.amount' as TKey)}
            {active.type === 'donate' ? '（≥ ¥1,000）' : '（≥ ¥1）'}
          </span>
          <input
            type="number"
            min={active.type === 'donate' ? 1000 : 1}
            step={100}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            disabled={awaiting}
            placeholder={active.type === 'donate' ? '10000' : '500'}
          />
        </label>
      )}

      {active.type === 'consume' && (
        <label className="wealth-action-form__row">
          <span>{t('wealth.action.reason' as TKey)}（≤40）</span>
          <input
            type="text"
            maxLength={40}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            disabled={awaiting}
            placeholder="…"
          />
        </label>
      )}

      {formError && <div className="wealth-action-form__error" role="alert">{formError}</div>}
    </div>
  );

  return (
    <div className="wealth-actionbar">
      <div className="wealth-actionbar__budget">
        <span className="wealth-badge wealth-badge--budget">
          {t('wealth.actionBudget' as TKey, { n: budgetLeft })}
        </span>
      </div>
      <div className="wealth-actionbar__buttons">
        {WEALTH_ACTIONS.map((meta) => {
          const disabled = actionsDisabled || stopped ||
            (meta.type === 'stop_side_business' && !(my?.assets ?? []).some((a) => a.kind === 'side_business'));
          return (
            <button
              key={meta.type}
              type="button"
              className="wealth-action-btn"
              disabled={disabled}
              title={meta.hint}
              onClick={() => handleBtn(meta)}
            >
              <span className="wealth-action-btn__icon">{meta.icon}</span>
              <span className="wealth-action-btn__label">
                {t(`wealth.action.${meta.i18nKey}` as TKey)}
              </span>
            </button>
          );
        })}
        <button
          type="button"
          className="wealth-action-btn wealth-action-btn--submit"
          disabled={!playing || !acting || stopped}
          title={WEALTH_SUBMIT_MONTH.hint}
          onClick={() => handleBtn(WEALTH_SUBMIT_MONTH)}
        >
          <span className="wealth-action-btn__icon">{WEALTH_SUBMIT_MONTH.icon}</span>
          <span className="wealth-action-btn__label">
            {t(`wealth.action.${WEALTH_SUBMIT_MONTH.i18nKey}` as TKey)}
          </span>
        </button>
      </div>

      {active && (
        <AppModal
          title={modalTitle}
          icon={active.icon}
          kind="info"
          maxWidth={460}
          dismissible={!awaiting}
          blockBackdropClose
          loading={awaiting}
          onClose={closeModal}
          footer={
            <>
              <button type="button" className="btn btn-secondary" onClick={closeModal} disabled={awaiting}>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn-primary" onClick={handleSubmit} disabled={awaiting}>
                {awaiting ? t('common.loading') : t('wealth.action.confirm' as TKey)}
              </button>
            </>
          }
        >
          {modalBody}
        </AppModal>
      )}
    </div>
  );
}
