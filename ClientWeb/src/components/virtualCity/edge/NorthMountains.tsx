/**
 * 批次 26 · 北缘雪山带（z ∈ [−80,−62]，x ∈ [−90,+90]）。
 *
 * 内容（方案文档 §2.1 坐标带契约）：
 *   - 雪山脊线：snow_mountain.glb ×7，z ≈ −70~−76 错落，scale 6~10
 *     （模型基准高 ~25m → 150~250m 视觉山脊），经 GlbInstanced 实例化
 *     （draw call = GLB 子网格数，与座数无关）；fallback = 程序化锥体山。
 *   - 山脚针叶植被带：pine_tree.glb instanced ×60，z ∈ [−70,−62]；
 *     fallback = ProceduralPines。
 *
 * 布点全部确定性（hashStr + mulberry32，与 civic/rand.ts 同源），禁 Math.random。
 * 罗盘契约：北 = −Z（方案文档 §1.1）。
 *
 * 契约：lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md §2.4。
 */

import { useMemo } from 'react';
import { modelUrl } from '@/assets/models';
import { u } from '../cityScale';
import { hashStr, mulberry32 } from '../civic/rand';
import { GlbInstanced, type GlbInstanceTRS } from './glbInstanced';
import { ProceduralPines, type FloraSpot } from './proceduralFlora';

/** 雪山座数（方案 §2.1：×6~8）。 */
const MOUNTAIN_COUNT = 7;
/** 山脚针叶树棵数（方案 §2.1：×~60）。 */
const PINE_COUNT = 60;

export function NorthMountains() {
  // 雪山脊线：x ∈ [−90,90] 均分 + 抖动，z ∈ [−76,−70]，scale 6~10（≈150~250m 高）
  const mountains = useMemo<GlbInstanceTRS[]>(() => {
    const rnd = mulberry32(hashStr('edge26:north:mountains'));
    const out: GlbInstanceTRS[] = [];
    for (let i = 0; i < MOUNTAIN_COUNT; i++) {
      const x = -90 + (i * 180) / (MOUNTAIN_COUNT - 1) + (rnd() - 0.5) * 12;
      const z = -73 + (rnd() - 0.5) * 6; // −76 ~ −70
      out.push({
        position: [x, 0, z],
        rotationY: rnd() * Math.PI * 2,
        scale: 6 + rnd() * 4,
      });
    }
    return out;
  }, []);

  // 山脚针叶植被带：z ∈ [−70,−62]，x ∈ [−88,88]，scale 0.8~1.4
  const pines = useMemo<FloraSpot[]>(() => {
    const rnd = mulberry32(hashStr('edge26:north:pines'));
    const out: FloraSpot[] = [];
    for (let i = 0; i < PINE_COUNT; i++) {
      out.push({
        x: -88 + rnd() * 176,
        z: -70 + rnd() * 8,
        scale: 0.8 + rnd() * 0.6,
        rotY: rnd() * Math.PI * 2,
      });
    }
    return out;
  }, []);

  const pineInstances = useMemo<GlbInstanceTRS[]>(
    () =>
      pines.map((t) => ({
        position: [t.x, 0, t.z],
        rotationY: t.rotY,
        scale: t.scale,
      })),
    [pines],
  );

  return (
    <group>
      {/* 雪山脊线（GLB 实例化；fallback 程序化锥体山） */}
      <GlbInstanced
        url={modelUrl('nature', 'snow_mountain')}
        instances={mountains}
        fallback={<ProceduralMountains mountains={mountains} />}
      />
      {/* 山脚针叶树（GLB 实例化；fallback 程序化圆锥树） */}
      <GlbInstanced
        url={modelUrl('nature', 'pine_tree')}
        instances={pineInstances}
        fallback={<ProceduralPines spots={pines} />}
      />
    </group>
  );
}

/** 雪山程序化 fallback：灰岩锥体 + 雪顶小锥 + 山腰绿带环（三段，对应 GLB 三段材质）。 */
function ProceduralMountains({ mountains }: { mountains: GlbInstanceTRS[] }) {
  return (
    <group>
      {mountains.map((m, i) => {
        const s = m.scale ?? 1;
        // 基准高 u(25) × s（与 GLB 契约 25m 基准一致）
        const h = u(25) * s;
        const r = u(18) * s;
        return (
          <group key={`mt-${i}`} position={m.position} rotation={[0, m.rotationY ?? 0, 0]}>
            {/* 灰岩山体 */}
            <mesh position={[0, h * 0.35, 0]}>
              <coneGeometry args={[r, h * 0.7, 7]} />
              <meshStandardMaterial color="#6f757d" roughness={0.95} flatShading />
            </mesh>
            {/* 山腰绿带 */}
            <mesh position={[0, h * 0.16, 0]}>
              <coneGeometry args={[r * 0.98, h * 0.32, 7]} />
              <meshStandardMaterial color="#3d5a34" roughness={0.95} flatShading />
            </mesh>
            {/* 雪顶 */}
            <mesh position={[0, h * 0.82, 0]}>
              <coneGeometry args={[r * 0.42, h * 0.36, 7]} />
              <meshStandardMaterial color="#eef2f5" roughness={0.7} flatShading />
            </mesh>
          </group>
        );
      })}
    </group>
  );
}

export default NorthMountains;
