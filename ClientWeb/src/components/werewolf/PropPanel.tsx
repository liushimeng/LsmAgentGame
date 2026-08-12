/**
 * PropPanel — 狼人杀 13 人局「道具系统」UI
 *
 * 2026-07-21 §13 道具系统:
 *   - 6 种心理战武器(对应 6 类 LLM 注入攻击)。
 *   - 仅白天发言阶段(phase=speak)+ 存活人类玩家可用。
 *   - 显示:道具列表(emoji+名称+价格+中招率+描述)、目标座位选择(存活/排除自己/
 *     狼人暴露类禁对队友)、金币余额与消耗、本局剩余次数、冷却倒计时、"使用"按钮。
 *
 * §7.1:失败必须当前页最高可见 — 使用 formError 内联 + reportGlobalError 兜底。
 * §12.5:挂在右侧栏或中栏不影响底部座位布局;这里采用中栏内嵌面板,与 DayControlPanel 同位。
 *
 * 对齐 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §6。
 */

import React, { useEffect, useMemo, useState } from 'react';
import type { PropUseEvent, WerewolfGameState, WerewolfProp } from '@/types/werewolf';
import { fetchProps } from '@/api/werewolf';
import { useWerewolfStore } from '@/store/werewolf.store';
import { useT } from '@/hooks/useT';
import { reportGlobalError, errorMessage } from '@/services/globalError';
import { isSessionExpiredError } from '@/services/http';

/** 道具 prop_key 对应默认 emoji(后端也会下发 prop_emoji,本兜底仅用于离线/失败场景)。 */
const PROP_EMOJI_FALLBACK: Record<string, string> = {
  markdown_bomb: '📰',
  nested_maze: '🎭',
  char_confuse: '🔣',
  long_swear: '📜',
  task_disguise: '🎪',
  emotion_plea: '🥺',
};

/** 暴露类道具(§1.3 阵营保护 — 狼人不能对狼队友使用)。 */
const EXPOSURE_PROP_KEYS = new Set(['markdown_bomb', 'task_disguise', 'emotion_plea']);

interface Props {
  gameState: WerewolfGameState;
  mySeat: number;
  myRole?: string;
  myFaction?: string;
  /** useProp 回调,由 useWerewolf() 暴露;发 game.werewolf_use_prop 帧。 */
  onUseProp: (propKey: string, targetSeat: number, payload?: string) => void;
  /** 提交中锁定按钮。 */
  busy: boolean;
  // 2026-08-09 §20260808-03 — 玩家死亡守卫。死态时:
  //   - 面板仍渲染(用户能看到金币/档位/历史事件,透明度降低)
  //   - 所有使用按钮强制 disabled
  //   - 顶部加 ☠ 已死亡徽章
  iAmDead?: boolean;
}

/**
 * 决定"我方"是否为狼人阵营。仅对人类玩家暴露道具面板(玩家/观众都是观众禁道具)。
 * 服务端 my_role / my_faction 来自 game.state,本地冗余判定以保证 UI 立即响应。
 */
function isWolfFaction(role?: string, faction?: string): boolean {
  if (faction === 'wolf') return true;
  if (typeof role === 'string' && role.toLowerCase() === 'werewolf') return true;
  return false;
}

