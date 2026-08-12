import { useEffect, useState } from 'react';
import { versionService } from '@/services/auth.service';
import { useT } from '@/hooks/useT';
import type { VersionInfo } from '@/types/api';

// 关于页 —— 展示项目与版本信息。
export function AboutPage() {
  const [ver, setVer] = useState<VersionInfo | null>(null);
  const t = useT();

  useEffect(() => {
    versionService.get().then(setVer).catch(() => setVer(null));
  }, []);

  return (
    <div>
      <h1 style={{ marginTop: 0 }}>{t('about.title')}</h1>
      <div className="panel-card">
        <div className="kv">
          <span className="kv__k">{t('about.version')}</span>
          <span className="kv__v">
            {ver?.version ?? '—'}
            {ver?.git_sha && ver.git_sha !== 'nogit' && (
              <span className="kv__hint" style={{ marginLeft: 8, opacity: 0.7 }}>
                ({ver.git_sha})
              </span>
            )}
          </span>
        </div>
        <div className="kv">
          <span className="kv__k">{t('about.gitSha')}</span>
          <span className="kv__v">
            {ver?.git_sha
              ? (ver.git_sha === 'nogit' ? t('about.gitShaUnknown') : ver.git_sha)
              : '—'}
          </span>
        </div>
        <div className="kv">
          <span className="kv__k">{t('about.buildTime')}</span>
          <span className="kv__v">{ver?.build_time ?? '—'}</span>
        </div>
      </div>
    </div>
  );
}
