/**
 * NegotiateModal — 议价弹窗：展示议价会话轮次记录（轮次 / 报价 / 留言），
 * 轮到我方时提供「接受 / 拒绝 / 还价」操作。
 *
 * 数据源：store.listingBook.negotiates（增量帧 game.negotiate_started /
 * game.negotiate_responded 合并）。
 * 发送：sendTrade({ type: 'negotiate_respond', action, offer_cny, comment })。
 *
 * 错误展示（§7.1）：弹窗内联红条（formError，弹窗不关闭）。
 */

import { useEffect, useMemo, useState } from 'react';
import { AppModal } from '@/components/ui/AppModal';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  type WealthGameState,
  type WealthNegotiateAction,
  type WealthNegotiateSession,
  type WealthTradeAction,
} from '@/types/wealth';
import { useWealthStore } from '@/store/wealth.store';

interface Props {
  open: boolean;
  gameState: WealthGameState | null;
  mySeat: number;
  /** 关联挂单 id（用于标题展示）。 */
  listingId: string;
  sendTrade: (action: WealthTradeAction) => void;
  onClose: () => void;
}

function seatName(gameState: WealthGameState | null, seat: number): string {
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

export function NegotiateModal({ open, gameState, mySeat, listingId, sendTrade, onClose }: Props) {
  const t = useT();
  const negotiates = useWealthStore((s) => s.listingBook?.negotiates ?? []);

  const [offer, setOffer] = useState('');
  const [comment, setComment] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  // 找到本挂单关联的、我方参与的活跃议价会话。
  const session: WealthNegotiateSession | undefined = useMemo(
    () =>
      negotiates.find(
        (n) =>
          n.listing_id === listingId &&
          n.status === 'active' &&
          (n.proposer_seat === mySeat || n.respond_seat === mySeat),
      ),
    [negotiates, listingId, mySeat],
  );

  // 是否轮到我方：最新报价方不是我方。
  const myTurn = useMemo(() => {
    if (!session) return false;
    return session.last_offer_by !== mySeat;
  }, [session, mySeat]);

  useEffect(() => {
    if (!open) {
      setFormError(null);
      setComment('');
    }
  }, [open]);

  if (!open) return null;

  const respond = (action: WealthNegotiateAction) => {
    if (!session) return;
    if (action === 'counter') {
      const v = Math.floor(Number(offer) || 0);
      if (v <= 0) {
        setFormError(t('wealth.negotiate.firstOffer' as TKey));
        return;
      }
      setFormError(null);
      sendTrade({ type: 'negotiate_respond', neg_id: session.id, action, offer_cny: v, comment: comment.slice(0, 60) || undefined });
    } else {
      setFormError(null);
      sendTrade({ type: 'negotiate_respond', neg_id: session.id, action, offer_cny: session.last_offer_cny });
    }
  };

  const title = session
    ? `${t('wealth.negotiate.title' as TKey)} · ${listingId}`
    : `${t('wealth.negotiate.start' as TKey)} · ${listingId}`;

  return (
    <AppModal
      title={title}
      icon="🤝"
      kind="info"
      maxWidth={500}
      onClose={onClose}
      footer={
        session ? (
          myTurn ? (
            <>
              <button type="button" className="btn btn-secondary" onClick={onClose}>
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn-danger" onClick={() => respond('reject')}>
                {t('wealth.negotiate.reject' as TKey)}
              </button>
              <button type="button" className="btn btn-primary" onClick={() => respond('accept')}>
                {t('wealth.negotiate.accept' as TKey)}
              </button>
            </>
          ) : (
            <button type="button" className="btn btn-secondary" onClick={onClose}>
              {t('common.cancel')}
            </button>
          )
        ) : (
          <>
            <button type="button" className="btn btn-secondary" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => {
                // 无会话时：以当前报价发起议价（父组件已通过 negotiate_start 创建会话；
                // 此处兜底，当会话尚未出现时允许直接发 start）。
                const v = Math.floor(Number(offer) || 0);
                if (v <= 0) {
                  setFormError(t('wealth.negotiate.firstOffer' as TKey));
                  return;
                }
                setFormError(null);
                sendTrade({ type: 'negotiate_start', listing_id: listingId, offer_cny: v });
              }}
            >
              {t('wealth.negotiate.start' as TKey)}
            </button>
          </>
        )
      }
    >
      <div className="wealth-negotiate">
        {session && session.turns.length > 0 && (
          <ul className="wealth-negotiate__history">
            {session.turns.map((turn, i) => (
              <li key={i} className={'wealth-negotiate__turn' + (turn.from === mySeat ? ' wealth-negotiate__turn--mine' : '')}>
                <span className="wealth-negotiate__who">{seatName(gameState, turn.from)}</span>
                <span className={'wealth-negotiate__action wealth-negotiate__action--' + turn.action}>
                  {t(`wealth.negotiate.${turn.action}` as TKey)}
                </span>
                <span className="wealth-negotiate__offer">¥{formatCny(turn.offer_cny)}</span>
                {turn.comment && <span className="wealth-negotiate__comment">“{turn.comment}”</span>}
              </li>
            ))}
          </ul>
        )}

        {session && !myTurn && (
          <p className="wealth-negotiate__waiting">{t('wealth.negotiate.waiting' as TKey)}</p>
        )}

        {session && session.status === 'deal' && (
          <p className="wealth-negotiate__deal">🎉 {t('wealth.negotiate.deal' as TKey)}</p>
        )}
        {session && (session.status === 'reject' || session.status === 'expired') && (
          <p className="wealth-negotiate__expired">
            {session.status === 'reject'
              ? t('wealth.negotiate.rejectMsg' as TKey)
              : t('wealth.negotiate.expired' as TKey)}
          </p>
        )}

        {/* 还价输入（仅轮到我方且会话仍活跃）。 */}
        {(!session || (myTurn && session.status === 'active')) && (
          <>
            <label className="wealth-negotiate__row">
              <span>{t('wealth.negotiate.offer' as TKey)}</span>
              <input
                type="number"
                min={0}
                step={1000}
                value={offer}
                onChange={(e) => setOffer(e.target.value)}
                placeholder={t('wealth.negotiate.offerPlaceholder' as TKey)}
              />
            </label>
            <label className="wealth-negotiate__row">
              <span>{t('wealth.negotiate.comment' as TKey)}（≤60）</span>
              <input
                type="text"
                maxLength={60}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder={t('wealth.negotiate.commentPlaceholder' as TKey)}
              />
            </label>
            {myTurn && (
              <button
                type="button"
                className="btn btn-secondary wealth-negotiate__counter"
                onClick={() => respond('counter')}
              >
                {t('wealth.negotiate.counter' as TKey)}
              </button>
            )}
          </>
        )}

        {formError && <div className="wealth-negotiate__error" role="alert">{formError}</div>}
      </div>
    </AppModal>
  );
}
