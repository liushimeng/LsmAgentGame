/**
 * LandmarksLayer — 城市功能地标层（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 组合 4 个地标（确定性布点，坐标契约见 02-架构设计 §8）：
 *   - Fountain        中央公园正中
 *   - ParkGrounds     中央公园园路 + 花坛
 *   - ConstructionSite 软件园区界（塔吊工地；**批次 50 迁到 (30.5,-1.5)、场坪
 *     34×26 → 50×44 m** —— 原 (28,-1) 的围挡压在 arterial-z26 主路里 4.0 m）
 *   - ParkingLot      商业中心东北角（划线停车场 + 静态车）
 *
 * 纯渲染层，无 props；由 VirtualCityCityMap 注入一次。
 */

import { Fountain } from './props/Fountain';
import { ParkGrounds } from './props/ParkGrounds';
import { ConstructionSite } from './props/ConstructionSite';
import { ParkingLot } from './props/ParkingLot';
import { filterAllowsInstance, useCityShowStore } from './cityShowStore';
import { districtCenter } from '@/types/virtualCity';

export function LandmarksLayer() {
  const park = districtCenter('central_park');
  // 批次 85 §3.2.3：逐地标实例级筛选（tag = objectInfo 注册表 id）。
  const filter = useCityShowStore((s) => s.filter);
  const vis = (tag: string) => filterAllowsInstance(filter, tag);
  return (
    <group>
      <group visible={vis('landmark.fountain')}><Fountain x={park.x} z={park.z} /></group>
      <group visible={vis('landmark.park-grounds')}><ParkGrounds /></group>
      <group visible={vis('landmark.construction-site')}><ConstructionSite /></group>
      <group visible={vis('landmark.parking-lot')}><ParkingLot /></group>
    </group>
  );
}

export default LandmarksLayer;
