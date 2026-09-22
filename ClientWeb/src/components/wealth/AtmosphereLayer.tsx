/**
 * AtmosphereLayer — 城市氛围层（15-3D城市全面真实感深化 · 阶段 P）：
 *
 * 组合三层氛围元素：
 *   1. FarBuildingSilhouette × 4（4 方向远景建筑剪影，城市天际线）
 *   2. Sparkles × central_park 区域（落叶粒子，呼吸感）
 *   3. WaterMist 已经在 WaterLayer 内部注入（运河 + 港池两岸）
 *
 * 纯渲染层，无 props 依赖；由 WealthCityMap 注入一次。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §7.1。
 */

import { Sparkles } from '@react-three/drei';
import { FarBuildingSilhouette } from './FarBuildingSilhouette';
import { districtCenter } from '@/types/wealth';

export function AtmosphereLayer() {
  // 公园中心（central_park 城区中心）用于 Sparkles 落叶
  const park = districtCenter('central_park');
  return (
    <group>
      {/* 4 方向远景建筑剪影 */}
      <FarBuildingSilhouette side="north" count={18} />
      <FarBuildingSilhouette side="south" count={18} />
      <FarBuildingSilhouette side="east" count={14} />
      <FarBuildingSilhouette side="west" count={14} />
      {/* 中央公园落叶粒子（drei Sparkles 性能可控，count 6 scale 6） */}
      <Sparkles
        position={[park.x, 1.5, park.z]}
        count={6}
        scale={[6, 3, 6]}
        size={2}
        speed={0.2}
        color="#a3c585"
        opacity={0.6}
      />
    </group>
  );
}

export default AtmosphereLayer;