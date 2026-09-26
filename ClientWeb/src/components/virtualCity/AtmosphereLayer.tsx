/**
 * AtmosphereLayer — 城市氛围层（15-3D城市全面真实感深化 · 阶段 P）：
 *
 * 组合氛围元素：
 *   1. Sparkles × central_park 区域（落叶粒子，呼吸感）
 *   2. WaterMist 已经在 WaterLayer 内部注入（运河 + 港池两岸）
 *
 * 批次 26「城市坐标系统与四缘环境带」：原 4 方向 FarBuildingSilhouette 灰盒
 * 远景剪影全部移除——四缘由真实环境带（北雪山 / 西沙漠 / 东森林 / 南海洋，
 * edge/CityEdgeLayer）取代；FarBuildingSilhouette.tsx 文件已删除。
 *
 * 纯渲染层，无 props 依赖；由 VirtualCityCityMap 注入一次。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §7.1 +
 *       lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md §2.4。
 */

import { Sparkles } from '@react-three/drei';
import { districtCenter } from '@/types/virtualCity';

export function AtmosphereLayer() {
  // 公园中心（central_park 城区中心）用于 Sparkles 落叶
  const park = districtCenter('central_park');
  return (
    <group>
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
