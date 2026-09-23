/**
 * 路口信号灯（18-AA · §5.2 IntersectionSignals）
 *   环路 × 12 条放射干道交点各 2 杆。复用 props/TrafficLight。
 *   计算交点：每条 len>12 的 main 干道（center → origin），交点在环路半径 5.6 上。
 *   干道 angle = atan2(dx, dz)，交点 = direction * 5.6。
 *   每处 2 杆：沿干道 ±(路宽/2 + 0.3) 偏移，朝向来车。
 */
import { useMemo } from 'react';
import { TrafficLight } from '../props/TrafficLight';
import { WEALTH_DISTRICTS } from '@/types/wealth';

const RING_RADIUS = 5.6;

export function IntersectionSignals() {
  const junctions = useMemo(() => {
    const out: Array<{ x: number; z: number; rot: number }[]> = [];
    for (const d of WEALTH_DISTRICTS) {
      if (d.id === 'finance') continue;
      const dx = -d.x;
      const dz = -d.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len <= 12) continue;
      // 交点 = origin + direction * RING_RADIUS
      void 0;
      const jx = 0 + (dx / len) * RING_RADIUS;
      const jz = 0 + (dz / len) * RING_RADIUS;
      // 干道单位方向
      const ux = dx / len;
      const uz = dz / len;
      // 干道垂直方向（左侧）
      const vx = -uz;
      const vz = ux;
      const offset = 0.7 + 0.3; // 路半宽 0.7 + 0.3 余量
      // 2 杆（对角偏移），朝向来车（朝向 = 干道中心 → 交点）
      // 实际来车方向 = 干道 from-end (城区侧) → 交点 (环路侧)，rotation 朝向来车
      const fromX = d.x;
      const fromZ = d.z;
      const fromRot = Math.atan2(jx - fromX, jz - fromZ);
      out.push([
        { x: jx + vx * offset, z: jz + vz * offset, rot: fromRot },
        { x: jx - vx * offset, z: jz - vz * offset, rot: fromRot + Math.PI },
      ]);
    }
    return out;
  }, []);

  return (
    <group>
      {junctions.map((pair, i) =>
        pair.map((p, j) => (
          <TrafficLight key={`tl-${i}-${j}`} x={p.x} z={p.z} rotation={p.rot} />
        )),
      )}
    </group>
  );
}