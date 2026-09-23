/**
 * 警局（18-AA · §5.2 PoliceStation）
 *   蓝白外墙 + 门厅雨棚 + 警灯柱 + 巡逻车 1 辆。布点 (-5.8, -8.5)。
 */
import { u } from '../cityScale';

const BLUE = '#2a4e7a';
const WHITE = '#e8e3dc';
const CAR_BLACK = '#2a2e36';

export function PoliceStation() {
  return (
    <group position={[-5.8, 0, -8.5]}>
      {/* 主屋：u(12) × u(6) × u(7) */}
      <mesh position={[0, u(3), 0]} castShadow receiveShadow>
        <boxGeometry args={[u(12), u(6), u(7)]} />
        <meshStandardMaterial color={BLUE} roughness={0.85} />
      </mesh>
      {/* 顶部白色腰线 */}
      <mesh position={[0, u(5.4), u(3.55)]}>
        <boxGeometry args={[u(12.1), u(0.6), u(0.05)]} />
        <meshStandardMaterial color={WHITE} />
      </mesh>
      {/* 门厅雨棚 */}
      <mesh position={[0, u(5.4), u(4.4)]} castShadow>
        <boxGeometry args={[u(3.5), u(0.12), u(1.2)]} />
        <meshStandardMaterial color="#4a6b95" />
      </mesh>
      <mesh position={[0, u(5.4), u(4.6)]} castShadow>
        <boxGeometry args={[u(3.5), u(0.15), u(1.2)]} />
        <meshStandardMaterial color={WHITE} />
      </mesh>
      {/* 门厅立柱 ×2 */}
      {[-u(1.4), u(1.4)].map((dx, i) => (
        <mesh key={`col-${i}`} position={[dx, u(3.5), u(4.6)]}>
          <cylinderGeometry args={[u(0.1), u(0.1), u(7), 6]} />
          <meshStandardMaterial color={WHITE} />
        </mesh>
      ))}
      {/* 警灯柱 + 警灯（红蓝闪烁感，单色） */}
      <mesh position={[-u(5), u(2.5), u(4.5)]}>
        <cylinderGeometry args={[u(0.08), u(0.08), u(5), 6]} />
        <meshStandardMaterial color="#5a6270" metalness={0.5} roughness={0.5} />
      </mesh>
      <mesh position={[-u(5), u(5.2), u(4.5)]}>
        <boxGeometry args={[u(0.6), u(0.2), u(0.3)]} />
        <meshStandardMaterial color="#ff3b30" emissive="#ff3b30" emissiveIntensity={0.7} />
      </mesh>
      <mesh position={[-u(5), u(5.5), u(4.5)]}>
        <boxGeometry args={[u(0.6), u(0.2), u(0.3)]} />
        <meshStandardMaterial color="#5a86b8" emissive="#5a86b8" emissiveIntensity={0.7} />
      </mesh>
      {/* 巡逻车（黑色蓝条 box 简化） */}
      <mesh position={[u(4), u(0.6), u(4.5)]} castShadow>
        <boxGeometry args={[u(2.2), u(1), u(0.95)]} />
        <meshStandardMaterial color={CAR_BLACK} roughness={0.4} metalness={0.4} />
      </mesh>
      <mesh position={[u(4), u(1.4), u(4.5)]} castShadow>
        <boxGeometry args={[u(1.3), u(0.7), u(0.9)]} />
        <meshStandardMaterial color={CAR_BLACK} roughness={0.4} metalness={0.4} />
      </mesh>
      <mesh position={[u(3.2), u(0.7), u(4.5)]}>
        <boxGeometry args={[u(0.5), u(0.15), u(0.9)]} />
        <meshStandardMaterial color="#5a86b8" />
      </mesh>
    </group>
  );
}