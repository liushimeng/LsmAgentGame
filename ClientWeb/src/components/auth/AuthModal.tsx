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
        {mode === 'login'
          ? <LoginForm onSwitch={() => setMode('register')} />
          : <RegisterForm onSwitch={() => setMode('login')} />}
      </div>
    </div>
  );
}
