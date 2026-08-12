/** Texas Hold'em (德州扑克) 卡面资产索引。
 *
 *  设计：PNG 优先 + CSS 文字回退。
 *  python-generate-image-tool 生成两风格 PNG，前端用 new URL() 打包。
 *  若 PNG 缺失，组件 TexasCardView 自动降级为纯 CSS 卡面（点数+花色字符）。
 */

export type StyleKey = 'western_cowboy' | 'wilderness_escape';

export const STYLE_LABELS: Record<StyleKey, string> = {
  western_cowboy: '西部牛仔',
  wilderness_escape: '荒野逃生',
};

export const STYLE_ICONS: Record<StyleKey, string> = {
  western_cowboy: '🤠',
  wilderness_escape: '🏕️',
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
  western_cowboy: {
    cardBg: '#f3e9d2',      // 棕褐皮革
    cardBorder: '#7a5230',  // 深棕边框
    cardBack: '#5a3a22',    // 马鞍棕牌背
    textRed: '#a8311c',     // 牛仔红
    textBlack: '#2b1d12',   // 深棕黑
    boardBg: '#3a5d3a',     // 深绿赌桌
  },
  wilderness_escape: {
    cardBg: '#dadfd0',      // 帆布灰绿
    cardBorder: '#3a4a2a',  // 军绿边框
    cardBack: '#23301b',    // 橄榄绿牌背
    textRed: '#7a3a1c',     // 锈红
    textBlack: '#1a1a0e',   // 深黑
    boardBg: '#2c3a23',     // 暗橄榄绿
  },
};

/** 获取 PNG 卡面路径。若文件不存在，返回空串（组件自动回退）。 */
export function cardImg(style: StyleKey, rank: number, suit: number): string {
  const rankMap: Record<number, string> = {
    2:'2', 3:'3', 4:'4', 5:'5', 6:'6', 7:'7', 8:'8', 9:'9', 10:'10',
    11:'J', 12:'Q', 13:'K', 14:'A',
  };
  const suitMap: Record<number, string> = {
    1:'spade', 2:'heart', 3:'club', 4:'diamond',
  };
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

/** 筹码堆 PNG 路径。 */
export function chipImg(style: StyleKey): string {
  return imgPath(style, 'chips');
}

/** 庄家按钮 PNG 路径。 */
export function dealerButtonImg(style: StyleKey): string {
  return imgPath(style, 'dealer_button');
}

function imgPath(style: StyleKey, name: string): string {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    return new URL(`./${style}/${name}.png`, import.meta.url).href;
  } catch {
    return '';
  }
}
