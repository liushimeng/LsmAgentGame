// Game scene — empty R3F canvas host. Real game logic is out of scope for
// the initial scaffold; this proves the canvas mounts cleanly with WS-driven
// state ready to feed into entities.
//
// 批次 22（2.5D → 3D 世界升级）：裸 <Canvas> 切换为 engine3d <EngineCanvas>，
// 渲染器初始化（ACESFilmic / PCFSoft / debug info）统一由引擎层负责。

import { OrbitControls, Environment } from '@react-three/drei';
import { EngineCanvas } from '@/engine3d';
import { useWebSocket } from '@/hooks/useWebSocket';

export function GameScene() {
  const env = useWebSocket();
  return (
    <div className="canvas-host">
      <EngineCanvas camera={{ position: [0, 2, 5], fov: 60 }}>
        <ambientLight intensity={0.4} />
        <directionalLight position={[3, 5, 3]} intensity={1.1} castShadow />
        <Environment preset="city" />
        <mesh receiveShadow rotation={[-Math.PI / 2, 0, 0]} position={[0, 0, 0]}>
          <planeGeometry args={[30, 30]} />
          <meshStandardMaterial color="#1f2933" />
        </mesh>
        <mesh castShadow position={[0, 0.5, 0]}>
          <sphereGeometry args={[0.5, 32, 32]} />
          <meshStandardMaterial color="#3fb950" />
        </mesh>
        <OrbitControls />
      </EngineCanvas>
      <div style={{ position: 'fixed', bottom: 12, left: 12, color: 'var(--muted)', fontSize: 12 }}>
        WS last: {env ? env.type : 'connecting…'}
      </div>
    </div>
  );
}
