/**
 * RoomCreateModal — 德州扑克创建房间弹窗
 *
 * 2026-08-19 §德州扑克Agent — 加 AI 配置块(0..6 bot 数量 slider + 模型选择)
 *
 * 2026-08-22 §BUG-TEXAS-CREATE-OVERLAY — 把盲注档位(BlindSelector)与
 * 买入(BuyinSlider)移入弹窗内部,避免全屏遮罩挡住页面层选择区导致
 * CDP 真实点击无响应。弹窗 owner 持有 bb/buyin,提交时作为 big_blind/
 * start_stack 一并回传(原 TexasHoldemLobbyPage 把选择器渲染在弹窗下方的
 * `.texas-create-dialog` 容器,被 `.thp-create-modal` 全屏遮罩
 * pointer-events:auto 罩住,人类无法切换档位)。
 *
 * 设计要点（沿用狼人杀 RoomCreateModal 模式,简化版）：
 * - 容量上限 6
 * - 无法官模式（v1.0 不实现）
 * - 无角色选择 / 难度档位 / AI 解说（v1.1 再加）
 */
import { useEffect, useMemo, useState } from 'react';
import { listModels, type ModelInfo } from '@/api/llm';
import { useT } from '@/hooks/useT';
import { BlindSelector, buyInRange } from '@/components/ui/BlindSelector';
import { BuyinSlider } from '@/components/ui/BuyinSlider';
import { useWallet } from '@/hooks/useWallet';
import type { TKey } from '@/i18n';

interface AgentSeatInput {
  seat: number;
  model_key: string;
}

interface Props {
  open: boolean;
  onClose: () => void;
  onSubmit: (req: {
    name?: string;
    agent_seats: AgentSeatInput[];
    big_blind: number;
    start_stack: number;
  }) => Promise<boolean>;
  submitting?: boolean;
}

const MAX_AI_SEATS = 6;
const ALL_SEATS = Array.from({ length: MAX_AI_SEATS }, (_, i) => i);

