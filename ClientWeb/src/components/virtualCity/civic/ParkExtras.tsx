/**
 * 公园补全（18-AA · §5.2 ParkExtras）
 *   六角凉亭 + 滑梯 + 秋千架 + 公厕小屋。布点：凉亭 (-2.2, -24.5)；游乐 (2.2, -24.5)；公厕 (-2.5, -19.8)。
 *
 * 批次 28 二轮（DC 攻坚）：21 mesh → 2 mesh（几何全等合并，engine3d/geoMerge）：
 *   1) 纯色件 mesh（顶点色）：凉亭（6 柱 + 锥顶 + 顶冠球）+ 滑梯（梯 + 斜面）+
 *      秋千架（2 柱 + 横梁 + 2 绳 + 2 椅）+ 公厕（体 + 檐 + 门）共 20 件 ——
 *      部件颜色经顶点色逐件保留。
 *   2) 公厕标牌 mesh：保留自发光 #5a6270（emissive 件不并入顶点色 mesh）。
 * 批次 28 二轮取舍：纯色件粗糙度/金属度统一 0.8/0.25（原 0.5–1.0 / 0–0.6 区间
 *   取中，木件/金属件质感差异在公园尺度不可辨）；caster 裁剪——市政设施不投影
 *   （shadow pass 实测 1044 DC 超 500 阈值，只留楼体+树干）。
 */
import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { boxPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const WOOD = '#8a5a44';
const WOOD_LIGHT = '#a87055';
const METAL = '#5a6270';
const TILE = '#d8d4ca';
const DARK = '#3a3f4a';
const SLIDE_RED = '#c0392b';

/** 纯色件统一材质参数（批次 28 二轮视觉取舍：原 roughness 0.5–1.0 / metalness 0–0.6 取中）。 */
const FLAT_ROUGH = 0.8;
const FLAT_METAL = 0.25;

/** 欧拉旋转 + 平移的部件矩阵（等价原 JSX 的 rotation + position 组合）。 */
function partMatrix(x: number, y: number, z: number, rx: number, ry: number, rz: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)),
    new THREE.Vector3(1, 1, 1),
  );
}

/** 六角凉亭部件（6 立柱 + 圆锥尖顶 + 顶冠球）。 */
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
  parts.push({ geo: new THREE.ConeGeometry(u(2.2), u(1.4), 6), x, y: u(2.6), z, color: WOOD_LIGHT });
  parts.push({ geo: new THREE.SphereGeometry(u(0.12), 8, 8), x, y: u(3.5), z, color: DARK });
  return parts;
}

/** 游乐场部件（滑梯梯 + 斜面 + 秋千 2 柱 + 横梁 + 2 绳 + 2 椅）。 */
function playgroundParts(x: number, z: number): MergePart[] {
  const parts: MergePart[] = [
    // 滑梯：梯 + 斜面（斜面绕 X 倾斜 -0.5rad）
    boxPart(u(0.4), u(2), u(0.4), x - u(0.5), u(1), z, WOOD),
    {
      geo: new THREE.BoxGeometry(u(0.6), u(0.06), u(2.5)),
      matrix: partMatrix(x - u(0.3), u(1), z + u(1.2), -0.5, 0, 0),
      color: SLIDE_RED,
    },
    // 秋千横梁（绕 Z 转 90° 的水平圆柱）
    {
      geo: new THREE.CylinderGeometry(u(0.05), u(0.05), u(3.2), 6),
      rotZ: Math.PI / 2,
      x, y: u(3), z,
      color: METAL,
    },
  ];
  for (const dx of [-u(1.5), u(1.5)]) {
    parts.push({ geo: new THREE.CylinderGeometry(u(0.08), u(0.08), u(3), 6), x: x + dx, y: u(1.5), z, color: METAL });
  }
  for (const dx of [-u(0.8), u(0.8)]) {
    parts.push({ geo: new THREE.CylinderGeometry(u(0.02), u(0.02), u(1.4), 6), x: x + dx, y: u(3), z, color: METAL });
    parts.push(boxPart(u(0.6), u(0.1), u(0.3), x + dx, u(2.3), z, WOOD));
  }
  return parts;
}

/** 公厕小屋部件（体 + 檐 + 门 plane；标牌为 emissive 独立 mesh）。 */
function restroomParts(x: number, z: number): MergePart[] {
  return [
    boxPart(u(3), u(2.8), u(2), x, u(1.4), z, TILE),
    boxPart(u(3.2), u(0.15), u(2.2), x, u(2.9), z, WOOD),
    { geo: new THREE.PlaneGeometry(u(0.8), u(1.8)), x, y: u(1), z: z + u(1.01), color: DARK },
  ];
}

export function ParkExtras() {
  const info = useObjectInfoProps('civic.park-extras', { anchorY: 1.5 });

  // 纯色件 20 件合并（凉亭 / 游乐场 / 公厕三组布点随组偏移展开）
  const flatGeo = useMemo(
    () =>
      mergeParts([
        ...pavilionParts(-2.2, -24.5),
        ...playgroundParts(2.2, -24.5),
        ...restroomParts(-2.5, -19.8),
      ]),
    [],
  );
  useEffect(() => () => flatGeo.dispose(), [flatGeo]);

  // 公厕标牌（自发光件，独立 mesh 保留 emissive）
  const signGeo = useMemo(() => mergeParts([boxPart(u(0.6), u(0.3), u(0.05), -2.5 + u(1.4), u(2.5), -19.8 + u(1.01))]), []);
  useEffect(() => () => signGeo.dispose(), [signGeo]);

  return (
    <group {...info}>
      {/* 纯色件：凉亭 + 游乐场 + 公厕 20 件合 1 mesh（caster 裁剪不投影） */}
      <mesh geometry={flatGeo}>
        <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
      </mesh>
      {/* 公厕标牌（自发光） */}
      <mesh geometry={signGeo}>
        <meshStandardMaterial color={METAL} emissive={METAL} emissiveIntensity={0.3} />
      </mesh>
    </group>
  );
}
