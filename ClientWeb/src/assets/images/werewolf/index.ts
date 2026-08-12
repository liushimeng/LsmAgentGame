/**
 * 狼人杀 13 人标准竞技局 视觉资源索引 (历史兼容 12/7 人局)
 *
 * 当前唯一支持的 StyleKey:`dark_medieval`(欧式暗黑中世纪桌游风)。
 * 资源位于 ./dark_medieval/ 下,PIL 程序化生成(§37 教训:不依赖外网)。
 * 2026-07-10: 新增 白痴(idiot)占位立绘(紫色卡背),待美术素材到位后替换。
 */

import werewolfPng from './dark_medieval/werewolf.png';
import seerPng from './dark_medieval/seer.png';
import witchPng from './dark_medieval/witch.png';
import hunterPng from './dark_medieval/hunter.png';
import idiotPng from './dark_medieval/idiot.png';
import villagerPng from './dark_medieval/villager.png';
import antidotePng from './dark_medieval/antidote.png';
import poisonPng from './dark_medieval/poison.png';
import knifePng from './dark_medieval/knife.png';
import moonPng from './dark_medieval/moon_icon.png';
import agendaPng from './dark_medieval/agenda.png';
import bgPng from './dark_medieval/bg.png';

export type StyleKey = 'dark_medieval';

export const WEREWOLF_STYLES: readonly StyleKey[] = ['dark_medieval'] as const;

/** 单个角色立绘卡(用于玩家手上的身份面板 / 死亡公布)。*/
export const roleImageByRole = {
  werewolf: werewolfPng,
  seer:     seerPng,
  witch:    witchPng,
  hunter:   hunterPng,
  idiot:    idiotPng,
  villager: villagerPng,
  // 2026-07-11: 扩展神职角色(占位图,待美术素材到位后替换)
  guard:        villagerPng,
  // ⚠️ 2026-07-29 已退役:无引擎/工具/美术实现,前端隐藏
  // knight:       villagerPng,
  // magician:     villagerPng,
  // merchant:     villagerPng,
  // dreamer:      villagerPng,
  // crow:         villagerPng,
  // scarecrow:    villagerPng,
  // prince:       villagerPng,
  // demon_hunter: villagerPng,
  // pure_white:   villagerPng,
} as const;

export const roleImageByKey: Record<string, string> = {
  werewolf: werewolfPng,
  seer:     seerPng,
  witch:    witchPng,
  hunter:   hunterPng,
  idiot:    idiotPng,
  villager: villagerPng,
  // 2026-07-11: 扩展神职角色(占位图)
  guard:        villagerPng,
  // ⚠️ 2026-07-29 已退役:无引擎/工具/美术实现,前端隐藏
  // knight:       villagerPng,
  // magician:     villagerPng,
  // merchant:     villagerPng,
  // dreamer:      villagerPng,
  // crow:         villagerPng,
  // scarecrow:    villagerPng,
  // prince:       villagerPng,
  // demon_hunter: villagerPng,
  // pure_white:   villagerPng,
};

/** 道具与图标。*/
export const werewolfAssets = {
  antidote: antidotePng,
  poison:   poisonPng,
  knife:    knifePng,
  moon:     moonPng,
  agenda:   agendaPng,
  bg:       bgPng,
} as const;
