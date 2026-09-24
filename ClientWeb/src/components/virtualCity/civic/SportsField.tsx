/**
 * 学校操场（18-AA · §5.2 SportsField）
 *   椭圆跑道（赭红 + 白分道线）+ 内场绿茵 + 足球门 ×2 + 看台 3 级台阶 + 篮球场 1 块。
 *   布点 (-16.5, 22)，占地 7×4.5 世界单位（02 §5.5 已预检）。
 */
import { useCivicPBR } from './CivicPBR';
import { u } from '../cityScale';

const TRACK_RED = '#a85040';
const LINE_WHITE = '#e8e8e8';
const FIELD_GREEN = '#5d9a4e';
const GOAL_WHITE = '#d8d4ca';

export function SportsField() {
  const grass = useCivicPBR('foliage', [1.0, 1.0]);
  const concrete = useCivicPBR('concrete', [0.7, 0.7]);
  const g = grass.matProps;
  const c = concrete.matProps;
  // 椭圆跑道：torusGeometry（圆环），压扁成椭圆（rotation.x = π/2）
  // 半径 R = u(28) (28m)，管半径 r = u(4) (4m 跑道宽度) —— 实测标尺为「中小学操场」
  return (
    <group position={[-16.5, 0, 22]}>
      {/* 内场草茵 */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.015, 0]} receiveShadow>
        <circleGeometry args={[u(24), 32]} />
        <meshStandardMaterial
          color={FIELD_GREEN}
          {...g}
          roughness={g.roughnessMap ? undefined : 0.95}
        />
      </mesh>
      {/* 跑道椭圆环（torusGeometry 在水平面） */}
      <mesh rotation={[Math.PI / 2, 0, 0]} position={[0, 0.012, 0]} receiveShadow>
        <torusGeometry args={[u(28), u(4), 8, 64]} />
        <meshStandardMaterial color={TRACK_RED} roughness={0.85} />
      </mesh>
      {/* 跑道分道白线（4 条）：用 ringGeometry 切片 */}
      {[u(27), u(27.5), u(28.5), u(29)].map((r, i) => (
        <mesh key={`lane-${i}`} rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.013, 0]}>
          <ringGeometry args={[r - 0.05, r + 0.05, 64]} />
          <meshBasicMaterial color={LINE_WHITE} side={THREE.DoubleSide} transparent opacity={0.85} />
        </mesh>
      ))}
      {/* 足球门 ×2：长边中点（z=±28） */}
      {[-1, 1].map((s) => (
        <group key={`goal-${s}`} position={[0, 0, s * u(28)]}>
          {/* 横梁 */}
          <mesh position={[0, u(1.2), 0]}>
            <boxGeometry args={[u(7.32), u(0.08), u(0.08)]} />
            <meshStandardMaterial color={GOAL_WHITE} />
          </mesh>
          {/* 左立柱 */}
          <mesh position={[-u(3.66), u(0.6), 0]}>
            <boxGeometry args={[u(0.08), u(1.2), u(0.08)]} />
            <meshStandardMaterial color={GOAL_WHITE} />
          </mesh>
          {/* 右立柱 */}
          <mesh position={[u(3.66), u(0.6), 0]}>
            <boxGeometry args={[u(0.08), u(1.2), u(0.08)]} />
            <meshStandardMaterial color={GOAL_WHITE} />
          </mesh>
        </group>
      ))}
      {/* 看台 3 级台阶：长边一侧（z=+30） */}
      {[0, 1, 2].map((i) => (
        <mesh
          key={`stand-${i}`}
          position={[0, 0.05 + i * u(0.6), u(33 + i * 0.6)]}
          castShadow
          receiveShadow
        >
          <boxGeometry args={[u(40), u(0.6), u(1.2)]} />
          <meshStandardMaterial color="#a8a4a0" {...c} roughness={c.roughnessMap ? undefined : 0.9} />
        </mesh>
      ))}
      {/* 篮球场：短边内侧（z=-30），标线 + 一对篮架 */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.014, -u(33)]} receiveShadow>
        <planeGeometry args={[u(15), u(7)]} />
        <meshStandardMaterial color="#c4a945" roughness={0.85} />
      </mesh>
      <mesh position={[-u(6.5), u(1.5), -u(33)]} castShadow>
        <cylinderGeometry args={[u(0.05), u(0.06), u(3), 6]} />
        <meshStandardMaterial color={GOAL_WHITE} />
      </mesh>
      <mesh position={[-u(6.5), u(3), -u(33)]} castShadow>
        <boxGeometry args={[u(1.8), u(0.9), u(0.05)]} />
        <meshStandardMaterial color={GOAL_WHITE} />
      </mesh>
    </group>
  );
}

// 需 THREE 引用
import * as THREE from 'three';