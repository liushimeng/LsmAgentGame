/**
 * ParkBenches — 中央公园长椅（批次 45 B2/C3：全园 10 把，实例化）。
 *
 * GB 51192-2016 §3.5 条文说明：座椅应分布在游人集中活动场所，「沿园路布置时
 * 考虑到老年人行走易疲劳，建议间隔 50~100 m」—— 园路 2×74 m ⇒ 四臂各 2 把
 * （两侧交替、面向园路）+ 游乐/健身场旁各 1 把看护座 = 10 把。
 *
 * 渲染：`GlbInstanced`（`civic/park_bench.glb`，材质槽 2 ⇒ 2 draw call 全园）；
 * GLB 缺失/加载中/`disable-blender-models=1` → 10 把程序化板条椅合并单 mesh
 * （mergeParts 顶点色，5 座板 + 3 背板 + 弓形脚，观感与 GLB 同构）。
 *
 * 点位单一事实来源：`parkLayout.ts::PARK_BENCHES`（世界坐标 + 朝向）。
 */
import { useEffect, useMemo } from 'react';
import { blenderModelsEnabled, boxPart, mergeParts, type MergePart } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { u, sizeTargetFor } from '../cityScale';
import { PARK_BENCHES } from './parkLayout';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';
import { GlbInstanced, type GlbInstanceTRS } from '../edge/glbInstanced';

const WOOD = '#7a5a3a';
const LEG = '#494e57';

/** 程序化兜底：单把板条椅部件（局部原点 = 椅中心；座面向 +Z）。 */
function benchParts(x: number, z: number, rot: number): MergePart[] {
  const cos = Math.cos(rot);
  const sin = Math.sin(rot);
  /** 局部 (lx,lz) → 世界 XZ（绕 Y 旋转 rot 后平移到 (x,z)）。 */
  const p = (lx: number, lz: number): [number, number] => [x + lx * cos + lz * sin, z - lx * sin + lz * cos];
  const parts: MergePart[] = [];
  // 座板 ×5（座高 0.43 / 深 0.41：y ∈ [-0.16, +0.16]）
  for (let i = 0; i < 5; i++) {
    const ly = -0.16 + i * 0.085;
    const [wx, wz] = p(0, u(ly));
    parts.push(boxPart(u(1.8), u(0.04), u(0.07), wx, u(0.41), wz, WOOD));
  }
  // 背板 ×3（后倾：y 越大越靠 -Z）
  for (const [ly, h] of [[-0.20, 0.52], [-0.215, 0.68], [-0.23, 0.84]] as const) {
    const [wx, wz] = p(0, u(ly));
    parts.push(boxPart(u(1.8), u(0.09), u(0.035), wx, u(h), wz, WOOD));
  }
  // 弓形脚 ×2（脚段 + 前后撑）
  for (const s of [-0.7, 0.7]) {
    const [fx, fz] = p(u(s), 0);
    parts.push(boxPart(u(0.05), u(0.06), u(0.46), fx, u(0.03), fz, LEG));
    const [sx, sz] = p(u(s), u(0.155));
    parts.push(boxPart(u(0.05), u(0.42), u(0.05), sx, u(0.215), sz, LEG));
    const [bx, bz] = p(u(s), u(-0.155));
    parts.push(boxPart(u(0.05), u(0.56), u(0.05), bx, u(0.28), bz, LEG));
  }
  return parts;
}

export function ParkBenches() {
  const info = useObjectInfoProps('park.bench', { anchorY: 0.9 });
  const url = blenderModelsEnabled() ? modelUrl('civic', 'park_bench') : '';
  const sizeTarget = useMemo(
    () => sizeTargetFor('parkBench', { label: 'civic/park_bench' }),
    [],
  );

  const instances = useMemo<GlbInstanceTRS[]>(
    () => PARK_BENCHES.map((s) => ({ position: [s.x, 0, s.z], rotationY: s.rot })),
    [],
  );

  // 程序化兜底：10 把一次合并单 mesh（GLB 成功时不渲染）
  const fbGeo = useMemo(
    () => mergeParts(PARK_BENCHES.flatMap((s) => benchParts(s.x, s.z, s.rot))),
    [],
  );
  useEffect(() => () => fbGeo.dispose(), [fbGeo]);

  return (
    <group {...info}>
      <GlbInstanced
        url={url}
        instances={instances}
        sizeTarget={sizeTarget}
        fallback={
          <mesh geometry={fbGeo}>
            <meshStandardMaterial vertexColors roughness={0.75} metalness={0.1} />
          </mesh>
        }
      />
    </group>
  );
}

export default ParkBenches;
