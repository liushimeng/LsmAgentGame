/**
 * LandmarksLayer — 城市功能地标层（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 组合 4 个地标（确定性布点，坐标契约见 02-架构设计 §8）：
 *   - Fountain        中央公园正中
 *   - ParkGrounds     中央公园园路 + 花坛
 *   - ConstructionSite 软件园区界（塔吊工地；批次 20 §3.4 坐标 (28,-1) 不动，注释改注）
 *   - ParkingLot      商业中心东北角（划线停车场 + 静态车）
 *
 * 纯渲染层，无 props；由 WealthCityMap 注入一次。
 */

import { Fountain } from './props/Fountain';
import { ParkGrounds } from './props/ParkGrounds';
import { ConstructionSite } from './props/ConstructionSite';
import { ParkingLot } from './props/ParkingLot';
import { districtCenter } from '@/types/wealth';

export function LandmarksLayer() {
  const park = districtCenter('central_park');
  return (
    <group>
      <Fountain x={park.x} z={park.z} />
      <ParkGrounds />
      <ConstructionSite />
      <ParkingLot />
    </group>
  );
}

export default LandmarksLayer;
