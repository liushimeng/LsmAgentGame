/**
 * 北环轻轨（18-AA · §5.2 RailViaduct）
 *   沿 z=+46 横向架设，箱梁 + 13 桥墩 + 2 车站（西站/北站）+ 停靠列车 3 节。
 *   批次 20（文档 1 §3.4）：z=30 → 46 北迁 —— 32 区后 wetland(-12,40)/
 *   sports(10,40)/university(-32,34)/fin_sub(24,30)/bay(40,28) 底板半径 4
 *   与旧 z=30 走廊交叠；z=46 位于 fin_sub/sports 北侧、|z|≤48 底板带内
 *   且 WORLD_SIZE=120 边缘留 ≥14 单位缓冲；进出站方向不变。
 *   布点 x∈[-30,30]（已预检：z=46 不与任何城区底板相交）。
 */
import { useCivicPBR } from './CivicPBR';
import { u } from '../cityScale';

const STATION_SILVER = '#c0c5cd';
const TRAIN_BODY = '#b8c5d5';
const TRAIN_DARK = '#3a4250';

export function RailViaduct() {
  const concrete = useCivicPBR('concrete', [0.8, 0.8]);
  const c = concrete.matProps;
  // 13 桥墩 @ 每 5 世界单位一根
  const piers = [];
  for (let i = 0; i < 13; i++) {
    piers.push(-30 + i * 5);
  }
  return (
    <group>
      {/* 箱梁：60m × 0.16m × 4.5m @ y=u(9) */}
      <mesh castShadow position={[0, u(9), 46]}>
        <boxGeometry args={[60, u(1.6), u(4.5)]} />
        <meshStandardMaterial color="#7a8290" {...c} roughness={c.roughnessMap ? undefined : 0.85} />
      </mesh>
      {/* 桥墩 13 根 */}
      {piers.map((x) => (
        <mesh key={`pier-${x}`} castShadow receiveShadow position={[x, u(4.5), 46]}>
          <boxGeometry args={[u(1.8), u(9), u(1.8)]} />
          <meshStandardMaterial color="#8a8f98" {...c} roughness={c.roughnessMap ? undefined : 0.85} />
        </mesh>
      ))}
      {/* 栏杆：箱梁两侧 `mergeBoxes` 等价（此处每 5m 一根，13+1=14 根 + 2 条扶手，共 16 mesh） */}
      {[0, 1].map((side) =>
        [-28, -23, -18, -13, -8, -3, 2, 7, 12, 17, 22, 27].map((x, i) => (
          <mesh
            key={`rail-${side}-${i}`}
            position={[x, u(10.6), 46 + side * (u(2.3))]}
          >
            <cylinderGeometry args={[u(0.04), u(0.04), u(1.2), 6]} />
            <meshStandardMaterial color="#7a8290" metalness={0.4} roughness={0.5} />
          </mesh>
        )),
      )}
      {/* 车站 2 座：西站 (-18,46)、北站 (0,46) */}
      <Station x={-18} z={46} side={'W'} />
      <Station x={0} z={46} side={'N'} />
      {/* 列车 3 节：停靠北站附近 */}
      {[-1, 0, 1].map((i) => (
        <mesh
          key={`car-${i}`}
          castShadow
          position={[4 + i * u(23), u(11.5), 46]}
          rotation={[0, Math.PI, 0]}
        >
          <boxGeometry args={[u(22), u(3.2), u(3.2)]} />
          <meshStandardMaterial color={TRAIN_BODY} metalness={0.6} roughness={0.3} />
        </mesh>
      ))}
      {/* 列车窗带（半透明深色，3 节） */}
      {[-1, 0, 1].map((i) => (
        <mesh key={`window-${i}`} position={[4 + i * u(23), u(12), 46]}>
          <boxGeometry args={[u(20), u(1.0), u(3.4)]} />
          <meshStandardMaterial color={TRAIN_DARK} transparent opacity={0.65} roughness={0.1} metalness={0.4} />
        </mesh>
      ))}
    </group>
  );
}

function Station({ x, z }: { x: number; z: number; side: 'W' | 'N' }) {
  return (
    <group position={[x, 0, z]}>
      {/* 月台：u(80) × u(6) @ y=u(10) */}
      <mesh position={[0, u(10), 0]} castShadow receiveShadow>
        <boxGeometry args={[u(80), u(0.5), u(6)]} />
        <meshStandardMaterial color={STATION_SILVER} roughness={0.5} metalness={0.4} />
      </mesh>
      {/* 雨棚：u(80) × u(0.5) × u(8) @ y=u(13) */}
      <mesh position={[0, u(13), 0]} castShadow>
        <boxGeometry args={[u(80), u(0.5), u(8)]} />
        <meshStandardMaterial color="#5a6270" roughness={0.6} metalness={0.3} />
      </mesh>
      {/* 雨棚支柱（4 根） */}
      {[-u(35), -u(10), u(10), u(35)].map((dx, i) => (
        <mesh key={`pillar-${i}`} position={[dx, u(11.5), u(2)]}>
          <cylinderGeometry args={[u(0.15), u(0.15), u(3), 6]} />
          <meshStandardMaterial color="#7a8290" metalness={0.5} roughness={0.5} />
        </mesh>
      ))}
      {/* 站牌柱 + 牌 */}
      <mesh position={[-u(45), u(11.5), u(2)]}>
        <cylinderGeometry args={[u(0.08), u(0.08), u(3), 6]} />
        <meshStandardMaterial color="#5a6270" metalness={0.4} roughness={0.5} />
      </mesh>
      <mesh position={[-u(45), u(13), u(2.2)]}>
        <boxGeometry args={[u(4), u(1.4), u(0.1)]} />
        <meshStandardMaterial color="#d4a017" emissive="#d4a017" emissiveIntensity={0.4} />
      </mesh>
      {/* 站名文字（emoji 占位，避免新 i18n 键） */}
      <mesh position={[-u(45), u(13), u(2.3)]}>
        <boxGeometry args={[u(3.8), u(1.2), u(0.02)]} />
        <meshBasicMaterial color="#161d28" />
      </mesh>
    </group>
  );
}