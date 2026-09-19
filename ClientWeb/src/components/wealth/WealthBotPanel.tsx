/**
 * WealthBotPanel — Agent 思维展示（bot_contexts：本人座位 + 观战者可见）。
 * 仿 texasholdem/BotThoughtPanel 的折叠风格：每座位一张卡，
 * 决策摘要 / 工具入参 / 工具结果 / 内心独白 四段。
 */

import { useMemo, useState } from 'react';
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

type BotFilter = 'all' | 'active' | 'decision';

function hasCompletedDecision(ctx: WealthBotContext): boolean {
  const decisionMonth = ctx.last_decision_month || 0;
  const currentMonth = ctx.month || 0;
  const toolInput = ctx.last_tool_input.trim();
  return (
    decisionMonth > 0 &&
    decisionMonth === currentMonth &&
    toolInput.length > 0
  );
}

export function WealthBotPanel({ botContexts, players }: Props) {
  const t = useT();
  const [filter, setFilter] = useState<BotFilter>('active');
  const contexts = botContexts ?? [];
  const allPlayers = players ?? [];

  const rows = useMemo(
    () =>
      contexts.map((ctx) => {
        const player = allPlayers.find((item) => item.seat === ctx.seat);
        return {
          ctx,
          player,
          alive: ctx.active ?? (player?.alive ?? true),
          hasDecision: hasCompletedDecision(ctx),
          lastDecisionMonth: ctx.last_decision_month || 0,
        };
      }),
    [contexts, allPlayers],
  );
  const visibleRows = useMemo(
    () =>
      rows.filter((row) => {
        if (filter === 'active') return row.alive;
        if (filter === 'decision') return row.hasDecision;
        return true;
      }),
    [rows, filter],
  );

  if (contexts.length === 0) {
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
        🤖{' '}
        {visibleRows.length === contexts.length
          ? t('wealth.botPanel.titleCount' as TKey, { n: contexts.length })
          : t('wealth.botPanel.titleFiltered' as TKey, {
              visible: visibleRows.length,
              total: contexts.length,
            })}
      </div>
      <div
        className="wealth-botpanel__filters"
        role="group"
        aria-label={t('wealth.botPanel.filterGroup' as TKey)}
      >
        {(
          [
            ['active', 'wealth.botPanel.filterActive'],
            ['decision', 'wealth.botPanel.filterDecision'],
            ['all', 'wealth.botPanel.filterAll'],
          ] as const
        ).map(([key, labelKey]) => (
          <button
            key={key}
            type="button"
            className={`wealth-botpanel__filter${filter === key ? ' wealth-botpanel__filter--active' : ''}`}
            onClick={() => setFilter(key)}
            aria-pressed={filter === key}
          >
            {t(labelKey as TKey)}
          </button>
        ))}
      </div>
      {visibleRows.length === 0 && (
        <p className="wealth-botpanel__filtered-empty">
          {t('wealth.botPanel.filterEmpty' as TKey)}
        </p>
      )}
      {visibleRows.map(({ ctx, player: p, alive, lastDecisionMonth }) => {
        const color = p ? professionColor(p.profession.id) : '#9ca3af';
        return (
          <details
            key={ctx.seat}
            className={`wealth-botpanel__card${alive ? '' : ' wealth-botpanel__card--out'}`}
          >
            <summary>
              <span
                className="wealth-botpanel__dot"
                style={{ background: color }}
                aria-hidden="true"
              />
              <span className="wealth-botpanel__meta">
                <span className="wealth-botpanel__name">
                  {t('wealth.botPanel.seat' as TKey, { n: ctx.seat + 1 })} {p?.nickname ?? ''}
                </span>
                <span className="wealth-botpanel__emoji" aria-hidden="true">
                  {p ? professionEmoji(p.profession.id) : ''}
                </span>
                {p?.model_display ? (
                  <span className="wealth-botpanel__model">{p.model_display}</span>
                ) : null}
              </span>
              <span
                className={`wealth-botpanel__status${alive ? '' : ' wealth-botpanel__status--out'}`}
              >
                {t(
                  alive
                    ? ('wealth.botPanel.statusActive' as TKey)
                    : ('wealth.botPanel.statusOut' as TKey),
                )}
              </span>
              {lastDecisionMonth > 0 && (
                <span className="wealth-botpanel__decision-month">M{lastDecisionMonth}</span>
              )}
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
