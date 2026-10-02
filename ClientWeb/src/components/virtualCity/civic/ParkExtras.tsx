/**
 * 公园补全（18-AA §5.2 → **批次 45 重构**：凉亭/游乐/健身 GLB 化 + 健身新设）
 *
 * 批次 45 前状态：六角凉亭（6 柱+六棱锥+球）/ 滑梯（1 斜板）/ 秋千 / 公厕
 * 四组合并为 2 个顶点色 mesh，材质统一 roughness 0.8 / metalness 0.25（批次 28
 * 二轮取舍）—— 木构/瓦面/塑料不可辨，是「公园设施真实感」的主要缺口。
 *
 * 批次 45 改造（一种物体 = 公园设施族，方案 45 §4 B1/C4）：
 *   · 凉亭 / 游乐 / 健身三件 GLB 优先（`useSharedGLTF` + clone + 条件
 *     `<primitive>`，同 BusStop 范式；GLB 成功时 fallback 整体不渲染）；
 *   · 程序化 fallback 保留为降级链（§27.3-3），并补两处硬伤（D1）：
 *     凉亭锥顶下加檐口环（不再直接坐柱头）、滑梯斜板加双边板；
 *   · 健身三件套为**新设**（GB 51192 §8.6 游戏健身设施），fallback 为简化杆件；
 *   · 公厕**不动**（方案 45 §8 明确不做，与 gamma 全量重导批次合并评估）；
 *   · 凉亭檐下灯（C4）：1 个 emissive 小盒挂檐口内侧 + 夜间地面光斑 ——
 *     公园夜景锚点（路灯只照园外路缘）。
 *
 * 布点单一事实来源：`props/parkLayout.ts`（批次 45 C1）。
 * 材质/比例真实依据见 `3d_script/build_park_pavilion.py` 等脚本头注释。
 */
import { useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Instances, Instance } from '@react-three/drei';
import { blenderModelsEnabled, useSharedGLTF, boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { u, sizeTargetFor } from '../cityScale';
import { getDayNight } from '../cityTimeStore';
import { PAVILION_SPOT, PLAYGROUND_SPOT, FITNESS_SPOT, RESTROOM_SPOT, parkWorld } from '../props/parkLayout';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const WOOD = '#8a5a44';
const WOOD_LIGHT = '#a87055';
const METAL = '#5a6270';
const TILE = '#d8d4ca';
const DARK = '#3a3f4a';
const SLIDE_RED = '#c0392b';
const STEEL_GREEN = '#2f7d4f';

/** 程序化件统一材质参数（批次 28 二轮口径保留）。 */
const FLAT_ROUGH = 0.8;
const FLAT_METAL = 0.25;

/** 凉亭 GLB 檐下灯：夜间 emissive 强度（昼 0.08 → 夜 2.2，随 dayFactor01 渐变）。 */
const EAVE_LAMP_NIGHT = 2.2;

/** 欧拉旋转 + 平移的部件矩阵（等价 rotation + position 组合）。 */
function partMatrix(x: number, y: number, z: number, rx: number, ry: number, rz: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)),
    new THREE.Vector3(1, 1, 1),
  );
}

// ── 程序化 fallback（批次 18-AA 原几何 + 批次 45 D1 两处修补）──────────────

/** 六角凉亭 fallback：6 立柱 + 圆锥尖顶 + 顶冠球。D1：锥顶下加檐口六棱环。 */
function pavilionParts(x: number, z: number): MergePart[] {
  const parts: MergePart[] = [];
  for (let i = 0; i < 6; i++) {
    const a = (i * Math.PI) / 3;
    parts.push({
      geo: new THREE.CylinderGeometry(u(0.08), u(0.08), u(2.4), 6),
      x: x + Math.cos(a) * u(1.6),
      y: u(1.2),
      z: z + Math.sin(a) * u(1.6),
      color: WOOD,
    });
  }
  // D1 修补：檐口六棱环（薄扁六棱柱）—— 锥顶不再直接坐在柱头上
  parts.push(cylPart(u(2.0), u(2.2), u(0.18), 6, x, u(2.45), z, WOOD_LIGHT));
  parts.push({ geo: new THREE.ConeGeometry(u(2.2), u(1.4), 6), x, y: u(3.0), z, color: WOOD_LIGHT });
  parts.push({ geo: new THREE.SphereGeometry(u(0.12), 8, 8), x, y: u(3.8), z, color: DARK });
  return parts;
}

