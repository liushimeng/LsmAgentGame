// 狼人杀「赛后复盘问答」面板(2026-08-11 §20260811-05 U2)。
//
// 对局结束后(SettlementModal 内 / 房间 over 态)玩家与观战者可向本局任意
// bot 座位提问(如「你第二晚为什么毒 5 号?」),bot 用冻结的本局 Memory 快照
// + 复盘 system 指令做单轮问答。不写回 Memory、不进 chat 表(§119 反面:
// 对局已结束,没有频道隔离对象需要保护)。
//
// 错误展示(§7.1):面板内联红条(最高可见位置) + reportGlobalError 全局兜底。
import { useMemo, useState } from 'react';
import { http, ApiError, isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

/** RecallChatAnswer 对齐后端 werewolf.RecallChatResult(§121 数据形状契约)。 */
export interface RecallChatAnswer {
  seat: number;
  model_key: string;
  role: string;
  answer: string;
  /** true = LLM 失败的降级文案(bot「太累不想复盘」)。 */
  fallback: boolean;
  took_ms: number;
}

/** BotSeatOption 是复盘问答可选的 bot 座位。 */
export interface BotSeatOption {
  seat: number;
  label: string; // 例 "3 号 · DeepSeek-model · 女巫"
}

interface RecallChatPanelProps {
  roomId: string;
  /** 本局 bot 座位候选(由父组件从 gameState.players 过滤 is_bot/agent_name)。 */
  botSeats: BotSeatOption[];
  /** §20260823-02 P7 — 右上角 ✕ 关闭回调(由父组件控制 dock 不再自动出现)。 */
  onClose?: () => void;
}

interface QAItem {
  seat: number;
  seatLabel: string;
  question: string;
  answer: string;
  fallback: boolean;
  tookMs: number;
}

const MAX_QUESTION_LEN = 200;

/**
 * RecallChatPanel —— 赛后复盘问答。
 * 纯前端本地 state 管理问答历史(不落库);提问走 REST 单轮请求/响应。
 */
export function RecallChatPanel({ roomId, botSeats, onClose }: RecallChatPanelProps) {
  const t = useT();
  const [seat, setSeat] = useState<number>(botSeats[0]?.seat ?? -1);
  const [question, setQuestion] = useState('');
  const [items, setItems] = useState<QAItem[]>([]);
  const [asking, setAsking] = useState(false);
  const [err, setErr] = useState('');

  const seatLabel = useMemo(
    () => (s: number) => botSeats.find((b) => b.seat === s)?.label ?? `${s + 1} 号`,
    [botSeats],
  );

  if (botSeats.length === 0) {
    return (
      <div className="recall-panel recall-panel--empty">
        {t('werewolf.recall.noBots' as TKey)}
      </div>
    );
  }

  const ask = async () => {
    const q = question.trim();
    if (!q || asking || seat < 0) return;
    setAsking(true);
    setErr('');
    try {
      const resp = await http<RecallChatAnswer>(
        `/api/games/werewolf/rooms/${encodeURIComponent(roomId)}/recall_chat`,
        {
          method: 'POST',
          body: JSON.stringify({ seat, question: q.slice(0, MAX_QUESTION_LEN) }),
          // 慢模型复盘回答可达分钟级 — 放宽到 10 分钟(§197 对齐)。
          timeoutMs: 600_000,
        },
      );
      setItems((prev) => [
        ...prev,
        {
          seat: resp.seat,
          seatLabel: seatLabel(resp.seat),
          question: q,
          answer: resp.answer,
          fallback: resp.fallback,
          tookMs: resp.took_ms,
        },
      ]);
      setQuestion('');
    } catch (e) {
      // §7.1 规范:会话过期走全局重登弹层,不重复展示;其它错误
      // 面板内联 + 全局 toast 双通道。
      if (isSessionExpiredError(e)) return;
      let msg: string;
      if (e instanceof ApiError) {
        if (e.status === 403) {
          msg = t('werewolf.recall.errForbidden' as TKey);
        } else if (e.status === 429) {
          msg = t('werewolf.recall.errRateLimit' as TKey);
        } else {
          msg = e.message || `HTTP ${e.status}`;
        }
      } else {
        msg = (e as Error).message || t('werewolf.recall.errNetwork' as TKey);
      }
      setErr(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setAsking(false);
    }
  };

  return (
    <div className="recall-panel">
      {/* §20260823-02 P7 — 标题行右侧 ✕ 关闭按钮(44px 触控目标,i18n aria-label)。 */}
      <div className="recall-panel__title-row">
        <div className="recall-panel__title">💬 {t('werewolf.recall.title' as TKey)}</div>
        {onClose && (
          <button
            type="button"
            className="recall-panel__close"
            onClick={onClose}
            aria-label={t('werewolf.panel.close' as TKey)}
            title={t('werewolf.panel.close' as TKey)}
            data-testid="werewolf-recall-close"
          >
            ✕
          </button>
        )}
      </div>
      <div className="recall-panel__hint">{t('werewolf.recall.hint' as TKey)}</div>

      <div className="recall-panel__composer">
        <select
          className="recall-panel__seat"
          value={seat}
          onChange={(e) => setSeat(Number(e.target.value))}
          disabled={asking}
        >
          {botSeats.map((b) => (
            <option key={b.seat} value={b.seat}>
              {b.label}
            </option>
          ))}
        </select>
        <textarea
          className="recall-panel__input"
          value={question}
          maxLength={MAX_QUESTION_LEN}
          rows={2}
          placeholder={t('werewolf.recall.placeholder' as TKey)}
          disabled={asking}
          onChange={(e) => setQuestion(e.target.value)}
        />
        <button
          type="button"
          className="btn btn-primary recall-panel__ask"
          disabled={asking || !question.trim() || seat < 0}
          onClick={ask}
        >
          {asking ? t('werewolf.recall.asking' as TKey) : t('werewolf.recall.ask' as TKey)}
        </button>
      </div>

      {err && (
        <div className="recall-panel__error" role="alert">
          ⚠️ {err}
        </div>
      )}

      <div className="recall-panel__history">
        {items.length === 0 && (
          <div className="recall-panel__empty">{t('werewolf.recall.empty' as TKey)}</div>
        )}
        {items.map((it, i) => (
          <div key={i} className="recall-qa">
            <div className="recall-qa__q">
              <span className="recall-qa__seat">{it.seatLabel}</span>
              <span className="recall-qa__qtext">Q: {it.question}</span>
            </div>
            <div className={'recall-qa__a' + (it.fallback ? ' recall-qa__a--fallback' : '')}>
              {it.answer}
              <span className="recall-qa__meta">
                {(it.tookMs / 1000).toFixed(1)}s
                {it.fallback ? ` · ${t('werewolf.recall.fallbackTag' as TKey)}` : ''}
              </span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default RecallChatPanel;
