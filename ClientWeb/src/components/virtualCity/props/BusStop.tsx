/**
 * BusStop — 公交候车亭（批次 42 街具真实感 · B1/C2/D1）：
 *
 * 双渲染路径（CLAUDE.md §27.3 硬约束 3/7，包围盒同取 cityScale.REAL_DIMS_M.busStop
 * = 5.00 × 2.70 × 1.80 m，长边 X 沿道路向、+Z 朝路心开敞面）：
 *   - GLB 优先：`road/bus_stop.glb`（节点 BusStop，单节点 join）经 useSharedGLTF +
 *     scene.clone(true)（批次 41 V1：成功时 fallback **整体不渲染**，绝不双渲染）；
 *   - 程序化 fallback：斜顶棚 + 青绿檐口色带 + 3 立柱 + 玻璃背板 + 铝条座椅 +
 *     站牌灯箱（emissive，材质名含 `Lightbox`）。
 *
 * C2 昼夜调制（复用 Vehicle.tsx 材质名命中模式）：traverse 收集材质名含 `Lightbox`
 * 的共享材质并烘焙 emissiveIntensity 基值，useFrame 按 getDayNight()?.dayFactor01
 * 调制 `base × (0.25 + 1.75·nightK)`（夜间灯箱成为街道光源感、白天不抢戏）；
 * fallback 灯箱同名同口径。
 *
 * 朝向契约（批次 42 C1，StreetPropsLayer::busStopsForNetwork）：布点 rotation 使
 * 局部 +X 平行道路向、+Z 指向路心 —— 站台长边（GLB X 轴）平行道路，开敞面朝路。
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

/** GLB 灯箱条目（mat = 共享材质引用；base = GLB 烘焙 emissiveIntensity）。 */
interface LightboxEntry {
  mat: THREE.MeshStandardMaterial;
  base: number;
}

/** 尺寸/落地校验目标（dev 态 glbSizeGuard；与 fallback 同行表值）。 */
const BUS_STOP_SIZE_TARGET = sizeTargetFor('busStop', { label: 'road/bus_stop' });

// ── fallback 构图常量（米制经 u()；包络取 worldDims('busStop') 唯一事实来源）──
const D = worldDims('busStop'); // x 0.50 / y 0.27 / z 0.18（世界单位）
const POLE_COLOR = '#5a6270';
const ROOF_COLOR = '#3a414c';
/** 檐口识别色带（青绿 bus_teal 族）。 */
const FASCIA_TEAL = '#2a8f8a';
/** 铝条座椅。 */
const BENCH_ALU = '#c5c8ce';
/** 玻璃背板（半透明）。 */
const GLASS_PANEL = '#a8d5e8';
/** 灯箱暖光（ACES 下 0.5 不过曝）。 */
const LIGHTBOX_EMISSIVE = '#ffe9b8';
/** 顶棚微斜排水坡（rad ≈ 2°，绕 X 倾向开敞面）。 */
const ROOF_SLOPE = 0.035;
const ROOF_T = u(0.12);
/** 顶棚中心 y：斜置后顶缘 ≈ 表值 D.y（半深 × sin(坡) ≈ 0.003 留裕）。 */
const ROOF_Y = D.y - ROOF_T / 2 - u(0.04);
const POST_R = u(0.045);
const POST_Z = -D.z * 0.28;

/**
 * fallback 原语 → 顶点色合并（柱/顶棚/檐带/玻璃框/座椅/灯箱外框/法兰），
 * 几何与逐件 JSX 全等；灯箱发光面板独立 mesh 供昼夜调制（材质名 `Lightbox`）。
 */
function busStopSolidParts(): MergePart[] {
  const parts: MergePart[] = [];
  // 立柱 ×3（⌀90 mm 钢管；柱顶埋入顶棚）
  const postH = ROOF_Y - ROOF_T / 2 + u(0.02);
  for (const px of [-D.x * 0.38, 0, D.x * 0.38]) {
    parts.push(cylPart(POST_R, POST_R, postH, 10, px, postH / 2, POST_Z, POLE_COLOR));
    // 柱底法兰
    parts.push(cylPart(POST_R * 1.9, POST_R * 2.1, u(0.03), 10, px, u(0.015), POST_Z, ROOF_COLOR));
  }
  // 斜顶棚（钢板微斜 2° 排水坡，倾向开敞面 +Z）
  {
    const geo = new THREE.BoxGeometry(D.x, ROOF_T, D.z);
    const q = new THREE.Quaternion().setFromEuler(new THREE.Euler(ROOF_SLOPE, 0, 0));
    parts.push({
      geo,
      matrix: new THREE.Matrix4().compose(
        new THREE.Vector3(0, ROOF_Y, 0),
        q,
        new THREE.Vector3(1, 1, 1),
      ),
      color: ROOF_COLOR,
    });
  }
  // 檐口色带（青绿；沿开敞面檐口 + 两短边）
  parts.push(boxPart(D.x, u(0.12), u(0.1), 0, ROOF_Y - ROOF_T / 2 - u(0.06), D.z / 2 - u(0.05), FASCIA_TEAL));
  for (const sx of [-1, 1]) {
    parts.push(boxPart(u(0.1), u(0.12), D.z, sx * (D.x / 2 - u(0.05)), ROOF_Y - ROOF_T / 2 - u(0.06), 0, FASCIA_TEAL));
  }
  // 背板金属框（上下横档；玻璃本体半透明独立 mesh，见组件 JSX）
  const glassH = u(2.0);
  const glassY = u(0.12) + glassH / 2;
  parts.push(boxPart(D.x * 0.84, u(0.05), u(0.08), 0, glassY + glassH / 2, -D.z * 0.4, POLE_COLOR));
  parts.push(boxPart(D.x * 0.84, u(0.05), u(0.08), 0, glassY - glassH / 2, -D.z * 0.4, POLE_COLOR));
  // 铝条座椅（高 0.44 m、深 0.42 m；3 横档沿 X，档间留缝）
  const seatY = u(0.44);
  for (let i = 0; i < 3; i++) {
    parts.push(boxPart(D.x * 0.62, u(0.035), u(0.1), -D.x * 0.06, seatY, -D.z * 0.18 + i * u(0.13), BENCH_ALU));
  }
  // 座椅腿 ×2（靠背板侧落地）
  for (const lx of [-D.x * 0.22, D.x * 0.1]) {
    parts.push(boxPart(u(0.05), seatY, u(0.3), lx, seatY / 2, -D.z * 0.11, POLE_COLOR));
    // 座椅靠背竖档
    parts.push(boxPart(u(0.05), u(0.42), u(0.05), lx, seatY + u(0.21), -D.z * 0.32, POLE_COLOR));
  }
  // 站牌灯箱外框（−X 端；发光面板独立 mesh，见 LIGHTBOX）
  parts.push(boxPart(u(0.34), u(1.32), u(0.1), -D.x * 0.38, u(1.55), D.z * 0.22, POLE_COLOR));
  // 灯箱立杆
  parts.push(cylPart(u(0.035), u(0.04), u(1.05), 8, -D.x * 0.38, u(0.52), D.z * 0.22, POLE_COLOR));
  return parts;
}

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

