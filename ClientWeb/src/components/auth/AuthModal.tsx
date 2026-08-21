import { useState } from 'react';
import { LoginForm } from './LoginForm';
import { RegisterForm } from './RegisterForm';
import { useT } from '@/hooks/useT';

export function AuthModal() {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const t = useT();
  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true">
      <div className="modal">
        <h2>{mode === 'login' ? t('auth.signIn') : t('auth.createAccount')}</h2>
        <div className="tabs" data-testid="auth-modal-tabs">
          <button
            type="button"
            data-testid="auth-tab-login"
            className={mode === 'login' ? 'active' : ''}
            onClick={() => setMode('login')}
          >
            {t('auth.signIn')}
          </button>
          <button
            type="button"
            data-testid="auth-tab-register"
            className={mode === 'register' ? 'active' : ''}
            onClick={() => setMode('register')}
          >
            {t('auth.register')}
          </button>
        </div>
        {/* §20260821-05: 使用 CSS display 控制显示/隐藏，保持组件挂载以保留状态 */}
        <div style={{ display: mode === 'login' ? 'block' : 'none' }}>
          <LoginForm onSwitch={() => setMode('register')} />
        </div>
        <div style={{ display: mode === 'register' ? 'block' : 'none' }}>
          <RegisterForm onSwitch={() => setMode('login')} />
        </div>
      </div>
    </div>
  );
}
