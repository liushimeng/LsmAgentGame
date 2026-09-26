/**
 * 批次 26 · 西缘沙漠带（x ∈ [−80,−62]，z ∈ [−62,+58]）。
 *
 * 内容（方案文档 §2.1 坐标带契约）：
 *   - 沙丘 ×5：单位 sphere 压扁（drei Instances 1 draw call），沙色；
 *   - 仙人掌 ×24：cactus.glb 经 GlbInstanced 实例化；fallback = ProceduralCacti；
 *   - 枯木 ×3：单位 cylinder 倾斜实例（trunk+branch 共 6 段，1 draw call）。
 *
 * 沙地地表 patch 由 CityEdgeLayer 统一铺设（sand_tile 贴图，缺失降级纯色）。
 * 布点确定性（hashStr + mulberry32）。罗盘契约：西 = −X（方案文档 §1.1）。
 *
 * 契约：lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md §2.4。
 */

import { useMemo } from 'react';
import { Instances, Instance } from '@react-three/drei';
import { modelUrl } from '@/assets/models';
import { u } from '../cityScale';
import { hashStr, mulberry32 } from '../civic/rand';
import { GlbInstanced, type GlbInstanceTRS } from './glbInstanced';
import { ProceduralCacti, type FloraSpot } from './proceduralFlora';

const SAND_COLOR = '#d9b36c';
const DEADWOOD_COLOR = '#6b5a45';

/** 沙丘数（方案 §2.1：×5）。 */
const DUNE_COUNT = 5;
/** 仙人掌数（方案 §2.1：×~24）。 */
const CACTUS_COUNT = 24;

interface Dune {
  x: number;
  z: number;
  r: number;
  sy: number;
  rotY: number;
}

export function WestDesert() {
  // 沙丘：x ∈ [−78,−64]，z ∈ [−58,+54]，半径 3~5 单位压扁
  const dunes = useMemo<Dune[]>(() => {
    const rnd = mulberry32(hashStr('edge26:west:dunes'));
    const out: Dune[] = [];
    for (let i = 0; i < DUNE_COUNT; i++) {
      out.push({
        x: -78 + rnd() * 14,
        z: -58 + rnd() * 112,
        r: 3 + rnd() * 2,
        sy: 0.18 + rnd() * 0.12,
        rotY: rnd() * Math.PI * 2,
      });
    }
    return out;
  }, []);

  // 仙人掌：全带散布，与沙丘心 |dx|,|dz| 同小于 2.5 时向东外推避让
  const cacti = useMemo<FloraSpot[]>(() => {
    const rnd = mulberry32(hashStr('edge26:west:cacti'));
    const out: FloraSpot[] = [];
    for (let i = 0; i < CACTUS_COUNT; i++) {
      let x = -79 + rnd() * 16;
      let z = -61 + rnd() * 118;
      for (const d of dunes) {
        if (Math.abs(x - d.x) < 2.5 && Math.abs(z - d.z) < 2.5) {
          x = Math.max(-79.5, Math.min(-62.5, x + 3));
          z += 3;
        }
      }
      out.push({ x, z, scale: 0.8 + rnd() * 0.7, rotY: rnd() * Math.PI * 2 });
    }
    return out;
  }, [dunes]);

  const cactusInstances = useMemo<GlbInstanceTRS[]>(
    () => cacti.map((t) => ({ position: [t.x, 0, t.z], rotationY: t.rotY, scale: t.scale })),
    [cacti],
  );

  // 枯木 ×3：主干倾斜 + 1 枝，合计 6 段单位 cylinder → 1 draw call
  const deadwood = useMemo(() => {
    const rnd = mulberry32(hashStr('edge26:west:deadwood'));
    const out: Array<{
      x: number; z: number; tilt: number; rotY: number; len: number;
      branchRotY: number; branchTilt: number; branchLen: number;
    }> = [];
    for (let i = 0; i < 3; i++) {
      out.push({
        x: -78 + rnd() * 14,
        z: -56 + rnd() * 108,
        tilt: 0.25 + rnd() * 0.35,
        rotY: rnd() * Math.PI * 2,
        len: u(2.4) + rnd() * u(1.2),
        branchRotY: rnd() * Math.PI * 2,
        branchTilt: 0.7 + rnd() * 0.5,
        branchLen: u(1.2) + rnd() * u(0.8),
      });
    }
    return out;
  }, []);

  return (
    <group>
      {/* 沙丘 ×5（单位 sphere 压扁 → 1 draw call） */}
      <Instances limit={DUNE_COUNT} range={DUNE_COUNT}>
        <sphereGeometry args={[1, 16, 10]} />
        <meshStandardMaterial color={SAND_COLOR} roughness={0.95} />
        {dunes.map((d, i) => (
          <Instance
            key={`dune-${i}`}
            position={[d.x, 0, d.z]}
            rotation={[0, d.rotY, 0]}
            scale={[d.r, d.r * d.sy, d.r * 0.7]}
          />
        ))}
      </Instances>
      {/* 仙人掌 ×24（GLB 实例化；fallback 程序化主干+双臂） */}
      <GlbInstanced
        url={modelUrl('nature', 'cactus')}
        instances={cactusInstances}
        fallback={<ProceduralCacti spots={cacti} />}
      />
      {/* 枯木 ×3（主干 + 枝，全部单位 cylinder 实例 → 1 draw call） */}
      <Instances limit={deadwood.length * 2} range={deadwood.length * 2}>
        <cylinderGeometry args={[u(0.05), u(0.09), 1, 6]} />
        <meshStandardMaterial color={DEADWOOD_COLOR} roughness={0.95} />
        {deadwood.flatMap((w, i) => {
          // 主干：绕本地倾斜轴 tilt，整体再 rotY
          const trunkY = (w.len / 2) * Math.cos(w.tilt);
          // 枝：近似挂点在主干 0.7 高度处，沿独立方向伸出
          const branchY = trunkY * 1.4 + (w.branchLen / 2) * Math.cos(w.branchTilt);
          return [
            <Instance
              key={`dw-t-${i}`}
              position={[w.x, trunkY, w.z]}
              rotation={[w.tilt * Math.sin(w.rotY), w.rotY, w.tilt * Math.cos(w.rotY)]}
              scale={[1, w.len, 1]}
            />,
            <Instance
              key={`dw-b-${i}`}
              position={[w.x, branchY, w.z]}
              rotation={[
                w.branchTilt * Math.sin(w.branchRotY),
                w.branchRotY,
                w.branchTilt * Math.cos(w.branchRotY),
              ]}
              scale={[0.6, w.branchLen, 0.6]}
            />,
          ];
        })}
      </Instances>
    </group>
  );
}

export default WestDesert;