// 批次 28 A1/A3：memo + 街具默认不投影（阴影 pass caster 裁剪）。
export const BusStop = memo(function BusStop({ x, z, rotation = 0 }: Props) {
  const info = useObjectInfoProps('prop.bus-stop', { anchorY: 2.6 });
  const url = blenderModelsEnabled() ? modelUrl('road', 'bus_stop') : '';
  const { scene } = useSharedGLTF(url, BUS_STOP_SIZE_TARGET);
  const glbCloned = useMemo(() => (scene ? scene.clone(true) : null), [scene]);

  // ── C2：GLB 灯箱材质收集（材质名含 `Lightbox`；基值 = GLB 烘焙常量）──
  const glbLightsRef = useRef<LightboxEntry[]>([]);
  useEffect(() => {
    if (!glbCloned) return;
    const acc: LightboxEntry[] = [];
    glbCloned.traverse((o) => {
      const mesh = o as THREE.Mesh;
      if (!mesh.isMesh) return;
      const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
      for (const m of mats) {
        const std = m as THREE.MeshStandardMaterial | undefined;
        if (!std || !std.name || !('emissiveIntensity' in std)) continue;
        if (std.name.includes('Lightbox')) acc.push({ mat: std, base: std.emissiveIntensity });
      }
    });
    glbLightsRef.current = acc;
  }, [glbCloned]);

  // fallback 灯箱发光面板（同名 `Lightbox`，调制口径与 GLB 一致）
  const fbLightbox = useMemo(
    () =>
      new THREE.MeshStandardMaterial({
        name: 'BusStop_Lightbox_Mat',
        color: LIGHTBOX_EMISSIVE,
        emissive: LIGHTBOX_EMISSIVE,
        emissiveIntensity: 0.5,
        roughness: 0.4,
        metalness: 0.1,
      }),
    [],
  );
  useEffect(() => () => fbLightbox.dispose(), [fbLightbox]);

  // fallback 实体件顶点色合并（1 mesh；灯箱面板独立）
  const solidsGeo = useMemo(() => mergeParts(busStopSolidParts()), []);
  useEffect(() => () => solidsGeo.dispose(), [solidsGeo]);

  useFrame(() => {
    const day = getDayNight()?.dayFactor01 ?? 1;
    const nightK = 1 - day;
    // C2 契约：灯箱 base × (0.25 + 1.75·nightK)（夜间亮、白天弱）
    const mul = 0.25 + 1.75 * nightK;
    for (const e of glbLightsRef.current) e.mat.emissiveIntensity = e.base * mul;
    if (!glbCloned) fbLightbox.emissiveIntensity = 0.5 * mul;
  });

  return (
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* GLB 优先；成功时 fallback 整体不渲染（批次 41 V1） */}
      {glbCloned ? (
        <primitive object={glbCloned} />
      ) : (
        <>
          <mesh geometry={solidsGeo}>
            <meshStandardMaterial vertexColors roughness={0.72} metalness={0.18} />
          </mesh>
          {/* 玻璃背板（半透明，独立材质；不投影） */}
          <mesh position={[0, u(1.12), -D.z * 0.4]}>
            <boxGeometry args={[D.x * 0.82, u(2.0), u(0.06)]} />
            <meshStandardMaterial
              color={GLASS_PANEL}
              transparent
              opacity={0.35}
              roughness={0.08}
              metalness={0.25}
            />
          </mesh>
          {/* 站牌灯箱发光面板（材质名含 Lightbox；昼夜调制见 useFrame） */}
          <mesh position={[-D.x * 0.38, u(1.55), D.z * 0.22 + u(0.055)]}>
            <boxGeometry args={[u(0.3), u(1.26), u(0.02)]} />
            <primitive object={fbLightbox} attach="material" />
          </mesh>
        </>
      )}
    </group>
  );
});
