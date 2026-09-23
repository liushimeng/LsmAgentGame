/**
 * 消防站（18-AA · §5.2 FireStation）
 *   红白条外墙 + 2 个车库门 + 滑杆塔 + 红色消防车 1 辆。布点 (8, -12)。
 */
import { u } from '../cityScale';

const RED = '#c0392b';
const WHITE = '#e8e3dc';
const DOOR_DARK = '#3a4250';

export function FireStation() {
  return (
    <group position={[8, 0, -12]}>
      {/* 主屋：u(18) × u(7) × u(8) */}
      <mesh position={[0, u(3.5), 0]} castShadow receiveShadow>
        <boxGeometry args={[u(18), u(7), u(8)]} />
        <meshStandardMaterial color={RED} roughness={0.85} />
      </mesh>
      {/* 顶部白色腰线 */}
      <mesh position={[0, u(5.5), u(4.05)]}>
        <boxGeometry args={[u(18.1), u(0.8), u(0.05)]} />
        <meshStandardMaterial color={WHITE} />
      </mesh>
      {/* 车库门 ×2：面向 +z */}
      {[-u(4), u(4)].map((dx, i) => (
        <group key={`door-${i}`} position={[dx, 0, u(4.05)]}>
          <mesh position={[0, u(2.2), 0.01]} castShadow>
            <boxGeometry args={[u(5), u(4.4), u(0.05)]} />
            <meshStandardMaterial color={DOOR_DARK} roughness={0.7} />
          </mesh>
          {/* 4 道横纹装饰 */}
          {[u(1), u(2), u(3), u(3.8)].map((yy, j) => (
            <mesh key={`stripe-${i}-${j}`} position={[0, yy, 0.02]}>
              <boxGeometry args={[u(5), u(0.06), u(0.02)]} />
              <meshStandardMaterial color={RED} />
            </mesh>
          ))}
        </group>
      ))}
      {/* 滑杆塔（独立小塔）：box + 杆 */}
      <mesh position={[u(7), u(4), -u(3.5)]} castShadow>
        <boxGeometry args={[u(0.8), u(8), u(0.8)]} />
        <meshStandardMaterial color={RED} />
      </mesh>
      <mesh position={[u(7), u(4), -u(3.2)]}>
        <cylinderGeometry args={[u(0.06), u(0.06), u(8), 6]} />
        <meshStandardMaterial color="#3a3f4a" metalness={0.6} />
      </mesh>
      {/* 站顶警灯（蓝白闪烁感，单色静态） */}
      <mesh position={[u(7), u(8.3), -u(3.5)]}>
        <sphereGeometry args={[u(0.15), 8, 8]} />
        <meshStandardMaterial color="#5a86b8" emissive="#5a86b8" emissiveIntensity={0.5} />
      </mesh>
      {/* 红色消防车（用 box 简化）—— 实车在 StreetPropsLayer 里 */}
      <mesh position={[-u(6), u(0.7), u(5.5)]} castShadow>
        <boxGeometry args={[u(2.4), u(1.2), u(1)]} />
        <meshStandardMaterial color={RED} roughness={0.5} metalness={0.3} />
      </mesh>
      <mesh position={[-u(6), u(1.6), u(5.5)]} castShadow>
        <boxGeometry args={[u(1.4), u(0.8), u(0.95)]} />
        <meshStandardMaterial color={RED} roughness={0.5} metalness={0.3} />
      </mesh>
    </group>
  );
}