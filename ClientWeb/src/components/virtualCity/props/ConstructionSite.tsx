/**
 * ConstructionSite — 施工工地 + 塔吊（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 文创区东南角：裸土面 + 工程黄围挡 4 面 + 塔吊（格构塔身 / 起重臂 /
 * 平衡臂+配重 / 操作室 / 吊钩钢缆）。城市天际线「在生长」的标配符号。
 *
 * 批次 28 二轮（DC 攻坚）：21 mesh → 3 mesh（engine3d/geoMerge，几何全等）：
 *   1) 裸土面独立保留（receiveShadow 语义不动，泥土粗糙度 0.98 不参与统一）；
 *   2) 非金属纯色件（围挡黄板 ×4 + 压条 ×4 / 塔身 / 横撑 ×3 / 起重臂 / 配重）
 *      顶点色合并单 mesh；
 *   3) 金属件（操作室 / 平衡臂 / 塔头 / 拉索 / 钢缆 / 吊钩）顶点色合并单 mesh。
 * 批次 28 二轮取舍：非金属组粗糙度 0.6–0.98 → 0.7、金属度恒 0、塔身/起重臂
 * envMapIntensity 0.6 → 默认 1.0（组内多数件本为默认）；金属组粗糙度
 * 0.3–0.8 → 0.5、金属度恒 0.5、envMapIntensity 0.8–1.0 → 0.9（钢缆原无
 * 金属归入金属组，0.008 半径细件不可辨）。
 * 批次 28 二轮取舍：caster 裁剪——地标不投影（shadow pass 实测 1044 DC 超阈，
 * 全地标 castShadow=false；原围挡/塔身/臂/操作室的投影随批取消，裸土面
 * receiveShadow 保留仍承接楼体投影）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.3。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { u } from '../cityScale';
import { type MergePart, boxPart, cylPart, mergeParts } from '@/engine3d';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const SITE_X = 28; // 批次 20 §3.4 语义改注：软件园区界（software_park (38,14) 西南侧空地，
const SITE_Z = -1; // 坐标不动，与 16 新区底板两两校验无碰撞）；楼群 / 围挡 / 塔吊不与城区建筑穿插
const SITE_W = 3.4;
const SITE_D = 2.6;

const CRANE_X = -0.6; // 塔吊场内靠后侧（原嵌套 group position 烘焙进部件坐标）
const CRANE_Z = -0.4;

const YELLOW = '#e8b930';
const YELLOW_DARK = '#b8921f';
const STEEL = '#d8dce2';
const MUD = '#6b5a44';
const CABIN = '#2a4a6e';       // 操作室
const COUNTERWEIGHT = '#8a8d96'; // 配重块
const CABLE = '#3a3f47';       // 吊钩钢缆
const HOOK = '#5a6270';        // 钩块

/** 非金属组统一粗糙度（批次 28 二轮取舍：原 0.6–0.98 取中）。 */
const PLAIN_ROUGH = 0.7;
/** 金属组统一材质参数（批次 28 二轮取舍：原 0.3–0.8 / 0.5 取中）。 */
const METAL_ROUGH = 0.5;
const METAL_METAL = 0.5;

interface Props {
  /** 朝向（弧度；默认面向园区中心）。 */
  rotation?: number;
}

