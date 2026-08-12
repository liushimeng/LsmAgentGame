import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { useI18nStore } from './store/i18n.store';
import './styles/globals.css';

// 启动时根据持久化的语言偏好同步 <html lang>,无障碍/SEO 友好。
document.documentElement.lang = useI18nStore.getState().lang;

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </React.StrictMode>,
);
