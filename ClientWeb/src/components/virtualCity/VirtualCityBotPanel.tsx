/**
 * VirtualCityBotPanel — 居民思维展示（bot_contexts：本人座位 + 观察者可见）。
 * 仿 texasholdem/BotThoughtPanel 的折叠风格：每座位一张卡，
 * 决策摘要 / 工具入参 / 工具结果 / 内心独白 四段；
 * 2026-09-22 §CityHuman重构：追加「感知」小节（看见/听见/闻到三段式，
 * 数据源 bot_contexts[].last_senses，契约见设计文档 1 §4.3/§6）。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { VirtualCityBotContext, VirtualCityPlayer, VirtualCitySenseResult } from '@/types/virtualCity';
import { professionColor, professionEmoji } from '@/types/virtualCity';
import { CollapsibleSection } from '@/components/virtualCity/CollapsibleSection';

interface Props {
  botContexts: VirtualCityBotContext[];
  players: VirtualCityPlayer[];
}

function prettyJson(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

type BotFilter = 'all' | 'active' | 'decision';

/** 感知三段式渲染辅助：把一条 SenseResult 拍平成人读文本行。 */
function senseLines(sense: VirtualCitySenseResult): string[] {
  const lines: string[] = [];
  if (sense.people?.length) {
    lines.push(
      sense.people
        .map((p) => (p.occupation ? `${p.name}(${p.occupation})` : p.name))
        .join('、'),
    );
  }
  if (sense.things?.length) lines.push(sense.things.join('、'));
  if (sense.events?.length) lines.push(sense.events.join('；'));
  if (sense.utterances?.length) lines.push(sense.utterances.join('；'));
  if (sense.smells?.length) lines.push(sense.smells.join('、'));
  if (sense.sounds?.length) lines.push(sense.sounds.join('、'));
  return lines;
}

/** 感知条目 → 三段式标签键（缺 kind 时按字段内容推断，兼容旧帧）。 */
function senseLabelKey(sense: VirtualCitySenseResult): TKey {
  if (sense.kind === 'see' || sense.kind === 'hear' || sense.kind === 'smell') {
    return `virtualCity.botPanel.sense.${sense.kind}` as TKey;
  }
  if (sense.smells?.length) return 'virtualCity.botPanel.sense.smell' as TKey;
  if (sense.utterances?.length || sense.sounds?.length) return 'virtualCity.botPanel.sense.hear' as TKey;
  return 'virtualCity.botPanel.sense.see' as TKey;
}

/** 「感知」小节：看见/听见/闻到三段式（last_senses 缺省时不渲染）。 */
function SenseSection({ senses, t }: { senses?: VirtualCitySenseResult[]; t: ReturnType<typeof useT> }) {
  if (!senses || senses.length === 0) return null;
  return (
    <div className="virtualCity-botpanel__senses">
      <b>👁 {t('virtualCity.botPanel.sense.title' as TKey)}</b>
      {senses.map((sense, i) => {
        const lines = senseLines(sense);
        if (lines.length === 0) return null;
        return (
          <p key={i} className="virtualCity-botpanel__row virtualCity-botpanel__sense">
            <b>{t(senseLabelKey(sense))}</b>
            <span>
              [{sense.district}] {lines.join(' · ')}
            </span>
          </p>
        );
      })}
    </div>
  );
}

function hasCompletedDecision(ctx: VirtualCityBotContext): boolean {
  const decisionMonth = ctx.last_decision_month || 0;
  const currentMonth = ctx.month || 0;
  const toolInput = ctx.last_tool_input.trim();
  return (
    decisionMonth > 0 &&
    decisionMonth === currentMonth &&
    toolInput.length > 0
  );
}

