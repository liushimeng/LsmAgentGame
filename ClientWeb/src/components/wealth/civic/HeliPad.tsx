/**
 * 医疗直升机坪（18-AA · §5.2 HeliPad）
 *   圆坪（白色 H + 圆环标线 + 4 角边灯 + 风向袋）。布点 (15.5, 22)，直径 u(28)。
 */
import { u } from '../cityScale';

const PAD_WHITE = '#d8d4ca';
const PAD_LINE = '#e8e8e8';
const LIGHT_RED = '#ff3b30';

export function HeliPad() {
  return (
    <group position={[15.5, 0.05, 22]}>
      {/* 圆坪 */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0, 0]} receiveShadow>
        <circleGeometry args={[u(14), 48]} />
        <meshStandardMaterial color={PAD_WHITE} roughness={0.85} />
      </mesh>
      {/* 圆环标线（外圈） */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.001, 0]}>
        <ringGeometry args={[u(12.5), u(13), 48]} />
        <meshBasicMaterial color={PAD_LINE} side={2} transparent opacity={0.9} />
      </mesh>
      {/* H 字符（两竖一横） */}
      {[-u(3.5), u(3.5)].map((x, i) => (
        <mesh key={`hv-${i}`} rotation={[-Math.PI / 2, 0, 0]} position={[x, 0.002, 0]}>
          <planeGeometry args={[u(0.6), u(7)]} />
          <meshBasicMaterial color={PAD_WHITE} side={2} transparent opacity={0.9} />
        </mesh>
      ))}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.002, 0]}>
        <planeGeometry args={[u(7.5), u(0.6)]} />
        <meshBasicMaterial color={PAD_WHITE} side={2} transparent opacity={0.9} />
      </mesh>
      {/* 4 角边灯（emissive 红） */}
      {[
        [u(11), 0, u(11)],
        [-u(11), 0, u(11)],
        [u(11), 0, -u(11)],
        [-u(11), 0, -u(11)],
      ].map(([lx, _ly, lz], i) => (
        <mesh key={`light-${i}`} position={[lx, u(0.15), lz]}>
          <sphereGeometry args={[u(0.25), 8, 8]} />
          <meshStandardMaterial
            color={LIGHT_RED}
            emissive={LIGHT_RED}
            emissiveIntensity={0.8}
          />
        </mesh>
      ))}
      {/* 风向袋：圆柱杆 + 三角锥 */}
      <mesh position={[-u(11.5), u(1.5), -u(11.5)]}>
        <cylinderGeometry args={[u(0.04), u(0.04), u(3), 6]} />
        <meshStandardMaterial color="#7a8290" metalness={0.5} />
      </mesh>
      <mesh position={[-u(11.8), u(2.8), -u(11.5)]} castShadow>
        <coneGeometry args={[u(0.35), u(0.8), 4]} />
        <meshStandardMaterial color="#ff8b1a" roughness={0.7} />
      </mesh>
    </group>
  );
}