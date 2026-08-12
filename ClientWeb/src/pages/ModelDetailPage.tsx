// ModelDetailPage — /admin/models/:providerId
// Shows provider info, bot wallet, and recent games for a single LLM model.

import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useAuthStore } from '@/store/auth.store';
import { useT } from '@/hooks/useT';
import { listProviders, listProviderGames, getBotWallet, grantDailyToAll } from '@/api/modelAdmin';
import { reportGlobalError } from '@/services/globalError';
import type { LlmProvider, ModelGameLog, BotWallet, WalletTx } from '@/types/model';
import { formatBalance } from '@/shared/utils/balance';
import { useI18nStore } from '@/store/i18n.store';

const PAGE_SIZE = 20;

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

function formatDate(iso?: string): string {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleString('zh-CN');
  } catch {
    return iso;
  }
}

export function ModelDetailPage() {
  const navigate = useNavigate();
  const { providerId = '' } = useParams<{ providerId: string }>();
  const t = useT();
  const lang = useI18nStore((s) => s.lang);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const userType = useAuthStore((s) => s.userType);

  const isSuper = userType === 3;

  const [provider, setProvider] = useState<LlmProvider | null>(null);
  const [games, setGames] = useState<ModelGameLog[]>([]);
  const [gamesTotal, setGamesTotal] = useState(0);
  const [wallet, setWallet] = useState<BotWallet | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // §135 — 详情页针对单个 model 的 grant 入口(超管可见)。
  // 后端走 provider_id 唯一 grant,与列表页批量 grant 共用同一张表。
  const [granting, setGranting] = useState(false);
  const [grantErr, setGrantErr] = useState<string | null>(null);
  const [grantOk, setGrantOk] = useState<string | null>(null);

  useEffect(() => {
    if (!isAuthenticated) {
      navigate('/');
    }
  }, [isAuthenticated, navigate]);

  const reload = useCallback(async () => {
    if (!providerId) return;
    setLoading(true);
    setError(null);
    try {
      const all = await listProviders();
      const p = all.find((x) => x.id === providerId) ?? null;
      setProvider(p);
      if (!p) {
        setLoading(false);
        return;
      }
      // 同时拉对局历史 + 钱包 —— 任一失败不影响另一个
      // §135 修复 — 钱包 API 路由参数是 bot_user_id(provider.id != bot_user.id);
      // 后端 listProviders/create/update 现在会在 providerView 附带 bot_user_id,
      // 没有时按"该 provider 尚未生成 bot user"处理(显示空钱包,而不是用错 ID 调
      // getBotWallet 然后拿到 500 错位)。
      const botUserID = p.bot_user_id || '';
      const [g, w] = await Promise.allSettled([
        listProviderGames(p.id, { limit: PAGE_SIZE, offset: 0 }),
        botUserID ? getBotWallet(botUserID) : Promise.reject(new Error('bot_user 未生成')),
      ]);
      if (g.status === 'fulfilled') {
        setGames(g.value.items);
        setGamesTotal(g.value.total);
      } else {
        setGames([]);
        setGamesTotal(0);
      }
      if (w.status === 'fulfilled') {
        setWallet(w.value);
      } else {
        setWallet(null);
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : '加载失败';
      setError(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setLoading(false);
    }
  }, [providerId]);

  useEffect(() => {
    void reload();
  }, [reload]);

  if (!isAuthenticated) return null;
  const isAdmin = userType != null && userType >= 2;
  if (!isAdmin) {
    return (
      <div className="model-admin-page">
        <h1>🤖 {t('modelAdmin.title')}</h1>
        <div className="error">{t('adminUsers.userTypeNormal')}</div>
      </div>
    );
  }

  // §135 — 详情页单 model grant 入口:确认弹窗用 window.confirm 简单实现,
  // 避免在详情页引入第二个 AppModal 状态;高级 UI 在列表页 AppModal 完成。
  const grantOne = async () => {
    if (!provider) return;
    const raw = window.prompt(
      `${t('modelAdmin.detail.grantAmountHint')}\n${t('modelAdmin.detail.grantAmount')}:`,
      '500',
    );
    if (raw == null) return;
    const amount = Number.parseInt(raw, 10);
    if (!Number.isFinite(amount) || amount <= 0 || amount > 1000000) {
      setGrantErr('amount 必须 1..1000000');
      return;
    }
    const remark = window.prompt(
      t('modelAdmin.detail.grantRemark'),
      t('modelAdmin.detail.grantRemark'),
    );
    if (remark == null || remark.trim() === '') return;
    if (!window.confirm(`${t('modelAdmin.detail.grantDailyDesc')}\namount=${amount}\nremark=${remark}`)) {
      return;
    }
    setGranting(true);
    setGrantErr(null);
    setGrantOk(null);
    try {
      const data = await grantDailyToAll({ provider_id: provider.id, amount, remark: remark.trim() });
      const granted = data.granted.find((g) => g.provider_id === provider.id);
      if (granted) {
        setGrantOk(`+${granted.amount} → ${granted.balance_after}`);
      } else {
        setGrantOk(t('modelAdmin.detail.grantSkippedHint'));
      }
      void reload();
    } catch (e: any) {
      const msg = e?.message ?? 'grant failed';
      setGrantErr(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    } finally {
      setGranting(false);
    }
  };

  return (
    <div className="model-admin-page">
      <div className="model-admin-toolbar">
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => navigate('/admin/models')}
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

      {loading && !provider && (
        <div className="model-admin-table-wrapper">
          <div className="empty-state">{t('common.loading')}</div>
        </div>
      )}

      {!loading && !provider && !error && (
        <div className="error">Model not found</div>
      )}

      {provider && (
        <>
          <h1>🤖 {provider.agent_name}</h1>
          <p className="model-admin-page__subtitle">
            <code>{provider.model}</code> · {provider.provider_type}
          </p>

          {/* 基本信息卡片 */}
          <div className="model-card">
            <h2 className="model-card__title">{t('modelAdmin.detail.basicInfo')}</h2>
            <div className="info-grid">
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.fieldAgentName')}</span>
                <span className="info-row__v">{provider.agent_name}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.fieldModel')}</span>
                <span className="info-row__v"><code>{provider.model}</code></span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.fieldProviderType')}</span>
                <span className="info-row__v">{provider.provider_type}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.colApiKeyHint')}</span>
                <span className="info-row__v"><code>{provider.api_key_hint || '-'}</code></span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.fieldEndpoint')}</span>
                <span className="info-row__v">{provider.endpoint || '-'}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.fieldThinkingRequired')}</span>
                <span className="info-row__v">
                  {provider.thinking_enabled
                    ? `🧠 ${provider.thinking_budget_tokens || '-'}`
                    : '—'}
                </span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.fieldEnabled')}</span>
                <span className="info-row__v">
                  {provider.enabled ? '✓' : '✗'}
                </span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.fieldRemark')}</span>
                <span className="info-row__v">{provider.remark || '-'}</span>
              </div>
              <div className="info-row">
                <span className="info-row__k">{t('modelAdmin.colUpdatedAt')}</span>
                <span className="info-row__v">{formatDate(provider.updated_at)}</span>
              </div>
            </div>
          </div>

          {/* 钱包卡片 */}
          <div className="model-card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h2 className="model-card__title">{t('modelAdmin.detail.wallet')}</h2>
              {/* §135 — 详情页单 model grant 入口,仅超管可见 */}
              {isSuper && provider && (
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => void grantOne()}
                  disabled={granting}
                  data-testid="btn-grant-daily-detail"
                  title={t('modelAdmin.detail.grantDailyDesc')}
                >
                  🎁 {t('modelAdmin.detail.grantDaily')}
                </button>
              )}
            </div>
            {grantErr && <div className="error">{grantErr}</div>}
            {grantOk && <div className="info">{grantOk}</div>}

            {!wallet && (
              <div className="empty-state">{t('modelAdmin.detail.noTx')}</div>
            )}
            {wallet && (
              <>
                <div className="wallet-stats">
                  <div className="wallet-stat">
                    <span className="wallet-stat__k">{t('modelAdmin.detail.balance')}</span>
                    <span className="wallet-stat__v wallet-stat__v--big">
                      {formatBalance(wallet.balance, lang)}
                    </span>
                  </div>
                  <div className="wallet-stat">
                    <span className="wallet-stat__k">{t('modelAdmin.detail.totalEarned')}</span>
                    <span className="wallet-stat__v wallet-stat__v--gain">
                      +{formatBalance(wallet.total_earned, lang)}
                    </span>
                  </div>
                  <div className="wallet-stat">
                    <span className="wallet-stat__k">{t('modelAdmin.detail.totalSpent')}</span>
                    <span className="wallet-stat__v wallet-stat__v--loss">
                      -{formatBalance(wallet.total_spent, lang)}
                    </span>
                  </div>
                </div>
                <h3 className="model-card__subtitle">
                  {t('modelAdmin.detail.transactions')}
                </h3>
                {wallet.transactions.length === 0 ? (
                  <div className="empty-state">{t('modelAdmin.detail.noTx')}</div>
                ) : (
                  <div className="model-admin-table-wrapper">
                    <table className="admin-users-table">
                      <thead>
                        <tr>
                          <th>{t('wallet.time')}</th>
                          <th>{t('wallet.reason')}</th>
                          <th style={{ textAlign: 'right' }}>{t('wallet.amount')}</th>
                          <th style={{ textAlign: 'right' }}>余额</th>
                        </tr>
                      </thead>
                      <tbody>
                        {wallet.transactions.slice(0, 20).map((tx: WalletTx) => (
                          <tr key={tx.id}>
                            <td>{formatDate(tx.created_at)}</td>
                            <td>{tx.remark || tx.tx_type}</td>
                            <td
                              style={{
                                textAlign: 'right',
                                color: tx.amount >= 0
                                  ? 'var(--ok)'
                                  : 'var(--danger)',
                              }}
                            >
                              {tx.amount >= 0 ? '+' : ''}
                              {formatBalance(tx.amount, lang)}
                            </td>
                            <td style={{ textAlign: 'right' }}>
                              {formatBalance(tx.balance_after, lang)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            )}
          </div>

          {/* 对局历史 */}
          <div className="model-card">
            <h2 className="model-card__title">{t('modelAdmin.detail.games')}</h2>
            {games.length === 0 ? (
              <div className="empty-state">{t('modelAdmin.detail.noGames')}</div>
            ) : (
              <div className="model-admin-table-wrapper">
                <table className="admin-users-table">
                  <thead>
                    <tr>
                      <th>{t('modelAdmin.detail.colGameKind')}</th>
                      <th>{t('modelAdmin.detail.colStartedAt')}</th>
                      <th>{t('modelAdmin.detail.colResult')}</th>
                      <th style={{ textAlign: 'right' }}>{t('modelAdmin.detail.colCoinDelta')}</th>
                      <th style={{ textAlign: 'right' }}>{t('modelAdmin.detail.colLlmCalls')}</th>
                      <th>{t('modelAdmin.detail.colAction')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {games.map((g) => (
                      <tr key={g.id}>
                        <td>{g.game_kind}</td>
                        <td>{formatDate(g.started_at)}</td>
                        <td>
                          <span className={'result-badge ' + (RESULT_CLASS[g.result] ?? '')}>
                            {t((RESULT_KEY[g.result] ?? 'adminUsers.errorEmpty') as never)}
                          </span>
                        </td>
                        <td
                          style={{
                            textAlign: 'right',
                            color: g.coin_delta > 0
                              ? 'var(--ok)'
                              : g.coin_delta < 0
                                ? 'var(--danger)'
                                : 'var(--muted)',
                          }}
                        >
                          {g.coin_delta > 0 ? '+' : ''}
                          {g.coin_delta}
                        </td>
                        <td style={{ textAlign: 'right' }}>{g.llm_call_count}</td>
                        <td>
                          <button
                            type="button"
                            className="btn btn-secondary btn-sm"
                            onClick={() =>
                              navigate(
                                `/admin/models/${encodeURIComponent(providerId)}/games/${encodeURIComponent(g.id)}`,
                              )
                            }
                            data-testid={`row-view-${g.id}`}
                          >
                            {t('modelAdmin.detail.viewGame')}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <div className="model-admin-page__subtitle" style={{ marginTop: 8 }}>
                  共 {gamesTotal} 条,显示前 {Math.min(games.length, PAGE_SIZE)}
                </div>
              </div>
            )}
          </div>
        </>
      )}

      <style>{`
        .model-card {
          margin-top: 20px;
          padding: 16px 20px;
          background: linear-gradient(180deg, var(--panel) 0%, rgba(22,27,34,0.85) 100%);
          border: 1px solid var(--border);
          border-radius: 10px;
          box-shadow: 0 8px 24px rgba(0,0,0,0.35);
        }
        .model-card__title {
          margin: 0 0 12px;
          font-size: 15px;
          font-weight: 600;
          color: var(--text);
        }
        .model-card__subtitle {
          margin: 16px 0 8px;
          font-size: 13px;
          font-weight: 500;
          color: var(--muted);
        }
        .info-grid {
          display: grid;
          grid-template-columns: repeat(2, minmax(0, 1fr));
          gap: 8px 24px;
        }
        .info-row {
          display: flex;
          align-items: baseline;
          gap: 12px;
          padding: 6px 0;
          border-bottom: 1px dashed var(--border);
        }
        .info-row__k {
          flex: 0 0 100px;
          color: var(--muted);
          font-size: 12px;
        }
        .info-row__v {
          flex: 1;
          color: var(--text);
          font-size: 13px;
          word-break: break-all;
        }
        .wallet-stats {
          display: grid;
          grid-template-columns: repeat(3, minmax(0, 1fr));
          gap: 12px;
        }
        .wallet-stat {
          padding: 12px 16px;
          background: var(--bg);
          border: 1px solid var(--border);
          border-radius: 8px;
          display: flex;
          flex-direction: column;
          gap: 6px;
        }
        .wallet-stat__k {
          font-size: 12px;
          color: var(--muted);
        }
        .wallet-stat__v {
          font-size: 18px;
          font-weight: 600;
          color: var(--text);
        }
        .wallet-stat__v--big { font-size: 24px; }
        .wallet-stat__v--gain { color: var(--ok); }
        .wallet-stat__v--loss { color: var(--danger); }
        .result-badge {
          display: inline-block;
          padding: 2px 8px;
          border-radius: 999px;
          font-size: 12px;
          font-weight: 500;
        }
        .result-badge--win {
          background: rgba(63,185,80,0.15);
          color: var(--ok);
          border: 1px solid rgba(63,185,80,0.35);
        }
        .result-badge--lose {
          background: rgba(248,81,73,0.15);
          color: var(--danger);
          border: 1px solid rgba(248,81,73,0.35);
        }
        .result-badge--draw {
          background: rgba(255,196,0,0.15);
          color: #ffc400;
          border: 1px solid rgba(255,196,0,0.35);
        }
        .result-badge--abandoned {
          background: rgba(150,150,150,0.15);
          color: var(--muted);
          border: 1px solid rgba(150,150,150,0.35);
        }
        @media (max-width: 720px) {
          .info-grid { grid-template-columns: 1fr; }
          .wallet-stats { grid-template-columns: 1fr; }
        }
      `}</style>
    </div>
  );
}

export default ModelDetailPage;
