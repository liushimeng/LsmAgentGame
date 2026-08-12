// Tiny re-export so feature code can `import { useAuth } from '@/hooks/useAuth'`.
import { useAuthStore } from '@/store/auth.store';
export const useAuth = useAuthStore;
