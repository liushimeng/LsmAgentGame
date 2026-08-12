// Lobby scene — a slow, ambient, low-poly cube + ground plane. Purely
// decorative; demonstrates R3F plumbing without committing to a game design.

import { useRef } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { OrbitControls } from '@react-three/drei';
import type { Mesh } from 'three';

function Spinner() {
  const ref = useRef<Mesh>(null);
  useFrame((_, dt) => {
    if (!ref.current) return;
    ref.current.rotation.x += dt * 0.2;
    ref.current.rotation.y += dt * 0.3;
  });
  return (
    <mesh ref={ref} position={[0, 0.6, 0]} castShadow>
      <boxGeometry args={[1.2, 1.2, 1.2]} />
      <meshStandardMaterial color="#0a84ff" />
    </mesh>
  );
}

export function LobbyScene() {
  return (
    <Canvas shadows camera={{ position: [3, 2.5, 4], fov: 50 }}>
      <ambientLight intensity={0.4} />
      <directionalLight position={[5, 8, 5]} intensity={0.9} castShadow />
      <Spinner />
      <mesh receiveShadow rotation={[-Math.PI / 2, 0, 0]} position={[0, -0.5, 0]}>
        <planeGeometry args={[20, 20]} />
        <meshStandardMaterial color="#161b22" />
      </mesh>
      <OrbitControls enablePan={false} />
    </Canvas>
  );
}