/** 游乐场 fallback：滑梯（梯 + 斜面 + D1 双边板）+ 秋千（横梁 + 双绳双椅）。 */
function playgroundParts(x: number, z: number): MergePart[] {
  const parts: MergePart[] = [
    boxPart(u(0.4), u(2), u(0.4), x - u(0.5), u(1), z, WOOD),
  ];
  // D1 修补：滑道双边板（防「一块裸斜板」观感）
  for (const dz of [-0.24, 0.24]) {
    parts.push({
      geo: new THREE.BoxGeometry(u(2.5), u(0.14), u(0.04)),
      matrix: partMatrix(x - u(0.3), u(1), z + u(1.2) + u(dz), -0.5, 0, 0),
      color: DARK,
    });
  }
  parts.push({
    geo: new THREE.BoxGeometry(u(0.6), u(0.06), u(2.5)),
    matrix: partMatrix(x - u(0.3), u(1), z + u(1.2), -0.5, 0, 0),
    color: SLIDE_RED,
  });
  // 秋千横梁 + 双摆位
  parts.push({
    geo: new THREE.CylinderGeometry(u(0.05), u(0.05), u(3.2), 6),
    rotZ: Math.PI / 2,
    x, y: u(3), z,
    color: METAL,
  });
  for (const dx of [-u(1.5), u(1.5)]) {
    parts.push({ geo: new THREE.CylinderGeometry(u(0.08), u(0.08), u(3), 6), x: x + dx, y: u(1.5), z, color: METAL });
  }
  for (const dx of [-u(0.8), u(0.8)]) {
    parts.push({ geo: new THREE.CylinderGeometry(u(0.02), u(0.02), u(1.4), 6), x: x + dx, y: u(3), z, color: METAL });
    parts.push(boxPart(u(0.6), u(0.1), u(0.3), x + dx, u(2.3), z, WOOD));
  }
  return parts;
}

/** 健身三件套 fallback（批次 45 新设的简化杆件：漫步机 + 扭腰盘 + 单杠）。 */
function fitnessParts(x: number, z: number): MergePart[] {
  const parts: MergePart[] = [
    // 双位漫步机：双柱 + 顶梁 + 4 摆杆
    cylPart(u(0.06), u(0.06), u(1.1), 6, x + u(1.9), u(0.55), z - u(0.3), STEEL_GREEN),
    cylPart(u(0.06), u(0.06), u(1.1), 6, x + u(1.9), u(0.55), z + u(0.3), STEEL_GREEN),
    {
      geo: new THREE.CylinderGeometry(u(0.05), u(0.05), u(0.66), 6),
      matrix: partMatrix(x + u(1.9), u(1.1), z, Math.PI / 2, 0, 0),
      color: STEEL_GREEN,
    },
    ...[-0.16, 0.16].flatMap((dz) => [
      cylPart(u(0.03), u(0.03), u(0.85), 6, x + u(1.9), u(0.68), z + u(dz), DARK),
      boxPart(u(0.3), u(0.04), u(0.12), x + u(1.9), u(0.24), z + u(dz), DARK),
    ]),
    // 扭腰器：立柱 + 圆盘 + 扶手杠
    cylPart(u(0.05), u(0.05), u(1.05), 6, x, u(0.53), z, STEEL_GREEN),
    cylPart(u(0.62), u(0.62), u(0.1), 12, x, u(0.05), z, DARK),
    boxPart(u(0.05), u(0.9), u(0.05), x, u(1.0), z, STEEL_GREEN),
    // 单杠：双柱 + 横杠
    cylPart(u(0.05), u(0.05), u(2.1), 6, x - u(1.6), u(1.05), z - u(0.6), STEEL_GREEN),
    cylPart(u(0.05), u(0.05), u(2.1), 6, x - u(1.6), u(1.05), z + u(0.6), STEEL_GREEN),
    {
      geo: new THREE.CylinderGeometry(u(0.03), u(0.03), u(1.3), 6),
      matrix: partMatrix(x - u(1.6), u(2.1), z, Math.PI / 2, 0, 0),
      color: DARK,
    },
  ];
  return parts;
}

