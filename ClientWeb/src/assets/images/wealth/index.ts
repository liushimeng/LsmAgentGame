/** 财商流游戏 (Wealth) 资产索引。
 *
 * 降级策略（前端架构文档 §9）：所有 PNG 由 python-generate-image-tool/
 * generate_wealth_assets.py 并行生成（skip-if-exists 可续跑），前端构建
 * **不依赖**生成成功 —— import.meta.glob 按构建时存在的文件打包容错，
 * 缺失的键返回 ''，组件运行时用职业色 / emoji / CSS 渐变兜底。
 *
 * 文件清单：
 *   banner.png                                2560×1440
 *   agents/{p01,p03,p05,p07,p08,p09,p10,p11,p15,p16}.png   512×512 透明
 *   districts/{finance,tech,industry,oldtown,commerce,residential,suburb,riverside}.png
 */

const agentImgs = import.meta.glob<string>('./agents/*.png', { eager: true, import: 'default' });
const districtImgs = import.meta.glob<string>('./districts/*.png', { eager: true, import: 'default' });
const bannerImgs = import.meta.glob<string>('./banner.png', { eager: true, import: 'default' });

/** 大厅 banner（缺失 = ''，WealthLobbyPage 回落 CSS 渐变）。 */
export const WEALTH_BANNER: string = bannerImgs['./banner.png'] ?? '';

/**
 * 职业头像 URL。id 大小写不敏感（"P01" / "p01" 皆可）；文档池随机 id
 * （如 "N9012345"）无对应 PNG 时返回 ''，AgentToken 回落职业色圆环 + emoji。
 */
export function professionAvatar(id: string): string {
  return agentImgs[`./agents/${id.toLowerCase()}.png`] ?? '';
}

/** 城区底板纹理 URL（缺失 = ''，DistrictBlock 回落 DistrictDefs 主色）。 */
export function districtTexture(id: string): string {
  return districtImgs[`./districts/${id}.png`] ?? '';
}
