/**
 * 单个辩方 Bot Token 统计卡片 (2026-08-31 §20260831-09)
 *
 * 对齐 docs/辩论比赛/07 §5.1 — 每个 Bot 卡片:
 * 模型名(简称) + LLM 调用次数 + 输入/输出/总 Token。
 *
 * 样式由 debate-stats.css 统一管理(.debate-bot-token-card)。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { formatK } from '@/shared/utils/format';
import type { DebateAgentTokenSnapshot } from '@/types/debate';

export default function DebateBotTokenCard({ bot }: { bot: DebateAgentTokenSnapshot }) {
  const roleCN = bot.role_name ?? bot.role ?? '辩手';
  const modelShort = (bot.model_key ?? '').replace(/-model$/, '');
  const hasFail = bot.api_fail_count > 0;

  return (
    <div
      className={`debate-bot-token-card${hasFail ? ' debate-bot-token-card--fail' : ''}`}
      data-testid={`debate-bot-card-${bot.team_id}:${bot.seat}`}
    >
      <header className="debate-bot-token-card__header">
        <span className="debate-bot-token-card__role">{roleCN}</span>
        {modelShort && (
          <span className="debate-bot-token-card__model" title={bot.model_key}>
            {modelShort}
          </span>
        )}
      </header>
      <div className="debate-bot-token-card__body">
        <span className="debate-bot-token-card__stat" title="LLM 调用次数">
          📞 {bot.llm_call_count}
        </span>
        <span className="debate-bot-token-card__stat" title="输入 Token">
          ⬇ {formatK(bot.input_tokens)}
        </span>
        <span className="debate-bot-token-card__stat" title="输出 Token">
          ⬆ {formatK(bot.output_tokens)}
        </span>
        <span className="debate-bot-token-card__stat debate-bot-token-card__stat--total" title="总 Token">
          ⚡ {formatK(bot.api_tokens)}
        </span>
        {hasFail && (
          <span className="debate-bot-token-card__badge" title="失败次数">
            ✗ {bot.api_fail_count}
          </span>
        )}
      </div>
    </div>
  );
}