/** 公厕小屋部件（批次 18-AA 原几何，本批不动）。 */
function restroomParts(x: number, z: number): MergePart[] {
  return [
    boxPart(u(3), u(2.8), u(2), x, u(1.4), z, TILE),
    boxPart(u(3.2), u(0.15), u(2.2), x, u(2.9), z, WOOD),
    { geo: new THREE.PlaneGeometry(u(0.8), u(1.8)), x, y: u(1), z: z + u(1.01), color: DARK },
  ];
}

/** 单件「GLB 优先 + 程序化 fallback」的公园大件（useSharedGLTF + clone 范式）。 */
function ParkMajorPiece({
  glbName,
  dimsKey,
  spot,
  infoId,
  anchorY,
  fallbackParts,
}: {
  glbName: string;
  dimsKey: 'parkPavilion' | 'parkPlayground' | 'parkFitness';
  spot: { x: number; z: number; rot: number };
  infoId: string;
  anchorY: number;
  fallbackParts: (x: number, z: number) => MergePart[];
}) {
  const info = useObjectInfoProps(infoId, { anchorY });
  const url = blenderModelsEnabled() ? modelUrl('civic', glbName) : '';
  const sizeTarget = useMemo(
    () => sizeTargetFor(dimsKey, { label: `civic/${glbName}` }),
    [dimsKey, glbName],
  );
  const { scene } = useSharedGLTF(url, sizeTarget);
  const cloned = useMemo(() => (scene ? scene.clone(true) : null), [scene]);

  // fallback 顶点色合并（GLB 成功时不渲染；几何仍一次性构建，量小无妨）
  const geo = useMemo(() => mergeParts(fallbackParts(spot.x, spot.z)), [spot.x, spot.z]);
  useEffect(() => () => geo.dispose(), [geo]);

  return (
    <group {...info} position={[spot.x, 0, spot.z]} rotation={[0, spot.rot, 0]}>
      {cloned ? <primitive object={cloned} /> : (
        <mesh geometry={geo}>
          <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
        </mesh>
      )}
    </group>
  );
}

/** 凉亭檐下灯（C4）：emissive 小盒 + 夜间地面光斑（GLB 与 fallback 共有）。 */
function PavilionEaveLamp({ x, z }: { x: number; z: number }) {
  const matRef = useRef<THREE.MeshStandardMaterial>(null);
  const poolMatRef = useRef<THREE.MeshBasicMaterial>(null);
  useFrame((_s, delta) => {
    const day = getDayNight()?.dayFactor01 ?? 1;
    const k = Math.min(1, delta * 2);
    const night = 1 - day;
    const m = matRef.current;
    if (m) m.emissiveIntensity += (0.08 + (EAVE_LAMP_NIGHT - 0.08) * night - m.emissiveIntensity) * k;
    const p = poolMatRef.current;
    if (p) p.opacity += (Math.max(0, 1 - day * 2) * 0.6 - p.opacity) * k;
  });
  // 檐下灯纹理：径向暖光（复用 StreetLightsInstanced 的 pool 画法，本件独立小图）
  const poolTex = useMemo(() => {
    const size = 64;
    const canvas = document.createElement('canvas');
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext('2d');
    if (ctx) {
      const g = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
      g.addColorStop(0, 'rgba(255,230,178,1)');
      g.addColorStop(0.45, 'rgba(255,210,140,0.32)');
      g.addColorStop(1, 'rgba(255,200,120,0)');
      ctx.fillStyle = g;
      ctx.fillRect(0, 0, size, size);
    }
    const tex = new THREE.CanvasTexture(canvas);
    tex.colorSpace = THREE.SRGBColorSpace;
    return tex;
  }, []);
  useEffect(() => () => poolTex.dispose(), [poolTex]);

  // 檐口高度 ≈ 台基 0.4 + 柱 2.8 = 3.2 m；光池 ⌀7 m（≈ 2 倍檐高）
  return (
    <group position={[x, 0, z]}>
      <mesh position={[0, u(3.12), u(0.9)]}>
        <boxGeometry args={[u(0.36), u(0.14), u(0.22)]} />
        <meshStandardMaterial
          ref={matRef}
          color="#f2e3c0"
          emissive="#ffd9a0"
          emissiveIntensity={0.08}
          roughness={0.4}
        />
      </mesh>
      <Instances limit={1} range={1} raycast={() => null}>
        <planeGeometry args={[u(7), u(7)]} />
        <meshBasicMaterial
          ref={poolMatRef}
          map={poolTex}
          transparent
          opacity={0}
          depthWrite={false}
          blending={THREE.AdditiveBlending}
        />
        <Instance position={[0, 0.032, 0]} rotation={[-Math.PI / 2, 0, 0]} />
      </Instances>
    </group>
  );
}

