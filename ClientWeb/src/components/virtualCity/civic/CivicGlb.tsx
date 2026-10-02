/**
 * CivicGlb — 「程序化 fallback → Blender GLB」通用接线件（批次 46 城市公用设施）。
 *
 * 范式来源：批次 45 `civic/ParkExtras.tsx::ParkMajorPiece`。本件把该范式抽成
 * 可复用组件，让 3d_script 产出的市政 GLB 与原程序化几何共用同一段接线：
 *
 *   1. **降级链**（CLAUDE.md §27.3 硬约束 3）：`blenderModelsEnabled()` →
 *      `modelUrl(...)` → GLB 加载中/失败 ⇒ 逐级退回 `fallback` 子节点。
 *      **不**用 drei 的 `<Model>`：尺寸守卫要挂在 `useSharedGLTF` 侧，
 *      且单实例 clone 比 N 次 mount 便宜。
 *   2. **尺寸守卫**：`sizeTargetFor(dimsKey)` 把 `cityScale.REAL_DIMS_M`（米）
 *      换算成世界单位目标，dev 态载入即量 Box3 比对，偏差 >±5% 告警
 *      （`engine3d/glbSizeGuard.ts`）。**只告警不改几何**。
 *   3. **昼夜材质调制**：GLB 的发光件靠**材质名**匹配（`.includes()`），
 *      昼 0.05 → 夜 `night` 指数趋近（`k = min(1, delta*2)`，同
 *      `ParkExtras.tsx::PavilionEaveLamp` 的做法）。
 *   4. `infoId` + `anchorY` 挂可点击登记点 —— 跟着**组件**走，不跟着 GLB 走，
 *      这样 GLB 缺失时 objectInfo 行为不变。
 *
 * 节点契约（§27.3-6）：消费端零旋转零 scale 直挂，尺寸只在顶点里。
 */
import { useEffect, useMemo, type ReactNode } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';
import { blenderModelsEnabled, useSharedGLTF } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { sizeTargetFor, type RealDimKey } from '../cityScale';
import { getDayNight } from '../cityTimeStore';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/** 白天（dayFactor01 = 1）的 emissiveIntensity 底值：留一点，避免昼夜交界处硬切。 */
const EMISSIVE_DAY = 0.05;

export interface CivicGlbPieceProps {
  /** `3d_script/build_<glbName>.py` 的 stem（与 `assets/models/civic/<glbName>.glb` 对齐）。 */
  glbName: string;
  /** `cityScale.REAL_DIMS_M` 的表键（尺寸唯一事实来源）。 */
  dimsKey: RealDimKey;
  /** `objectInfo` 登记 id（`catalog-scene.ts` 内的条目）。 */
  infoId: string;
  /** 可点击锚点的世界 y（米 → 世界单位由调用方给，本字段已是世界单位）。 */
  anchorY: number;
  /** 组的世界位置（世界单位）。 */
  position: [number, number, number];
  /** 组的欧拉角（世界单位语义下的弧度）。 */
  rotation?: [number, number, number];
  /** 材质名子串 → 夜间 emissiveIntensity（如 `{ 'SIGN_Panel': 2.4 }`）。 */
  litMaterials?: Record<string, number>;
  /** GLB 不可用时的程序化几何（与 GLB 同尺寸，§27.3 硬约束 7）。 */
  fallback: ReactNode;
}

/**
 * 按材质名收集可调制的 GLB 材质（`.includes()` 匹配，容忍 Blender 导出的
 * `_Mat` 后缀）。clone 与原 scene **共享材质 datablock**，故同一 GLB 的多个
 * 实例会共享同一组材质 —— 单实例物件无影响。
 */
function collectLitMaterials(
  root: THREE.Object3D | null,
  table: Record<string, number>,
): Array<[THREE.MeshStandardMaterial, number]> {
  if (!root) return [];
  const names = Object.keys(table);
  if (names.length === 0) return [];
  const out: Array<[THREE.MeshStandardMaterial, number]> = [];
  const seen = new Set<THREE.Material>();
  root.traverse((o) => {
    const mesh = o as THREE.Mesh;
    if (!mesh.isMesh) return;
    for (const m of Array.isArray(mesh.material) ? mesh.material : [mesh.material]) {
      const std = m as THREE.MeshStandardMaterial;
      if (!std.isMeshStandardMaterial || seen.has(m)) continue;
      const key = names.find((n) => m.name.includes(n));
      if (key !== undefined) {
        seen.add(m);
        out.push([std, table[key]]);
      }
    }
  });
  return out;
}

/** 单件「GLB 优先 + 程序化 fallback」的市政大件。 */
export function CivicGlbPiece({
  glbName,
  dimsKey,
  infoId,
  anchorY,
  position,
  rotation,
  litMaterials,
  fallback,
}: CivicGlbPieceProps) {
  const info = useObjectInfoProps(infoId, { anchorY });
  const url = blenderModelsEnabled() ? modelUrl('civic', glbName) : '';
  const sizeTarget = useMemo(
    () => sizeTargetFor(dimsKey, { label: `civic/${glbName}` }),
    [dimsKey, glbName],
  );
  const { scene } = useSharedGLTF(url, sizeTarget);
  const cloned = useMemo(() => (scene ? scene.clone(true) : null), [scene]);

  // 昼夜调制：(材质, 夜间值) 对只在 scene / 配置变化时重算，逐帧只做指数趋近。
  const litMats = useMemo(
    () => collectLitMaterials(cloned, litMaterials ?? {}),
    [cloned, litMaterials],
  );
  // 防 §130「声明了却从不接线」：litMaterials 的键靠**材质名**匹配，键与
  // `build_<glbName>.py` 里的 `make_material('...')` 漂移时调制会静默失效
  // （批次 46 首版即踩：节点名 `GasStation_PUMP_Screen` vs 材质名
  // `..._PUMPScreen_Mat`，只有 CDP 数值审计查得出来）。dev 态直接告警。
  useEffect(() => {
    if (!import.meta.env.DEV || !cloned || !litMaterials) return;
    const hit = new Set(litMats.map(([m]) => Object.keys(litMaterials).find((n) => m.name.includes(n))!));
    const missing = Object.keys(litMaterials).filter((n) => !hit.has(n));
    if (missing.length > 0) {
      console.warn(
        `[CivicGlb] ${glbName}：litMaterials 键未命中任何 GLB 材质（§130）—— ${missing.join(', ')}`,
      );
    }
  }, [cloned, litMats, litMaterials, glbName]);

  useFrame((_s, delta) => {
    if (litMats.length === 0) return;
    const night = 1 - (getDayNight()?.dayFactor01 ?? 1);
    const k = Math.min(1, delta * 2);
    for (const [m, nightValue] of litMats) {
      m.emissiveIntensity += (EMISSIVE_DAY + (nightValue - EMISSIVE_DAY) * night - m.emissiveIntensity) * k;
    }
  });

  return (
    <group {...info} position={position} rotation={rotation}>
      {cloned ? <primitive object={cloned} /> : fallback}
    </group>
  );
}
