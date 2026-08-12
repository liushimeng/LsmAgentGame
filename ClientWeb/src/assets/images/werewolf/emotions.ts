/**
 * 狼人杀 Agent 表情头像素材聚合 (2026-08-04 §表情特效 — 设计文档 20260804-02)
 *
 * 素材来源: art-designer 经 chroma-key 绿幕管线生成(256×256 透明 PNG),
 * 输出到 ./dark_medieval/emotions/<key>.png。
 * 12 个 key: neutral/confident/excited/calm/panic/wary/irritated/grievance/
 * confused/guilty/tired (+dead 备用)。
 */
import neutralPng   from './dark_medieval/emotions/neutral.png';
import confidentPng from './dark_medieval/emotions/confident.png';
import excitedPng   from './dark_medieval/emotions/excited.png';
import calmPng      from './dark_medieval/emotions/calm.png';
import panicPng     from './dark_medieval/emotions/panic.png';
import waryPng      from './dark_medieval/emotions/wary.png';
import irritatedPng from './dark_medieval/emotions/irritated.png';
import grievancePng from './dark_medieval/emotions/grievance.png';
import confusedPng  from './dark_medieval/emotions/confused.png';
import guiltyPng    from './dark_medieval/emotions/guilty.png';
import tiredPng     from './dark_medieval/emotions/tired.png';
import deadPng      from './dark_medieval/emotions/dead.png';

export const emotionImageByKey: Record<string, string> = {
  neutral:   neutralPng,
  confident: confidentPng,
  excited:   excitedPng,
  calm:      calmPng,
  panic:     panicPng,
  wary:      waryPng,
  irritated: irritatedPng,
  grievance: grievancePng,
  confused:  confusedPng,
  guilty:    guiltyPng,
  tired:     tiredPng,
  dead:      deadPng,
};
