/**
 * 物流港码头（18-AA · §5.2 PortTerminal）
 *   局部坐标：岸线沿 x，海侧朝 −z。岸吊 4 腿门架 + 横梁 + 悬臂 + 小车 + 吊具 + 钢缆；
 *   集装箱 3×3×2 共 12 只（4 色确定性）；系缆桩 4 只；码头面 `pbr/synth/concrete_n/r`。
 *   布点 (-30, -9.5)（02 §5.5 已预检通过）。
 */
import { useMemo } from 'react';
import { u } from '../cityScale';
import { hashStr, mulberry32 } from './rand';
import { useCivicPBR } from './CivicPBR';

const CONCRETE_COLOR = '#6b7280';
const STEEL_COLOR = '#8a8d96';
const STEEL_DARK = '#5a6270';
const CONTAINER_COLORS = ['#c0392b', '#2471a3', '#e67e22', '#27ae60'];

export function PortTerminal() {
  const concrete = useCivicPBR('concrete', [0.8, 0.8]);
  const metal = useCivicPBR('metal_deck', [0.9, 0.9]);

  // 集装箱确定性 4 色轮转
  const containers = useMemo(() => {
    const rnd = mulberry32(hashStr('port:containers'));
    const out: Array<{ x: number; z: number; layer: 0 | 1; color: string }> = [];
    for (let row = 0; row < 2; row++) {
      for (let cx = 0; cx < 3; cx++) {
        for (let cz = 0; cz < 2; cz++) {
          out.push({
            x: -u(11) + cx * u(6),
            z: -u(2.5) + cz * u(2.5),
            layer: row as 0 | 1,
            color: CONTAINER_COLORS[Math.floor(rnd() * CONTAINER_COLORS.length)],
          });
        }
      }
    }
    return out;
  }, []);

  const concreteProps = concrete.matProps;
  const metalProps = metal.matProps;

  return (
    <group position={[-30, 0, -9.5]} rotation={[0, Math.PI, 0]}>
      {/* 码头面：80m × 25m，`u(80) × u(25)`（契约 02 §5.2） */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.03, 0]} receiveShadow>
        <planeGeometry args={[u(80), u(25)]} />
        <meshStandardMaterial
          color={CONCRETE_COLOR}
          {...concreteProps}
          roughness={concreteProps.roughnessMap ? undefined : 0.85}
          metalness={0.05}
        />
      </mesh>
      {/* 岸吊门架：4 腿 + 横梁 + 悬臂 + 小车 + 吊具 */}
      {/* 腿：4 根 box 0.5×22m×0.5m @ x=±9m, z=±4m，y=11m（顶高 u(22)） */}
      {[-1, 1].map((sx) =>
        [-1, 1].map((sz) => (
          <mesh key={`leg-${sx}-${sz}`} castShadow position={[sx * u(9), u(11), sz * u(4)]}>
            <boxGeometry args={[u(0.5), u(22), u(0.5)]} />
            <meshStandardMaterial color={STEEL_COLOR} {...metalProps} metalness={0.6} roughness={0.4} />
          </mesh>
        )),
      )}
      {/* 横梁：u(22) 跨度 × u(1.2) 高 × u(1.0) 深，y=22.5 */}
      <mesh castShadow position={[0, u(22.5), 0]}>
        <boxGeometry args={[u(22), u(1.2), u(1.0)]} />
        <meshStandardMaterial color={STEEL_DARK} {...metalProps} metalness={0.6} roughness={0.5} />
      </mesh>
      {/* 海侧悬臂：伸向 +z（局部坐标，岸线朝 -z） */}
      <mesh castShadow position={[0, u(22.5), u(5.5)]}>
        <boxGeometry args={[u(10), u(0.8), u(0.8)]} />
        <meshStandardMaterial color={STEEL_COLOR} {...metalProps} metalness={0.6} roughness={0.4} />
      </mesh>
      {/* 小车：在悬臂上滑行（u(1.6) × u(1.0) × u(1.4)） */}
      <mesh castShadow position={[u(3), u(22.5), u(5.5)]}>
        <boxGeometry args={[u(1.6), u(1.0), u(1.4)]} />
        <meshStandardMaterial color={STEEL_DARK} metalness={0.7} roughness={0.4} />
      </mesh>
      {/* 钢缆 + 吊具：从小车下垂 */}
      <mesh position={[u(3), u(17.5), u(5.5)]}>
        <cylinderGeometry args={[0.006, 0.006, u(8), 6]} />
        <meshStandardMaterial color="#3a3f4a" />
      </mesh>
      <mesh castShadow position={[u(3), u(13.5), u(5.5)]}>
        <boxGeometry args={[u(2.4), u(0.35), u(1.6)]} />
        <meshStandardMaterial color="#c0392b" {...metalProps} roughness={0.7} />
      </mesh>
      {/* 集装箱堆：3×3 平铺 + 2 层（仅在岸侧 +z 端；尺寸 6×2.6×2.4） */}
      {containers.map((c, i) => (
        <mesh
          key={`box-${i}`}
          castShadow
          position={[c.x, 0.05 + (c.layer ? u(2.8) : u(1.4)), c.z]}
        >
          <boxGeometry args={[u(6), u(2.6), u(2.4)]} />
          <meshStandardMaterial color={c.color} {...metalProps} roughness={0.55} metalness={0.25} />
        </mesh>
      ))}
      {/* 系缆桩 4 只：沿岸每 u(15) 一只 */}
      {[-1.5, -0.5, 0.5, 1.5].map((cx, i) => (
        <mesh key={`mooring-${i}`} castShadow position={[cx * u(15), u(0.25), -u(11.5)]}>
          <cylinderGeometry args={[u(0.15), u(0.15), u(0.5), 8]} />
          <meshStandardMaterial color={STEEL_DARK} metalness={0.5} roughness={0.6} />
        </mesh>
      ))}
    </group>
  );
}