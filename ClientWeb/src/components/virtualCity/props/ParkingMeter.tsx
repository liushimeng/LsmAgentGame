/**
 * ParkingMeter — 停车咪表（批次 42 街具真实感 · B1/C2/D1）：
 *
 * 双渲染路径（CLAUDE.md §27.3 硬约束 3/7，包围盒同取 cityScale.REAL_DIMS_M.parkingMeter
 * = 0.32 × 1.38 × 0.28 m，直立件；屏幕朝局部 +Z，布点层保证 +Z 指向路心）：
 *   - GLB 优先：`road/parking_meter.glb`（节点 ParkingMeter）经 useSharedGLTF +
 *     scene.clone(true)（批次 41 V1：成功时 fallback **整体不渲染**，绝不双渲染）；
 *   - 程序化 fallback：前倾表头 + 深色屏幕（emissive，材质名含 `Screen`）+ 投币槽
 *     + 读卡圆 + 太阳能顶板 + 圆盘底座。
 *
 * C2 昼夜调制（复用 Vehicle.tsx 材质名命中模式）：traverse 收集材质名含 `Screen`
 * 的共享材质并烘焙 emissiveIntensity 基值，useFrame 按 getDayNight()?.dayFactor01
 * 调制 `base × (0.4 + 1.4·nightK)`（夜间屏幕微亮、白天不抢戏）；fallback 屏幕同名同口径。
 *
 * 布点（批次 42 C1）：`StreetPropsLayer::parkingMetersForNetwork` 沿路缘成排
 * （间距 REAL_SPACING_M.parkingMeter = 8 m，两侧交替）。
 */

