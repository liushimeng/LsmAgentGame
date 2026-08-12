/** Doudizhu (斗地主) 卡面资产索引。
 *
 *  设计：PNG 优先 + CSS 文字回退。
 *  python-generate-image-tool 生成两风格 PNG，前端用 new URL() 打包。
 *  若 PNG 缺失，组件 CardView 自动降级为纯 CSS 卡面（点数+花色字符）。
 */

export type StyleKey = 'traditional_landlord' | 'urban_worker';

export const STYLE_LABELS: Record<StyleKey, string> = {
  traditional_landlord: '传统地主',
  urban_worker: '都市打工仔',
};

export const STYLE_ICONS: Record<StyleKey, string> = {
  traditional_landlord: '🀄',
  urban_worker: '👷',
};

/** 风格配色（用于 CSS 回退卡面）。 */
export const STYLE_COLORS: Record<StyleKey, {
  cardBg: string;
  cardBorder: string;
  cardBack: string;
  textRed: string;
  textBlack: string;
  boardBg: string;
}> = {
  traditional_landlord: {
    cardBg: '#faf6ee',      // 米黄宣纸
    cardBorder: '#b8860b',  // 金边
    cardBack: '#8b0000',    // 深红牌背
    textRed: '#c0392b',
    textBlack: '#1a1a1a',
    boardBg: '#2d5a27',     // 深绿牌桌
  },
  urban_worker: {
    cardBg: '#f5f5f5',      // 灰白工装
    cardBorder: '#333',     // 硬朗黑边
    cardBack: '#2c3e50',    // 深蓝牌背
    textRed: '#e74c3c',
    textBlack: '#222',
    boardBg: '#34495e',     // 都市灰蓝
  },
};

/** 获取 PNG 卡面路径。若文件不存在，返回空串（组件自动回退）。 */
export function cardImg(style: StyleKey, rank: number, suit: number): string {
  const rankMap: Record<number, string> = {
    3:'3', 4:'4', 5:'5', 6:'6', 7:'7', 8:'8', 9:'9', 10:'10',
    11:'J', 12:'Q', 13:'K', 14:'A', 15:'2',
  };
  const suitMap: Record<number, string> = {
    1:'spade', 2:'heart', 3:'club', 4:'diamond',
  };
  // 王
  if (rank === 16) return imgPath(style, 'joker_small');
  if (rank === 17) return imgPath(style, 'joker_big');
  const r = rankMap[rank];
  const s = suitMap[suit];
  if (!r || !s) return '';
  return imgPath(style, `${r}_${s}`);
}

/** 牌背 PNG 路径。 */
export function cardBack(style: StyleKey): string {
  return imgPath(style, 'back');
}

/** 背景图 PNG 路径。 */
export function getBoardBg(style: StyleKey): string {
  return imgPath(style, 'board_bg');
}

function imgPath(style: StyleKey, name: string): string {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    return new URL(`./${style}/${name}.png`, import.meta.url).href;
  } catch {
    return '';
  }
}
