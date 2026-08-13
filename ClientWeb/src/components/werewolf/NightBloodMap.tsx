/**
 * NightBloodMap.tsx — §20260813-02 U4 夜间血迹图(S2,M3 §3)
 *
 * spectator-only 的夜间行动空间可视化:把上帝视角快照(GodModeSnapshot)中
 * 的夜间行动渲染成 13 座位椭圆阵上的箭头/连线 ——
 *   🐺 狼刀  : 全体狼座 → 刀口目标(红色箭头)
 *   👁 查验  : 预言家 ↔ 目标(绿/红虚线,按结果染色)
 *   🧪 用药  : 女巫 → 解药目标(绿实线)/ 毒药目标(紫实线)
 *   🛡 守护  : 守卫 → 目标(盾形虚线 + 守护环)
 *   ⚔ 猎魔  : 猎魔人 → 狩猎目标(橙色箭头)
 *
 * §119 协议层隔离:只消费 GodModeSnapshot 既有/§20260813-02 新增字段,
 * 不引入 wolf_whisper / chat 数据(V2 推理路径保留另做)。
 * §135:本组件仅在 spectator 分支渲染(HistoryDrawer 双重守卫),
 * 数据本身也只下发 spectator(view.go populateGodModeLocked 单点)。
 * §44:所有数组消费前做 ?? [] 双保险。
 */

import { useMemo, useState, type ReactElement } from 'react';
import type { GodModeSnapshot, WerewolfPlayerJSON } from '@/types/werewolf';
import { useT } from '@/hooks/useT';

interface Props {
  snapshot: GodModeSnapshot;
  players: WerewolfPlayerJSON[];
}

const SVG_W = 440;
const SVG_H = 330;
const CX = SVG_W / 2;
const CY = SVG_H / 2;
const RX = 185;
const RY = 128;
const SEAT_R = 15;
const MAX_SEATS = 13;

/** 座位号 → 椭圆坐标(0 号在正上方,顺时针)。 */
function seatPos(seat: number): { x: number; y: number } {
  const angle = -Math.PI / 2 + (seat * 2 * Math.PI) / MAX_SEATS;
  return { x: CX + RX * Math.cos(angle), y: CY + RY * Math.sin(angle) };
}

/** 把 from→to 的线段两端各缩进 seatRadius,避免盖住座位圆点。 */
function shrinkLine(from: { x: number; y: number }, to: { x: number; y: number }, pad: number) {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const len = Math.max(1, Math.hypot(dx, dy));
  const ux = dx / len;
  const uy = dy / len;
  return {
    x1: from.x + ux * pad,
    y1: from.y + uy * pad,
    x2: to.x - ux * pad,
    y2: to.y - uy * pad,
  };
}

