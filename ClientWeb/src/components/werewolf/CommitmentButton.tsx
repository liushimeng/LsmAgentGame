// CommitmentButton.tsx — 做出承诺按钮 + 弹窗（§20260810-06）。
//
// 白天发言阶段，玩家可点击此按钮做出公开承诺。

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import { wsClient } from '../../services/ws';

interface CommitmentButtonProps {
  roomId: string;
  mySeat: number;
  aliveSeats: number[];
  disabled?: boolean;
}

const TEMPLATES = [
  { value: 'seer_check', label: '🔮 如果我是预言家，今晚验 N 号' },
  { value: 'vote_target', label: '🗳️ 如果 N 号是狼，我明天投票放逐他' },
  { value: 'no_vote_for', label: '🚫 本轮我不会投票给 N 号' },
  { value: 'no_use_skill', label: '🛡️ 我今晚不会使用技能' },
  { value: 'apology_if_good', label: '🙏 N 号如果是好人，我公开道歉' },
];

export function CommitmentButton({ roomId, mySeat, aliveSeats, disabled }: CommitmentButtonProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [template, setTemplate] = useState('seer_check');
  const [targetSeat, setTargetSeat] = useState(-1);
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const needsTarget = template !== 'no_use_skill';

  const handleSubmit = async () => {
    if (needsTarget && targetSeat < 0) {
      setError(t('werewolf.commitment.selectTarget'));
      return;
    }
    if (!reason.trim()) {
      setError(t('werewolf.commitment.reasonRequired'));
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      wsClient.send('game.werewolf_commit', {
        room_id: roomId,
        template,
        target: needsTarget ? targetSeat : -1,
        reason: reason.trim().slice(0, 30),
      });
      setOpen(false);
      setReason('');
      setTargetSeat(-1);
    } catch (e: any) {
      setError(e.message || t('werewolf.commitment.submitFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <button
        className="btn btn--secondary commitment-button"
        onClick={() => setOpen(true)}
        disabled={disabled}
        title={t('werewolf.commitment.buttonTitle')}
      >
        📝 {t('werewolf.commitment.button')}
      </button>

      {open && (
        <div className="commitment-modal-overlay" onClick={() => setOpen(false)}>
          <div className="commitment-modal" onClick={e => e.stopPropagation()}>
            <h3 className="commitment-modal__title">📝 {t('werewolf.commitment.modalTitle')}</h3>
            <p className="commitment-modal__hint">
              {t('werewolf.commitment.modalHint')}
            </p>

            <div className="commitment-modal__field">
              <label>{t('werewolf.commitment.templateLabel')}</label>
              <select
                value={template}
                onChange={e => setTemplate(e.target.value)}
                className="commitment-modal__select"
              >
                {TEMPLATES.map(tpl => (
                  <option key={tpl.value} value={tpl.value}>
                    {tpl.label}
                  </option>
                ))}
              </select>
            </div>

            {needsTarget && (
              <div className="commitment-modal__field">
                <label>{t('werewolf.commitment.targetLabel')}</label>
                <select
                  value={targetSeat}
                  onChange={e => setTargetSeat(Number(e.target.value))}
                  className="commitment-modal__select"
                >
                  <option value={-1}>{t('werewolf.commitment.selectPlaceholder')}</option>
                  {aliveSeats
                    .filter(s => s !== mySeat)
                    .map(s => (
                      <option key={s} value={s}>
                        {s + 1}{t('werewolf.commitment.seatSuffix')}
                      </option>
                    ))}
                </select>
              </div>
            )}

            <div className="commitment-modal__field">
              <label>{t('werewolf.commitment.reasonLabel')}</label>
              <input
                type="text"
                value={reason}
                onChange={e => setReason(e.target.value)}
                maxLength={30}
                placeholder={t('werewolf.commitment.reasonPlaceholder')}
                className="commitment-modal__input"
              />
            </div>

            {error && <div className="commitment-modal__error">{error}</div>}

            <div className="commitment-modal__actions">
              <button
                className="btn btn--secondary"
                onClick={() => setOpen(false)}
                disabled={submitting}
              >
                {t('common.cancel')}
              </button>
              <button
                className="btn btn--primary"
                onClick={handleSubmit}
                disabled={submitting}
              >
                {submitting ? t('common.loading') : t('werewolf.commitment.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
