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
  type VirtualCityGameState,
  type VirtualCityNegotiateAction,
  type VirtualCityNegotiateSession,
  type VirtualCityTradeAction,
} from '@/types/virtualCity';
import { useVirtualCityStore } from '@/store/virtualCity.store';

interface Props {
  open: boolean;
  gameState: VirtualCityGameState | null;
  mySeat: number;
  /** 关联挂单 id（用于标题展示）。 */
  listingId: string;
  sendTrade: (action: VirtualCityTradeAction) => void;
  onClose: () => void;
}

function seatName(gameState: VirtualCityGameState | null, seat: number): string {
  const p = gameState?.players.find((x) => x.seat === seat);
  return p ? `#${seat + 1} ${p.nickname}` : `#${seat + 1}`;
}

export function NegotiateModal({ open, gameState, mySeat, listingId, sendTrade, onClose }: Props) {
  const t = useT();
  const negotiates = useVirtualCityStore((s) => s.listingBook?.negotiates ?? []);

  const [offer, setOffer] = useState('');
  const [comment, setComment] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  // 找到本挂单关联的、我方参与的活跃议价会话。
  const session: VirtualCityNegotiateSession | undefined = useMemo(
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

  const respond = (action: VirtualCityNegotiateAction) => {
    if (!session) return;
    if (action === 'counter') {
      const v = Math.floor(Number(offer) || 0);
      if (v <= 0) {
        setFormError(t('virtualCity.negotiate.firstOffer' as TKey));
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
    ? `${t('virtualCity.negotiate.title' as TKey)} · ${listingId}`
    : `${t('virtualCity.negotiate.start' as TKey)} · ${listingId}`;

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
                {t('virtualCity.negotiate.reject' as TKey)}
              </button>
              <button type="button" className="btn btn-primary" onClick={() => respond('accept')}>
                {t('virtualCity.negotiate.accept' as TKey)}
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
                  setFormError(t('virtualCity.negotiate.firstOffer' as TKey));
                  return;
                }
                setFormError(null);
                sendTrade({ type: 'negotiate_start', listing_id: listingId, offer_cny: v });
              }}
            >
              {t('virtualCity.negotiate.start' as TKey)}
            </button>
          </>
        )
      }
    >
      {/* 18/04 AB-2：virtualCity-modal__body 标记驱动统一高度/收缩链（见 virtualCity-city3d.css）。 */}
      <div className="virtualCity-negotiate virtualCity-modal__body">
        {session && session.turns.length > 0 && (
          <ul className="virtualCity-negotiate__history">
            {session.turns.map((turn, i) => (
              <li key={i} className={'virtualCity-negotiate__turn' + (turn.from === mySeat ? ' virtualCity-negotiate__turn--mine' : '')}>
                <span className="virtualCity-negotiate__who">{seatName(gameState, turn.from)}</span>
                <span className={'virtualCity-negotiate__action virtualCity-negotiate__action--' + turn.action}>
                  {t(`virtualCity.negotiate.${turn.action}` as TKey)}
                </span>
                <span className="virtualCity-negotiate__offer">¥{formatCny(turn.offer_cny)}</span>
                {turn.comment && <span className="virtualCity-negotiate__comment">“{turn.comment}”</span>}
              </li>
            ))}
          </ul>
        )}

        {session && !myTurn && (
          <p className="virtualCity-negotiate__waiting">{t('virtualCity.negotiate.waiting' as TKey)}</p>
        )}

        {session && session.status === 'deal' && (
          <p className="virtualCity-negotiate__deal">🎉 {t('virtualCity.negotiate.deal' as TKey)}</p>
        )}
        {session && (session.status === 'reject' || session.status === 'expired') && (
          <p className="virtualCity-negotiate__expired">
            {session.status === 'reject'
              ? t('virtualCity.negotiate.rejectMsg' as TKey)
              : t('virtualCity.negotiate.expired' as TKey)}
          </p>
        )}

        {/* 还价输入（仅轮到我方且会话仍活跃）。 */}
        {(!session || (myTurn && session.status === 'active')) && (
          <>
            <label className="virtualCity-negotiate__row">
              <span>{t('virtualCity.negotiate.offer' as TKey)}</span>
              <input
                type="number"
                min={0}
                step={1000}
                value={offer}
                onChange={(e) => setOffer(e.target.value)}
                placeholder={t('virtualCity.negotiate.offerPlaceholder' as TKey)}
              />
            </label>
            <label className="virtualCity-negotiate__row">
              <span>{t('virtualCity.negotiate.comment' as TKey)}（≤60）</span>
              <input
                type="text"
                maxLength={60}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder={t('virtualCity.negotiate.commentPlaceholder' as TKey)}
              />
            </label>
            {myTurn && (
              <button
                type="button"
                className="btn btn-secondary virtualCity-negotiate__counter"
                onClick={() => respond('counter')}
              >
                {t('virtualCity.negotiate.counter' as TKey)}
              </button>
            )}
          </>
        )}

        {formError && <div className="virtualCity-negotiate__error" role="alert">{formError}</div>}
      </div>
    </AppModal>
  );
}
