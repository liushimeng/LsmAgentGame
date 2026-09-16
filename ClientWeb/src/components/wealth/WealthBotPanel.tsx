/**
 * WealthBotPanel — Agent 思维展示（bot_contexts：本人座位 + 观战者可见）。
 * 仿 texasholdem/BotThoughtPanel 的折叠风格：每座位一张卡，
 * 决策摘要 / 工具入参 / 工具结果 / 内心独白 四段。
 */

import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { WealthBotContext, WealthPlayer } from '@/types/wealth';
import { professionColor, professionEmoji } from '@/types/wealth';

interface Props {
  botContexts: WealthBotContext[];
  players: WealthPlayer[];
}

function prettyJson(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

export function WealthBotPanel({ botContexts, players }: Props) {
  const t = useT();
  if (!botContexts || botContexts.length === 0) {
    return (
      <div className="wealth-botpanel wealth-botpanel--empty">
        <p>{t('wealth.botPanel.empty' as TKey)}</p>
      </div>
    );
  }

  return (
    <div className="wealth-botpanel">
      {/* 10–12 座位房：观战者最多同时看到 12 张卡 → 标题带计数，卡片列表可滚动 */}
      <div className="wealth-botpanel__title">
        🤖 {t('wealth.botPanel.titleCount' as TKey, { n: botContexts.length })}
      </div>
      {botContexts.map((ctx) => {
        const p = players.find((x) => x.seat === ctx.seat);
        const color = p ? professionColor(p.profession.id) : '#9ca3af';
        return (
          <details key={ctx.seat} className="wealth-botpanel__card">
            <summary>
              <span
                className="wealth-botpanel__dot"
                style={{ background: color }}
                aria-hidden="true"
              />
              {t('wealth.botPanel.seat' as TKey, { n: ctx.seat + 1 })} {p?.nickname ?? ''}{' '}
              <span className="wealth-botpanel__emoji">
                {p ? professionEmoji(p.profession.id) : ''}
              </span>
              {p?.model_display ? (
                <span className="wealth-botpanel__model">{p.model_display}</span>
              ) : null}
            </summary>
            <div className="wealth-botpanel__body">
              {ctx.last_decision_summary && (
                <p className="wealth-botpanel__row">
                  <b>🎯 {t('wealth.botPanel.decision' as TKey)}</b>
                  {ctx.last_decision_summary}
                </p>
              )}
              {ctx.heart_thought && (
                <p className="wealth-botpanel__row">
                  <b>💭 {t('wealth.botPanel.heart' as TKey)}</b>
                  {ctx.heart_thought}
                </p>
              )}
              {ctx.last_tool_input && (
                <details className="wealth-botpanel__tool">
                  <summary>🔧 {t('wealth.botPanel.tool' as TKey)}</summary>
                  <pre className="wealth-botpanel__pre">{prettyJson(ctx.last_tool_input)}</pre>
                  {ctx.last_tool_result && (
                    <p className="wealth-botpanel__result">{ctx.last_tool_result}</p>
                  )}
                </details>
              )}
            </div>
          </details>
        );
      })}
    </div>
  );
}