import { memo, useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { modelUrl } from '@/assets/models';
import {
  blenderModelsEnabled,
  useSharedGLTF,
  type MergePart,
  boxPart,
  cylPart,
  mergeParts,
} from '@/engine3d';
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { getDayNight } from '../cityTimeStore';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/** GLB 屏幕条目（mat = 共享材质引用；base = GLB 烘焙 emissiveIntensity）。 */
interface ScreenEntry {
  mat: THREE.MeshStandardMaterial;
  base: number;
}

/** 尺寸/落地校验目标（dev 态 glbSizeGuard；与 fallback 同行表值）。 */
const METER_SIZE_TARGET = sizeTargetFor('parkingMeter', { label: 'road/parking_meter' });

// ── fallback 构图常量（米制经 u()；包络取 worldDims('parkingMeter') 唯一事实来源）──
const D = worldDims('parkingMeter'); // x 0.032 / y 0.138 / z 0.028（世界单位）
const BODY_BLUE = '#3a5a8a';
const TOP_DARK = '#2a2f38';
const SCREEN_BG = '#0d1218';
const COIN_DARK = '#1a1a1d';
/** 表头前倾角（rad ≈ 12°）。 */
const HEAD_TILT = 0.21;
const HEAD_H = u(0.3);
const HEAD_Y = u(1.05) + HEAD_H / 2;

/**
 * fallback 原语 → 顶点色合并 1 mesh（底座/杆/表头/投币槽/读卡圆/太阳能顶板）。
 * 屏幕发光面板独立 mesh 供昼夜调制（材质名含 `Screen`）。
 */
function meterParts(): MergePart[] {
  const parts: MergePart[] = [];
  // 圆盘底座
  parts.push(cylPart(u(0.13), u(0.15), u(0.03), 14, 0, u(0.015), 0, TOP_DARK));
  // ⌀76 mm 钢管
  parts.push(cylPart(u(0.038), u(0.042), u(1.05), 10, 0, u(0.03) + u(0.51), 0, BODY_BLUE));
  // 表头盒（前倾 12°）
  {
    const geo = new THREE.BoxGeometry(u(0.28), HEAD_H, u(0.18));
    const q = new THREE.Quaternion().setFromEuler(new THREE.Euler(-HEAD_TILT, 0, 0));
    parts.push({
      geo,
      matrix: new THREE.Matrix4().compose(new THREE.Vector3(0, HEAD_Y, 0), q, new THREE.Vector3(1, 1, 1)),
      color: BODY_BLUE,
    });
  }
  // 投币/刷卡槽（表头侧下）
  parts.push(boxPart(u(0.06), u(0.1), u(0.02), -u(0.08), u(1.12), u(0.07), COIN_DARK));
  // 读卡圆
  parts.push(cylPart(u(0.035), u(0.035), u(0.012), 10, u(0.07), u(1.1), u(0.075), COIN_DARK));
  // 太阳能顶板（微斜 10°，0.30×0.20 m；上缘贴表值 y）
  {
    const geo = new THREE.BoxGeometry(u(0.3), u(0.022), u(0.2));
    const q = new THREE.Quaternion().setFromEuler(new THREE.Euler(0.17, 0, 0));
    parts.push({
      geo,
      matrix: new THREE.Matrix4().compose(new THREE.Vector3(0, D.y - u(0.016), 0), q, new THREE.Vector3(1, 1, 1)),
      color: TOP_DARK,
    });
  }
  return parts;
}

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

// 批次 28 A1/A3：memo + 街具默认不投影（阴影 pass caster 裁剪）。
export const ParkingMeter = memo(function ParkingMeter({ x, z, rotation = 0 }: Props) {
  const info = useObjectInfoProps('prop.parking-meter', { anchorY: 1.8 });
  const url = blenderModelsEnabled() ? modelUrl('road', 'parking_meter') : '';
  const { scene } = useSharedGLTF(url, METER_SIZE_TARGET);
  const glbCloned = useMemo(() => (scene ? scene.clone(true) : null), [scene]);

  // ── C2：GLB 屏幕材质收集（材质名含 `Screen`；基值 = GLB 烘焙常量）──
  const glbScreensRef = useRef<ScreenEntry[]>([]);
  useEffect(() => {
    if (!glbCloned) return;
    const acc: ScreenEntry[] = [];
    glbCloned.traverse((o) => {
      const mesh = o as THREE.Mesh;
      if (!mesh.isMesh) return;
      const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
      for (const m of mats) {
        const std = m as THREE.MeshStandardMaterial | undefined;
        if (!std || !std.name || !('emissiveIntensity' in std)) continue;
        if (std.name.includes('Screen')) acc.push({ mat: std, base: std.emissiveIntensity });
      }
    });
    glbScreensRef.current = acc;
  }, [glbCloned]);

  // fallback 屏幕发光面板（同名 `Screen`，调制口径与 GLB 一致）
  const fbScreen = useMemo(
    () =>
      new THREE.MeshStandardMaterial({
        name: 'Meter_Screen_Mat',
        color: SCREEN_BG,
        emissive: '#7ec8ff',
        emissiveIntensity: 0.45,
        roughness: 0.25,
        metalness: 0.1,
      }),
    [],
  );
  useEffect(() => () => fbScreen.dispose(), [fbScreen]);

  const solidsGeo = useMemo(() => mergeParts(meterParts()), []);
  useEffect(() => () => solidsGeo.dispose(), [solidsGeo]);

  useFrame(() => {
    const day = getDayNight()?.dayFactor01 ?? 1;
    const nightK = 1 - day;
    // C2 契约：屏幕 base × (0.4 + 1.4·nightK)（夜间微亮）
    const mul = 0.4 + 1.4 * nightK;
    for (const e of glbScreensRef.current) e.mat.emissiveIntensity = e.base * mul;
    if (!glbCloned) fbScreen.emissiveIntensity = 0.45 * mul;
  });

  return (
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* GLB 优先；成功时 fallback 整体不渲染（批次 41 V1） */}
      {glbCloned ? (
        <primitive object={glbCloned} />
      ) : (
        <>
          <mesh geometry={solidsGeo}>
            <meshStandardMaterial vertexColors roughness={0.6} metalness={0.35} />
          </mesh>
          {/* 深色屏幕（材质名含 Screen；昼夜调制见 useFrame）；随表头前倾（z 外缘贴表值 z/2） */}
          <mesh position={[0, HEAD_Y + u(0.02), u(0.06)]} rotation={[-HEAD_TILT, 0, 0]}>
            <boxGeometry args={[u(0.18), u(0.12), u(0.016)]} />
            <primitive object={fbScreen} attach="material" />
          </mesh>
        </>
      )}
    </group>
  );
});

export default ParkingMeter;
