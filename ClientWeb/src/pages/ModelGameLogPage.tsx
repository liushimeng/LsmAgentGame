// ModelGameLogPage — /admin/models/:providerId/games/:gameLogId
// Timeline view of all chat messages and actions for a single bot's game.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useAuthStore } from '@/store/auth.store';
import { useT } from '@/hooks/useT';
import { getGameLog, getGameMessages } from '@/api/modelAdmin';
import { reportGlobalError } from '@/services/globalError';
import type {
  ModelChatMessage,
  ModelAction,
  ModelGameLog,
} from '@/types/model';

const RESULT_KEY: Record<string, string> = {
  win: 'modelAdmin.game.result.win',
  lose: 'modelAdmin.game.result.lose',
  draw: 'modelAdmin.game.result.draw',
  abandoned: 'modelAdmin.game.result.abandoned',
};
const RESULT_CLASS: Record<string, string> = {
  win: 'result-badge--win',
  lose: 'result-badge--lose',
  draw: 'result-badge--draw',
  abandoned: 'result-badge--abandoned',
};

// Heuristic color for action_type badges — maps a handful of well-known
// action types to semantic colors; anything else falls back to neutral.
const ACTION_CLASS: Record<string, string> = {
  speak: 'action-badge--speak',
  vote: 'action-badge--vote',
  wolf_kill: 'action-badge--kill',
  seer_check: 'action-badge--check',
  witch_act: 'action-badge--witch',
  sheriff_elect: 'action-badge--speak',
  hunter_shoot: 'action-badge--kill',
  move: 'action-badge--vote',
  play_card: 'action-badge--vote',
  bid: 'action-badge--speak',
  pass: 'action-badge--neutral',
  fold: 'action-badge--neutral',
  call: 'action-badge--vote',
  raise: 'action-badge--vote',
  check_action: 'action-badge--neutral',
  all_in: 'action-badge--kill',
  bet: 'action-badge--vote',
  ante: 'action-badge--neutral',
};

function formatDate(iso?: string): string {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleString('zh-CN');
  } catch {
    return iso;
  }
}