export function NightBloodMap({ snapshot, players }: Props) {
  const t = useT();
  const wolfKills = snapshot.wolf_kills ?? [];
  const seerChecks = snapshot.seer_checks ?? [];
  const witchDecisions = snapshot.witch_decisions ?? [];
  const guardEntries = snapshot.guard_protect_entries ?? [];
  const publicActions = snapshot.public_actions ?? [];
  const factions = snapshot.factions ?? {};

  // 可选夜晚集合:所有来源出现过的 day(升序)。
  const nights = useMemo(() => {
    const set = new Set<number>();
    wolfKills.forEach((k) => set.add(k.day));
    seerChecks.forEach((c) => set.add(c.day));
    witchDecisions.forEach((w) => set.add(w.day));
    guardEntries.forEach((g) => set.add(g.day));
    publicActions.filter((a) => a.kind === 'demon_hunter').forEach((a) => set.add(a.day));
    // 当前进行中的夜:狼刀已落但账本尚未归档 → 用 maxDay+1 作合成夜。
    if (snapshot.wolf_kill_target >= 0) {
      const maxDay = set.size > 0 ? Math.max(...Array.from(set)) : 0;
      if (!Array.from(set).some((d) => wolfKills.some((k) => k.day === d && k.target === snapshot.wolf_kill_target && d === maxDay))) {
        set.add(maxDay + 1);
      }
    }
    return Array.from(set).sort((a, b) => a - b);
  }, [snapshot.wolf_kill_target, wolfKills, seerChecks, witchDecisions, guardEntries, publicActions]);

  const [selectedRaw, setSelectedRaw] = useState<string>('');
  const selected = selectedRaw === '' ? (nights.length > 0 ? nights[nights.length - 1] : 0) : Number(selectedRaw);

  // 该夜的动作切片
  const kill = wolfKills.find((k) => k.day === selected)
    ?? (selected > 0 && snapshot.wolf_kill_target >= 0 && selected === Math.max(0, ...nights)
      ? { day: selected, target: snapshot.wolf_kill_target }
      : undefined);
  const checks = seerChecks.filter((c) => c.day === selected);
  const witch = witchDecisions.filter((w) => w.day === selected);
  const guards = guardEntries.filter((g) => g.day === selected);
  const demonHunts = publicActions.filter((a) => a.kind === 'demon_hunter' && a.day === selected);

  const wolfSeats = Object.entries(factions)
    .filter(([, f]) => f === 'wolf')
    .map(([s]) => Number(s));

  // 守卫座位:优先用结构条目的 seat;缺失时从 roles 反查(§134 角色 key)。
  const guardSeat = useMemo(() => {
    if (guards.length > 0 && guards[0].seat >= 0) return guards[0].seat;
    const roles = snapshot.roles ?? {};
    for (const [seat, role] of Object.entries(roles)) {
      if (role === 'guard') return Number(seat);
    }
    return -1;
  }, [guards, snapshot.roles]);

  const aliveBySeat = useMemo(() => {
    const m = new Map<number, boolean>();
    players.forEach((p) => m.set(p.seat, p.alive));
    return m;
  }, [players]);

  if (nights.length === 0) {
    return <div className="ww-bloodmap-empty">{t('werewolf.bloodmap.empty')}</div>;
  }

  const isEmpty = !kill && checks.length === 0 && witch.length === 0 && guards.length === 0 && demonHunts.length === 0;

  return (
    <div className="ww-bloodmap" data-testid="ww-night-blood-map">
      <div className="ww-bloodmap-toolbar">
        <label className="ww-bloodmap-label" htmlFor="ww-bloodmap-night">
          {t('werewolf.bloodmap.title')}
        </label>
        <select
          id="ww-bloodmap-night"
          className="ww-bloodmap-select"
          value={String(selected)}
          onChange={(e) => setSelectedRaw(e.target.value)}
          data-testid="ww-bloodmap-night-select"
        >
          {nights.map((n) => (
            <option key={n} value={String(n)}>
              {t('werewolf.bloodmap.night', { n })}
            </option>
          ))}
        </select>
      </div>

      {isEmpty ? (
        <div className="ww-bloodmap-empty">{t('werewolf.bloodmap.empty')}</div>
      ) : (
        <svg
          viewBox={`0 0 ${SVG_W} ${SVG_H}`}
          className="ww-bloodmap-svg"
          role="img"
          aria-label={t('werewolf.bloodmap.title')}
        >
          <defs>
            <marker id="ww-bm-arrow-wolf" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
              <path d="M0,0 L6,3 L0,6 Z" fill="#ff5252" />
            </marker>
            <marker id="ww-bm-arrow-demon" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
              <path d="M0,0 L6,3 L0,6 Z" fill="#ffa040" />
            </marker>
          </defs>

          {/* 🐺 狼刀:全体狼座 → 刀口 */}
          {kill && kill.target >= 0 && wolfSeats.map((ws) => {
            const l = shrinkLine(seatPos(ws), seatPos(kill.target), SEAT_R + 4);
            return (
              <line
                key={`wolf-${ws}`}
                className="ww-bloodmap-line ww-bloodmap-line--wolf"
                x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2}
                markerEnd="url(#ww-bm-arrow-wolf)"
              />
            );
          })}

          {/* 👁 查验:预言家 ↔ 目标(绿/红虚线) */}
          {checks.map((c, i) => {
            const l = shrinkLine(seatPos(c.seat), seatPos(c.target), SEAT_R + 4);
            return (
              <line
                key={`seer-${i}`}
                className={`ww-bloodmap-line ww-bloodmap-line--seer ${c.result === 'werewolf' ? 'is-wolf' : 'is-good'}`}
                x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2}
              />
            );
          })}

          {/* 🧪 用药:解药绿实线 / 毒药紫实线 */}
          {witch.flatMap((w) => {
            const out: ReactElement[] = [];
            if (w.antidote_use >= 0) {
              const l = shrinkLine(seatPos(w.seat), seatPos(w.antidote_use), SEAT_R + 4);
              out.push(<line key={`anti-${w.day}`} className="ww-bloodmap-line ww-bloodmap-line--antidote" x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} />);
            }
            if (w.poison_use >= 0) {
              const l = shrinkLine(seatPos(w.seat), seatPos(w.poison_use), SEAT_R + 4);
              out.push(<line key={`poison-${w.day}`} className="ww-bloodmap-line ww-bloodmap-line--poison" x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} />);
            }
            return out;
          })}

          {/* 🛡 守护:守卫 → 目标虚线 + 目标守护环 */}
          {guards.map((g, i) => {
            const from = g.seat >= 0 ? g.seat : guardSeat;
            if (from < 0) return null;
            const l = shrinkLine(seatPos(from), seatPos(g.target), SEAT_R + 4);
            const tp = seatPos(g.target);
            return (
              <g key={`guard-${i}`}>
                <line className="ww-bloodmap-line ww-bloodmap-line--guard" x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} />
                <circle className="ww-bloodmap-shield" cx={tp.x} cy={tp.y} r={SEAT_R + 6} />
              </g>
            );
          })}

          {/* ⚔ 猎魔人:橙色箭头 */}
          {demonHunts.map((a, i) => {
            if (a.target < 0) return null;
            const l = shrinkLine(seatPos(a.seat), seatPos(a.target), SEAT_R + 4);
            return (
              <line
                key={`demon-${i}`}
                className="ww-bloodmap-line ww-bloodmap-line--demon"
                x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2}
                markerEnd="url(#ww-bm-arrow-demon)"
              />
            );
          })}

          {/* 座位圆点(最后绘制,盖在线端上方) */}
          {Array.from({ length: MAX_SEATS }, (_, seat) => {
            const p = seatPos(seat);
            const faction = factions[seat];
            const alive = aliveBySeat.get(seat) ?? true;
            const cls = [
              'ww-bloodmap-seat',
              faction === 'wolf' ? 'is-wolf' : faction ? 'is-good' : '',
              alive ? '' : 'is-dead',
            ].join(' ').trim();
            return (
              <g key={`seat-${seat}`}>
                <circle className={cls} cx={p.x} cy={p.y} r={SEAT_R} />
                <text className="ww-bloodmap-seat-label" x={p.x} y={p.y + 4} textAnchor="middle">
                  {seat + 1}
                </text>
              </g>
            );
          })}
        </svg>
      )}

      {/* 图例 */}
      <div className="ww-bloodmap-legend">
        <span><i className="ww-bloodmap-key ww-bloodmap-key--wolf" />{t('werewolf.bloodmap.legend.wolfKill')}</span>
        <span><i className="ww-bloodmap-key ww-bloodmap-key--seer" />{t('werewolf.bloodmap.legend.seerCheck')}</span>
        <span><i className="ww-bloodmap-key ww-bloodmap-key--witch" />{t('werewolf.bloodmap.legend.witch')}</span>
        <span><i className="ww-bloodmap-key ww-bloodmap-key--guard" />{t('werewolf.bloodmap.legend.guard')}</span>
        <span><i className="ww-bloodmap-key ww-bloodmap-key--demon" />{t('werewolf.bloodmap.legend.demonHunt')}</span>
      </div>
    </div>
  );
}

export default NightBloodMap;
