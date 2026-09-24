/**
 * 加油站（18-AA · §5.2 GasStation）
 *   大挑檐雨棚 + 4 柱 + 4 号加油机 + 便利店小屋 + 高杆招牌。布点 (5.8, 6.7)。
 */
import { u } from '../cityScale';

const CANOPY_RED = '#c0392b';
const PUMP_GREY = '#5a6270';
const SHOP_WHITE = '#e8e8e8';
const SIGN_GLOW = '#ffe066';

export function GasStation() {
  return (
    <group position={[5.8, 0, 6.7]}>
      {/* 雨棚：u(22) × u(14) 平面，y=u(5) */}
      <mesh position={[0, u(5), 0]} castShadow>
        <boxGeometry args={[u(22), u(0.15), u(14)]} />
        <meshStandardMaterial color={CANOPY_RED} roughness={0.7} metalness={0.2} />
      </mesh>
      {/* 雨棚底沿浅色边条 */}
      <mesh position={[0, u(4.92), 0]}>
        <boxGeometry args={[u(22.1), u(0.06), u(14.1)]} />
        <meshStandardMaterial color="#e8e3dc" />
      </mesh>
      {/* 雨棚支柱 ×4 */}
      {[-1, 1].map((sx) =>
        [-1, 1].map((sz) => (
          <mesh key={`col-${sx}-${sz}`} position={[sx * u(10), u(2.5), sz * u(6)]} castShadow>
            <cylinderGeometry args={[u(0.18), u(0.18), u(5), 8]} />
            <meshStandardMaterial color="#7a8290" metalness={0.4} roughness={0.5} />
          </mesh>
        )),
      )}
      {/* 4 号加油机（每个独立柱+机+枪） */}
      {[-1, 1].map((sx) =>
        [-1, 1].map((sz) => (
          <group key={`pump-${sx}-${sz}`} position={[sx * u(5), 0, sz * u(4.5)]}>
            <mesh position={[0, u(0.8), 0]} castShadow>
              <boxGeometry args={[u(0.9), u(1.6), u(0.5)]} />
              <meshStandardMaterial color={PUMP_GREY} metalness={0.4} roughness={0.5} />
            </mesh>
            <mesh position={[0, u(2), 0]}>
              <boxGeometry args={[u(0.7), u(0.3), u(0.4)]} />
              <meshStandardMaterial color="#161d28" />
            </mesh>
          </group>
        )),
      )}
      {/* 便利店小屋（雨棚外一侧） */}
      <mesh position={[0, u(1.5), u(11)]} castShadow receiveShadow>
        <boxGeometry args={[u(6), u(3), u(3)]} />
        <meshStandardMaterial color={SHOP_WHITE} roughness={0.85} />
      </mesh>
      <mesh position={[0, u(3.1), u(11)]} castShadow>
        <boxGeometry args={[u(6.1), u(0.15), u(3.1)]} />
        <meshStandardMaterial color="#a85040" />
      </mesh>
      {/* 高杆招牌（h=8m） */}
      <mesh position={[u(13), u(4), 0]}>
        <cylinderGeometry args={[u(0.15), u(0.15), u(8), 6]} />
        <meshStandardMaterial color="#7a8290" metalness={0.5} roughness={0.5} />
      </mesh>
      <mesh position={[u(13), u(8), 0]} castShadow>
        <boxGeometry args={[u(3), u(2.5), u(0.15)]} />
        <meshStandardMaterial color={SIGN_GLOW} emissive={SIGN_GLOW} emissiveIntensity={0.6} />
      </mesh>
    </group>
  );
}