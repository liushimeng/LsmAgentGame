/**
 * ConstructionSite — 施工工地 + 塔吊（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 文创区东南角：裸土面 + 工程黄围挡 4 面 + 塔吊（格构塔身 / 起重臂 /
 * 平衡臂+配重 / 操作室 / 吊钩钢缆）。城市天际线「在生长」的标配符号。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.3。
 */

import { u } from '../cityScale';

const SITE_X = 28; // 批次 20 §3.4 语义改注：软件园区界（software_park (38,14) 西南侧空地，
const SITE_Z = -1; // 坐标不动，与 16 新区底板两两校验无碰撞）；楼群 / 围挡 / 塔吊不与城区建筑穿插
const SITE_W = 3.4;
const SITE_D = 2.6;

const YELLOW = '#e8b930';
const YELLOW_DARK = '#b8921f';
const STEEL = '#d8dce2';
const MUD = '#6b5a44';

interface Props {
  /** 朝向（弧度；默认面向园区中心）。 */
  rotation?: number;
}

export function ConstructionSite({ rotation }: Props) {
  // 默认朝向：面向 cultural_creative 中心 (24,8)
  const rot = rotation ?? Math.atan2(24 - SITE_X, 8 - SITE_Z);
  const mastH = u(9);
  return (
    <group position={[SITE_X, 0, SITE_Z]} rotation={[0, rot, 0]}>
      {/* 裸土面 */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.028, 0]} receiveShadow>
        <planeGeometry args={[SITE_W, SITE_D]} />
        <meshStandardMaterial color={MUD} roughness={0.98} />
      </mesh>
      {/* 围挡 4 面（工程黄 + 深色压条） */}
      {[
        [0, -SITE_D / 2, SITE_W, 0],
        [0, SITE_D / 2, SITE_W, 0],
        [-SITE_W / 2, 0, 0, 0],
        [SITE_W / 2, 0, 0, 0],
      ].map(([fx, fz], i) => {
        const horizontal = i < 2;
        return (
          <group key={`fence-${i}`} position={[fx, 0, fz]}>
            <mesh position={[0, u(0.9), 0]} castShadow>
              <boxGeometry args={[horizontal ? SITE_W : 0.04, u(1.8), horizontal ? 0.04 : SITE_D]} />
              <meshStandardMaterial color={YELLOW} roughness={0.7} />
            </mesh>
            {/* 深色压条 */}
            <mesh position={[0, u(1.72), 0]}>
              <boxGeometry args={[horizontal ? SITE_W : 0.05, u(0.16), horizontal ? 0.05 : SITE_D]} />
              <meshStandardMaterial color={YELLOW_DARK} roughness={0.7} />
            </mesh>
          </group>
        );
      })}
      {/* 塔吊（场内靠后侧） */}
      <group position={[-0.6, 0, -0.4]}>
        {/* 格构塔身 + 3 道横撑 */}
        <mesh position={[0, mastH / 2, 0]} castShadow>
          <boxGeometry args={[0.16, mastH, 0.16]} />
          <meshStandardMaterial color={YELLOW} roughness={0.6} envMapIntensity={0.6} />
        </mesh>
        {[0.3, 0.55, 0.8].map((t, i) => (
          <mesh key={`brace-${i}`} position={[0, mastH * t, 0]}>
            <boxGeometry args={[0.2, 0.03, 0.2]} />
            <meshStandardMaterial color={YELLOW_DARK} roughness={0.6} />
          </mesh>
        ))}
        {/* 操作室（塔顶） */}
        <mesh position={[0.14, mastH + u(0.4), 0]} castShadow>
          <boxGeometry args={[u(0.9), u(0.8), u(0.9)]} />
          <meshStandardMaterial color="#2a4a6e" roughness={0.3} metalness={0.5} envMapIntensity={1.0} />
        </mesh>
        {/* 起重臂（长臂，沿 +x）+ 尖端拉索塔头 */}
        <mesh position={[u(3.6), mastH + u(0.9), 0]} castShadow>
          <boxGeometry args={[u(7.2), 0.1, 0.1]} />
          <meshStandardMaterial color={YELLOW} roughness={0.6} envMapIntensity={0.6} />
        </mesh>
        {/* 平衡臂（短臂，沿 -x）+ 配重块 */}
        <mesh position={[-u(1.1), mastH + u(0.9), 0]} castShadow>
          <boxGeometry args={[u(2.2), 0.1, 0.1]} />
          <meshStandardMaterial color={STEEL} roughness={0.5} metalness={0.5} envMapIntensity={0.8} />
        </mesh>
        <mesh position={[-u(1.9), mastH + u(0.7), 0]} castShadow>
          <boxGeometry args={[u(0.6), u(0.5), u(0.5)]} />
          <meshStandardMaterial color="#8a8d96" roughness={0.7} />
        </mesh>
        {/* 臌塔头 + 前后拉索（细杆近似） */}
        <mesh position={[0, mastH + u(1.6), 0]}>
          <boxGeometry args={[0.05, u(1.4), 0.05]} />
          <meshStandardMaterial color={STEEL} roughness={0.5} metalness={0.5} />
        </mesh>
        <mesh position={[u(2.4), mastH + u(1.35), 0] } rotation={[0, 0, 0.32]}>
          <boxGeometry args={[u(4.6), 0.015, 0.015]} />
          <meshStandardMaterial color={STEEL} roughness={0.5} metalness={0.5} />
        </mesh>
        {/* 吊钩钢缆 + 钩块（臂前段垂下） */}
        <mesh position={[u(4.6), mastH + u(0.25), 0]}>
          <cylinderGeometry args={[0.008, 0.008, u(1.3), 4]} />
          <meshStandardMaterial color="#3a3f47" roughness={0.8} />
        </mesh>
        <mesh position={[u(4.6), mastH - u(0.45), 0]} castShadow>
          <boxGeometry args={[0.06, 0.06, 0.06]} />
          <meshStandardMaterial color="#5a6270" roughness={0.6} metalness={0.5} />
        </mesh>
      </group>
    </group>
  );
}

export default ConstructionSite;
