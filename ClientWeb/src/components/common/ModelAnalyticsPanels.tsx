/**
 * ModelAnalyticsPanels.tsx — §20260813-02 U1/U2 运营数据分析面板
 *
 * 两块面板,由自包含的 <ModelAnalyticsSection> 聚合(自带 toggle 按钮 +
 * 数据拉取),供 ModelAdminPage 以最小接线挂载(该页已逼近 §4 1800 行上限,
 * 页内只允许 import + 1 行 JSX):
 *
 *   - ModelWinTrendPanel(U1, T12):GET /api/llm/win-trends —
 *     每模型近 30 天胜率 sparkline + 总胜率 + 按角色/座位明细。
 *   - PropEconomyPanel(U2, T13):GET /api/games/werewolf/prop-economy —
 *     金币四流向汇总条 + 每道具使用/实测中招率 vs 基础中招率表。
 *
 * §121 数据形状:win-trends 是 map 直解;prop-economy 是 wrapper
 * {summary, entries},api 层已显式声明 PropEconomyResponse。
 * §7.1:两块面板均为 best-effort 只读展示,失败静默降级为空态
 * (与雷达图 getRadarStats 的 catch 语义一致)。
 */

import { useEffect, useState } from 'react';
import { getWinTrends, type ModelWinTrend } from '@/api/llm';
import { fetchPropEconomy, type PropEconomyResponse } from '@/api/werewolf';
import { useT } from '@/hooks/useT';

/* ─────────────────── U1:胜率趋势 sparkline ─────────────────── */

