// RulesRegistry — maps a game_kind slug to the URL of its markdown rule file
// and the per-game accent color used by the RulesViewer modal header.
//
// Why this exists:
//   - 5 games each have a polished markdown file under ClientWeb/public/rules/
//     (served by Vite at /rules/<kind>.md in dev and bundled in prod).
//   - The RulesViewer needs both the URL and a small visual accent to feel
//     native to the game page; centralizing avoids scattering kind → url
//     mappings across components.
//   - Adding a new game is one entry here, one md file in public/rules/, and
//     one GamePage button — see docs/游戏规则与查看器.md.

export type GameKind = 'xiangqi' | 'chess' | 'junqi' | 'doudizhu' | 'texasholdem' | 'werewolf';

/** Per-game accent color used in the RulesViewer header / border. */
export const GAME_ACCENT: Record<GameKind, string> = {
  xiangqi: '#c98a3a',      // 象棋 — 暖金（呼应棋盘木色）
  chess: '#3a8aff',         // 国际象棋 — 蓝（Cyber/European 都偏冷蓝）
  junqi: '#7a4e2a',         // 军棋 — 棕褐（军旅 / 棋盘木质）
  doudizhu: '#c93636',      // 斗地主 — 红（地主身份 / 主色调）
  texasholdem: '#1f7a4d',   // 德州 — 绿（牌桌绒布）
  werewolf: '#7b2c2c',      // 狼人杀 — 暗血红（暗黑中世纪 / 哥特）
};

/** URL of the markdown file for the given game. */
export function rulesUrl(kind: GameKind): string {
  return `/rules/${kind}.md`;
}
