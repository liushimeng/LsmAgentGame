/**
 * 变电站（18-AA · §5.2 Substation）
 *   围栏 + 变压器 2 台（散热片）+ 电线杆 2 根 + 架空线。布点 (-18.5, -11)。
 */
import { u } from '../cityScale';

const FENCE = '#6b7280';
const XFMR = '#4a5568';
const POLE = '#7a8290';
const WIRE = '#3a414c';

export function Substation() {
  // 4 段围栏（矩形，x∈[-2,2], z∈[-1.5,1.5]）
  return (
    <group position={[-18.5, 0, -11]}>
      {/* 围栏 4 段 */}
      {[
        { pos: [0, u(0.6), u(1.6)], size: [u(4), u(1.2), u(0.05)] },
        { pos: [0, u(0.6), -u(1.6)], size: [u(4), u(1.2), u(0.05)] },
        { pos: [-u(2), u(0.6), 0], size: [u(0.05), u(1.2), u(3.2)] },
        { pos: [u(2), u(0.6), 0], size: [u(0.05), u(1.2), u(3.2)] },
      ].map((s, i) => (
        <mesh key={`fence-${i}`} position={s.pos as any}>
          <boxGeometry args={s.size as any} />
          <meshStandardMaterial color={FENCE} metalness={0.5} roughness={0.5} />
        </mesh>
      ))}
      {/* 围栏顶部横梁 */}
      {[-u(1.95), u(1.95)].map((x, i) => (
        <mesh key={`beam-${i}`} position={[x, u(1.3), 0]}>
          <boxGeometry args={[u(0.08), u(0.05), u(3.2)]} />
          <meshStandardMaterial color={FENCE} metalness={0.5} roughness={0.5} />
        </mesh>
      ))}
      {/* 变压器 2 台：圆筒 + 散热片 */}
      {[-u(0.7), u(0.7)].map((x, i) => (
        <group key={`xfmr-${i}`} position={[x, 0, 0]}>
          <mesh position={[0, u(0.6), 0]} castShadow>
            <cylinderGeometry args={[u(0.5), u(0.5), u(1.2), 16]} />
            <meshStandardMaterial color={XFMR} metalness={0.5} roughness={0.6} />
          </mesh>
          {/* 顶部套管 */}
          <mesh position={[0, u(1.4), 0]} castShadow>
            <cylinderGeometry args={[u(0.12), u(0.12), u(0.4), 8]} />
            <meshStandardMaterial color="#3a414c" metalness={0.4} roughness={0.5} />
          </mesh>
        </group>
      ))}
      {/* 电线杆 2 根（围栏外侧） */}
      {[-u(2.6), u(2.6)].map((x, i) => (
        <mesh key={`pole-${i}`} position={[x, u(4), -u(2.2)]}>
          <cylinderGeometry args={[u(0.1), u(0.12), u(8), 6]} />
          <meshStandardMaterial color={POLE} roughness={0.7} />
        </mesh>
      ))}
      {/* 架空线（细圆柱，跨两杆） */}
      <mesh position={[0, u(8), -u(2.2)]}>
        <cylinderGeometry args={[u(0.02), u(0.02), u(5.4), 6]} />
        <meshStandardMaterial color={WIRE} metalness={0.6} roughness={0.5} />
      </mesh>
      <mesh position={[0, u(8), -u(2.2)]} rotation={[0, 0, Math.PI / 2]}>
        <cylinderGeometry args={[u(0.02), u(0.02), u(5.4), 6]} />
        <meshStandardMaterial color={WIRE} metalness={0.6} roughness={0.5} />
      </mesh>
    </group>
  );
}