export function VirtualCityBotPanel({ botContexts, players }: Props) {
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
      <div className="virtualCity-botpanel virtualCity-botpanel--empty">
        <p>{t('virtualCity.botPanel.empty' as TKey)}</p>
      </div>
    );
  }

  // 10–12 座位房：观战者最多同时看到 12 张卡 → 标题带计数，卡片列表可滚动。
  // 阶段 E（13-3D城市渲染优化）：融合式折叠（CollapsibleSection 接管标题行，
  // 折叠态 localStorage 持久化 virtualCity.ui.collapsed.bot_panel）。
  const titleNode = (
    <>
      🤖{' '}
      {visibleRows.length === contexts.length
        ? t('virtualCity.botPanel.titleCount' as TKey, { n: contexts.length })
        : t('virtualCity.botPanel.titleFiltered' as TKey, {
            visible: visibleRows.length,
            total: contexts.length,
          })}
    </>
  );

  return (
    <CollapsibleSection
      className="virtualCity-botpanel"
      storageKey="virtualCity.ui.collapsed.bot_panel"
      title={titleNode}
      bodyClassName="virtualCity-botpanel__collapse-body"
    >
      <div
        className="virtualCity-botpanel__filters"
        role="group"
        aria-label={t('virtualCity.botPanel.filterGroup' as TKey)}
      >
        {(
          [
            ['active', 'virtualCity.botPanel.filterActive'],
            ['decision', 'virtualCity.botPanel.filterDecision'],
            ['all', 'virtualCity.botPanel.filterAll'],
          ] as const
        ).map(([key, labelKey]) => (
          <button
            key={key}
            type="button"
            className={`virtualCity-botpanel__filter${filter === key ? ' virtualCity-botpanel__filter--active' : ''}`}
            onClick={() => setFilter(key)}
            aria-pressed={filter === key}
          >
            {t(labelKey as TKey)}
          </button>
        ))}
      </div>
      {visibleRows.length === 0 && (
        <p className="virtualCity-botpanel__filtered-empty">
          {t('virtualCity.botPanel.filterEmpty' as TKey)}
        </p>
      )}
      {visibleRows.map(({ ctx, player: p, alive, lastDecisionMonth }) => {
        const color = p ? professionColor(p.profession.id) : '#9ca3af';
        return (
          <details
            key={ctx.seat}
            className={`virtualCity-botpanel__card${alive ? '' : ' virtualCity-botpanel__card--out'}`}
          >
            <summary>
              <span
                className="virtualCity-botpanel__dot"
                style={{ background: color }}
                aria-hidden="true"
              />
              <span className="virtualCity-botpanel__meta">
                <span className="virtualCity-botpanel__name">
                  {t('virtualCity.botPanel.seat' as TKey, { n: ctx.seat + 1 })} {p?.nickname ?? ''}
                </span>
                <span className="virtualCity-botpanel__emoji" aria-hidden="true">
                  {p ? professionEmoji(p.profession.id) : ''}
                </span>
                {p?.model_display ? (
                  <span className="virtualCity-botpanel__model">{p.model_display}</span>
                ) : null}
              </span>
              <span
                className={`virtualCity-botpanel__status${alive ? '' : ' virtualCity-botpanel__status--out'}`}
              >
                {t(
                  alive
                    ? ('virtualCity.botPanel.statusActive' as TKey)
                    : ('virtualCity.botPanel.statusOut' as TKey),
                )}
              </span>
              {lastDecisionMonth > 0 && (
                <span className="virtualCity-botpanel__decision-month">M{lastDecisionMonth}</span>
              )}
            </summary>
            <div className="virtualCity-botpanel__body">
              <SenseSection senses={ctx.last_senses} t={t} />
              {ctx.last_decision_summary && (
                <p className="virtualCity-botpanel__row">
                  <b>🎯 {t('virtualCity.botPanel.decision' as TKey)}</b>
                  {ctx.last_decision_summary}
                </p>
              )}
              {ctx.heart_thought && (
                <p className="virtualCity-botpanel__row">
                  <b>💭 {t('virtualCity.botPanel.heart' as TKey)}</b>
                  {ctx.heart_thought}
                </p>
              )}
              {ctx.last_tool_input && (
                <details className="virtualCity-botpanel__tool">
                  <summary>🔧 {t('virtualCity.botPanel.tool' as TKey)}</summary>
                  <pre className="virtualCity-botpanel__pre">{prettyJson(ctx.last_tool_input)}</pre>
                  {ctx.last_tool_result && (
                    <p className="virtualCity-botpanel__result">{ctx.last_tool_result}</p>
                  )}
                </details>
              )}
            </div>
          </details>
        );
      })}
    </CollapsibleSection>
  );
}
