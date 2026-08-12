/**
 * modelStyle.ts — §20260811-08 U5 模型风格标识符(M3 §5 P2)
 *
 * 座位卡此前只渲染 `player.agent_name` 纯文本(10px + ellipsis),13 人局 4 行
 * 密集布局下 8 个模型名互相之间几乎无视觉区分度,观战者无法一眼看出
 * 「🐋 DeepSeek 投了 7 号,🥟 豆包 投了 9 号」。
 *
 * ── 为什么是**前端派发表**而不是后端字段 ────────────────────────────
 * 模型列表由 admin 可增删(§118 模型管理与持久化玩家)。若在后端加
 * `BrandStyleKey` 字段,每加一个模型都要改 DB + API + 前端三处;而
 * 「子串匹配 + 🤖 兜底」让新模型零改动即可用。
 * 这与 §137 教训 (1)「条件判断漏枚举是 P0 高发区」的规避思路一致 ——
 * **有兜底分支的派发表**比**必须穷举的枚举**更抗腐化。
 *
 * ── §26 对比度约束 ──────────────────────────────────────────────
 * 座位卡在 `.is-night` 下有 `brightness(0.4)` 滤镜。若只改文字颜色,夜晚
 * 会不可辨(§26.2 反模式 2「浅色文字 + 低透明度背景」)。故本模块:
 *   - 用 **emoji 前缀**(字形不受 brightness 衰减)承担主要区分度;
 *   - 用 `box-shadow` 细光晕而非低透明度背景色承担色相(§26.2 反模式 4
 *     明确要求选中/强调态必须加光晕,不被 night brightness 衰减);
 *   - 文字仍用既有 `.werewolf-seat__model` 的高对比前景色,不覆盖。
 */

/** 单个模型的视觉身份。 */
export interface ModelStyle {
  /** 固定 emoji —— 主要区分度来源(不受 night brightness 影响)。 */
  emoji: string;
  /** 光晕色相(rgba 字符串,直接进 box-shadow)。 */
  glow: string;
  /** 流派标签的 i18n key 后缀,对应 `werewolf.modelstyle.<key>`。 */
  schoolKey: string;
}

/**
 * 派发规则:按 `agent_name` **小写子串**匹配,自上而下取首个命中。
 * 顺序即优先级 —— 新增条目时注意不要被更宽泛的关键词提前截胡。
 */
const RULES: Array<{ match: string[]; style: ModelStyle }> = [
  { match: ['deepseek'], style: { emoji: '🐋', glow: 'rgba(64, 148, 255, 0.55)', schoolKey: 'logic' } },
  { match: ['doubao', '豆包'], style: { emoji: '🥟', glow: 'rgba(255, 148, 64, 0.55)', schoolKey: 'emotion' } },
  { match: ['glm', '智谱'], style: { emoji: '🧠', glow: 'rgba(178, 112, 255, 0.55)', schoolKey: 'textbook' } },
  { match: ['kimi'], style: { emoji: '🌙', glow: 'rgba(120, 128, 255, 0.55)', schoolKey: 'steady' } },
  { match: ['minimax', 'mini max'], style: { emoji: '⚡', glow: 'rgba(255, 214, 64, 0.55)', schoolKey: 'aggressive' } },
  { match: ['qwen', '通义'], style: { emoji: '🦉', glow: 'rgba(64, 214, 200, 0.55)', schoolKey: 'steady' } },
  { match: ['longcat', '美团'], style: { emoji: '🐱', glow: 'rgba(255, 96, 112, 0.55)', schoolKey: 'drama' } },
  // 注意:'mimo' 必须排在含 'mi' 的宽泛规则之后;当前无更宽泛规则,保持末位即可。
  { match: ['mimo', 'xiaomi', '小米'], style: { emoji: '🍚', glow: 'rgba(148, 168, 200, 0.55)', schoolKey: 'calculator' } },
];

/** 未匹配任何已知模型时的兜底(新增模型零改动可用)。 */
export const FALLBACK_MODEL_STYLE: ModelStyle = {
  emoji: '🤖',
  glow: 'rgba(160, 160, 160, 0.45)',
  schoolKey: 'unknown',
};

/**
 * 按 agent_name 解析模型视觉身份。
 * agentName 为空/未匹配时返回 FALLBACK_MODEL_STYLE(永不返回 null,
 * 调用方无需判空 —— §44 教训:任何可能为 null 的字段前端都要双保险)。
 */
export function modelStyleOf(agentName?: string | null): ModelStyle {
  if (!agentName) return FALLBACK_MODEL_STYLE;
  const lower = agentName.toLowerCase();
  for (const rule of RULES) {
    if (rule.match.some((m) => lower.includes(m))) return rule.style;
  }
  return FALLBACK_MODEL_STYLE;
}
