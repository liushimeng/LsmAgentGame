// Game scene — empty R3F canvas host. Real game logic is out of scope for
// the initial scaffold; this proves the canvas mounts cleanly with WS-driven
// state ready to feed into entities.

import { Canvas } from '@react-three/fiber';
import { OrbitControls, Environment } from '@react-three/drei';
import { useWebSocket } from '@/hooks/useWebSocket';

export function GameScene() {
  const env = useWebSocket();
  return (
    <div className="canvas-host">
      <Canvas shadows camera={{ position: [0, 2, 5], fov: 60 }}>
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
      </Canvas>
      <div style={{ position: 'fixed', bottom: 12, left: 12, color: 'var(--muted)', fontSize: 12 }}>
        WS last: {env ? env.type : 'connecting…'}
      </div>
    </div>
  );
}
