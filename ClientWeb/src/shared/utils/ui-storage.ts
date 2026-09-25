// Lightweight encrypted localStorage helper for the login form.
//
// 2026-09-25 v4：恢复「记住密码」为 **用户显式 opt-in**（登录页勾选框）。
//   - rememberPassword: 勾选状态持久化；false 时行为与 v3 完全一致（不存密码）
//   - accountPassword/phonePassword: 仅在勾选时写入并回填（§20260821-05 按模式隔离）
//   - 迁移：version < 4 的存量数据密码字段一律置空 —— v2 及更早是「无勾选
//     无条件保存」，用户从未同意，防旧密文在不知情时复活。
//
// 2026-08-25 v3 安全加固的历史结论依然成立，务必知悉：
//   - Encryption (AES-GCM + PBKDF2, key from UA+host) is obfuscation only —
//     同源脚本可复现密钥还原明文。勾选「记住密码」= 用户显式接受该残留风险。
//   - 取消勾选后下次登录成功即以空串覆盖，清除全部存量密码密文。
//
// Important properties:
//   - Fails silently to empty defaults on any crypto / parse error.

const STORAGE_KEY = 'lsm.auth.ui';
const PBKDF2_ITER = 50_000;
const STORAGE_VERSION = 4; // 2026-09-25: v4 起 opt-in 记住密码（v3 及更早 load 时密码清空）

export interface SavedCredentials {
  account: string;
  password: string;
  phone: string;
  mode: 'account' | 'phone';
  savedAt: number;
  // §20260821-05: 新增按模式分别保存的密码
  accountPassword: string;
  phonePassword: string;
  // v4: 「记住密码」勾选状态（默认 false = v3 行为）
  rememberPassword: boolean;
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
  rememberPassword: false,
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
      // v4 迁移门控：仅 v4+ 且用户勾选过「记住密码」才加载密码；
      // v3 及更早（含 v2 无条件保存的存量密文）密码字段一律置空。
      const remember =
        typeof obj.rememberPassword === 'boolean' &&
        obj.rememberPassword === true &&
        typeof obj.version === 'number' &&
        obj.version >= STORAGE_VERSION;
      const str = (v: unknown): string => (typeof v === 'string' ? v : '');
      return {
        account: str(obj.account),
        password: '',
        phone: str(obj.phone),
        mode: obj.mode === 'phone' ? 'phone' : 'account',
        savedAt: typeof obj.savedAt === 'number' ? obj.savedAt : 0,
        accountPassword: remember ? str(obj.accountPassword) : '',
        phonePassword: remember ? str(obj.phonePassword) : '',
        rememberPassword: remember,
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
