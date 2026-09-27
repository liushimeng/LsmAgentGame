/**
 * seasonAssets — 季节 → 贴图/模型资产适配层（批次 27 §6.1/§6.2 消费侧）。
 *
 * 资产由 art-designer 并行登记（python-generate-image-tool
 * generate_virtual_city_season_assets.py 季节贴图 + 3d_script/build_oak_tree_season.py
 * 三季 GLB），前端构建**不依赖**生成成功：
 *   - 贴图：groundTileUrl 对缺失文件返回 '' → 回退夏季既有 grass_tile（视觉零回归）；
 *   - GLB：modelUrl('nature', 'oak_tree_<season>') 缺失 → 回退 oak_tree →
 *     再缺失走 GlbInstanced 程序化 fallback（§27.3 降级链不变）。
 */

import { groundTileUrl } from '@/assets/images/virtualCity';
import type { GroundTileName } from '@/assets/images/virtualCity';
import { modelUrl } from '@/assets/models';
import type { CitySeason } from './cityTimeStore';

/** 季节 → 草皮贴图键（批次 27 §6.1：夏 = 既有 grass_tile，三季新增键已登记）。 */
const SEASON_GRASS_TILE: Record<CitySeason, GroundTileName> = {
  spring: 'grass_spring_tile',
  summer: 'grass_tile',
  autumn: 'grass_autumn_tile',
  winter: 'snow_cover_tile',
};

/**
 * 季节草皮贴图 URL（DistrictBlock 中央公园草地覆盖层消费）。
 * 季节贴图缺失（art 未生成/构建未打包）→ 回退 grass_tile。
 */
export function seasonGrassTileUrl(season: CitySeason): string {
  return groundTileUrl(SEASON_GRASS_TILE[season]) || groundTileUrl('grass_tile');
}

/** 季节阔叶树 GLB URL（EastForest 消费；oak_tree_<season> 缺失回退 oak_tree）。 */
export function seasonOakModelUrl(season: CitySeason): string {
  if (season === 'summer') return modelUrl('nature', 'oak_tree');
  return modelUrl('nature', `oak_tree_${season}`) || modelUrl('nature', 'oak_tree');
}