/** 纯 SVG sparkline(无第三方图表库)。宽度自适应 viewBox。 */
function WinRateSparkline({ points }: { points: ModelWinTrend['trend'] }) {
  const W = 160;
  const H = 36;
  if (points.length === 0) {
    return <span className="model-analytics-muted">—</span>;
  }
  if (points.length === 1) {
    // 单点画圆点
    const y = H - (points[0].win_rate / 100) * (H - 4) - 2;
    return (
      <svg width={W} height={H} role="img" aria-label="trend">
        <circle cx={W / 2} cy={y} r={3} fill="var(--accent, #7c5cff)" />
      </svg>
    );
  }
  const stepX = W / (points.length - 1);
  const coords = points.map((p, i) => {
    const x = i * stepX;
    const y = H - (p.win_rate / 100) * (H - 4) - 2;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  return (
    <svg width={W} height={H} role="img" aria-label="win-rate trend">
      {/* 50% 基准线 */}
      <line x1={0} y1={H / 2} x2={W} y2={H / 2} stroke="rgba(255,255,255,0.12)" strokeDasharray="3 3" />
      <polyline
        points={coords.join(' ')}
        fill="none"
        stroke="var(--accent, #7c5cff)"
        strokeWidth={1.6}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

/** U1 面板:每模型一行 — sparkline + 摘要徽章 + 角色/座位折叠明细。 */
export function ModelWinTrendPanel({ data }: { data: Record<string, ModelWinTrend> }) {
  const t = useT();
  const models = Object.values(data).sort((a, b) => b.games - a.games);
  if (models.length === 0) {
    return <div className="model-analytics-empty">{t('modelAdmin.analytics.empty')}</div>;
  }
  return (
    <div className="model-analytics-block">
      <h3 className="model-analytics-h3">{t('modelAdmin.analytics.winTrendTitle')}</h3>
      <table className="admin-users-table admin-users-table--wide model-analytics-table">
        <thead>
          <tr>
            <th>{t('modelAdmin.colAgentName')}</th>
            <th>{t('modelAdmin.analytics.last30d')}</th>
            <th>{t('modelAdmin.analytics.winRate')}</th>
            <th>{t('modelAdmin.analytics.games')}</th>
            <th>{t('modelAdmin.analytics.bestRole')}</th>
            <th>{t('modelAdmin.analytics.bestSeat')}</th>
          </tr>
        </thead>
        <tbody>
          {models.map((m) => {
            const bestRole = m.by_role.length > 0 ? m.by_role[0] : null;
            const bestSeat = m.by_seat.reduce<(typeof m.by_seat)[number] | null>(
              (acc, s) => (acc === null || s.win_rate > acc.win_rate ? s : acc),
              null,
            );
            return (
              <tr key={m.provider_id}>
                <td className="admin-users-table__nick">
                  {m.agent_name}
                  {!m.sample_ok && (
                    <span className="model-analytics-badge model-analytics-badge--warn" title={t('modelAdmin.analytics.sampleLow')}>
                      ⚠
                    </span>
                  )}
                </td>
                <td><WinRateSparkline points={m.trend} /></td>
                <td>{m.win_rate.toFixed(1)}%</td>
                <td>{m.games}</td>
                <td>{bestRole ? `${bestRole.key} (${bestRole.win_rate.toFixed(0)}%)` : '—'}</td>
                <td>{bestSeat ? `#${Number(bestSeat.key) + 1} (${bestSeat.win_rate.toFixed(0)}%)` : '—'}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {/* 角色/座位明细折叠区 */}
      {models.map((m) => (
        (m.by_role.length > 0 || m.by_seat.length > 0) && (
          <details key={`detail-${m.provider_id}`} className="model-analytics-details">
            <summary>
              {m.agent_name} — {t('modelAdmin.analytics.byRole')} / {t('modelAdmin.analytics.bySeat')}
            </summary>
            <div className="model-analytics-details-grid">
              <div>
                <h4>{t('modelAdmin.analytics.byRole')}</h4>
                <ul>
                  {m.by_role.map((r) => (
                    <li key={`r-${r.key}`}>
                      {r.key}: {r.win_rate.toFixed(1)}% ({r.wins}/{r.games})
                    </li>
                  ))}
                </ul>
              </div>
              <div>
                <h4>{t('modelAdmin.analytics.bySeat')}</h4>
                <ul>
                  {m.by_seat.map((s) => (
                    <li key={`s-${s.key}`}>
                      #{Number(s.key) + 1}: {s.win_rate.toFixed(1)}% ({s.wins}/{s.games})
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </details>
        )
      ))}
    </div>
  );
}

/* ─────────────────── U2:道具经济分析 ─────────────────── */

/** U2 面板:金币四流向汇总条 + 每道具实测/基础中招率对比表。 */
export function PropEconomyPanel({ data }: { data: PropEconomyResponse }) {
  const t = useT();
  const { summary, entries } = data;
  if (entries.length === 0) {
    return <div className="model-analytics-empty">{t('modelAdmin.analytics.empty')}</div>;
  }
  // 金币三流向占比(销毁/回彩/补偿),总支出为分母。
  const spent = Math.max(1, summary.total_spent);
  const segPot = (summary.total_pot_return / spent) * 100;
  const segAbsorb = (summary.total_system_absorb / spent) * 100;
  const segComp = (summary.total_target_compens / spent) * 100;
  return (
    <div className="model-analytics-block">
      <h3 className="model-analytics-h3">{t('modelAdmin.analytics.propEconTitle')}</h3>
      <div className="model-analytics-flowbar" role="img" aria-label="coin flow">
        <div className="model-analytics-flowbar__seg model-analytics-flowbar__seg--pot" style={{ width: `${segPot}%` }}
          title={`${t('modelAdmin.analytics.potReturn')}: ${summary.total_pot_return}`} />
        <div className="model-analytics-flowbar__seg model-analytics-flowbar__seg--absorb" style={{ width: `${segAbsorb}%` }}
          title={`${t('modelAdmin.analytics.systemAbsorb')}: ${summary.total_system_absorb}`} />
        <div className="model-analytics-flowbar__seg model-analytics-flowbar__seg--comp" style={{ width: `${segComp}%` }}
          title={`${t('modelAdmin.analytics.targetCompens')}: ${summary.total_target_compens}`} />
      </div>
      <div className="model-analytics-flowlegend">
        <span><i className="model-analytics-dot model-analytics-dot--pot" />{t('modelAdmin.analytics.potReturn')} {segPot.toFixed(0)}%</span>
        <span><i className="model-analytics-dot model-analytics-dot--absorb" />{t('modelAdmin.analytics.systemAbsorb')} {segAbsorb.toFixed(0)}%</span>
        <span><i className="model-analytics-dot model-analytics-dot--comp" />{t('modelAdmin.analytics.targetCompens')} {segComp.toFixed(0)}%</span>
        <span className="model-analytics-muted">
          {t('modelAdmin.analytics.totalSpent')}: {summary.total_spent} · {t('modelAdmin.analytics.hitRate')}: {summary.overall_hit_rate.toFixed(1)}%
        </span>
      </div>
      <table className="admin-users-table admin-users-table--wide model-analytics-table">
        <thead>
          <tr>
            <th>{t('modelAdmin.analytics.propName')}</th>
            <th>{t('modelAdmin.analytics.price')}</th>
            <th>{t('modelAdmin.analytics.uses')}</th>
            <th>{t('modelAdmin.analytics.hitRate')}</th>
            <th>{t('modelAdmin.analytics.baseHitRate')}</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e) => {
            const diff = e.hit_rate - e.base_hit_rate;
            const cls = diff > 5 ? 'model-analytics-over' : diff < -5 ? 'model-analytics-under' : '';
            return (
              <tr key={e.prop_id}>
                <td className="admin-users-table__nick">{e.name_zh || e.prop_key}</td>
                <td>{e.price}</td>
                <td>{e.uses}</td>
                <td className={cls}>
                  {e.hit_rate.toFixed(1)}% ({e.hits}/{e.uses})
                </td>
                <td>{e.base_hit_rate}%</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/* ─────────────────── 自包含聚合区(ModelAdminPage 挂载点) ─────────────────── */

/**
 * ModelAnalyticsSection — 自带 toggle + 数据拉取的聚合区。
 * ModelAdminPage 仅需 `<ModelAnalyticsSection show={isAdmin} />` 一行接线。
 */
export function ModelAnalyticsSection({ show }: { show: boolean }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [winTrends, setWinTrends] = useState<Record<string, ModelWinTrend> | null>(null);
  const [propEcon, setPropEcon] = useState<PropEconomyResponse | null>(null);

  useEffect(() => {
    if (!show || !open) return;
    if (winTrends === null) {
      void getWinTrends().then(setWinTrends).catch(() => setWinTrends({}));
    }
    if (propEcon === null) {
      void fetchPropEconomy().then((r) => setPropEcon(r ?? { summary: { total_uses: 0, total_hits: 0, overall_hit_rate: 0, total_spent: 0, total_pot_return: 0, total_system_absorb: 0, total_target_compens: 0 }, entries: [] }));
    }
  }, [show, open, winTrends, propEcon]);

  if (!show) return null;
  return (
    <div className="model-analytics-section">
      <button
        type="button"
        className={'btn btn-secondary' + (open ? ' btn-primary' : '')}
        onClick={() => setOpen((v) => !v)}
        data-testid="toggle-analytics"
      >
        📈 {t('modelAdmin.analytics.toggle')}
      </button>
      {open && (
        <div className="model-analytics-body">
          {winTrends === null && propEcon === null && (
            <div className="model-analytics-empty">{t('common.loading')}</div>
          )}
          {winTrends !== null && <ModelWinTrendPanel data={winTrends} />}
          {propEcon !== null && <PropEconomyPanel data={propEcon} />}
        </div>
      )}
    </div>
  );
}

export default ModelAnalyticsSection;
