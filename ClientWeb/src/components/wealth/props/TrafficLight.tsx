/**
 * TrafficLight — 路口红绿灯（v2.13 阶段 D，13-3D城市渲染优化 02-架构 §3.2）：
 *
 * 纯几何（无贴图依赖）：立杆 + 横臂 + 三色灯头。
 *   - 立杆：高 u(6.5)（6.5m 城市道路标杆），半径 u(0.15)，深灰。
 *   - 横臂：长 u(4)，沿 local +x 向道路方向伸出（由 rotation 对准）。
 *   - 灯头：u(1.2)×u(2.4)×u(0.6) 近黑箱体，挂于横臂末端。
 *   - 三灯：红上 / 黄中 / 绿下，半径 u(0.28)；绿灯常亮（emissiveIntensity 1.2），
 *     红/黄暗态 0.15（静态交通流，不做相位切换——见 01 文档 §5 非目标）。
 *
 * 米制尺寸统一经 cityScale.u() 换算（高度系统唯一事实来源）。
 */

import { u } from '../cityScale';

interface Props {
  x: number;
  z: number;
  /** 绕 Y 旋转（local +x 横臂指向道路；布点层传 angle + π 面向来车）。 */
  rotation: number;
}

const POLE_COLOR = '#3a3f46';
const HEAD_COLOR = '#17191d';

/** 灯头三灯（红上绿下；亮灯/暗态 emissive 分档）。 */
const BULBS: Array<{ color: string; yOff: number; on: boolean }> = [
  { color: '#e5484d', yOff: u(0.8), on: false },  // 红（上）
  { color: '#f5b83d', yOff: 0, on: false },        // 黄（中）
  { color: '#46a758', yOff: -u(0.8), on: true },   // 绿（下，常亮）
];

export function TrafficLight({ x, z, rotation }: Props) {
  const poleH = u(6.5);
  const armLen = u(4);
  const headW = u(1.2);
  const headH = u(2.4);
  const headD = u(0.6);
  // 灯头悬挂高度：顶部贴横臂下沿
  const headY = poleH - u(0.3) - headH / 2;

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 立杆 */}
      <mesh position={[0, poleH / 2, 0]}>
        <cylinderGeometry args={[u(0.15), u(0.15), poleH, 8]} />
        <meshStandardMaterial color={POLE_COLOR} roughness={0.6} metalness={0.4} />
      </mesh>
      {/* 横臂（local +x 伸向道路） */}
      <mesh position={[armLen / 2, poleH - u(0.3), 0]}>
        <boxGeometry args={[armLen, u(0.25), u(0.25)]} />
        <meshStandardMaterial color={POLE_COLOR} roughness={0.6} metalness={0.4} />
      </mesh>
      {/* 灯头箱体（横臂末端） */}
      <mesh position={[armLen - headW / 2, headY, 0]}>
        <boxGeometry args={[headW, headH, headD]} />
        <meshStandardMaterial color={HEAD_COLOR} roughness={0.7} metalness={0.2} />
      </mesh>
      {/* 三色灯（微凸出灯头前面 local +z） */}
      {BULBS.map((b) => (
        <mesh
          key={b.color}
          position={[armLen - headW / 2, headY + b.yOff, headD / 2 + u(0.1)]}
        >
          <sphereGeometry args={[u(0.28), 12, 8]} />
          <meshStandardMaterial
            color={b.color}
            emissive={b.color}
            emissiveIntensity={b.on ? 1.2 : 0.15}
            roughness={0.35}
          />
        </mesh>
      ))}
    </group>
  );
}
