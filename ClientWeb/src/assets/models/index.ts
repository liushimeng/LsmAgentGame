/**
 * Blender 真实 3D 模型（.glb）访问层（19-Blender3D模型集成）。
 *
 * 与 `assets/images/virtualCity/index.ts` 完全同构：
 *   - `import.meta.glob` eager=true,import default → Vite 自动为每个 .glb 产物生成 contenthash
 *   - 缺失文件返回 ''（不抛错），调用方走降级链（程序化几何）
 *
 * 规约（02-架构设计 §2）：
 *   - URL 拼接：`./<category>/<name>.glb`，category ∈ {civic,vehicles,nature,characters,road,ocean}
 *     （ocean 为批次 26 新增类别：灯塔/货船/帆船，南·海洋带用）
 *   - 单文件 ≤ 500 KB（CI 硬约束，3d_script/__common__.py::export_glb 内不强制但 build_city_hall.py 已自检）
 *   - 调用方 `modelUrl(...)` 必传 category+name 两参；MODEL_NAMES 字面量禁止运行时拼字符串
 *
 * ⚠️ 注意：前端构建**不依赖**所有 .glb 都已生成 —— import.meta.glob 按构建时存在的文件打包容错
 *    新增/删除 .glb 只需重启 vite dev server 或重新 build。
 */

const civicImgs = import.meta.glob<string>('./civic/*.glb', { eager: true, query: '?url', import: 'default' });
const vehicleImgs = import.meta.glob<string>('./vehicles/*.glb', { eager: true, query: '?url', import: 'default' });
const natureImgs = import.meta.glob<string>('./nature/*.glb', { eager: true, query: '?url', import: 'default' });
const charImgs = import.meta.glob<string>('./characters/*.glb', { eager: true, query: '?url', import: 'default' });
const roadImgs = import.meta.glob<string>('./road/*.glb', { eager: true, query: '?url', import: 'default' });
const oceanImgs = import.meta.glob<string>('./ocean/*.glb', { eager: true, query: '?url', import: 'default' });

export type ModelCategory = 'civic' | 'vehicles' | 'nature' | 'characters' | 'road' | 'ocean';

/**
 * 模型 URL（缺失 = ''，调用方退回原程序化几何）。
 * @param category 类别（civic/vehicles/nature/characters/road/ocean）
 * @param name 文件 stem（不带 .glb 后缀；与 3d_script/build_<name>.py 一一对应）
 */
export function modelUrl(category: ModelCategory, name: string): string {
  switch (category) {
    case 'civic':
      return civicImgs[`./civic/${name}.glb`] ?? '';
    case 'vehicles':
      return vehicleImgs[`./vehicles/${name}.glb`] ?? '';
    case 'nature':
      return natureImgs[`./nature/${name}.glb`] ?? '';
    case 'characters':
      return charImgs[`./characters/${name}.glb`] ?? '';
    case 'road':
      return roadImgs[`./road/${name}.glb`] ?? '';
    case 'ocean':
      return oceanImgs[`./ocean/${name}.glb`] ?? '';
  }
}

/** 模型名字面量（与 3d_script/build_*.py 文件名一一对齐，禁止运行时拼字符串）。 */
export const MODEL_NAMES = {
  civic: ['city_hall', 'comm_tower', 'water_tower', 'fire_station', 'police_station'] as const,
  vehicles: ['sedan', 'truck', 'bus', 'taxi'] as const,
  nature: ['oak_tree', 'snow_mountain', 'pine_tree', 'cactus'] as const,
  characters: ['pedestrian_walk'] as const,
  road: ['road_props'] as const,
  ocean: ['lighthouse', 'cargo_ship', 'sailboat'] as const,
} as const;