const PropPanel: React.FC<Props> = ({ gameState, mySeat, myRole, myFaction, onUseProp, busy, iAmDead = false }) => {
  const t = useT();
  // §7.1 — 内联 formError 优先 + 全局 toast 兜底。
  const [formError, setFormError] = useState<string | null>(null);
  const [selectedProp, setSelectedProp] = useState<string | null>(null);
  const [selectedTarget, setSelectedTarget] = useState<number | null>(null);
  // 2026-08-09 §20260808-03 — 道具列表折叠状态。默认展开;持久化到
  // localStorage(对齐 §23 LS_FOLD_CHAT/LS_FOLD_INFO 模式),刷新后保留。
  // 折叠仅影响列表展示,PropUseOverlay 事件特效正常触发(正交)。
  const LS_PROP_COLLAPSED = 'werewolf.prop.collapsed';
  const [isCollapsed, setIsCollapsed] = useState<boolean>(() => {
    try {
      return typeof window !== 'undefined' && window.localStorage.getItem(LS_PROP_COLLAPSED) === '1';
    } catch {
      return false;
    }
  });
  useEffect(() => {
    try {
      if (typeof window !== 'undefined') {
        window.localStorage.setItem(LS_PROP_COLLAPSED, isCollapsed ? '1' : '0');
      }
    } catch { /* 隐身模式降级 */ }
  }, [isCollapsed]);

  const props = useWerewolfStore((s) => s.props);
  const myBalance = useWerewolfStore((s) => s.propMyBalance);
  const myRemaining = useWerewolfStore((s) => s.propMyRemaining);
  const cooldownSec = useWerewolfStore((s) => s.propCooldownRemainingSec);
  const econTier = useWerewolfStore((s) => s.propEconTier);
  const econTierAbsorbPct = useWerewolfStore((s) => s.propEconTierAbsorbPct);
  const setProps = useWerewolfStore((s) => s.setProps);
  const recentEvents = useWerewolfStore((s) => s.propRecentEvents);

  const isWolf = isWolfFaction(myRole, myFaction);

  // 进入面板时拉取道具目录(失败仅 log,不阻塞 UI)。
  // §R180-P3-OBS2/3 修复:依赖仅基于 room_id 而非完整 ${room}:${phase}:${day} —
  // 之前依赖每次切阶段/天都会取消前一次 fetch,易导致 store 停留在初始 defaults
  // (Health/30%/balance 0/0/0) 而服务器已返回 Critical/60%/12680/6 的情况。
  const roomKey = gameState.room_id;
  const propBalanceEcho = gameState.prop_my_balance;
  const propRemainingEcho = gameState.prop_my_remaining;
  const propCooldownEcho = gameState.prop_cooldown_remaining_sec;
  // §R183-P2-2 修复:服务端 game.state 若补发 econ_tier / econ_tier_absorb_pct
  // echo 字段,PropPanel 立即同步(权威源是服务端),无需等待 fetchProps。
  const propEconTierEcho = gameState.prop_econ_tier;
  const propEconTierAbsorbPctEcho = gameState.prop_econ_tier_absorb_pct;
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const resp = await fetchProps();
        if (cancelled || !resp) return;
        // §R180 §121 教训:后端 wrapper 已展开 data 字段;字段缺失才回落到默认值,确保
        // 真实返回值不被 fallback 覆盖。econ_tier / econ_tier_absorb_pct 是可选字段,
        // 但 my_balance / my_props_remaining / cooldown_remaining_sec 是必填,缺失
        // 视为 null/0 而非默认 0/30 —— §R183-P2-1 修复:null 表示 loading,
        // UI 渲染「同步中…」而非字面量 -1,避免「金币余额 -1」歧义文本。
        setProps(
          resp.props ?? [],
          resp.my_balance ?? null,
          resp.my_props_remaining ?? 0,
          resp.cooldown_remaining_sec ?? 0,
          resp.econ_tier,
          resp.econ_tier_absorb_pct,
        );
      } catch {
        /* fetchProps 已内置 catch 返回 null,此处兜底以防异常逃逸 */
      }
    })();
    return () => {
      cancelled = true;
    };
    // BUG-R238-P2-01 (2026-08-04):roomKey 在整局内恒定,原依赖只在进入面板时拉取一次,
    // 道具消费后金币余额 / 本局剩余 / 冷却不刷新(后端未实现 prop_my_balance 等 echo 字段)。
    // 追加 propRecentEvents.length:玩家自己成功用道具后 store 会 append 事件,触发重新拉取;
    // 服务端道具公开广播驱动 balance 变化,与 REST 权威值一致。
  }, [roomKey, recentEvents.length, setProps]);

  // §R180-P3-OBS2 修复:服务端 game.state 主动下发的余额/剩余/冷却 echo 字段
  // 优先于 REST 缓存(权威源是服务端),避免 fetchProps 在新阶段落后于实际值。
  // §R183-P2-2 修复:同步支持 econ_tier / econ_tier_absorb_pct echo(若后端补发);
  // fetchProps 未返回时仍可通过 echo 拿到真实档位/销毁率,避免 store 永远停在默认 health/30。
  useEffect(() => {
    if (
      propBalanceEcho == null &&
      propRemainingEcho == null &&
      propCooldownEcho == null &&
      propEconTierEcho == null &&
      propEconTierAbsorbPctEcho == null
    ) return;
    // 只更新 echo 字段,保留 props[] / econ_tier 由 fetchProps 优先;echo 仅在 fetchProps
    // 未拉到值时回退 —— store 仍是「最后一次 fetchProps 的快照 + echo 实时覆盖」。
    useWerewolfStore.setState((s) => ({
      propMyBalance: propBalanceEcho ?? s.propMyBalance,
      propMyRemaining: propRemainingEcho ?? s.propMyRemaining,
      propCooldownRemainingSec: propCooldownEcho ?? s.propCooldownRemainingSec,
      propEconTier: propEconTierEcho ?? s.propEconTier,
      propEconTierAbsorbPct: propEconTierAbsorbPctEcho ?? s.propEconTierAbsorbPct,
    }));
  }, [propBalanceEcho, propRemainingEcho, propCooldownEcho, propEconTierEcho, propEconTierAbsorbPctEcho]);

  // 冷却倒计时(本地 tick;服务端 game.state.prop_cooldown_remaining_sec 是权威源)。
  const [cooldownLocal, setCooldownLocal] = useState<number>(cooldownSec);
  useEffect(() => {
    setCooldownLocal(cooldownSec);
    if (cooldownSec <= 0) return;
    const id = setInterval(() => {
      setCooldownLocal((s) => (s > 0 ? s - 1 : 0));
    }, 1000);
    return () => clearInterval(id);
  }, [cooldownSec]);

  // 存活座位候选(排除自己;AOE 道具也选一个 target 让 UI 简单;后端按 is_aoe 决定是否忽略)。
  const aliveCandidates = useMemo<number[]>(() => {
    const out: number[] = [];
    for (let i = 0; i < gameState.max_seat; i++) {
      const p = gameState.players[i];
      if (!p || !p.alive) continue;
      if (i === mySeat) continue;
      out.push(i);
    }
    return out;
  }, [gameState.max_seat, gameState.players, mySeat]);

  // 选中道具(过滤禁用 / 价格 > 余额 / 冷却中 / 剩余次数用尽)。
  const activeProps = useMemo<WerewolfProp[]>(() => {
    return props.filter((p) => p.enabled !== false);
  }, [props]);

  const canUseNow = (p: WerewolfProp): { ok: boolean; reason?: string } => {
    // 2026-08-09 §20260808-03 — 死亡守卫优先级最高,避免后端 40112 拒收。
    if (iAmDead) return { ok: false, reason: t('werewolf.prop.deadLockedHint') };
    if (myRemaining <= 0) return { ok: false, reason: t('werewolf.prop.error.remainingZero') };
    if (cooldownLocal > 0) return { ok: false, reason: t('werewolf.prop.error.cooldown', { sec: cooldownLocal }) };
    // §R183-P2-1 修复:myBalance 可能是 null(loading 中),用 0 比较,按钮将被 disabled,
    // 直到 fetchProps 返回真实值才允许购买。
    if (myBalance == null || myBalance < p.price) return { ok: false, reason: t('werewolf.prop.error.insufficient', { price: p.price, balance: myBalance ?? 0 }) };
    return { ok: true };
  };

  // 处理「使用道具」点击。§7.1 失败 → setFormError + reportGlobalError。
  //
  // BUG-R186-P3 修复: 之前采用「一次点击=选中,二次点击=提交」两段式流程,导致用户
  // 第一次点击时按钮文案从「使用」变到「确认使用 #X」,但没有可见的弹窗 / 网络请求 /
  // 控制台反馈,测试报告误判为「事件绑定失效」。
  // 修复: 一次点击直接提交 ——
  //   - AOE 道具: target=-1,无目标选择。
  //   - 非 AOE 道具: 若未选目标,自动取 aliveCandidates[0] 作为默认目标;提交同时把
  //     selectedProp / selectedTarget 状态亮出来,UI 出现目标高亮 + 按钮「冷却中」反馈,
  //     与后端 prop_cooldown_remaining_sec 同步。
  //   - 用户仍可点目标座位 chip 切换 target 后再点「使用」,等于「切换 + 立即再发」语义。
  const handleUse = async (p: WerewolfProp) => {
    setFormError(null);
    // 切换 prop_key 时清掉旧 target,避免 A 类道具的 target 残留到 B 类道具。
    if (selectedProp !== p.prop_key) {
      setSelectedProp(p.prop_key);
      setSelectedTarget(null);
    }
    // 选定目标(若非 AOE 且尚未选 → 自动取第一个存活候选人)。
    const targetSeat = p.is_aoe ? -1 : (selectedTarget ?? aliveCandidates[0] ?? null);
    if (!p.is_aoe && targetSeat === null) {
      setFormError(t('werewolf.prop.error.noTarget'));
      return;
    }
    // 把选中的 target 同步到 state(让 chip 高亮 + 二次点击时复用)。
    if (!p.is_aoe && selectedTarget === null && targetSeat !== null) {
      setSelectedTarget(targetSeat);
    }
    const verdict = canUseNow(p);
    if (!verdict.ok) {
      setFormError(verdict.reason ?? t('werewolf.prop.error.unknown'));
      return;
    }
    try {
      // AOE 道具 target_seat = -1;后端 prop_engine 会按 is_aoe 判定。
      onUseProp(p.prop_key, targetSeat);
    } catch (e: unknown) {
      if (isSessionExpiredError(e)) return;
      const msg = errorMessage(e, t('werewolf.prop.error.unknown'));
      setFormError(msg);
      reportGlobalError({ message: msg, severity: 'error' });
    }
  };

  return (
    <div
      className={`werewolf-action-panel werewolf-prop-panel${iAmDead ? ' is-dead' : ''}${isCollapsed ? ' is-collapsed' : ''}`}
      data-testid="werewolf-prop-panel"
      data-dead={iAmDead ? '1' : undefined}
    >
      {/* 2026-08-09 §20260808-03 — header 包含 title + 死亡徽章 + 折叠按钮。
          折叠/死亡正交:折叠只是收起列表,死态不强制折叠(用户可能想看历史)。
          折叠后整个 props list / recent events 隐藏,header 仍可点。 */}
      <div className="werewolf-prop-panel__header">
        <h4>
          🎴 {t('werewolf.prop.title')}
          {iAmDead && (
            <span className="werewolf-prop-panel__dead-badge" data-testid="werewolf-prop-dead-badge">
              {t('werewolf.prop.deadBadge')}
            </span>
          )}
        </h4>
        <button
          type="button"
          className="werewolf-prop-panel__collapse-btn"
          onClick={() => setIsCollapsed((v) => !v)}
          data-testid="werewolf-prop-collapse"
          aria-label={isCollapsed ? t('werewolf.prop.expand') : t('werewolf.prop.collapse')}
          title={isCollapsed ? t('werewolf.prop.expand') : t('werewolf.prop.collapse')}
        >
          {isCollapsed ? `▶ ${t('werewolf.prop.expand')}` : `▲ ${t('werewolf.prop.collapse')}`}
        </button>
      </div>
      {isCollapsed && (
        <p className="werewolf-prop-panel__collapsed-hint">
          {iAmDead ? t('werewolf.prop.deadLockedHint') : t('werewolf.prop.collapse')}
        </p>
      )}
      {/* v5 EconTier 5 档徽章 — 展示当前房间经济档位 + 销毁率。
          后端 ComputeEconTier 输出;UI 与 server docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md §16.3 表格对齐 */}
      <p
        className={`werewolf-prop-panel__econ-tier econ-tier-${econTier}`}
        data-testid="werewolf-prop-econ-tier"
      >
        <span className="werewolf-prop-panel__econ-tier-label">
          {t('werewolf.prop.econTier.label')}:
        </span>
        <span className="werewolf-prop-panel__econ-tier-value">
          {t(`werewolf.prop.econTier.${econTier}` as const)}
        </span>
        <span className="werewolf-prop-panel__econ-tier-absorb">
          {' · '}{t('werewolf.prop.econTier.absorbPct', { pct: econTierAbsorbPct })}
        </span>
      </p>
      <p className="werewolf-prop-panel__balance">
        {/* §R183-P2-1 修复:myBalance 为 null(loading 哨兵)时显示「同步中…」,
            避免 UI 展示字面量 -1 造成的「金币余额 -1」歧义文本;真实余额走正常分支。 */}
        {myBalance == null
          ? t('werewolf.prop.balanceLoading')
          : t('werewolf.prop.balance', { balance: myBalance })}
        {' · '}
        {t('werewolf.prop.remaining', { count: myRemaining })}
        {cooldownLocal > 0 && (
          <span className="werewolf-prop-panel__cooldown">
            {' · '}{t('werewolf.prop.cooldown', { sec: cooldownLocal })}
          </span>
        )}
      </p>
      {formError && (
        <p className="werewolf-prop-panel__error" role="alert" data-testid="werewolf-prop-error">
          {formError}
        </p>
      )}

      {activeProps.length === 0 ? (
        <p className="werewolf-prop-panel__empty">{t('werewolf.prop.empty')}</p>
      ) : (
        <ul className="werewolf-prop-panel__list">
          {activeProps.map((p) => {
            const verdict = canUseNow(p);
            const selected = selectedProp === p.prop_key;
            return (
              <li
                key={p.prop_key}
                className={`werewolf-prop-panel__item ${selected ? 'is-selected' : ''}`}
                data-testid={`werewolf-prop-item-${p.prop_key}`}
              >
                <div className="werewolf-prop-panel__item-head">
                  <span className="werewolf-prop-panel__emoji">
                    {p.prop_emoji || PROP_EMOJI_FALLBACK[p.prop_key] || '🎴'}
                  </span>
                  <span className="werewolf-prop-panel__name">
                    {p.name_zh} <small>({p.name_en})</small>
                  </span>
                  <span className="werewolf-prop-panel__price">
                    💰 {p.price}
                  </span>
                  <span className="werewolf-prop-panel__hit">
                    🎯 {p.base_hit_rate}%
                  </span>
                  {p.is_aoe && (
                    <span className="werewolf-prop-panel__aoe">AOE</span>
                  )}
                </div>
                <p className="werewolf-prop-panel__desc">{p.description}</p>

                {selected && !p.is_aoe && (
                  <div className="werewolf-prop-panel__targets" role="radiogroup" aria-label={t('werewolf.prop.targetLabel')}>
                    {aliveCandidates.map((s) => {
                      const targetP = gameState.players[s];
                      // 阵营保护:暴露类道具不能对同阵营(狼)使用。
                      const sameFaction = isWolf && isWolfFaction(targetP?.role, targetP?.faction);
                      const disabled = (EXPOSURE_PROP_KEYS.has(p.prop_key) && sameFaction) || busy;
                      return (
                        <button
                          key={s}
                          type="button"
                          role="radio"
                          aria-checked={selectedTarget === s}
                          className={`seat-chip ${selectedTarget === s ? 'is-selected' : ''}`}
                          disabled={disabled}
                          onClick={() => setSelectedTarget(s)}
                          data-testid={`werewolf-prop-target-${s}`}
                          title={
                            disabled && EXPOSURE_PROP_KEYS.has(p.prop_key) && sameFaction
                              ? t('werewolf.prop.error.sameFaction')
                              : `#${s + 1}`
                          }
                        >
                          #{s + 1}
                        </button>
                      );
                    })}
                  </div>
                )}

                <button
                  type="button"
                  className="btn btn-primary werewolf-prop-panel__use"
                  disabled={!verdict.ok || busy}
                  onClick={() => handleUse(p)}
                  data-testid={`werewolf-prop-use-${p.prop_key}`}
                  title={verdict.reason ?? t('werewolf.prop.use')}
                >
                  {/* BUG-R186-P3 修复: 一次点击直接提交,不需要「确认使用 #X」二次点击文案;
                      默认 target = aliveCandidates[0],UI 反馈由目标 chip 高亮 + 服务端
                      prop_cooldown_remaining_sec 同步冷却按钮显示。
                      2026-08-09 §20260808-03 — 死态按钮文案加 ☠ 前缀,冗余写明原因
                      (canUseNow 已在死态时返回 deadLockedHint 覆盖 title)。 */}
                  {iAmDead ? '☠ ' : ''}
                  {p.is_aoe
                    ? t('werewolf.prop.use')
                    : t('werewolf.prop.useWithTarget', { target: (selectedTarget ?? aliveCandidates[0] ?? -1) + 1 })}
                </button>
              </li>
            );
          })}
        </ul>
      )}

      {/* 最近道具使用公开事件流(全房间可见)。最近 10 条。 */}
      {recentEvents.length > 0 && (
        <details className="werewolf-prop-panel__events" data-testid="werewolf-prop-events">
          <summary>{t('werewolf.prop.recentEvents', { count: Math.min(10, recentEvents.length) })}</summary>
          <ul>
            {recentEvents.slice(-10).reverse().map((ev, idx) => (
              <PropEventRow key={`${ev.at}-${ev.from_seat}-${ev.prop_key}-${idx}`} ev={ev} t={t} />
            ))}
          </ul>
        </details>
      )}
    </div>
  );
};

function PropEventRow({ ev, t }: { ev: PropUseEvent; t: ReturnType<typeof useT> }) {
  return (
    <li className={`werewolf-prop-panel__event ${ev.hit ? 'is-hit' : ''}`}>
      <span>{ev.prop_emoji}</span>
      <span>
        #{ev.from_seat + 1} → #{ev.target_seat >= 0 ? ev.target_seat + 1 : 'ALL'}
        {' · '}
        {ev.prop_name}
      </span>
      <span className="werewolf-prop-panel__event-hit">
        {ev.hit ? t('werewolf.prop.event.hit') : t('werewolf.prop.event.miss')}
      </span>
    </li>
  );
}

export default PropPanel;