// Lightweight encrypted localStorage helper for the login form.
//
// 2026-08-25 安全加固：**不再持久化任何密码**。仅保存表单回填所需的
// 非敏感字段 {account, phone, mode}。旧版本（≤v2）写入的密码字段在 load
// 迁移时一律置空，且下次 save 用 v3 覆盖，存量密文随之失效。
//   - account/phone: 通用字段，用于表单回填
//   - accountPassword/phonePassword: 兼容字段，恒为空串
//
// Important properties:
//   - Fails silently to empty defaults on any crypto / parse error.
//   - Encryption (AES-GCM + PBKDF2, key from UA+host) is obfuscation only —
//     这也是不再存密码的原因：同源脚本可复现密钥。

const STORAGE_KEY = 'lsm.auth.ui';
const PBKDF2_ITER = 50_000;
const STORAGE_VERSION = 3; // 2026-08-25: v3 起不存密码（v2 及更早的密码字段 load 时清空）

export interface SavedCredentials {
  account: string;
  password: string;
  phone: string;
  mode: 'account' | 'phone';
  savedAt: number;
  // §20260821-05: 新增按模式分别保存的密码
  accountPassword: string;
  phonePassword: string;
  version: number;
}

const EMPTY: SavedCredentials = {
  account: '',
  password: '',
  phone: '',
  mode: 'account',
  savedAt: 0,
  accountPassword: '',
  phonePassword: '',
  version: STORAGE_VERSION,
};

function b64encode(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let s = '';
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s);
}
function b64decode(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function concat(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

async function deriveKey(): Promise<CryptoKey> {
  const raw = `${navigator.userAgent}|${location.host}`;
  const enc = new TextEncoder();
  const baseMat = await crypto.subtle.digest('SHA-256', enc.encode(raw));
  const salt = enc.encode('lsm.auth.ui.v1');
  const baseKey = await crypto.subtle.importKey('raw', baseMat, 'PBKDF2', false, ['deriveKey']);
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt, iterations: PBKDF2_ITER, hash: 'SHA-256' },
    baseKey,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

async function encryptString(plain: string): Promise<string | null> {
  try {
    const key = await deriveKey();
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const ct = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv },
      key,
      new TextEncoder().encode(plain),
    );
    return b64encode(concat(iv, new Uint8Array(ct)));
  } catch {
    return null;
  }
}

async function decryptString(token: string): Promise<string | null> {
  try {
    const key = await deriveKey();
    const buf = b64decode(token);
    const iv = buf.slice(0, 12);
    const ct = buf.slice(12);
    const pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ct);
    return new TextDecoder().decode(pt);
  } catch {
    return null;
  }
}

export const uiStorage = {
  async save(creds: Omit<SavedCredentials, 'savedAt' | 'version'>): Promise<void> {
    const payload: SavedCredentials = { ...creds, savedAt: Date.now(), version: STORAGE_VERSION };
    const cipher = await encryptString(JSON.stringify(payload));
    if (cipher == null) return;
    localStorage.setItem(STORAGE_KEY, cipher);
  },
  async load(): Promise<SavedCredentials> {
    const cipher = localStorage.getItem(STORAGE_KEY);
    if (!cipher) return EMPTY;
    const plain = await decryptString(cipher);
    if (!plain) return EMPTY;
    try {
      const obj = JSON.parse(plain) as Partial<SavedCredentials>;
      // 2026-08-25: v3 起不加载任何密码（含旧版本迁移），密码字段恒为空。
      return {
        account: typeof obj.account === 'string' ? obj.account : '',
        password: '',
        phone: typeof obj.phone === 'string' ? obj.phone : '',
        mode: obj.mode === 'phone' ? 'phone' : 'account',
        savedAt: typeof obj.savedAt === 'number' ? obj.savedAt : 0,
        accountPassword: '',
        phonePassword: '',
        version: STORAGE_VERSION,
      };
    } catch {
      return EMPTY;
    }
  },
  clear(): void {
    localStorage.removeItem(STORAGE_KEY);
  },
};