export function ParkExtras() {
  const [pavX, pavZ] = parkWorld(PAVILION_SPOT.x, PAVILION_SPOT.z);
  const [pgX, pgZ] = parkWorld(PLAYGROUND_SPOT.x, PLAYGROUND_SPOT.z);
  const [fitX, fitZ] = parkWorld(FITNESS_SPOT.x, FITNESS_SPOT.z);
  // 公厕（批次 18-AA 原位原样；批次 45 只把它从旧 flatGeo 拆出独立挂 info）
  const [wcX, wcZ] = parkWorld(RESTROOM_SPOT.x, RESTROOM_SPOT.z);
  const wcInfo = useObjectInfoProps('civic.park-extras', { anchorY: 1.5 });

  const wcGeo = useMemo(() => mergeParts(restroomParts(wcX, wcZ)), [wcX, wcZ]);
  useEffect(() => () => wcGeo.dispose(), [wcGeo]);
  const wcSignGeo = useMemo(
    () => mergeParts([boxPart(u(0.6), u(0.3), u(0.05), wcX + u(1.4), u(2.5), wcZ + u(1.01))]),
    [wcX, wcZ],
  );
  useEffect(() => () => wcSignGeo.dispose(), [wcSignGeo]);

  return (
    <group>
      {/* 三大件：GLB 优先 + 程序化 fallback（GLB 成功时 fallback 不渲染） */}
      <ParkMajorPiece
        glbName="park_pavilion" dimsKey="parkPavilion" spot={{ x: pavX, z: pavZ, rot: PAVILION_SPOT.rot }}
        infoId="park.pavilion" anchorY={2.6} fallbackParts={pavilionParts}
      />
      <ParkMajorPiece
        glbName="park_playground" dimsKey="parkPlayground"
        spot={{ x: pgX, z: pgZ, rot: PLAYGROUND_SPOT.rot }}
        infoId="park.playground" anchorY={1.6} fallbackParts={playgroundParts}
      />
      <ParkMajorPiece
        glbName="park_fitness" dimsKey="parkFitness"
        spot={{ x: fitX, z: fitZ, rot: FITNESS_SPOT.rot }}
        infoId="park.fitness" anchorY={1.4} fallbackParts={fitnessParts}
      />
      {/* 凉亭檐下灯（C4：公园夜景锚点；GLB/fallback 两路共有） */}
      <PavilionEaveLamp x={pavX} z={pavZ} />
      {/* 公厕（不动；独立 objectInfo 挂点） */}
      <group {...wcInfo}>
        <mesh geometry={wcGeo}>
          <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
        </mesh>
        <mesh geometry={wcSignGeo}>
          <meshStandardMaterial color={METAL} emissive={METAL} emissiveIntensity={0.3} />
        </mesh>
      </group>
    </group>
  );
}

export default ParkExtras;
