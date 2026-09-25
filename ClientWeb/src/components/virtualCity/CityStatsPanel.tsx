/**
 * CityStatsPanel — 城市背景层面板（§20260921 建房解耦，契约 §2.4）。
 *
 * 数据源 game.state.city（VirtualCityCitySnapshot，view.go omitempty）：
 * resident_count=0 的旧房不下发 → 本面板整块不渲染（return null）。
 *
 * 展示分四段：
 *   ① 头部 `🏙 城市 · N 人`（+ 「👥 居民档案」按钮 → ResidentProfileDrawer）
 *      + 锚定进度条（city.profiles：hydrating=done/total 进度条；
 *      ready=✓ 已锚定 N 份真实档案；failed/idle=灰字说明 —— 档案锚定设计 §8.3）
 *   ② 指标网格（就业率 / 收入中位数 / 居民储蓄合计 /
 *      压力率；金额千分位、率百分比 —— formatCny / formatPct 复用）
 *   ③ 城区人口迷你条形（v2.12 阶段 2：自适应 8→16 城区；auto-fill 网格 +
 *      底对齐竖条，相对最大区人口；城区数 >12 时紧凑模式仅显示 top 10 +
 *      「其他 N 个」；数值以文本显式标注在条外，色盲不依赖颜色判读 —— CLAUDE.md §26）
 *   ④ 「居民之声」最近列表（月份 + 姓名 + 一句话 + 服务模型小徽标；
 *      锚定后 resident_id 非空 → 显示「姓名·职业」并可点击打开档案抽屉定位该卡）
 *
 * 样式在同名 CityStatsPanel.css（组件内直接 import， precedent：
 * ChatSettingsModal.css / WalletModal.css；不新增 globals.css 入口）。
 * 抽屉样式在 styles/virtualCity-residents.css（globals.css 链尾 @import）。
 */

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  formatCny,
  formatPct,
  type VirtualCityCitySnapshot,
  type VirtualCityPlayer,
  type VirtualCityPublicServices,
} from '@/types/virtualCity';
import { ResidentProfileDrawer } from '@/components/virtualCity/ResidentProfileDrawer';
import { CollapsibleSection } from '@/components/virtualCity/CollapsibleSection';
import './CityStatsPanel.css';
import './virtualCity-batch20.css';

interface Props {
  /** 城市背景层快照；缺省（旧房 / 尚未到达）时整面板不渲染。 */
  city?: VirtualCityCitySnapshot | null;
  /** 房间 ID（居民档案抽屉 REST 拉取用）。 */
  roomId: string;
  /** 批次 20 文档 3 A4：选举段快照（public_services）；未启用/无票型 → 政务组不渲染。 */
  election?: VirtualCityPublicServices | null;
  /** 票型表格座位→昵称查表（可选）。 */
  players?: VirtualCityPlayer[];
}

/** 城区人口竖条单元（v2.12 阶段 2）：人口数 / 底对齐条形 / 区名（自上而下）。
 *  条形高度 = population / maxPop 百分比；条形为纯装饰通道（aria-hidden），
 *  数值走顶部显式文字（§26 色盲可判读）。dimmed = 紧凑模式「其他」聚合行。 */
function DistrictBarCell({
  id,
  name,
  population,
  maxPop,
  dimmed = false,
}: {
  id: number;
  name: string;
  population: number;
  maxPop: number;
  dimmed?: boolean;
}) {
  const pct = Math.max(4, Math.round((population / maxPop) * 100));
  return (
    <div
      className={`virtualCity-citypanel__district${dimmed ? ' virtualCity-citypanel__district--other' : ''}`}
      data-testid={`virtualCity-city-district-${id}`}
      title={`${name} · ${population.toLocaleString()}`}
    >
      <span className="virtualCity-citypanel__district-pop">{population.toLocaleString()}</span>
      <span className="virtualCity-citypanel__district-bar">
        <span
          className="virtualCity-citypanel__district-fill"
          style={{ height: `${pct}%` }}
          aria-hidden="true"
        />
      </span>
      <span className="virtualCity-citypanel__district-name">{name}</span>
    </div>
  );
}