export function RoomCreateModal({ open, onClose, onSubmit, submitting = false }: Props) {
  const t = useT();
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [agentCount, setAgentCount] = useState(0);
  const [seats, setSeats] = useState<AgentSeatInput[]>([]);
  const [formError, setFormError] = useState<string | null>(null);
  const [localSubmitting, setLocalSubmitting] = useState(false);
  const [shuffleNonce, setShuffleNonce] = useState(0);

  // 2026-08-22 §BUG-TEXAS-CREATE-OVERLAY — bb/buyin 状态上移入弹窗 owner,
  // 选择器渲染在弹窗 body 内(全屏遮罩不再挡住)。外部页面 TexasHoldemLobbyPage
  // 不再持有这些状态,提交时由 onSubmit 一并回传。
  const [bb, setBb] = useState<number>(50);
  const [buyin, setBuyin] = useState<number>(buyInRange(bb).defaultBI);
  const { balance, refresh } = useWallet();

  const handleBbChange = (nextBb: number) => {
    const { defaultBI } = buyInRange(nextBb);
    setBb(nextBb);
    setBuyin(defaultBI);
  };

  // 弹窗打开时拉取钱包余额,确保买入档位的 insufficient 校验是最新值
  useEffect(() => {
    if (open) {
      void refresh();
    }
  }, [open, refresh]);

  useEffect(() => {
    if (!open) {
      setFormError(null);
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    listModels()
      .then((ms) => {
        if (cancelled) return;
        setModels(ms);
      })
      .catch((e) => {
        if (cancelled) return;
        setError(String(e?.message ?? e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  useEffect(() => {
    if (agentCount === 0) {
      setSeats([]);
      return;
    }
    if (models.length === 0) {
      setSeats((prev) => prev.slice(0, agentCount));
      return;
    }

    setSeats((prev) => {
      const shuffled = models.map((m) => m.model);
      for (let i = shuffled.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
      }
      const validModelKeys = new Set(models.map((m) => m.model));
      const usedModelKeys = new Set<string>();
      const usedSeatNumbers = new Set<number>();
      const next: AgentSeatInput[] = [];

      for (let i = 0; i < prev.length && i < agentCount; i++) {
        const existing = prev[i];
        if (existing && existing.model_key && validModelKeys.has(existing.model_key) &&
            existing.seat >= 0 && existing.seat < MAX_AI_SEATS && !usedSeatNumbers.has(existing.seat)) {
          next.push(existing);
          usedModelKeys.add(existing.model_key);
          usedSeatNumbers.add(existing.seat);
        }
      }

      const freeSeats: number[] = [];
      for (let s = 0; s < MAX_AI_SEATS; s++) {
        if (!usedSeatNumbers.has(s)) freeSeats.push(s);
      }
      for (let i = freeSeats.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [freeSeats[i], freeSeats[j]] = [freeSeats[j], freeSeats[i]];
      }
      let fillCursor = 0;
      for (let i = next.length; i < agentCount; i++) {
        const targetSeat = freeSeats[fillCursor];
        let pick = shuffled.find((k) => !usedModelKeys.has(k));
        if (pick === undefined) pick = shuffled[fillCursor % shuffled.length];
        next.push({ seat: targetSeat, model_key: pick });
        usedModelKeys.add(pick);
        usedSeatNumbers.add(targetSeat);
        fillCursor += 1;
      }

      return next;
    });
  }, [agentCount, models, shuffleNonce]);

  const usedSeats = useMemo(() => new Set(seats.map((s) => s.seat)), [seats]);

  const updateSeat = (idx: number, patch: Partial<AgentSeatInput>) => {
    setSeats((prev) =>
      prev.map((s, i) => (i === idx ? { ...s, ...patch } : s)),
    );
  };

  const valid = useMemo(() => {
    if (agentCount === 0) return true;
    if (models.length === 0) return false;
    if (seats.length !== agentCount) return false;
    // 2026-08-22 §BUG-TEXAS-ORPHAN-MODEL — 校验 model_key 必须在 /api/llm/models
    // 当前返回的注册表里(管理员下线某个模型后,旧座位值变成"孤儿",<select>
    // 显示空白且提交会被服务端拒)。用 Set 让 O(N) 而不是 O(N²)。
    const validModelKeys = new Set(models.map((m) => m.model));
    const seen = new Set<number>();
    for (const s of seats) {
      if (seen.has(s.seat)) return false;
      if (s.seat < 0 || s.seat >= MAX_AI_SEATS) return false;
      if (!s.model_key) return false;
      if (!validModelKeys.has(s.model_key)) return false;
      seen.add(s.seat);
    }
    return true;
  }, [agentCount, seats, models]);

  if (!open) return null;

  return (
    <div className="thp-create-modal" role="dialog" aria-modal="true">
      <div className="thp-create-modal__card">
        <header className="thp-create-modal__header">
          <h2>{t('texasholdem.createModal.title' as TKey)}</h2>
          <button
            className="thp-create-modal__close"
            onClick={onClose}
            data-testid="thp-create-modal__close"
          >
            ×
          </button>
        </header>

        <div className="thp-create-modal__body">
          <div className="thp-create-modal__row">
            <label className="thp-create-modal__field">
              <span>{t('texasholdem.createModal.roomName' as TKey)}</span>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('texasholdem.createModal.roomNamePlaceholder' as TKey)}
                maxLength={32}
              />
            </label>

            {/* 2026-08-22 §BUG-TEXAS-CREATE-OVERLAY — 盲注档位与买入选择器
                移入弹窗内部,确保全屏遮罩不阻挡真实 CDP 点击 */}
            <section className="thp-create-modal__field">
              <BlindSelector value={bb} onChange={handleBbChange} />
            </section>
            <section className="thp-create-modal__field">
              <BuyinSlider bb={bb} value={buyin} onChange={setBuyin} />
              {balance != null && balance < buyInRange(bb).min && (
                <p className="thp-create-modal__hint thp-create-modal__hint--warn">
                  {t('ante.insufficient' as TKey)}
                </p>
              )}
            </section>

            <div className="thp-create-modal__field">
              <div className="thp-create-modal__field-head">
                <span data-testid="thp-create-modal__ai-count-label">
                  {t('texasholdem.createModal.aiCount' as TKey, { count: agentCount })}
                </span>
                <span className="thp-create-modal__hint">
                  {agentCount === 0
                    ? t('texasholdem.createModal.allHuman' as TKey)
                    : t('texasholdem.createModal.humanAiMix' as TKey, {
                        human: MAX_AI_SEATS - agentCount,
                        ai: agentCount,
                      })}
                </span>
              </div>
              <input
                type="range"
                min={0}
                max={MAX_AI_SEATS}
                value={agentCount}
                data-testid="thp-create-modal__ai-count-slider"
                onChange={(e) => setAgentCount(Number(e.target.value))}
              />
            </div>
          </div>

          {loading && (
            <p className="thp-create-modal__hint">
              {t('texasholdem.createModal.loadingModels' as TKey)}
            </p>
          )}
          {error && (
            <p className="thp-create-modal__error">
              {t('texasholdem.createModal.modelsUnavailable' as TKey, { error })}
            </p>
          )}

          {agentCount > 0 && models.length > 0 && (
            <div className="thp-create-modal__seatblock">
              <div className="thp-create-modal__seatblock-head">
                <span>
                  {t('texasholdem.createModal.aiSeats' as TKey, { count: seats.length })}
                </span>
                <button
                  type="button"
                  className="thp-create-modal__reshuffle"
                  onClick={() => {
                    setSeats([]);
                    setShuffleNonce((n) => n + 1);
                  }}
                >
                  {t('texasholdem.createModal.reshuffle' as TKey)}
                </button>
              </div>
              <div className="thp-create-modal__seats">
                {seats.map((s, i) => (
                  <div key={i} className="thp-create-modal__seatrow">
                    <span className="thp-create-modal__seatidx">AI {i + 1}</span>
                    <select
                      value={s.seat}
                      onChange={(e) => updateSeat(i, { seat: Number(e.target.value) })}
                      aria-label={t('texasholdem.createModal.aiSeatLabel' as TKey, { index: i + 1 })}
                    >
                      {ALL_SEATS.map((n) => (
                        <option
                          key={n}
                          value={n}
                          disabled={usedSeats.has(n) && n !== s.seat}
                        >
                          {t('texasholdem.createModal.seatNumber' as TKey, { n })}
                        </option>
                      ))}
                    </select>
                    <select
                      value={s.model_key}
                      onChange={(e) => updateSeat(i, { model_key: e.target.value })}
                      aria-label={t('texasholdem.createModal.aiModelLabel' as TKey, { index: i + 1 })}
                    >
                      {models.map((m) => (
                        <option key={m.model} value={m.model}>
                          {m.agent_name}
                        </option>
                      ))}
                    </select>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {formError && (
          <p className="thp-create-modal__error" role="alert" data-testid="thp-create-modal__form-error">
            {formError}
          </p>
        )}

        <footer className="thp-create-modal__footer">
          <button className="thp-create-modal__cancel" onClick={onClose}>
            {t('texasholdem.createModal.cancel' as TKey)}
          </button>
          <button
            className="thp-create-modal__submit"
            disabled={!valid || loading || submitting || localSubmitting}
            onClick={async () => {
              setFormError(null);
              setLocalSubmitting(true);
              try {
                const ok = await onSubmit({
                  name: name.trim() || undefined,
                  agent_seats: seats.map((s) => ({
                    seat: s.seat,
                    model_key: s.model_key,
                  })),
                  big_blind: bb,
                  start_stack: buyin,
                });
                if (ok === false) {
                  setFormError(t('texasholdem.createModal.submitFailed' as TKey));
                }
              } catch (e: any) {
                setFormError(
                  t('texasholdem.createModal.submitError' as TKey, { message: e?.message ?? String(e) }),
                );
              } finally {
                setLocalSubmitting(false);
              }
            }}
          >
            {t('texasholdem.createModal.submit' as TKey)}
          </button>
        </footer>
      </div>
    </div>
  );
}

export default RoomCreateModal;