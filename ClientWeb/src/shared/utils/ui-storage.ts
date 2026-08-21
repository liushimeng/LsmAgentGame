// Lightweight encrypted localStorage helper for the login form.
//
// Stores {account, password, phone, mode, accountPassword, phonePassword} as a
// single AES-GCM-encrypted JSON blob keyed by lsm.auth.ui. The key is derived
// from a deterministic per-browser passphrase (UA + location.host), wrapped with
// PBKDF2 to slow offline brute force on a stolen localStorage dump.
//
// §20260821-05: 支持按登录模式分别保存凭证
//   - account/phone: 通用字段，用于表单回填
//   - accountPassword: 账号登录模式保存的密码
//   - phonePassword: 手机号登录模式保存的密码
//   - 这样切换登录模式时可以自动加载对应的密码
//
// Important properties:
//   - Fails silently to empty defaults on any crypto / parse error.
//   - The passphrase is NOT user-supplied. It is derived from public browser
//     attributes. That makes this obfuscation, not real confidentiality —
//     the security model is "no plaintext password at rest," not
//     "deny the attacker who has my localStorage."

const STORAGE_KEY = 'lsm.auth.ui';
const PBKDF2_ITER = 50_000;
const STORAGE_VERSION = 2; // §20260821-05: 版本标记，用于向前兼容

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
      // §20260821-05: 向前兼容旧格式（无 accountPassword/phonePassword）
      const isV2 = obj.version === STORAGE_VERSION;
      return {
        account: typeof obj.account === 'string' ? obj.account : '',
        password: typeof obj.password === 'string' ? obj.password : '',
        phone: typeof obj.phone === 'string' ? obj.phone : '',
        mode: obj.mode === 'phone' ? 'phone' : 'account',
        savedAt: typeof obj.savedAt === 'number' ? obj.savedAt : 0,
        // v2 新增字段，旧版本默认为空或使用旧的 password
        accountPassword: isV2
          ? (typeof obj.accountPassword === 'string' ? obj.accountPassword : '')
          : (typeof obj.password === 'string' ? obj.password : ''),
        phonePassword: isV2
          ? (typeof obj.phonePassword === 'string' ? obj.phonePassword : '')
          : '',
        version: isV2 ? STORAGE_VERSION : 0,
      };
    } catch {
      return EMPTY;
    }
  },
  clear(): void {
    localStorage.removeItem(STORAGE_KEY);
  },
};
