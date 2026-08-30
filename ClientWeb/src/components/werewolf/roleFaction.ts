/**
 * roleFaction — §20260830-01 死亡身份公开:角色 → 三档阵营配色映射。
 *
 * 消费方:WerewolfTable(死座身份徽章) / HistoryDrawer(⚰ 死亡页 chip) /
 * LastWordsStage(死者条身份徽章)。三处必须共用同一映射,避免各自维护
 * 本地表后逐渐漂移(§130 变体)。
 *
 * 与后端对齐说明:
 * - 后端 `ServerGo/game/werewolf/cards.go::FactionOf` 只分 wolf / good 两阵营
 *   (胜负判定用);前端这里多拆一档「god 神职」纯粹是**展示层配色**需求
 *   (§7.2 三档:狼红 / 神紫 / 民蓝),不影响任何逻辑。
 * - 角色白名单与后端 `FactionOf` case 列表 + RoomCreateModal SELECTABLE_ROLES
 *   同步:werewolf / seer / witch / hunter / idiot / guard / knight /
 *   demon_hunter / villager。已退役角色(magician 等)不在列表 → 返回 null,
 *   调用方回落到中性底色(不猜阵营,避免误导)。
 */

/** 三档展示阵营(与 CSS `werewolf-seat__role-badge--<tier>` 修饰类一一对应)。 */
export type WerewolfRoleTier = 'wolf' | 'god' | 'good';

const GOD_ROLES = new Set([
  'seer', 'witch', 'hunter', 'idiot', 'guard', 'knight', 'demon_hunter',
]);

/**
 * 角色 key → 三档阵营。未知角色(已退役 / 未来新增未同步)返回 null,
 * 调用方渲染中性徽章,不做猜测。
 */
export function roleTierOf(role: string | null | undefined): WerewolfRoleTier | null {
  if (!role) return null;
  const r = role.toLowerCase();
  if (r === 'werewolf' || r === 'wolf') return 'wolf';
  if (GOD_ROLES.has(r)) return 'god';
  if (r === 'villager') return 'good';
  return null;
}