export function ConstructionSite({ rotation }: Props) {
  // 默认朝向：面向 cultural_creative 中心 (24,8)
  const rot = rotation ?? Math.atan2(24 - SITE_X, 8 - SITE_Z);
  const mastH = u(9);

  // 非金属纯色件（围挡 + 塔身/横撑/起重臂/配重）→ 单顶点色 mesh
  const plainGeo = useMemo(() => {
    const parts: MergePart[] = [];
    // 围挡 4 面（工程黄 + 深色压条；原嵌套 group 偏移烘焙进部件坐标）
    const fences: Array<[number, number, boolean]> = [
      [0, -SITE_D / 2, true],
      [0, SITE_D / 2, true],
      [-SITE_W / 2, 0, false],
      [SITE_W / 2, 0, false],
    ];
    for (const [fx, fz, horizontal] of fences) {
      parts.push(
        boxPart(
          horizontal ? SITE_W : 0.04, u(1.8), horizontal ? 0.04 : SITE_D,
          fx, u(0.9), fz, YELLOW,
        ),
        boxPart(
          horizontal ? SITE_W : 0.05, u(0.16), horizontal ? 0.05 : SITE_D,
          fx, u(1.72), fz, YELLOW_DARK,
        ),
      );
    }
    // 格构塔身 + 3 道横撑
    parts.push(boxPart(0.16, mastH, 0.16, CRANE_X, mastH / 2, CRANE_Z, YELLOW));
    for (const t of [0.3, 0.55, 0.8]) {
      parts.push(boxPart(0.2, 0.03, 0.2, CRANE_X, mastH * t, CRANE_Z, YELLOW_DARK));
    }
    // 起重臂（长臂，沿 +x）+ 配重块
    parts.push(boxPart(u(7.2), 0.1, 0.1, CRANE_X + u(3.6), mastH + u(0.9), CRANE_Z, YELLOW));
    parts.push(boxPart(u(0.6), u(0.5), u(0.5), CRANE_X - u(1.9), mastH + u(0.7), CRANE_Z, COUNTERWEIGHT));
    return mergeParts(parts);
  }, [mastH]);
  useEffect(() => () => plainGeo.dispose(), [plainGeo]);

  // 金属件（操作室 / 平衡臂 / 塔头 / 拉索 / 钢缆 / 吊钩）→ 单顶点色 mesh
  const metalGeo = useMemo(() => {
    const parts: MergePart[] = [
      // 操作室（塔顶）
      boxPart(u(0.9), u(0.8), u(0.9), CRANE_X + 0.14, mastH + u(0.4), CRANE_Z, CABIN),
      // 平衡臂（短臂，沿 -x）
      boxPart(u(2.2), 0.1, 0.1, CRANE_X - u(1.1), mastH + u(0.9), CRANE_Z, STEEL),
      // 塔头
      boxPart(0.05, u(1.4), 0.05, CRANE_X, mastH + u(1.6), CRANE_Z, STEEL),
      // 前拉索（rotZ 0.32 逐位保留）
      {
        geo: new THREE.BoxGeometry(u(4.6), 0.015, 0.015),
        x: CRANE_X + u(2.4), y: mastH + u(1.35), z: CRANE_Z,
        rotZ: 0.32, color: STEEL,
      },
      // 吊钩钢缆 + 钩块（臂前段垂下）
      cylPart(0.008, 0.008, u(1.3), 4, CRANE_X + u(4.6), mastH + u(0.25), CRANE_Z, CABLE),
      boxPart(0.06, 0.06, 0.06, CRANE_X + u(4.6), mastH - u(0.45), CRANE_Z, HOOK),
    ];
    return mergeParts(parts);
  }, [mastH]);
  useEffect(() => () => metalGeo.dispose(), [metalGeo]);

  const info = useObjectInfoProps('landmark.construction-site', { anchorY: 1.0 });
  return (
    <group
      {...info}
      position={[SITE_X, 0, SITE_Z]}
      rotation={[0, rot, 0]}
    >
      {/* 裸土面（独立保留：receiveShadow + 粗糙度 0.98 均不动） */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.028, 0]} receiveShadow>
        <planeGeometry args={[SITE_W, SITE_D]} />
        <meshStandardMaterial color={MUD} roughness={0.98} />
      </mesh>
      {/* 非金属纯色件合并（顶点色逐件保留；批次 28 二轮 caster 裁剪 → 不投影） */}
      <mesh geometry={plainGeo}>
        <meshStandardMaterial vertexColors roughness={PLAIN_ROUGH} metalness={0} />
      </mesh>
      {/* 金属件合并（顶点色逐件保留；envMapIntensity 统一 0.9；不投影） */}
      <mesh geometry={metalGeo}>
        <meshStandardMaterial
          vertexColors
          roughness={METAL_ROUGH}
          metalness={METAL_METAL}
          envMapIntensity={0.9}
        />
      </mesh>
    </group>
  );
}

export default ConstructionSite;