export function CityStatsPanel({ city, roomId, election, players }: Props) {
  const t = useT();
  // 档案抽屉开关 + 定位卡号（城市之声 resident_id 点击进入）。
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerCardId, setDrawerCardId] = useState<string | null>(null);
  // 批次 20 文档 3 A4：政务票型表格折叠（默认展开一次选举数据；内滚动防溢出）。
  const [govOpen, setGovOpen] = useState(true);
  // 批次 20 §3.4：32 区紧凑模式新增「展开全部 32 区」折叠列表 —— 展开态网格
  // 滚动内收（max-height + overflow-y，控件不出界，防溢出规约 18-04）。
  const [districtsExpanded, setDistrictsExpanded] = useState(false);

  const openDrawer = (cardId: string | null) => {
    setDrawerCardId(cardId);
    setDrawerOpen(true);
  };

  if (!city) return null;

  const districts = city.districts ?? [];
  const maxPop = districts.reduce((m, d) => Math.max(m, d.population || 0), 0) || 1;
  // v2.12 阶段 2：16 城区自适应 —— >12 区切换紧凑模式（top 10 + 「其他 N 个」），
  // 其余区人口合计展示，避免面板无限增高。行序 = 人口降序（紧凑）/ 服务端序（≤12）。
  const COMPACT_THRESHOLD = 12;
  const COMPACT_TOP = 10;
  const compact = districts.length > COMPACT_THRESHOLD;
  const ranked = compact
    ? [...districts].sort((a, b) => (b.population || 0) - (a.population || 0))
    : districts;
  const restRows = compact ? ranked.slice(COMPACT_TOP) : [];
  const districtRows = compact ? ranked.slice(0, COMPACT_TOP) : ranked;
  const gridRows = compact && districtsExpanded ? ranked : districtRows;
  const gridScroll = compact && districtsExpanded;
  const voices = (city.voices ?? []).slice(-8).reverse();
  const prof = city.profiles ?? null;
  const hydratePct =
    prof && prof.status === 'hydrating' && prof.total > 0
      ? Math.min(100, Math.round((prof.done / prof.total) * 100))
      : 0;

  return (
    <>
      {/* 阶段 E（13-3D城市渲染优化）：融合式折叠 —— 原 ① 标题头改为
          CollapsibleSection 标题行（title + headerExtra），不叠加第二层标题；
          折叠态 localStorage 持久化（virtualCity.ui.collapsed.city_stats）。 */}
      <CollapsibleSection
        className="virtualCity-citypanel"
        storageKey="virtualCity.ui.collapsed.city_stats"
        defaultCollapsed
        testId="virtualCity-city-panel"
        title={
          <>
            🏙 {t('virtualCity.cityTitle' as TKey)} · {(city.resident_count || 0).toLocaleString()}{' '}
            {t('virtualCity.cityPopulation' as TKey)}
          </>
        }
        headerExtra={
          <button
            type="button"
            className="virtualCity-citypanel__open"
            onClick={() => openDrawer(null)}
            aria-label={t('virtualCity.cityProfiles.openDrawer' as TKey)}
            data-testid="virtualCity-city-open-profiles"
          >
            👥 {t('virtualCity.cityProfiles.openDrawer' as TKey)}
          </button>
        }
      >

      {/* ①+ 锚定进度条（档案锚定设计 §8.3；city.profiles 缺省=旧房不渲染） */}
      {prof && (
        <div
          className={`virtualCity-citypanel__anchor virtualCity-citypanel__anchor--${prof.status}`}
          title={t('virtualCity.cityProfiles.title' as TKey)}
          data-testid="virtualCity-city-anchor"
        >
          {prof.status === 'hydrating' ? (
            <>
              <span className="virtualCity-citypanel__anchor-text">
                {t('virtualCity.cityProfiles.anchorProgress' as TKey, {
                  done: prof.done,
                  total: prof.total,
                  pool: prof.pool_size,
                })}
              </span>
              <span
                className="virtualCity-citypanel__anchor-bar"
                role="progressbar"
                aria-valuenow={hydratePct}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <span
                  className="virtualCity-citypanel__anchor-fill"
                  style={{ width: `${hydratePct}%` }}
                  aria-hidden="true"
                />
              </span>
              <span className="virtualCity-citypanel__anchor-pct">{hydratePct}%</span>
            </>
          ) : prof.status === 'ready' ? (
            <span className="virtualCity-citypanel__anchor-text virtualCity-citypanel__anchor-text--ready">
              ✓ {t('virtualCity.cityProfiles.anchorReady' as TKey, { n: prof.anchored })}
            </span>
          ) : (
            <span className="virtualCity-citypanel__anchor-text">
              {prof.status === 'failed'
                ? t('virtualCity.cityProfiles.anchorFailed' as TKey)
                : t('virtualCity.cityProfiles.anchorIdle' as TKey)}
            </span>
          )}
        </div>
      )}

      {/* 指标网格：就业率 / 收入中位数 / 居民储蓄合计 / 压力率 */}
      <div className="virtualCity-citypanel__stats">
        <div className="virtualCity-citypanel__stat">
          <span className="virtualCity-citypanel__stat-k">{t('virtualCity.cityEmployment' as TKey)}</span>
          <span className="virtualCity-citypanel__stat-v">{formatPct(city.employment_rate)}</span>
        </div>
        <div className="virtualCity-citypanel__stat">
          <span className="virtualCity-citypanel__stat-k">{t('virtualCity.cityMedianIncome' as TKey)}</span>
          <span className="virtualCity-citypanel__stat-v">{formatCny(city.median_income)}</span>
        </div>
        <div className="virtualCity-citypanel__stat">
          <span className="virtualCity-citypanel__stat-k">{t('virtualCity.citySavings' as TKey)}</span>
          <span className="virtualCity-citypanel__stat-v">{formatCny(city.total_savings)}</span>
        </div>
        <div className="virtualCity-citypanel__stat">
          <span className="virtualCity-citypanel__stat-k">{t('virtualCity.cityStress' as TKey)}</span>
          <span className="virtualCity-citypanel__stat-v">{formatPct(city.stressed_rate)}</span>
        </div>
        {/* 17-CityHuman 全民驱动 — 驱动层指示（02 §6：driver 块存在时渲染）。 */}
        {city.driver && (
          <div className="virtualCity-citypanel__stat" data-testid="virtualCity-city-driver">
            <span className="virtualCity-citypanel__stat-k">🤖</span>
            <span className="virtualCity-citypanel__stat-v">
              {t('virtualCity.cityDriven' as TKey, { n: city.driver.driven_last || 0 })}
            </span>
          </div>
        )}
        {/* 2026-09-25 §LLM线路池配额 — 本房生效线路数（旧房 driver.lines 缺省不渲染）。 */}
        {city.driver && city.driver.lines > 0 && (
          <div className="virtualCity-citypanel__stat" data-testid="virtualCity-city-driver-lines">
            <span className="virtualCity-citypanel__stat-k">🔌</span>
            <span className="virtualCity-citypanel__stat-v">
              {t('virtualCity.cityDriverLines' as TKey, { n: city.driver.lines })}
            </span>
          </div>
        )}
      </div>

      {/* ② 城区人口迷你条形（v2.12 阶段 2：auto-fill 网格 + 底对齐竖条，§26 对比度：
          实底条形 + 显式文字；>12 区紧凑模式 top 10 + 「其他 N 个」） */}
      {districts.length > 0 && (
        <div className="virtualCity-citypanel__districts-wrap">
          <div
            className={`virtualCity-citypanel__districts${gridScroll ? ' virtualCity-citypanel__districts--scroll' : ''}`}
          >
            {gridRows.map((d, i) => (
              <DistrictBarCell
                key={d.id >= 0 ? d.id : `row-${i}`}
                id={d.id}
                name={d.name}
                population={d.population || 0}
                maxPop={maxPop}
              />
            ))}
            {compact && !districtsExpanded && restRows.length > 0 && (
              <DistrictBarCell
                id={-1}
                name={t('virtualCity.cityOtherDistricts' as TKey, { n: restRows.length })}
                population={restRows.reduce((s, d) => s + (d.population || 0), 0)}
                maxPop={maxPop}
                dimmed
              />
            )}
          </div>
          {compact && (
            <button
              type="button"
              className="virtualCity-citypanel__expand"
              onClick={() => setDistrictsExpanded((v) => !v)}
              aria-expanded={districtsExpanded}
              data-testid="virtualCity-city-districts-expand"
            >
              {districtsExpanded
                ? `▲ ${t('virtualCity.cityDistrictsCollapse' as TKey)}`
                : `▼ ${t('virtualCity.cityDistrictsExpand' as TKey, { n: districts.length })}`}
            </button>
          )}
        </div>
      )}

      {/* ②.5 政务（批次 20 文档 3 A4）：市长选举票型表格。
          未启用选举 / 无 last_votes → 整组不渲染；表格内滚动，控件不出界（18-04）。 */}
      {election?.election_enabled && (election.last_votes?.length ?? 0) > 0 && (
        <div className="virtualCity-citypanel__gov" data-testid="virtualCity-gov-panel">
          <button
            type="button"
            className="virtualCity-citypanel__gov-title"
            onClick={() => setGovOpen((v) => !v)}
            aria-expanded={govOpen}
            data-testid="virtualCity-gov-toggle"
          >
            {govOpen ? '▲' : '▼'} 🗳 {t('virtualCity.election.votePanel' as TKey)}
          </button>
          {govOpen && (
            <div className="virtualCity-citypanel__gov-scroll">
              {(election.last_votes ?? []).map((v) => {
                const name = players?.find((p) => p.seat === v.seat)?.nickname;
                const isMayor = v.seat === election.mayor_seat;
                return (
                  <div
                    key={v.seat}
                    className={`virtualCity-citypanel__gov-row${isMayor ? ' virtualCity-citypanel__gov-row--winner' : ''}`}
                    data-testid={`virtualCity-gov-row-${v.seat}`}
                  >
                    <span className="virtualCity-citypanel__gov-seat" title={name ?? undefined}>
                      S{v.seat + 1}
                      {isMayor ? ' 🗳' : ''}
                      {name ? ` ${name}` : ''}
                    </span>
                    <span className="virtualCity-citypanel__gov-bars">
                      {([
                        ['virtualCity.election.colScore', v.score],
                        ['virtualCity.election.colWealth', v.virtualCity_score],
                        ['virtualCity.election.colNetwork', v.network_score],
                        ['virtualCity.election.colSatisfaction', v.satisfaction],
                      ] as const).map(([k, val]) => (
                        <span className="virtualCity-citypanel__gov-bar" key={k}>
                          <span className="virtualCity-citypanel__gov-k">{t(k as TKey)}</span>
                          <span className="virtualCity-citypanel__gov-track">
                            {/* bar 宽度 = score%（0..100 直渲；文档 3 A4） */}
                            <span
                              className="virtualCity-citypanel__gov-fill"
                              style={{ width: `${Math.max(0, Math.min(100, val))}%` }}
                              aria-hidden="true"
                            />
                          </span>
                          <span className="virtualCity-citypanel__gov-v">{Math.round(val)}</span>
                        </span>
                      ))}
                    </span>
                  </div>
                );
              })}
              {(election.next_election_month ?? 0) > 0 && (
                <div className="virtualCity-citypanel__gov-note">
                  {t('virtualCity.election.nextElection' as TKey, { m: election.next_election_month ?? 0 })}
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* ③ 居民之声（最近若干条，倒序展示最新在前） */}
      {voices.length > 0 && (
        <div className="virtualCity-citypanel__voices">
          <div className="virtualCity-citypanel__voices-title">
            🗣 {t('virtualCity.cityVoices' as TKey)}
          </div>
          {voices.map((v, i) => (
            <div
              key={`${v.month}-${v.name}-${i}`}
              className="virtualCity-citypanel__voice"
              data-testid="virtualCity-city-voice"
            >
              <span className="virtualCity-citypanel__voice-meta">
                <span className="virtualCity-citypanel__voice-month">
                  {t('virtualCity.cityVoiceOfMonth' as TKey, { month: v.month })}
                </span>
                {/* 锚定后 resident_id 非空 → 「姓名·职业」可点击打开档案抽屉定位该卡 */}
                {v.resident_id ? (
                  <button
                    type="button"
                    className="virtualCity-citypanel__voice-name virtualCity-citypanel__voice-name--link"
                    onClick={() => openDrawer(v.resident_id ?? null)}
                    title={t('virtualCity.residentDrawer.voiceOf' as TKey, { name: v.name })}
                    data-testid={`virtualCity-city-voice-link-${i}`}
                  >
                    {v.name}
                    {v.occupation ? `·${v.occupation}` : ''}
                  </button>
                ) : (
                  <span className="virtualCity-citypanel__voice-name">{v.name}</span>
                )}
                {v.model && (
                  <span className="virtualCity-citypanel__voice-model" title={v.model}>
                    🤖 {v.model}
                  </span>
                )}
              </span>
              <span className="virtualCity-citypanel__voice-text">{v.text}</span>
            </div>
          ))}
        </div>
      )}

      {/* 居民档案抽屉（fixed 覆盖层，挂在折叠体之外：折叠不卸载抽屉；
          key=定位卡号：切换定位卡 / 重新打开时整体重挂载，state 归零）。 */}
      </CollapsibleSection>
      <ResidentProfileDrawer
        key={drawerCardId ?? 'all'}
        open={drawerOpen}
        roomId={roomId}
        focusCardId={drawerCardId}
        onClose={() => {
          setDrawerOpen(false);
          setDrawerCardId(null);
        }}
      />
    </>
  );
}

export default CityStatsPanel;
