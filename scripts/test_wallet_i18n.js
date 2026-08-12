#!/usr/bin/env node
// test_wallet_i18n.js — check wallet.* keys across zh-CN / en / ja and report
// any missing key in any of the three languages.
//
// Usage: node scripts/test_wallet_i18n.js
// Output: TAP. Exits 0 if all three languages share the same set of wallet.*
// keys, 1 if any key is missing in any language.

const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const LOCALES_DIR = path.join(ROOT, 'ClientWeb', 'src', 'i18n', 'locales');
const LANGS = ['zh-CN', 'ja', 'en'];

const pass = (msg) => { console.log(`ok ${++pass.n} - ${msg}`); };
const fail = (msg) => { console.log(`not ok ${++pass.n} - ${msg}`); fail.list.push(msg); };
pass.n = 0;
fail.list = [];

function extractKeys(content) {
  // Lines look like:  'wallet.title': 'My Wallet',
  // Pull the dotted key wrapped in single or double quotes at start-of-label.
  const re = /^\s*(?:'|")([a-zA-Z_][a-zA-Z0-9_.]*)(?:'|")\s*:/gm;
  const keys = new Set();
  let m;
  while ((m = re.exec(content)) !== null) {
    keys.add(m[1]);
  }
  return keys;
}

const files = LANGS.map((l) => {
  const p = path.join(LOCALES_DIR, `${l}.ts`);
  if (!fs.existsSync(p)) return { lang: l, keys: null, path: p };
  const txt = fs.readFileSync(p, 'utf8');
  return { lang: l, keys: extractKeys(txt), path: p };
});

// Bail with SKIP if any file is missing.
const missing = files.filter((f) => f.keys === null);
if (missing.length) {
  console.log(`1..0 # SKIP locale files missing: ${missing.map((m) => m.lang).join(', ')}`);
  process.exit(0);
}

// Build the universe of wallet.* keys across all langs.
const walletUniverse = new Set();
for (const f of files) {
  for (const k of f.keys) {
    if (k.startsWith('wallet.')) walletUniverse.add(k);
  }
}

let totalChecks = 0;
for (const k of [...walletUniverse].sort()) {
  for (const f of files) {
    totalChecks++;
    if (f.keys.has(k)) {
      pass(`${f.lang} has ${k}`);
    } else {
      fail(`${f.lang} MISSING ${k}`);
    }
  }
}

console.log(`1..${totalChecks}`);
if (fail.list.length) {
  console.log(`# FAIL: ${fail.list.length} wallet.* key(s) are missing in at least one locale`);
  process.exit(1);
}
process.exit(0);