function durationStr(start?: string, end?: string): string {
  if (!start) return '-';
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  if (Number.isNaN(s) || Number.isNaN(e) || e < s) return '-';
  const sec = Math.floor((e - s) / 1000);
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`;
  return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
}

function shortId(id: string, len = 8): string {
  if (!id) return '-';
  return id.length > len ? id.slice(0, len) : id;
}

function tryParseJson(s: string): unknown {
  if (!s) return null;
  const trimmed = s.trim();
  if (!trimmed) return null;
  if (!(trimmed.startsWith('{') || trimmed.startsWith('['))) return null;
  try {
    return JSON.parse(trimmed);
  } catch {
    return null;
  }
}

type TimelineItem =
  | { kind: 'message'; data: ModelChatMessage; ts: number }
  | { kind: 'action'; data: ModelAction; ts: number };

export function ModelGameLogPage() {
  const navigate = useNavigate();
  const { providerId = '', gameLogId = '' } = useParams<{
    providerId: string;
    gameLogId: string;
  }>();
  const t = useT();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const userType = useAuthStore((s) => s.userType);

  const [game, setGame] = useState<ModelGameLog | null>(null);
  const [actions, setActions] = useState<ModelAction[]>([]);
  const [messages, setMessages] = useState<ModelChatMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // filters
  const [toolOnly, setToolOnly] = useState(false);
  const [thinkingOnly, setThinkingOnly] = useState(false);
  const [expandReasoning, setExpandReasoning] = useState(false);

  useEffect(() => {
    if (!isAuthenticated) {
      navigate('/');
    }
  }, [isAuthenticated, navigate]);

  const reload = useCallback(async () => {
    if (!gameLogId) return;
    setLoading(true);
    setError(null);
    try {
      const [detail, msgs] = await Promise.all([
        getGameLog(gameLogId),
        getGameMessages(gameLogId).catch(() => [] as ModelChatMessage[]),
      ]);
      setGame(detail.game);
      setActions(detail.actions);
      setMessages(msgs);
    } catch (e) {
      const msg = e instanceof Error ? e.message : t('modelAdmin.game.errorLoad');
      setError(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setLoading(false);
    }
  }, [gameLogId, t]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const timeline: TimelineItem[] = useMemo(() => {
    const out: TimelineItem[] = [];
    for (const m of messages) {
      const ts = new Date(m.created_at).getTime() || 0;
      out.push({ kind: 'message', data: m, ts });
    }
    for (const a of actions) {
      const ts = new Date(a.created_at).getTime() || 0;
      out.push({ kind: 'action', data: a, ts });
    }
    out.sort((a, b) => a.ts - b.ts || a.kind.localeCompare(b.kind));
    return out;
  }, [messages, actions]);

  const filtered = useMemo(() => {
    return timeline.filter((item) => {
      if (toolOnly) {
        if (item.kind === 'message') {
          if (!item.data.tool_name && item.data.role !== 'tool_result') return false;
        }
        // include all actions when "tool only"
      }
      if (thinkingOnly) {
        if (item.kind === 'message' && !item.data.thinking) return false;
        if (item.kind === 'action' && !item.data.reasoning) return false;
      }
      return true;
    });
  }, [timeline, toolOnly, thinkingOnly]);

  if (!isAuthenticated) return null;
  const isAdmin = userType != null && userType >= 2;
  if (!isAdmin) {
    return (
      <div className="model-admin-page">
        <h1>🤖 {t('modelAdmin.game.title')}</h1>
        <div className="error">{t('adminUsers.userTypeNormal')}</div>
      </div>
    );
  }

  return (
    <div className="model-admin-page">
      <div className="model-admin-toolbar">
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() =>
            navigate(`/admin/models/${encodeURIComponent(providerId)}`)
          }
          data-testid="btn-back"
        >
          ← {t('modelAdmin.actionBack')}
        </button>
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => void reload()}
          disabled={loading}
          data-testid="btn-refresh"
        >
          🔄 {t('common.loading') === '加载中…' ? '刷新' : 'Refresh'}
        </button>
      </div>

      {error && <div className="error">{error}</div>}

      {loading && !game && (
        <div className="empty-state">{t('modelAdmin.game.loading')}</div>
      )}

      {game && (
        <>
          <h1>🎮 {t('modelAdmin.game.title')}</h1>
          <p className="model-admin-page__subtitle">
            <code>{shortId(game.id)}</code> · {game.game_kind}
          </p>

          {/* 顶部信息条 */}
          <div className="model-card">
            <div className="game-info-grid">
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.game.bot')}</span>
                <span className="info-row__v">
                  <code>{shortId(game.bot_user_id)}</code>
                </span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.game.room')}</span>
                <span className="info-row__v">
                  <code>{shortId(game.room_id)}</code>
                </span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.detail.colGameKind')}</span>
                <span className="info-row__v">{game.game_kind}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.detail.colStartedAt')}</span>
                <span className="info-row__v">{formatDate(game.started_at)}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.game.duration')}</span>
                <span className="info-row__v">
                  {durationStr(game.started_at, game.ended_at)}
                </span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.detail.colResult')}</span>
                <span className="info-row__v">
                  <span className={'result-badge ' + (RESULT_CLASS[game.result] ?? '')}>
                    {t((RESULT_KEY[game.result] ?? 'adminUsers.errorEmpty') as never)}
                  </span>
                </span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.game.coinDelta')}</span>
                <span
                  className={
                    'info-row__v ' +
                    (game.coin_delta > 0
                      ? 'amount--gain'
                      : game.coin_delta < 0
                        ? 'amount--loss'
                        : '')
                  }
                >
                  {game.coin_delta > 0 ? '+' : ''}
                  {game.coin_delta}
                </span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.game.totalCalls')}</span>
                <span className="info-row__v">{game.llm_call_count}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.game.inputTokens')}</span>
                <span className="info-row__v">{game.input_tokens}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.game.outputTokens')}</span>
                <span className="info-row__v">{game.output_tokens}</span>
              </div>
              {game.final_hand && (
                <div className="info-row" style={{ gridColumn: '1 / -1' }}>
                  <span className="info-row__k">Final</span>
                  <span className="info-row__v"><code>{game.final_hand}</code></span>
                </div>
              )}
            </div>
          </div>

          {/* filter 开关 */}
          <div className="game-filters">
            <label>
              <input
                type="checkbox"
                checked={toolOnly}
                onChange={(e) => setToolOnly(e.target.checked)}
                data-testid="filter-tool-only"
              />{' '}
              {t('modelAdmin.game.filter.toolOnly')}
            </label>
            <label>
              <input
                type="checkbox"
                checked={thinkingOnly}
                onChange={(e) => setThinkingOnly(e.target.checked)}
                data-testid="filter-thinking-only"
              />{' '}
              {t('modelAdmin.game.filter.thinkingOnly')}
            </label>
            <label>
              <input
                type="checkbox"
                checked={expandReasoning}
                onChange={(e) => setExpandReasoning(e.target.checked)}
                data-testid="filter-expand"
              />{' '}
              {t('modelAdmin.game.filter.expandReasoning')}
            </label>
          </div>

          {/* 时间线 */}
          <div className="timeline">
            {filtered.length === 0 && !loading && (
              <div className="empty-state">{t('modelAdmin.game.empty')}</div>
            )}
            {filtered.map((item, idx) => {
              if (item.kind === 'message') {
                return (
                  <MessageCard
                    key={`m-${item.data.id}-${idx}`}
                    msg={item.data}
                    expandReasoning={expandReasoning}
                  />
                );
              }
              return (
                <ActionCard
                  key={`a-${item.data.id}-${idx}`}
                  act={item.data}
                  expandReasoning={expandReasoning}
                />
              );
            })}
          </div>
        </>
      )}

      <style>{`
        .game-info-grid {
          display: grid;
          grid-template-columns: repeat(2, minmax(0, 1fr));
          gap: 8px 24px;
        }
        @media (max-width: 720px) {
          .game-info-grid { grid-template-columns: 1fr; }
        }
        .game-filters {
          display: flex;
          gap: 16px;
          flex-wrap: wrap;
          margin: 16px 0;
          padding: 10px 14px;
          background: var(--panel);
          border: 1px solid var(--border);
          border-radius: 8px;
          font-size: 13px;
          color: var(--text);
        }
        .game-filters label {
          display: inline-flex;
          align-items: center;
          gap: 6px;
          cursor: pointer;
        }
        .timeline {
          display: flex;
          flex-direction: column;
          gap: 10px;
          margin-top: 12px;
        }
        .timeline-card {
          padding: 10px 14px;
          background: linear-gradient(180deg, var(--panel) 0%, rgba(22,27,34,0.85) 100%);
          border: 1px solid var(--border);
          border-radius: 8px;
          box-shadow: 0 4px 12px rgba(0,0,0,0.25);
        }
        .timeline-card__header {
          display: flex;
          flex-wrap: wrap;
          align-items: center;
          gap: 8px;
          font-size: 12px;
          color: var(--muted);
          margin-bottom: 6px;
        }
        .timeline-card__body {
          color: var(--text);
          font-size: 13px;
          line-height: 1.5;
          white-space: pre-wrap;
          word-break: break-word;
        }
        .timeline-card__meta {
          margin-top: 6px;
          font-size: 12px;
          color: var(--muted);
        }
        .timeline-card--message-assistant { border-left: 3px solid var(--accent); }
        .timeline-card--message-user { border-left: 3px solid var(--ok); }
        .timeline-card--message-tool_result { border-left: 3px solid #ffc400; }
        .timeline-card--message-system { border-left: 3px solid var(--muted); opacity: 0.85; }
        .timeline-card--action { border-left: 3px solid #b66bff; }
        .role-badge {
          display: inline-block;
          padding: 1px 8px;
          border-radius: 999px;
          font-size: 11px;
          font-weight: 500;
        }
        .role-badge--assistant {
          background: rgba(10,132,255,0.18);
          color: var(--accent);
          border: 1px solid rgba(10,132,255,0.35);
        }
        .role-badge--user {
          background: rgba(63,185,80,0.18);
          color: var(--ok);
          border: 1px solid rgba(63,185,80,0.35);
        }
        .role-badge--tool_result {
          background: rgba(255,196,0,0.18);
          color: #ffc400;
          border: 1px solid rgba(255,196,0,0.35);
        }
        .role-badge--system {
          background: rgba(150,150,150,0.18);
          color: var(--muted);
          border: 1px solid rgba(150,150,150,0.35);
        }
        .action-badge {
          display: inline-block;
          padding: 2px 8px;
          border-radius: 999px;
          font-size: 11px;
          font-weight: 500;
        }
        .action-badge--speak {
          background: rgba(63,185,80,0.18);
          color: var(--ok);
          border: 1px solid rgba(63,185,80,0.35);
        }
        .action-badge--vote {
          background: rgba(10,132,255,0.18);
          color: var(--accent);
          border: 1px solid rgba(10,132,255,0.35);
        }
        .action-badge--kill {
          background: rgba(248,81,73,0.18);
          color: var(--danger);
          border: 1px solid rgba(248,81,73,0.35);
        }
        .action-badge--check {
          background: rgba(182,107,255,0.18);
          color: #b66bff;
          border: 1px solid rgba(182,107,255,0.35);
        }
        .action-badge--witch {
          background: rgba(255,196,0,0.18);
          color: #ffc400;
          border: 1px solid rgba(255,196,0,0.35);
        }
        .action-badge--neutral {
          background: rgba(150,150,150,0.18);
          color: var(--muted);
          border: 1px solid rgba(150,150,150,0.35);
        }
        .collapsible {
          margin-top: 8px;
          padding: 6px 10px;
          background: var(--bg);
          border: 1px solid var(--border);
          border-radius: 6px;
          font-size: 12px;
          color: var(--text);
          white-space: pre-wrap;
          word-break: break-word;
        }
        .collapsible summary {
          cursor: pointer;
          color: var(--muted);
        }
        .amount--gain { color: var(--ok); }
        .amount--loss { color: var(--danger); }
      `}</style>
    </div>
  );
}

function MessageCard({
  msg,
  expandReasoning,
}: {
  msg: ModelChatMessage;
  expandReasoning: boolean;
}) {
  const t = useT();
  const cardCls = 'timeline-card timeline-card--message-' + msg.role;
  const roleLabel = msg.role;
  return (
    <div className={cardCls} data-testid={`msg-${msg.id}`}>
      <div className="timeline-card__header">
        <span className={'role-badge role-badge--' + msg.role}>{roleLabel}</span>
        {msg.phase && (
          <span className="muted-tag">⏱ {msg.phase}</span>
        )}
        {msg.tool_name && (
          <span className="action-badge action-badge--check">
            🔧 {t('modelAdmin.game.toolUse')}: {msg.tool_name}
          </span>
        )}
        {msg.latency_ms > 0 && (
          <span className="muted-tag">⌛ {msg.latency_ms}ms</span>
        )}
        {msg.stop_reason && (
          <span className="muted-tag">stop: {msg.stop_reason}</span>
        )}
        {msg.seq > 0 && <span className="muted-tag">#{msg.seq}</span>}
      </div>
      {msg.content && <div className="timeline-card__body">{msg.content}</div>}

      {msg.thinking && (
        <details className="collapsible" open={expandReasoning}>
          <summary>🧠 {t('modelAdmin.game.thinking')}</summary>
          <div style={{ marginTop: 6 }}>{msg.thinking}</div>
        </details>
      )}

      {msg.tool_input && (() => {
        const parsed = tryParseJson(msg.tool_input);
        return (
          <details className="collapsible" open={expandReasoning}>
            <summary>📥 {t('modelAdmin.game.payload')}</summary>
            <pre style={{ margin: '6px 0 0', fontSize: 11 }}>
              {parsed ? JSON.stringify(parsed, null, 2) : msg.tool_input}
            </pre>
          </details>
        );
      })()}
    </div>
  );
}

function ActionCard({
  act,
  expandReasoning,
}: {
  act: ModelAction;
  expandReasoning: boolean;
}) {
  const t = useT();
  const actionCls = 'action-badge ' + (ACTION_CLASS[act.action_type] ?? 'action-badge--neutral');
  const parsed = tryParseJson(act.payload);
  return (
    <div className="timeline-card timeline-card--action" data-testid={`act-${act.id}`}>
      <div className="timeline-card__header">
        <span className={actionCls}>⚙ {act.action_type}</span>
        {act.phase && <span className="muted-tag">⏱ {act.phase}</span>}
        {act.action_target && (
          <span className="muted-tag">
            🎯 {t('modelAdmin.game.target')}: {act.action_target}
          </span>
        )}
        {act.accepted === false && (
          <span className="muted-tag" style={{ color: 'var(--danger)' }}>
            ✗ rejected
          </span>
        )}
      </div>

      {parsed ? (
        <pre className="collapsible" style={{ margin: 0 }}>
          {JSON.stringify(parsed, null, 2)}
        </pre>
      ) : act.payload ? (
        <div className="timeline-card__body">{act.payload}</div>
      ) : null}

      {act.reasoning && (
        <details className="collapsible" open={expandReasoning}>
          <summary>💭 {t('modelAdmin.game.reasoning')}</summary>
          <div style={{ marginTop: 6 }}>{act.reasoning}</div>
        </details>
      )}
    </div>
  );
}

export default ModelGameLogPage;
