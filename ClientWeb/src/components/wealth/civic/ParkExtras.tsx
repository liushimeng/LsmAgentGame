/**
 * 公园补全（18-AA · §5.2 ParkExtras）
 *   六角凉亭 + 滑梯 + 秋千架 + 公厕小屋。布点：凉亭 (-2.2, -24.5)；游乐 (2.2, -24.5)；公厕 (-2.5, -19.8)。
 */
import { u } from '../cityScale';

const WOOD = '#8a5a44';
const WOOD_LIGHT = '#a87055';
const METAL = '#5a6270';
const TILE = '#d8d4ca';

export function ParkExtras() {
  return (
    <group>
      <Pavilion x={-2.2} z={-24.5} />
      <Playground x={2.2} z={-24.5} />
      <Restroom x={-2.5} z={-19.8} />
    </group>
  );
}

function Pavilion({ x, z }: { x: number; z: number }) {
  // 六角凉亭：6 立柱 + 圆锥尖顶
  return (
    <group position={[x, 0, z]}>
      {Array.from({ length: 6 }).map((_, i) => {
        const a = (i * Math.PI) / 3;
        return (
          <mesh
            key={`col-${i}`}
            position={[Math.cos(a) * u(1.6), u(1.2), Math.sin(a) * u(1.6)]}
            castShadow
          >
            <cylinderGeometry args={[u(0.08), u(0.08), u(2.4), 6]} />
            <meshStandardMaterial color={WOOD} roughness={0.85} />
          </mesh>
        );
      })}
      <mesh position={[0, u(2.6), 0]} castShadow>
        <coneGeometry args={[u(2.2), u(1.4), 6]} />
        <meshStandardMaterial color={WOOD_LIGHT} roughness={0.8} />
      </mesh>
      {/* 顶冠球 */}
      <mesh position={[0, u(3.5), 0]} castShadow>
        <sphereGeometry args={[u(0.12), 8, 8]} />
        <meshStandardMaterial color="#3a3f4a" />
      </mesh>
    </group>
  );
}

function Playground({ x, z }: { x: number; z: number }) {
  return (
    <group position={[x, 0, z]}>
      {/* 滑梯：梯 + 斜面 */}
      <mesh position={[-u(0.5), u(1), 0]} castShadow>
        <boxGeometry args={[u(0.4), u(2), u(0.4)]} />
        <meshStandardMaterial color={WOOD} roughness={0.85} />
      </mesh>
      <mesh
        position={[-u(0.3), u(1), u(1.2)]}
        rotation={[-0.5, 0, 0]}
        castShadow
      >
        <boxGeometry args={[u(0.6), u(0.06), u(2.5)]} />
        <meshStandardMaterial color="#c0392b" roughness={0.6} />
      </mesh>
      {/* 秋千架：2 柱 + 横梁 + 2 椅 */}
      {[-u(1.5), u(1.5)].map((dx, i) => (
        <mesh key={`pole-${i}`} position={[dx, u(1.5), 0]} castShadow>
          <cylinderGeometry args={[u(0.08), u(0.08), u(3), 6]} />
          <meshStandardMaterial color={METAL} metalness={0.6} roughness={0.5} />
        </mesh>
      ))}
      <mesh position={[0, u(3), 0]} rotation={[0, 0, Math.PI / 2]} castShadow>
        <cylinderGeometry args={[u(0.05), u(0.05), u(3.2), 6]} />
        <meshStandardMaterial color={METAL} metalness={0.6} roughness={0.5} />
      </mesh>
      {[-u(0.8), u(0.8)].map((dx, i) => (
        <group key={`swing-${i}`}>
          <mesh position={[dx, u(3), 0]}>
            <cylinderGeometry args={[u(0.02), u(0.02), u(1.4), 6]} />
            <meshStandardMaterial color={METAL} />
          </mesh>
          <mesh position={[dx, u(2.3), 0]} castShadow>
            <boxGeometry args={[u(0.6), u(0.1), u(0.3)]} />
            <meshStandardMaterial color={WOOD} />
          </mesh>
        </group>
      ))}
    </group>
  );
}

function Restroom({ x, z }: { x: number; z: number }) {
  return (
    <group position={[x, 0, z]}>
      <mesh position={[0, u(1.4), 0]} castShadow receiveShadow>
        <boxGeometry args={[u(3), u(2.8), u(2)]} />
        <meshStandardMaterial color={TILE} roughness={0.85} />
      </mesh>
      <mesh position={[0, u(2.9), 0]} castShadow>
        <boxGeometry args={[u(3.2), u(0.15), u(2.2)]} />
        <meshStandardMaterial color={WOOD} roughness={0.8} />
      </mesh>
      {/* 门 */}
      <mesh position={[0, u(1), u(1.01)]}>
        <planeGeometry args={[u(0.8), u(1.8)]} />
        <meshStandardMaterial color="#3a3f4a" />
      </mesh>
      {/* 标牌 */}
      <mesh position={[u(1.4), u(2.5), u(1.01)]} castShadow>
        <boxGeometry args={[u(0.6), u(0.3), u(0.05)]} />
        <meshStandardMaterial color="#5a6270" emissive="#5a6270" emissiveIntensity={0.3} />
      </mesh>
    </group>
  );
}