/**
 * DeadPlayerBanner — §20260830-02 死亡玩家常驻状态横幅。
 *
 * 设计文档:docs/狼人杀-角色设计/狼人杀自爆遗言与带走设计-20260830-02.md §7-3③。
 *
 * 需求背景:人类玩家死亡后,界面此前没有任何常驻的「我已出局 + 当前还能
 * 做什么」指引 —— 遗言面板只在轮到自己时出现,猎枪/带走面板只对角色命中者
 * 出现,中间空窗期玩家不知道自己是否还有操作。
 *
 * 渲染条件:!spectator && 我已死亡 && 对局未结束(由 WerewolfGamePage 控制)。
 * 内容按当前阶段给出最小指引:
 *   - death_lyric:轮到你时下方面板会出现遗言输入框(等待/操作中)。
 *   - hunter_shoot:死亡猎人可开枪带走一名玩家。
 *   - suicide_take:自爆狼可选择带走一名玩家。
 *   - 其它阶段:已进入幽灵观战模式,可继续观看对局。
 *
 * 纯展示组件,无副作用;不渲染任何身份信息(公平性)。
 */

import { useT } from '@/hooks/useT';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  gameState: WerewolfGameState;
  /** 我的座位(0-indexed;-1 = 观战)。 */
  mySeat: number;
}

export function DeadPlayerBanner({ gameState, mySeat }: Props) {
  const t = useT();
  const phase = gameState.phase;
  const me = gameState.players?.[mySeat];
  if (mySeat < 0 || !me || me.alive) return null;

  // 按阶段给出「还能做什么」指引;默认 = 幽灵观战。
  let hint = t('werewolf.dead.banner.ghostHint');
  if (phase === 'death_lyric') {
    const currentSeat = gameState.phase_extra?.death_lyric?.current_seat ?? -1;
    hint =
      currentSeat === mySeat
        ? t('werewolf.dead.banner.lastWordsActing')
        : t('werewolf.dead.banner.lastWordsWaiting');
  } else if (phase === 'hunter_shoot') {
    hint = t('werewolf.dead.banner.hunterHint');
  } else if (phase === 'suicide_take') {
    hint = t('werewolf.dead.banner.suicideTakeHint');
  }

  return (
    <div className="dead-player-banner" role="status" data-testid="dead-player-banner">
      <span className="dead-player-banner__icon">☠️</span>
      <span className="dead-player-banner__title">{t('werewolf.dead.banner.title')}</span>
      <span className="dead-player-banner__hint">{hint}</span>
    </div>
  );
}
